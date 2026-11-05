// revocationlog.go implementa el RevocationLog productivo con cursor por marca
// de tiempo (A-10, opción b aprobada en DEC-088: cursor = timestamp del momento
// del snapshot).
//
// El cursor es el instante de la consulta codificado en base64. El snapshot
// inicial (sin cursor) devuelve todos los tombstones no expirados y un cursor
// con el instante de la consulta. Una continuación (Since) devuelve las
// revocaciones comprometidas DESPUÉS de ese instante y un cursor nuevo.
//
// Limitación conocida (H-6, aceptada por DEC-088): `revocada_en = now()` es la
// hora de INICIO de la transacción, no la del commit, así que dos revocaciones
// concurrentes pueden quedar con marcas que no respetan el orden de commit. El
// riesgo es un hueco micrométrico; para el volumen inicial es aceptable y la
// opción b se eligió a sabiendas. Una implementación por visibilidad de snapshot
// de Postgres (xid8) lo cerraría, pero no se adoptó en V1.
package sesion

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"
)

// timestampLog implementa RevocationLog con cursor por marca de tiempo (A-10 b).
type timestampLog struct {
	tx BeginTx
}

// newTimestampLog devuelve el RevocationLog productivo con cursor por timestamp.
func newTimestampLog(tx BeginTx) RevocationLog {
	return &timestampLog{tx: tx}
}

// Snapshot devuelve todos los tombstones no expirados y un cursor con el
// instante de la consulta (Requisito 7.1, 7.2).
func (l *timestampLog) Snapshot(ctx context.Context) (PaginaRevocadas, error) {
	tx, err := l.tx.Begin(ctx)
	if err != nil {
		return PaginaRevocadas{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ahora time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&ahora); err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: snapshot, hora de consulta: %w", err)
	}

	rows, err := tx.Query(ctx,
		`SELECT sesion_id, revocada_en, expira_en
		   FROM identidad.sesion
		  WHERE revocada_en IS NOT NULL
		    AND revocada_en <= $1
		    AND expira_en > $1
		  ORDER BY revocada_en, sesion_id`,
		ahora)
	if err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: snapshot revocadas: %w", err)
	}
	items, err := escanearTombstones(rows)
	if err != nil {
		return PaginaRevocadas{}, err
	}

	cursor := codificarCursorTiempo(ahora)
	return PaginaRevocadas{Items: items, CursorSiguiente: &cursor}, nil
}

// Since devuelve los tombstones comprometidos después del instante del cursor y
// un cursor nuevo con el instante de esta consulta (Requisito 7.2, 7.3). Un
// cursor ilegible es un error de validación (el consumidor debe reiniciar con
// un snapshot).
func (l *timestampLog) Since(ctx context.Context, cursor string) (PaginaRevocadas, error) {
	desde, err := decodificarCursorTiempo(cursor)
	if err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: cursor de revocadas inválido: %w", err)
	}

	tx, err := l.tx.Begin(ctx)
	if err != nil {
		return PaginaRevocadas{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var ahora time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&ahora); err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: since, hora de consulta: %w", err)
	}

	rows, err := tx.Query(ctx,
		`SELECT sesion_id, revocada_en, expira_en
		   FROM identidad.sesion
		  WHERE revocada_en IS NOT NULL
		    AND revocada_en > $1
		    AND revocada_en <= $2
		    AND expira_en > $2
		  ORDER BY revocada_en, sesion_id`,
		desde, ahora)
	if err != nil {
		return PaginaRevocadas{}, fmt.Errorf("sesion: since revocadas: %w", err)
	}
	items, err := escanearTombstones(rows)
	if err != nil {
		return PaginaRevocadas{}, err
	}

	nuevo := codificarCursorTiempo(ahora)
	return PaginaRevocadas{Items: items, CursorSiguiente: &nuevo}, nil
}

// escanearTombstones consume las filas de un snapshot/continuación. Cierra las
// rows.
func escanearTombstones(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}) ([]Tombstone, error) {
	defer rows.Close()
	var items []Tombstone
	for rows.Next() {
		var tb Tombstone
		if err := rows.Scan(&tb.SesionID, &tb.RevocadaEn, &tb.ExpiraEn); err != nil {
			return nil, fmt.Errorf("sesion: escanear tombstone: %w", err)
		}
		items = append(items, tb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sesion: iterar tombstones: %w", err)
	}
	return items, nil
}

// codificarCursorTiempo codifica un instante como cursor opaco base64 (RFC 3339
// con nanosegundos, en UTC). No expone un formato que el cliente deba interpretar.
func codificarCursorTiempo(t time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano)))
}

// decodificarCursorTiempo revierte codificarCursorTiempo.
func decodificarCursorTiempo(cursor string) (time.Time, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, string(raw))
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
