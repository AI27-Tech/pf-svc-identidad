// idp_keycloak.go es la PUERTA PÚBLICA para que la raíz de composición (core)
// construya el adaptador Keycloak del puerto idp.IdentityProvider e inyecte el
// resultado en Deps.IDP de New.
//
// Por qué existe: el adaptador y su puerto viven bajo internal/idp, inalcanzable
// desde otro módulo por la regla de internal/ de Go. core NO puede importar
// internal/idp/keycloak ni nombrar idp.IdentityProvider. Este archivo, en la
// raíz del módulo, reexporta la superficie MÍNIMA que core necesita —el tipo del
// proveedor, su Config y un constructor— sin revelar el resto de internal/
// (identidad.md (d), (l)): core ve justo lo necesario para cablear el IdP real.
//
// Mantiene el aislamiento del proveedor: este archivo no conoce el protocolo
// OIDC ni el SDK de Keycloak; sólo delega en la fachada interna.
package identidad

import (
	"context"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/keycloak"
)

// IdentityProvider es el puerto que Deps.IDP exige. Se reexporta aquí para que
// la raíz de composición pueda nombrar el tipo que New requiere sin importar
// internal/idp (inalcanzable desde otro módulo). Es un alias: el tipo sigue
// siendo idp.IdentityProvider, de modo que el resultado de NewIDPKeycloak es
// directamente asignable a Deps.IDP.
type IdentityProvider = idp.IdentityProvider

// IDPKeycloakConfig es la configuración del adaptador Keycloak. Se reexporta
// como alias de la Config interna del adaptador para que la raíz la arme sin
// alcanzar internal/. Sus campos son las URLs y credenciales del IdP
// (IDENTIDAD_KEYCLOAK_*); idpconfig.Cargar la arma desde el entorno sin que core
// duplique la lógica de secretos.
type IDPKeycloakConfig = keycloak.Config

// NewIDPKeycloak construye el adaptador Keycloak y lo devuelve como el puerto
// IdentityProvider que Deps.IDP exige. Es la ÚNICA forma pública de instanciar
// el IdP real desde fuera del módulo; la raíz de composición inyecta el
// resultado en identidad.Deps{IDP: ...}. No lee entorno: recibe la config ya
// resuelta (usar idpconfig.Cargar para armarla desde las variables
// IDENTIDAD_KEYCLOAK_*).
func NewIDPKeycloak(cfg IDPKeycloakConfig) (IdentityProvider, error) {
	return keycloak.New(cfg)
}

// ProbeIDPKeycloak devuelve una función de alcanzabilidad del servidor Keycloak,
// pensada para montarla en un check de readiness aparte (/ready/idp): su caída
// no debe bloquear el /ready principal (ADR 26 §5; la caché y el IdP no son la
// fuente de verdad del arranque). Reexporta keycloak.Probe para que la raíz no
// alcance internal/.
func ProbeIDPKeycloak(cfg IDPKeycloakConfig) (func(ctx context.Context) error, error) {
	return keycloak.Probe(cfg)
}
