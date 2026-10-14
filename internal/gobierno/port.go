// Package gobierno es la superficie de GOBIERNO de identidad: alta y baja de
// sujetos, asignación y revocación de rol (siempre como reemplazo), y la traza
// inmutable de esos cambios (`cambio_identidad`). También resuelve la
// correlación `IdpSubject → SujetoID` que el login de E4-T3 necesita (DEC-072).
//
// Dos principios gobiernan el paquete:
//
//   - Local manda. La fuente de verdad es Postgres: cada cambio compromete
//     estado + traza + outbox en una sola transacción, y el IdP se sincroniza
//     después, de forma reintentable e idempotente (DEC-085, C1). Un fallo del
//     IdP no revierte el commit local.
//   - El `sub` del IdP es opaco (DEC-072). El `SujetoID` de AI27 se resuelve en
//     servidor por `idp_subject`; nunca se asume `sub == SujetoID`.
//
// La capa HTTP (`/api/v1/sujetos`, `/api/v1/roles`) NO vive aquí todavía: esas
// operaciones no existen en el contrato (brecha G-2). Este paquete define el
// servicio y sus puertos; los handlers se cablean cuando el contrato las
// publique, igual que `internal/http/handlers.go` para sesiones.
package gobierno

import (
	"context"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// Gobierno es la superficie de ESCRITURA del gobierno de identidad. El actor y
// la autorización no son parámetros del dominio: el handler HTTP los valida
// contra los claims verificados y pasa el `ActorID` ya resuelto en cada entrada.
type Gobierno interface {
	// AltaSujeto crea el usuario en el IdP y el `sujeto` local con su rol
	// inicial (Otorgamiento: IdP primero, G-3). Publica `sujeto.creado` y
	// `rol.asignado` en la misma transacción local.
	AltaSujeto(ctx context.Context, in AltaSujeto) (autorizacion.Sujeto, error)

	// DesactivarSujeto marca el `sujeto` inactivo (Retiro de acceso: local
	// primero, G-3), revoca su sesión viva (≤30 s, G-6) y publica
	// `sujeto.desactivado`.
	DesactivarSujeto(ctx context.Context, in CambioEstado) error

	// ReactivarSujeto marca el `sujeto` activo de nuevo (Otorgamiento) y publica
	// `sujeto.reactivado` (C5). Es un hecho propio, no reusa `sujeto.creado`.
	ReactivarSujeto(ctx context.Context, in CambioEstado) error

	// ReemplazarRol revoca el rol activo y asigna el nuevo en una sola
	// transacción local (C1): dos filas de traza, dos hechos. Es la ÚNICA vía
	// para cambiar el rol de un sujeto que ya tiene uno; no existe revocar
	// suelto (C2).
	ReemplazarRol(ctx context.Context, in ReemplazoRol) error

	// AsignarRolInicial asigna un rol a un sujeto SIN rol activo (parte del alta,
	// o un sujeto que nunca tuvo rol). Si el sujeto ya tiene un rol activo,
	// devuelve ErrSujetoYaTieneRol (usar ReemplazarRol).
	AsignarRolInicial(ctx context.Context, in AsignacionInicial) error
}

// Consulta es la superficie de LECTURA del gobierno (Requisito 10). Separada de
// la escritura; el handler restringe el acceso al rol administrador (G-10).
type Consulta interface {
	// SujetoPorID devuelve el detalle de un sujeto y su rol activo.
	SujetoPorID(ctx context.Context, sujetoID string) (SujetoDetalle, error)

	// HistorialDe devuelve los cambios administrativos de un sujeto, ordenados
	// por marca de tiempo, paginados por cursor (apoyado en el índice 0004).
	HistorialDe(ctx context.Context, sujetoID, cursor string) (PaginaHistorial, error)
}

// AltaSujeto es la entrada del alta. Los campos funcionales salen de E4-HU-018;
// la forma de wire la fija el contrato (G-2), así que esta forma de dominio es
// provisional hasta que el contrato publique la operación.
type AltaSujeto struct {
	// Nombre del usuario (E4-HU-018).
	Nombre string
	// IdentificadorUsuario es el identificador de acceso (correo o usuario).
	IdentificadorUsuario string
	// Rol inicial. Debe ser uno de los ocho de DEC-053.
	Rol autorizacion.Rol
	// TenantID del sujeto. Para `cliente`, lo determina su organización
	// (E4-HU-018); para los roles internos, es el Tenant_AI27 (G-9).
	TenantID string
	// ActorID es el `sujeto_id` del administrador que ejecuta el alta.
	ActorID string
	// ActorEsAdmin indica si el invocante tiene rol administrador. Lo resuelve
	// el handler desde los claims verificados.
	ActorEsAdmin bool
}

// CambioEstado es la entrada de desactivar o reactivar un sujeto. ActorEsAdmin
// y ActorID los resuelve el handler desde los claims verificados.
type CambioEstado struct {
	SujetoID     string
	ActorID      string
	ActorEsAdmin bool
}

// ReemplazoRol es la entrada del reemplazo: el rol nuevo sustituye al activo.
type ReemplazoRol struct {
	SujetoID     string
	RolNuevo     autorizacion.Rol
	ActorID      string
	ActorEsAdmin bool
}

// AsignacionInicial es la entrada de la asignación a un sujeto sin rol activo.
type AsignacionInicial struct {
	SujetoID     string
	Rol          autorizacion.Rol
	ActorID      string
	ActorEsAdmin bool
}

// SujetoDetalle es la lectura de un sujeto con su rol activo.
type SujetoDetalle struct {
	Sujeto    autorizacion.Sujeto
	RolActivo autorizacion.Rol
	TieneRol  bool
}

// CambioAdministrativo es una fila de la traza `cambio_identidad`.
type CambioAdministrativo struct {
	Tipo       string // nombre canónico del hecho: sujeto.creado, rol.asignado, …
	SujetoID   string
	TenantID   string
	ActorID    string
	Detalle    map[string]any
	OcurridoEn string // RFC3339; lo fija la base de datos al insertar
}

// PaginaHistorial es una página de la traza de un sujeto, con su cursor de
// continuación (nil cuando no hay más), al estilo de PaginaRevocadas de sesión.
type PaginaHistorial struct {
	Cambios         []CambioAdministrativo
	CursorSiguiente *string
}
