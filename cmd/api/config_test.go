package main

import (
	"errors"
	"strings"
	"testing"
)

// entornoBase devuelve un lookup con una configuración de Keycloak completa y
// válida, sobre la que cada caso vacía o altera lo que necesita.
func entornoBase() map[string]string {
	return map[string]string{
		"IDENTIDAD_IDP":                            "keycloak",
		"IDENTIDAD_KEYCLOAK_TOKEN_URL":             "https://kc.interno/realms/ai27/protocol/openid-connect/token",
		"IDENTIDAD_KEYCLOAK_ADMIN_SESSIONS_URL":    "https://kc.interno/admin/realms/ai27/sessions",
		"IDENTIDAD_KEYCLOAK_ADMIN_USERS_URL":       "https://kc.interno/admin/realms/ai27/users",
		"IDENTIDAD_KEYCLOAK_ADMIN_GROUPS_URL":      "https://kc.interno/admin/realms/ai27/groups",
		"IDENTIDAD_KEYCLOAK_CLIENT_ID":             "ai27-login",
		"IDENTIDAD_KEYCLOAK_CLIENT_SECRET":         "secreto-login",
		"IDENTIDAD_KEYCLOAK_SERVICE_CLIENT_ID":     "ai27-identidad-svc",
		"IDENTIDAD_KEYCLOAK_SERVICE_CLIENT_SECRET": "secreto-svc",
		"IDENTIDAD_KEYCLOAK_ADMIN_CLIENT_ID":       "ai27-identidad-admin-svc",
		"IDENTIDAD_KEYCLOAK_ADMIN_CLIENT_SECRET":   "secreto-admin-svc",
		"IDENTIDAD_KEYCLOAK_TIMEOUT":               "5s",
	}
}

// lookupDe convierte un mapa en una función lookup estilo os.LookupEnv.
func lookupDe(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// readFileNoUsado falla si se intenta leer un archivo (ningún caso con _FILE).
func readFileNoUsado(string) ([]byte, error) {
	return nil, errors.New("no debería leer archivos")
}

func TestCargarConfigMemoryPorOmision(t *testing.T) {
	casos := map[string]map[string]string{
		"ausente": {},
		"vacio":   {"IDENTIDAD_IDP": ""},
		"memory":  {"IDENTIDAD_IDP": "memory"},
	}
	for nombre, env := range casos {
		t.Run(nombre, func(t *testing.T) {
			cfg, err := cargarConfig(lookupDe(env), readFileNoUsado)
			if err != nil {
				t.Fatalf("esperaba sin error, obtuvo %v", err)
			}
			if cfg.Seleccion != idpMemory {
				t.Errorf("esperaba memory, obtuvo %s", cfg.Seleccion)
			}
		})
	}
}

func TestCargarConfigValorNoAceptado(t *testing.T) {
	_, err := cargarConfig(lookupDe(map[string]string{"IDENTIDAD_IDP": "cognito"}), readFileNoUsado)
	if err == nil {
		t.Fatal("esperaba error con un valor no aceptado")
	}
	msg := err.Error()
	if !strings.Contains(msg, "IDENTIDAD_IDP") {
		t.Errorf("el error no nombra la variable: %q", msg)
	}
	if strings.Contains(msg, "cognito") {
		t.Errorf("el error repitió el valor recibido: %q", msg)
	}
}

func TestCargarConfigKeycloakCompleta(t *testing.T) {
	cfg, err := cargarConfig(lookupDe(entornoBase()), readFileNoUsado)
	if err != nil {
		t.Fatalf("esperaba sin error, obtuvo %v", err)
	}
	if cfg.Seleccion != idpKeycloak {
		t.Fatalf("esperaba keycloak, obtuvo %s", cfg.Seleccion)
	}
	// REFRESH_URL por omisión toma TOKEN_URL.
	if cfg.Keycloak.RefreshURL != cfg.Keycloak.TokenURL {
		t.Errorf("RefreshURL por omisión debía igualar TokenURL: %q vs %q", cfg.Keycloak.RefreshURL, cfg.Keycloak.TokenURL)
	}
}

func TestCargarConfigJuntaFaltantesSinValores(t *testing.T) {
	env := entornoBase()
	delete(env, "IDENTIDAD_KEYCLOAK_CLIENT_ID")
	delete(env, "IDENTIDAD_KEYCLOAK_TIMEOUT")

	_, err := cargarConfig(lookupDe(env), readFileNoUsado)
	if err == nil {
		t.Fatal("esperaba error con variables faltantes")
	}
	msg := err.Error()
	// Nombra ambas faltantes en un solo error.
	if !strings.Contains(msg, "IDENTIDAD_KEYCLOAK_CLIENT_ID") || !strings.Contains(msg, "IDENTIDAD_KEYCLOAK_TIMEOUT") {
		t.Errorf("el error no junta todas las faltantes: %q", msg)
	}
	// No incluye valores de las variables presentes.
	for _, valor := range []string{"ai27-login", "secreto-login", "secreto-svc", "ai27-identidad-svc"} {
		if strings.Contains(msg, valor) {
			t.Errorf("el error filtró un valor: %q", msg)
		}
	}
}

func TestLeerSecretoDesdeArchivo(t *testing.T) {
	env := entornoBase()
	delete(env, "IDENTIDAD_KEYCLOAK_CLIENT_SECRET")
	env["IDENTIDAD_KEYCLOAK_CLIENT_SECRET_FILE"] = "/run/secrets/login"

	readFile := func(ruta string) ([]byte, error) {
		if ruta == "/run/secrets/login" {
			return []byte("secreto-de-archivo\n"), nil
		}
		return nil, errors.New("ruta inesperada")
	}

	cfg, err := cargarConfig(lookupDe(env), readFile)
	if err != nil {
		t.Fatalf("esperaba sin error, obtuvo %v", err)
	}
	// El salto final se recorta.
	if cfg.Keycloak.ClientSecret != "secreto-de-archivo" {
		t.Errorf("esperaba el secreto del archivo recortado, obtuvo %q", cfg.Keycloak.ClientSecret)
	}
}

func TestLeerSecretoAmbosDefinidosEsError(t *testing.T) {
	env := entornoBase()
	env["IDENTIDAD_KEYCLOAK_CLIENT_SECRET_FILE"] = "/run/secrets/login"
	// CLIENT_SECRET directo ya está en entornoBase.

	readFile := func(string) ([]byte, error) { return []byte("x"), nil }
	_, err := cargarConfig(lookupDe(env), readFile)
	if err == nil {
		t.Fatal("esperaba error cuando el secreto está definido por archivo y directo")
	}
	if !strings.Contains(err.Error(), "IDENTIDAD_KEYCLOAK_CLIENT_SECRET") {
		t.Errorf("el error no nombra el secreto en conflicto: %q", err.Error())
	}
}

func TestTimeoutInvalidoEsError(t *testing.T) {
	env := entornoBase()
	env["IDENTIDAD_KEYCLOAK_TIMEOUT"] = "cinco-segundos"
	_, err := cargarConfig(lookupDe(env), readFileNoUsado)
	if err == nil {
		t.Fatal("esperaba error con un TIMEOUT no parseable")
	}
	if !strings.Contains(err.Error(), "IDENTIDAD_KEYCLOAK_TIMEOUT") {
		t.Errorf("el error no nombra TIMEOUT: %q", err.Error())
	}
}
