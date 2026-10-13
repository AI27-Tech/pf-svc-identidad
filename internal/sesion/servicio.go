// servicio.go implementa sesion.Servicio (ADR 26). SCAFFOLD: trae la FORMA de
// la transaccionalidad y el orden deliberado de la revocación en dos pasos; la
// lógica real de Postgres y Redis queda como TODO.
//
// Fuente de verdad: `identidad.sesion` en Postgres. Redis/Valkey (clave
// `sesion:{sesion_id}`) es sólo caché caliente y NUNCA fuente de verdad: la
// ausencia de la clave no significa "sesión viva" (identidad.md (e)). Toda
// lectura de sesión cae a Postgres.
//
// La implementación vive en el MISMO paquete `sesion` que su puerto: el paquete
// ya está bajo el `internal/` de nivel superior del módulo, así que queda
// inalcanzable desde fuera. Un `internal/` anidado adicional sólo introduciría
// un ciclo de importación con la fábrica, sin ganar ocultamiento.
package sesion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/claims"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// errNoImplementado marca los puntos de scaffold donde la lógica real espera.
// No es la taxonomía de error de la plataforma: eso lo mapea el handler HTTP.
var errNoImplementado = errors.New("sesion: no implementado (scaffold)")

// servicio implementa Servicio. Reúne el IdP (aislado tras su puerto), la
// composición de claims, la frontera transaccional, el publicador de outbox y
// la caché. No se exporta: la raíz del módulo lo obtiene por NewServicio.
type servicio struct {
	// Colaboradores inyectados
	idp       idp.IdentityProvider
	tx        BeginTx
	publisher *outbox.Publisher
	cache     Cache

	// Costuras configurables por opciones (design.md §Components, seams.go)
	idGenerator          func() string
	emissionFlow         EmissionFlow
	sujetoResolver       SujetoResolver
	expiryPolicy         ExpiryPolicy
	refreshResolver      RefreshResolver
	revokedRefreshMode   RevokedRefresh
	revokeFailureHandler IdpRevokeFailureHandler
	auditRecorder        AuditRecorder
	revocationLog        RevocationLog
	payloadEncoder       PayloadEncoder
	cacheTimeout         time.Duration

	// Envoltura best-effort de la caché (inicializada en NewServicio)
	bestEffortCache bestEffortCache
}

var _ Servicio = (*servicio)(nil)

// NewServicio construye el servicio de sesión concreto y lo devuelve como la
// interfaz pública Servicio. Recibe sus colaboradores ya resueltos; no lee
// entorno ni construye el adaptador del IdP (eso es de New del módulo, que a su
// vez espera a ADR 32 para el adaptador real).
//
// Sin opciones, el servicio se comporta como hoy (scaffold): EmitirSesion y
// RefrescarSesion devuelven errNoImplementado, el snapshot no tiene cursor,
// etc. Cada opción cablea una costura: WithEmissionFlow, WithExpiryPolicy,
// WithSujetoResolver, etc.
func NewServicio(prov idp.IdentityProvider, tx BeginTx, pub *outbox.Publisher, cache Cache, opts ...Option) Servicio {
	s := &servicio{
		idp:            prov,
		tx:             tx,
		publisher:      pub,
		cache:          cache,
		idGenerator:    DefaultIDGenerator,
		payloadEncoder: DefaultPayloadEncoder,
		// El resto inicia con cero (nil o 0)
	}
	for _, opt := range opts {
		opt(s)
	}
	// Envolver la caché en best-effort (K-8)
	s.bestEffortCache = newBestEffortCache(cache, s.cacheTimeout)
	// Inicializar el log de revocación por omisión (A-10)
	if s.revocationLog == nil {
		s.revocationLog = newSnapshotOnlyLog(tx)
	}
	return s
}

// EmitirSesion: verifica credencial en el IdP, compone claims y registra la
// sesión con un `sesion_id` nuevo.
func (s *servicio) EmitirSesion(ctx context.Context, usuario, contrasena string) (Emitida, error) {
	// SCAFFOLD: el `sesion_id` lo GENERA AI27 (DEC-083 #1/#3) y se pasa a Issue
	// para que el IdP lo transporte como claim. Aquí va un marcador: la
	// generación real y, sobre todo, el ORDEN entre Issue y el registro en
	// `identidad.sesion` lo cierra DEC-085 (pendiente) en E4-T3; no se cablea en
	// producción hasta entonces (nota del review).
	var sesionID string // TODO(E4-T3/DEC-085): generar el sesion_id y fijar el orden Issue↔registro.
	tok, err := s.idp.Issue(ctx, idp.Credentials{Usuario: usuario, Contrasena: contrasena}, sesionID)
	if err != nil {
		return Emitida{}, err
	}
	_ = tok
	// TODO(sesion): resolver el modelo vigente del sujeto, componer claims
	// (claims.Componer) para dejar constancia de la composición en emisión,
	// persistir en `identidad.sesion` (incluido tok.IdpSessionID como
	// `idp_session_id`) y cachear en Redis (best-effort). La composición real
	// corre en el IdP (ADR 32 §4); aquí se deja la costura.
	_ = claims.Componer
	return Emitida{}, errNoImplementado
}

// RefrescarSesion: conserva el `sesion_id` y REEVALÚA los claims (DEC-031).
func (s *servicio) RefrescarSesion(ctx context.Context, refreshToken string) (Emitida, error) {
	tok, err := s.idp.Refresh(ctx, refreshToken)
	if err != nil {
		return Emitida{}, err
	}
	_ = tok
	// TODO(sesion): recomponer claims desde el modelo vigente (DEC-031), sin
	// cambiar el sesion_id (ADR 26 §1). El alcance NO entra en el token (DEC-053).
	return Emitida{}, errNoImplementado
}

// RevocarSesion ejecuta los DOS PASOS en el orden deliberado de identidad.md (h).
// El orden está en el CÓDIGO, no sólo en un comentario (Requisito 4.1, 4.3).
func (s *servicio) RevocarSesion(ctx context.Context, sesionID string) error {
	// --- PASO 1: revocación local + evento en la MISMA transacción -----------
	// Si este paso falla, "no ocurrió": el estado queda consistente y el cliente
	// reintenta. El evento `sesion.revocada` se escribe en `outbox` dentro de la
	// misma tx que la revocación local (P17, Requisito 4.1).
	res, err := s.revocarLocalConEvento(ctx, sesionID)
	if err != nil {
		return err // Requisito 4.4: error en Paso 1 = no ocurrió; sin Paso 2
	}

	// Caché: invalidar es best-effort y NO fuente de verdad (Requisito 8).
	// Un fallo aquí no revierte el paso 1 ni bloquea el paso 2 (K-8).
	s.bestEffortCache.invalidate(ctx, sesionID)

	// --- PASO 2: revocar en el IdP con el idp_session_id de la fila -----------
	// Paso 2 se ejecuta SOLO si Paso 1 se comprometió. Corrige H-1: Revoke recibe
	// idp_session_id, no sesionID (Requisito 4.2, 4.3).

	if res.Kind == Paso1NotFound {
		// Sesión inexistente. Comportamiento de hoy: éxito (204). A-8 puede
		// cambiar esto a 404; eso cambia solo el handler HTTP, no aquí.
		return nil
	}

	pend := PendingIdpRevocation{SesionID: sesionID, IdpSessionID: res.IdpSessionID}

	// Si idp_session_id está vacío, no se puede invocar el IdP. El handler A-7
	// decide qué hacer; si no está configurado, devolver el error (K-19).
	if res.IdpSessionID == "" {
		return s.onIdpRevokeFailure(ctx, pend, ErrMissingIdpSessionID)
	}

	// Invocar Revoke con el idp_session_id de la fila (Requisito 4.2; corrige H-1).
	// Si falla, el Paso 1 ya es efectivo (Requisito 4.5); reintento via A-7.
	if err := s.idp.Revoke(ctx, res.IdpSessionID); err != nil {
		return s.onIdpRevokeFailure(ctx, pend, err)
	}

	return nil
}

// onIdpRevokeFailure es invocado cuando el Paso 2 falla o no puede ejecutarse.
// El Paso 1 ya es efectivo (Requisito 4.5).
func (s *servicio) onIdpRevokeFailure(ctx context.Context, p PendingIdpRevocation, cause error) error {
	if s.revokeFailureHandler != nil {
		return s.revokeFailureHandler.OnFailure(ctx, p, cause) // A-7
	}
	// Sin handler, devolver el error envuelto. El handler HTTP lo mapea a 501.
	return fmt.Errorf("%w: %w", ErrIdpRevokePending, cause)
}

// revocarLocalConEvento es el PASO 1 (Requisito 4.1): una sola transacción que
// compromete la revocación en `identidad.sesion` y la fila `sesion.revocada` en
// `outbox`. Devuelve el Paso1Result para que el Paso 2 sepa qué hacer.
//
// Idempotente (P13, Requisito 4.7): el UPDATE sólo marca una sesión viva
// (revocada_en IS NULL); si ya estaba revocada, no vuelve a marcar ni re-emite
// el evento. Una sesión inexistente es un no-op con éxito (comportamiento actual).
func (s *servicio) revocarLocalConEvento(ctx context.Context, sesionID string) (Paso1Result, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Paso1Result{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op si ya hubo commit

	st := newStore(s.tx)
	res, err := st.revocarLocalConEvento(ctx, tx, sesionID)
	if err != nil {
		return Paso1Result{}, err
	}

	// Solo emitir evento si se marcó una fila viva (no si ya estaba revocada
	// o no existe). Requisito 4.7: idempotencia significa sin evento nuevo
	// cuando ya está revocada.
	if res.Kind == Paso1Revoked {
		// El evento se escribe en la MISMA tx: dominio y evento se comprometen
		// juntos (P17, Requisito 4.1). Payload mínimo (ADR 26 §7, Requisito 5.1):
		// sesion_id, revocada_en, expira_en.
		payload, err := s.payloadEncoder(res.Tombstone)
		if err != nil {
			return Paso1Result{}, fmt.Errorf("sesion: codificar payload: %w", err)
		}

		ev := outbox.Event{
			Tipo:         "sesion.revocada",
			Version:      1,
			AgregadoID:   sesionID,
			Sensibilidad: outbox.SensibilidadAmplia,
			Payload:      payload,
		}
		if err := s.publisher.Publish(ctx, tx, ev); err != nil {
			return Paso1Result{}, err
		}

		// Registrar auditoría local si hay recorder configurado (A-12, A-16,
		// Requisito 5.3). Mismo motivo que el evento: en la misma tx.
		if s.auditRecorder != nil {
			audit := RevocationAudit{
				SesionID:   sesionID,
				SujetoID:   "",  // TODO(A-5): obtener del store o del Paso 1
				TenantID:   nil, // TODO(A-16): con A-5
				ActorID:    nil, // TODO(A-12): en headers u otro lado
				Motivo:     nil, // TODO(A-12): en headers u otro lado
				RevocadaEn: res.Tombstone.RevocadaEn,
			}
			if err := s.auditRecorder.RecordRevocation(ctx, tx, audit); err != nil {
				return Paso1Result{}, err
			}
		}
	}

	return res, tx.Commit(ctx)
}

// ObtenerSesion lee el estado durable desde Postgres (Requisito 6.1, 6.2).
// Devuelve 404 si no existe (Requisito 6.2; Requisito 4.2 de requerimientos).
func (s *servicio) ObtenerSesion(ctx context.Context, sesionID string) (Estado, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Estado{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := newStore(s.tx)
	estado, err := st.obtenerSesion(ctx, tx, sesionID)
	if err != nil {
		// Envolver ErrNoEncontrada en un PlatformError de categoría NoEncontrado
		// para que el handler HTTP lo traduzca a 404 (Requisito 6.2, H-4).
		return Estado{}, WrapNotFound(err)
	}
	return estado, tx.Commit(ctx)
}

// ListarRevocadas entrega el snapshot de tombstones + cursor (Requisito 7, ADR 26 §4).
// Lee las sesiones revocadas AÚN NO expiradas: una vez expirada, un consumidor
// ya la rechaza por el `exp` del propio token (Requisito 7.1).
func (s *servicio) ListarRevocadas(ctx context.Context, cursor string) (PaginaRevocadas, error) {
	// La costura RevocationLog (A-10) encapsula el snapshot y la continuación.
	// Se inicializa en NewServicio si no está configurada (comportamiento por omisión).
	if cursor == "" {
		return s.revocationLog.Snapshot(ctx)
	}
	return s.revocationLog.Since(ctx, cursor)
}
