package sesion

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// --- ExpiryPolicy (A-3, opción b) -------------------------------------------

func TestExpiryAtIssueEsLaDelToken(t *testing.T) {
	p := NewExpiryAtRefresh()
	exp := time.Now().Add(2 * time.Hour).UTC()
	got, err := p.AtIssue(idp.Token{ExpiraEn: exp})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(exp) {
		t.Errorf("AtIssue = %v, se esperaba %v", got, exp)
	}
}

func TestExpiryAtRefreshTomaElMayor(t *testing.T) {
	p := NewExpiryAtRefresh()
	actual := time.Now().Add(time.Hour).UTC()

	// Token con expiración MAYOR: extiende.
	mayor := actual.Add(time.Hour)
	got, _ := p.AtRefresh(actual, idp.Token{ExpiraEn: mayor})
	if !got.Equal(mayor) {
		t.Errorf("con token mayor, AtRefresh = %v, se esperaba %v", got, mayor)
	}

	// Token con expiración MENOR: no retrocede, conserva la actual.
	menor := actual.Add(-time.Hour)
	got, _ = p.AtRefresh(actual, idp.Token{ExpiraEn: menor})
	if !got.Equal(actual) {
		t.Errorf("con token menor, AtRefresh = %v, se esperaba %v (no retroceder)", got, actual)
	}
}

// --- IdpRevokeFailureHandler (A-7, reintento) -------------------------------

// idpContador cuenta las llamadas a Revoke y falla las primeras n.
type idpContador struct {
	idp.IdentityProvider
	llamadas int
	fallaseN int // falla las primeras n llamadas, luego éxito
	fallaErr error
}

func (c *idpContador) Revoke(_ context.Context, _ string) error {
	c.llamadas++
	if c.llamadas <= c.fallaseN {
		if c.fallaErr != nil {
			return c.fallaErr
		}
		return idp.ErrProveedorNoDisponible
	}
	return nil
}

// retrierConSleepFalso construye un retrier cuyo sleep no espera en tiempo real.
func retrierConSleepFalso(prov idp.IdentityProvider) *idpRevokeRetrier {
	return &idpRevokeRetrier{
		prov:  prov,
		sleep: func(context.Context, time.Duration) error { return nil },
	}
}

func TestReintentoExitosoAlSegundoIntento(t *testing.T) {
	prov := &idpContador{fallaseN: 1} // falla el 1er reintento, éxito el 2º
	r := retrierConSleepFalso(prov)
	err := r.OnFailure(context.Background(),
		PendingIdpRevocation{SesionID: "s", IdpSessionID: "idp-1"},
		idp.ErrProveedorNoDisponible)
	if err != nil {
		t.Fatalf("se esperaba éxito tras reintentar: %v", err)
	}
	if prov.llamadas != 2 {
		t.Errorf("Revoke se llamó %d veces, se esperaban 2", prov.llamadas)
	}
}

func TestReintentoAgotadoDevuelveDegradado(t *testing.T) {
	prov := &idpContador{fallaseN: 99} // siempre falla
	r := retrierConSleepFalso(prov)
	err := r.OnFailure(context.Background(),
		PendingIdpRevocation{SesionID: "s", IdpSessionID: "idp-1"},
		idp.ErrProveedorNoDisponible)
	if err == nil {
		t.Fatal("se esperaba error tras agotar los reintentos")
	}
	if platformerrors.AsCategory(err) != platformerrors.CategoryDegradado {
		t.Errorf("categoría = %v, se esperaba Degradado (503)", platformerrors.AsCategory(err))
	}
	if !stderrors.Is(err, ErrIdpRevokePending) {
		t.Error("el error debería envolver ErrIdpRevokePending")
	}
	// 3 reintentos (las 3 esperas de A-7).
	if prov.llamadas != len(esperasReintento) {
		t.Errorf("Revoke se llamó %d veces, se esperaban %d", prov.llamadas, len(esperasReintento))
	}
}

func TestReintentoSinIdpSessionIDNoLlamaRevoke(t *testing.T) {
	prov := &idpContador{fallaseN: 99}
	r := retrierConSleepFalso(prov)
	err := r.OnFailure(context.Background(),
		PendingIdpRevocation{SesionID: "s", IdpSessionID: ""},
		ErrMissingIdpSessionID)
	if err == nil {
		t.Fatal("se esperaba error degradado")
	}
	if platformerrors.AsCategory(err) != platformerrors.CategoryDegradado {
		t.Errorf("categoría = %v, se esperaba Degradado", platformerrors.AsCategory(err))
	}
	if prov.llamadas != 0 {
		t.Errorf("sin idp_session_id no debió llamar Revoke; llamó %d veces", prov.llamadas)
	}
}

// --- Cursor por timestamp (A-10, opción b) ----------------------------------

func TestCursorTiempoRoundTrip(t *testing.T) {
	original := time.Date(2026, 10, 14, 15, 4, 5, 123456789, time.UTC)
	cur := codificarCursorTiempo(original)
	got, err := decodificarCursorTiempo(cur)
	if err != nil {
		t.Fatalf("decodificar: %v", err)
	}
	if !got.Equal(original) {
		t.Errorf("round-trip del cursor = %v, se esperaba %v", got, original)
	}
}

func TestCursorTiempoInvalido(t *testing.T) {
	if _, err := decodificarCursorTiempo("no-es-base64-valido-!!!"); err == nil {
		t.Error("se esperaba error ante un cursor ilegible")
	}
}

// --- RefreshResolver (A-4, opción a) ----------------------------------------

func TestRefreshResolverLeeSesionIDDelToken(t *testing.T) {
	r := NewRefreshResolver(func(tok idp.Token) (string, error) {
		return "sesion-del-claim", nil
	})
	got, err := r.SesionIDFor(context.Background(), idp.Token{AccessToken: "at"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "sesion-del-claim" {
		t.Errorf("SesionIDFor = %q, se esperaba sesion-del-claim", got)
	}
}

func TestRefreshResolverTokenSinSesionID(t *testing.T) {
	r := NewRefreshResolver(func(idp.Token) (string, error) { return "", nil })
	_, err := r.SesionIDFor(context.Background(), idp.Token{})
	if !stderrors.Is(err, ErrRefreshUnresolved) {
		t.Errorf("se esperaba ErrRefreshUnresolved, got %v", err)
	}
}
