// revokeretry.go implementa el IdpRevokeFailureHandler productivo (A-7, opción
// aprobada en DEC-088: reintento local exponencial 1s, 2s, 4s; máximo 3
// intentos; 503 si sigue fallando).
//
// El Paso 1 de la revocación (local + outbox) ya está comprometido cuando este
// handler corre: su trabajo es insistir con el Paso 2 (revocar en el IdP) y, si
// agota los reintentos, devolver un error de categoría Degradado para que el
// handler HTTP responda 503. Nunca revierte el Paso 1 (Requisito 4.5): la
// revocación ya es efectiva en AI27 aunque el IdP no haya confirmado.
package sesion

import (
	"context"
	"fmt"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// esperasReintento son las esperas entre intentos del Paso 2 (A-7): tras el
// primer fallo espera 1s, tras el segundo 2s, tras el tercero 4s. Con 3
// reintentos hay 3 esperas como máximo. El primer intento (el del flujo normal)
// ya ocurrió antes de llamar a este handler.
var esperasReintento = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// idpRevokeRetrier implementa IdpRevokeFailureHandler reintentando Revoke en el
// IdP con espera creciente. Es el handler productivo de A-7.
type idpRevokeRetrier struct {
	prov idp.IdentityProvider
	// sleep espera la duración dada respetando la cancelación del contexto. Se
	// inyecta para que las pruebas no esperen en tiempo real; en producción es
	// sleepConContexto.
	sleep func(ctx context.Context, d time.Duration) error
}

// NewIdpRevokeRetrier construye el handler de reintento del Paso 2 (A-7). Si
// sleep es nil, usa la espera real respetando el contexto.
func NewIdpRevokeRetrier(prov idp.IdentityProvider) IdpRevokeFailureHandler {
	return &idpRevokeRetrier{prov: prov, sleep: sleepConContexto}
}

// OnFailure reintenta el Paso 2. Recibe la causa del primer fallo. Si alguno de
// los reintentos tiene éxito, devuelve nil (la revocación quedó confirmada en el
// IdP). Si se agotan, devuelve un error de categoría Degradado (503) que envuelve
// ErrIdpRevokePending y la última causa.
//
// No reintenta cuando no hay idp_session_id (no se puede llamar a Revoke): en ese
// caso la revocación local ya ocurrió, pero el Paso 2 es imposible; se devuelve
// el error degradado directamente.
func (r *idpRevokeRetrier) OnFailure(ctx context.Context, p PendingIdpRevocation, cause error) error {
	if p.IdpSessionID == "" {
		return degradadoRevocacion(p, cause)
	}

	ultima := cause
	for _, espera := range esperasReintento {
		if err := r.sleep(ctx, espera); err != nil {
			// El contexto se canceló durante la espera: el Paso 1 ya quedó; se
			// reporta como degradado con la causa de cancelación.
			return degradadoRevocacion(p, err)
		}
		if err := r.prov.Revoke(ctx, p.IdpSessionID); err != nil {
			ultima = err
			continue
		}
		// Reintento exitoso: el Paso 2 quedó confirmado.
		return nil
	}
	return degradadoRevocacion(p, ultima)
}

// degradadoRevocacion arma el error de categoría Degradado (503) para un Paso 2
// que no pudo completarse tras los reintentos. Envuelve ErrIdpRevokePending (para
// que errors.Is lo reconozca) y la última causa.
func degradadoRevocacion(p PendingIdpRevocation, cause error) error {
	return errors.NewDegradadoErr(
		fmt.Errorf("%w: %w", ErrIdpRevokePending, cause),
		"la sesión se revocó localmente pero el IdP no confirmó la revocación",
	)
}

// sleepConContexto espera d respetando la cancelación del contexto.
func sleepConContexto(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
