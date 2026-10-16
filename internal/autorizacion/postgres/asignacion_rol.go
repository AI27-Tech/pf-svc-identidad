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
// Mapea violaciones de constraint a errores de dominio. La unicidad de "un solo
// rol activo por sujeto" la garantiza el índice asignacion_rol_un_activo_idx
// (migración 0005); este repo ya NO valida duplicados en código.
type AsignacionRolRepo struct {
	db Querier
}

// NewAsignacionRolRepo crea un repositorio de asignaciones sobre un Querier
// (una conexión en pruebas, o el pool del módulo en producción).
func NewAsignacionRolRepo(db Querier) *AsignacionRolRepo {
	return &AsignacionRolRepo{db: db}
}

// AsignarInicial crea la asignación de rol de un sujeto que NO tiene rol activo,
// dentro de la transacción del cambio administrativo (atomicidad, Requisito 6).
// `asignado_por` es el ACTOR del cambio, no el propio sujeto (corrección E4-T6).
//
// Mapeo de errores de constraint:
//   - 23505 (índice asignacion_rol_un_activo_idx): el sujeto YA tiene un rol
//     activo → ErrAsignacionActivaExiste (el servicio lo traduce a
//     ErrSujetoYaTieneRol). Esta es la validación que antes se hacía en código.
//   - 23503/23514 (FK o CHECK de rol_codigo): rol fuera del catálogo →
//     ErrRolFueraDeCatalogo.
func (r *AsignacionRolRepo) AsignarInicial(ctx context.Context, tx pgx.Tx, a autorizacion.AsignacionRol) error {
	const query = `
		INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por)
		VALUES ($1, $2, $3)
	`
	_, err := tx.Exec(ctx, query, a.SujetoID, a.Rol, a.AsignadoPor)
	if err == nil {
		return nil
	}
	return mapearErrorAsignacion(err)
}

// Reemplazar revoca el rol activo del sujeto y asigna el nuevo, EN LA MISMA
// transacción (C2/G-5). Es la única vía de cambio de rol. El UPDATE marca la
// activa como revocada por el actor; el INSERT crea la nueva. Si el sujeto no
// tenía rol activo, el UPDATE no afecta filas y se devuelve ErrSujetoSinRol a
// través de autorizacion.ErrRolNoUnico (el servicio lo interpreta). Si el rol
// nuevo coincide con el que se acaba de revocar, el flujo del servicio ya lo
// rechazó antes como ErrAsignacionDuplicada.
func (r *AsignacionRolRepo) Reemplazar(ctx context.Context, tx pgx.Tx, in autorizacion.Reemplazo) error {
	const revocar = `
		UPDATE identidad.asignacion_rol
		SET revocado_por = $2, revocado_en = now()
		WHERE sujeto_id = $1 AND revocado_en IS NULL
	`
	ct, err := tx.Exec(ctx, revocar, in.SujetoID, in.ActorID)
	if err != nil {
		return fmt.Errorf("revocar rol activo: %w", err)
	}
	if ct.RowsAffected() == 0 {
		// No había rol activo que revocar: no es un reemplazo válido.
		return fmt.Errorf("reemplazar rol: %w", autorizacion.ErrRolNoUnico)
	}

	const asignar = `
		INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por)
		VALUES ($1, $2, $3)
	`
	if _, err := tx.Exec(ctx, asignar, in.SujetoID, in.RolNuevo, in.ActorID); err != nil {
		return mapearErrorAsignacion(err)
	}
	return nil
}

// ActivasDe retorna todas las asignaciones de rol activas (revocado_en IS NULL)
// de un sujeto. Con el índice 0005 hay a lo sumo una, pero el método conserva
// su firma de slice para no romper a los consumidores existentes.
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
		if err := rows.Scan(&a.SujetoID, &a.Rol); err != nil {
			return nil, fmt.Errorf("escanear asignación activa: %w", err)
		}
		asignaciones = append(asignaciones, a)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("obtener asignaciones activas: %w", err)
	}
	return asignaciones, nil
}

// ContarActivosConRol cuenta cuántos sujetos tienen una asignación ACTIVA del
// rol dado. Lo usa la guarda del "último administrador" (G-7): antes de que un
// administrador se quite el rol o se desactive, se verifica que no sea el
// último activo.
func (r *AsignacionRolRepo) ContarActivosConRol(ctx context.Context, rol autorizacion.Rol) (int, error) {
	const query = `
		SELECT count(*)
		FROM identidad.asignacion_rol
		WHERE rol_codigo = $1 AND revocado_en IS NULL
	`
	var n int
	if err := r.db.QueryRow(ctx, query, rol).Scan(&n); err != nil {
		return 0, fmt.Errorf("contar activos con rol: %w", err)
	}
	return n, nil
}

// mapearErrorAsignacion traduce los errores de constraint de un INSERT en
// asignacion_rol a los centinelas de dominio.
func mapearErrorAsignacion(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation: índice de un solo rol activo
			return fmt.Errorf("asignar rol: %w", autorizacion.ErrAsignacionActivaExiste)
		case "23503", "23514": // foreign_key_violation o check_violation
			return fmt.Errorf("asignar rol: %w", autorizacion.ErrRolFueraDeCatalogo)
		}
	}
	return fmt.Errorf("asignar rol: %w", err)
}

var _ autorizacion.AsignacionRolRepo = (*AsignacionRolRepo)(nil)
