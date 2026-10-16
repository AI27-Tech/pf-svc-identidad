package sesion

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

// Option configura una costura del servicio (A-1, A-3, A-4, A-5, A-7, A-10, A-12, A-14, A-16).
// Sin opciones, NewServicio construye el mismo servicio que hoy.
type Option func(*servicio)

// WithIDGenerator configura el generador de sesion_id (Requisito 1.1).
// Por omisión: uuid.NewString (v4).
func WithIDGenerator(gen func() string) Option {
	return func(s *servicio) {
		s.idGenerator = gen
	}
}

// WithEmissionFlow configura el flujo de emisión (A-1, DEC-085).
// Sin él, EmitirSesion devuelve errNoImplementado.
func WithEmissionFlow(f EmissionFlow) Option {
	return func(s *servicio) {
		s.emissionFlow = f
	}
}

// WithSujetoResolver configura la resolución del sujeto de AI27 (A-5).
// Sin él, EmitirSesion devuelve errNoImplementado.
func WithSujetoResolver(r SujetoResolver) Option {
	return func(s *servicio) {
		s.sujetoResolver = r
	}
}

// WithExpiryPolicy configura la política de expiración (A-3).
// Sin él, EmitirSesion y RefrescarSesion devuelven errNoImplementado.
func WithExpiryPolicy(p ExpiryPolicy) Option {
	return func(s *servicio) {
		s.expiryPolicy = p
	}
}

// WithRefresh configura el resoledor de sesión refrescada y su modo (A-4).
// Sin él, RefrescarSesion devuelve errNoImplementado.
func WithRefresh(r RefreshResolver, m RevokedRefresh) Option {
	return func(s *servicio) {
		s.refreshResolver = r
		s.revokedRefreshMode = m
	}
}

// WithIdpRevokeFailureHandler configura el manejo de fallos del Paso 2 (A-7).
// Sin él, el error del Paso 2 se devuelve envuelto en ErrIdpRevokePending.
func WithIdpRevokeFailureHandler(h IdpRevokeFailureHandler) Option {
	return func(s *servicio) {
		s.revokeFailureHandler = h
	}
}

// WithRevocationLog configura el snapshot y continuación de tombstones (A-10).
// Sin él, ListarRevocadas usa snapshotOnlyLog (comportamiento de hoy: snapshot sin cursor).
func WithRevocationLog(l RevocationLog) Option {
	return func(s *servicio) {
		s.revocationLog = l
	}
}

// WithAuditRecorder configura la auditoría local (A-12, A-16).
// Sin él, RevocarSesion no registra en cambio_identidad.
func WithAuditRecorder(a AuditRecorder) Option {
	return func(s *servicio) {
		s.auditRecorder = a
	}
}

// WithPayloadEncoder configura la serialización del payload de sesion.revocada (A-14).
// Sin él, usa el codificador por omisión (Requisito 5.1).
func WithPayloadEncoder(e PayloadEncoder) Option {
	return func(s *servicio) {
		s.payloadEncoder = e
	}
}

// WithCacheTimeout configura el timeout de la caché (K-31).
// Cero = solo el ctx del llamador. Sin configurar = 0.
func WithCacheTimeout(d time.Duration) Option {
	return func(s *servicio) {
		s.cacheTimeout = d
	}
}

// DefaultIDGenerator es el generador de sesion_id por omisión: UUID v4.
func DefaultIDGenerator() string {
	return uuid.NewString()
}

// DefaultPayloadEncoder es el codificador de payload por omisión (Requisito 5.1).
// Serializa el Tombstone como JSON con nombres de campo canónicos.
func DefaultPayloadEncoder(t Tombstone) (json.RawMessage, error) {
	return json.Marshal(t)
}
