package sesion

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/jackc/pgx/v5"
)

// SesionRecord es la fila de sesión que se registra en la emisión (Requisito 2.1).
type SesionRecord struct {
	SesionID     string
	SujetoID     string
	EmitidaEn    time.Time
	ExpiraEn     time.Time
	IdpSessionID string // idp.Token.IdpSessionID, sin transformar
}

// SujetoRef es la referencia del sujeto de AI27 al que pertenece una sesión
// (usada en A-5 y A-16).
type SujetoRef struct {
	SujetoID string
	TenantID string
}

// EmissionSteps son los pasos atómicos que el servicio ya sabe ejecutar.
// El EmissionFlow (A-1) decide el ORDEN y la compensación.
type EmissionSteps interface {
	// NewSesionID genera un nuevo sesion_id antes de llamar a Issue (Requisito 1.1).
	NewSesionID() string

	// Issue invoca al IdP con el sesion_id ya generado (Requisito 1.1; R1).
	// Recibe las credenciales completas, incluido el segundo factor (A-15).
	Issue(ctx context.Context, cred Credentials, sesionID string) (idp.Token, error)

	// ResolveSujeto obtiene el sujeto de AI27 del token emitido (A-5).
	ResolveSujeto(ctx context.Context, tok idp.Token) (SujetoRef, error)

	// ExpiresAtIssue fija la expiración que se registra en la fila (A-3).
	ExpiresAtIssue(tok idp.Token) (time.Time, error)

	// Register inserta la fila de sesión (Requisito 2.1). Si falla con
	// violación de UNIQUE (dos emisiones del mismo sujeto), la decisión
	// de compensación la decide EmissionFlow (A-1, DEC-085).
	Register(ctx context.Context, r SesionRecord) error

	// Compensate termina en el IdP la sesión de un token que no se pudo registrar.
	// Lo usaría la opción 1 de DEC-085; ningún flujo productivo lo llama hoy (A-1).
	Compensate(ctx context.Context, idpSessionID string) error

	// WarmCache calienta el registro caliente después del commit (K-29).
	// best-effort; nunca falla.
	WarmCache(ctx context.Context, r SesionRecord)
}

// EmissionFlow fija el orden emisión↔registro (A-1, DEC-085).
// Recibe las credenciales completas para pasar el segundo factor al IdP (A-15).
type EmissionFlow interface {
	Emit(ctx context.Context, steps EmissionSteps, cred Credentials) (Emitida, error)
}

// SujetoResolver obtiene el sujeto de AI27 de una emisión (A-5).
type SujetoResolver interface {
	ResolveSujeto(ctx context.Context, tok idp.Token) (SujetoRef, error)
}

// ExpiryPolicy fija el expira_en de la fila (A-3).
type ExpiryPolicy interface {
	AtIssue(tok idp.Token) (time.Time, error)
	AtRefresh(current time.Time, tok idp.Token) (time.Time, error)
}

// RefreshResolver identifica la fila de la sesión refrescada (A-4).
type RefreshResolver interface {
	SesionIDFor(ctx context.Context, tok idp.Token) (string, error)
}

// RevokedRefresh responde la pregunta abierta de A-4: qué pasa cuando se
// refresca una sesión revocada.
type RevokedRefresh int

const (
	RevokedRefreshUndecided RevokedRefresh = iota
	RevokedRefreshReject
	RevokedRefreshDelegateToIdP
)

// PendingIdpRevocation identifica un Paso 2 que no terminó (A-7).
type PendingIdpRevocation struct {
	SesionID     string
	IdpSessionID string // vacío si la fila no lo tiene
}

// IdpRevokeFailureHandler decide qué pasa cuando el Paso 2 falla o no puede
// ejecutarse (A-7). Nunca revierte el Paso 1. Lo que devuelve es lo que recibe
// el llamador de RevocarSesion.
type IdpRevokeFailureHandler interface {
	OnFailure(ctx context.Context, p PendingIdpRevocation, cause error) error
}

// RevocationAudit es lo que la auditoría local registra por revocación
// (ADR 26 §7; Requisito 5.3).
type RevocationAudit struct {
	SesionID   string
	SujetoID   string
	TenantID   *string // A-16
	ActorID    *string // A-12
	Motivo     *string // A-12
	RevocadaEn time.Time
}

// AuditRecorder escribe en cambio_identidad (Requisito 5.3; A-12, A-16).
// Recibe la tx del Paso 1 para que la decisión de K-6 (misma tx o aparte)
// no cambie la firma.
type AuditRecorder interface {
	RecordRevocation(ctx context.Context, tx pgx.Tx, a RevocationAudit) error
}

// RevocationLog entrega snapshot y continuación (A-10).
type RevocationLog interface {
	Snapshot(ctx context.Context) (PaginaRevocadas, error)
	Since(ctx context.Context, cursor string) (PaginaRevocadas, error)
}

// PayloadEncoder serializa el payload de sesion.revocada (A-14, Requisito 5.1).
type PayloadEncoder func(t Tombstone) (json.RawMessage, error)

// Fase distingue en qué ruta se midió la latencia de la composición de claims
// (Requisito 3 de E4-T4): la emisión (login) o el refresco. El refresco llega a
// ráfagas, así que operación quiere vigilar su latencia por separado de la de
// emisión.
type Fase int

const (
	// FaseEmision es la medición en POST /api/v1/sesiones (login).
	FaseEmision Fase = iota
	// FaseRefresco es la medición en POST /api/v1/sesiones/refresh.
	FaseRefresco
)

// String devuelve el nombre de la fase, para etiquetar la señal sin acoplar el
// paquete a un backend de métricas concreto (B-2, pendiente).
func (f Fase) String() string {
	switch f {
	case FaseEmision:
		return "emision"
	case FaseRefresco:
		return "refresco"
	default:
		return "desconocida"
	}
}

// LatencyObserver registra la latencia de la composición de claims del IdP,
// distinguiendo emisión de refresco (Requisito 3 de E4-T4, ADR 16).
//
// La composición real corre DENTRO del IdP (mapper o extensión, ADR 32 §4): la
// latencia que `identidad` observa es la del Issue/Refresh del adaptador, que
// incluye la composición (B-2, opción a por omisión). La forma concreta de la
// señal (nombre de métrica, etiquetas, backend) queda tras esta costura hasta
// que se decida B-2; el valor por omisión es un no-op que no reporta nada.
//
// La observación es best-effort: nunca falla ni demora la operación (mismo
// criterio que la caché best-effort de E4-T3).
type LatencyObserver interface {
	// ObservarComposicion registra que una composición de la fase dada tardó d.
	ObservarComposicion(fase Fase, d time.Duration)
}

// nopLatencyObserver es el LatencyObserver por omisión: descarta la observación.
// Deja a NewServicio sin opción comportándose como antes de E4-T4.
type nopLatencyObserver struct{}

// ObservarComposicion no hace nada (valor por omisión, Requisito 3.1).
func (nopLatencyObserver) ObservarComposicion(Fase, time.Duration) {}
