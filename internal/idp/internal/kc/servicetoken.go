package kc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// serviceToken es un token para la cuenta de servicio. Protegido con caché
// y válido hasta 10 segundos antes de su expiración.
type serviceToken struct {
	token     string
	expiresAt time.Time
}

// ServiceTokenProvider maneja la emisión y caché del token de servicio. Las
// credenciales son explícitas (clientID/clientSecret), de modo que puedan
// coexistir dos cuentas de servicio separadas: la de revocación y la de
// administración de usuarios (C4/G-4, DEC-073).
type ServiceTokenProvider struct {
	cfg          Config
	clientID     string
	clientSecret string
	http         *http.Client
	now          func() time.Time
	mu           sync.Mutex
	cached       serviceToken
}

// NewServiceTokenProvider crea un proveedor para la cuenta de servicio de
// REVOCACIÓN (ServiceClientID/ServiceClientSecret), por compatibilidad con el
// revoker existente.
func NewServiceTokenProvider(cfg Config, httpClient *http.Client) *ServiceTokenProvider {
	return NewServiceTokenProviderCon(cfg, cfg.ServiceClientID, cfg.ServiceClientSecret, httpClient)
}

// NewServiceTokenProviderCon crea un proveedor con credenciales explícitas.
// Lo usa la administración de usuarios (E4-T6) para su cuenta separada.
func NewServiceTokenProviderCon(cfg Config, clientID, clientSecret string, httpClient *http.Client) *ServiceTokenProvider {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &ServiceTokenProvider{
		cfg:          cfg,
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         httpClient,
		now:          time.Now,
	}
}

// GetToken obtiene el token de servicio, usando caché si aún es válido.
// Si la caché está vacía o vencida, se emite uno nuevo.
func (p *ServiceTokenProvider) GetToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Verificar si la caché aún es válida (con 10s de margen)
	now := p.now()
	if p.cached.token != "" && now.Add(10*time.Second).Before(p.cached.expiresAt) {
		return p.cached.token, nil
	}

	// Emitir nuevo token
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	form := "grant_type=client_credentials&client_id=" + url.QueryEscape(p.clientID) +
		"&client_secret=" + url.QueryEscape(p.clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", p.cfg.TokenURL, bytes.NewReader([]byte(form)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		io.ReadAll(resp.Body) // descartar el cuerpo
		return "", idp.ErrProveedorNoDisponible
	}

	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, _ := io.ReadAll(limited)

	var tokenResp tokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return "", idp.ErrProveedorNoDisponible
	}

	if tokenResp.AccessToken == "" || tokenResp.ExpiresIn <= 0 {
		return "", idp.ErrProveedorNoDisponible
	}

	// Cachear el token
	p.cached = serviceToken{
		token:     tokenResp.AccessToken,
		expiresAt: now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	return p.cached.token, nil
}

// Invalidate invalida la caché del token (usado cuando se recibe 401).
func (p *ServiceTokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cached = serviceToken{}
}
