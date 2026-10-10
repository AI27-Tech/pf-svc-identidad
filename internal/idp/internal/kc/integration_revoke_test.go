//go:build integration
// +build integration

package kc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// TestServicioNoModificaUsuarios verifica R7.4 / A-16 (DEC-073): la cuenta de
// servicio sigue el principio de privilegio mínimo. Puede, a lo sumo, terminar
// sesiones; NUNCA modificar usuarios. La prueba intenta un PUT a un usuario con
// el token de la cuenta de servicio y exige 403: si pudiera modificarlo, la
// prueba falla, de modo que la opción de permisos del realm queda verificada,
// no supuesta.
func TestServicioNoModificaUsuarios(t *testing.T) {
	kc := arranqueKeycloak(t)
	svc := kc.tokenServicio(t)

	userID := subPorUsuario["factory-monitorista"]
	cuerpo := []byte(`{"attributes":{"tenant_id":["ffff0000-0000-0000-0000-000000000000"]}}`)

	status, _ := kc.peticionAdmin(t, "PUT", "/users/"+userID, svc, cuerpo)
	if status != 403 {
		t.Errorf("PUT a un usuario con la cuenta de servicio respondió %d; se esperaba 403 (R7.4, DEC-073: la cuenta nunca modifica usuarios)", status)
	}

	// Confirmar que el tenant_id no cambió, pase lo que pase con el status.
	admin := kc.tokenAdmin(t)
	st, datos := kc.peticionAdmin(t, "GET", "/users/"+userID, admin, nil)
	if st != 200 {
		t.Fatalf("GET admin del usuario respondió %d: %s", st, datos)
	}
	var u struct {
		Attributes map[string][]string `json:"attributes"`
	}
	if err := json.Unmarshal(datos, &u); err != nil {
		t.Fatalf("decodificar usuario: %v", err)
	}
	esperado := tenantPorUsuario["factory-monitorista"]
	if len(u.Attributes["tenant_id"]) != 1 || u.Attributes["tenant_id"][0] != esperado {
		t.Errorf("la cuenta de servicio modificó el tenant_id: %v (R7.4)", u.Attributes["tenant_id"])
	}
}

// TestLoginNoCierraSesionPorAdmin verifica R7.7: con la credencial del cliente
// de login, la API de administración rechaza cerrar sesiones (el cliente no
// tiene cuenta de servicio ni roles de administración).
func TestLoginNoCierraSesionPorAdmin(t *testing.T) {
	kc := arranqueKeycloak(t)
	prov := kc.adaptador(t)

	// Emitir para tener un access token del cliente de login.
	tok, err := emitirConTOTP(t, prov, "factory-administrador")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Intentar borrar una sesión arbitraria por la API de administración con el
	// token del cliente de login: debe rechazar.
	status, _ := kc.peticionAdmin(t, "DELETE", "/sessions/00000000-0000-0000-0000-000000000000", tok.AccessToken, nil)
	if status != 401 && status != 403 {
		t.Errorf("DELETE /sessions con token de login respondió %d; se esperaba 401/403 (R7.7)", status)
	}
}

// TestRevocarUnaSesion verifica R7.1 y R7.5: dos sesiones del mismo usuario;
// revocar una por el adaptador deja la otra viva (su refresh sigue funcionando)
// y el Revoke es idempotente.
//
// Depende de que la cuenta de servicio tenga el permiso MÍNIMO para terminar
// sesiones (A-16/DEC-073: FGAP v2 como primera opción, Logout OIDC como
// alternativa). La prueba falla con un mensaje claro si el permiso no permite
// la terminación de sesión, para que la opción del realm se corrija contra la
// versión fijada de Keycloak (A-7) en vez de quedar como supuesto.
func TestRevocarUnaSesion(t *testing.T) {
	kc := arranqueKeycloak(t)
	prov := kc.adaptador(t)

	usuario := "factory-coordinador"
	nuevaSesion := func() idp.Token {
		tok, err := emitirConTOTP(t, prov, usuario)
		if err != nil {
			t.Fatalf("Issue (%s): %v", usuario, err)
		}
		return tok
	}

	// El realm tiene revokeRefreshToken + maxReuse=0, así que cada emisión
	// nueva del mismo usuario abre una sesión distinta.
	sesionA := nuevaSesion()
	sesionB := nuevaSesion()

	// Resolver el id de la sesión de Keycloak de A a partir de su access token.
	verifierSesion := kc.sesionIDDeToken(t, sesionA.AccessToken)

	// Revocar la sesión A por el adaptador (usa el service token).
	if err := prov.Revoke(context.Background(), verifierSesion); err != nil {
		t.Fatalf("Revoke de la sesión A falló: %v (A-16/DEC-073: la cuenta de servicio debe poder terminar sesiones)", err)
	}
	// Idempotente: un segundo Revoke no cambia nada ni error.
	if err := prov.Revoke(context.Background(), verifierSesion); err != nil {
		t.Fatalf("segundo Revoke (idempotente) falló: %v", err)
	}

	// La sesión B sigue viva: su refresh funciona.
	if _, err := prov.Refresh(context.Background(), sesionB.RefreshToken); err != nil {
		t.Errorf("la sesión B dejó de refrescar tras revocar A (R7.5): %v", err)
	}
	// La sesión A ya no refresca.
	if _, err := prov.Refresh(context.Background(), sesionA.RefreshToken); err == nil {
		t.Error("la sesión A siguió refrescando tras ser revocada (R7.1)")
	}
}

// sesionIDDeToken lee el claim sesion_id de un access token emitido, para usarlo
// como argumento de Revoke (con SessionResolver nil, Revoke lo usa como id de
// sesión de Keycloak: A-4 identidad).
func (k kcDePrueba) sesionIDDeToken(t *testing.T, accessToken string) string {
	t.Helper()
	v := auth.NewVerifier(k.jwksURL(), "ai27-internal", time.Minute)
	c, err := v.VerifyToken(context.Background(), accessToken)
	if err != nil {
		t.Fatalf("verificar token para leer sesion_id: %v", err)
	}
	if c.SesionID == "" {
		t.Fatal("el token no trae sesion_id; no se puede revocar por id de sesión")
	}
	return c.SesionID
}
