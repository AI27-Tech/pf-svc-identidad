package sesiontest

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PuntoDeFalla indica en qué paso de la transacción del Paso 1 debe fallar
// FaultyBeginTx. Permite ejercer la atomicidad: ante un fallo en cualquier
// punto, no debe quedar efecto parcial (ni fila marcada ni fila de outbox).
// [Propuesta de Kiro, no aprobada]
type PuntoDeFalla int

const (
	// FallaNinguno deja pasar toda la transacción (camino feliz).
	FallaNinguno PuntoDeFalla = iota
	// FallaBegin falla al abrir la transacción.
	FallaBegin
	// FallaUpdate falla en el primer Exec/Query (el UPDATE de la fila).
	FallaUpdate
	// FallaOutbox falla en el Exec del INSERT de outbox (segunda escritura).
	FallaOutbox
	// FallaCommit falla al confirmar la transacción.
	FallaCommit
)

// ErrFallaInyectada es el error que FaultyBeginTx devuelve en el punto elegido.
var ErrFallaInyectada = errors.New("sesiontest: falla inyectada en la transacción")

// FaultyBeginTx implementa sesion.BeginTx envolviendo un BeginTx real (un pool)
// y falla en el PuntoDeFalla configurado. Delega todo lo demás en la tx real,
// de modo que el camino feliz se comporta como Postgres de verdad. Lo usan los
// property tests de atomicidad del Paso 1 (8.2) y del Paso 2 (8.3).
// [Propuesta de Kiro, no aprobada]
type FaultyBeginTx struct {
	// Real es el BeginTx verdadero (p. ej. un *pgxpool.Pool adaptado).
	Real interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	}
	// Punto indica dónde falla.
	Punto PuntoDeFalla
}

// Begin abre la transacción real (o falla si Punto == FallaBegin) y la envuelve
// en una faultyTx que intercepta las escrituras y el commit.
func (f *FaultyBeginTx) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.Punto == FallaBegin {
		return nil, ErrFallaInyectada
	}
	tx, err := f.Real.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &faultyTx{Tx: tx, punto: f.Punto}, nil
}

// faultyTx envuelve una pgx.Tx real. Embebe la interfaz, así que todos los
// métodos que no sobreescribimos delegan en la tx verdadera. Interceptamos
// Exec (para distinguir el UPDATE del INSERT de outbox) y Commit.
type faultyTx struct {
	pgx.Tx
	punto    PuntoDeFalla
	execVist int // cuántos Exec se han visto (1 = UPDATE del Paso 1, 2 = outbox)
}

// Exec cuenta las escrituras: la primera es el UPDATE de la fila, la segunda es
// el INSERT en outbox. Falla en la que corresponda al punto configurado.
func (t *faultyTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execVist++
	if t.punto == FallaUpdate && t.execVist == 1 {
		return pgconn.CommandTag{}, ErrFallaInyectada
	}
	if t.punto == FallaOutbox && t.execVist == 2 {
		return pgconn.CommandTag{}, ErrFallaInyectada
	}
	return t.Tx.Exec(ctx, sql, args...)
}

// QueryRow cuenta como una escritura cuando el Paso 1 usa UPDATE … RETURNING
// (que en pgx se ejecuta vía QueryRow). Falla si el punto es FallaUpdate.
func (t *faultyTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.execVist++
	if t.punto == FallaUpdate && t.execVist == 1 {
		return filaConError{err: ErrFallaInyectada}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

// Commit falla si el punto configurado es FallaCommit; si no, confirma de verdad.
func (t *faultyTx) Commit(ctx context.Context) error {
	if t.punto == FallaCommit {
		// Abortar de verdad para no dejar efectos: hacemos rollback real.
		_ = t.Tx.Rollback(ctx)
		return ErrFallaInyectada
	}
	return t.Tx.Commit(ctx)
}

// filaConError implementa pgx.Row devolviendo siempre el error inyectado.
type filaConError struct{ err error }

func (f filaConError) Scan(dest ...any) error { return f.err }
