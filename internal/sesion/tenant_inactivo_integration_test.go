package sesion_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"testing"
	"time"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// TestRefrescoRechazaTenantInactivo verifica DEC-139 #4: el sujeto de una
// organización desactivada pierde el acceso en su siguiente refresco, con el
// mismo 401 que un refresh inválido. La sesión de un tenant activo refresca.
func TestRefrescoRechazaTenantInactivo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, cerrar, ejecutar := prepararEsquema(t, ctx)
	defer cerrar()

	const (
		tenantActivo   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		tenantInactivo = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		sujetoActivo   = "11111111-1111-4111-8111-111111111111"
		sujetoInactivo = "22222222-2222-4222-8222-222222222222"
		sesionActiva   = "33333333-3333-4333-8333-333333333333"
		sesionInactiva = "44444444-4444-4444-8444-444444444444"
	)
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','Activa SA','cliente'), ('%s','Baja SA','cliente')`, tenantActivo, tenantInactivo))
	ejecutar(fmt.Sprintf(`UPDATE identidad.tenant SET activa = false WHERE id = '%s'`, tenantInactivo))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','sub-a','%s'), ('%s','sub-b','%s')`, sujetoActivo, tenantActivo, sujetoInactivo, tenantInactivo))
	ejecutar(fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ('%s','%s', now() + interval '1 hour','idp-a'), ('%s','%s', now() + interval '1 hour','idp-b')`, sesionActiva, sujetoActivo, sesionInactiva, sujetoInactivo))

	prov := &idpRefrescador{expira: time.Now().Add(time.Hour).UTC()}
	svc := sesion.NewServicio(prov, pool, outbox.NewPublisher(), nil,
		sesion.WithExpiryPolicy(sesion.NewExpiryAtRefresh()),
		sesion.WithRefresh(sesion.NewRefreshResolver(func(tok idp.Token) (string, error) { return tok.AccessToken, nil }), sesion.RevokedRefreshReject),
	)

	prov.sesionIDObjetivo = sesionInactiva
	_, err := svc.RefrescarSesion(ctx, "refresh-b")
	if platformerrors.AsCategory(err) != platformerrors.CategoryNoAutorizado {
		t.Fatalf("tenant inactivo: err %v; quiere 401 no_autorizado", err)
	}
	var pe *platformerrors.PlatformError
	if !stderrors.As(err, &pe) || pe.Message != "refresh token inválido" {
		t.Errorf("el mensaje no debe revelar la causa: %v", err)
	}

	prov.sesionIDObjetivo = sesionActiva
	if _, err := svc.RefrescarSesion(ctx, "refresh-a"); err != nil {
		t.Errorf("tenant activo: %v", err)
	}
}
