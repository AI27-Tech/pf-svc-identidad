//go:build integration
// +build integration

package kc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/roles"
	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/claims"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// Feature: e4-t1-adaptador-idp-keycloak, Property 13: Coherencia de claims en emisión y refresco (integración)
//
// Para cada factory-<rol> con modelo vigente m: emitir, refrescar y verificar
// con auth.NewVerifier contra el JWKS del contenedor, comparando con
// claims.Componer(m). La Audiencia se lee del claim de dominio en Raw; el token
// no contiene identificadores de alcance.
//
// Con A-3 (DEC-071) y A-13 (DEC-072 opción c) resueltos, las aserciones de
// emisión exitosa y de `sub` ya no se omiten (DEC-074).
func TestPropiedadCoherenciaClaims(t *testing.T) {
	kc := arranqueKeycloak(t)
	prov := kc.adaptador(t)

	// Todos los tokens del realm llevan la audiencia técnica ai27-internal
	// (audience mapper del scope ai27-claims). La Audiencia de DOMINIO
	// (Cliente/Interno) viaja en el claim personalizado `audiencia`.
	verifier := auth.NewVerifier(kc.jwksURL(), "ai27-internal", time.Minute)

	for _, rol := range roles.Todos {
		rol := rol
		usuario := "factory-" + string(rol)
		t.Run(usuario, func(t *testing.T) {
			m := modeloDe(usuario, rol)
			esperado := claims.Componer(m)

			cred := idp.Credentials{
				Usuario:       usuario,
				Contrasena:    contrasenaFactory,
				SegundoFactor: factorPara(usuario),
			}
			emitido, err := prov.Issue(context.Background(), cred)
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			verificarClaims(t, verifier, emitido.AccessToken, m, esperado, "emisión")

			if emitido.RefreshToken == "" {
				t.Fatal("la emisión no entregó refresh token")
			}
			refrescado, err := prov.Refresh(context.Background(), emitido.RefreshToken)
			if err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			verificarClaims(t, verifier, refrescado.AccessToken, m, esperado, "refresco")
		})
	}
}

// verificarClaims verifica un access token contra el JWKS y compara con el
// modelo esperado.
func verificarClaims(t *testing.T, v *auth.Verifier, accessToken string, m autorizacion.ModeloVigente, esperado claims.Compuestos, etapa string) {
	t.Helper()

	c, err := v.VerifyToken(context.Background(), accessToken)
	if err != nil {
		t.Fatalf("[%s] VerifyToken: %v", etapa, err)
	}

	if c.Subject != esperadoSujeto(m) {
		t.Errorf("[%s] sub = %q; se esperaba %q (A-13/DEC-072 c)", etapa, c.Subject, esperadoSujeto(m))
	}
	if c.TenantID != esperado.TenantID {
		t.Errorf("[%s] tenant_id = %q; se esperaba %q", etapa, c.TenantID, esperado.TenantID)
	}
	if c.Rol != string(esperado.Rol) {
		t.Errorf("[%s] rol = %q; se esperaba %q", etapa, c.Rol, esperado.Rol)
	}
	// La Audiencia de dominio debe leerse del claim `audiencia` en Raw (no del
	// respaldo derivado del rol que aplica el verifier si falta el claim).
	audRaw, ok := c.Raw["audiencia"].(string)
	if !ok || audRaw == "" {
		t.Errorf("[%s] el token no trae el claim de dominio `audiencia`", etapa)
	} else if auth.Audiencia(audRaw) != esperado.Audiencia {
		t.Errorf("[%s] audiencia = %q; se esperaba %q", etapa, audRaw, esperado.Audiencia)
	}
	if c.SesionID == "" {
		t.Errorf("[%s] sesion_id vacío; el mapper de sesión debe poblarlo", etapa)
	}

	// El token no debe contener identificadores de alcance (DEC-053).
	for _, prohibido := range []string{"alcance", "scope_unidad", "unidades", "clientes_alcance"} {
		if _, existe := c.Raw[prohibido]; existe {
			t.Errorf("[%s] el token contiene un claim de alcance prohibido %q", etapa, prohibido)
		}
	}
}

// TestCambioRolRefresco verifica R6.7: mover cambio-rol de grupo con la
// credencial administrativa y refrescar recompone el rol desde el modelo
// vigente (ADR 16).
func TestCambioRolRefresco(t *testing.T) {
	kc := arranqueKeycloak(t)
	prov := kc.adaptador(t)
	admin := kc.tokenAdmin(t)

	verifier := auth.NewVerifier(kc.jwksURL(), "ai27-internal", time.Minute)

	// Emitir con cambio-rol (hoy en /monitorista).
	cred := idp.Credentials{
		Usuario:       "cambio-rol",
		Contrasena:    contrasenaFactory,
		SegundoFactor: factorPara("cambio-rol"),
	}
	emitido, err := prov.Issue(context.Background(), cred)
	if err != nil {
		t.Fatalf("Issue cambio-rol: %v", err)
	}
	c, err := verifier.VerifyToken(context.Background(), emitido.AccessToken)
	if err != nil {
		t.Fatalf("VerifyToken inicial: %v", err)
	}
	if c.Rol != string(roles.RolMonitorista) {
		t.Fatalf("rol inicial = %q; se esperaba monitorista", c.Rol)
	}

	// Mover de grupo: quitar /monitorista y poner /supervisor con el admin.
	userID := "33333333-0000-0000-0000-000000000001"
	gidMonitorista := idGrupo(t, kc, admin, "monitorista")
	gidSupervisor := idGrupo(t, kc, admin, "supervisor")

	if st, cuerpo := kc.peticionAdmin(t, "DELETE", "/users/"+userID+"/groups/"+gidMonitorista, admin, nil); st != 204 {
		t.Fatalf("quitar de /monitorista respondió %d: %s", st, cuerpo)
	}
	if st, cuerpo := kc.peticionAdmin(t, "PUT", "/users/"+userID+"/groups/"+gidSupervisor, admin, nil); st != 204 {
		t.Fatalf("agregar a /supervisor respondió %d: %s", st, cuerpo)
	}

	// Refrescar: el token recompone el rol desde el modelo vigente.
	refrescado, err := prov.Refresh(context.Background(), emitido.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh tras cambio de grupo: %v", err)
	}
	c2, err := verifier.VerifyToken(context.Background(), refrescado.AccessToken)
	if err != nil {
		t.Fatalf("VerifyToken refrescado: %v", err)
	}
	if c2.Rol != string(roles.RolSupervisor) {
		t.Errorf("rol tras refresco = %q; se esperaba supervisor (ADR 16, R6.7)", c2.Rol)
	}
}

// idGrupo busca el id de un grupo de primer nivel por nombre con el admin.
func idGrupo(t *testing.T, kc kcDePrueba, admin, nombre string) string {
	t.Helper()
	st, cuerpo := kc.peticionAdmin(t, "GET", "/groups?search="+nombre, admin, nil)
	if st != 200 {
		t.Fatalf("GET /groups?search=%s respondió %d: %s", nombre, st, cuerpo)
	}
	var grupos []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(cuerpo, &grupos); err != nil {
		t.Fatalf("decodificar grupos: %v", err)
	}
	for _, g := range grupos {
		if g.Name == nombre {
			return g.ID
		}
	}
	t.Fatalf("no se encontró el grupo %q", nombre)
	return ""
}
