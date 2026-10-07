// Package memory implementa IdentityProvider en memoria, SOLO para pruebas y
// desarrollo. NUNCA en producción: no verifica firma, no habla con ningún IdP y
// no persiste nada. Existe para que el resto del módulo (sesión, claims, http)
// se pueda construir y probar sin esperar a que ADR 32 elija el IdP concreto.
//
// Es, además, el primer cliente de la suite de contrato `idptest`: el doble
// debe pasarla igual que la pasará el adaptador real.
package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// ErrCredencialInvalida lo devuelve Issue cuando la credencial no corresponde a
// un sujeto conocido del doble. Es un marcador de pruebas, no la taxonomía de
// error de la plataforma (eso lo mapea el handler HTTP).
var ErrCredencialInvalida = errors.New("idp/memory: credencial inválida")

// ErrRefreshInvalido lo devuelve Refresh ante un refresh token desconocido.
var ErrRefreshInvalido = errors.New("idp/memory: refresh token inválido")

// Sujeto es el registro mínimo que el doble conoce para poder emitir. No es el
// modelo de autorización (ese vive en internal/autorizacion); aquí sólo basta
// para simular la emisión.
type Sujeto struct {
	Usuario    string
	Contrasena string
	AccessTTL  time.Duration
}

// Provider es el doble en memoria. Es seguro para uso concurrente.
type Provider struct {
	mu       sync.Mutex
	sujetos  map[string]Sujeto // por Usuario
	refresh  map[string]string // refreshToken -> Usuario
	revocado map[string]bool   // sesionID -> revocada
}

var _ idp.IdentityProvider = (*Provider)(nil)

// New crea un doble vacío.
func New() *Provider {
	return &Provider{
		sujetos:  make(map[string]Sujeto),
		refresh:  make(map[string]string),
		revocado: make(map[string]bool),
	}
}

// Registrar agrega un sujeto conocido al doble (ayuda de pruebas).
func (p *Provider) Registrar(s Sujeto) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.AccessTTL == 0 {
		s.AccessTTL = time.Hour
	}
	p.sujetos[s.Usuario] = s
}

// Issue emite un token simulado si la credencial corresponde a un sujeto
// registrado. El token no está firmado: es un marcador para pruebas.
func (p *Provider) Issue(_ context.Context, cred idp.Credentials) (idp.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sujetos[cred.Usuario]
	if !ok || s.Contrasena != cred.Contrasena {
		return idp.Token{}, ErrCredencialInvalida
	}
	return p.emitir(s), nil
}

// Refresh renueva un token simulado para un refresh token previamente emitido.
func (p *Provider) Refresh(_ context.Context, refreshToken string) (idp.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	usuario, ok := p.refresh[refreshToken]
	if !ok {
		return idp.Token{}, ErrRefreshInvalido
	}
	// El refresh anterior se invalida: el doble rota el refresh token en cada uso.
	delete(p.refresh, refreshToken)
	s := p.sujetos[usuario]
	return p.emitir(s), nil
}

// Revoke marca la sesión como revocada en el doble. Idempotente (P13): revocar
// una sesión ya revocada no es error.
func (p *Provider) Revoke(_ context.Context, sesionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revocado[sesionID] = true
	return nil
}

// Revocada expone, sólo para pruebas, si una sesión fue revocada en el IdP.
func (p *Provider) Revocada(sesionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revocado[sesionID]
}

// emitir construye un token simulado y registra su refresh. Debe llamarse con
// el lock tomado.
func (p *Provider) emitir(s Sujeto) idp.Token {
	refreshToken := uuid.NewString()
	p.refresh[refreshToken] = s.Usuario
	return idp.Token{
		AccessToken:  "memory-access-" + uuid.NewString(),
		RefreshToken: refreshToken,
		ExpiraEn:     time.Now().UTC().Add(s.AccessTTL),
	}
}
