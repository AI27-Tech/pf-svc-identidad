package sesion_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// sujetoResolverFijo es un SujetoResolver de prueba que siempre devuelve el
// mismo sujeto. En producción es el ResolverSujeto de E4-T6 (sub → sujeto.id).
type sujetoResolverFijo struct {
	ref sesion.SujetoRef
}

func (r sujetoResolverFijo) ResolveSujeto(context.Context, idp.Token) (sesion.SujetoRef, error) {
	return r.ref, nil
}

// prepararEsquema aplica migraciones y devuelve un pool como svc_identidad,
// junto con una función de limpieza y un helper para ejecutar SQL de siembra.
func prepararEsquema(t *testing.T, ctx context.Context) (*pgxpool.Pool, func(), func(sql string)) {
	t.Helper()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	dbtest.Exec(t, ctx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	dbtest.Exec(t, ctx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	aplicarMigraciones(t, ctx, admin)
	closeAdmin()

	p, closePool := dbtest.PoolAs(t, ctx, role, schema)
	ejecutar := func(sql string) {
		if _, err := p.Exec(ctx, sql); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}
	cerrar := func() {
		closePool()
		admin2, c2 := dbtest.AdminConn(t, ctx)
		defer c2()
		dbtest.Exec(t, ctx, admin2, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, ctx, admin2, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	}
	return p, cerrar, ejecutar
}

// TestEmisionEndToEnd verifica el login productivo contra Postgres (A-1 opción
// 1): EmitirSesion genera el sesion_id, lo pasa al IdP, resuelve el sujeto,
// registra la fila con el idp_session_id del token y devuelve el sesion_id
// generado. La fila queda viva con el sujeto y el idp_session_id correctos.
func TestEmisionEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, cerrar, ejecutar := prepararEsquema(t, ctx)
	defer cerrar()

	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sujetoID := "11111111-1111-1111-1111-111111111111"
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno')`, tenantID))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-sub-1','%s')`, sujetoID, tenantID))

	// RecordingIdP.Issue devuelve IdpSessionID = "idp-"+sesionID y ExpiraEn cero;
	// damos una expiración vía un IdP que envuelve. Para simplicidad usamos un
	// IdP que devuelve una expiración futura.
	prov := &idpEmisor{expira: time.Now().Add(time.Hour).UTC()}

	svc := sesion.NewServicio(prov, pool, outbox.NewPublisher(), nil,
		sesion.WithSujetoResolver(sujetoResolverFijo{ref: sesion.SujetoRef{SujetoID: sujetoID, TenantID: tenantID}}),
		sesion.WithExpiryPolicy(sesion.NewExpiryAtRefresh()),
		sesion.WithEmissionFlow(sesion.NewCompensatingEmissionFlow()),
	)

	em, err := svc.EmitirSesion(ctx, sesion.Credentials{Usuario: "u", Contrasena: "p"})
	if err != nil {
		t.Fatalf("EmitirSesion: %v", err)
	}
	if em.SesionID == "" {
		t.Fatal("la emisión debió devolver un sesion_id")
	}

	// La fila quedó viva con el sujeto y el idp_session_id del token.
	var sujetoFila, idpSID string
	var revocadaEn *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT sujeto_id, idp_session_id, revocada_en FROM identidad.sesion WHERE sesion_id = $1`,
		em.SesionID).Scan(&sujetoFila, &idpSID, &revocadaEn); err != nil {
		t.Fatalf("leer fila de sesión: %v", err)
	}
	if sujetoFila != sujetoID {
		t.Errorf("sujeto_id de la fila = %q, se esperaba %q", sujetoFila, sujetoID)
	}
	if idpSID != "idp-"+em.SesionID {
		t.Errorf("idp_session_id = %q, se esperaba idp-%s", idpSID, em.SesionID)
	}
	if revocadaEn != nil {
		t.Error("la sesión recién emitida no debería estar revocada")
	}
}

// TestRefrescoRechazaSesionRevocada verifica A-4 (Reject): refrescar una sesión
// revocada no devuelve token y responde NoAutorizado (401). También verifica el
// camino feliz: refrescar una sesión viva conserva el sesion_id y actualiza la
// expiración (A-3 b).
func TestRefrescoRechazaSesionRevocada(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, cerrar, ejecutar := prepararEsquema(t, ctx)
	defer cerrar()

	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sujetoID := "11111111-1111-1111-1111-111111111111"
	sesionViva := "33333333-3333-3333-3333-333333333333"
	sesionRevocada := "44444444-4444-4444-4444-444444444444"
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno')`, tenantID))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-sub-1','%s')`, sujetoID, tenantID))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ('%s','%s', now() + interval '1 hour','idp-viva')`, sesionViva, sujetoID))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, revocada_en, idp_session_id) VALUES ('%s','%s', now() + interval '1 hour', now(),'idp-revocada')`, sesionRevocada, sujetoID))

	// El IdP de refresco devuelve un token con expiración extendida; el claim
	// sesion_id lo decide la costura TokenSesionID según el refresh token.
	expiraNueva := time.Now().Add(3 * time.Hour).UTC()
	prov := &idpRefrescador{expira: expiraNueva}
	// La costura mapea el refresh token al sesion_id (en producción lo lee del
	// claim del access token nuevo).
	sesionIDDeToken := func(tok idp.Token) (string, error) {
		return tok.AccessToken, nil // el emisor pone el sesion_id en AccessToken
	}

	svc := sesion.NewServicio(prov, pool, outbox.NewPublisher(), nil,
		sesion.WithExpiryPolicy(sesion.NewExpiryAtRefresh()),
		sesion.WithRefresh(sesion.NewRefreshResolver(sesionIDDeToken), sesion.RevokedRefreshReject),
	)

	// --- Refrescar la sesión REVOCADA: rechazo 401, sin token. ---
	prov.sesionIDObjetivo = sesionRevocada
	_, err := svc.RefrescarSesion(ctx, "refresh-de-revocada")
	if err == nil {
		t.Fatal("refrescar una sesión revocada debió fallar (A-4 Reject)")
	}
	if platformerrors.AsCategory(err) != platformerrors.CategoryNoAutorizado {
		t.Errorf("categoría = %v, se esperaba NoAutorizado (401)", platformerrors.AsCategory(err))
	}

	// --- Refrescar la sesión VIVA: conserva sesion_id, extiende expiración. ---
	prov.sesionIDObjetivo = sesionViva
	em, err := svc.RefrescarSesion(ctx, "refresh-de-viva")
	if err != nil {
		t.Fatalf("refrescar sesión viva: %v", err)
	}
	if em.SesionID != sesionViva {
		t.Errorf("el refresco debió conservar el sesion_id %q, devolvió %q", sesionViva, em.SesionID)
	}
	// La expiración de la fila se actualizó a la nueva (A-3 b).
	var expiraFila time.Time
	if err := pool.QueryRow(ctx,
		`SELECT expira_en FROM identidad.sesion WHERE sesion_id = $1`, sesionViva).Scan(&expiraFila); err != nil {
		t.Fatalf("leer expira_en: %v", err)
	}
	if expiraFila.Before(expiraNueva.Add(-time.Second)) {
		t.Errorf("expira_en no se actualizó: fila=%v, esperada≈%v", expiraFila, expiraNueva)
	}
}

// TestCursorContinuacion verifica el cursor por timestamp (A-10 b): un snapshot
// devuelve cursor; una revocación posterior aparece en Since(cursor); una
// revocación anterior no se repite en la continuación.
func TestCursorContinuacion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, cerrar, ejecutar := prepararEsquema(t, ctx)
	defer cerrar()

	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sujetoID := "11111111-1111-1111-1111-111111111111"
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno')`, tenantID))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-sub-1','%s')`, sujetoID, tenantID))

	s1 := "33333333-3333-3333-3333-333333333333"
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, revocada_en, idp_session_id) VALUES ('%s','%s', now() + interval '2 hours', now() - interval '1 minute','idp-1')`, s1, sujetoID))

	svc := sesion.NewServicio(&idpEmisor{}, pool, outbox.NewPublisher(), nil)

	// Snapshot inicial: trae s1 y un cursor.
	pag, err := svc.ListarRevocadas(ctx, "")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !contieneTombstone(pag.Items, s1) {
		t.Errorf("el snapshot debió incluir s1")
	}
	if pag.CursorSiguiente == nil {
		t.Fatal("el snapshot debió devolver un cursor")
	}
	cursor := *pag.CursorSiguiente

	// Esperar un instante y revocar s2 DESPUÉS del snapshot.
	time.Sleep(10 * time.Millisecond)
	s2 := "55555555-5555-5555-5555-555555555555"
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, revocada_en, idp_session_id) VALUES ('%s','%s', now() + interval '2 hours', now(),'idp-2')`, s2, sujetoID))

	// Since(cursor): trae s2 (posterior) pero NO s1 (anterior al cursor).
	cont, err := svc.ListarRevocadas(ctx, cursor)
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if !contieneTombstone(cont.Items, s2) {
		t.Errorf("la continuación debió incluir s2 (revocada después del cursor)")
	}
	if contieneTombstone(cont.Items, s1) {
		t.Errorf("la continuación NO debió repetir s1 (revocada antes del cursor)")
	}
}

func contieneTombstone(items []sesion.Tombstone, sesionID string) bool {
	for _, tb := range items {
		if tb.SesionID == sesionID {
			return true
		}
	}
	return false
}

// --- Dobles de IdP para las pruebas de emisión y refresco -------------------

// idpEmisor emite tokens con IdpSessionID = "idp-"+sesionID y una expiración
// configurable. El AccessToken lleva el sesion_id para que la costura de
// refresco lo pueda leer.
type idpEmisor struct {
	expira time.Time
}

func (e *idpEmisor) Issue(_ context.Context, _ idp.Credentials, sesionID string) (idp.Token, error) {
	return idp.Token{
		AccessToken:  sesionID,
		RefreshToken: "refresh-" + sesionID,
		IdpSessionID: "idp-" + sesionID,
		ExpiraEn:     e.expira,
	}, nil
}
func (e *idpEmisor) Refresh(context.Context, string) (idp.Token, error) {
	return idp.Token{}, idp.ErrRefreshInvalido
}
func (e *idpEmisor) Revoke(context.Context, string) error { return nil }

// idpRefrescador devuelve en Refresh un token cuyo AccessToken es el sesion_id
// objetivo, para probar la resolución de la fila por claim.
type idpRefrescador struct {
	expira           time.Time
	sesionIDObjetivo string
}

func (r *idpRefrescador) Issue(context.Context, idp.Credentials, string) (idp.Token, error) {
	return idp.Token{}, stderrors.New("no usado")
}
func (r *idpRefrescador) Refresh(context.Context, string) (idp.Token, error) {
	return idp.Token{
		AccessToken:  r.sesionIDObjetivo,
		RefreshToken: "refresh-nuevo",
		IdpSessionID: "idp-" + r.sesionIDObjetivo,
		ExpiraEn:     r.expira,
	}, nil
}
func (r *idpRefrescador) Revoke(context.Context, string) error { return nil }
