//go:build integration
// +build integration

// integration_sesion_test.go ejercita el Servicio_Sesion de E4-T3 (emisión y
// login productivo) contra Keycloak 26.7.4 real y un Postgres efímero (R9).
//
// Vive en el paquete kc (opción 1 del diseño de E4-T3): reutiliza el arnés de
// Keycloak (arranqueKeycloak, emitirConTOTP, subPorUsuario) sin exponer nada
// fuera de internal/idp, y así el conocimiento del protocolo del proveedor se
// queda en un solo árbol (E4-T1 R2.1). No hay ciclo de importación: internal/
// sesion no importa kc; es este test el que importa sesion.
package kc

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
)

// verificadorTokenParser adapta auth.Verifier a sesion.TokenParser: lee los
// claims sub y sesion_id de un access token verificándolo contra el JWKS del
// Keycloak de prueba. Es la costura que activa el login de E4-T3 en producción.
type verificadorTokenParser struct {
	v *auth.Verifier
}

func (p verificadorTokenParser) SubDelAccessToken(accessToken string) (string, error) {
	claims, err := p.v.VerifyToken(context.Background(), accessToken)
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

func (p verificadorTokenParser) SesionIDDelAccessToken(accessToken string) (string, error) {
	claims, err := p.v.VerifyToken(context.Background(), accessToken)
	if err != nil {
		return "", err
	}
	return claims.SesionID, nil
}

// sujetoResolverPorToken implementa sesion.SujetoResolver leyendo el sub del
// access token y resolviéndolo al sujeto sembrado. En producción es el
// ResolverSujeto de E4-T6; aquí el sub == SujetoID por construcción del fixture.
type sujetoResolverPorToken struct {
	parser   verificadorTokenParser
	tenantID string
}

func (r sujetoResolverPorToken) ResolveSujeto(_ context.Context, tok idp.Token) (sesion.SujetoRef, error) {
	sub, err := r.parser.SubDelAccessToken(tok.AccessToken)
	if err != nil {
		return sesion.SujetoRef{}, err
	}
	return sesion.SujetoRef{SujetoID: sub, TenantID: r.tenantID}, nil
}

// TestEmisionCorrespondenciaSesionID (R9.1) verifica, contra Keycloak y Postgres
// reales, que el sesion_id que genera identidad es el mismo en el claim del
// token, en la respuesta de EmitirSesion y en la fila registrada; y que el
// idp_session_id de la fila es el del token (correspondencia de H-1).
func TestEmisionCorrespondenciaSesionID(t *testing.T) {
	ctx := context.Background()

	kc := arranqueKeycloak(t) // Docker no disponible → t.Skip
	pool := arranquePostgres(t)

	usuario := "factory-monitorista"
	tenantID := tenantPorUsuario[usuario]
	sub := subPorUsuario[usuario]

	// Sembrar el tenant y el sujeto para que el resolver (sub → SujetoID) y la
	// FK sujeto.tenant_id se satisfagan. El sujeto_id del fixture es el sub.
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno') ON CONFLICT DO NOTHING`, tenantID)); err != nil {
		t.Fatalf("sembrar tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','%s','%s') ON CONFLICT DO NOTHING`, sub, sub, tenantID)); err != nil {
		t.Fatalf("sembrar sujeto: %v", err)
	}

	// Verificador JWT real contra el JWKS del contenedor; de ahí sale el parser.
	verif := auth.NewVerifier(kc.jwksURL(), "ai27-internal", 10*time.Minute)
	parser := verificadorTokenParser{v: verif}

	svc := sesion.NewServicio(
		kc.adaptador(t), // adaptador real de Keycloak
		pool,
		outbox.NewPublisher(),
		nil,
		sesion.WithSujetoResolver(sujetoResolverPorToken{parser: parser, tenantID: tenantID}),
		sesion.WithExpiryPolicy(sesion.NewExpiryAtRefresh()),
		sesion.WithEmissionFlow(sesion.NewCompensatingEmissionFlow()),
	)

	em, err := svc.EmitirSesion(ctx, sesion.Credentials{
		Usuario:       usuario,
		Contrasena:    contrasenaFactory,
		SegundoFactor: codigoTOTPFresco(),
	})
	if err != nil {
		t.Fatalf("EmitirSesion contra Keycloak: %v", err)
	}
	if em.SesionID == "" {
		t.Fatal("la emisión no devolvió sesion_id")
	}

	// (a) El claim sesion_id del access token == el sesion_id de la respuesta.
	claims, err := verif.VerifyToken(ctx, em.AccessToken)
	if err != nil {
		t.Fatalf("verificar access token: %v", err)
	}
	if claims.SesionID != em.SesionID {
		t.Errorf("claim sesion_id = %q, respuesta = %q; deberían coincidir", claims.SesionID, em.SesionID)
	}

	// (b) La fila registrada tiene el mismo sesion_id y un idp_session_id no vacío.
	var idpSID, sujetoFila string
	if err := pool.QueryRow(ctx,
		`SELECT idp_session_id, sujeto_id FROM identidad.sesion WHERE sesion_id = $1`,
		em.SesionID).Scan(&idpSID, &sujetoFila); err != nil {
		t.Fatalf("leer fila de sesión: %v", err)
	}
	if idpSID == "" {
		t.Error("la fila debería registrar el idp_session_id del token")
	}
	if idpSID == em.SesionID {
		t.Error("idp_session_id no debe ser igual al sesion_id de AI27 (regresión de H-1)")
	}
	if sujetoFila != sub {
		t.Errorf("sujeto_id de la fila = %q, se esperaba %q", sujetoFila, sub)
	}
}

// arranquePostgres levanta un Postgres 16 efímero, aplica todas las migraciones
// de identidad y devuelve un pool como svc_identidad. Docker no disponible →
// t.Skip; cualquier otra falla → t.Fatalf con la etapa (R9.6/R9.7).
func arranquePostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Skipf("Docker no disponible: %v", err)
	}
	if err := provider.Health(ctx); err != nil {
		t.Skipf("Docker no disponible: %v", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "postgres",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("arranque postgres: %v", err)
	}
	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatalf("arranque postgres: host: %v", err)
	}
	puerto, err := ctr.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("arranque postgres: puerto: %v", err)
	}
	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/postgres", host, puerto.Port())

	// Aplicar migraciones como administrador (crea esquema y rol).
	admin, closeAdmin := dbtest.AdminConnDSN(t, ctx, dsn)
	defer closeAdmin()
	files, _ := fs.Glob(migrations.FS, "identidad/*.sql")
	sort.Strings(files)
	for _, f := range files {
		sqlBytes, _ := migrations.FS.ReadFile(f)
		if _, err := admin.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("aplicar migración %s: %v", f, err)
		}
	}

	pool, closePool := dbtest.PoolAsDSN(t, ctx, dsn, "svc_identidad", "identidad")
	t.Cleanup(closePool)
	return pool
}
