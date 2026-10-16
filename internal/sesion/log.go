package sesion

import (
	"context"
	"fmt"
)

// snapshotOnlyLog es la implementación por omisión de RevocationLog (A-10).
// Devuelve el snapshot de tombstones sin cursor (comportamiento de hoy, Requisito 7.1).
// Sin cursor devuelve snapshot completo; un cursor recibido se ignora y se
// devuelve el snapshot completo (sin progreso).
type snapshotOnlyLog struct {
	tx BeginTx
}

// newSnapshotOnlyLog devuelve un log de snapshot-solo.
func newSnapshotOnlyLog(tx BeginTx) RevocationLog {
	return &snapshotOnlyLog{tx: tx}
}

// Snapshot devuelve el snapshot de tombstones no expirados (Requisito 7.1).
func (l *snapshotOnlyLog) Snapshot(ctx context.Context) (PaginaRevocadas, error) {
	tx, err := l.tx.Begin(ctx)
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
		return PaginaRevocadas{}, fmt.Errorf("sesion: listar revocadas snapshot: %w", err)
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

	return PaginaRevocadas{Items: items}, nil // CursorSiguiente = nil (sin cursor)
}

// Since ignora el cursor y devuelve el snapshot completo (comportamiento de hoy,
// sin progreso; A-10 define semántica del cursor).
func (l *snapshotOnlyLog) Since(ctx context.Context, cursor string) (PaginaRevocadas, error) {
	// Sin cursor implementado, devolver el snapshot completo.
	return l.Snapshot(ctx)
}
