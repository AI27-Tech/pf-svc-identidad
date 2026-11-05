package sesiontest

import (
	"context"
	"strings"
	"sync"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// SegundoFactorIdP es un doble de idp.IdentityProvider para E4-T5 que modela el
// rechazo del segundo factor tal como lo hace el IdP real, pero SIN validar
// TOTP de verdad (regla anti-OTP de archtest: `identidad` no genera, almacena
// ni valida códigos). El doble no es un generador TOTP: simula la validación
// comparando el código recibido contra un código "vigente" y, opcionalmente, el
// del "periodo anterior" dentro de tolerancia. Eso basta para ejercer el flujo
// de emisión del servicio (rechazo sin residuo, 401 uniforme, IdP caído) sin
// depender de Keycloak.
//
// Cubre la tarea 2.1 de E4-T5:
//   - rechaza con idp.ErrCredencialInvalida si el código falta, difiere del
//     vigente, o es el del periodo anterior cuando la tolerancia es cero;
//   - rechaza con idp.ErrProveedorNoDisponible en modo caída;
//   - cuenta las llamadas a Issue y Revoke;
//   - expone el último código recibido, para verificar que el servicio lo
//     reenvía sin transformarlo (Requisito 4.1).
//
// Vive SOLO en sesiontest (regla E de archtest); ningún paquete de producción
// lo importa.
type SegundoFactorIdP struct {
	mu sync.Mutex

	usuario       string
	contrasena    string
	codigoVigente string
	// codigoAnterior es el código del periodo inmediatamente anterior. El doble
	// lo acepta solo si tolerarAnterior es true (ventana de tolerancia del
	// realm); si está vacío, no hay periodo anterior que tolerar.
	codigoAnterior  string
	tolerarAnterior bool

	caida bool

	// Observabilidad para las aserciones de las pruebas.
	issueCalls   int
	revokeCalls  int
	ultimoCodigo string // último SegundoFactor recibido por Issue
}

var _ idp.IdentityProvider = (*SegundoFactorIdP)(nil)

// ConfigSegundoFactor configura un SegundoFactorIdP. Los campos sin valor toman
// defaults razonables en NewSegundoFactorIdP.
type ConfigSegundoFactor struct {
	Usuario         string
	Contrasena      string
	CodigoVigente   string
	CodigoAnterior  string
	TolerarAnterior bool
}

// NewSegundoFactorIdP construye el doble con una credencial válida registrada.
// Por omisión, el único código aceptado es CodigoVigente (tolerancia cero):
// cualquier otro código, incluido el del periodo anterior, se rechaza como
// credencial inválida, que es el caso "vencido" del Requisito 3.2.
func NewSegundoFactorIdP(cfg ConfigSegundoFactor) *SegundoFactorIdP {
	if cfg.Usuario == "" {
		cfg.Usuario = "sujeto@ai27.test"
	}
	if cfg.Contrasena == "" {
		cfg.Contrasena = "secreto"
	}
	if cfg.CodigoVigente == "" {
		cfg.CodigoVigente = "123456"
	}
	return &SegundoFactorIdP{
		usuario:         cfg.Usuario,
		contrasena:      cfg.Contrasena,
		codigoVigente:   cfg.CodigoVigente,
		codigoAnterior:  cfg.CodigoAnterior,
		tolerarAnterior: cfg.TolerarAnterior,
	}
}

// CredValida devuelve una credencial que el doble acepta (usuario, contraseña y
// código vigente), para la ruta feliz de las pruebas.
func (p *SegundoFactorIdP) CredValida() idp.Credentials {
	p.mu.Lock()
	defer p.mu.Unlock()
	return idp.Credentials{
		Usuario:       p.usuario,
		Contrasena:    p.contrasena,
		SegundoFactor: p.codigoVigente,
	}
}

// Issue valida credencial y segundo factor. El orden de rechazo no revela cuál
// elemento falló: todos los fallos de autenticación devuelven
// idp.ErrCredencialInvalida con Token cero (no enumeración, Requisito 3.3).
func (p *SegundoFactorIdP) Issue(_ context.Context, cred idp.Credentials, sesionID string) (idp.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.issueCalls++
	p.ultimoCodigo = cred.SegundoFactor

	if p.caida {
		return idp.Token{}, idp.ErrProveedorNoDisponible
	}
	if cred.Usuario != p.usuario || cred.Contrasena != p.contrasena {
		return idp.Token{}, idp.ErrCredencialInvalida
	}
	if !p.factorAceptado(cred.SegundoFactor) {
		return idp.Token{}, idp.ErrCredencialInvalida
	}
	return idp.Token{
		AccessToken:  "at-" + sesionID,
		RefreshToken: "rt-" + sesionID,
		IdpSessionID: "idp-" + sesionID,
	}, nil
}

// factorAceptado decide si el código presentado es válido. Debe llamarse con el
// lock tomado. Un código vacío o solo-espacios nunca se acepta (aunque la guarda
// local de DEC-094 ya lo rechaza antes; esto defiende el contrato del doble).
func (p *SegundoFactorIdP) factorAceptado(codigo string) bool {
	if strings.TrimSpace(codigo) == "" {
		return false
	}
	if codigo == p.codigoVigente {
		return true
	}
	if p.tolerarAnterior && p.codigoAnterior != "" && codigo == p.codigoAnterior {
		return true
	}
	return false
}

// Refresh no es el foco de E4-T5; el doble entrega un token nuevo para cualquier
// refresh no vacío, salvo en modo caída.
func (p *SegundoFactorIdP) Refresh(_ context.Context, refreshToken string) (idp.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return idp.Token{}, idp.ErrProveedorNoDisponible
	}
	if refreshToken == "" {
		return idp.Token{}, idp.ErrRefreshInvalido
	}
	return idp.Token{
		AccessToken:  "at-refrescado",
		RefreshToken: "rt-refrescado",
		IdpSessionID: "idp-refrescado",
	}, nil
}

// Revoke cuenta la llamada; es idempotente y no falla salvo en modo caída.
func (p *SegundoFactorIdP) Revoke(_ context.Context, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revokeCalls++
	if p.caida {
		return idp.ErrProveedorNoDisponible
	}
	return nil
}

// SimularCaida activa o desactiva el modo caída (Requisito 5).
func (p *SegundoFactorIdP) SimularCaida(activo bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.caida = activo
}

// IssueCalls devuelve cuántas veces se llamó a Issue.
func (p *SegundoFactorIdP) IssueCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.issueCalls
}

// RevokeCalls devuelve cuántas veces se llamó a Revoke.
func (p *SegundoFactorIdP) RevokeCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.revokeCalls
}

// UltimoCodigoRecibido devuelve el último SegundoFactor que recibió Issue, para
// verificar que el servicio lo reenvió sin transformarlo (Requisito 4.1).
func (p *SegundoFactorIdP) UltimoCodigoRecibido() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ultimoCodigo
}
