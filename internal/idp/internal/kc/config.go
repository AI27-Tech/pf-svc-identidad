// Package kc implementa el adaptador del IdP Keycloak contra el puerto
// idp.IdentityProvider. Este paquete contiene el único lugar del módulo que
// importa el SDK de Keycloak y conoce detalles del protocolo. Está sujeto a
// la restricción de aislamiento (internal/archtest).
package kc

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// SessionResolver es una función inyectable que traduce un ID de sesión de
// AI27 (sesionID_ai27) a un ID de sesión de Keycloak (kcSessionID). Si es
// nil, se asume la identidad (IDs iguales). Un error devuelto es no recuperable
// y se propaga.
//
// [Pendiente A-4]: la decisión sobre cómo se vinculan las sesiones de AI27 con
// las de Keycloak afecta qué implementación debe proporcionar el SessionResolver.
// Por ahora es nil, lo que significa que los IDs son idénticos.
type SessionResolver func(ai27SessionID string) (kcSessionID string, err error)

// Config agrupa la configuración necesaria para crear un adaptador Keycloak.
// Todos los campos son obligatorios salvo HTTPClient y MeterProvider.
type Config struct {
	// TokenURL es la URL del endpoint de emisión de tokens en Keycloak.
	// Formato típico: https://keycloak.example.com/realms/{realm}/protocol/openid-connect/token
	TokenURL string

	// RefreshURL es la URL del endpoint de refresco de tokens. Si está vacío,
	// se asume que es igual a TokenURL.
	RefreshURL string

	// AdminSessionsURL es la URL base del endpoint de administración de sesiones.
	// Formato típico: https://keycloak.example.com/admin/realms/{realm}/sessions
	AdminSessionsURL string

	// ClientID es el identificador del cliente Keycloak para emisión de tokens.
	ClientID string

	// ClientSecret es el secreto del cliente para emisión de tokens.
	ClientSecret string

	// ServiceClientID es el identificador del cliente de servicio (cuenta de
	// servicio) para operaciones administrativas como revocación de sesiones.
	ServiceClientID string

	// ServiceClientSecret es el secreto del cliente de servicio.
	ServiceClientSecret string

	// Timeout es el timeout para cada llamada HTTP al IdP. Debe ser > 0.
	Timeout time.Duration

	// HTTPClient es el cliente HTTP a usar. Si es nil, se usa http.DefaultClient.
	HTTPClient *http.Client

	// MeterProvider es el proveedor de métricas OTel. Si es nil, se usa
	// otel.GetMeterProvider().
	MeterProvider metric.MeterProvider

	// SessionResolver es la función que traduce IDs de sesión. Si es nil, se
	// asume la identidad.
	SessionResolver SessionResolver
}

// Validate verifica que la configuración sea válida. Retorna un error que
// agrupa todos los problemas encontrados sin revelar los valores de los campos
// (sólo sus nombres).
func (c *Config) Validate() error {
	var problems []string

	// Campos obligatorios
	if c.TokenURL == "" {
		problems = append(problems, "TokenURL")
	}
	if c.AdminSessionsURL == "" {
		problems = append(problems, "AdminSessionsURL")
	}
	if c.ClientID == "" {
		problems = append(problems, "ClientID")
	}
	if c.ClientSecret == "" {
		problems = append(problems, "ClientSecret")
	}
	if c.ServiceClientID == "" {
		problems = append(problems, "ServiceClientID")
	}
	if c.ServiceClientSecret == "" {
		problems = append(problems, "ServiceClientSecret")
	}
	if c.Timeout <= 0 {
		problems = append(problems, "Timeout")
	}

	// Parse de URLs
	if c.TokenURL != "" {
		if err := c.validateURL("TokenURL", c.TokenURL); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if c.RefreshURL != "" {
		if err := c.validateURL("RefreshURL", c.RefreshURL); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if c.AdminSessionsURL != "" {
		if err := c.validateURL("AdminSessionsURL", c.AdminSessionsURL); err != nil {
			problems = append(problems, err.Error())
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("idp/keycloak: configuración incompleta o inválida: %v", problems)
	}

	return nil
}

// validateURL verifica que fieldName sea una URL válida con esquema http o https.
func (c *Config) validateURL(fieldName, urlStr string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("%s no es una URL válida", fieldName)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s tiene esquema %q; se requiere http o https", fieldName, parsed.Scheme)
	}
	return nil
}
