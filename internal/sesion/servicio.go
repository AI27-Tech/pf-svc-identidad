// servicio.go implementa sesion.Servicio (ADR 26). La transaccionalidad y el
// orden deliberado de la revocación en dos pasos están implementados con la
// lógica real de Postgres y la caché (Redis). Sólo la emisión y el refresco
// (login) quedan tras costuras: sin ellas, EmitirSesion/RefrescarSesion
// devuelven errNoImplementado; con ellas cableadas (identidad.New tras DEC-088),
// ejecutan el flujo real.
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
	// Inicializar el log de revocación por omisión (A-10, opción b aprobada en
	// DEC-088: cursor por timestamp). Una opción WithRevocationLog puede
	// sustituirlo (p. ej. por una implementación de prueba).
	if s.revocationLog == nil {
		s.revocationLog = newTimestampLog(tx)
	}
	return s
}

// EmitirSesion verifica la credencial en el IdP y registra la sesión con un
// `sesion_id` nuevo (Requisito 1, R1). Delega en el EmissionFlow configurado
// (A-1, opción 1 de DEC-085: emitir primero, compensar si falla el registro).
//
// Si falta alguna de las costuras necesarias (EmissionFlow, SujetoResolver o
// ExpiryPolicy), devuelve errNoImplementado (501): sin ellas no se puede emitir
// de forma segura. En producción, identidad.New las cablea.
func (s *servicio) EmitirSesion(ctx context.Context, cred Credentials) (Emitida, error) {
	if s.emissionFlow == nil || s.sujetoResolver == nil || s.expiryPolicy == nil {
		return Emitida{}, errNoImplementado
	}
	return s.emissionFlow.Emit(ctx, &emissionSteps{s: s}, cred)
}

// RefrescarSesion renueva una sesión conservando su `sesion_id` (ADR 26 §1).
// Resuelve la fila a partir del token refrescado (A-4, opción a: el sesion_id
// del claim del token nuevo), rechaza el refresco de una sesión revocada (A-4,
// Reject aprobado en DEC-088) y actualiza la expiración de la fila (A-3, opción
// b). La reevaluación de claims es E4-T4.
//
// Si falta RefreshResolver o ExpiryPolicy, o el modo es RevokedRefreshUndecided,
// devuelve errNoImplementado (501). En producción, identidad.New las cablea.
func (s *servicio) RefrescarSesion(ctx context.Context, refreshToken string) (Emitida, error) {
	if s.refreshResolver == nil || s.expiryPolicy == nil || s.revokedRefreshMode == RevokedRefreshUndecided {
		return Emitida{}, errNoImplementado
	}
	return s.refrescar(ctx, refreshToken)
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
	//
	// RIESGO ABIERTO (E4-T3, no se cierra aquí): `res.IdpSessionID` debe ser el id
	// NATIVO de la sesión en el IdP, no el `sesion_id` de AI27 (contrato de
	// idp.IdentityProvider.Revoke). El adaptador Keycloak sólo los trata como
	// iguales cuando su `SessionResolver` es nil (pendiente A-4). Mientras no se
	// cablee el `SessionResolver` real ni se pueble `idp_session_id` (DEC-085),
	// el corte en Keycloak podría apuntar al id equivocado y NO ser efectivo
	// aunque ya no haya panic. Resolverlo es alcance de E4-T3.
	if err := s.idp.Revoke(ctx, res.IdpSessionID); err != nil {
		return s.onIdpRevokeFailure(ctx, pend, err)
	}

	return nil
}

// onIdpRevokeFailure es invocado cuando el Paso 2 falla o no puede ejecutarse.
// El Paso 1 ya es efectivo (Requisito 4.5). Normaliza el error para no revelar
// detalles de autorización del IdP (hallazgo de seguridad: 403 del proveedor no
// se propaga al cliente, para evitar reconocimiento de que el idp_session_id
// existe pero es prohibido revocar).
func (s *servicio) onIdpRevokeFailure(ctx context.Context, p PendingIdpRevocation, cause error) error {
	if s.revokeFailureHandler != nil {
		return s.revokeFailureHandler.OnFailure(ctx, p, cause) // A-7
	}
	// Sin handler, devolver error genérico sin revelar detalles del IdP.
	// La causa se puede loguear internamente si se requiere auditoría, pero
	// NO se propaga al cliente.
	return ErrIdpRevokePending
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
		//
		// Datos (DEC-088):
		//   - sujeto_id (A-5): de la fila revocada (res.SujetoID).
		//   - tenant_id (A-16): de sujeto.tenant_id por join en la misma tx.
		//   - actor_id (A-12): lo resuelve el recorder desde el contexto.
		//   - motivo (A-12): nulo en V1 (se difiere a V2).
		if s.auditRecorder != nil {
			var tenantID *string
			if res.SujetoID != "" {
				t, err := st.tenantDeSujeto(ctx, tx, res.SujetoID)
				if err != nil {
					return Paso1Result{}, fmt.Errorf("sesion: auditoría, tenant del sujeto: %w", err)
				}
				tenantID = &t
			}
			audit := RevocationAudit{
				SesionID:   sesionID,
				SujetoID:   res.SujetoID,
				TenantID:   tenantID,
				ActorID:    nil, // lo resuelve el recorder desde el contexto (A-12)
				Motivo:     nil, // A-12: motivo diferido a V2
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
