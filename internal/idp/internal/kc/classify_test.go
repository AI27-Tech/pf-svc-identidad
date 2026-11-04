package kc

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// codigosOAuthCerrados es la lista cerrada de códigos OAuth que el adaptador
// reconoce (diseño §Tabla de clasificación).
var codigosOAuthCerrados = []string{
	"invalid_grant",
	"invalid_client",
	"unauthorized_client",
	"invalid_request",
	"unsupported_grant_type",
	"invalid_scope",
	"unknown_error",
}

// escenarioRespuesta es un caso generado para la propiedad de clasificación:
// cubre operación, código HTTP, código OAuth, cuerpo, error de transporte y el
// estado de los dos contextos.
type escenarioRespuesta struct {
	op           string
	statusCode   int
	oauthCode    string
	transportErr error
	callerCancel bool
	callCancel   bool
	bodyOK       bool
}

// Generate implementa quick.Generator.
func (escenarioRespuesta) Generate(r *rand.Rand, _ int) reflect.Value {
	e := escenarioRespuesta{}
	e.op = []string{"Issue", "Refresh", "Revoke"}[r.Intn(3)]
	e.statusCode = []int{0, 200, 204, 302, 400, 401, 403, 404, 418, 429, 500, 503}[r.Intn(12)]

	// Código OAuth: a veces de la lista cerrada, a veces arbitrario, a veces vacío.
	switch r.Intn(3) {
	case 0:
		e.oauthCode = codigosOAuthCerrados[r.Intn(len(codigosOAuthCerrados))]
	case 1:
		e.oauthCode = fmt.Sprintf("codigo_%d", r.Intn(1000))
	default:
		e.oauthCode = ""
	}

	if r.Intn(4) == 0 {
		e.transportErr = errors.New([]string{"no such host", "connection refused", "i/o timeout", "EOF"}[r.Intn(4)])
	}
	e.callerCancel = r.Intn(5) == 0
	e.callCancel = r.Intn(5) == 0
	e.bodyOK = r.Intn(2) == 0
	return reflect.ValueOf(e)
}

// TestPropiedadClasificacion verifica que classify sea total y exclusiva: para
// cada escenario devuelve exactamente una categoría, y errorDe produce un solo
// centinela (o envuelve ctx.Err(), o nil) coherente con esa categoría. La
// categoría coincide con la primera fila aplicable de la tabla.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 1: Clasificación total, exclusiva y conforme a la tabla
func TestPropiedadClasificacion(t *testing.T) {
	propiedad := func(e escenarioRespuesta) bool {
		var callerErr, callErr error
		if e.callerCancel {
			callerErr = context.Canceled
		}
		if e.callCancel {
			callErr = context.DeadlineExceeded
		}

		cat, _ := classify(e.op, e.statusCode, e.oauthCode, e.transportErr, callerErr, callErr, e.bodyOK)

		// La categoría devuelta debe coincidir con el oráculo (primera fila aplicable).
		if cat != oraculoCategoria(e) {
			return false
		}

		// errorDe debe mapear a exactamente un resultado observable.
		err := errorDe(e.op, cat, callErr)
		return exclusividadError(cat, err)
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}

// oraculoCategoria replica la tabla del diseño de forma independiente a classify,
// aplicando la misma precedencia por fila.
func oraculoCategoria(e escenarioRespuesta) categoria {
	switch {
	case e.callerCancel: // Fila 1
		return cancelado
	case e.callCancel: // Fila 2
		return proveedorNoDisponible
	case e.transportErr != nil: // Fila 3
		return proveedorNoDisponible
	case e.statusCode == 500 && e.oauthCode == "unknown_error" && e.op == "Issue": // Fila 4a
		return credencialInvalida
	case e.statusCode >= 500 && e.statusCode < 600: // Fila 4
		return proveedorNoDisponible
	case e.statusCode == 200 && !e.bodyOK && e.op != "Revoke": // Fila 5
		return proveedorNoDisponible
	case e.statusCode == 200 && e.bodyOK: // Fila 6
		return exito
	case e.op == "Revoke" && (e.statusCode == 204 || e.statusCode == 200 || e.statusCode == 404): // Fila 7
		return exito
	case e.statusCode == 400 && e.oauthCode == "invalid_grant": // Fila 9
		if e.op == "Refresh" {
			return refreshInvalido
		}
		return credencialInvalida
	default: // Filas 8, 10, 11
		return proveedorNoDisponible
	}
}

// exclusividadError verifica que el error producido corresponda a exactamente
// un centinela (o a ctx.Err(), o a nil) según la categoría.
func exclusividadError(cat categoria, err error) bool {
	esCred := errors.Is(err, idp.ErrCredencialInvalida)
	esRefresh := errors.Is(err, idp.ErrRefreshInvalido)
	esProv := errors.Is(err, idp.ErrProveedorNoDisponible)
	esCtx := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)

	switch cat {
	case exito:
		return err == nil && !esCred && !esRefresh && !esProv
	case credencialInvalida:
		return esCred && !esRefresh && !esProv && !esCtx
	case refreshInvalido:
		return esRefresh && !esCred && !esProv && !esCtx
	case proveedorNoDisponible:
		return esProv && !esCred && !esRefresh
	case cancelado:
		return esCtx && !esCred && !esRefresh && !esProv
	default:
		return false
	}
}

// TestClasificacionPorFila fija un ejemplo con nombre por cada fila de la tabla
// de clasificación, incluido el 404 idempotente de Revoke y el 401/403 de
// Revoke como no-éxito.
func TestClasificacionPorFila(t *testing.T) {
	casos := []struct {
		nombre     string
		op         string
		statusCode int
		oauthCode  string
		transport  error
		callerErr  error
		callErr    error
		bodyOK     bool
		esperado   categoria
	}{
		{"fila1 llamador cancelado", "Issue", 0, "", nil, context.Canceled, nil, false, cancelado},
		{"fila2 timeout propio", "Issue", 0, "", nil, nil, context.DeadlineExceeded, false, proveedorNoDisponible},
		{"fila3 transporte", "Issue", 0, "", errors.New("connection refused"), nil, nil, false, proveedorNoDisponible},
		{"fila4 5xx", "Issue", 503, "", nil, nil, nil, false, proveedorNoDisponible},
		{"fila4a issue 500 unknown_error (sin OTP)", "Issue", 500, "unknown_error", nil, nil, nil, false, credencialInvalida},
		{"fila4a acotada: refresh 500 unknown_error sigue no disponible", "Refresh", 500, "unknown_error", nil, nil, nil, false, proveedorNoDisponible},
		{"fila4a acotada: revoke 500 unknown_error sigue no disponible", "Revoke", 500, "unknown_error", nil, nil, nil, false, proveedorNoDisponible},
		{"fila4a acotada: issue 500 sin oauth sigue no disponible", "Issue", 500, "", nil, nil, nil, false, proveedorNoDisponible},
		{"fila5 200 cuerpo invalido", "Refresh", 200, "", nil, nil, nil, false, proveedorNoDisponible},
		{"fila6 200 valido", "Issue", 200, "", nil, nil, nil, true, exito},
		{"fila7 revoke 204", "Revoke", 204, "", nil, nil, nil, false, exito},
		{"fila7 revoke 404 idempotente", "Revoke", 404, "", nil, nil, nil, false, exito},
		{"fila9 issue invalid_grant", "Issue", 400, "invalid_grant", nil, nil, nil, false, credencialInvalida},
		{"fila9 refresh invalid_grant", "Refresh", 400, "invalid_grant", nil, nil, nil, false, refreshInvalido},
		{"fila10 invalid_client", "Issue", 400, "invalid_client", nil, nil, nil, false, proveedorNoDisponible},
		{"fila11 429 sin oauth", "Issue", 429, "", nil, nil, nil, false, proveedorNoDisponible},
		{"revoke 401 no exito", "Revoke", 401, "", nil, nil, nil, false, proveedorNoDisponible},
		{"revoke 403 no exito", "Revoke", 403, "", nil, nil, nil, false, proveedorNoDisponible},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cat, _ := classify(c.op, c.statusCode, c.oauthCode, c.transport, c.callerErr, c.callErr, c.bodyOK)
			if cat != c.esperado {
				t.Errorf("classify(%s, %d, %q) = %q; esperaba %q", c.op, c.statusCode, c.oauthCode, cat, c.esperado)
			}
		})
	}
}
