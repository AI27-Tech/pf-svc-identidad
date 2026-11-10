package gobierno

import (
	"context"
	"fmt"
	"strings"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"
)

// Tenant es el espacio de pertenencia de un sujeto: el interno de AI27 o el de
// una organización cliente, con el id de su `cliente_id` (DEC-112).
type Tenant struct {
	ID     string
	Nombre string
	// Tipo es `interno` o `cliente`.
	Tipo string
	// Activa es false si la organización está desactivada (terminal, DEC-139 #4).
	Activa bool
	// UsuariosActivos cuenta los sujetos activos del tenant (B-3).
	UsuariosActivos int
}

// PaginaTenants es una página del listado con su cursor (nil si no hay más).
type PaginaTenants struct {
	Tenants         []Tenant
	CursorSiguiente *string
}

// tipoInterno es el `tipo` del tenant único de AI27, que no se pisa ni se
// desactiva.
const tipoInterno = "interno"

// TenantRepo es la persistencia de tenants que el servicio usa. Interfaz en el
// consumidor; la cumple gobierno/postgres.TenantRepo.
type TenantRepo interface {
	CrearCliente(ctx context.Context, id, nombre string) (creado bool, err error)
	PorID(ctx context.Context, id string) (Tenant, bool, error)
	Listar(ctx context.Context, cursor string) ([]Tenant, *string, error)
	DesactivarCliente(ctx context.Context, id string) error
}

// Tenants es la superficie de los tenants de organización cliente (tag
// `tenants`, E8-T19, DEC-144). Todo exige administrador. El contrato no
// publica hechos de tenant, así que no escribe traza ni outbox.
type Tenants interface {
	// CrearTenant crea el tenant `cliente` con el id dado, idempotente por id:
	// si ya existe uno `cliente` lo devuelve sin cambios y creado=false. El id
	// del tenant interno es ErrTenantInterno.
	CrearTenant(ctx context.Context, in AltaTenant) (t Tenant, creado bool, err error)
	// ListarTenants pagina los tenants, internos y de cliente.
	ListarTenants(ctx context.Context, in ListadoTenants) (PaginaTenants, error)
	// DesactivarTenant marca inactivo el tenant (terminal e idempotente). Sus
	// sujetos no pasan la siguiente emisión ni el siguiente refresco.
	DesactivarTenant(ctx context.Context, in CambioTenant) error
}

// AltaTenant es la entrada de CrearTenant.
type AltaTenant struct {
	ID           string
	Nombre       string
	ActorEsAdmin bool
}

// ListadoTenants es la entrada de ListarTenants.
type ListadoTenants struct {
	Cursor       string
	ActorEsAdmin bool
}

// CambioTenant es la entrada de DesactivarTenant.
type CambioTenant struct {
	ID           string
	ActorEsAdmin bool
}

type servicioTenants struct {
	repo TenantRepo
}

// NewTenants construye el servicio de tenants sobre su repositorio.
func NewTenants(repo TenantRepo) Tenants {
	return &servicioTenants{repo: repo}
}

func (s *servicioTenants) CrearTenant(ctx context.Context, in AltaTenant) (Tenant, bool, error) {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return Tenant{}, false, fmt.Errorf("crear tenant: %w", err)
	}
	if strings.TrimSpace(in.Nombre) == "" {
		return Tenant{}, false, platformerrors.NewValidacion("el nombre del tenant no puede ir vacío")
	}
	creado, err := s.repo.CrearCliente(ctx, in.ID, in.Nombre)
	if err != nil {
		return Tenant{}, false, err
	}
	t, ok, err := s.repo.PorID(ctx, in.ID)
	if err != nil {
		return Tenant{}, false, err
	}
	if !ok {
		return Tenant{}, false, fmt.Errorf("crear tenant: el tenant %s no quedó registrado", in.ID)
	}
	if t.Tipo == tipoInterno {
		return Tenant{}, false, fmt.Errorf("crear tenant: %w", ErrTenantInterno)
	}
	return t, creado, nil
}

func (s *servicioTenants) ListarTenants(ctx context.Context, in ListadoTenants) (PaginaTenants, error) {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return PaginaTenants{}, fmt.Errorf("listar tenants: %w", err)
	}
	items, cursor, err := s.repo.Listar(ctx, in.Cursor)
	if err != nil {
		return PaginaTenants{}, err
	}
	return PaginaTenants{Tenants: items, CursorSiguiente: cursor}, nil
}

func (s *servicioTenants) DesactivarTenant(ctx context.Context, in CambioTenant) error {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return fmt.Errorf("desactivar tenant: %w", err)
	}
	t, ok, err := s.repo.PorID(ctx, in.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("desactivar tenant: %w", ErrTenantNoEncontrado)
	}
	if t.Tipo == tipoInterno {
		return fmt.Errorf("desactivar tenant: %w", ErrTenantInterno)
	}
	return s.repo.DesactivarCliente(ctx, in.ID)
}
