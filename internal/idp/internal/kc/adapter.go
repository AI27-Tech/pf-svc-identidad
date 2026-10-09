package kc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// adapter es la implementación del puerto idp.IdentityProvider contra Keycloak.
type adapter struct {
	config Config
	http   *http.Client
	now    func() time.Time
	tel    *telemetría
	rev    *revoker
}

var _ idp.IdentityProvider = (*adapter)(nil)

// New crea un adaptador Keycloak configurado. Valida la configuración antes de
// crear recursos y no hace llamadas de red. El HTTPClient y MeterProvider se
// inicializan con valores por defecto si no están provistos.
func New(cfg Config) (*adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Valor por defecto para RefreshURL si no se proporciona
	if cfg.RefreshURL == "" {
		cfg.RefreshURL = cfg.TokenURL
	}

	// Valor por defecto para HTTPClient
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}

	// Inicializar telemetría (será reemplazada por OTel en tarea 10)
	tel := &telemetría{
		meter:  nil, // se inicializa cuando MeterProvider se carga
		tracer: nil, // se inicializa cuando se carga OTel
	}

	// Inicializar proveedor de token de servicio y revoker
	srvTok := NewServiceTokenProvider(cfg, cfg.HTTPClient)
	rev := newRevoker(nil, srvTok) // el adaptador se asigna después

	adapter := &adapter{
		config: cfg,
		http:   cfg.HTTPClient,
		now:    time.Now,
		tel:    tel,
		rev:    rev,
	}

	// Ahora asignar el adaptador al revoker
	rev.adapter = adapter

	return adapter, nil
}

// Issue emite un token verificando las credenciales contra Keycloak.
func (a *adapter) Issue(ctx context.Context, cred idp.Credentials) (idp.Token, error) {
	const op = "Issue"
	defer a.recordTelemetry(op, ctx)

	// Contexto con timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	// Armar el formulario de credenciales
	body := a.issueForm(cred)

	// Hacer la llamada HTTP
	req, err := http.NewRequestWithContext(timeoutCtx, "POST", a.config.TokenURL, bytes.NewReader(body))
	if err != nil {
		cat, _ := classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.http.Do(req)
	if err != nil {
		cat, _ := classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	defer resp.Body.Close()

	// Leer y limitar la respuesta
	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		cat, _ := classify(op, resp.StatusCode, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}

	// Decodificar y clasificar respuesta
	var tokenResp tokenResponse
	var oauthErr oauthError
	bodyOK := false

	if resp.StatusCode == 200 {
		if err := json.Unmarshal(bodyBytes, &tokenResp); err == nil && tokenResp.AccessToken != "" && tokenResp.ExpiresIn > 0 {
			bodyOK = true
		}
	} else {
		json.Unmarshal(bodyBytes, &oauthErr)
	}

	cat, _ := classify(op, resp.StatusCode, oauthErr.Error, nil, ctx.Err(), timeoutCtx.Err(), bodyOK)
	if cat != exito {
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}

	// Construir Token
	recibidoEn := a.now()
	tok := idp.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiraEn:     recibidoEn.UTC().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	return tok, nil
}

// Refresh renueva un token usando el refresh token.
func (a *adapter) Refresh(ctx context.Context, refreshToken string) (idp.Token, error) {
	const op = "Refresh"
	defer a.recordTelemetry(op, ctx)

	// Contexto con timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()

	// Armar el formulario de refresco
	form := "grant_type=refresh_token&refresh_token=" + url.QueryEscape(refreshToken) +
		"&client_id=" + url.QueryEscape(a.config.ClientID) +
		"&client_secret=" + url.QueryEscape(a.config.ClientSecret)

	req, err := http.NewRequestWithContext(timeoutCtx, "POST", a.config.RefreshURL, bytes.NewReader([]byte(form)))
	if err != nil {
		cat, _ := classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.http.Do(req)
	if err != nil {
		cat, _ := classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	defer resp.Body.Close()

	// Leer y limitar respuesta
	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		cat, _ := classify(op, resp.StatusCode, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}

	// Decodificar y clasificar respuesta
	var tokenResp tokenResponse
	var oauthErr oauthError
	bodyOK := false

	if resp.StatusCode == 200 {
		if err := json.Unmarshal(bodyBytes, &tokenResp); err == nil && tokenResp.AccessToken != "" && tokenResp.ExpiresIn > 0 {
			bodyOK = true
		}
	} else {
		json.Unmarshal(bodyBytes, &oauthErr)
	}

	cat, _ := classify(op, resp.StatusCode, oauthErr.Error, nil, ctx.Err(), timeoutCtx.Err(), bodyOK)
	if cat != exito {
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}

	// Construir Token
	recibidoEn := a.now()
	tok := idp.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiraEn:     recibidoEn.UTC().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	return tok, nil
}

// Revoke revoca la sesión. Implementa la lógica de reintento y clasificación
// según el diseño (tarea 8.2).
func (a *adapter) Revoke(ctx context.Context, sesionID string) error {
	const op = "Revoke"
	defer a.recordTelemetry(op, ctx)

	if a.rev == nil {
		return idp.ErrProveedorNoDisponible
	}

	return a.rev.revoke(ctx, sesionID)
}

// issueForm arma el formulario de credenciales para el endpoint de tokens.
// [Pendiente A-3]: aquí va la costura para agregar el segundo factor (OTP).
func (a *adapter) issueForm(cred idp.Credentials) []byte {
	form := "grant_type=password&username=" + url.QueryEscape(cred.Usuario) +
		"&password=" + url.QueryEscape(cred.Contrasena) +
		"&client_id=" + url.QueryEscape(a.config.ClientID) +
		"&client_secret=" + url.QueryEscape(a.config.ClientSecret)
	// TODO: agregar cred.SegundoFactor al formulario (A-3)
	return []byte(form)
}

// recordTelemetry registra telemetría para la operación.
// La implementación completa viene en la tarea 10.1.
func (a *adapter) recordTelemetry(op string, ctx context.Context) {
	// Provisional: la tarea 10.1 implementará telemetría OTel.
}

// tokenResponse es la estructura de la respuesta OAuth del servidor de tokens.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	// SessionState se decodifica pero no se usa aún (A-4).
	SessionState string `json:"session_state,omitempty"`
}

// oauthError es la estructura de un error OAuth.
type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// telemetría es un placeholder para la telemetría real (tarea 10.1).
type telemetría struct {
	meter  metric.Meter
	tracer trace.Tracer
}
