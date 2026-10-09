package kc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

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

	// Inicializar telemetría OTel. Si MeterProvider es nil, usa el proveedor
	// global (H6). Un error al crear instrumentos degrada a no-op (R10.6).
	tel := nuevaTelemetria(cfg.MeterProvider)

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

	// Telemetría: una sola medición por llamada, con la categoría final (R10.1,
	// R10.2). El span cliente y el histograma se cierran en el defer.
	inicio := a.now()
	spanCtx, span := a.tel.iniciarSpan(ctx, op)
	cat := proveedorNoDisponible
	var causa causaTransporte
	defer func() { a.tel.record(spanCtx, span, op, cat, causa, inicio) }()

	// Contexto con timeout
	timeoutCtx, cancel := context.WithTimeout(spanCtx, a.config.Timeout)
	defer cancel()

	// Armar el formulario de credenciales
	body := a.issueForm(cred)

	// Hacer la llamada HTTP
	req, err := http.NewRequestWithContext(timeoutCtx, "POST", a.config.TokenURL, bytes.NewReader(body))
	if err != nil {
		cat, causa = classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.http.Do(req)
	if err != nil {
		cat, causa = classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	defer resp.Body.Close()

	// Leer y limitar la respuesta
	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		cat, causa = classify(op, resp.StatusCode, "", err, ctx.Err(), timeoutCtx.Err(), false)
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

	cat, causa = classify(op, resp.StatusCode, oauthErr.Error, nil, ctx.Err(), timeoutCtx.Err(), bodyOK)
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

	inicio := a.now()
	spanCtx, span := a.tel.iniciarSpan(ctx, op)
	cat := proveedorNoDisponible
	var causa causaTransporte
	defer func() { a.tel.record(spanCtx, span, op, cat, causa, inicio) }()

	// Contexto con timeout
	timeoutCtx, cancel := context.WithTimeout(spanCtx, a.config.Timeout)
	defer cancel()

	// Armar el formulario de refresco
	form := "grant_type=refresh_token&refresh_token=" + url.QueryEscape(refreshToken) +
		"&client_id=" + url.QueryEscape(a.config.ClientID) +
		"&client_secret=" + url.QueryEscape(a.config.ClientSecret)

	req, err := http.NewRequestWithContext(timeoutCtx, "POST", a.config.RefreshURL, bytes.NewReader([]byte(form)))
	if err != nil {
		cat, causa = classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.http.Do(req)
	if err != nil {
		cat, causa = classify(op, 0, "", err, ctx.Err(), timeoutCtx.Err(), false)
		return idp.Token{}, errorDe(op, cat, timeoutCtx.Err())
	}
	defer resp.Body.Close()

	// Leer y limitar respuesta
	limited := io.LimitReader(resp.Body, 64*1024)
	bodyBytes, err := io.ReadAll(limited)
	if err != nil {
		cat, causa = classify(op, resp.StatusCode, "", err, ctx.Err(), timeoutCtx.Err(), false)
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

	cat, causa = classify(op, resp.StatusCode, oauthErr.Error, nil, ctx.Err(), timeoutCtx.Err(), bodyOK)
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

	inicio := a.now()
	spanCtx, span := a.tel.iniciarSpan(ctx, op)
	cat := proveedorNoDisponible
	var causa causaTransporte
	defer func() { a.tel.record(spanCtx, span, op, cat, causa, inicio) }()

	if a.rev == nil {
		return idp.ErrProveedorNoDisponible
	}

	err := a.rev.revoke(spanCtx, sesionID)
	cat = categoriaDeRevoke(err)
	return err
}

// categoriaDeRevoke mapea el resultado de revoke a la categoría de telemetría.
// Un Revoke exitoso (incluido el idempotente) es `exito`; todo lo demás es
// `proveedor_no_disponible`, nunca credencial ni refresh. El error del
// resolvedor no es un centinela del puerto y también cuenta como no disponible
// a efectos de la señal, sin alterar el error que ve el llamador.
func categoriaDeRevoke(err error) categoria {
	if err == nil {
		return exito
	}
	return proveedorNoDisponible
}

// issueForm arma el formulario de credenciales para el endpoint de tokens.
//
// A-3 (DEC-071): el segundo factor de V1 es TOTP, nativo de Keycloak y
// compatible con el grant directo (ROPC). Keycloak lee el código de un paso de
// la cuenta desde el parámetro de formulario `totp` cuando el flujo de grant
// directo tiene el ejecutor OTP en REQUIRED. Si `SegundoFactor` viene vacío, no
// se agrega el parámetro: la emisión fallará con credencial inválida contra un
// realm con OTP REQUIRED, que es el comportamiento esperado.
func (a *adapter) issueForm(cred idp.Credentials) []byte {
	form := "grant_type=password&username=" + url.QueryEscape(cred.Usuario) +
		"&password=" + url.QueryEscape(cred.Contrasena) +
		"&client_id=" + url.QueryEscape(a.config.ClientID) +
		"&client_secret=" + url.QueryEscape(a.config.ClientSecret)
	if cred.SegundoFactor != "" {
		form += "&totp=" + url.QueryEscape(cred.SegundoFactor)
	}
	return []byte(form)
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
