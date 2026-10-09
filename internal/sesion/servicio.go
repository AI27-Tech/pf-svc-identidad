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

	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/claims"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// errNoImplementado marca los puntos de scaffold donde la lógica real espera.
// No es la taxonomía de error de la plataforma: eso lo mapea el handler HTTP.
var errNoImplementado = errors.New("sesion: no implementado (scaffold)")

// ErrNoEncontrada indica que no existe una sesión con el id dado. El handler
// HTTP la mapea a NoEncontrado (404); no es la taxonomía de plataforma aquí.
var ErrNoEncontrada = errors.New("sesion: no encontrada")

// servicio implementa Servicio. Reúne el IdP (aislado tras su puerto), la
// composición de claims, la frontera transaccional, el publicador de outbox y
// la caché. No se exporta: la raíz del módulo lo obtiene por NewServicio.
type servicio struct {
	idp       idp.IdentityProvider
	tx        BeginTx
	publisher *outbox.Publisher
	cache     Cache
}

var _ Servicio = (*servicio)(nil)

// NewServicio construye el servicio de sesión concreto y lo devuelve como la
// interfaz pública Servicio. Recibe sus colaboradores ya resueltos; no lee
// entorno ni construye el adaptador del IdP (eso es de New del módulo, que a su
// vez espera a ADR 32 para el adaptador real).
func NewServicio(prov idp.IdentityProvider, tx BeginTx, pub *outbox.Publisher, cache Cache) Servicio {
	return &servicio{idp: prov, tx: tx, publisher: pub, cache: cache}
}

// EmitirSesion: verifica credencial en el IdP, compone claims y registra la
// sesión con un `sesion_id` nuevo.
func (s *servicio) EmitirSesion(ctx context.Context, usuario, contrasena string) (Emitida, error) {
	tok, err := s.idp.Issue(ctx, idp.Credentials{Usuario: usuario, Contrasena: contrasena})
	if err != nil {
		return Emitida{}, err
	}
	_ = tok
	// TODO(sesion): resolver el modelo vigente del sujeto, componer claims
	// (claims.Componer) para dejar constancia de la composición en emisión,
	// generar el sesion_id propio de AI27, persistir en `identidad.sesion` y
	// cachear en Redis (best-effort). La composición real corre en el IdP
	// (ADR 32 §4); aquí se deja la costura.
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
// El orden está en el CÓDIGO, no sólo en un comentario.
func (s *servicio) RevocarSesion(ctx context.Context, sesionID string) error {
	// --- PASO 1: revocación local + evento en la MISMA transacción -----------
	// Si este paso falla, "no ocurrió": el estado queda consistente y el cliente
	// reintenta. El evento `sesion.revocada` se escribe en `outbox` dentro de la
	// misma tx que la revocación local (P17).
	if err := s.revocarLocalConEvento(ctx, sesionID); err != nil {
		return err
	}

	// Caché: invalidar es best-effort y NO fuente de verdad. Un fallo aquí no
	// revierte el paso 1 ni bloquea el paso 2.
	if s.cache != nil {
		_ = s.cache.InvalidarSesion(ctx, sesionID)
	}

	// --- PASO 2: revocar en el IdP (idempotente) -----------------------------
	// La revocación YA es efectiva en AI27 tras el paso 1. Si el IdP falla, se
	// reintenta de forma idempotente; no se invierte el orden.
	if err := s.idp.Revoke(ctx, sesionID); err != nil {
		// TODO(sesion): encolar reintento idempotente del paso 2. El error no
		// revierte la revocación local, que ya es efectiva (ADR 26).
		return err
	}
	return nil
}

// revocarLocalConEvento es el PASO 1: una sola transacción que compromete la
// revocación en `identidad.sesion` y la fila `sesion.revocada` en `outbox`.
//
// Idempotente (P13): el UPDATE sólo marca una sesión viva (revocada_en IS NULL);
// si ya estaba revocada, no vuelve a marcar ni re-emite el evento, y la llamada
// tiene éxito. Una sesión inexistente es un no-op con éxito: revocar lo que no
// hay deja el sistema en el estado deseado (no existe sesión viva con ese id).
func (s *servicio) revocarLocalConEvento(ctx context.Context, sesionID string) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op si ya hubo commit

	// Marca la fila sólo si está viva, y devuelve el expira_en para el payload.
	// Si no hay fila viva (ya revocada o inexistente), no devuelve filas.
	var revocadaEn, expiraEn time.Time
	err = tx.QueryRow(ctx,
		`UPDATE identidad.sesion
		    SET revocada_en = now()
		  WHERE sesion_id = $1 AND revocada_en IS NULL
		  RETURNING revocada_en, expira_en`,
		sesionID,
	).Scan(&revocadaEn, &expiraEn)
	if errors.Is(err, pgx.ErrNoRows) {
		// Ya revocada o inexistente: idempotente, nada que emitir. Commit vacío.
		return tx.Commit(ctx)
	}
	if err != nil {
		return fmt.Errorf("sesion: marcar revocada %s: %w", sesionID, err)
	}

	// El evento se escribe en la MISMA tx: dominio y evento se comprometen
	// juntos (P17). Payload mínimo (ADR 26 §7): sesion_id, revocada_en, expira_en,
	// con los valores REALES de la fila recién marcada.
	ev := outbox.Event{
		Tipo:         "sesion.revocada",
		Version:      1,
		AgregadoID:   sesionID, // orden por sesion_id (INVENTARIO.md)
		Sensibilidad: outbox.SensibilidadAmplia,
		Payload: map[string]any{
			"sesion_id":   sesionID,
			"revocada_en": revocadaEn.UTC(),
			"expira_en":   expiraEn.UTC(),
		},
	}
	if err := s.publisher.Publish(ctx, tx, ev); err != nil {
		return err
	}

	// TODO(sesion): registrar en `cambio_identidad` el motivo/sujeto/actor
	// (auditoría local, ADR 26 §7), en esta misma tx.

	return tx.Commit(ctx)
}

// ObtenerSesion lee el estado durable desde Postgres (fallback, ADR 26 §5).
func (s *servicio) ObtenerSesion(ctx context.Context, sesionID string) (Estado, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Estado{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var revocadaEn *time.Time
	var expiraEn time.Time
	err = tx.QueryRow(ctx,
		`SELECT revocada_en, expira_en FROM identidad.sesion WHERE sesion_id = $1`,
		sesionID,
	).Scan(&revocadaEn, &expiraEn)
	if errors.Is(err, pgx.ErrNoRows) {
		return Estado{}, ErrNoEncontrada
	}
	if err != nil {
		return Estado{}, fmt.Errorf("sesion: obtener %s: %w", sesionID, err)
	}
	return Estado{
		SesionID:   sesionID,
		Revocada:   revocadaEn != nil,
		RevocadaEn: revocadaEn,
		ExpiraEn:   expiraEn,
	}, nil
}

// ListarRevocadas entrega el snapshot de tombstones + cursor (ADR 26 §4). Lee
// las sesiones revocadas AÚN NO expiradas: una vez expirada, un consumidor ya la
// rechaza por el `exp` del propio token, así que no necesita el tombstone.
func (s *servicio) ListarRevocadas(ctx context.Context, cursor string) (PaginaRevocadas, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return PaginaRevocadas{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`SELECT sesion_id, revocada_en, expira_en
		   FROM identidad.sesion
		  WHERE revocada_en IS NOT NULL AND expira_en > now()
		  ORDER BY revocada_en`)
	if err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: listar revocadas: %w", err)
	}
	defer rows.Close()

	var items []Tombstone
	for rows.Next() {
		var tb Tombstone
		if err := rows.Scan(&tb.SesionID, &tb.RevocadaEn, &tb.ExpiraEn); err != nil {
			return PaginaRevocadas{}, fmt.Errorf("sesion: escanear tombstone: %w", err)
		}
		items = append(items, tb)
	}
	if err := rows.Err(); err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: iterar tombstones: %w", err)
	}
	// TODO(sesion): paginación por cursor consistente para catch-up sin ventana
	// (ADR 26 §4). Hoy devuelve el snapshot completo de no expirados, sin cursor.
	return PaginaRevocadas{Items: items}, nil
}
