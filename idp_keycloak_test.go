package identidad

import (
	"testing"
	"time"
)

// TestIDPAdminDe fija la puerta pública que usa core para cablear Deps.IDPAdmin:
// el adaptador de NewIDPKeycloak administra usuarios (no es nil) y un proveedor
// nil no produce una administración.
func TestIDPAdminDe(t *testing.T) {
	p, err := NewIDPKeycloak(IDPKeycloakConfig{
		TokenURL:            "http://kc.local/realms/ai27/protocol/openid-connect/token",
		AdminSessionsURL:    "http://kc.local/admin/realms/ai27/sessions",
		AdminUsersURL:       "http://kc.local/admin/realms/ai27/users",
		AdminGroupsURL:      "http://kc.local/admin/realms/ai27/groups",
		ClientID:            "ai27-login",
		ClientSecret:        "x",
		ServiceClientID:     "ai27-identidad-svc",
		ServiceClientSecret: "x",
		AdminClientID:       "ai27-identidad-svc",
		AdminClientSecret:   "x",
		Timeout:             time.Second,
	})
	if err != nil {
		t.Fatalf("NewIDPKeycloak: %v", err)
	}
	if IDPAdminDe(p) == nil {
		t.Error("el adaptador de Keycloak debe exponer su administración (IdpAdmin)")
	}
	if IDPAdminDe(nil) != nil {
		t.Error("un proveedor nil no debe producir administración")
	}
}
