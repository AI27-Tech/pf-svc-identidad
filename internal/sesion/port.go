// Package sesion es dueño del CICLO DE VIDA DE SESIÓN observable de AI27 (ADR
// 26): emitir, refrescar, revocar, estado durable y snapshot de tombstones. El
// `sesion_id` es propio de AI27 y estable durante todos los refresh.
//
// La revocación es el mecanismo ÚNICO de revocación efectiva en AI27 (addendum
// §3): revocar en el IdP no invalida un token verificado localmente por firma y
// expiración; `sesion.revocada` + la proyección de tombstones de los
// consumidores sí. Por eso la revocación ocurre en DOS PASOS, en un orden
// deliberado (ver RevocarSesion).
//
// SCAFFOLD: el puerto define la superficie; internal/ trae el molde de la
// transaccionalidad (paso local+outbox en una sola tx) sin la lógica real.
package sesion

import (
	"context"
	"time"
)

// Emitida es el resultado de emitir o refrescar: el token del IdP más el
// `sesion_id` propio de AI27. Coincide en forma con el contrato SesionEmitida.
type Emitida struct {
	SesionID     string
	AccessToken  string
	RefreshToken string
	ExpiraEn     time.Time
}

// Estado es el estado DURABLE de una sesión leído desde Postgres (ADR 26 §5).
// Coincide en forma con el contrato EstadoSesion. Es sólo fallback de
// degradación, no ruta normal.
type Estado struct {
	SesionID   string
	Revocada   bool
	RevocadaEn *time.Time
	ExpiraEn   time.Time
}

// Tombstone es un registro de revocación. Coincide en forma con el payload del
// evento `sesion.revocada` y con el contrato Revocacion: payload MÍNIMO
// (sesion_id, revocada_en, expira_en). El motivo, sujeto, tenant y actor quedan
// en la auditoría local (`cambio_identidad`), no aquí (ADR 26 §7).
type Tombstone struct {
	SesionID   string
	RevocadaEn time.Time
	ExpiraEn   time.Time
}

// PaginaRevocadas es el snapshot de tombstones no expirados más un cursor
// consistente para catch-up sin ventana (ADR 26 §4).
type PaginaRevocadas struct {
	Items           []Tombstone
	CursorSiguiente *string
}

// Servicio es la superficie de casos de uso de sesión. La implementa internal/.
type Servicio interface {
	// EmitirSesion emite una sesión (login): verifica la credencial en el IdP,
	// compone los claims (DEC-031) y registra la sesión. El `sesion_id` es nuevo.
	EmitirSesion(ctx context.Context, usuario, contrasena string) (Emitida, error)

	// RefrescarSesion renueva una sesión conservando su `sesion_id` (ADR 26 §1).
	// Es el punto donde los claims se REEVALÚAN desde el modelo vigente (DEC-031).
	RefrescarSesion(ctx context.Context, refreshToken string) (Emitida, error)

	// RevocarSesion revoca una sesión en DOS PASOS, en este orden (identidad.md (h)):
	//   1. Comprometer la revocación local Y su `sesion.revocada` en `outbox`,
	//      en UNA transacción de Postgres. Si falla, no ocurrió; el estado queda
	//      consistente y el cliente reintenta.
	//   2. Revocar en el IdP on-premise. Si falla, la revocación YA es efectiva
	//      en AI27 (paso 1); se reintenta de forma idempotente.
	// Invertir el orden abriría la ventana en que el IdP da la sesión por muerta
	// y AI27 por viva. Ambos pasos son idempotentes (P13).
	RevocarSesion(ctx context.Context, sesionID string) error

	// ObtenerSesion devuelve el estado durable de una sesión desde Postgres.
	// Sólo fallback cuando la proyección local de un consumidor está obsoleta
	// (ADR 26 §5); no es ruta normal.
	ObtenerSesion(ctx context.Context, sesionID string) (Estado, error)

	// ListarRevocadas devuelve el snapshot de tombstones no expirados y el
	// cursor de continuación (ADR 26 §4). `cursor` vacío = snapshot inicial.
	ListarRevocadas(ctx context.Context, cursor string) (PaginaRevocadas, error)
}
