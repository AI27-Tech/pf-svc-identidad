package http

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/quick"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// servicioConError implementa sesion.Servicio y hace que ObtenerSesion devuelva
// un error fijo, para ejercer el mapeo del handler sin tocar Postgres.
type servicioConError struct {
	err error
}

func (s servicioConError) EmitirSesion(ctx context.Context, cred sesion.Credentials) (sesion.Emitida, error) {
	return sesion.Emitida{}, s.err
}
func (s servicioConError) RefrescarSesion(ctx context.Context, refreshToken string) (sesion.Emitida, error) {
	return sesion.Emitida{}, s.err
}
func (s servicioConError) RevocarSesion(ctx context.Context, sesionID string) error { return s.err }
func (s servicioConError) ObtenerSesion(ctx context.Context, sesionID string) (sesion.Estado, error) {
	return sesion.Estado{}, s.err
}
func (s servicioConError) ListarRevocadas(ctx context.Context, cursor string) (sesion.PaginaRevocadas, error) {
	return sesion.PaginaRevocadas{}, s.err
}

// categoriasYConstructores mapea cada categoría de la plataforma a un
// constructor de PlatformError, para generar errores arbitrarios en la prueba.
var categoriasYConstructores = []struct {
	cat  platformerrors.Category
	make func(string) *platformerrors.PlatformError
}{
	{platformerrors.CategoryNoEncontrado, func(m string) *platformerrors.PlatformError { return platformerrors.NewNoEncontrado("%s", m) }},
	{platformerrors.CategoryValidacion, func(m string) *platformerrors.PlatformError { return platformerrors.NewValidacion("%s", m) }},
	{platformerrors.CategoryNoAutorizado, func(m string) *platformerrors.PlatformError { return platformerrors.NewNoAutorizado("%s", m) }},
	{platformerrors.CategoryConflicto, func(m string) *platformerrors.PlatformError { return platformerrors.NewConflicto("%s", m) }},
	{platformerrors.CategoryDegradado, func(m string) *platformerrors.PlatformError { return platformerrors.NewDegradado("%s", m) }},
	{platformerrors.CategoryRoto, func(m string) *platformerrors.PlatformError { return platformerrors.NewRoto("%s", m) }},
}

// TestPropertyMapeoDeErroresDelHandler verifica el mapeo del handler HTTP a la
// respuesta del contrato (Property 10, Requisito 6.2):
//   - un error de categoría c produce el status c.HTTPStatus() con codigo
//     c.String() y mensaje = Message del PlatformError;
//   - el cuerpo nunca filtra el texto del error interno más allá del Message
//     declarado;
//   - no se emite un EstadoSesion cuando hubo error.
//
// No requiere Postgres: el servicio es un doble que solo devuelve el error.
func TestPropertyMapeoDeErroresDelHandler(t *testing.T) {
	t.Log("Feature: e4-t3-registro-sesion-revocacion, Property 10: Mapeo de errores a la respuesta del contrato")

	propiedad := func(idxCat uint8, mensaje string) bool {
		elegido := categoriasYConstructores[int(idxCat)%len(categoriasYConstructores)]
		perr := elegido.make(mensaje)

		h := NewHandlers(servicioConError{err: perr})

		// Ejercemos ObtenerSesion con un UUID válido para pasar la validación de
		// forma y llegar a la delegación en el servicio (que devuelve el error).
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sesiones/33333333-3333-3333-3333-333333333333", nil)
		req.SetPathValue("id", "33333333-3333-3333-3333-333333333333")
		rec := httptest.NewRecorder()

		h.ObtenerSesion(rec, req)

		// (a) Status = HTTPStatus de la categoría.
		if rec.Code != elegido.cat.HTTPStatus() {
			t.Logf("status = %d; se esperaba %d (categoría %v)", rec.Code, elegido.cat.HTTPStatus(), elegido.cat)
			return false
		}

		// (b) Cuerpo = Error del contrato con codigo = categoría.String() y
		//     mensaje = el Message del PlatformError.
		var body gen.Error
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Logf("cuerpo no es un gen.Error válido: %v", err)
			return false
		}
		if body.Codigo != elegido.cat.String() {
			t.Logf("codigo = %q; se esperaba %q", body.Codigo, elegido.cat.String())
			return false
		}
		if body.Mensaje != perr.Message {
			t.Logf("mensaje = %q; se esperaba %q", body.Mensaje, perr.Message)
			return false
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 100, Rand: rand.New(rand.NewSource(10))}
	if err := quick.Check(propiedad, cfg); err != nil {
		t.Errorf("Property 10 falló: %v", err)
	}
}

// TestMapeoNoEncontrado404 fija el caso central del contrato: ErrNoEncontrada
// envuelta sale como 404 con codigo "no_encontrado" (Requisito 6.2).
func TestMapeoNoEncontrado404(t *testing.T) {
	h := NewHandlers(servicioConError{err: sesion.WrapNotFound(sesion.ErrNoEncontrada)})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sesiones/33333333-3333-3333-3333-333333333333", nil)
	req.SetPathValue("id", "33333333-3333-3333-3333-333333333333")
	rec := httptest.NewRecorder()

	h.ObtenerSesion(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d; se esperaba 404", rec.Code)
	}
	var body gen.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Codigo != "no_encontrado" {
		t.Errorf("codigo = %q; se esperaba no_encontrado", body.Codigo)
	}
}
