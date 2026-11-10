package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
)

// gobFalso registra lo que el handler le pasa al servicio. Los métodos no
// sobrescritos caen en la interfaz embebida nil (panic si una prueba los toca).
type gobFalso struct {
	gobierno.Gobierno
	gobierno.Consulta
	listado gobierno.ListadoSujetos
}

func (g *gobFalso) ListarSujetos(_ context.Context, in gobierno.ListadoSujetos) (gobierno.PaginaSujetos, error) {
	g.listado = in
	return gobierno.PaginaSujetos{}, nil
}

type actoresFalsos struct{}

func (actoresFalsos) PorIdpSubject(_ context.Context, sub string) (autorizacion.Sujeto, bool, error) {
	return autorizacion.Sujeto{ID: "99999999-9999-4999-8999-999999999999", IdpSubject: sub}, true, nil
}

// peticionGob ejecuta el handler con claims del rol dado en el contexto, como
// lo dejaría el middleware de auth.
func peticionGob(h http.HandlerFunc, metodo, url, cuerpo string, rol string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(metodo, url, strings.NewReader(cuerpo))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Subject: "sub-actor", Rol: rol}))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// TestListarSujetosHandler: el handler pasa los filtros y el rol del claim al
// servicio (que aplica la regla del jefe_operativo) y valida tenant_id y activo.
func TestListarSujetosHandler(t *testing.T) {
	g := &gobFalso{}
	h := NewGobiernoHandlers(g, g, actoresFalsos{})

	rec := peticionGob(h.ListarSujetos, "GET", "/api/v1/sujetos?q=ana&rol=monitorista&activo=false&tenant_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa&cursor=abc", "", "jefe_operativo")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, cuerpo %s", rec.Code, rec.Body)
	}
	f := g.listado.Filtro
	if f.Q != "ana" || f.Rol != autorizacion.RolMonitorista || f.Activo == nil || *f.Activo ||
		f.TenantID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || g.listado.Cursor != "abc" ||
		g.listado.ActorRol != autorizacion.RolJefeOperativo {
		t.Errorf("listado recibido: %+v", g.listado)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("una página vacía debe llevar items [], cuerpo %s", rec.Body)
	}

	for _, url := range []string{"/api/v1/sujetos?tenant_id=x", "/api/v1/sujetos?activo=quizas"} {
		if rec := peticionGob(h.ListarSujetos, "GET", url, "", "administrador"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d; quiere 400", url, rec.Code)
		}
	}
}

func (g *gobFalso) EditarSujeto(_ context.Context, in gobierno.EdicionSujeto) (gobierno.SujetoDetalle, error) {
	return gobierno.SujetoDetalle{Sujeto: autorizacion.Sujeto{ID: in.SujetoID, Nombre: in.Nombre}}, nil
}

// TestEditarSujetoHandler: el cuerpo solo admite `nombre`; mandar el
// identificador es 400 (DEC-139 #5). Con nombre responde 200 con el detalle.
func TestEditarSujetoHandler(t *testing.T) {
	g := &gobFalso{}
	h := NewGobiernoHandlers(g, g, actoresFalsos{})
	const url = "/api/v1/sujetos/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	editar := func(cuerpo string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PATCH", url, strings.NewReader(cuerpo))
		req.SetPathValue("id", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Subject: "sub-actor", Rol: "administrador"}))
		rec := httptest.NewRecorder()
		h.EditarSujeto(rec, req)
		return rec
	}
	if rec := editar(`{"nombre":"Ana","identificador_usuario":"otra@ai27.mx"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("con identificador: status %d; quiere 400", rec.Code)
	}
	rec := editar(`{"nombre":"Ana"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"nombre":"Ana"`) {
		t.Errorf("con nombre: status %d, cuerpo %s", rec.Code, rec.Body)
	}
}
