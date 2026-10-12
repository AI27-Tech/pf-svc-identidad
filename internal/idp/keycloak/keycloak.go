// Package keycloak es la fachada pública del adaptador Keycloak del puerto idp.IdentityProvider.
// Expone `New`, `Probe` y reasigna `Config` desde el paquete interno kc.
// No contiene lógica; sólo actúa como punto de entrada público sin revelar
// detalles de implementación.
package keycloak

import (
	"context"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/internal/kc"
)

// Config reasigna el tipo Config desde el paquete interno.
type Config = kc.Config

// New crea un adaptador Keycloak. Es la única forma pública de instanciar el IdP.
func New(cfg Config) (idp.IdentityProvider, error) {
	return kc.New(cfg)
}

// Probe retorna una función de prueba de alcanzabilidad del servidor Keycloak.
func Probe(cfg Config) (func(ctx context.Context) error, error) {
	return kc.NewProbe(cfg)
}
