package sesion

import (
	"context"
	"errors"
	"testing"
	"time"
)

// cacheQueFalla implementa Cache y siempre devuelve error. Verifica que
// bestEffortCache descarta el error y nunca lo propaga al servicio (Requisito 8.3).
type cacheQueFalla struct{}

func (cacheQueFalla) GuardarSesion(ctx context.Context, sesionID string, expiraEn time.Time) error {
	return errors.New("falla simulada de la caché")
}
func (cacheQueFalla) InvalidarSesion(ctx context.Context, sesionID string) error {
	return errors.New("falla simulada de la caché")
}

// cacheQueBloquea implementa Cache y bloquea hasta que el ctx se cancela.
// Verifica que WithCacheTimeout corta la operación (Requisito 8.3).
type cacheQueBloquea struct {
	// liberada se cierra cuando la operación retorna, para que el test sepa
	// que no quedó una goroutine colgada.
	liberada chan struct{}
}

func (c *cacheQueBloquea) GuardarSesion(ctx context.Context, sesionID string, expiraEn time.Time) error {
	<-ctx.Done()
	close(c.liberada)
	return ctx.Err()
}
func (c *cacheQueBloquea) InvalidarSesion(ctx context.Context, sesionID string) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestBestEffortCacheDescartaError: un error de la caché nunca llega al servicio.
// store e invalidate no devuelven error (su firma no lo permite), así que la
// prueba es que no haga panic ni bloquee, y que el comportamiento sea un no-op
// observable (el servicio sigue su curso).
func TestBestEffortCacheDescartaError(t *testing.T) {
	b := newBestEffortCache(cacheQueFalla{}, 0)
	ctx := context.Background()

	// Ninguna de las dos debe propagar ni entrar en pánico.
	b.store(ctx, "sesion-1", time.Now().Add(time.Hour))
	b.invalidate(ctx, "sesion-1")
}

// TestBestEffortCacheNilEsNoOp: con Cache nil, store e invalidate no hacen nada
// y no fallan (comportamiento actual; K-3).
func TestBestEffortCacheNilEsNoOp(t *testing.T) {
	b := newBestEffortCache(nil, 0)
	ctx := context.Background()
	b.store(ctx, "sesion-1", time.Now().Add(time.Hour))
	b.invalidate(ctx, "sesion-1")
}

// TestBestEffortCacheTimeout: con timeout > 0 y una caché que bloquea, la
// operación retorna al vencer el límite, sin colgarse (Requisito 8.3).
func TestBestEffortCacheTimeout(t *testing.T) {
	bloqueante := &cacheQueBloquea{liberada: make(chan struct{})}
	b := newBestEffortCache(bloqueante, 50*time.Millisecond)
	ctx := context.Background()

	inicio := time.Now()
	b.store(ctx, "sesion-1", time.Now().Add(time.Hour))
	transcurrido := time.Since(inicio)

	// Debe haber retornado cerca del timeout (50 ms), no haberse colgado.
	if transcurrido > 2*time.Second {
		t.Errorf("store con timeout tardó demasiado (%v); el timeout no cortó", transcurrido)
	}
	// La caché efectivamente vio su ctx cancelado.
	select {
	case <-bloqueante.liberada:
		// ok: la operación retornó al vencer el timeout
	case <-time.After(2 * time.Second):
		t.Error("la operación de caché no se liberó tras el timeout")
	}
}

// TestBestEffortCacheTimeoutCeroUsaCtxLlamador: con timeout cero, la operación
// se corta cuando el ctx del llamador vence (Requisito 8.3, K-31).
func TestBestEffortCacheTimeoutCeroUsaCtxLlamador(t *testing.T) {
	bloqueante := &cacheQueBloquea{liberada: make(chan struct{})}
	b := newBestEffortCache(bloqueante, 0) // sin timeout propio
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	inicio := time.Now()
	b.store(ctx, "sesion-1", time.Now().Add(time.Hour))
	transcurrido := time.Since(inicio)

	if transcurrido > 2*time.Second {
		t.Errorf("store no respetó el ctx del llamador (%v)", transcurrido)
	}
	select {
	case <-bloqueante.liberada:
	case <-time.After(2 * time.Second):
		t.Error("la operación no se liberó al vencer el ctx del llamador")
	}
}
