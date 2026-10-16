package sesion_test

import (
	"context"
	"encoding/json"
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

// Este archivo cubre las pruebas de la Definición de Terminado (DoD) de E4-T3
// Parte A que no estaban escritas: el contract test del payload mínimo (7.6),
// la atomicidad del Paso 1 ante fallo de la transacción (8.2) y que el Paso 2
// recibe el idp_session_id de la fila (8.3, el corazón del fix H-1). Todas
// requieren Postgres y se saltan sin IDENTIDAD_TEST_DB.

const (
	dodSchema = "identidad"
	dodRole   = "svc_identidad"
)

// --- 7.6: contract test del payload mínimo ----------------------------------

// TestPayloadMinimoEnOutbox revoca una fila y comprueba que el payload escrito
// en identidad.outbox tiene EXACTAMENTE las claves {sesion_id, revocada_en,
// expira_en} con los valores de la fila, y que tipo/version son los del evento
// sesion.revocada (Tarea 7.6, Requisitos 5.1, 5.2, 10.6).
func TestPayloadMinimoEnOutbox(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := dodPrepararPool(t, ctx)
	tenantID := dodSembrarTenant(t, ctx, pool)

	sujetoID := "11111111-1111-1111-1111-111111111111"
	sesionID := "33333333-3333-3333-3333-333333333333"
	idpSessionID := "idp-session-xyz"
	dodExec(t, ctx, pool, `INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ($1,'idp-1',$2)`, sujetoID, tenantID)
	dodExec(t, ctx, pool, `INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ($1,$2, now() + interval '2 hours', $3)`, sesionID, sujetoID, idpSessionID)

	svc := sesion.NewServicio(&sesiontest.RecordingIdP{}, pool, outbox.NewPublisher(), nil)
	if err := svc.RevocarSesion(ctx, sesionID); err != nil {
		t.Fatalf("RevocarSesion: %v", err)
	}

	// Leer el payload y la fila para comparar.
	var payloadRaw []byte
	var tipo string
	var version int
	if err := pool.QueryRow(ctx,
		`SELECT payload, tipo, version FROM identidad.outbox WHERE tipo='sesion.revocada' AND agregado_id=$1`,
		sesionID).Scan(&payloadRaw, &tipo, &version); err != nil {
		t.Fatalf("leer payload de outbox: %v", err)
	}

	if tipo != "sesion.revocada" {
		t.Errorf("tipo = %q; se esperaba sesion.revocada", tipo)
	}
	if version != 1 {
		t.Errorf("version = %d; se esperaba 1", version)
	}

	// El payload debe tener EXACTAMENTE tres claves.
	var m map[string]any
	if err := json.Unmarshal(payloadRaw, &m); err != nil {
		t.Fatalf("payload no es JSON válido: %v", err)
	}
	claves := make([]string, 0, len(m))
	for k := range m {
		claves = append(claves, k)
	}
	sort.Strings(claves)
	esperadas := []string{"expira_en", "revocada_en", "sesion_id"}
	if len(claves) != len(esperadas) {
		t.Fatalf("claves del payload = %v; se esperaban exactamente %v", claves, esperadas)
	}
	for i := range esperadas {
		if claves[i] != esperadas[i] {
			t.Errorf("claves del payload = %v; se esperaban %v", claves, esperadas)
			break
		}
	}
	// El sesion_id del payload coincide con la fila.
	if m["sesion_id"] != sesionID {
		t.Errorf("payload sesion_id = %v; se esperaba %q", m["sesion_id"], sesionID)
	}
}

// --- 8.2: atomicidad del Paso 1 ---------------------------------------------

// TestPropertyAtomicidadPaso1 verifica que, ante un fallo en cualquier punto de
// la transacción del Paso 1 (Begin, UPDATE, outbox, Commit), NO queda efecto
// parcial: revocada_en sigue nulo, no hay fila sesion.revocada y no se invoca
// Revoke. Sin falla, la revocación marca la fila y escribe exactamente un
// evento (Tarea 8.2, Property 4, Requisitos 4.1, 4.4, 10.2).
func TestPropertyAtomicidadPaso1(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool := dodPrepararPool(t, ctx)
	tenantID := dodSembrarTenant(t, ctx, pool)
	t.Log("Feature: e4-t3-registro-sesion-revocacion, Property 4: Atomicidad del Paso 1")

	puntos := []struct {
		nombre     string
		punto      sesiontest.PuntoDeFalla
		debeFallar bool
	}{
		{"sin falla", sesiontest.FallaNinguno, false},
		{"falla en Begin", sesiontest.FallaBegin, true},
		{"falla en UPDATE", sesiontest.FallaUpdate, true},
		{"falla en outbox", sesiontest.FallaOutbox, true},
		{"falla en Commit", sesiontest.FallaCommit, true},
	}

	for i, caso := range puntos {
		sujetoID := uuidFijo(i, 'a')
		sesionID := uuidFijo(i, 'c')
		dodExec(t, ctx, pool, `INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ($1,$2,$3)`, sujetoID, "idp-"+sesionID, tenantID)
		dodExec(t, ctx, pool, `INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ($1,$2, now() + interval '2 hours', $3)`, sesionID, sujetoID, "idp-"+sesionID)

		idpDoble := &sesiontest.RecordingIdP{}
		faulty := &sesiontest.FaultyBeginTx{Real: pool, Punto: caso.punto}
		svc := sesion.NewServicio(idpDoble, faulty, outbox.NewPublisher(), nil)

		err := svc.RevocarSesion(ctx, sesionID)

		// Estado observado en Postgres tras la operación.
		var revocadaEn *time.Time
		if e := pool.QueryRow(ctx, `SELECT revocada_en FROM identidad.sesion WHERE sesion_id=$1`, sesionID).Scan(&revocadaEn); e != nil {
			t.Fatalf("%s: leer revocada_en: %v", caso.nombre, e)
		}
		var eventos int
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM identidad.outbox WHERE agregado_id=$1`, sesionID).Scan(&eventos); e != nil {
			t.Fatalf("%s: contar eventos: %v", caso.nombre, e)
		}

		if caso.debeFallar {
			if err == nil {
				t.Errorf("%s: se esperaba error, no hubo", caso.nombre)
			}
			if revocadaEn != nil {
				t.Errorf("%s: revocada_en no debería estar fijado tras un fallo", caso.nombre)
			}
			if eventos != 0 {
				t.Errorf("%s: no debería haber eventos tras un fallo; hubo %d", caso.nombre, eventos)
			}
			if len(idpDoble.RevokeCalls) != 0 {
				t.Errorf("%s: no debería invocarse Revoke tras un fallo del Paso 1; hubo %d", caso.nombre, len(idpDoble.RevokeCalls))
			}
		} else {
			if err != nil {
				t.Errorf("%s: no se esperaba error: %v", caso.nombre, err)
			}
			if revocadaEn == nil {
				t.Errorf("%s: revocada_en debería estar fijado", caso.nombre)
			}
			if eventos != 1 {
				t.Errorf("%s: debería haber exactamente 1 evento; hubo %d", caso.nombre, eventos)
			}
		}
	}
}

// --- 8.3: el Paso 2 recibe el idp_session_id de la fila ---------------------

// TestPropertyPaso2RecibeIdpSessionID verifica que el argumento de Revoke es
// SIEMPRE el idp_session_id de la fila y NUNCA el sesion_id (Tarea 8.3,
// Property 5, Requisitos 4.2, 4.3, 4.5, 10.1 — el corazón del fix H-1). Con
// idp_session_id vacío, Revoke no se invoca.
func TestPropertyPaso2RecibeIdpSessionID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool := dodPrepararPool(t, ctx)
	tenantID := dodSembrarTenant(t, ctx, pool)
	t.Log("Feature: e4-t3-registro-sesion-revocacion, Property 5: El Paso 2 recibe el idp_session_id de la fila")

	casos := []struct {
		nombre        string
		idpSessionID  string
		esperaInvocar bool
	}{
		{"idp_session_id normal", "idp-session-abc", true},
		{"idp_session_id distinto del sesion_id", "99999999-9999-9999-9999-999999999999", true},
		{"idp_session_id vacío (no invoca)", "", false},
	}

	for i, caso := range casos {
		sujetoID := uuidFijo(i, 'd')
		sesionID := uuidFijo(i, 'e')
		dodExec(t, ctx, pool, `INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ($1,$2,$3)`, sujetoID, "idp-"+sesionID, tenantID)
		if caso.idpSessionID == "" {
			dodExec(t, ctx, pool, `INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en) VALUES ($1,$2, now() + interval '2 hours')`, sesionID, sujetoID)
		} else {
			dodExec(t, ctx, pool, `INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en, idp_session_id) VALUES ($1,$2, now() + interval '2 hours', $3)`, sesionID, sujetoID, caso.idpSessionID)
		}

		idpDoble := &sesiontest.RecordingIdP{}
		svc := sesion.NewServicio(idpDoble, pool, outbox.NewPublisher(), nil)

		// Con idp_session_id vacío, RevocarSesion devuelve ErrIdpRevokePending
		// (sin handler A-7); eso no es un fallo del test, es lo esperado.
		_ = svc.RevocarSesion(ctx, sesionID)

		if caso.esperaInvocar {
			if len(idpDoble.RevokeCalls) != 1 {
				t.Errorf("%s: se esperaba 1 invocación de Revoke; hubo %d", caso.nombre, len(idpDoble.RevokeCalls))
				continue
			}
			arg := idpDoble.RevokeCalls[0].IdpSessionID
			if arg != caso.idpSessionID {
				t.Errorf("%s: Revoke recibió %q; se esperaba el idp_session_id de la fila %q", caso.nombre, arg, caso.idpSessionID)
			}
			if arg == sesionID {
				t.Errorf("%s: Revoke recibió el sesion_id (regresión de H-1)", caso.nombre)
			}
		} else {
			if len(idpDoble.RevokeCalls) != 0 {
				t.Errorf("%s: con idp_session_id vacío, Revoke NO debe invocarse; hubo %d", caso.nombre, len(idpDoble.RevokeCalls))
			}
		}
	}
}

// --- utilidades de este archivo ---------------------------------------------

func dodPrepararPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	t.Cleanup(closeAdmin)

	limpiar := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		dbtest.Exec(t, c, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{dodSchema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, c, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{dodRole}.Sanitize())
	}
	limpiar()
	t.Cleanup(limpiar)

	files, err := migrations.FS.ReadDir("identidad")
	if err != nil {
		t.Fatalf("listar migraciones: %v", err)
	}
	nombres := make([]string, 0, len(files))
	for _, f := range files {
		nombres = append(nombres, f.Name())
	}
	sort.Strings(nombres)
	for _, n := range nombres {
		sql, err := migrations.FS.ReadFile("identidad/" + n)
		if err != nil {
			t.Fatalf("leer %s: %v", n, err)
		}
		if _, err := admin.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("aplicar %s: %v", n, err)
		}
	}

	pool, closePool := dbtest.PoolAs(t, ctx, dodRole, dodSchema)
	t.Cleanup(closePool)
	return pool
}

func dodExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// dodSembrarTenant inserta un tenant interno y devuelve su id. 0006 agregó la
// FK sujeto.tenant_id → tenant(id), así que todo sujeto de prueba necesita un
// tenant real antes de insertarse. svc_identidad tiene INSERT sobre tenant.
func dodSembrarTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	const tenantID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	dodExec(t, ctx, pool, `INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1,'AI27','interno')`, tenantID)
	return tenantID
}

// uuidFijo genera un UUID v4 determinista por índice y letra, para datos de
// prueba distintos en cada iteración sin colisiones. Usa dígitos hex válidos.
func uuidFijo(i int, letra byte) string {
	// letra debe ser hex (a-f); i se mapea a un dígito 0-9.
	l := string([]byte{letra})
	n := string([]byte{byte('0') + byte(i%10)})
	rep := func(s string, k int) string {
		out := ""
		for j := 0; j < k; j++ {
			out += s
		}
		return out
	}
	// Formato 8-4-4-4-12 con versión 4 y variante 8.
	return rep(l, 8) + "-" + rep(n, 4) + "-4" + rep(n, 3) + "-8" + rep(n, 3) + "-" + rep(l, 8) + rep(n, 4)
}
