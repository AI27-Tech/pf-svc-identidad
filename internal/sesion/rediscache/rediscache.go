// Package rediscache implementa el puerto sesion.Cache sobre Redis (A-11,
// aprobado en DEC-088: Redis en V1). La caché es la caché CALIENTE de sesión;
// NUNCA es fuente de verdad (identidad.md (e), ADR 26 §5): un fallo o ausencia
// en Redis jamás decide el estado de una sesión. El servicio envuelve este
// puerto con bestEffortCache, que descarta los errores; por eso aquí sí se
// devuelven, pero el llamador los trata como best-effort.
//
// Compatible con Redis 7.0+ y con Valkey (fork compatible por protocolo). El
// cliente es github.com/redis/go-redis/v9, cliente oficial de Redis.
package rediscache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// Verificación en tiempo de compilación de que *Cache implementa el puerto.
var _ sesion.Cache = (*Cache)(nil)

// prefijoClave es el espacio de nombres de las claves de sesión en Redis. La
// clave de una sesión es "sesion:{sesion_id}". Mantener el prefijo evita
// colisiones con otras cachés que compartan la misma instancia.
const prefijoClave = "sesion:"

// Cache implementa sesion.Cache sobre Redis. El valor almacenado es la marca de
// expiración de la sesión (RFC 3339); el TTL de la clave se fija al tiempo que
// falta para esa expiración, de modo que Redis purga por sí mismo las sesiones
// ya expiradas sin trabajo adicional.
type Cache struct {
	cliente redis.UniversalClient
}

// New construye el adaptador a partir de un cliente de Redis ya configurado.
// El llamador (la raíz de composición) es dueño del ciclo de vida del cliente.
func New(cliente redis.UniversalClient) *Cache {
	return &Cache{cliente: cliente}
}

// NewFromURL construye el adaptador a partir de una URL de conexión de Redis
// (p. ej. "redis://host:6379/0"). Devuelve también una función de cierre para
// liberar el pool de conexiones. Útil para la raíz de composición.
func NewFromURL(url string) (*Cache, func() error, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, nil, fmt.Errorf("rediscache: URL inválida: %w", err)
	}
	cliente := redis.NewClient(opts)
	c := &Cache{cliente: cliente}
	return c, cliente.Close, nil
}

// clave compone la clave de Redis de una sesión.
func clave(sesionID string) string {
	return prefijoClave + sesionID
}

// GuardarSesion escribe la sesión en la caché con un TTL igual al tiempo que
// falta para su expiración. Si la sesión ya expiró (expiraEn en el pasado), no
// escribe nada: no tiene sentido cachear una sesión muerta. El valor es la
// marca de expiración en RFC 3339 (suficiente para que un consumidor sepa hasta
// cuándo es válida sin volver a Postgres).
func (c *Cache) GuardarSesion(ctx context.Context, sesionID string, expiraEn time.Time) error {
	ttl := time.Until(expiraEn)
	if ttl <= 0 {
		// Sesión ya expirada: nada que cachear. No es error.
		return nil
	}
	if err := c.cliente.Set(ctx, clave(sesionID), expiraEn.UTC().Format(time.RFC3339), ttl).Err(); err != nil {
		return fmt.Errorf("rediscache: guardar sesión: %w", err)
	}
	return nil
}

// InvalidarSesion borra la clave de la sesión al revocar. Borrar una clave que
// no existe no es error (Redis DEL es idempotente): una revocación de una
// sesión que nunca se cacheó es un no-op silencioso.
func (c *Cache) InvalidarSesion(ctx context.Context, sesionID string) error {
	if err := c.cliente.Del(ctx, clave(sesionID)).Err(); err != nil {
		return fmt.Errorf("rediscache: invalidar sesión: %w", err)
	}
	return nil
}

// Ping comprueba la alcanzabilidad de Redis. La raíz de composición la usa para
// el check de /ready (A-11: check de Redis en readiness). Un error significa
// que Redis no responde; como la caché nunca es fuente de verdad, la operación
// puede seguir degradada, pero el check lo refleja para operación.
func (c *Cache) Ping(ctx context.Context) error {
	if err := c.cliente.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("rediscache: ping: %w", err)
	}
	return nil
}
