package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
)

// AlcanceRepo lee y escribe el alcance de monitoreo (0010): `alcance_unidad` y
// `jerarquia`. SQL plano; las reglas viven en el servicio.
type AlcanceRepo struct {
	db querier
}

// NewAlcanceRepo crea el repositorio de alcance sobre un querier (conexión en
// pruebas, pool del módulo en producción).
func NewAlcanceRepo(db querier) *AlcanceRepo {
	return &AlcanceRepo{db: db}
}

var _ gobierno.AlcanceRepoTx = (*AlcanceRepo)(nil)

// RolActivoEnTx lee el rol activo y bloquea su fila de asignación hasta el
// commit. Un ReemplazarRol concurrente espera en su UPDATE y, al seguir, su
// DELETE del alcance ya ve lo que esta tx escribió (READ COMMITTED).
func (r *AlcanceRepo) RolActivoEnTx(ctx context.Context, tx pgx.Tx, sujetoID string) (autorizacion.Rol, bool, error) {
	var rol string
	err := tx.QueryRow(ctx, `
		SELECT rol_codigo FROM identidad.asignacion_rol
		 WHERE sujeto_id = $1 AND revocado_en IS NULL
		   FOR UPDATE`, sujetoID).Scan(&rol)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("leer rol activo: %w", err)
	}
	return autorizacion.Rol(rol), true, nil
}

// CandidatosEnTx bloquea en modo compartido las asignaciones activas de `ids`
// (un cambio de rol concurrente de un subordinado espera) y luego lee, en otra
// sentencia para ver el estado ya comprometido, su rol y su superior.
func (r *AlcanceRepo) CandidatosEnTx(ctx context.Context, tx pgx.Tx, ids []string) ([]gobierno.CandidatoSubordinado, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `
		SELECT 1 FROM identidad.asignacion_rol
		 WHERE sujeto_id = ANY($1::uuid[]) AND revocado_en IS NULL
		   FOR SHARE`, ids); err != nil {
		return nil, fmt.Errorf("bloquear subordinados: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT s.id::text, coalesce(a.rol_codigo, ''), coalesce(j.superior_id::text, '')
		  FROM identidad.sujeto s
		  LEFT JOIN identidad.asignacion_rol a ON a.sujeto_id = s.id AND a.revocado_en IS NULL
		  LEFT JOIN identidad.jerarquia j ON j.subordinado_id = s.id
		 WHERE s.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("leer subordinados: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (gobierno.CandidatoSubordinado, error) {
		var c gobierno.CandidatoSubordinado
		var rol string
		err := row.Scan(&c.ID, &rol, &c.SuperiorID)
		c.Rol = autorizacion.Rol(rol)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("leer subordinados: %w", err)
	}
	return out, nil
}

// ReemplazarUnidades borra las unidades del Monitorista e inserta el conjunto
// nuevo. Con el mismo conjunto el estado final es el mismo.
func (r *AlcanceRepo) ReemplazarUnidades(ctx context.Context, tx pgx.Tx, monitoristaID string, unidades []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM identidad.alcance_unidad WHERE monitorista_id = $1`, monitoristaID); err != nil {
		return fmt.Errorf("reemplazar unidades: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO identidad.alcance_unidad (monitorista_id, unidad_id)
		SELECT $1, u FROM unnest($2::uuid[]) AS u`, monitoristaID, unidades); err != nil {
		return fmt.Errorf("reemplazar unidades: %w", err)
	}
	return nil
}

// ReemplazarSubordinados borra los subordinados del superior e inserta el
// conjunto nuevo. Si otro superior insertó a uno de ellos en paralelo, la
// unicidad de `subordinado_id` lo rechaza y se traduce al centinela.
func (r *AlcanceRepo) ReemplazarSubordinados(ctx context.Context, tx pgx.Tx, superiorID string, subordinados []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM identidad.jerarquia WHERE superior_id = $1`, superiorID); err != nil {
		return fmt.Errorf("reemplazar subordinados: %w", err)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO identidad.jerarquia (superior_id, subordinado_id)
		SELECT $1, s FROM unnest($2::uuid[]) AS s`, superiorID, subordinados)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "jerarquia_subordinado_unico" {
		return fmt.Errorf("reemplazar subordinados: %w: %w", gobierno.ErrSubordinadoConSuperior, err)
	}
	if err != nil {
		return fmt.Errorf("reemplazar subordinados: %w", err)
	}
	return nil
}
