package sesion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// store agrupa el SQL sobre la tabla sesion (Requisito 2.1, Requisito 3.2,
// Requisito 4.1). No trae lógica de decisión: solo traduce entre el modelo
// de dominio y el relacional. Un sql.Store podría surgir si un consumidor
// abre el acceso a Postgres; por ahora es privado de sesion.
type store struct {
	tx BeginTx
}

// newStore devuelve un store listo para operaciones.
func newStore(tx BeginTx) *store {
	return &store{tx: tx}
}

// insert registra una fila de sesión con su idp_session_id (Requisito 2.1).
// Si el INSERT viola el índice una_sesion_viva (ya hay una sesión viva del
// mismo sujeto, A-2 opción conservada por DEC-088), Postgres devuelve el
// SQLSTATE 23505 (unique_violation), que se traduce a ErrLiveSesionExists para
// que el flujo de emisión lo distinga de otros fallos. No modifica el índice.
func (st *store) insert(ctx context.Context, tx pgx.Tx, r SesionRecord) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO identidad.sesion (sesion_id, sujeto_id, emitida_en, expira_en, idp_session_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		r.SesionID, r.SujetoID, r.EmitidaEn, r.ExpiraEn, r.IdpSessionID)
	if err != nil {
		// 23505 = unique_violation en Postgres. El único índice único sobre
		// filas vivas es una_sesion_viva; se traduce a ErrLiveSesionExists.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: %w", ErrLiveSesionExists, err)
		}
		return fmt.Errorf("sesion: insertar fila: %w", err)
	}
	return nil
}

// Paso1Result es el resultado del Paso 1 (revocación local + evento).
// Clasifica lo que ocurrió: se marcó la fila, ya estaba revocada o no existe.
type Paso1Result struct {
	Kind         Paso1Kind
	IdpSessionID string // idp_session_id de la fila, o "" si es NULL o no hay fila
	SujetoID     string // sujeto_id de la fila revocada (A-5, para la auditoría)
	Tombstone    Tombstone
}

type Paso1Kind int

const (
	Paso1Revoked        Paso1Kind = iota // La fila estaba viva y se marcó
	Paso1AlreadyRevoked                  // Ya tenía revocada_en
	Paso1NotFound                        // No hay fila
)

// revocarLocalConEvento ejecuta el Paso 1 dentro de una transacción del llamador.
// Devuelve la clasificación y los datos para el evento (o el Paso 2). El commit
// lo hace el llamador.
func (st *store) revocarLocalConEvento(ctx context.Context, tx pgx.Tx, sesionID string) (Paso1Result, error) {
	var revocadaEn, expiraEn time.Time
	var idpSessionID *string // nullable
	var sujetoID string
	err := tx.QueryRow(ctx,
		`UPDATE identidad.sesion
		    SET revocada_en = now()
		  WHERE sesion_id = $1 AND revocada_en IS NULL
		  RETURNING revocada_en, expira_en, idp_session_id, sujeto_id`,
		sesionID,
	).Scan(&revocadaEn, &expiraEn, &idpSessionID, &sujetoID)

	if errors.Is(err, pgx.ErrNoRows) {
		// No hay fila viva: clasifica si ya revocada o inexistente.
		var hasRow bool
		var existingIdpSessionID *string
		_ = tx.QueryRow(ctx,
			`SELECT true, idp_session_id FROM identidad.sesion WHERE sesion_id = $1`,
			sesionID,
		).Scan(&hasRow, &existingIdpSessionID)

		if hasRow {
			// Ya revocada: devolver la idp_session_id anterior (para reintento
			// del Paso 2, K-17 abierta, pero aquí ya queda el dato).
			idpSID := ""
			if existingIdpSessionID != nil {
				idpSID = *existingIdpSessionID
			}
			return Paso1Result{Kind: Paso1AlreadyRevoked, IdpSessionID: idpSID}, nil
		}
		// Inexistente: no hay nada que hacer. A-8 decide si es 204 o 404.
		return Paso1Result{Kind: Paso1NotFound}, nil
	}

	if err != nil {
		return Paso1Result{}, fmt.Errorf("sesion: paso 1 marcar revocada: %w", err)
	}

	// Se marcó exitosamente; devolver el tombstone para el evento.
	idpSID := ""
	if idpSessionID != nil {
		idpSID = *idpSessionID
	}
	return Paso1Result{
		Kind:         Paso1Revoked,
		IdpSessionID: idpSID,
		SujetoID:     sujetoID,
		Tombstone: Tombstone{
			SesionID:   sesionID,
			RevocadaEn: revocadaEn,
			ExpiraEn:   expiraEn,
		},
	}, nil
}

// tenantDeSujeto devuelve el tenant_id del sujeto, usado por la auditoría de
// revocación (A-16, opción a: el tenant sale de la fila del sujeto revocado, no
// del token del actor, que sería incorrecto en una revocación administrativa
// entre tenants). Se consulta dentro de la misma tx del Paso 1.
func (st *store) tenantDeSujeto(ctx context.Context, tx pgx.Tx, sujetoID string) (string, error) {
	var tenantID string
	err := tx.QueryRow(ctx,
		`SELECT tenant_id FROM identidad.sujeto WHERE id = $1`,
		sujetoID,
	).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoEncontrada
	}
	if err != nil {
		return "", fmt.Errorf("sesion: tenant del sujeto: %w", err)
	}
	return tenantID, nil
}

// obtenerSesion lee el estado durable de una sesión (Requisito 6.1).
func (st *store) obtenerSesion(ctx context.Context, tx pgx.Tx, sesionID string) (Estado, error) {
	var revocadaEn *time.Time
	var expiraEn time.Time
	err := tx.QueryRow(ctx,
		`SELECT revocada_en, expira_en FROM identidad.sesion WHERE sesion_id = $1`,
		sesionID,
	).Scan(&revocadaEn, &expiraEn)

	if errors.Is(err, pgx.ErrNoRows) {
		return Estado{}, ErrNoEncontrada
	}
	if err != nil {
		return Estado{}, fmt.Errorf("sesion: obtener estado: %w", err)
	}

	return Estado{
		SesionID:   sesionID,
		Revocada:   revocadaEn != nil,
		RevocadaEn: revocadaEn,
		ExpiraEn:   expiraEn,
	}, nil
}
