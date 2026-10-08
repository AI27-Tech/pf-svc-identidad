package postgres

import (
	"context"
	"fmt"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// CargadorModeloVigente construye el ModeloVigente a partir de un sujeto y
// sus asignaciones de rol activas. Valida que exista exactamente una
// asignación activa; rechaza cero o múltiples con ErrRolNoUnico.
type CargadorModeloVigente struct {
	sujetoRepo     autorizacion.SujetoRepo
	asignacionRepo autorizacion.AsignacionRolRepo
}

// NewCargadorModeloVigente crea un cargador de modelo vigente.
func NewCargadorModeloVigente(sujetoRepo autorizacion.SujetoRepo, asignacionRepo autorizacion.AsignacionRolRepo) *CargadorModeloVigente {
	return &CargadorModeloVigente{
		sujetoRepo:     sujetoRepo,
		asignacionRepo: asignacionRepo,
	}
}

// ModeloVigenteDe construye el modelo de autorización vigente de un sujeto.
// Retorna ErrRolNoUnico si el sujeto tiene cero o múltiples roles activos.
func (c *CargadorModeloVigente) ModeloVigenteDe(ctx context.Context, sujetoID string) (autorizacion.ModeloVigente, error) {
	asignaciones, err := c.asignacionRepo.ActivasDe(ctx, sujetoID)
	if err != nil {
		return autorizacion.ModeloVigente{}, fmt.Errorf("cargar modelo vigente: %w", err)
	}

	// Validar exactamente un rol activo (Decisión B implícita en diseño).
	if len(asignaciones) != 1 {
		return autorizacion.ModeloVigente{}, fmt.Errorf("cargar modelo vigente: %w", autorizacion.ErrRolNoUnico)
	}

	asignacion := asignaciones[0]

	// Construir el modelo vigente.
	modelo := autorizacion.ModeloVigente{
		SujetoID: asignacion.SujetoID,
		TenantID: asignacion.TenantID,
		Rol:      asignacion.Rol,
	}

	return modelo, nil
}

var _ autorizacion.CargadorModeloVigente = (*CargadorModeloVigente)(nil)
