package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
)

// querier es la superficie de Postgres que los repositorios de gobierno usan.
// La cumplen *pgx.Conn (pruebas) y *pgxpool.Pool (módulo): "accept interfaces".
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TenantRepo lee y escribe la tabla `tenant` del esquema identidad (0006,
// 0009): alta idempotente del tenant de una organización cliente, listado con
// su conteo de usuarios activos y desactivación (DEC-144).
type TenantRepo struct {
	db querier
}

// NewTenantRepo crea un repositorio de tenants sobre un querier (conexión en
// pruebas, pool del módulo en producción).
func NewTenantRepo(db querier) *TenantRepo {
	return &TenantRepo{db: db}
}

var _ gobierno.TenantRepo = (*TenantRepo)(nil)

// columnasTenant proyecta un tenant con sus sujetos activos (B-3).
const columnasTenant = `t.id, t.nombre, t.tipo, t.activa,
	(SELECT count(*) FROM identidad.sujeto s WHERE s.tenant_id = t.id AND s.activo)`

func escanearTenant(row pgx.Row) (gobierno.Tenant, error) {
	var t gobierno.Tenant
	err := row.Scan(&t.ID, &t.Nombre, &t.Tipo, &t.Activa, &t.UsuariosActivos)
	return t, err
}

// CrearCliente inserta un tenant de tipo `cliente`, activo, con el id dado.
// Si el id ya existe no modifica nada y devuelve creado=false (DEC-144 #2).
func (r *TenantRepo) CrearCliente(ctx context.Context, id, nombre string) (creado bool, err error) {
	ct, err := r.db.Exec(ctx,
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1, $2, 'cliente') ON CONFLICT (id) DO NOTHING`,
		id, nombre)
	if err != nil {
		return false, fmt.Errorf("crear tenant: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// PorID lee un tenant con su conteo de usuarios activos.
func (r *TenantRepo) PorID(ctx context.Context, id string) (gobierno.Tenant, bool, error) {
	t, err := escanearTenant(r.db.QueryRow(ctx,
		`SELECT `+columnasTenant+` FROM identidad.tenant t WHERE t.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return gobierno.Tenant{}, false, nil
	}
	if err != nil {
		return gobierno.Tenant{}, false, fmt.Errorf("leer tenant: %w", err)
	}
	return t, true, nil
}

// tamanoPaginaTenants es el máximo de tenants por página del listado.
const tamanoPaginaTenants = 50

// Listar devuelve una página de tenants ordenada por id, con cursor opaco (el
// id de la última fila en base64, como el del historial). Un cursor ilegible es
// un error de validación (400).
func (r *TenantRepo) Listar(ctx context.Context, cursor string) ([]gobierno.Tenant, *string, error) {
	var desde *string
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, nil, platformerrors.NewValidacion("cursor inválido")
		}
		id, err := uuid.Parse(string(raw))
		if err != nil {
			return nil, nil, platformerrors.NewValidacion("cursor inválido")
		}
		s := id.String()
		desde = &s
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+columnasTenant+` FROM identidad.tenant t
		  WHERE ($1::uuid IS NULL OR t.id > $1)
		  ORDER BY t.id LIMIT $2`, desde, tamanoPaginaTenants+1)
	if err != nil {
		return nil, nil, fmt.Errorf("listar tenants: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (gobierno.Tenant, error) {
		return escanearTenant(row)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("listar tenants: %w", err)
	}
	var siguiente *string
	if len(items) > tamanoPaginaTenants {
		items = items[:tamanoPaginaTenants]
		cur := base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].ID))
		siguiente = &cur
	}
	return items, siguiente, nil
}

// DesactivarCliente marca inactivo un tenant de tipo `cliente`. Desactivar uno
// ya inactivo no cambia nada (idempotente). El tenant interno nunca se toca.
func (r *TenantRepo) DesactivarCliente(ctx context.Context, id string) error {
	if _, err := r.db.Exec(ctx,
		`UPDATE identidad.tenant SET activa = false WHERE id = $1 AND tipo = 'cliente'`, id); err != nil {
		return fmt.Errorf("desactivar tenant: %w", err)
	}
	return nil
}
