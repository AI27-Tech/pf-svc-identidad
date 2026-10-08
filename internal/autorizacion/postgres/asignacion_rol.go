package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// AsignacionRolRepo implementa autorizacion.AsignacionRolRepo sobre Postgres.
// Mapea violaciones de constraint a errores de dominio.
type AsignacionRolRepo struct {
	db *pgx.Conn
}

// NewAsignacionRolRepo crea un repositorio de asignaciones sobre una conexión Postgres.
func NewAsignacionRolRepo(db *pgx.Conn) *AsignacionRolRepo {
	return &AsignacionRolRepo{db: db}
}

// Asignar persiste una asignación de rol, con validación en código de
// (sujeto_id, rol_codigo) duplicados mientras no exista constraint en BD.
// Mapea violaciones de FK/CHECK a ErrRolFueraDeCatalogo.
func (r *AsignacionRolRepo) Asignar(ctx context.Context, a autorizacion.AsignacionRol) (autorizacion.AsignacionRol, error) {
	// Validación en código (Decisión B): antes de insertar, verificar que no
	// existe otra asignación activa del mismo rol al mismo sujeto.
	existentes, err := r.ActivasDe(ctx, a.SujetoID)
	if err != nil {
		return autorizacion.AsignacionRol{}, fmt.Errorf("asignar rol: %w", err)
	}
	for _, existe := range existentes {
		if existe.Rol == a.Rol {
			return autorizacion.AsignacionRol{}, fmt.Errorf("asignar rol: %w", autorizacion.ErrAsignacionDuplicada)
		}
	}

	const query = `
		INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por)
		VALUES ($1, $2, $3)
		RETURNING sujeto_id, rol_codigo
	`

	var asignado autorizacion.AsignacionRol
	err = r.db.QueryRow(ctx, query, a.SujetoID, a.Rol, a.SujetoID).
		Scan(&asignado.SujetoID, &asignado.Rol)

	if err == nil {
		asignado.TenantID = a.TenantID
		asignado.Dimension = a.Dimension
		return asignado, nil
	}

	// Mapear errores de constraint: FK de rol_codigo o CHECK del catálogo.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23503" || pgErr.Code == "23514" { // foreign_key_violation o check_violation
			return autorizacion.AsignacionRol{}, fmt.Errorf("asignar rol: %w", autorizacion.ErrRolFueraDeCatalogo)
		}
	}

	return autorizacion.AsignacionRol{}, fmt.Errorf("asignar rol: %w", err)
}

// ActivasDe retorna todas las asignaciones de rol activas (revocado_en IS NULL)
// de un sujeto.
func (r *AsignacionRolRepo) ActivasDe(ctx context.Context, sujetoID string) ([]autorizacion.AsignacionRol, error) {
	const query = `
		SELECT sujeto_id, rol_codigo
		FROM identidad.asignacion_rol
		WHERE sujeto_id = $1 AND revocado_en IS NULL
	`

	rows, err := r.db.Query(ctx, query, sujetoID)
	if err != nil {
		return nil, fmt.Errorf("obtener asignaciones activas: %w", err)
	}
	defer rows.Close()

	var asignaciones []autorizacion.AsignacionRol
	for rows.Next() {
		var a autorizacion.AsignacionRol
		err := rows.Scan(&a.SujetoID, &a.Rol)
		if err != nil {
			return nil, fmt.Errorf("escanear asignación activa: %w", err)
		}
		asignaciones = append(asignaciones, a)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("obtener asignaciones activas: %w", err)
	}

	return asignaciones, nil
}

var _ autorizacion.AsignacionRolRepo = (*AsignacionRolRepo)(nil)
