package sesiontest

import (
	"context"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// FailingIdP implementa idp.IdentityProvider para pruebas.
// [Propuesta de Kiro, no aprobada]
// Permite inyectar fallos controlados en cada operación.
type FailingIdP struct {
	IssueFn   func(ctx context.Context, cred idp.Credentials, sesionID string) (idp.Token, error)
	RefreshFn func(ctx context.Context, refreshToken string) (idp.Token, error)
	RevokeFn  func(ctx context.Context, idpSessionID string) error
}

// Compile-time check que implementa la interfaz
var _ idp.IdentityProvider = (*FailingIdP)(nil)

// Issue implementa idp.IdentityProvider.Issue
func (f *FailingIdP) Issue(ctx context.Context, cred idp.Credentials, sesionID string) (idp.Token, error) {
	if f.IssueFn != nil {
		return f.IssueFn(ctx, cred, sesionID)
	}
	return idp.Token{}, idp.ErrProveedorNoDisponible
}

// Refresh implementa idp.IdentityProvider.Refresh
func (f *FailingIdP) Refresh(ctx context.Context, refreshToken string) (idp.Token, error) {
	if f.RefreshFn != nil {
		return f.RefreshFn(ctx, refreshToken)
	}
	return idp.Token{}, idp.ErrProveedorNoDisponible
}

// Revoke implementa idp.IdentityProvider.Revoke
func (f *FailingIdP) Revoke(ctx context.Context, idpSessionID string) error {
	if f.RevokeFn != nil {
		return f.RevokeFn(ctx, idpSessionID)
	}
	return idp.ErrProveedorNoDisponible
}

// RecordingIdP implementa idp.IdentityProvider y registra cada llamada.
// [Propuesta de Kiro, no aprobada]
type RecordingIdP struct {
	IssueCalls   []RecordedIssueCall
	RefreshCalls []RecordedRefreshCall
	RevokeCalls  []RecordedRevokeCall
}

// RecordedIssueCall es un registro de una llamada a Issue.
type RecordedIssueCall struct {
	Credentials idp.Credentials
	SesionID    string
	Result      idp.Token
	Error       error
}

// RecordedRefreshCall es un registro de una llamada a Refresh.
type RecordedRefreshCall struct {
	RefreshToken string
	Result       idp.Token
	Error        error
}

// RecordedRevokeCall es un registro de una llamada a Revoke.
type RecordedRevokeCall struct {
	IdpSessionID string
	Error        error
}

// Compile-time check que implementa la interfaz
var _ idp.IdentityProvider = (*RecordingIdP)(nil)

// Issue implementa idp.IdentityProvider.Issue
func (r *RecordingIdP) Issue(ctx context.Context, cred idp.Credentials, sesionID string) (idp.Token, error) {
	tok := idp.Token{
		AccessToken:  "access-" + sesionID,
		RefreshToken: "refresh-" + sesionID,
		IdpSessionID: "idp-" + sesionID,
	}
	call := RecordedIssueCall{
		Credentials: cred,
		SesionID:    sesionID,
		Result:      tok,
		Error:       nil,
	}
	r.IssueCalls = append(r.IssueCalls, call)
	return tok, nil
}

// Refresh implementa idp.IdentityProvider.Refresh
func (r *RecordingIdP) Refresh(ctx context.Context, refreshToken string) (idp.Token, error) {
	tok := idp.Token{
		AccessToken:  "access-new",
		RefreshToken: "refresh-new",
		IdpSessionID: "idp-new",
	}
	call := RecordedRefreshCall{
		RefreshToken: refreshToken,
		Result:       tok,
		Error:        nil,
	}
	r.RefreshCalls = append(r.RefreshCalls, call)
	return tok, nil
}

// Revoke implementa idp.IdentityProvider.Revoke
func (r *RecordingIdP) Revoke(ctx context.Context, idpSessionID string) error {
	call := RecordedRevokeCall{
		IdpSessionID: idpSessionID,
		Error:        nil,
	}
	r.RevokeCalls = append(r.RevokeCalls, call)
	return nil
}
