// refresh.go implementa el refresco productivo de sesión (A-4). Decisiones de
// DEC-088 aplicadas:
//   - A-4 (resolución de la fila): opción a — el sesion_id sale del claim del
//     access token nuevo que devuelve el IdP (la extensión de E4-T1 lo
//     transporta estable entre refrescos). Se lee con la costura TokenSesionID.
//   - A-4 (sesión revocada): Reject — identidad rechaza el refresco de una
//     sesión ya revocada; no devuelve token nuevo.
//   - A-3 (expiración): opción b — el refresco actualiza expira_en de la fila
//     (ExpiryPolicy.AtRefresh).
//
// La reevaluación de claims en el refresco es E4-T4, fuera de alcance aquí.
package sesion

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// TokenSesionID extrae el sesion_id del access token emitido por el IdP. El
// idp.Token no expone el claim directamente: el sesion_id viaja dentro del
// AccessToken (lo pone la extensión de E4-T1). La costura permite leerlo
// verificando el token con auth, sin acoplar el paquete sesion a auth.
type TokenSesionID func(tok idp.Token) (sesionID string, err error)

// claimRefreshResolver implementa RefreshResolver con la opción a de A-4: el
// sesion_id del claim del access token nuevo.
type claimRefreshResolver struct {
	sesionIDDeToken TokenSesionID
}

// NewRefreshResolver construye el RefreshResolver productivo (A-4, a). La costura
// sesionIDDeToken lee el claim del access token.
func NewRefreshResolver(sesionIDDeToken TokenSesionID) RefreshResolver {
	return &claimRefreshResolver{sesionIDDeToken: sesionIDDeToken}
}

// SesionIDFor devuelve el sesion_id del token refrescado.
func (r *claimRefreshResolver) SesionIDFor(_ context.Context, tok idp.Token) (string, error) {
	if r.sesionIDDeToken == nil {
		return "", fmt.Errorf("%w: costura TokenSesionID no configurada", ErrRefreshUnresolved)
	}
	sesionID, err := r.sesionIDDeToken(tok)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRefreshUnresolved, err)
	}
	if sesionID == "" {
		return "", fmt.Errorf("%w: el token no trae sesion_id", ErrRefreshUnresolved)
	}
	return sesionID, nil
}

// refrescar ejecuta el refresco productivo. Secuencia:
//  1. Refresh en el IdP (token nuevo, mismo sesion_id en el claim).
//  2. Resolver el sesion_id del token (A-4 a).
//  3. En una tx: bloquear la fila (FOR UPDATE); si está revocada, Reject
//     (A-4); si no, actualizar expira_en (A-3 b). No se inserta fila nueva.
//  4. Calentar la caché (best-effort).
func (s *servicio) refrescar(ctx context.Context, refreshToken string) (Emitida, error) {
	tok, err := s.idp.Refresh(ctx, refreshToken)
	if err != nil {
		return Emitida{}, mapearErrorRefresco(err)
	}

	sesionID, err := s.refreshResolver.SesionIDFor(ctx, tok)
	if err != nil {
		return Emitida{}, err
	}

	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return Emitida{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := newStore(s.tx)
	actual, revocada, err := st.lockSesionParaRefresco(ctx, tx, sesionID)
	if err != nil {
		return Emitida{}, err
	}
	if revocada {
		// A-4 Reject: no se refresca una sesión revocada. 401, el cliente debe
		// reautenticarse. El token que el IdP acaba de emitir no se devuelve.
		return Emitida{}, errors.NewNoAutorizado("la sesión fue revocada")
	}

	nuevaExpira, err := s.expiryPolicy.AtRefresh(actual, tok)
	if err != nil {
		return Emitida{}, fmt.Errorf("refresco: expiración: %w", err)
	}
	if err := st.actualizarExpiracion(ctx, tx, sesionID, nuevaExpira); err != nil {
		return Emitida{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Emitida{}, err
	}

	s.bestEffortCache.store(ctx, sesionID, nuevaExpira)

	return Emitida{
		SesionID:     sesionID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiraEn:     nuevaExpira,
	}, nil
}

// mapearErrorRefresco traduce los errores del IdP en el refresco a la taxonomía
// de plataforma: refresh inválido → 401; IdP no disponible → 503.
func mapearErrorRefresco(err error) error {
	switch {
	case stderrors.Is(err, idp.ErrRefreshInvalido):
		return errors.NewNoAutorizado("refresh token inválido")
	case stderrors.Is(err, idp.ErrProveedorNoDisponible):
		return errors.NewDegradadoErr(err, "el proveedor de identidad no está disponible")
	default:
		return fmt.Errorf("refresco: %w", err)
	}
}

// lockSesionParaRefresco bloquea la fila de la sesión (FOR UPDATE) y devuelve su
// expiración actual y si está revocada. Error ErrNoEncontrada si no existe.
func (st *store) lockSesionParaRefresco(ctx context.Context, tx pgx.Tx, sesionID string) (expiraActual time.Time, revocada bool, err error) {
	var revocadaEn *time.Time
	err = tx.QueryRow(ctx,
		`SELECT expira_en, revocada_en
		   FROM identidad.sesion
		  WHERE sesion_id = $1
		  FOR UPDATE`,
		sesionID,
	).Scan(&expiraActual, &revocadaEn)
	if stderrors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, ErrNoEncontrada
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("refresco: bloquear fila: %w", err)
	}
	return expiraActual, revocadaEn != nil, nil
}

// actualizarExpiracion fija expira_en de la fila (A-3 b). No toca otras columnas.
func (st *store) actualizarExpiracion(ctx context.Context, tx pgx.Tx, sesionID string, expiraEn time.Time) error {
	_, err := tx.Exec(ctx,
		`UPDATE identidad.sesion SET expira_en = $2 WHERE sesion_id = $1`,
		sesionID, expiraEn)
	if err != nil {
		return fmt.Errorf("refresco: actualizar expiración: %w", err)
	}
	return nil
}
