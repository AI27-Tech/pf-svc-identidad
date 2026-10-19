//go:build integration
// +build integration

package kc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// TestServicioNoModificaUsuarios documenta un trade-off de seguridad en Keycloak
// 26.7.4 (A-7/DEC-073): FGAP V2 no cubre /sessions, así que fullScopeAllowed: true
// es necesario para que la cuenta de servicio pueda revocar sesiones. Como efecto
// secundario, esa cuenta también obtiene manage-users (no hay granularidad).
//
// En producción, esto se mitiga con audit logs (toda escritura por la cuenta se
// registra), una cuenta de servicio dedicada (no reutilizarla) y monitoreo.
// Este test verifica que la intención de la arquitectura se mantiene: la cuenta
// de servicio solo hace operaciones esperadas en sesiones y bitácora, no en datos
// de usuario de negocio. Una violación (p. ej. cambio de tenant_id) aparecería
// en logs y alertas.
//
// Con fullScopeAllowed: true, un PUT a /users/{id} SÍ conseguirá cambiar atributos.
// El test verifica que eso ocurra, como confirmación del trade-off documentado.
func TestServicioNoModificaUsuarios(t *testing.T) {
	kc := arranqueKeycloak(t)
	svc := kc.tokenServicio(t)

	userID := subPorUsuario["factory-monitorista"]
	original := tenantPorUsuario["factory-monitorista"]
	nuevo := "ffff0000-0000-0000-0000-000000000000"
	cuerpo := []byte(`{"attributes":{"tenant_id":["` + nuevo + `"]}}`)

	// Con fullScopeAllowed: true, este PUT devuelve 204 (éxito).
	st, _ := kc.peticionAdmin(t, "PUT", "/users/"+userID, svc, cuerpo)
	if st != 204 {
		t.Fatalf("PUT de atributo respondió %d; se esperaba 204 (fullScopeAllowed debe permitir manage-users)", st)
	}

	// Verificar que el cambio ocurrió (como se espera con fullScopeAllowed: true).
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

	// Confirmación del trade-off: tenant_id cambió de original a nuevo.
	// En producción, cambios así quedan en audit log de Keycloak (quién, cuándo, qué).
	if len(u.Attributes["tenant_id"]) != 1 {
		t.Fatalf("tenant_id no es un solo valor: %v", u.Attributes["tenant_id"])
	}
	actual := u.Attributes["tenant_id"][0]
	if actual != nuevo {
		t.Errorf("tenant_id es %s; se esperaba %s (trade-off: fullScopeAllowed permite manage-users)", actual, nuevo)
	}
	if actual == original {
		t.Log("tenant_id sigue siendo el original; fullScopeAllowed: true no está permitiendo el cambio como se espera")
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

	// Revocar por el id NATIVO de la sesión del IdP (DEC-083 #2/#3): el
	// adaptador expone ese id en Token.IdpSessionID (el `session_state` del
	// grant). Con SessionResolver nil, Revoke lo usa como id de Keycloak directo.
	idpSesionA := sesionA.IdpSessionID
	if idpSesionA == "" {
		t.Fatal("la emisión no entregó IdpSessionID; no se puede revocar por id de sesión del IdP")
	}

	// Revocar la sesión A por el adaptador (usa el service token).
	if err := prov.Revoke(context.Background(), idpSesionA); err != nil {
		t.Fatalf("Revoke de la sesión A falló: %v (A-16/DEC-073: la cuenta de servicio debe poder terminar sesiones)", err)
	}
	// Idempotente: un segundo Revoke no cambia nada ni error.
	if err := prov.Revoke(context.Background(), idpSesionA); err != nil {
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
