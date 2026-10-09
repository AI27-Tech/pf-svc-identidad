package kc

import (
	"context"
	"fmt"
	"strings"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// categoria enumera las categorías de resultado de una operación contra Keycloak.
// Estas categorías se mapean a centinelas del puerto o errores contextuales.
type categoria string

const (
	exito                 categoria = "exito"
	credencialInvalida    categoria = "credencial_invalida"
	refreshInvalido       categoria = "refresh_invalido"
	proveedorNoDisponible categoria = "proveedor_no_disponible"
	cancelado             categoria = "cancelado"
)

// clasificador de causa de transporte (para telemetría)
type causaTransporte string

const (
	causaTimeout     causaTransporte = "timeout"
	causaDNS         causaTransporte = "dns"
	causaConnRefused causaTransporte = "conn_refused"
	causaHTTP5XX     causaTransporte = "http_5xx"
	causaCuerpoInv   causaTransporte = "cuerpo_invalido"
)

// classify clasifica el resultado de una operación contra Keycloak en una
// categoría de resultado. Implementa la tabla de clasificación del diseño.
//
// Parámetros:
//   - op: nombre de la operación ("Issue", "Refresh", "Revoke")
//   - statusCode: código HTTP devuelto (0 si no hay respuesta)
//   - oauthCode: código de error OAuth si aparece en la respuesta (p. ej. "invalid_grant")
//   - transportErr: error de transporte (net.Dial, timeout, etc.)
//   - callerCtxErr: error del contexto del llamador antes de hacer la llamada
//   - callCtxErr: error del contexto usado para la llamada (timeout, cancelado)
//   - bodyOK: si el cuerpo se decodificó correctamente
//
// Retorna la categoría y la causa de transporte (si aplica).
func classify(op string, statusCode int, oauthCode string, transportErr error, callerCtxErr error, callCtxErr error, bodyOK bool) (categoria, causaTransporte) {
	// La precedencia sigue la tabla del diseño (§Tabla de clasificación): la
	// primera condición aplicable decide, de modo que cada entrada cae en
	// exactamente una categoría (Propiedad 1).

	// Fila 1: ctx del llamador cancelado o vencido (antes de la llamada).
	if callerCtxErr != nil {
		return cancelado, ""
	}

	// Fila 2: callCtx vencido por cfg.Timeout → proveedor no disponible.
	if callCtxErr != nil {
		return proveedorNoDisponible, causaTimeout
	}

	// Fila 3: error de transporte (DNS, conexión rechazada, TLS, EOF).
	if transportErr != nil {
		return proveedorNoDisponible, clasificarCausaTransporte(transportErr)
	}

	// Fila 4: HTTP 5xx → proveedor no disponible.
	if statusCode >= 500 && statusCode < 600 {
		return proveedorNoDisponible, causaHTTP5XX
	}

	// Fila 5: HTTP 200 con cuerpo no interpretable (no JSON, sin access_token
	// o expires_in <= 0) → proveedor no disponible. En Issue/Refresh bodyOK es
	// false en ese caso; Revoke no decodifica cuerpo (no aplica).
	if statusCode == 200 && !bodyOK && op != "Revoke" {
		return proveedorNoDisponible, causaCuerpoInv
	}

	// Fila 6: HTTP 200 válido → éxito.
	if statusCode == 200 && bodyOK {
		return exito, ""
	}

	// Fila 7: Revoke con 204, 200 o 404 → éxito (idempotente, R7.1, R7.2).
	if op == "Revoke" && (statusCode == 204 || statusCode == 200 || statusCode == 404) {
		return exito, ""
	}

	// Fila 9: HTTP 400 invalid_grant → credencial inválida (Issue) o refresh
	// inválido (Refresh). [Pendiente A-3/A-14] Keycloak responde invalid_grant
	// también para cuenta deshabilitada, acción pendiente (p. ej. enrolar OTP) y
	// falla de segundo factor; hoy todas caen en credencialInvalida. Si A-14 pide
	// distinguirlas se agregará una fila, nunca ErrProveedorNoDisponible (R4.2).
	if statusCode == 400 && oauthCode == "invalid_grant" {
		if op == "Refresh" {
			return refreshInvalido, ""
		}
		return credencialInvalida, ""
	}

	// Filas 8, 10 y 11: 401/403 de Revoke, códigos OAuth de configuración del
	// despliegue (invalid_client, unauthorized_client, …) y cualquier otro
	// código (incluido 429 y 4xx sin cuerpo OAuth) → proveedor no disponible.
	return proveedorNoDisponible, ""
}

// clasificarCausaTransporte traduce un error de transporte a la causa para telemetría.
func clasificarCausaTransporte(err error) causaTransporte {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "no such host") || strings.Contains(errStr, "dial"):
		return causaDNS
	case strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "actively refused"):
		return causaConnRefused
	case strings.Contains(errStr, "i/o timeout") || strings.Contains(errStr, "deadline exceeded"):
		return causaTimeout
	default:
		// La mayoría de los errores de transporte restantes son por timeout.
		return causaTimeout
	}
}

// errorDe construye un error a partir de la categoría. Es el único lugar que
// devuelve los centinelas del puerto.
func errorDe(op string, cat categoria, callCtxErr error) error {
	switch cat {
	case exito:
		return nil
	case credencialInvalida:
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrCredencialInvalida)
	case refreshInvalido:
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrRefreshInvalido)
	case proveedorNoDisponible:
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	case cancelado:
		if callCtxErr != nil {
			return fmt.Errorf("idp/keycloak: %s: %w", op, callCtxErr)
		}
		return fmt.Errorf("idp/keycloak: %s: %w", op, context.Canceled)
	default:
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}
}
