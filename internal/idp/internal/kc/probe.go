package kc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NewProbe crea una función de prueba de alcanzabilidad del servidor Keycloak.
// La función valida la configuración y retorna un error si las URLs no son válidas.
// El probe hace GET a .well-known/openid-configuration derivado de TokenURL.
func NewProbe(cfg Config) (func(ctx context.Context) error, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Derivar la URL del discovery endpoint desde TokenURL
	// p. ej. si TokenURL es https://keycloak.example.com/realms/{realm}/protocol/openid-connect/token
	// entonces el discovery es https://keycloak.example.com/realms/{realm}/.well-known/openid-configuration
	discoveryURL := deriveDiscoveryURL(cfg.TokenURL)

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return func(ctx context.Context) error {
		return probeKeycloak(ctx, discoveryURL, httpClient)
	}, nil
}

// deriveDiscoveryURL extrae la URL base del realm desde TokenURL y apunta
// a .well-known/openid-configuration.
func deriveDiscoveryURL(tokenURL string) string {
	// Encontrar el índice de "/protocol/openid-connect/token"
	const protocolPath = "/protocol/openid-connect/token"
	idx := strings.Index(tokenURL, protocolPath)
	if idx > 0 {
		baseURL := tokenURL[:idx]
		return baseURL + "/.well-known/openid-configuration"
	}
	// Si no encuentra el patrón, usar la URL tal cual (fallará si no es válida)
	return tokenURL + "/.well-known/openid-configuration"
}

// probeKeycloak verifica si Keycloak es alcanzable haciendo GET al discovery endpoint.
// Devuelve un error saneado si no es alcanzable o responde con error.
func probeKeycloak(ctx context.Context, discoveryURL string, httpClient *http.Client) error {
	// Timeout de 5 segundos
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	if err != nil {
		return fmt.Errorf("idp:keycloak no alcanzable")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("idp:keycloak no alcanzable")
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("idp:keycloak respondió %d", resp.StatusCode)
	}

	return nil
}
