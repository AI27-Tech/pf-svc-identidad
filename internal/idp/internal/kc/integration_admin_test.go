//go:build integration
// +build integration

package kc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// adminDePrueba construye el adaptador real contra el contenedor y lo devuelve
// como idp.IdpAdmin. Reusa el mismo New del adaptador: la administración de
// usuarios es la misma implementación que la emisión/revocación.
func (k kcDePrueba) adminIdp(t *testing.T) idp.IdpAdmin {
	t.Helper()
	a, err := New(k.configAdaptador())
	if err != nil {
		t.Fatalf("New(adminIdp): %v", err)
	}
	return a
}

// TestAdminCrearUsuarioAltaYGrupo verifica el alta real contra Keycloak
// (Requisito 7.2/7.5): CrearUsuario crea el usuario, le fija el atributo
// tenant_id y lo coloca en el grupo del rol inicial. La lectura administrativa
// confirma el estado.
func TestAdminCrearUsuarioAltaYGrupo(t *testing.T) {
	kc := arranqueKeycloak(t)
	admin := kc.adminIdp(t)
	tokenAdmin := kc.tokenAdmin(t)

	tenantID := "aaaa0000-0000-0000-0000-0000000000ff"
	usuario := "alta-" + uuid.NewString()[:8] + "@ai27.test"

	sub, err := admin.CrearUsuario(context.Background(), idp.NuevoUsuario{
		Usuario:  usuario,
		Nombre:   "Usuario De Alta",
		TenantID: tenantID,
		Rol:      "monitorista",
	})
	if err != nil {
		t.Fatalf("CrearUsuario: %v", err)
	}
	if sub == "" {
		t.Fatal("CrearUsuario devolvió sub vacío")
	}

	// Lectura administrativa: el usuario existe, enabled, con tenant_id.
	status, datos := kc.peticionAdmin(t, "GET", "/users/"+sub, tokenAdmin, nil)
	if status != 200 {
		t.Fatalf("GET /users/%s respondió %d: %s", sub, status, datos)
	}
	var u struct {
		Enabled    bool                `json:"enabled"`
		Attributes map[string][]string `json:"attributes"`
	}
	if err := json.Unmarshal(datos, &u); err != nil {
		t.Fatalf("decodificar usuario: %v", err)
	}
	if !u.Enabled {
		t.Error("el usuario recién creado no quedó habilitado")
	}
	if len(u.Attributes["tenant_id"]) != 1 || u.Attributes["tenant_id"][0] != tenantID {
		t.Errorf("tenant_id = %v; se esperaba [%s]", u.Attributes["tenant_id"], tenantID)
	}

	// El usuario quedó en el grupo del rol inicial.
	status, datos = kc.peticionAdmin(t, "GET", "/users/"+sub+"/groups", tokenAdmin, nil)
	if status != 200 {
		t.Fatalf("GET /users/%s/groups respondió %d: %s", sub, status, datos)
	}
	var grupos []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(datos, &grupos); err != nil {
		t.Fatalf("decodificar grupos: %v", err)
	}
	if len(grupos) != 1 || grupos[0].Name != "monitorista" {
		t.Errorf("grupos del usuario = %v; se esperaba [monitorista]", grupos)
	}
}

// TestAdminCrearUsuarioIdempotente verifica que un segundo alta con el mismo
// identificador devuelve el mismo sub sin crear un segundo usuario.
func TestAdminCrearUsuarioIdempotente(t *testing.T) {
	kc := arranqueKeycloak(t)
	admin := kc.adminIdp(t)

	u := idp.NuevoUsuario{
		Usuario:  "idem-" + uuid.NewString()[:8] + "@ai27.test",
		Nombre:   "Usuario Idempotente",
		TenantID: "aaaa0000-0000-0000-0000-0000000000fe",
		Rol:      "ejecutivo",
	}

	sub1, err := admin.CrearUsuario(context.Background(), u)
	if err != nil {
		t.Fatalf("primer CrearUsuario: %v", err)
	}
	sub2, err := admin.CrearUsuario(context.Background(), u)
	if err != nil {
		t.Fatalf("segundo CrearUsuario (idempotente): %v", err)
	}
	if sub1 != sub2 {
		t.Errorf("idempotencia violada: sub1=%q, sub2=%q", sub1, sub2)
	}
}

// TestAdminMoverDeGrupo verifica el Reemplazo real (Requisito 7.6): tras mover
// de grupo, el usuario queda SOLO en el grupo nuevo, nunca en dos a la vez.
func TestAdminMoverDeGrupo(t *testing.T) {
	kc := arranqueKeycloak(t)
	admin := kc.adminIdp(t)
	tokenAdmin := kc.tokenAdmin(t)

	sub, err := admin.CrearUsuario(context.Background(), idp.NuevoUsuario{
		Usuario:  "mover-" + uuid.NewString()[:8] + "@ai27.test",
		Nombre:   "Usuario A Mover",
		TenantID: "aaaa0000-0000-0000-0000-0000000000fd",
		Rol:      "monitorista",
	})
	if err != nil {
		t.Fatalf("CrearUsuario: %v", err)
	}

	// Mover de monitorista a supervisor.
	if err := admin.MoverDeGrupo(context.Background(), sub, "monitorista", "supervisor"); err != nil {
		t.Fatalf("MoverDeGrupo: %v", err)
	}

	status, datos := kc.peticionAdmin(t, "GET", "/users/"+sub+"/groups", tokenAdmin, nil)
	if status != 200 {
		t.Fatalf("GET grupos respondió %d: %s", status, datos)
	}
	var grupos []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(datos, &grupos); err != nil {
		t.Fatalf("decodificar grupos: %v", err)
	}
	// Nunca dos grupos de rol a la vez (DEC-084): solo supervisor.
	if len(grupos) != 1 || grupos[0].Name != "supervisor" {
		t.Errorf("tras mover, grupos = %v; se esperaba [supervisor] (DEC-084)", grupos)
	}
}

// TestAdminDeshabilitarYHabilitar verifica el ciclo de estado real: tras
// deshabilitar el usuario queda enabled=false, y tras habilitar enabled=true.
// Ambas operaciones son idempotentes.
func TestAdminDeshabilitarYHabilitar(t *testing.T) {
	kc := arranqueKeycloak(t)
	admin := kc.adminIdp(t)
	tokenAdmin := kc.tokenAdmin(t)

	sub, err := admin.CrearUsuario(context.Background(), idp.NuevoUsuario{
		Usuario:  "estado-" + uuid.NewString()[:8] + "@ai27.test",
		Nombre:   "Usuario De Estado",
		TenantID: "aaaa0000-0000-0000-0000-0000000000fc",
		Rol:      "reaccion",
	})
	if err != nil {
		t.Fatalf("CrearUsuario: %v", err)
	}

	enabledDe := func() bool {
		status, datos := kc.peticionAdmin(t, "GET", "/users/"+sub, tokenAdmin, nil)
		if status != 200 {
			t.Fatalf("GET /users/%s respondió %d: %s", sub, status, datos)
		}
		var u struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(datos, &u); err != nil {
			t.Fatalf("decodificar usuario: %v", err)
		}
		return u.Enabled
	}

	// Deshabilitar (idempotente).
	if err := admin.DeshabilitarUsuario(context.Background(), sub); err != nil {
		t.Fatalf("DeshabilitarUsuario: %v", err)
	}
	if err := admin.DeshabilitarUsuario(context.Background(), sub); err != nil {
		t.Fatalf("DeshabilitarUsuario (idempotente): %v", err)
	}
	if enabledDe() {
		t.Error("tras deshabilitar, el usuario sigue enabled=true")
	}

	// Habilitar (idempotente).
	if err := admin.HabilitarUsuario(context.Background(), sub); err != nil {
		t.Fatalf("HabilitarUsuario: %v", err)
	}
	if err := admin.HabilitarUsuario(context.Background(), sub); err != nil {
		t.Fatalf("HabilitarUsuario (idempotente): %v", err)
	}
	if !enabledDe() {
		t.Error("tras habilitar, el usuario sigue enabled=false")
	}
}
