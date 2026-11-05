package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier es la superficie de Postgres que los repositorios de gobierno usan.
// La cumplen *pgx.Conn (pruebas) y *pgxpool.Pool (módulo): "accept interfaces".
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TenantRepo lee la tabla `tenant` del esquema identidad (migración 0006). El
// alta de un sujeto valida que su tenant exista antes de crear al sujeto; el
// gobierno de los tenants de cliente (su alta) es una superficie aparte que
// este repo no cubre todavía.
type TenantRepo struct {
	db querier
}

// NewTenantRepo crea un repositorio de tenants sobre un querier (conexión en
// pruebas, pool del módulo en producción).
func NewTenantRepo(db querier) *TenantRepo {
	return &TenantRepo{db: db}
}

// Existe indica si un tenant está registrado. Lo usa el alta para no crear un
// sujeto con un tenant inexistente (la FK sujeto_tenant_fk lo garantiza en BD,
// pero comprobarlo antes da un error de dominio claro en vez de un fallo de FK).
func (r *TenantRepo) Existe(ctx context.Context, tenantID string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM identidad.tenant WHERE id = $1)`
	var existe bool
	if err := r.db.QueryRow(ctx, query, tenantID).Scan(&existe); err != nil {
		return false, fmt.Errorf("consultar tenant: %w", err)
	}
	return existe, nil
}
