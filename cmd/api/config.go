package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/keycloak"
)

// seleccionIDP es el proveedor de identidad elegido por el entorno.
type seleccionIDP string

const (
	idpMemory   seleccionIDP = "memory"
	idpKeycloak seleccionIDP = "keycloak"
)

// configIDP es el resultado de resolver el proveedor de identidad desde el
// entorno: la selección y, cuando es Keycloak, la configuración ya armada.
type configIDP struct {
	Seleccion seleccionIDP
	Keycloak  keycloak.Config
}

// cargarConfig resuelve el proveedor de identidad desde el entorno. Recibe
// `lookup` (p. ej. os.LookupEnv) y `readFile` (p. ej. os.ReadFile) inyectables
// para poder probarlo sin tocar el entorno real ni el disco.
//
// Reglas (R9.2, R9.3, R9.6):
//   - IDENTIDAD_IDP ausente, vacío o "memory" → doble en memoria.
//   - "keycloak" → arma keycloak.Config desde IDENTIDAD_KEYCLOAK_*.
//   - cualquier otro valor → error que nombra la variable y los valores
//     aceptados, sin repetir el valor recibido.
//
// Ante configuración de Keycloak incompleta, junta TODAS las variables
// obligatorias faltantes (y los conflictos de secretos) en un solo error, sin
// incluir ningún valor (R9.3, R9.4).
func cargarConfig(lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (configIDP, error) {
	sel, _ := lookup("IDENTIDAD_IDP")
	switch seleccionIDP(sel) {
	case "", idpMemory:
		return configIDP{Seleccion: idpMemory}, nil
	case idpKeycloak:
		cfg, err := cargarConfigKeycloak(lookup, readFile)
		if err != nil {
			return configIDP{}, err
		}
		return configIDP{Seleccion: idpKeycloak, Keycloak: cfg}, nil
	default:
		return configIDP{}, fmt.Errorf("IDENTIDAD_IDP: valor no aceptado; valores aceptados: %s, %s", idpMemory, idpKeycloak)
	}
}

// cargarConfigKeycloak arma keycloak.Config desde las variables
// IDENTIDAD_KEYCLOAK_*. Acumula todos los problemas y devuelve un solo error.
func cargarConfigKeycloak(lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (keycloak.Config, error) {
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
		return keycloak.Config{}, fmt.Errorf("configuración de Keycloak inválida: %s", strings.Join(partes, "; "))
	}

	return keycloak.Config{
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
