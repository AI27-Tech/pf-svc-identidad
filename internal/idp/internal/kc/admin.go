package kc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// admin maneja la administración de usuarios y grupos en Keycloak para IdpAdmin.
// Usa la cuenta de servicio de administración (separada de la de revocación).
type admin struct {
	adapter *adapter
	srvTok  *ServiceTokenProvider
}

// newAdmin crea un nuevo admin para IdpAdmin.
func newAdmin(adapter *adapter, srvTok *ServiceTokenProvider) *admin {
	return &admin{
		adapter: adapter,
		srvTok:  srvTok,
	}
}

// CrearUsuario da de alta al usuario con su atributo de tenant (inmutable por
// el usuario, E4-T1) y lo coloca en el grupo del rol inicial. Devuelve el `sub`
// opaco que Keycloak asigna (DEC-072). Idempotente por el identificador de
// usuario: dar de alta dos veces no crea dos usuarios (busca primero, si existe
// devuelve su sub).
func (a *admin) CrearUsuario(ctx context.Context, u idp.NuevoUsuario) (string, error) {
	const op = "CrearUsuario"

	// Paso 1: obtener token de servicio.
	token, err := a.srvTok.GetToken(ctx)
	if err != nil {
		return "", fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}

	// Paso 2: buscar el usuario por username en Keycloak. Si existe, devolver su sub.
	existente, err := a.buscarUsuarioPorUsername(ctx, token, u.Usuario)
	if err == nil && existente != "" {
		return existente, nil // idempotente: ya existe
	}
	// Si el error no es "no encontrado", propagar.
	if err != nil && !strings.Contains(err.Error(), "no encontrado") {
		return "", fmt.Errorf("idp/keycloak: %s: buscar usuario: %v: %w", op, err, idp.ErrProveedorNoDisponible)
	}

	// Paso 3: crear el usuario en Keycloak.
	sub, err := a.crearUsuarioEnKeycloak(ctx, token, u)
	if err != nil {
		// Si es 401, invalidar token y reintentar una sola vez.
		if strings.Contains(err.Error(), "401") {
			a.srvTok.Invalidate()
			token, err = a.srvTok.GetToken(ctx)
			if err != nil {
				return "", fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
			}
			sub, err = a.crearUsuarioEnKeycloak(ctx, token, u)
			if err != nil {
				return "", fmt.Errorf("idp/keycloak: %s: crear usuario (reintento): %v: %w", op, err, idp.ErrProveedorNoDisponible)
			}
		} else {
			return "", fmt.Errorf("idp/keycloak: %s: crear usuario: %v: %w", op, err, idp.ErrProveedorNoDisponible)
		}
	}

	// Paso 4: agregar el usuario al grupo del rol inicial.
	if err := a.AgregarUsuarioAGrupo(ctx, token, sub, u.Rol); err != nil {
		// El usuario ya fue creado en Keycloak pero falló al agregarlo al grupo.
		// Es no-op para idempotencia: si ya está en el grupo, devuelve el sub.
		// Si es un error real, se propaga porque el usuario quedó en estado
		// inconsistente (creado pero sin grupo).
		return "", fmt.Errorf("idp/keycloak: %s: agregar a grupo: %v: %w", op, err, idp.ErrProveedorNoDisponible)
	}

	return sub, nil
}

// DeshabilitarUsuario deja al usuario sin poder autenticarse. Idempotente: si
// ya está deshabilitado, no es error.
func (a *admin) DeshabilitarUsuario(ctx context.Context, idpSubject string) error {
	const op = "DeshabilitarUsuario"

	token, err := a.srvTok.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}

	// En Keycloak, deshabilitar es poner enabled=false.
	if err := a.actualizarUsuario(ctx, token, idpSubject, map[string]interface{}{
		"enabled": false,
	}); err != nil {
		// Si es 401, reintentar.
		if strings.Contains(err.Error(), "401") {
			a.srvTok.Invalidate()
			token, err = a.srvTok.GetToken(ctx)
			if err != nil {
				return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
			}
			if err := a.actualizarUsuario(ctx, token, idpSubject, map[string]interface{}{
				"enabled": false,
			}); err != nil {
				return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
			}
		} else {
			return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
		}
	}
	return nil
}

// HabilitarUsuario revierte la deshabilitación. Idempotente.
func (a *admin) HabilitarUsuario(ctx context.Context, idpSubject string) error {
	const op = "HabilitarUsuario"

	token, err := a.srvTok.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}

	// En Keycloak, habilitar es poner enabled=true.
	if err := a.actualizarUsuario(ctx, token, idpSubject, map[string]interface{}{
		"enabled": true,
	}); err != nil {
		// Si es 401, reintentar.
		if strings.Contains(err.Error(), "401") {
			a.srvTok.Invalidate()
			token, err = a.srvTok.GetToken(ctx)
			if err != nil {
				return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
			}
			if err := a.actualizarUsuario(ctx, token, idpSubject, map[string]interface{}{
				"enabled": true,
			}); err != nil {
				return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
			}
		} else {
			return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
		}
	}
	return nil
}

// MoverDeGrupo quita al usuario del grupo del rol anterior y lo pone en el
// nuevo, EN ESE ORDEN (DEC-084), para no dejarlo nunca en dos grupos de rol a
// la vez. Un rolAnterior vacío significa solo agregar (asignación inicial).
func (a *admin) MoverDeGrupo(ctx context.Context, idpSubject, rolAnterior, rolNuevo string) error {
	const op = "MoverDeGrupo"

	token, err := a.srvTok.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
	}

	// Paso 1: quitar del grupo anterior (si hay).
	if rolAnterior != "" {
		if err := a.QuitarUsuarioDeGrupo(ctx, token, idpSubject, rolAnterior); err != nil {
			// Si es 401, invalidar y reintentar.
			if strings.Contains(err.Error(), "401") {
				a.srvTok.Invalidate()
				token, err = a.srvTok.GetToken(ctx)
				if err != nil {
					return fmt.Errorf("idp/keycloak: %s: %w", op, idp.ErrProveedorNoDisponible)
				}
				if err := a.QuitarUsuarioDeGrupo(ctx, token, idpSubject, rolAnterior); err != nil {
					return fmt.Errorf("idp/keycloak: %s: quitar de grupo anterior: %w", op, idp.ErrProveedorNoDisponible)
				}
			} else {
				return fmt.Errorf("idp/keycloak: %s: quitar de grupo anterior: %w", op, idp.ErrProveedorNoDisponible)
			}
		}
	}

	// Paso 2: agregar al nuevo grupo.
	if err := a.AgregarUsuarioAGrupo(ctx, token, idpSubject, rolNuevo); err != nil {
		// El usuario fue quitado del grupo anterior. Intentar reagregarlo
		// (rollback), pero si falla, el usuario queda sin grupo. Es un error
		// grave y se propaga para que el gobierno intente sincronizar de nuevo.
		if rolAnterior != "" {
			_ = a.AgregarUsuarioAGrupo(ctx, token, idpSubject, rolAnterior)
		}
		return fmt.Errorf("idp/keycloak: %s: agregar a nuevo grupo: %w", op, idp.ErrProveedorNoDisponible)
	}

	return nil
}

// === Métodos privados de ayuda ===

// buscarUsuarioPorUsername busca un usuario por su username y devuelve su sub.
// Si no existe, devuelve "" (no es error).
func (a *admin) buscarUsuarioPorUsername(ctx context.Context, token, username string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, a.adapter.config.Timeout)
	defer cancel()

	// `exact=true` evita que Keycloak devuelva coincidencias parciales por
	// infijo; `username` se escapa por si trae `@`, `+` o espacios.
	reqURL := strings.TrimRight(a.adapter.config.AdminUsersURL, "/") +
		"?exact=true&username=" + url.QueryEscape(username)
	req, err := http.NewRequestWithContext(timeoutCtx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.adapter.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", fmt.Errorf("usuario no encontrado")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, _ := io.ReadAll(limited)

	var usuarios []map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &usuarios); err != nil {
		return "", err
	}

	if len(usuarios) == 0 {
		return "", fmt.Errorf("usuario no encontrado")
	}

	// Devolver el primer resultado (Keycloak devuelve un arreglo).
	if sub, ok := usuarios[0]["id"].(string); ok {
		return sub, nil
	}
	return "", fmt.Errorf("no hay id en respuesta")
}

// crearUsuarioEnKeycloak crea un usuario en Keycloak. El username es el
// IdentificadorUsuario (correo o usuario); el nombre es el Nombre. El tenant y
// el rol se guardan como atributos y membresía de grupo, respectivamente.
func (a *admin) crearUsuarioEnKeycloak(ctx context.Context, token string, u idp.NuevoUsuario) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, a.adapter.config.Timeout)
	defer cancel()

	// Construir payload del usuario. El atributo de tenant se llama `tenant_id`:
	// el user profile del realm lo declara así y el mapper de claims lee
	// `user.attribute: tenant_id` (deploy/keycloak/realm-ai27.json).
	payload := map[string]interface{}{
		"username":  u.Usuario,
		"enabled":   true,
		"firstName": u.Nombre,
		"attributes": map[string]interface{}{
			"tenant_id": []string{u.TenantID},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(a.adapter.config.AdminUsersURL, "/")
	req, err := http.NewRequestWithContext(timeoutCtx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.adapter.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		io.ReadAll(resp.Body) // descartar cuerpo
		return "", fmt.Errorf("POST %d", resp.StatusCode)
	}

	// Keycloak devuelve el Location header con la URL del usuario creado.
	// Extraer el sub del Path (última parte).
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header")
	}

	parts := strings.Split(strings.TrimRight(location, "/"), "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("no sub en Location")
	}
	sub := parts[len(parts)-1]
	return sub, nil
}

// actualizarUsuario actualiza los campos del usuario en Keycloak. El payload
// es un mapa de campos a actualizar (enabled, firstName, etc.).
func (a *admin) actualizarUsuario(ctx context.Context, token, idpSubject string, campos map[string]interface{}) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, a.adapter.config.Timeout)
	defer cancel()

	body, err := json.Marshal(campos)
	if err != nil {
		return err
	}

	url := strings.TrimRight(a.adapter.config.AdminUsersURL, "/") + "/" + idpSubject
	req, err := http.NewRequestWithContext(timeoutCtx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.adapter.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("401")
	}
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %d", resp.StatusCode)
	}
	return nil
}

// AgregarUsuarioAGrupo agrega el usuario al grupo del rol especificado.
func (a *admin) AgregarUsuarioAGrupo(ctx context.Context, token, idpSubject, rol string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, a.adapter.config.Timeout)
	defer cancel()

	// Buscar el grupo por nombre. Los grupos de rol se nombran como el rol
	// canónico (administrador, jefe_operativo, etc.).
	grupoID, err := a.buscarGrupoPorNombre(ctx, token, rol)
	if err != nil {
		return err
	}

	// Agregar el usuario al grupo.
	url := strings.TrimRight(a.adapter.config.AdminUsersURL, "/") + "/" + idpSubject + "/groups/" + grupoID
	req, err := http.NewRequestWithContext(timeoutCtx, "PUT", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.adapter.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("401")
	}
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %d", resp.StatusCode)
	}
	return nil
}

// QuitarUsuarioDeGrupo quita el usuario del grupo del rol especificado.
func (a *admin) QuitarUsuarioDeGrupo(ctx context.Context, token, idpSubject, rol string) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, a.adapter.config.Timeout)
	defer cancel()

	// Buscar el grupo por nombre.
	grupoID, err := a.buscarGrupoPorNombre(ctx, token, rol)
	if err != nil {
		return err
	}

	// Quitar el usuario del grupo.
	url := strings.TrimRight(a.adapter.config.AdminUsersURL, "/") + "/" + idpSubject + "/groups/" + grupoID
	req, err := http.NewRequestWithContext(timeoutCtx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.adapter.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("401")
	}
	if resp.StatusCode == 404 {
		// El usuario no estaba en el grupo; es idempotente, no es error.
		return nil
	}
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		io.ReadAll(resp.Body)
		return fmt.Errorf("DELETE %d", resp.StatusCode)
	}
	return nil
}

// buscarGrupoPorNombre busca un grupo por su nombre y devuelve su id.
func (a *admin) buscarGrupoPorNombre(ctx context.Context, token, nombre string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, a.adapter.config.Timeout)
	defer cancel()

	// En Keycloak, la URL de grupos es /admin/realms/{realm}/groups. El nombre
	// se escapa; se filtra por nombre exacto más abajo porque `search` casa por
	// infijo (p. ej. "coordinador" casaría al buscar "coord").
	reqURL := strings.TrimRight(a.adapter.config.AdminGroupsURL, "/") +
		"?search=" + url.QueryEscape(nombre)
	req, err := http.NewRequestWithContext(timeoutCtx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.adapter.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET groups %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, _ := io.ReadAll(limited)

	var grupos []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(bodyBytes, &grupos); err != nil {
		return "", err
	}

	// `search` casa por infijo: elegir la coincidencia de nombre EXACTO, no el
	// primer resultado, para no mover al usuario a un grupo equivocado.
	for _, g := range grupos {
		if g.Name == nombre && g.ID != "" {
			return g.ID, nil
		}
	}
	return "", fmt.Errorf("grupo no encontrado: %s", nombre)
}
