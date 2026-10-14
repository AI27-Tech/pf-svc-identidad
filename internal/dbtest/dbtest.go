// Package dbtest ofrece utilidades para las pruebas de integración de identidad
// que requieren Postgres. Replica la forma de pf-svc-operacion/internal/dbtest y
// pf-svc-catalogos/internal/dbtest (el dbtest de pf-pkg-platform-go es internal y
// no se puede importar), acotada a lo que identidad necesita: aplicar sus
// migraciones y abrir pools por rol para comprobar los GRANT de E0-T5.
//
// Toda prueba que lo usa se salta (t.Skip) cuando IDENTIDAD_TEST_DB no está
// definido, de modo que `go test ./...` sigue en verde en CI sin Postgres.
//
// IDENTIDAD_TEST_DB debe ser un DSN de una base EFÍMERA con privilegios para
// CREATE ROLE y CREATE SCHEMA (superusuario o equivalente): las pruebas crean y
// eliminan el esquema identidad y el rol svc_identidad.
//
// Ejemplo:
//
//	IDENTIDAD_TEST_DB="postgres://postgres@localhost:5432/postgres" go test ./...
package dbtest

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvDSN es la variable de entorno que activa las pruebas de integración de BD.
const EnvDSN = "IDENTIDAD_TEST_DB"

// DSN devuelve el DSN de administración, o salta la prueba si no está definido.
func DSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(EnvDSN)
	if dsn == "" {
		t.Skipf("se salta la prueba de integración: %s no está definido", EnvDSN)
	}
	return dsn
}

// AdminConn abre una conexión de administración para crear o eliminar roles y
// esquemas. El llamador la cierra con la función devuelta.
func AdminConn(t *testing.T, ctx context.Context) (*pgx.Conn, func()) {
	return AdminConnDSN(t, ctx, DSN(t))
}

// AdminConnDSN es como AdminConn pero con el DSN explícito, para pruebas que
// levantan su propio Postgres efímero (no dependen de IDENTIDAD_TEST_DB).
func AdminConnDSN(t *testing.T, ctx context.Context, dsn string) (*pgx.Conn, func()) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("dbtest: conectar como admin: %v", err)
	}
	return conn, func() { _ = conn.Close(ctx) }
}

// Exec ejecuta una sentencia con la conexión de administración y falla la prueba
// si hay error. Útil para setup y teardown.
func Exec(t *testing.T, ctx context.Context, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("dbtest: exec %q: %v", sql, err)
	}
}

// PoolAs abre un pool que asume el rol dado en cada conexión (SET ROLE), de modo
// que los privilegios efectivos sean los del rol y no los del admin que lo abrió.
// searchPath, si no está vacío, se fija como en producción.
func PoolAs(t *testing.T, ctx context.Context, role, searchPath string) (*pgxpool.Pool, func()) {
	return PoolAsDSN(t, ctx, DSN(t), role, searchPath)
}

// PoolAsDSN es como PoolAs pero recibe el DSN explícitamente, en vez de leerlo
// del entorno. Lo usan las pruebas que levantan su propio Postgres efímero (p.
// ej. la suite de integración contra Keycloak, que arranca Postgres y Keycloak
// en contenedores y no depende de IDENTIDAD_TEST_DB).
func PoolAsDSN(t *testing.T, ctx context.Context, dsn, role, searchPath string) (*pgxpool.Pool, func()) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("dbtest: parsear DSN: %v", err)
	}
	if searchPath != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("dbtest: crear pool como %s: %v", role, err)
	}
	return pool, pool.Close
}
