// Package idpconfig arma la configuración del proveedor de identidad desde el
// entorno (variables IDENTIDAD_IDP / IDENTIDAD_KEYCLOAK_*), de forma reutilizable
// por CUALQUIER raíz de composición: el cmd/api standalone de este módulo y el
// deployable `core`.
//
// Por qué es un paquete público y no vive en cmd/api: la lógica de leer las
// variables de Keycloak, resolver los secretos `_FILE` vs directos y juntar
// todos los faltantes en un solo error es delicada y no debe duplicarse. Antes
// vivía como `package main` del cmd standalone, inalcanzable desde core; aquí se
// expone una sola vez para que core no reescriba los nombres de variable ni la
// lógica de secretos.
//
// NO lee entorno por su cuenta: recibe `lookup` y `readFile` inyectables (p. ej.
// os.LookupEnv y os.ReadFile), para poder probarlo sin tocar el entorno real ni
// el disco.
package idpconfig

import (
	"fmt"
	"strings"
	"time"

	identidad "github.com/AI27-Tech/pf-svc-identidad"
)

// Seleccion es el proveedor de identidad elegido por el entorno.
type Seleccion string

const (
	// Memory selecciona el doble en memoria (desarrollo).
	Memory Seleccion = "memory"
	// Keycloak selecciona el adaptador del IdP real.
	Keycloak Seleccion = "keycloak"
)

// Config es el resultado de resolver el proveedor desde el entorno: la selección
// y, cuando es Keycloak, la configuración ya armada del adaptador.
type Config struct {
	Seleccion Seleccion
	Keycloak  identidad.IDPKeycloakConfig
}

// Cargar resuelve el proveedor de identidad desde el entorno. Reglas (R9.2,
// R9.3, R9.6):
//   - IDENTIDAD_IDP ausente, vacío o "memory" → doble en memoria.
//   - "keycloak" → arma la config desde IDENTIDAD_KEYCLOAK_*.
//   - cualquier otro valor → error que nombra la variable y los valores
//     aceptados, sin repetir el valor recibido.
//
// Ante configuración de Keycloak incompleta, junta TODAS las variables
// obligatorias faltantes (y los conflictos de secretos) en un solo error, sin
// incluir ningún valor (R9.3, R9.4).
func Cargar(lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (Config, error) {
	sel, _ := lookup("IDENTIDAD_IDP")
	switch Seleccion(sel) {
	case "", Memory:
		return Config{Seleccion: Memory}, nil
	case Keycloak:
		cfg, err := cargarKeycloak(lookup, readFile)
		if err != nil {
			return Config{}, err
		}
		return Config{Seleccion: Keycloak, Keycloak: cfg}, nil
	default:
		return Config{}, fmt.Errorf("IDENTIDAD_IDP: valor no aceptado; valores aceptados: %s, %s", Memory, Keycloak)
	}
}

// cargarKeycloak arma la Config del adaptador desde las variables
// IDENTIDAD_KEYCLOAK_*. Acumula todos los problemas y devuelve un solo error.
func cargarKeycloak(lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (identidad.IDPKeycloakConfig, error) {
	var faltantes []string
	var conflictos []string

	// Variable obligatoria simple: la registra como faltante si está vacía.
	req := func(nombre string) string {
		v, _ := lookup(nombre)
		if v == "" {
			faltantes = append(faltantes, nombre)
		}
		return v
	}

	tokenURL := req("IDENTIDAD_KEYCLOAK_TOKEN_URL")
	adminSessionsURL := req("IDENTIDAD_KEYCLOAK_ADMIN_SESSIONS_URL")
	adminUsersURL := req("IDENTIDAD_KEYCLOAK_ADMIN_USERS_URL")
	adminGroupsURL := req("IDENTIDAD_KEYCLOAK_ADMIN_GROUPS_URL")
	clientID := req("IDENTIDAD_KEYCLOAK_CLIENT_ID")
	serviceClientID := req("IDENTIDAD_KEYCLOAK_SERVICE_CLIENT_ID")
	adminClientID := req("IDENTIDAD_KEYCLOAK_ADMIN_CLIENT_ID")

	// REFRESH_URL es opcional: por omisión toma TOKEN_URL (propuesta A-15).
	refreshURL, _ := lookup("IDENTIDAD_KEYCLOAK_REFRESH_URL")
	if refreshURL == "" {
		refreshURL = tokenURL
	}

	// Secretos: X_FILE (archivo montado) o X directo; ambos definidos = error.
	clientSecret := leerSecreto(lookup, readFile, "IDENTIDAD_KEYCLOAK_CLIENT_SECRET", &faltantes, &conflictos)
	serviceClientSecret := leerSecreto(lookup, readFile, "IDENTIDAD_KEYCLOAK_SERVICE_CLIENT_SECRET", &faltantes, &conflictos)
	adminClientSecret := leerSecreto(lookup, readFile, "IDENTIDAD_KEYCLOAK_ADMIN_CLIENT_SECRET", &faltantes, &conflictos)

	// TIMEOUT: obligatorio, sin valor por omisión, con time.ParseDuration.
	var timeout time.Duration
	if v, _ := lookup("IDENTIDAD_KEYCLOAK_TIMEOUT"); v == "" {
		faltantes = append(faltantes, "IDENTIDAD_KEYCLOAK_TIMEOUT")
	} else if d, err := time.ParseDuration(v); err != nil {
		conflictos = append(conflictos, "IDENTIDAD_KEYCLOAK_TIMEOUT no es una duración válida")
	} else {
		timeout = d
	}

	if len(faltantes) > 0 || len(conflictos) > 0 {
		var partes []string
		if len(faltantes) > 0 {
			partes = append(partes, "faltan "+strings.Join(faltantes, ", "))
		}
		if len(conflictos) > 0 {
			partes = append(partes, strings.Join(conflictos, "; "))
		}
		return identidad.IDPKeycloakConfig{}, fmt.Errorf("configuración de Keycloak inválida: %s", strings.Join(partes, "; "))
	}

	return identidad.IDPKeycloakConfig{
		TokenURL:            tokenURL,
		RefreshURL:          refreshURL,
		AdminSessionsURL:    adminSessionsURL,
		AdminUsersURL:       adminUsersURL,
		AdminGroupsURL:      adminGroupsURL,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		ServiceClientID:     serviceClientID,
		ServiceClientSecret: serviceClientSecret,
		AdminClientID:       adminClientID,
		AdminClientSecret:   adminClientSecret,
		Timeout:             timeout,
	}, nil
}

// leerSecreto resuelve un secreto desde el archivo montado (nombre+"_FILE") o
// desde la variable directa. Definir ambas es un conflicto; no definir ninguna
// lo registra como faltante. Nunca incluye el valor del secreto en los errores.
func leerSecreto(lookup func(string) (string, bool), readFile func(string) ([]byte, error), nombre string, faltantes, conflictos *[]string) string {
	nombreFile := nombre + "_FILE"
	ruta, hayFile := lookup(nombreFile)
	directo, hayDirecto := lookup(nombre)
	hayFile = hayFile && ruta != ""
	hayDirecto = hayDirecto && directo != ""

	switch {
	case hayFile && hayDirecto:
		*conflictos = append(*conflictos, fmt.Sprintf("%s y %s están ambos definidos", nombre, nombreFile))
		return ""
	case hayFile:
		datos, err := readFile(ruta)
		if err != nil {
			*conflictos = append(*conflictos, fmt.Sprintf("%s no se pudo leer", nombreFile))
			return ""
		}
		// Recortar espacios finales (los Secret de k3s suelen traer un salto).
		return strings.TrimRight(string(datos), " \t\r\n")
	case hayDirecto:
		return directo
	default:
		*faltantes = append(*faltantes, nombre)
		return ""
	}
}
