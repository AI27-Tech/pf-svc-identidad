package sesion_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion/sesiontest"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
)

const schema = "identidad"
const role = "svc_identidad"

// TestRevocarYLeer ejerce el PASO 1 de la revocación (ADR 26) contra Postgres
// real: marca la fila de sesión, escribe el evento en outbox dentro de la MISMA
// tx con el expira_en de la fila, es idempotente, y las lecturas (ObtenerSesion,
// ListarRevocadas) observan el tombstone. Se salta sin IDENTIDAD_TEST_DB.
//
// EmitirSesion sigue siendo scaffold, así que la sesión de prueba se inserta
// directo: este test ejercita la revocación y sus lecturas, no la emisión.
func TestRevocarYLeer(t *testing.T) {
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

	// Sujeto y sesión viva, insertados directo (EmitirSesion es scaffold).
	// La fila se inserta CON idp_session_id, distinto del sesion_id (H-1): el
	// Paso 2 de la revocación debe pasar al IdP el idp_session_id de la fila,
	// nunca el sesion_id. Sin este valor en la fila, la corrección de H-1 no se
	// podría verificar.
	sujetoID := "11111111-1111-1111-1111-111111111111"
	sesionID := "33333333-3333-3333-3333-333333333333"
	idpSessionID := "idp-session-abc-999" // deliberadamente != sesionID
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s','idp-1',gen_random_uuid())`, sujetoID))
	mustExec(t, ctx, pool, fmt.Sprintf(
		`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ('%s','%s', now() + interval '2 hours','%s')`, sesionID, sujetoID, idpSessionID))

	// RecordingIdP registra cada llamada a Revoke para verificar que el Paso 2
	// recibe el idp_session_id de la fila (H-1), no el sesion_id.
	idpDoble := &sesiontest.RecordingIdP{}
	svc := sesion.NewServicio(idpDoble, pool, outbox.NewPublisher(), nil)

	// --- ObtenerSesion antes de revocar: viva. ---
	est, err := svc.ObtenerSesion(ctx, sesionID)
	if err != nil {
		t.Fatalf("ObtenerSesion (viva): %v", err)
	}
	if est.Revocada {
		t.Error("la sesión no debería estar revocada todavía")
	}
	if est.ExpiraEn.IsZero() {
		t.Error("ExpiraEn debería venir de la fila")
	}

	// --- Revocar: marca la fila + evento en outbox en la misma tx (paso 1), y
	//     revoca en el IdP doble (paso 2, no-op). ---
	if err := svc.RevocarSesion(ctx, sesionID); err != nil {
		t.Fatalf("RevocarSesion: %v", err)
	}

	// La fila quedó marcada.
	est, err = svc.ObtenerSesion(ctx, sesionID)
	if err != nil {
		t.Fatalf("ObtenerSesion (revocada): %v", err)
	}
	if !est.Revocada {
		t.Error("la sesión debería constar revocada")
	}
	if est.RevocadaEn == nil {
		t.Error("RevocadaEn debería estar fijado")
	}

	// El evento sesion.revocada está en outbox con el expira_en REAL de la fila.
	verificarEventoOutbox(t, ctx, pool, sesionID, est.ExpiraEn)

	// H-1: el Paso 2 debe haber pasado al IdP el idp_session_id de la fila, no
	// el sesion_id. Esta es la verificación central de la corrección de H-1.
	if len(idpDoble.RevokeCalls) == 0 {
		t.Fatal("el Paso 2 debería haber invocado Revoke en el IdP")
	}
	for i, call := range idpDoble.RevokeCalls {
		if call.IdpSessionID != idpSessionID {
			t.Errorf("Revoke #%d recibió %q; se esperaba el idp_session_id de la fila %q", i, call.IdpSessionID, idpSessionID)
		}
		if call.IdpSessionID == sesionID {
			t.Errorf("Revoke #%d recibió el sesion_id %q en lugar del idp_session_id (regresión de H-1)", i, sesionID)
		}
	}

	// Idempotente: revocar de nuevo no falla ni cambia revocada_en.
	primeraRevocada := *est.RevocadaEn
	if err := svc.RevocarSesion(ctx, sesionID); err != nil {
		t.Fatalf("revocar idempotente: %v", err)
	}
	est2, _ := svc.ObtenerSesion(ctx, sesionID)
	if est2.RevocadaEn == nil || !est2.RevocadaEn.Equal(primeraRevocada) {
		t.Errorf("revocar dos veces no debe cambiar revocada_en: %v vs %v", primeraRevocada, est2.RevocadaEn)
	}

	// Idempotencia también en el outbox: dos revocaciones producen UN evento, no
	// dos. Re-emitir sería un duplicado espurio (la segunda llamada no marcó fila).
	var eventos int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM identidad.outbox WHERE tipo='sesion.revocada' AND agregado_id=$1`,
		sesionID).Scan(&eventos); err != nil {
		t.Fatalf("contar eventos de outbox: %v", err)
	}
	if eventos != 1 {
		t.Errorf("se esperaba 1 evento sesion.revocada tras dos revocaciones, hubo %d", eventos)
	}

	// --- ListarRevocadas: el snapshot incluye el tombstone. ---
	pag, err := svc.ListarRevocadas(ctx, "")
	if err != nil {
		t.Fatalf("ListarRevocadas: %v", err)
	}
	encontrada := false
	for _, tb := range pag.Items {
		if tb.SesionID == sesionID {
			encontrada = true
			if tb.ExpiraEn.IsZero() || tb.RevocadaEn.IsZero() {
				t.Error("el tombstone debe traer revocada_en y expira_en")
			}
		}
	}
	if !encontrada {
		t.Errorf("ListarRevocadas debería incluir la sesión revocada; trajo %d items", len(pag.Items))
	}
}

// verificarEventoOutbox confirma que hay una fila sesion.revocada en outbox cuyo
// payload lleva el sesion_id y el expira_en REAL de la fila (no un placeholder).
func verificarEventoOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sesionID string, expiraEn time.Time) {
	t.Helper()
	var payload []byte
	err := pool.QueryRow(ctx,
		`SELECT payload FROM identidad.outbox WHERE tipo = 'sesion.revocada' AND agregado_id = $1 ORDER BY creado_en DESC LIMIT 1`,
		sesionID).Scan(&payload)
	if err != nil {
		t.Fatalf("leer evento de outbox: %v", err)
	}
	var p struct {
		SesionID string    `json:"sesion_id"`
		ExpiraEn time.Time `json:"expira_en"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("payload del evento no es JSON esperado: %v (%s)", err, payload)
	}
	if p.SesionID != sesionID {
		t.Errorf("payload sesion_id = %q, se esperaba %q", p.SesionID, sesionID)
	}
	if p.ExpiraEn.IsZero() {
		t.Error("payload expira_en vacío: debe venir de la fila, no un placeholder")
	}
	if !p.ExpiraEn.Equal(expiraEn.UTC()) {
		t.Errorf("payload expira_en = %v, se esperaba el de la fila %v", p.ExpiraEn, expiraEn.UTC())
	}
}

func aplicarMigraciones(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	files, err := fs.Glob(migrations.FS, schema+"/*.sql")
	if err != nil {
		t.Fatalf("listar migraciones: %v", err)
	}
	sort.Strings(files)
	for _, f := range files {
		sqlBytes, err := migrations.FS.ReadFile(f)
		if err != nil {
			t.Fatalf("leer %s: %v", f, err)
		}
		if _, err := admin.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("aplicar %s: %v", f, err)
		}
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
