// emission.go implementa el flujo productivo de emisión de sesión (login):
// EmissionSteps sobre el IdP + Postgres, y el EmissionFlow con la opción 1 de
// A-1 aprobada en DEC-088 (emitir primero, compensar en el IdP si falla el
// registro local).
//
// Orden de la opción 1 (DEC-085, A-1):
//  1. Generar el sesion_id propio de AI27 (R1.1), ANTES de llamar al IdP.
//  2. Issue en el IdP con ese sesion_id (el IdP lo transporta al claim).
//  3. Resolver el sujeto de AI27 (A-5) y la expiración a registrar (A-3).
//  4. Register: insertar la fila de sesión con el idp_session_id del token.
//  5. Si Register falla, Compensate: revocar en el IdP la sesión recién emitida
//     (el token quedó vivo en el IdP pero sin fila en AI27), y devolver el error.
//  6. Calentar la caché (best-effort) tras el registro.
package sesion

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// emissionSteps es la implementación productiva de EmissionSteps. Reúne los
// colaboradores que ya viven en el servicio: el IdP, el generador de id, el
// resolvedor de sujeto, la política de expiración, el store y la caché.
type emissionSteps struct {
	s *servicio
}

var _ EmissionSteps = (*emissionSteps)(nil)

// NewSesionID genera un sesion_id nuevo (R1.1), antes de llamar a Issue.
func (e *emissionSteps) NewSesionID() string {
	return e.s.idGenerator()
}

// Issue invoca al IdP con el sesion_id ya generado (R1.1).
func (e *emissionSteps) Issue(ctx context.Context, cred Credentials, sesionID string) (idp.Token, error) {
	return e.s.idp.Issue(ctx, idp.Credentials{
		Usuario:       cred.Usuario,
		Contrasena:    cred.Contrasena,
		SegundoFactor: cred.SegundoFactor,
	}, sesionID)
}

// ResolveSujeto obtiene el sujeto de AI27 del token emitido (A-5).
func (e *emissionSteps) ResolveSujeto(ctx context.Context, tok idp.Token) (SujetoRef, error) {
	return e.s.sujetoResolver.ResolveSujeto(ctx, tok)
}

// ExpiresAtIssue fija la expiración que se registra en la fila (A-3).
func (e *emissionSteps) ExpiresAtIssue(tok idp.Token) (time.Time, error) {
	return e.s.expiryPolicy.AtIssue(tok)
}

// Register inserta la fila de sesión en una transacción propia (R2.1). El
// INSERT traduce el 23505 del índice una_sesion_viva a ErrLiveSesionExists (A-2).
func (e *emissionSteps) Register(ctx context.Context, r SesionRecord) error {
	tx, err := e.s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := newStore(e.s.tx)
	if err := st.insert(ctx, tx, r); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Compensate termina en el IdP la sesión de un token que no se pudo registrar
// (opción 1 de DEC-085). Es idempotente (P13).
func (e *emissionSteps) Compensate(ctx context.Context, idpSessionID string) error {
	if idpSessionID == "" {
		return nil
	}
	return e.s.idp.Revoke(ctx, idpSessionID)
}

// WarmCache calienta el registro caliente tras el commit (best-effort, K-29).
func (e *emissionSteps) WarmCache(ctx context.Context, r SesionRecord) {
	e.s.bestEffortCache.store(ctx, r.SesionID, r.ExpiraEn)
}

// compensatingFlow implementa EmissionFlow con la opción 1 de A-1 (emitir
// primero, compensar si falla el registro). Es el flujo productivo de login.
type compensatingFlow struct{}

// NewCompensatingEmissionFlow devuelve el EmissionFlow productivo (A-1, opción 1).
func NewCompensatingEmissionFlow() EmissionFlow {
	return compensatingFlow{}
}

// Emit ejecuta la secuencia de la opción 1. Mapea los errores del IdP a la
// taxonomía de plataforma: credencial inválida → 401; IdP no disponible → 503.
func (compensatingFlow) Emit(ctx context.Context, steps EmissionSteps, cred Credentials) (Emitida, error) {
	sesionID := steps.NewSesionID() // R1.1: antes de Issue

	tok, err := steps.Issue(ctx, cred, sesionID)
	if err != nil {
		return Emitida{}, mapearErrorIdP(err)
	}

	sujeto, err := steps.ResolveSujeto(ctx, tok)
	if err != nil {
		// El token quedó vivo en el IdP pero no se pudo resolver el sujeto:
		// compensar para no dejar una sesión viva sin fila en AI27.
		_ = steps.Compensate(ctx, tok.IdpSessionID)
		return Emitida{}, fmt.Errorf("emisión: resolver sujeto: %w", err)
	}

	expiraEn, err := steps.ExpiresAtIssue(tok)
	if err != nil {
		_ = steps.Compensate(ctx, tok.IdpSessionID)
		return Emitida{}, fmt.Errorf("emisión: expiración: %w", err)
	}

	rec := SesionRecord{
		SesionID:     sesionID,
		SujetoID:     sujeto.SujetoID,
		ExpiraEn:     expiraEn,
		IdpSessionID: tok.IdpSessionID,
	}
	if err := steps.Register(ctx, rec); err != nil {
		// Opción 1: el IdP ya emitió, pero el registro local falló. Compensar
		// (revocar en el IdP) y devolver un error. La sesión no queda viva en
		// AI27 ni en el IdP.
		_ = steps.Compensate(ctx, tok.IdpSessionID)
		return Emitida{}, fmt.Errorf("emisión: registrar sesión: %w", err)
	}

	// Registro comprometido: calentar la caché (best-effort) y devolver.
	steps.WarmCache(ctx, rec)

	return Emitida{
		SesionID:     sesionID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiraEn:     expiraEn,
	}, nil
}

// mapearErrorIdP traduce los errores centinela del IdP a la taxonomía de
// plataforma para que el handler HTTP responda el status correcto.
func mapearErrorIdP(err error) error {
	switch {
	case stderrors.Is(err, idp.ErrCredencialInvalida):
		return errors.NewNoAutorizado("credencial inválida")
	case stderrors.Is(err, idp.ErrProveedorNoDisponible):
		return errors.NewDegradadoErr(err, "el proveedor de identidad no está disponible")
	default:
		return fmt.Errorf("emisión: %w", err)
	}
}
