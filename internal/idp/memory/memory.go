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
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

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
	caida    bool              // si true, todas las ops devuelven ErrProveedorNoDisponible
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
// registrado. El token no está firmado: es un marcador para pruebas. El
// `sesionID` lo genera el llamador (E4-T3); el doble lo acepta pero no lo usa:
// no simula el transporte del claim, sólo cumple la firma del puerto.
func (p *Provider) Issue(_ context.Context, cred idp.Credentials, sesionID string) (idp.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return idp.Token{}, idp.ErrProveedorNoDisponible
	}
	s, ok := p.sujetos[cred.Usuario]
	if !ok || s.Contrasena != cred.Contrasena {
		return idp.Token{}, idp.ErrCredencialInvalida
	}
	return p.emitir(s), nil
}

// Refresh renueva un token simulado para un refresh token previamente emitido.
func (p *Provider) Refresh(_ context.Context, refreshToken string) (idp.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return idp.Token{}, idp.ErrProveedorNoDisponible
	}
	usuario, ok := p.refresh[refreshToken]
	if !ok {
		return idp.Token{}, idp.ErrRefreshInvalido
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
	if p.caida {
		return idp.ErrProveedorNoDisponible
	}
	p.revocado[sesionID] = true
	return nil
}

// SimularCaida activa o desactiva el modo de caída. Cuando está activo,
// Issue, Refresh y Revoke devuelven ErrProveedorNoDisponible y Token{}.
// Protegido para uso concurrente.
func (p *Provider) SimularCaida(activo bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.caida = activo
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
		// El doble no habla con ningún IdP; devuelve un id de sesión simulado
		// para que el Token siempre traiga IdpSessionID, como el adaptador real.
		IdpSessionID: "memory-sid-" + uuid.NewString(),
	}
}
