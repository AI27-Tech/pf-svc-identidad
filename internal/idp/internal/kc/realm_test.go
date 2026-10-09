package kc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realmArtifact es la vista parcial de deploy/keycloak/realm-ai27.json que
// necesitan las pruebas del artefacto (tareas 14.3 y 14.4). No pretende cubrir
// todo el esquema de Keycloak, sólo lo que estos casos verifican.
type realmArtifact struct {
	Realm                     string          `json:"realm"`
	DefaultSignatureAlgorithm string          `json:"defaultSignatureAlgorithm"`
	RevokeRefreshToken        bool            `json:"revokeRefreshToken"`
	RefreshTokenMaxReuse      int             `json:"refreshTokenMaxReuse"`
	Groups                    []realmGroup    `json:"groups"`
	ClientScopes              []realmScope    `json:"clientScopes"`
	Clients                   []realmClient   `json:"clients"`
	AuthenticationFlows       []realmFlow     `json:"authenticationFlows"`
	Components                realmComponents `json:"components"`
}

type realmGroup struct {
	Name       string              `json:"name"`
	Attributes map[string][]string `json:"attributes"`
}

type realmScope struct {
	Name            string        `json:"name"`
	ProtocolMappers []realmMapper `json:"protocolMappers"`
}

type realmMapper struct {
	Name           string            `json:"name"`
	ProtocolMapper string            `json:"protocolMapper"`
	Config         map[string]string `json:"config"`
}

type realmClient struct {
	ClientID                     string            `json:"clientId"`
	Secret                       string            `json:"secret"`
	ServiceAccountsEnabled       bool              `json:"serviceAccountsEnabled"`
	DirectAccessGrantsEnabled    bool              `json:"directAccessGrantsEnabled"`
	StandardFlowEnabled          bool              `json:"standardFlowEnabled"`
	AuthenticationFlowBindingOvr map[string]string `json:"authenticationFlowBindingOverrides"`
}

type realmFlow struct {
	Alias                    string          `json:"alias"`
	ProviderID               string          `json:"providerId"`
	TopLevel                 bool            `json:"topLevel"`
	AuthenticationExecutions []realmFlowExec `json:"authenticationExecutions"`
}

type realmFlowExec struct {
	Authenticator    string `json:"authenticator"`
	Requirement      string `json:"requirement"`
	AutheticatorFlow bool   `json:"autheticatorFlow"`
}

type realmComponents struct {
	UserProfile []realmUserProfileComponent `json:"org.keycloak.userprofile.UserProfileProvider"`
}

type realmUserProfileComponent struct {
	ProviderID string              `json:"providerId"`
	Config     map[string][]string `json:"config"`
}

// userProfileConfig es la forma del JSON embebido en kc.user.profile.config.
type userProfileConfig struct {
	Attributes []userProfileAttribute `json:"attributes"`
}

type userProfileAttribute struct {
	Name        string `json:"name"`
	Permissions struct {
		View []string `json:"view"`
		Edit []string `json:"edit"`
	} `json:"permissions"`
}

// cargarRealm localiza y parsea el artefacto del realm versionado. Falla la
// prueba si no se puede leer o parsear.
func cargarRealm(t *testing.T) realmArtifact {
	t.Helper()
	ruta := filepath.Join(repoRoot(t), "deploy", "keycloak", "realm-ai27.json")
	datos, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("no se pudo leer el artefacto del realm %q: %v", ruta, err)
	}
	var r realmArtifact
	if err := json.Unmarshal(datos, &r); err != nil {
		t.Fatalf("el artefacto del realm no es JSON válido: %v", err)
	}
	return r
}

// userProfile parsea el JSON embebido de la configuración de user profile.
func (r realmArtifact) userProfile(t *testing.T) userProfileConfig {
	t.Helper()
	if len(r.Components.UserProfile) == 0 {
		t.Fatal("el realm no declara componente de user profile")
	}
	cfg := r.Components.UserProfile[0].Config["kc.user.profile.config"]
	if len(cfg) == 0 {
		t.Fatal("el componente de user profile no trae kc.user.profile.config")
	}
	var upc userProfileConfig
	if err := json.Unmarshal([]byte(cfg[0]), &upc); err != nil {
		t.Fatalf("kc.user.profile.config no es JSON válido: %v", err)
	}
	return upc
}

// repoRoot sube desde el directorio de trabajo hasta encontrar go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		padre := filepath.Dir(dir)
		if padre == dir {
			t.Fatal("no se encontró go.mod subiendo desde el directorio de trabajo")
		}
		dir = padre
	}
}

// TestRealmFirmaYRotacion verifica RS256 y la rotación del refresh token (R6.8).
func TestRealmFirmaYRotacion(t *testing.T) {
	r := cargarRealm(t)
	if r.Realm != "ai27" {
		t.Errorf("realm = %q; se esperaba ai27 (A-1: un solo realm)", r.Realm)
	}
	if r.DefaultSignatureAlgorithm != "RS256" {
		t.Errorf("defaultSignatureAlgorithm = %q; se esperaba RS256 (H3)", r.DefaultSignatureAlgorithm)
	}
	if !r.RevokeRefreshToken {
		t.Error("revokeRefreshToken debe ser true (rotación del refresh token, R6.8)")
	}
	if r.RefreshTokenMaxReuse != 0 {
		t.Errorf("refreshTokenMaxReuse = %d; se esperaba 0 (R6.8)", r.RefreshTokenMaxReuse)
	}
}

// TestRealmFlujoGrantDirecto verifica el flujo de grant directo: usuario,
// contraseña y OTP en REQUIRED y sin subflujos condicionales (R4.1, DEC-071).
func TestRealmFlujoGrantDirecto(t *testing.T) {
	r := cargarRealm(t)

	var flujo *realmFlow
	for i := range r.AuthenticationFlows {
		if r.AuthenticationFlows[i].Alias == "ai27-direct-grant" {
			flujo = &r.AuthenticationFlows[i]
			break
		}
	}
	if flujo == nil {
		t.Fatal("no existe el flujo ai27-direct-grant")
	}
	if !flujo.TopLevel {
		t.Error("el flujo ai27-direct-grant debe ser topLevel")
	}

	esperados := map[string]bool{
		"direct-grant-validate-username": false,
		"direct-grant-validate-password": false,
		"direct-grant-validate-otp":      false,
	}
	for _, ex := range flujo.AuthenticationExecutions {
		if ex.AutheticatorFlow {
			t.Errorf("el flujo de grant directo no debe tener subflujos (ejecutor %q es subflujo)", ex.Authenticator)
		}
		if _, ok := esperados[ex.Authenticator]; ok {
			esperados[ex.Authenticator] = true
			if ex.Requirement != "REQUIRED" {
				t.Errorf("ejecutor %q tiene requirement %q; se esperaba REQUIRED (R4.1)", ex.Authenticator, ex.Requirement)
			}
		}
	}
	for auth, visto := range esperados {
		if !visto {
			t.Errorf("falta el ejecutor REQUIRED %q en el flujo de grant directo", auth)
		}
	}

	// El cliente de login debe ligar su grant directo a este flujo.
	var login *realmClient
	for i := range r.Clients {
		if r.Clients[i].ClientID == "ai27-login" {
			login = &r.Clients[i]
			break
		}
	}
	if login == nil {
		t.Fatal("no existe el cliente ai27-login")
	}
	if login.AuthenticationFlowBindingOvr["direct_grant"] != "ai27-direct-grant" {
		t.Errorf("ai27-login no liga su grant directo al flujo ai27-direct-grant: %v", login.AuthenticationFlowBindingOvr)
	}
	if !login.DirectAccessGrantsEnabled {
		t.Error("ai27-login debe tener directAccessGrantsEnabled (ROPC, A-3)")
	}
	if login.StandardFlowEnabled {
		t.Error("ai27-login no debe tener el flujo estándar habilitado en V1")
	}
	if login.ServiceAccountsEnabled {
		t.Error("ai27-login no debe tener cuenta de servicio (R5.9, R7.7)")
	}
}

// TestRealmTenantIDSoloAdmin verifica que tenant_id sólo lo pueda ver y editar
// admin (R5.1, R5.2).
func TestRealmTenantIDSoloAdmin(t *testing.T) {
	r := cargarRealm(t)
	upc := r.userProfile(t)

	var attr *userProfileAttribute
	for i := range upc.Attributes {
		if upc.Attributes[i].Name == "tenant_id" {
			attr = &upc.Attributes[i]
			break
		}
	}
	if attr == nil {
		t.Fatal("el user profile no declara el atributo tenant_id")
	}
	if !soloAdmin(attr.Permissions.Edit) {
		t.Errorf("tenant_id edit = %v; se esperaba sólo [admin] (R5.1)", attr.Permissions.Edit)
	}
	if !soloAdmin(attr.Permissions.View) {
		t.Errorf("tenant_id view = %v; se esperaba sólo [admin] (R5.2)", attr.Permissions.View)
	}
}

func soloAdmin(perms []string) bool {
	return len(perms) == 1 && perms[0] == "admin"
}

// TestRealmServicioMinimo verifica que el cliente de servicio exista como sólo
// cuenta de servicio, sin grant directo ni flujo estándar (R7.4, DEC-073).
func TestRealmServicioMinimo(t *testing.T) {
	r := cargarRealm(t)
	var svc *realmClient
	for i := range r.Clients {
		if r.Clients[i].ClientID == "ai27-identidad-svc" {
			svc = &r.Clients[i]
			break
		}
	}
	if svc == nil {
		t.Fatal("no existe el cliente ai27-identidad-svc")
	}
	if !svc.ServiceAccountsEnabled {
		t.Error("ai27-identidad-svc debe tener cuenta de servicio")
	}
	if svc.DirectAccessGrantsEnabled {
		t.Error("ai27-identidad-svc no debe tener grant directo")
	}
	if svc.StandardFlowEnabled {
		t.Error("ai27-identidad-svc no debe tener flujo estándar")
	}
}

// TestArtefactoVersionadoSinUsuarios verifica que el archivo versionado del
// realm no contenga usuarios (R5.7): los usuarios de prueba viven aparte en
// testdata.
func TestArtefactoVersionadoSinUsuarios(t *testing.T) {
	ruta := filepath.Join(repoRoot(t), "deploy", "keycloak", "realm-ai27.json")
	datos, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("no se pudo leer el artefacto del realm: %v", err)
	}
	var crudo map[string]json.RawMessage
	if err := json.Unmarshal(datos, &crudo); err != nil {
		t.Fatalf("el artefacto del realm no es JSON válido: %v", err)
	}
	if usuarios, ok := crudo["users"]; ok {
		var lista []json.RawMessage
		if err := json.Unmarshal(usuarios, &lista); err == nil && len(lista) > 0 {
			t.Errorf("el artefacto versionado del realm contiene %d usuarios; deben vivir en testdata (R5.7)", len(lista))
		}
	}
}

// TestArtefactoSinSecretosLiterales verifica que todo secret del artefacto sea
// un marcador ${...} y nunca un valor literal (R5.10).
func TestArtefactoSinSecretosLiterales(t *testing.T) {
	r := cargarRealm(t)
	for _, c := range r.Clients {
		if c.Secret == "" {
			continue
		}
		if !esMarcador(c.Secret) {
			t.Errorf("cliente %q tiene un secret que no es marcador ${...}: %q", c.ClientID, c.Secret)
		}
	}
}

func esMarcador(v string) bool {
	return strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}")
}
