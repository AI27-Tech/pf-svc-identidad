package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

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
