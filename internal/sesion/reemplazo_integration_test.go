package sesion_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// idpQueGrabaRevoke es un idpEmisor que registra los idp_session_id revocados.
type idpQueGrabaRevoke struct {
	idpEmisor
	mu        sync.Mutex
	revocadas []string
}

func (p *idpQueGrabaRevoke) Revoke(_ context.Context, idpSessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revocadas = append(p.revocadas, idpSessionID)
	return nil
}

// TestEmisionReemplazaSesionViva verifica DEC-138 contra Postgres: una sesión
// expirada sin revocar no bloquea el login, un segundo login revoca y reemplaza
// la sesión viva (con su `sesion.revocada` y su cierre en el IdP), y dos logins
// simultáneos del mismo sujeto terminan con exactamente una sesión viva.
func TestEmisionReemplazaSesionViva(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, cerrar, ejecutar := prepararEsquema(t, ctx)
	defer cerrar()

	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sujetoID := "11111111-1111-1111-1111-111111111111"
	expirada := "22222222-2222-4222-8222-222222222222"
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno')`, tenantID))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-sub-1','%s')`, sujetoID, tenantID))
	// Quien cerró el navegador: sesión expirada que nunca se revocó.
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id)
		VALUES ('%s','%s', now() - interval '1 hour', 'idp-expirada')`, expirada, sujetoID))

	prov := &idpQueGrabaRevoke{idpEmisor: idpEmisor{expira: time.Now().Add(time.Hour).UTC()}}
	svc := sesion.NewServicio(prov, pool, outbox.NewPublisher(), nil,
		sesion.WithSujetoResolver(sujetoResolverFijo{ref: sesion.SujetoRef{SujetoID: sujetoID, TenantID: tenantID}}),
		sesion.WithExpiryPolicy(sesion.NewExpiryAtRefresh()),
		sesion.WithEmissionFlow(sesion.NewCompensatingEmissionFlow()),
	)
	cred := sesion.Credentials{Usuario: "u", Contrasena: "p", SegundoFactor: "123456"}

	primera, err := svc.EmitirSesion(ctx, cred)
	if err != nil {
		t.Fatalf("login con sesión expirada sin revocar: %v", err)
	}
	segunda, err := svc.EmitirSesion(ctx, cred)
	if err != nil {
		t.Fatalf("segundo login con sesión viva: %v", err)
	}

	for _, id := range []string{expirada, primera.SesionID} {
		var revocada bool
		if err := pool.QueryRow(ctx, `SELECT revocada_en IS NOT NULL FROM identidad.sesion WHERE sesion_id = $1`, id).Scan(&revocada); err != nil {
			t.Fatalf("leer sesión %s: %v", id, err)
		}
		if !revocada {
			t.Errorf("la sesión %s debió quedar revocada", id)
		}
		var eventos int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM identidad.outbox WHERE tipo = 'sesion.revocada' AND agregado_id = $1`, id).Scan(&eventos); err != nil {
			t.Fatalf("leer outbox: %v", err)
		}
		if eventos != 1 {
			t.Errorf("sesión %s: %d eventos sesion.revocada, se esperaba 1", id, eventos)
		}
	}
	if got, want := fmt.Sprint(prov.revocadas), fmt.Sprint([]string{"idp-expirada", "idp-" + primera.SesionID}); got != want {
		t.Errorf("revocadas en el IdP = %s, se esperaba %s", got, want)
	}

	// Dos logins simultáneos: ninguno falla y queda una sola sesión viva.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.EmitirSesion(ctx, cred)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("login simultáneo %d: %v", i, err)
		}
	}
	var vivas int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM identidad.sesion WHERE sujeto_id = $1 AND revocada_en IS NULL`, sujetoID).Scan(&vivas); err != nil {
		t.Fatalf("contar sesiones vivas: %v", err)
	}
	if vivas != 1 {
		t.Errorf("sesiones vivas = %d, se esperaba 1", vivas)
	}
	var segundaRevocada bool
	if err := pool.QueryRow(ctx, `SELECT revocada_en IS NOT NULL FROM identidad.sesion WHERE sesion_id = $1`, segunda.SesionID).Scan(&segundaRevocada); err != nil {
		t.Fatalf("leer segunda sesión: %v", err)
	}
	if !segundaRevocada {
		t.Error("los logins simultáneos debieron reemplazar la segunda sesión")
	}
}
