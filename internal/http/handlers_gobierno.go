// handlers_gobierno.go monta los handlers de las seis operaciones de GOBIERNO
// del contrato (identidad.yaml, tag `gobierno`, tracer.11), usando los tipos
// GENERADOS de api-client-go/identidad (regla 6: no se escriben tipos de wire a
// mano).
//
// Las seis operaciones van PROTEGIDAS y restringidas a `administrador` (G-10):
// altaSujeto, obtenerSujeto, desactivarSujeto, reactivarSujeto,
// listarHistorialSujeto y fijarRolSujeto. El actor NO viaja en el cuerpo: se
// resuelve en servidor desde los claims verificados (DEC-072, sub ≠ sujeto_id),
// correlacionando el `sub` del token con el `sujeto_id` por `idp_subject`.
package http

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
)

// parseMarcaTiempo convierte la marca de tiempo de dominio (RFC3339, la fija la
// base de datos al insertar la traza) al time.Time que espera el contrato.
func parseMarcaTiempo(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// rolAdministrador es el rol que habilita las operaciones de gobierno (G-10).
const rolAdministrador = autorizacion.RolAdministrador

// ActorResolver correlaciona el `sub` opaco del token con el sujeto interno de
// AI27 (DEC-072). El handler lo usa para resolver el ActorID a partir de los
// claims; es la parte de autorizacion.SujetoRepo que el handler necesita.
type ActorResolver interface {
	PorIdpSubject(ctx context.Context, idpSubject string) (autorizacion.Sujeto, bool, error)
}

// GobiernoHandlers agrupa los handlers HTTP de gobierno sobre el servicio de
// escritura (Gobierno), el de lectura (Consulta) y el resolver del actor.
type GobiernoHandlers struct {
	gob      gobierno.Gobierno
	consulta gobierno.Consulta
	tenants  gobierno.Tenants
	actores  ActorResolver
}

// NewGobiernoHandlers crea los handlers de gobierno con sus colaboradores.
func NewGobiernoHandlers(gob gobierno.Gobierno, consulta gobierno.Consulta, tenants gobierno.Tenants, actores ActorResolver) *GobiernoHandlers {
	return &GobiernoHandlers{gob: gob, consulta: consulta, tenants: tenants, actores: actores}
}

// Actor es quien invoca una operación de gobierno, resuelto en servidor desde
// los claims verificados del contexto (DEC-072): su `sujeto_id` y su rol.
type Actor struct {
	ID  string
	Rol autorizacion.Rol
}

// EsAdmin indica si el actor tiene el rol administrador (G-10).
func (a Actor) EsAdmin() bool { return a.Rol == rolAdministrador }

// ActorDe resuelve el actor desde los claims que el middleware de auth dejó en
// `ctx`, correlacionando el `sub` con el sujeto por `idp_subject`. Sin claims,
// o con un `sub` que no es sujeto de AI27, es no autorizado (401). Lo usan los
// handlers HTTP y la fachada en proceso, así que los dos resuelven igual.
func ActorDe(ctx context.Context, actores ActorResolver) (Actor, error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil {
		return Actor{}, platformerrors.NewNoAutorizado("sin claims en la petición")
	}
	suj, ok, err := actores.PorIdpSubject(ctx, claims.Subject)
	if err != nil {
		return Actor{}, err
	}
	if !ok {
		// El token es válido pero su `sub` no corresponde a un sujeto de AI27.
		return Actor{}, platformerrors.NewNoAutorizado("el sujeto del token no existe")
	}
	return Actor{ID: suj.ID, Rol: autorizacion.Rol(claims.Rol)}, nil
}

// actor es ActorDe en la forma que usan los handlers.
func (h *GobiernoHandlers) actor(ctx context.Context) (actorID string, esAdmin bool, err error) {
	a, err := ActorDe(ctx, h.actores)
	return a.ID, a.EsAdmin(), err
}

// AltaSujeto maneja POST /api/v1/sujetos (protegida, administrador).
func (h *GobiernoHandlers) AltaSujeto(w http.ResponseWriter, r *http.Request) {
	actorID, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	var body gen.AltaSujetoEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	creado, err := h.gob.AltaSujeto(r.Context(), gobierno.AltaSujeto{
		Nombre:               body.Nombre,
		IdentificadorUsuario: body.IdentificadorUsuario,
		Rol:                  autorizacion.Rol(body.Rol),
		TenantID:             body.TenantId.String(),
		ActorID:              actorID,
		ActorEsAdmin:         esAdmin,
	})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusCreated, SujetoWire(creado))
}

// ObtenerSujeto maneja GET /api/v1/sujetos/{id} (protegida, administrador).
func (h *GobiernoHandlers) ObtenerSujeto(w http.ResponseWriter, r *http.Request) {
	_, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	det, err := h.consulta.SujetoPorID(r.Context(), gobierno.LecturaSujeto{SujetoID: id, ActorEsAdmin: esAdmin})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, SujetoDetalleWire(det))
}

// DesactivarSujeto maneja DELETE /api/v1/sujetos/{id} (protegida, administrador).
func (h *GobiernoHandlers) DesactivarSujeto(w http.ResponseWriter, r *http.Request) {
	actorID, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	if err := h.gob.DesactivarSujeto(r.Context(), gobierno.CambioEstado{
		SujetoID:     id,
		ActorID:      actorID,
		ActorEsAdmin: esAdmin,
	}); err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EditarSujeto maneja PATCH /api/v1/sujetos/{id} (protegida, administrador).
// El cuerpo solo admite `nombre`: un `identificador_usuario` es campo
// desconocido y responde 400 (DEC-139 #5).
func (h *GobiernoHandlers) EditarSujeto(w http.ResponseWriter, r *http.Request) {
	actorID, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	var body gen.EdicionSujetoEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	det, err := h.gob.EditarSujeto(r.Context(), gobierno.EdicionSujeto{
		SujetoID: id, Nombre: body.Nombre, ActorID: actorID, ActorEsAdmin: esAdmin,
	})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, SujetoDetalleWire(det))
}

// RevocarSesionesSujeto maneja POST /api/v1/sujetos/{id}/revocaciones
// (protegida, administrador). El motivo es obligatorio (DEC-139 #1).
func (h *GobiernoHandlers) RevocarSesionesSujeto(w http.ResponseWriter, r *http.Request) {
	actorID, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	var body gen.RevocacionSesionesEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	n, err := h.gob.RevocarSesionesSujeto(r.Context(), gobierno.RevocacionSesiones{
		SujetoID: id, Motivo: body.Motivo, ActorID: actorID, ActorEsAdmin: esAdmin,
	})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, gen.RevocacionSesionesResultado{SesionesRevocadas: n})
}

// ReactivarSujeto maneja POST /api/v1/sujetos/{id}/reactivacion (protegida).
func (h *GobiernoHandlers) ReactivarSujeto(w http.ResponseWriter, r *http.Request) {
	actorID, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	if err := h.gob.ReactivarSujeto(r.Context(), gobierno.CambioEstado{
		SujetoID:     id,
		ActorID:      actorID,
		ActorEsAdmin: esAdmin,
	}); err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListarHistorialSujeto maneja GET /api/v1/sujetos/{id}/historial (protegida).
func (h *GobiernoHandlers) ListarHistorialSujeto(w http.ResponseWriter, r *http.Request) {
	_, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	lectura := gobierno.LecturaSujeto{SujetoID: id, ActorEsAdmin: esAdmin}
	pagina, err := h.consulta.HistorialDe(r.Context(), lectura, r.URL.Query().Get("cursor"))
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, aPaginaHistorial(pagina))
}

// ListarSujetos maneja GET /api/v1/sujetos (protegida). Lee el administrador;
// el jefe_operativo solo con rol=monitorista (la regla vive en el servicio).
func (h *GobiernoHandlers) ListarSujetos(w http.ResponseWriter, r *http.Request) {
	actor, err := ActorDe(r.Context(), h.actores)
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	q := r.URL.Query()
	filtro := autorizacion.FiltroSujetos{Q: q.Get("q"), Rol: autorizacion.Rol(q.Get("rol"))}
	if v := q.Get("tenant_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			escribirError(w, platformerrors.NewValidacion("tenant_id inválido"))
			return
		}
		filtro.TenantID = id.String()
	}
	if v := q.Get("activo"); v != "" {
		activo, err := strconv.ParseBool(v)
		if err != nil {
			escribirError(w, platformerrors.NewValidacion("activo inválido"))
			return
		}
		filtro.Activo = &activo
	}
	pagina, err := h.consulta.ListarSujetos(r.Context(), gobierno.ListadoSujetos{
		Filtro: filtro, Cursor: q.Get("cursor"), ActorRol: actor.Rol,
	})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, PaginaSujetosWire(pagina))
}

// FijarRolSujeto maneja PUT /api/v1/sujetos/{id}/rol (protegida). Es la única
// vía para fijar el rol: si el sujeto no tiene rol activo lo asigna; si ya
// tiene, lo reemplaza (C2). El servicio distingue ambos casos por su centinela.
func (h *GobiernoHandlers) FijarRolSujeto(w http.ResponseWriter, r *http.Request) {
	actorID, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	var body gen.AsignacionRolEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	err = h.gob.FijarRol(r.Context(), gobierno.ReemplazoRol{
		SujetoID:     id,
		RolNuevo:     autorizacion.Rol(body.Rol),
		ActorID:      actorID,
		ActorEsAdmin: esAdmin,
	})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CrearTenant maneja PUT /api/v1/tenants/{id} (protegida, administrador): 201
// si lo creó, 200 con el guardado si ya existía (idempotente por id, DEC-144).
func (h *GobiernoHandlers) CrearTenant(w http.ResponseWriter, r *http.Request) {
	_, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de tenant inválido"))
		return
	}
	var body gen.TenantEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	t, creado, err := h.tenants.CrearTenant(r.Context(), gobierno.AltaTenant{ID: id, Nombre: body.Nombre, ActorEsAdmin: esAdmin})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	status := http.StatusOK
	if creado {
		status = http.StatusCreated
	}
	escribirJSON(w, status, TenantWire(t))
}

// ListarTenants maneja GET /api/v1/tenants (protegida, administrador).
func (h *GobiernoHandlers) ListarTenants(w http.ResponseWriter, r *http.Request) {
	_, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	p, err := h.tenants.ListarTenants(r.Context(), gobierno.ListadoTenants{Cursor: r.URL.Query().Get("cursor"), ActorEsAdmin: esAdmin})
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, PaginaTenantsWire(p))
}

// DesactivarTenant maneja DELETE /api/v1/tenants/{id} (protegida,
// administrador). Terminal e idempotente: uno ya inactivo también es 204.
func (h *GobiernoHandlers) DesactivarTenant(w http.ResponseWriter, r *http.Request) {
	_, esAdmin, err := h.actor(r.Context())
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de tenant inválido"))
		return
	}
	if err := h.tenants.DesactivarTenant(r.Context(), gobierno.CambioTenant{ID: id, ActorEsAdmin: esAdmin}); err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Mapeo dominio → tipos del contrato ------------------------------------
//
// SujetoWire y compañía se exportan porque la fachada en proceso (Gobierno())
// devuelve los mismos tipos del contrato: una sola traducción para las dos
// fronteras.

// SujetoWire traduce un sujeto de dominio al Sujeto del contrato.
func SujetoWire(s autorizacion.Sujeto) gen.Sujeto {
	out := gen.Sujeto{
		Id:       mustUUID(s.ID),
		TenantId: mustUUID(s.TenantID),
		Activo:   s.Activo,
	}
	if s.IdpSubject != "" {
		sub := s.IdpSubject
		out.IdpSubject = &sub
	}
	if s.Nombre != "" {
		nombre := s.Nombre
		out.Nombre = &nombre
	}
	if s.IdentificadorUsuario != "" {
		ident := s.IdentificadorUsuario
		out.IdentificadorUsuario = &ident
	}
	return out
}

// SujetoDetalleWire traduce el detalle de dominio al SujetoDetalle del contrato.
func SujetoDetalleWire(d gobierno.SujetoDetalle) gen.SujetoDetalle {
	out := gen.SujetoDetalle{
		Sujeto:   SujetoWire(d.Sujeto),
		TieneRol: d.TieneRol,
	}
	if d.TieneRol {
		rol := gen.Rol(d.RolActivo)
		out.Rol = &rol
	}
	return out
}

// PaginaSujetosWire traduce una página del listado a PaginaSujetos.
func PaginaSujetosWire(p gobierno.PaginaSujetos) gen.PaginaSujetos {
	items := make([]gen.SujetoDetalle, 0, len(p.Sujetos))
	for _, d := range p.Sujetos {
		items = append(items, SujetoDetalleWire(d))
	}
	return gen.PaginaSujetos{Items: items, CursorSiguiente: p.CursorSiguiente}
}

// TenantWire traduce un tenant de dominio al Tenant del contrato.
func TenantWire(t gobierno.Tenant) gen.Tenant {
	return gen.Tenant{
		Id:              mustUUID(t.ID),
		Nombre:          t.Nombre,
		Tipo:            gen.TenantTipo(t.Tipo),
		Activa:          t.Activa,
		UsuariosActivos: t.UsuariosActivos,
	}
}

// PaginaTenantsWire traduce una página de tenants a PaginaTenants.
func PaginaTenantsWire(p gobierno.PaginaTenants) gen.PaginaTenants {
	items := make([]gen.Tenant, 0, len(p.Tenants))
	for _, t := range p.Tenants {
		items = append(items, TenantWire(t))
	}
	return gen.PaginaTenants{Items: items, CursorSiguiente: p.CursorSiguiente}
}

// AlcanceWire traduce el alcance de dominio al AlcanceSujeto del contrato. Las
// tres listas salen como [] aunque estén vacías (el contrato las exige).
func AlcanceWire(a gobierno.Alcance) gen.AlcanceSujeto {
	return gen.AlcanceSujeto{
		UnidadesIds:          idsWire(a.UnidadesIDs),
		SubordinadosIds:      idsWire(a.SubordinadosIDs),
		UnidadesEfectivasIds: idsWire(a.UnidadesEfectivasIDs),
	}
}

func idsWire(ids []string) []gen.Id {
	out := make([]gen.Id, 0, len(ids))
	for _, id := range ids {
		out = append(out, mustUUID(id))
	}
	return out
}

// IDsDeWire traduce una lista de ids del contrato a la forma de dominio.
func IDsDeWire(ids []gen.Id) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func aPaginaHistorial(p gobierno.PaginaHistorial) gen.PaginaHistorial {
	items := make([]gen.CambioAdministrativo, 0, len(p.Cambios))
	for _, c := range p.Cambios {
		item := gen.CambioAdministrativo{
			Tipo:     c.Tipo,
			SujetoId: mustUUID(c.SujetoID),
		}
		if t, err := parseMarcaTiempo(c.OcurridoEn); err == nil {
			item.OcurridoEn = t
		}
		if c.TenantID != "" {
			tid := mustUUID(c.TenantID)
			item.TenantId = &tid
		}
		if c.ActorID != "" {
			aid := mustUUID(c.ActorID)
			item.ActorId = &aid
		}
		items = append(items, item)
	}
	return gen.PaginaHistorial{Items: items, CursorSiguiente: p.CursorSiguiente}
}

// escribirErrorGobierno traduce el error con la tabla única de gobierno
// (gobierno.Classify) a la forma `Error` del contrato, con su `codigo` de
// máquina (403 `autoescalamiento`, 409 `rol_igual`, `ultimo_administrador`…).
func escribirErrorGobierno(w http.ResponseWriter, err error) {
	escribirError(w, gobierno.Classify(err))
}
