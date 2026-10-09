// Package idp aísla por completo al proveedor de identidad externo.
//
// Es §A.5 R2 (identidad.md (d), (l)) aplicado a una dependencia externa: NADA
// fuera de este paquete conoce al proveedor. El IdP on-premise es dependencia
// EXTERNA, no una arista de AI27: no aparece en reglas.yaml (identidad.md (f)).
//
// En V1 el proveedor es Keycloak (confirmado 2026-10-08, pendiente de asentar
// en decision-log; DEC-062). El adaptador concreto vive bajo `internal/`: la
// implementación en `internal/kc` (único punto que conoce el protocolo OIDC) y
// la fachada pública en `keycloak`, que la raíz de composición importa para
// construirlo. El subpaquete `memory` ofrece un doble para desarrollo y la
// suite de contrato (`idptest`) define el comportamiento que ambas
// implementaciones deben cumplir.
package idp

import (
	"context"
	"errors"
	"time"
)

// Errores centinela del puerto. Estos errores son retornados por las
// implementaciones del IdentityProvider para indicar condiciones bien definidas
// del protocolo OIDC. El consumidor debe usar errors.Is para clasificarlos.
var (
	// ErrCredencialInvalida indica que la credencial presentada (usuario y
	// contraseña) no fue aceptada por el IdP. Puede deberse a usuario no
	// existente, contraseña incorrecta u otros rechazos de validación del IdP
	// que no revelan la causa exacta (por seguridad). El segundo factor, si es
	// requerido, se verifica por separado; este error no cubre rechazo de OTP.
	ErrCredencialInvalida = errors.New("idp: credencial inválida")

	// ErrRefreshInvalido indica que el refresh token no es válido. Puede
	// deberse a un token desconocido, ya usado, revocado, o de una sesión
	// revocada. El IdP no diferencia estas causas en el protocolo; todas caen
	// en invalid_grant con código HTTP 400.
	ErrRefreshInvalido = errors.New("idp: refresh token inválido")

	// ErrProveedorNoDisponible indica que el IdP no responde o no es
	// alcanzable. Incluye timeout, error de red, error HTTP 5xx o cualquier
	// condición que impida intentar la operación (p. ej. configuración
	// incompleta). El consumidor puede reintentar; la falla es transitoria.
	ErrProveedorNoDisponible = errors.New("idp: proveedor no disponible")
)

// Credentials es la credencial de acceso que el sujeto presenta al emitir una
// sesión. AI27 no custodia la contraseña: la verifica el IdP (identidad.md (a),
// E0-T3). No se registra ni se refleja en logs.
type Credentials struct {
	// Usuario es el identificador de acceso del sujeto según el IdP.
	Usuario string
	// Contrasena es la credencial; se pasa al IdP y se descarta. Nunca se persiste.
	Contrasena string
	// SegundoFactor es la credencial de segundo factor (p. ej. OTP). Requerida
	// por algunos flujos de autenticación. Se pasa al IdP y se descarta.
	SegundoFactor string
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
