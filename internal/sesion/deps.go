package sesion

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// BeginTx abre una transacción de Postgres. Es la frontera transaccional que el
// caso de uso controla explícitamente (patrón Unit of Work): el caso de uso
// decide el commit, no el repositorio. Es el único patrón que se reutiliza del
// repo de referencia (mp-backend-auth), traducido a Go. La cumple *pgxpool.Pool.
type BeginTx interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Cache es la caché caliente de sesión en Redis/Valkey. NUNCA fuente de verdad
// (identidad.md (e)): un fallo o ausencia en la caché nunca decide el estado.
type Cache interface {
	// GuardarSesion escribe la sesión en caché (best-effort).
	GuardarSesion(ctx context.Context, sesionID string, expiraEn time.Time) error
	// InvalidarSesion borra la clave de caché (best-effort) al revocar.
	InvalidarSesion(ctx context.Context, sesionID string) error
}
