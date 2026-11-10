package identidad

import (
	"context"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	svchttp "github.com/AI27-Tech/pf-svc-identidad/internal/http"
)

// GobiernoConsultas es la sub-fachada EN PROCESO del gobierno de identidad
// (tags `gobierno` y `tenants` de identidad.yaml, tracer.40) para el `bff`
// co-ubicado en `core`. Cada método corresponde 1:1 con una operación del
// contrato y usa SOLO tipos de api-client-go/identidad.
//
// El actor viaja en el contexto, con el mismo criterio que los handlers HTTP:
// el `ctx` debe traer los claims verificados del token del llamante
// (auth.ContextWithClaims, que deja el middleware de auth del `bff`). De ahí
// se toman el `sub`, que se correlaciona con el `sujeto_id` por `idp_subject`
// (DEC-072), y el `rol`, que decide los permisos (administrador; el
// jefe_operativo lee Monitoristas en ListarSujetos y en ObtenerAlcanceSujeto y
// escribe FijarUnidadesSujeto, DEC-142 #2). Sin claims, o con un `sub` que no
// es sujeto de AI27, la operación responde 401.
//
// Los errores son *PlatformError con el mismo `codigo` que el HTTP (tabla de
// gobierno.Classify): 403 `autoescalamiento`, 409 `rol_igual`,
// `sujeto_desactivado`, `ultimo_administrador`, `rol_cruza_audiencia`,
// `rol_sin_alcance`, `subordinado_rol_invalido`, `subordinado_con_superior`… Si el
// módulo no recibió IDPAdmin, las operaciones que tocan el IdP (alta, editar,
// desactivar, reactivar y fijar rol) responden 503 antes de escribir; las
// lecturas, la revocación de sesiones y los tenants funcionan igual.
type GobiernoConsultas struct {
	gob      gobierno.Gobierno
	consulta gobierno.Consulta
	tenants  gobierno.Tenants
	actores  svchttp.ActorResolver
}

// Gobierno devuelve la sub-fachada de gobierno (arista bff → identidad).
func (c ConsultasAPI) Gobierno() GobiernoConsultas {
	return c.gobierno
}

// actor resuelve el actor desde los claims del contexto. Una fachada sin
// construir (ConsultasAPI cero, fuera de New) responde 503, nunca panic.
func (g GobiernoConsultas) actor(ctx context.Context) (svchttp.Actor, error) {
	if g.actores == nil {
		return svchttp.Actor{}, platformerrors.NewDegradado("identidad: el gobierno en proceso no está construido")
	}
	return svchttp.ActorDe(ctx, g.actores)
}

// ListarSujetos es listarSujetos.
func (g GobiernoConsultas) ListarSujetos(ctx context.Context, params gen.ListarSujetosParams) (*gen.PaginaSujetos, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	in := gobierno.ListadoSujetos{ActorRol: a.Rol, Filtro: autorizacion.FiltroSujetos{Activo: params.Activo}}
	if params.Q != nil {
		in.Filtro.Q = *params.Q
	}
	if params.Rol != nil {
		in.Filtro.Rol = autorizacion.Rol(*params.Rol)
	}
	if params.TenantId != nil {
		in.Filtro.TenantID = params.TenantId.String()
	}
	if params.Cursor != nil {
		in.Cursor = *params.Cursor
	}
	p, err := g.consulta.ListarSujetos(ctx, in)
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	out := svchttp.PaginaSujetosWire(p)
	return &out, nil
}

// AltaSujeto es altaSujeto.
func (g GobiernoConsultas) AltaSujeto(ctx context.Context, body gen.AltaSujetoEntrada) (*gen.Sujeto, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	s, err := g.gob.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre:               body.Nombre,
		IdentificadorUsuario: body.IdentificadorUsuario,
		Rol:                  autorizacion.Rol(body.Rol),
		TenantID:             body.TenantId.String(),
		ActorID:              a.ID,
		ActorEsAdmin:         a.EsAdmin(),
	})
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	out := svchttp.SujetoWire(s)
	return &out, nil
}

// ObtenerSujeto es obtenerSujeto.
func (g GobiernoConsultas) ObtenerSujeto(ctx context.Context, id gen.Id) (*gen.SujetoDetalle, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := g.consulta.SujetoPorID(ctx, gobierno.LecturaSujeto{SujetoID: id.String(), ActorEsAdmin: a.EsAdmin()})
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	out := svchttp.SujetoDetalleWire(d)
	return &out, nil
}

// EditarSujeto es editarSujeto.
func (g GobiernoConsultas) EditarSujeto(ctx context.Context, id gen.Id, body gen.EdicionSujetoEntrada) (*gen.SujetoDetalle, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	d, err := g.gob.EditarSujeto(ctx, gobierno.EdicionSujeto{
		SujetoID: id.String(), Nombre: body.Nombre, ActorID: a.ID, ActorEsAdmin: a.EsAdmin(),
	})
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	out := svchttp.SujetoDetalleWire(d)
	return &out, nil
}

// DesactivarSujeto es desactivarSujeto.
func (g GobiernoConsultas) DesactivarSujeto(ctx context.Context, id gen.Id) error {
	a, err := g.actor(ctx)
	if err != nil {
		return err
	}
	return gobierno.Classify(g.gob.DesactivarSujeto(ctx, gobierno.CambioEstado{
		SujetoID: id.String(), ActorID: a.ID, ActorEsAdmin: a.EsAdmin(),
	}))
}

// ReactivarSujeto es reactivarSujeto.
func (g GobiernoConsultas) ReactivarSujeto(ctx context.Context, id gen.Id) error {
	a, err := g.actor(ctx)
	if err != nil {
		return err
	}
	return gobierno.Classify(g.gob.ReactivarSujeto(ctx, gobierno.CambioEstado{
		SujetoID: id.String(), ActorID: a.ID, ActorEsAdmin: a.EsAdmin(),
	}))
}

// FijarRolSujeto es fijarRolSujeto.
func (g GobiernoConsultas) FijarRolSujeto(ctx context.Context, id gen.Id, body gen.AsignacionRolEntrada) error {
	a, err := g.actor(ctx)
	if err != nil {
		return err
	}
	return gobierno.Classify(g.gob.FijarRol(ctx, gobierno.ReemplazoRol{
		SujetoID: id.String(), RolNuevo: autorizacion.Rol(body.Rol), ActorID: a.ID, ActorEsAdmin: a.EsAdmin(),
	}))
}

// RevocarSesionesSujeto es revocarSesionesSujeto.
func (g GobiernoConsultas) RevocarSesionesSujeto(ctx context.Context, id gen.Id, body gen.RevocacionSesionesEntrada) (*gen.RevocacionSesionesResultado, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	n, err := g.gob.RevocarSesionesSujeto(ctx, gobierno.RevocacionSesiones{
		SujetoID: id.String(), Motivo: body.Motivo, ActorID: a.ID, ActorEsAdmin: a.EsAdmin(),
	})
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	return &gen.RevocacionSesionesResultado{SesionesRevocadas: n}, nil
}

// CrearTenant es crearTenant. creado distingue el 201 (lo creó) del 200 (ya
// existía y se devuelve sin cambios).
func (g GobiernoConsultas) CrearTenant(ctx context.Context, id gen.Id, body gen.TenantEntrada) (t *gen.Tenant, creado bool, err error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, false, err
	}
	tenant, creado, err := g.tenants.CrearTenant(ctx, gobierno.AltaTenant{ID: id.String(), Nombre: body.Nombre, ActorEsAdmin: a.EsAdmin()})
	if err != nil {
		return nil, false, gobierno.Classify(err)
	}
	out := svchttp.TenantWire(tenant)
	return &out, creado, nil
}

// ListarTenants es listarTenants.
func (g GobiernoConsultas) ListarTenants(ctx context.Context, params gen.ListarTenantsParams) (*gen.PaginaTenants, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	in := gobierno.ListadoTenants{ActorEsAdmin: a.EsAdmin()}
	if params.Cursor != nil {
		in.Cursor = *params.Cursor
	}
	p, err := g.tenants.ListarTenants(ctx, in)
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	out := svchttp.PaginaTenantsWire(p)
	return &out, nil
}

// DesactivarTenant es desactivarTenant.
func (g GobiernoConsultas) DesactivarTenant(ctx context.Context, id gen.Id) error {
	a, err := g.actor(ctx)
	if err != nil {
		return err
	}
	return gobierno.Classify(g.tenants.DesactivarTenant(ctx, gobierno.CambioTenant{ID: id.String(), ActorEsAdmin: a.EsAdmin()}))
}

// ObtenerAlcanceSujeto es obtenerAlcanceSujeto.
func (g GobiernoConsultas) ObtenerAlcanceSujeto(ctx context.Context, id gen.Id) (*gen.AlcanceSujeto, error) {
	a, err := g.actor(ctx)
	if err != nil {
		return nil, err
	}
	alc, err := g.consulta.ObtenerAlcanceSujeto(ctx, gobierno.LecturaAlcance{SujetoID: id.String(), ActorRol: a.Rol})
	if err != nil {
		return nil, gobierno.Classify(err)
	}
	out := svchttp.AlcanceWire(alc)
	return &out, nil
}

// FijarUnidadesSujeto es fijarUnidadesSujeto.
func (g GobiernoConsultas) FijarUnidadesSujeto(ctx context.Context, id gen.Id, body gen.UnidadesSujetoEntrada) error {
	a, err := g.actor(ctx)
	if err != nil {
		return err
	}
	return gobierno.Classify(g.gob.FijarUnidadesSujeto(ctx, gobierno.FijacionUnidades{
		SujetoID: id.String(), UnidadesIDs: svchttp.IDsDeWire(body.UnidadesIds), ActorID: a.ID, ActorRol: a.Rol,
	}))
}

// FijarSubordinadosSujeto es fijarSubordinadosSujeto.
func (g GobiernoConsultas) FijarSubordinadosSujeto(ctx context.Context, id gen.Id, body gen.SubordinadosSujetoEntrada) error {
	a, err := g.actor(ctx)
	if err != nil {
		return err
	}
	return gobierno.Classify(g.gob.FijarSubordinadosSujeto(ctx, gobierno.FijacionSubordinados{
		SujetoID: id.String(), SubordinadosIDs: svchttp.IDsDeWire(body.SujetosIds), ActorID: a.ID, ActorRol: a.Rol,
	}))
}
