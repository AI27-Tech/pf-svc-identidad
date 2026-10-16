package sesion_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion/sesiontest"
)

// TestAuditoriaRevocacion verifica, contra Postgres real, que al revocar una
// sesión con un AuditRecorder configurado se escribe en `cambio_identidad`
// (Requisito 5.3; A-5, A-12, A-16, DEC-088):
//   - tipo = 'sesion.revocada'
//   - sesion_id = el revocado
//   - sujeto_id = el de la fila de sesión (A-5)
//   - tenant_id = el del sujeto por join (A-16, opción a)
//   - actor_id = el sujeto del actor resuelto desde el contexto (A-12)
//   - motivo = NULL (A-12: motivo diferido a V2)
//
// Y que la auditoría es idempotente con la revocación: revocar dos veces deja
// una sola fila de auditoría, porque la segunda llamada no marca fila viva.
//
// Se salta sin IDENTIDAD_TEST_DB.
func TestAuditoriaRevocacion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()

	cleanup := func() {
		dbtest.Exec(t, ctx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, ctx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	}
	cleanup()
	defer cleanup()
	aplicarMigraciones(t, ctx, admin)

	pool, closePool := dbtest.PoolAs(t, ctx, role, schema)
	defer closePool()

	// Tenant real (FK de 0006), sujeto dueño de la sesión, y el sujeto que actúa
	// como revocador (actor). En un logout propio el actor y el dueño coinciden;
	// aquí los separamos para verificar que actor_id sale del contexto, no de la
	// fila.
	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sujetoID := "11111111-1111-1111-1111-111111111111"
	actorSujetoID := "22222222-2222-4222-8222-222222222222"
	sesionID := "33333333-3333-3333-3333-333333333333"
	idpSessionID := "idp-session-abc-999"

	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno')`, tenantID))
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-dueno','%s')`, sujetoID, tenantID))
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-actor','%s')`, actorSujetoID, tenantID))
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ('%s','%s', now() + interval '2 hours','%s')`, sesionID, sujetoID, idpSessionID))

	// El AuditRecorder resuelve el actor desde el contexto. Aquí la costura
	// devuelve el sujeto del actor de forma determinista (en producción vendría
	// de auth.ClaimsFromContext → resolución sub→sujeto.id).
	actorResolver := func(context.Context) (string, bool, error) {
		return actorSujetoID, true, nil
	}
	recorder := sesion.NewAuditRecorder(actorResolver)

	idpDoble := &sesiontest.RecordingIdP{}
	svc := sesion.NewServicio(idpDoble, pool, outbox.NewPublisher(), nil,
		sesion.WithAuditRecorder(recorder))

	if err := svc.RevocarSesion(ctx, sesionID); err != nil {
		t.Fatalf("RevocarSesion: %v", err)
	}

	// Se escribió exactamente una fila de auditoría con los valores esperados.
	var (
		tipo        string
		gotSesionID string
		gotSujetoID *string
		gotTenantID *string
		gotActorID  *string
		gotMotivo   *string
		filas       int
	)
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM identidad.cambio_identidad WHERE sesion_id = $1`,
		sesionID).Scan(&filas); err != nil {
		t.Fatalf("contar filas de auditoría: %v", err)
	}
	if filas != 1 {
		t.Fatalf("se esperaba 1 fila de auditoría, hubo %d", filas)
	}

	if err := pool.QueryRow(ctx,
		`SELECT tipo, sesion_id, sujeto_id, tenant_id, actor_id, motivo
		   FROM identidad.cambio_identidad WHERE sesion_id = $1`,
		sesionID,
	).Scan(&tipo, &gotSesionID, &gotSujetoID, &gotTenantID, &gotActorID, &gotMotivo); err != nil {
		t.Fatalf("leer fila de auditoría: %v", err)
	}

	if tipo != "sesion.revocada" {
		t.Errorf("tipo = %q, se esperaba sesion.revocada", tipo)
	}
	if gotSesionID != sesionID {
		t.Errorf("sesion_id = %q, se esperaba %q", gotSesionID, sesionID)
	}
	if gotSujetoID == nil || *gotSujetoID != sujetoID {
		t.Errorf("sujeto_id = %v, se esperaba %q (de la fila, A-5)", gotSujetoID, sujetoID)
	}
	if gotTenantID == nil || *gotTenantID != tenantID {
		t.Errorf("tenant_id = %v, se esperaba %q (del sujeto, A-16)", gotTenantID, tenantID)
	}
	if gotActorID == nil || *gotActorID != actorSujetoID {
		t.Errorf("actor_id = %v, se esperaba %q (del contexto, A-12)", gotActorID, actorSujetoID)
	}
	if gotMotivo != nil {
		t.Errorf("motivo = %v, se esperaba NULL (A-12: diferido a V2)", *gotMotivo)
	}

	// Idempotencia: revocar de nuevo NO escribe otra fila de auditoría (la
	// segunda llamada no marca fila viva, así que no entra en la rama que audita).
	if err := svc.RevocarSesion(ctx, sesionID); err != nil {
		t.Fatalf("segunda revocación: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM identidad.cambio_identidad WHERE sesion_id = $1`,
		sesionID).Scan(&filas); err != nil {
		t.Fatalf("recontar filas de auditoría: %v", err)
	}
	if filas != 1 {
		t.Errorf("tras dos revocaciones se esperaba 1 fila de auditoría, hubo %d", filas)
	}
}

// TestAuditoriaActorDesconocido verifica que, sin actor resoluble en el
// contexto, la revocación igual registra la auditoría con actor_id NULL: la
// auditoría no bloquea la revocación (A-12).
func TestAuditoriaActorDesconocido(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()

	cleanup := func() {
		dbtest.Exec(t, ctx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, ctx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	}
	cleanup()
	defer cleanup()
	aplicarMigraciones(t, ctx, admin)

	pool, closePool := dbtest.PoolAs(t, ctx, role, schema)
	defer closePool()

	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sujetoID := "11111111-1111-1111-1111-111111111111"
	sesionID := "33333333-3333-3333-3333-333333333333"

	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('%s','AI27','interno')`, tenantID))
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-dueno','%s')`, sujetoID, tenantID))
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ('%s','%s', now() + interval '2 hours','idp-x')`, sesionID, sujetoID))

	// actorResolver devuelve "sin actor": el contexto no identifica a nadie.
	actorResolver := func(context.Context) (string, bool, error) {
		return "", false, nil
	}
	recorder := sesion.NewAuditRecorder(actorResolver)
	svc := sesion.NewServicio(&sesiontest.RecordingIdP{}, pool, outbox.NewPublisher(), nil,
		sesion.WithAuditRecorder(recorder))

	if err := svc.RevocarSesion(ctx, sesionID); err != nil {
		t.Fatalf("RevocarSesion: %v", err)
	}

	var gotActorID *string
	var gotSujetoID *string
	if err := pool.QueryRow(ctx,
		`SELECT sujeto_id, actor_id FROM identidad.cambio_identidad WHERE sesion_id = $1`,
		sesionID).Scan(&gotSujetoID, &gotActorID); err != nil {
		t.Fatalf("leer fila de auditoría: %v", err)
	}
	// La auditoría se escribió (sujeto conocido) aunque el actor sea desconocido.
	if gotSujetoID == nil || *gotSujetoID != sujetoID {
		t.Errorf("sujeto_id = %v, se esperaba %q", gotSujetoID, sujetoID)
	}
	if gotActorID != nil {
		t.Errorf("actor_id = %v, se esperaba NULL (actor desconocido)", *gotActorID)
	}
}
