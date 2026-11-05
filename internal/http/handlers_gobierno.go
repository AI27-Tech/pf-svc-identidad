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
	"errors"
	"net/http"
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
const rolAdministrador = "administrador"

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
	actores  ActorResolver
}

// NewGobiernoHandlers crea los handlers de gobierno con sus colaboradores.
func NewGobiernoHandlers(gob gobierno.Gobierno, consulta gobierno.Consulta, actores ActorResolver) *GobiernoHandlers {
	return &GobiernoHandlers{gob: gob, consulta: consulta, actores: actores}
}

// actor resuelve el actor de la petición desde los claims verificados: su
// `sujeto_id` (correlacionando el `sub`) y si tiene el rol administrador. El
// middleware de auth ya inyectó los claims; si faltan, es un fallo de montaje
// (la ruta debe ir protegida), que se trata como no autorizado.
func (h *GobiernoHandlers) actor(ctx context.Context) (actorID string, esAdmin bool, err error) {
	claims := auth.ClaimsFromContext(ctx)
	if claims == nil {
		return "", false, platformerrors.NewNoAutorizado("sin claims en la petición")
	}
	esAdmin = claims.Rol == rolAdministrador
	suj, ok, err := h.actores.PorIdpSubject(ctx, claims.Subject)
	if err != nil {
		return "", false, err
	}
	if !ok {
		// El token es válido pero su `sub` no corresponde a un sujeto de AI27.
		return "", esAdmin, platformerrors.NewNoAutorizado("el sujeto del token no existe")
	}
	return suj.ID, esAdmin, nil
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
	escribirJSON(w, http.StatusCreated, aSujeto(creado))
}

// ObtenerSujeto maneja GET /api/v1/sujetos/{id} (protegida, administrador).
func (h *GobiernoHandlers) ObtenerSujeto(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.actor(r.Context()); err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	det, err := h.consulta.SujetoPorID(r.Context(), id)
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, aSujetoDetalle(det))
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
	if _, _, err := h.actor(r.Context()); err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sujeto inválido"))
		return
	}
	pagina, err := h.consulta.HistorialDe(r.Context(), id, r.URL.Query().Get("cursor"))
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, aPaginaHistorial(pagina))
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
	rol := autorizacion.Rol(body.Rol)

	// Intentar el reemplazo; si el sujeto no tiene rol activo, asignar inicial.
	err = h.gob.ReemplazarRol(r.Context(), gobierno.ReemplazoRol{
		SujetoID:     id,
		RolNuevo:     rol,
		ActorID:      actorID,
		ActorEsAdmin: esAdmin,
	})
	if errors.Is(err, gobierno.ErrSujetoSinRol) {
		err = h.gob.AsignarRolInicial(r.Context(), gobierno.AsignacionInicial{
			SujetoID:     id,
			Rol:          rol,
			ActorID:      actorID,
			ActorEsAdmin: esAdmin,
		})
	}
	if err != nil {
		escribirErrorGobierno(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Mapeo dominio → tipos del contrato ------------------------------------

func aSujeto(s autorizacion.Sujeto) gen.Sujeto {
	out := gen.Sujeto{
		Id:       mustUUID(s.ID),
		TenantId: mustUUID(s.TenantID),
		Activo:   s.Activo,
	}
	if s.IdpSubject != "" {
		sub := s.IdpSubject
		out.IdpSubject = &sub
	}
	return out
}

func aSujetoDetalle(d gobierno.SujetoDetalle) gen.SujetoDetalle {
	out := gen.SujetoDetalle{
		Sujeto:   aSujeto(d.Sujeto),
		TieneRol: d.TieneRol,
	}
	if d.TieneRol {
		rol := gen.Rol(d.RolActivo)
		out.Rol = &rol
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

// escribirErrorGobierno traduce los centinelas de gobierno a los códigos del
// contrato. La taxonomía de plataforma NO tiene categoría 403 (solo 401), y el
// contrato de gobierno distingue 403 (autenticado pero no autorizado:
// no-administrador y autoescalamiento) de 409 (conflicto de estado). Por eso
// este mapeo es explícito aquí y no delega todo en AsCategory.
func escribirErrorGobierno(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gobierno.ErrNoAutorizado), errors.Is(err, gobierno.ErrAutoescalamiento):
		escribirJSON(w, http.StatusForbidden, gen.Error{Codigo: "prohibido", Mensaje: err.Error()})
	case errors.Is(err, gobierno.ErrUltimoAdministrador),
		errors.Is(err, gobierno.ErrSujetoYaTieneRol),
		errors.Is(err, gobierno.ErrSujetoDesactivado),
		errors.Is(err, autorizacion.ErrAsignacionDuplicada):
		escribirJSON(w, http.StatusConflict, gen.Error{Codigo: "conflicto", Mensaje: err.Error()})
	case errors.Is(err, autorizacion.ErrSujetoNoEncontrado):
		escribirJSON(w, http.StatusNotFound, gen.Error{Codigo: "no_encontrado", Mensaje: err.Error()})
	case errors.Is(err, autorizacion.ErrRolFueraDeCatalogo):
		escribirJSON(w, http.StatusBadRequest, gen.Error{Codigo: "validacion", Mensaje: err.Error()})
	case errors.Is(err, gobierno.ErrIdpSyncPending):
		// El cambio local SÍ ocurrió (local manda, C1). El IdP quedó pendiente
		// de sincronizar; se reporta como 202-equivalente degradado, pero el
		// contrato no modela ese caso, así que se responde 200/204 según la
		// operación lo haya hecho antes. Aquí llega solo si el flujo devolvió
		// el error tras commitear; se trata como conflicto operativo.
		escribirJSON(w, http.StatusConflict, gen.Error{Codigo: "idp_sync_pending", Mensaje: err.Error()})
	default:
		// Errores de plataforma (validación, no autorizado) o desconocidos.
		escribirError(w, err)
	}
}
