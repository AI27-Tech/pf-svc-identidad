package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// TestEmitirSesion401Uniforme verifica, a nivel HTTP, el criterio de no
// enumeración del Requisito 3.3 y de DEC-094 (C-2): el cuerpo del 401 es
// idéntico cuando falla la contraseña, cuando el usuario no existe y cuando el
// segundo factor es incorrecto o está vacío. El handler no distingue causas: el
// servicio devuelve siempre el mismo error de credencial inválida y el handler
// lo traduce igual.
//
// El rechazo local del segundo factor vacío ocurre dentro del flujo de emisión
// (DEC-094, opción b); aquí se verifica la propiedad observable por el cliente:
// un mismo cuerpo para todas las causas de rechazo de login.
func TestEmitirSesion401Uniforme(t *testing.T) {
	// Un único error de credencial inválida, usado para TODAS las causas: así es
	// como el servicio responde hoy (mapearErrorIdP) y como responde el rechazo
	// local de DEC-094. No enumeración.
	errUniforme := platformerrors.NewNoAutorizado("credencial inválida")

	h := NewHandlers(mockServicio{
		emitir: func(_ context.Context, _ sesion.Credentials) (sesion.Emitida, error) {
			return sesion.Emitida{}, errUniforme
		},
	})

	// Cada caso representa una causa de rechazo distinta. El cliente no debe
	// poder distinguirlas por el cuerpo ni por el status.
	factorAusente := `{"usuario":"u","contrasena":"p"}`
	factorNull := `{"usuario":"u","contrasena":"p","segundo_factor":null}`
	factorVacio := `{"usuario":"u","contrasena":"p","segundo_factor":""}`
	factorIncorrecto := `{"usuario":"u","contrasena":"p","segundo_factor":"000000"}`
	contrasenaErronea := `{"usuario":"u","contrasena":"mala","segundo_factor":"123456"}`
	usuarioInexistente := `{"usuario":"nadie","contrasena":"p","segundo_factor":"123456"}`

	casos := []struct {
		nombre string
		cuerpo string
	}{
		{"segundo factor ausente", factorAusente},
		{"segundo factor null", factorNull},
		{"segundo factor vacío", factorVacio},
		{"segundo factor incorrecto", factorIncorrecto},
		{"contraseña errónea", contrasenaErronea},
		{"usuario inexistente", usuarioInexistente},
	}

	var refStatus int
	var refCuerpo string
	for i, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sesiones", strings.NewReader(caso.cuerpo))
			rec := httptest.NewRecorder()

			h.EmitirSesion(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d; se esperaba 401", rec.Code)
			}
			var body gen.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("cuerpo no es un gen.Error válido: %v", err)
			}
			if body.Codigo != platformerrors.CategoryNoAutorizado.String() {
				t.Errorf("codigo = %q; se esperaba %q", body.Codigo, platformerrors.CategoryNoAutorizado.String())
			}

			// El primer caso fija la referencia; el resto debe coincidir exacto.
			if i == 0 {
				refStatus = rec.Code
				refCuerpo = rec.Body.String()
				return
			}
			if rec.Code != refStatus {
				t.Errorf("status = %d; se esperaba el mismo que el primer caso %d", rec.Code, refStatus)
			}
			if rec.Body.String() != refCuerpo {
				t.Errorf("cuerpo = %q; se esperaba idéntico al primer caso %q (no enumeración)", rec.Body.String(), refCuerpo)
			}
		})
	}
}

// TestEmitirSesionFactorValido verifica que un cuerpo con segundo_factor válido
// se decodifica y propaga al servicio, y que el 201 trae la sesión emitida.
// Complementa TestEmitirSesion401Uniforme: la ruta feliz no se ve afectada por
// el rechazo local de DEC-094.
func TestEmitirSesionFactorValido(t *testing.T) {
	var recibido sesion.Credentials
	h := NewHandlers(mockServicio{
		emitir: func(_ context.Context, c sesion.Credentials) (sesion.Emitida, error) {
			recibido = c
			return sesion.Emitida{
				SesionID:    "33333333-3333-3333-3333-333333333333",
				AccessToken: "at",
			}, nil
		},
	})

	cuerpo := `{"usuario":"u","contrasena":"p","segundo_factor":"123456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sesiones", strings.NewReader(cuerpo))
	rec := httptest.NewRecorder()

	h.EmitirSesion(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; se esperaba 201", rec.Code)
	}
	if recibido.SegundoFactor != "123456" {
		t.Errorf("el handler propagó SegundoFactor = %q; se esperaba 123456", recibido.SegundoFactor)
	}
}
