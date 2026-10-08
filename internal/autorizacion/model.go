package autorizacion

import (
	"context"
	"errors"
)

// Sujeto correlaciona la identidad externa del IdP con el sujeto interno de
// AI27 y su tenant. identidad no guarda credenciales.
type Sujeto struct {
	ID         string
	IdpSubject string
	TenantID   string
	Activo     bool
}

// ModeloVigente representa lo que el modelo de autorización afirma hoy sobre
// un sujeto. No incluye alcance porque el alcance se resuelve en servidor.
type ModeloVigente struct {
	SujetoID string
	TenantID string
	Rol      Rol
}

// SujetoRepo persiste y consulta sujetos. Las implementaciones deben devolver
// ErrSujetoDuplicado cuando se viola una clave de unicidad del sujeto.
type SujetoRepo interface {
	Crear(ctx context.Context, s Sujeto) (Sujeto, error)
	PorIdpSubject(ctx context.Context, idpSubject string) (Sujeto, bool, error)
}

// AsignacionRolRepo persiste y consulta asignaciones de rol activas.
type AsignacionRolRepo interface {
	Asignar(ctx context.Context, a AsignacionRol) (AsignacionRol, error)
	ActivasDe(ctx context.Context, sujetoID string) ([]AsignacionRol, error)
}

// CargadorModeloVigente construye el modelo vigente de un sujeto para
// componer sus claims. Rechaza una cardinalidad distinta de un rol activo.
type CargadorModeloVigente interface {
	ModeloVigenteDe(ctx context.Context, sujetoID string) (ModeloVigente, error)
}

var (
	// ErrSujetoDuplicado indica una violación de la unicidad de un sujeto, como
	// idp_subject o el constraint compuesto que se confirme para sujeto.
	ErrSujetoDuplicado = errors.New("sujeto duplicado")

	// ErrAsignacionDuplicada indica una violación de la unicidad de una
	// asignación de rol activa para un sujeto.
	ErrAsignacionDuplicada = errors.New("asignacion de rol duplicada")

	// ErrRolFueraDeCatalogo indica el rechazo de un rol por el CHECK o la FK
	// que limita las asignaciones al catálogo vigente de roles.
	ErrRolFueraDeCatalogo = errors.New("rol fuera de catalogo")

	// ErrRolNoUnico indica que el modelo vigente tiene cero o múltiples roles
	// activos cuando se exige exactamente una asignación.
	ErrRolNoUnico = errors.New("rol no unico")
)
