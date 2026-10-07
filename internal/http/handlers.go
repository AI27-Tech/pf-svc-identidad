// Package http monta los handlers de las CINCO operaciones de sesiones del
// contrato (identidad.yaml), usando los tipos GENERADOS de
// api-client-go/identidad (regla 6: no se escriben tipos de wire a mano).
//
// Reparto de protección (identidad.yaml): `emitirSesion` y `refrescarSesion`
// son públicas (aún no hay token); `revocarSesion`, `obtenerSesion` y
// `listarSesionesRevocadas` van protegidas por el esquema bearer del contrato.
// El montaje de ese middleware lo hace Routes del módulo; aquí van los handlers.
//
// SCAFFOLD: los handlers decodifican/validan la entrada, delegan en
// sesion.Servicio y traducen el error con la taxonomía de la plataforma (I3).
// Como el servicio aún devuelve "no implementado", responden 501 de momento.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// Handlers agrupa los handlers HTTP de sesiones sobre el servicio de dominio.
type Handlers struct {
	svc sesion.Servicio
}

// NewHandlers crea los handlers con el servicio de sesión ya resuelto.
func NewHandlers(svc sesion.Servicio) *Handlers {
	return &Handlers{svc: svc}
}

// EmitirSesion maneja POST /api/v1/sesiones (pública).
func (h *Handlers) EmitirSesion(w http.ResponseWriter, r *http.Request) {
	var body gen.CredencialesEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	emitida, err := h.svc.EmitirSesion(r.Context(), body.Usuario, body.Contrasena)
	if err != nil {
		escribirError(w, err)
		return
	}
	escribirJSON(w, http.StatusCreated, aSesionEmitida(emitida))
}

// RefrescarSesion maneja POST /api/v1/sesiones/refresh (pública).
func (h *Handlers) RefrescarSesion(w http.ResponseWriter, r *http.Request) {
	var body gen.RefrescoEntrada
	if err := decodeJSON(r, &body); err != nil {
		escribirError(w, platformerrors.NewValidacion("cuerpo inválido"))
		return
	}
	emitida, err := h.svc.RefrescarSesion(r.Context(), body.RefreshToken)
	if err != nil {
		escribirError(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, aSesionEmitida(emitida))
}

// RevocarSesion maneja DELETE /api/v1/sesiones/{id} (protegida).
func (h *Handlers) RevocarSesion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sesión inválido"))
		return
	}
	if err := h.svc.RevocarSesion(r.Context(), id); err != nil {
		escribirError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ObtenerSesion maneja GET /api/v1/sesiones/{id} (protegida, fallback ADR 26 §5).
func (h *Handlers) ObtenerSesion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		escribirError(w, platformerrors.NewValidacion("id de sesión inválido"))
		return
	}
	estado, err := h.svc.ObtenerSesion(r.Context(), id)
	if err != nil {
		escribirError(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, aEstadoSesion(estado))
}

// ListarSesionesRevocadas maneja GET /api/v1/sesiones/revocadas (protegida).
func (h *Handlers) ListarSesionesRevocadas(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	pagina, err := h.svc.ListarRevocadas(r.Context(), cursor)
	if err != nil {
		escribirError(w, err)
		return
	}
	escribirJSON(w, http.StatusOK, aPaginaRevocaciones(pagina))
}

// --- Mapeo dominio → tipos del contrato ------------------------------------

func aSesionEmitida(e sesion.Emitida) gen.SesionEmitida {
	out := gen.SesionEmitida{
		AccessToken: e.AccessToken,
		ExpiraEn:    e.ExpiraEn,
		SesionId:    mustUUID(e.SesionID),
	}
	if e.RefreshToken != "" {
		rt := e.RefreshToken
		out.RefreshToken = &rt
	}
	return out
}

func aEstadoSesion(e sesion.Estado) gen.EstadoSesion {
	return gen.EstadoSesion{
		SesionId:   mustUUID(e.SesionID),
		Revocada:   e.Revocada,
		RevocadaEn: e.RevocadaEn,
		ExpiraEn:   e.ExpiraEn,
	}
}

func aPaginaRevocaciones(p sesion.PaginaRevocadas) gen.PaginaRevocaciones {
	items := make([]gen.Revocacion, 0, len(p.Items))
	for _, t := range p.Items {
		items = append(items, gen.Revocacion{
			SesionId:   mustUUID(t.SesionID),
			RevocadaEn: t.RevocadaEn,
			ExpiraEn:   t.ExpiraEn,
		})
	}
	return gen.PaginaRevocaciones{Items: items, CursorSiguiente: p.CursorSiguiente}
}

// mustUUID convierte un sesion_id de dominio al tipo UUID del contrato. En el
// scaffold el id puede venir vacío; ParseUUID devuelve el cero y no entra a la
// ruta real todavía.
func mustUUID(s string) openapi_types.UUID {
	u, _ := uuid.Parse(s)
	return u
}

// --- Utilidades HTTP --------------------------------------------------------

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func escribirJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// escribirError traduce el error con la taxonomía de la plataforma (I3) a la
// forma `Error` del contrato (`codigo`, `mensaje`). Un error sin categoría se
// trata como "no implementado" (501) mientras el servicio es scaffold.
func escribirError(w http.ResponseWriter, err error) {
	cat := platformerrors.AsCategory(err)
	if cat == platformerrors.CategoryUnknown {
		escribirJSON(w, http.StatusNotImplemented, gen.Error{
			Codigo:  "no_implementado",
			Mensaje: "operación no implementada (scaffold)",
		})
		return
	}
	var pe *platformerrors.PlatformError
	mensaje := "error"
	if errors.As(err, &pe) {
		mensaje = pe.Message
	}
	escribirJSON(w, cat.HTTPStatus(), gen.Error{Codigo: cat.String(), Mensaje: mensaje})
}
