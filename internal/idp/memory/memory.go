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
//
// SegundoFactor es el código ESPERADO del segundo factor (DEC-095, C-3, opción
// b). El doble NO valida TOTP real —no genera ni verifica códigos de tiempo, eso
// es del IdP real (regla anti-OTP de archtest)—: solo compara el código recibido
// contra este valor esperado, igual que compara la contraseña. Un sujeto cuyo
// SegundoFactor queda vacío NO puede emitir: el doble dejó de emitir sin factor,
// que es justo lo que DEC-095 cierra (falla en abierto por omisión). Registrar
// un sujeto que pueda iniciar sesión exige, por tanto, configurar su factor.
type Sujeto struct {
	Usuario       string
	Contrasena    string
	SegundoFactor string
	AccessTTL     time.Duration
}

// usuarioAdmin es el estado que el doble guarda de un usuario administrado por
// IdpAdmin (alta de E4-T6), para que las pruebas puedan inspeccionarlo.
type usuarioAdmin struct {
	IdpSubject string
	Usuario    string
	TenantID   string
	Grupo      string // grupo de rol actual
	Habilitado bool
}

// Provider es el doble en memoria. Es seguro para uso concurrente. Implementa
// idp.IdentityProvider (emisión/revocación) e idp.IdpAdmin (administración de
// usuarios y grupos de E4-T6): el mismo doble cubre ambos puertos.
type Provider struct {
	mu       sync.Mutex
	sujetos  map[string]Sujeto // por Usuario
	refresh  map[string]string // refreshToken -> Usuario
	revocado map[string]bool   // sesionID -> revocada
	caida    bool              // si true, todas las ops devuelven ErrProveedorNoDisponible

	// Estado de IdpAdmin.
	admin      map[string]*usuarioAdmin // por idp_subject
	porUsuario map[string]string        // Usuario -> idp_subject (idempotencia del alta)
}

var (
	_ idp.IdentityProvider = (*Provider)(nil)
	_ idp.IdpAdmin         = (*Provider)(nil)
)

// New crea un doble vacío.
func New() *Provider {
	return &Provider{
		sujetos:    make(map[string]Sujeto),
		refresh:    make(map[string]string),
		revocado:   make(map[string]bool),
		admin:      make(map[string]*usuarioAdmin),
		porUsuario: make(map[string]string),
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
// registrado Y el segundo factor coincide con el esperado (DEC-095). El token
// no está firmado: es un marcador para pruebas. El `sesionID` lo genera el
// llamador (E4-T3); el doble lo acepta pero no lo usa: no simula el transporte
// del claim, sólo cumple la firma del puerto.
//
// El doble EXIGE segundo factor (DEC-095, C-3, opción b): un código vacío, que
// no coincide con el esperado, o un sujeto sin factor configurado, se rechazan
// con ErrCredencialInvalida —el mismo error que una contraseña errónea, para no
// enumerar—. Así el doble deja de ser una vía que emite sesiones sin factor.
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
	// Segundo factor obligatorio (DEC-095): el esperado del sujeto no puede
	// estar vacío y el recibido debe coincidir. Sin esto, el doble emitiría sin
	// factor, que es la vía de falla en abierto que la decisión cierra.
	if s.SegundoFactor == "" || cred.SegundoFactor != s.SegundoFactor {
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

// CrearUsuario da de alta a un usuario administrado y devuelve su idp_subject
// opaco. Idempotente por Usuario: si ya existe, devuelve el mismo idp_subject
// sin crear otro (como el adaptador real, que no duplica usuarios).
func (p *Provider) CrearUsuario(_ context.Context, u idp.NuevoUsuario) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return "", idp.ErrProveedorNoDisponible
	}
	if sub, ok := p.porUsuario[u.Usuario]; ok {
		return sub, nil // idempotente
	}
	sub := "memory-sub-" + uuid.NewString()
	p.admin[sub] = &usuarioAdmin{
		IdpSubject: sub,
		Usuario:    u.Usuario,
		TenantID:   u.TenantID,
		Grupo:      u.Rol,
		Habilitado: true,
	}
	p.porUsuario[u.Usuario] = sub
	return sub, nil
}

// DeshabilitarUsuario deja al usuario sin poder autenticarse. Idempotente.
func (p *Provider) DeshabilitarUsuario(_ context.Context, idpSubject string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return idp.ErrProveedorNoDisponible
	}
	if u, ok := p.admin[idpSubject]; ok {
		u.Habilitado = false
	}
	return nil
}

// HabilitarUsuario revierte la deshabilitación. Idempotente.
func (p *Provider) HabilitarUsuario(_ context.Context, idpSubject string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return idp.ErrProveedorNoDisponible
	}
	if u, ok := p.admin[idpSubject]; ok {
		u.Habilitado = true
	}
	return nil
}

// MoverDeGrupo quita al usuario del grupo del rol anterior y lo pone en el
// nuevo, EN ESE ORDEN (DEC-084): el doble nunca deja al usuario en dos grupos a
// la vez, porque guarda un único grupo actual. Un rolAnterior vacío es solo
// agregar.
func (p *Provider) MoverDeGrupo(_ context.Context, idpSubject, _ /*rolAnterior*/, rolNuevo string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.caida {
		return idp.ErrProveedorNoDisponible
	}
	if u, ok := p.admin[idpSubject]; ok {
		u.Grupo = rolNuevo
	}
	return nil
}

// UsuarioAdmin expone, sólo para pruebas, el estado administrado de un usuario.
func (p *Provider) UsuarioAdmin(idpSubject string) (sub, usuario, tenant, grupo string, habilitado, existe bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, ok := p.admin[idpSubject]
	if !ok {
		return "", "", "", "", false, false
	}
	return u.IdpSubject, u.Usuario, u.TenantID, u.Grupo, u.Habilitado, true
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
