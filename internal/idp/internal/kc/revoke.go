package kc

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// revoker maneja la revocación de sesiones en Keycloak.
type revoker struct {
	adapter *adapter
	srvTok  *ServiceTokenProvider
}

// newRevoker crea un nuevo revoker.
func newRevoker(adapter *adapter, srvTok *ServiceTokenProvider) *revoker {
	return &revoker{
		adapter: adapter,
		srvTok:  srvTok,
	}
}

// revoke revoca la sesión en Keycloak. Recibe el `idpSessionID`: el id nativo
// de la sesión en Keycloak (DEC-083 #2/#3). Implementa la lógica de reintento
// ante 401 y clasifica los resultados según el diseño (tarea 8.2).
func (r *revoker) revoke(ctx context.Context, idpSessionID string) error {
	const op = "Revoke"

	// En V1 el argumento YA es el id de sesión de Keycloak (SessionResolver
	// nil). El resolvedor queda como costura para un eventual mapeo distinto.
	kcSessionID := idpSessionID
	if r.adapter.config.SessionResolver != nil {
		var err error
		kcSessionID, err = r.adapter.config.SessionResolver(idpSessionID)
		if err != nil {
			return fmt.Errorf("idp/keycloak: %s: error al resolver sesión: %w", op, err)
		}
	}

	// Obtener el token de servicio
	token, err := r.srvTok.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}

	// Construir URL de revocación
	url := strings.TrimRight(r.adapter.config.AdminSessionsURL, "/") + "/" + kcSessionID

	// Primer intento
	statusCode, err := r.deleteSession(ctx, url, token)
	if err != nil {
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}

	// Clasificar resultado
	switch statusCode {
	case 204, 200:
		// Éxito: sesión revocada o ya no existe
		return nil
	case 404:
		// Sesión no existe (idempotente: es éxito)
		return nil
	case 401:
		// Token expirado: invalidar caché y reintentar una sola vez
		r.srvTok.Invalidate()
		token, err = r.srvTok.GetToken(ctx)
		if err != nil {
			return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
		}
		statusCode, err = r.deleteSession(ctx, url, token)
		if err != nil {
			return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
		}
		// Después del reintento, clasificar nuevamente
		if statusCode == 401 || statusCode == 403 {
			return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
		}
		if statusCode == 200 || statusCode == 204 || statusCode == 404 {
			return nil
		}
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	case 403:
		// Prohibido: permisos insuficientes
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	default:
		// Otros errores: no disponible
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}
}

// deleteSession hace DELETE a la URL de administración de sesiones.
// Retorna el status code y cualquier error de red.
func (r *revoker) deleteSession(ctx context.Context, url string, bearerToken string) (int, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.adapter.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, "DELETE", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := r.adapter.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}
