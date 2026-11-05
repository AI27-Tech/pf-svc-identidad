package sesion

import (
	"context"
	"time"
)

// bestEffortCache envuelve Cache (del puerto). Nunca devuelve error al servicio
// y nunca se consulta para decidir estado: Cache no tiene método de lectura
// (Requisito 8.2). Los errores de la caché se descartan explícitamente (K-8).
type bestEffortCache struct {
	c       Cache         // nil = sin caché (comportamiento actual; K-3 abierta)
	timeout time.Duration // K-31; cero = solo el ctx del llamador
}

// newBestEffortCache devuelve una envoltura best-effort.
func newBestEffortCache(c Cache, timeout time.Duration) bestEffortCache {
	return bestEffortCache{c: c, timeout: timeout}
}

// store intenta guardar en la caché sin bloquear, best-effort (K-8, K-29).
// Los errores se descartan con un comentario explícito.
func (b bestEffortCache) store(ctx context.Context, sesionID string, expiraEn time.Time) {
	if b.c == nil {
		return
	}
	// Aplicar timeout si está configurado (K-31). Con timeout en cero, una caché
	// que no responde bloquea hasta que venza el ctx del llamador (R8.3).
	storeCtx := ctx
	if b.timeout > 0 {
		var cancel context.CancelFunc
		storeCtx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}
	// Descarte explícito del error: la caché es best-effort (K-8). Un contador
	// de fallos de caché sería útil para observabilidad (propuesta de E4-T3 R10,
	// mismo patrón que E4-T1 R10), pero el nombre no está fijado; se omite.
	_ = b.c.GuardarSesion(storeCtx, sesionID, expiraEn)
}

// invalidate intenta invalidar sin bloquear, best-effort (K-8).
// Los errores se descartan.
func (b bestEffortCache) invalidate(ctx context.Context, sesionID string) {
	if b.c == nil {
		return
	}
	invalidateCtx := ctx
	if b.timeout > 0 {
		var cancel context.CancelFunc
		invalidateCtx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}
	_ = b.c.InvalidarSesion(invalidateCtx, sesionID)
}
