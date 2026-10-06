// Package idp aísla por completo al proveedor de identidad externo.
//
// Es §A.5 R2 (identidad.md (d), (l)) aplicado a una dependencia externa: NADA
// fuera de este paquete conoce al proveedor. El IdP on-premise es dependencia
// EXTERNA, no una arista de AI27: no aparece en reglas.yaml (identidad.md (f)).
//
// En V1 el proveedor es un IdP on-premise con OIDC (DEC-062; propuesta ADR 32:
// Keycloak, o el IdP corporativo). El adaptador concreto vive en el subpaquete
// `internal/` y queda EN ESPERA hasta que ADR 32 elija componente (DEC-062,
// Impacto): hoy sólo hay un doble en memoria (subpaquete `memory`) y la suite
// de contrato (`idptest`) que el adaptador real deberá pasar.
package idp

import (
	"context"
	"time"
)

// Credentials es la credencial de acceso que el sujeto presenta al emitir una
// sesión. AI27 no custodia la contraseña: la verifica el IdP (identidad.md (a),
// E0-T3). No se registra ni se refleja en logs.
type Credentials struct {
	// Usuario es el identificador de acceso del sujeto según el IdP.
	Usuario string
	// Contrasena es la credencial; se pasa al IdP y se descarta. Nunca se persiste.
	Contrasena string
}

// Token es el resultado de una emisión o refresco en el IdP. Son los tokens
// firmados por el proveedor; AI27 no los persiste (identidad.md (a)). El
// `access_token` lo verifica cada servicio localmente contra el JWKS con
// pf-pkg-platform-go/auth; aquí sólo transita.
type Token struct {
	AccessToken  string
	RefreshToken string
	// ExpiraEn es la expiración natural del access token (tiempo de dominio, UTC).
	ExpiraEn time.Time
}

// IdentityProvider es el ÚNICO punto que conoce al proveedor de identidad
// (IdP on-premise con OIDC; propuesta ADR 32: Keycloak). La implementación
// concreta espera a que ADR 32 elija componente (DEC-062, Impacto).
//
// El puerto es DELIBERADAMENTE pequeño: `identidad` NO es dueño de usuarios,
// credenciales, política de contraseña, MFA ni firma —eso es del IdP
// (identidad.md (a))—. Por eso aquí no hay alta de usuarios, cambio de
// contraseña ni enrolamiento de segundo factor; el OTP propio no existe
// (DEC-031).
type IdentityProvider interface {
	// Issue verifica la credencial contra el IdP y devuelve el token. La
	// composición de claims (tenant, rol, Audiencia) corre en el IdP como
	// mapper o extensión (ADR 32 §4), no aquí; este puerto sólo transporta el
	// resultado.
	Issue(ctx context.Context, cred Credentials) (Token, error)
	// Refresh renueva el token. La composición de claims se REEVALÚA en el
	// refresco (ADR 16 cerrado por DEC-031): el token recompone tenant, rol y
	// Audiencia desde el modelo vigente.
	Refresh(ctx context.Context, refreshToken string) (Token, error)
	// Revoke revoca la sesión en el IdP. Es el PASO 2 de la revocación en dos
	// pasos (identidad.md (h)): corre DESPUÉS de comprometer la revocación
	// local y su evento. Es idempotente (P13).
	Revoke(ctx context.Context, sesionID string) error

	// TODO(contrato): administración de usuarios (alta, baja, cambio de grupo)
	// queda fuera de este puerto mientras exista la brecha contractual de
	// /api/v1/sujetos y /api/v1/roles (ver internal/gobierno/port.go). No se
	// añade aquí hasta que pf-pkg-contracts publique esa superficie.
}
