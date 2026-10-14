package autorizacion

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
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
	// Crear persiste un nuevo sujeto DENTRO de la transacción del cambio (el
	// alta de gobierno compone Crear + AsignarInicial + traza + outbox en un
	// solo commit, Requisito 6 de E4-T6). Recibe la `tx` por eso.
	Crear(ctx context.Context, tx pgx.Tx, s Sujeto) (Sujeto, error)
	PorIdpSubject(ctx context.Context, idpSubject string) (Sujeto, bool, error)
	// PorID consulta un sujeto por su identificador interno de AI27.
	PorID(ctx context.Context, id string) (Sujeto, bool, error)
	// FijarActivo cambia el estado activo del sujeto dentro de la transacción
	// del cambio administrativo (desactivación/reactivación de E4-T6).
	FijarActivo(ctx context.Context, tx pgx.Tx, id string, activo bool) error
}

// AsignacionRolRepo persiste y consulta asignaciones de rol. El gobierno de
// E4-T6 compone el reemplazo y la asignación inicial dentro de la transacción
// del cambio (reciben la `tx`); ActivasDe es lectura fuera de transacción.
type AsignacionRolRepo interface {
	ActivasDe(ctx context.Context, sujetoID string) ([]AsignacionRol, error)
	// AsignarInicial crea la asignación de un sujeto que no tiene rol activo.
	// Mapea la violación del índice de un solo rol activo (0005) a
	// ErrSujetoYaTieneRol del paquete gobierno, vía ErrAsignacionActivaExiste.
	AsignarInicial(ctx context.Context, tx pgx.Tx, a AsignacionRol) error
	// Reemplazar revoca el rol activo y asigna el nuevo en la misma transacción
	// (C2/G-5). Es la única vía de cambio de rol.
	Reemplazar(ctx context.Context, tx pgx.Tx, in Reemplazo) error
	// ContarActivosConRol cuenta los sujetos con una asignación activa de un rol.
	ContarActivosConRol(ctx context.Context, rol Rol) (int, error)
}

// Reemplazo es la entrada del reemplazo de rol: revoca el rol activo del sujeto
// y asigna RolNuevo, atribuyendo ambos cambios al Actor.
type Reemplazo struct {
	SujetoID string
	RolNuevo Rol
	ActorID  string
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

	// ErrSujetoNoEncontrado indica que una operación referenció un sujeto que no
	// existe (p. ej. FijarActivo sobre un id inexistente).
	ErrSujetoNoEncontrado = errors.New("sujeto no encontrado")

	// ErrAsignacionActivaExiste indica que AsignarInicial chocó con el índice
	// de un solo rol activo (0005): el sujeto ya tiene un rol activo. El
	// servicio de gobierno lo traduce a ErrSujetoYaTieneRol.
	ErrAsignacionActivaExiste = errors.New("ya existe una asignacion de rol activa")
)
