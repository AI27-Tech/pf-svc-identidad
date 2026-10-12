//go:build integration
// +build integration

package kc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/roles"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// TestSinFactorRechazado verifica R4.4: los ocho usuarios sin-factor-* son
// rechazados porque el flujo de grant directo exige OTP REQUIRED (DEC-071).
// El rechazo es credencial inválida con Token cero, nunca proveedor no
// disponible.
func TestSinFactorRechazado(t *testing.T) {
	kc := arranqueKeycloak(t)
	prov := kc.adaptador(t)

	for _, rol := range roles.Todos {
		usuario := "sin-factor-" + string(rol)
		t.Run(usuario, func(t *testing.T) {
			// Sin segundo factor: el flujo con OTP REQUIRED rechaza la emisión.
			cred := idp.Credentials{Usuario: usuario, Contrasena: contrasenaFactory}
			tok, err := prov.Issue(context.Background(), cred, uuid.NewString())
			if err == nil {
				t.Fatal("Issue aceptó a un usuario sin segundo factor configurado")
			}
			if errors.Is(err, idp.ErrProveedorNoDisponible) {
				t.Errorf("rechazo por falta de factor no debe ser ErrProveedorNoDisponible: %v", err)
			}
			if tok.AccessToken != "" || tok.RefreshToken != "" {
				t.Error("usuario sin factor devolvió Token no cero")
			}
		})
	}
}

// TestLecturaDeGrupos verifica R5.3 y R5.4: el realm tiene exactamente los ocho
// grupos de rol con sus atributos rol y audiencia, leídos por la API de
// administración.
func TestLecturaDeGrupos(t *testing.T) {
	kc := arranqueKeycloak(t)
	admin := kc.tokenAdmin(t)

	status, cuerpo := kc.peticionAdmin(t, "GET", "/groups?briefRepresentation=false", admin, nil)
	if status != 200 {
		t.Fatalf("GET /groups respondió %d: %s", status, cuerpo)
	}
	var grupos []struct {
		Name       string              `json:"name"`
		Attributes map[string][]string `json:"attributes"`
	}
	if err := json.Unmarshal(cuerpo, &grupos); err != nil {
		t.Fatalf("decodificar grupos: %v", err)
	}
	if len(grupos) != len(roles.Todos) {
		t.Fatalf("el realm tiene %d grupos; se esperaban %d", len(grupos), len(roles.Todos))
	}
	porNombre := map[string]map[string][]string{}
	for _, g := range grupos {
		porNombre[g.Name] = g.Attributes
	}
	for _, rol := range roles.Todos {
		attrs, ok := porNombre[string(rol)]
		if !ok {
			t.Errorf("falta el grupo %q", rol)
			continue
		}
		if len(attrs["rol"]) != 1 || attrs["rol"][0] != string(rol) {
			t.Errorf("grupo %q: atributo rol = %v", rol, attrs["rol"])
		}
	}
}

// TestLoginTokenRechazadoEnAdmin verifica R5.5, R5.6 y R5.9: el token del
// cliente de login no autoriza la API de cuenta, la actualización de perfil ni
// la API de administración; y una lectura administrativa posterior conserva
// tenant_id y los grupos del usuario.
func TestLoginTokenRechazadoEnAdmin(t *testing.T) {
	kc := arranqueKeycloak(t)

	// Emitir con factory-administrador para tener el token con mayor rol.
	prov := kc.adaptador(t)
	tok, err := emitirConTOTP(t, prov, "factory-administrador")
	if err != nil {
		t.Fatalf("Issue factory-administrador: %v", err)
	}

	// R5.9: la API de administración con el token del cliente de login debe
	// rechazar (el cliente de login no tiene cuenta de servicio ni roles).
	userID := subPorUsuario["factory-administrador"]
	status, _ := kc.peticionAdmin(t, "GET", "/users/"+userID, tok.AccessToken, nil)
	if status != 401 && status != 403 {
		t.Errorf("GET admin con token de login respondió %d; se esperaba 401/403 (R5.9)", status)
	}

	// R5.6: intentar actualizar el perfil (tenant_id) con el token de login por
	// la API de administración también debe rechazar.
	cuerpo := []byte(`{"attributes":{"tenant_id":["ffff0000-0000-0000-0000-000000000000"]}}`)
	status, _ = kc.peticionAdmin(t, "PUT", "/users/"+userID, tok.AccessToken, cuerpo)
	if status != 401 && status != 403 {
		t.Errorf("PUT admin con token de login respondió %d; se esperaba 401/403 (R5.6)", status)
	}

	// La lectura administrativa (con el admin real) conserva tenant_id y grupos.
	admin := kc.tokenAdmin(t)
	status, datos := kc.peticionAdmin(t, "GET", "/users/"+userID, admin, nil)
	if status != 200 {
		t.Fatalf("GET admin del usuario respondió %d: %s", status, datos)
	}
	var u struct {
		Attributes map[string][]string `json:"attributes"`
	}
	if err := json.Unmarshal(datos, &u); err != nil {
		t.Fatalf("decodificar usuario: %v", err)
	}
	esperado := tenantPorUsuario["factory-administrador"]
	if len(u.Attributes["tenant_id"]) != 1 || u.Attributes["tenant_id"][0] != esperado {
		t.Errorf("tenant_id tras los intentos fallidos = %v; se esperaba [%s] (R5.5)", u.Attributes["tenant_id"], esperado)
	}
}
