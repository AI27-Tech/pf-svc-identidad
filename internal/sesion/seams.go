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
	Issue(ctx context.Context, usuario, contrasena string, sesionID string) (idp.Token, error)

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
type EmissionFlow interface {
	Emit(ctx context.Context, steps EmissionSteps, usuario, contrasena string) (Emitida, error)
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
