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
	// IdpSessionID es el id NATIVO de la sesión en el IdP (el `session_state`
	// del grant). NO es el `sesion_id` de AI27: ese lo genera `identidad` y el
	// IdP sólo lo transporta como claim. Este campo expone el id del IdP para
	// que E4-T3 lo persista como `idp_session_id` en la tabla `sesion` y lo use
	// luego al revocar (ADR 26; DEC-083 #2/#3). En inglés por ser el id del
	// proveedor, no un sustantivo de dominio (DEC-072, naming-conventions §4).
	IdpSessionID string
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
	// Issue verifica la credencial contra el IdP y devuelve el token. El
	// `sesionID` es el `sesion_id` propio de AI27, que GENERA el llamador
	// (E4-T3), no el adaptador: el IdP sólo lo transporta hasta el claim
	// `sesion_id` (DEC-083 #1/#3). Va como parámetro aparte, no en Credentials,
	// porque no es algo que el sujeto presente, sino identidad que AI27 produce
	// (es la distinción del doc de Credentials). La composición de claims
	// (tenant, rol, Audiencia) corre en el IdP como mapper o extensión (ADR 32
	// §4), no aquí; este puerto sólo transporta el resultado, incluido el
	// IdpSessionID del Token para que E4-T3 lo correlacione con el `sesion_id`.
	Issue(ctx context.Context, cred Credentials, sesionID string) (Token, error)
	// Refresh renueva el token. La composición de claims se REEVALÚA en el
	// refresco (ADR 16 cerrado por DEC-031): el token recompone tenant, rol y
	// Audiencia desde el modelo vigente. El `sesion_id` NO se vuelve a pasar: ya
	// vive en la sesión del IdP y se re-emite como claim con el mismo valor.
	Refresh(ctx context.Context, refreshToken string) (Token, error)
	// Revoke revoca la sesión en el IdP. Recibe el `idpSessionID` (el id NATIVO
	// de la sesión en el IdP, no el `sesion_id` de AI27): la correlación
	// `sesion_id ↔ idp_session_id` vive en la tabla `sesion` de E4-T3, no aquí
	// (DEC-083 #2/#3). Es el PASO 2 de la revocación en dos pasos (identidad.md
	// (h)): corre DESPUÉS de comprometer la revocación local y su evento. Es
	// idempotente (P13).
	Revoke(ctx context.Context, idpSessionID string) error

	// La administración de usuarios y grupos (alta, baja, cambio de grupo) vive
	// en IdpAdmin, abajo: es la superficie que E4-T6 necesita y que E4-T1
	// difirió a esa historia (A-5). Se mantiene en una interfaz aparte para que
	// el consumidor que solo emite/revoca no dependa de la administración.
}

// IdpAdmin administra usuarios y grupos en el IdP: es la superficie que el
// gobierno de identidad (E4-T6) necesita y que E4-T1 difirió a esta historia
// (A-5 de su spec). Vive junto al puerto del IdP porque es parte del mismo
// aislamiento: ningún paquete fuera de internal/idp conoce el proveedor
// (identidad.md (d), (l)).
//
// Usa una cuenta de servicio SEPARADA de la que revoca sesiones (C4/G-4,
// DEC-073): la de revocación no tiene permiso de administrar usuarios y no se
// amplía. El adaptador real de Keycloak y el doble en memoria implementan esta
// interfaz y ambos cumplen la suite de contrato idptest.
type IdpAdmin interface {
	// CrearUsuario da de alta al usuario con su atributo de tenant (inmutable
	// por el propio usuario, E4-T1) y lo coloca en el grupo del rol inicial.
	// Devuelve el `sub` OPACO que el IdP asigna (DEC-072): ese valor es el
	// idp_subject con el que el `sujeto` local se correlaciona. Es idempotente
	// por el identificador de usuario: dar de alta dos veces no crea dos
	// usuarios.
	CrearUsuario(ctx context.Context, u NuevoUsuario) (idpSubject string, err error)

	// DeshabilitarUsuario deja al usuario sin poder autenticarse (desactivación
	// del sujeto, G-6). Idempotente.
	DeshabilitarUsuario(ctx context.Context, idpSubject string) error

	// HabilitarUsuario revierte la deshabilitación (reactivación del sujeto,
	// C5). Idempotente.
	HabilitarUsuario(ctx context.Context, idpSubject string) error

	// MoverDeGrupo quita al usuario del grupo del rol anterior y lo pone en el
	// del nuevo, EN ESE ORDEN, para no dejarlo nunca en dos grupos de rol a la
	// vez (DEC-084: el mapper de un solo valor tomaría uno sin criterio). Un
	// rolAnterior vacío significa solo agregar (asignación inicial).
	MoverDeGrupo(ctx context.Context, idpSubject, rolAnterior, rolNuevo string) error
}

// NuevoUsuario es la entrada de CrearUsuario: lo mínimo que el IdP necesita para
// dar de alta y dejar listo el token (grupo del rol y atributo de tenant).
type NuevoUsuario struct {
	// Usuario es el identificador de acceso del sujeto (correo o usuario).
	Usuario string
	// Nombre del usuario, para el perfil del IdP.
	Nombre string
	// TenantID es el atributo de tenant, inmutable por el usuario (E4-T1).
	TenantID string
	// Rol es el identificador del grupo de rol inicial (uno de DEC-053).
	Rol string
}
