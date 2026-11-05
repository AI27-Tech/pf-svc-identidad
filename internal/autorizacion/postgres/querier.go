package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier es la superficie de Postgres que los repositorios de autorización
// usan. La cumplen tanto *pgx.Conn (una conexión, usada en las pruebas) como
// *pgxpool.Pool (el pool del módulo, usado en producción): "accept interfaces,
// return structs" (golang-best-practices §6). Así los repos se construyen con
// el pool del módulo sin acoplarse a una conexión única.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
