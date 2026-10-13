package sesiontest

import (
	"context"
	"errors"
	"sync"
)

// RecordingCache implementa sesion.Cache y registra cada llamada.
// [Propuesta de Kiro, no aprobada]
type RecordingCache struct {
	calls []RecordedCacheCall
	mu    sync.Mutex
}

// RecordedCacheCall es un registro de una llamada a Cache.
type RecordedCacheCall struct {
	Method   string // "GuardarSesion" o "InvalidarSesion"
	SesionID string
	Error    error
}

// NewRecordingCache construye un RecordingCache vacío.
func NewRecordingCache() *RecordingCache {
	return &RecordingCache{
		calls: []RecordedCacheCall{},
	}
}

// GuardarSesion implementa sesion.Cache.GuardarSesion
func (r *RecordingCache) GuardarSesion(ctx context.Context, sesionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, RecordedCacheCall{
		Method:   "GuardarSesion",
		SesionID: sesionID,
		Error:    nil,
	})
	return nil
}

// InvalidarSesion implementa sesion.Cache.InvalidarSesion
func (r *RecordingCache) InvalidarSesion(ctx context.Context, sesionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, RecordedCacheCall{
		Method:   "InvalidarSesion",
		SesionID: sesionID,
		Error:    nil,
	})
	return nil
}

// Calls devuelve el registro de llamadas.
func (r *RecordingCache) Calls() []RecordedCacheCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	calls := make([]RecordedCacheCall, len(r.calls))
	copy(calls, r.calls)
	return calls
}

// FailingCache implementa sesion.Cache y siempre falla.
// [Propuesta de Kiro, no aprobada]
// Usado para verificar que la caché caída no afecta la revocación (Requisito 8.4).
type FailingCache struct {
	err error
}

// NewFailingCache construye un FailingCache que siempre falla con el error dado.
func NewFailingCache(msg string) *FailingCache {
	return &FailingCache{err: errors.New(msg)}
}

// GuardarSesion implementa sesion.Cache.GuardarSesion (siempre falla)
func (f *FailingCache) GuardarSesion(ctx context.Context, sesionID string) error {
	return f.err
}

// InvalidarSesion implementa sesion.Cache.InvalidarSesion (siempre falla)
func (f *FailingCache) InvalidarSesion(ctx context.Context, sesionID string) error {
	return f.err
}

// InMemoryCache implementa sesion.Cache con almacenamiento en memoria.
// [Propuesta de Kiro, no aprobada]
// Usado para verificar que la caché en memoria funciona correctamente.
type InMemoryCache struct {
	store map[string]bool
	mu    sync.RWMutex
}

// NewInMemoryCache construye un InMemoryCache vacío.
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		store: make(map[string]bool),
	}
}

// GuardarSesion implementa sesion.Cache.GuardarSesion
func (c *InMemoryCache) GuardarSesion(ctx context.Context, sesionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.store[sesionID] = true
	return nil
}

// InvalidarSesion implementa sesion.Cache.InvalidarSesion
func (c *InMemoryCache) InvalidarSesion(ctx context.Context, sesionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.store, sesionID)
	return nil
}

// Contains devuelve true si el sesion_id está en la caché.
func (c *InMemoryCache) Contains(sesionID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.store[sesionID]
}
