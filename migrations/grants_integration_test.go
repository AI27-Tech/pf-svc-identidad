package migrations_test

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	schema = "identidad"
	role   = "svc_identidad"
)

// TestGrants verifica el invariante I1 (ADR 17) para identidad al nivel que
// cierra E0-T5: aislamiento por esquema y privilegio mínimo por tabla. Aplica
// las migraciones REALES (0001 estructura + 0002 grants, en orden de nombre)
// sobre una base efímera y comprueba, con escrituras reales ejecutadas como
// svc_identidad, qué permite y qué rechaza Postgres.
//
// Criterio del Lead (E0-T5): el rol lee lo suyo, escribe lo que su flujo
// documentado necesita y NUNCA borra. 0002 además pone svc_identidad en LOGIN y
// siembra los ocho roles de DEC-053; ambas cosas se verifican aquí.
//
// Prueba comportamiento observable, con el error literal de Postgres en lo
// denegado. Se salta si IDENTIDAD_TEST_DB no está definido (CI sin Postgres).
func TestGrants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()

	suffix := fmt.Sprintf("_e0t5_%d", time.Now().UnixNano())
	foreignSchema := "ajeno" + suffix
	foreignRole := "rol_ajeno" + suffix

	cleanup := func() {
		for _, s := range []string{schema, foreignSchema} {
			dbtest.Exec(t, ctx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{s}.Sanitize()+" CASCADE")
		}
		for _, r := range []string{role, foreignRole} {
			dbtest.Exec(t, ctx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{r}.Sanitize())
		}
	}
	cleanup()
	defer cleanup()

	applyMigrations(t, ctx, admin)

	// "Otro módulo": un esquema con una tabla, y un rol sin ningún GRANT.
	dbtest.Exec(t, ctx, admin, fmt.Sprintf("CREATE SCHEMA %s", foreignSchema))
	dbtest.Exec(t, ctx, admin, fmt.Sprintf("CREATE TABLE %s.dato (id int)", foreignSchema))
	dbtest.Exec(t, ctx, admin, fmt.Sprintf("CREATE ROLE %s NOLOGIN", foreignRole))
	// Una tabla creada DESPUÉS de 0002 (como haría una 0003) no debe heredar
	// privilegios por defecto.
	dbtest.Exec(t, ctx, admin, "CREATE TABLE identidad.tabla_futura (id int)")

	// --- 0002 debe haber puesto svc_identidad en LOGIN (decisión del Lead). ---
	assertCanLogin(t, ctx, admin, role)

	// --- 0002 debe haber sembrado los ocho roles de DEC-053. ---
	assertRolesSembrados(t, ctx, admin)

	pool, closePool := dbtest.PoolAs(t, ctx, role, schema)
	defer closePool()

	// --- Flujo vivo permitido: debe funcionar. ---
	sujetoID := "11111111-1111-1111-1111-111111111111"
	mustExec(t, ctx, pool,
		fmt.Sprintf(`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ('%s', 'idp-1', gen_random_uuid())`, sujetoID),
		"svc_identidad debe poder dar de alta un sujeto")
	mustExec(t, ctx, pool,
		`UPDATE identidad.sujeto SET activo = false, actualizado_en = now() WHERE id = '`+sujetoID+`'`,
		"svc_identidad debe poder actualizar un sujeto")

	// El catálogo rol es de solo lectura para el servicio.
	mustExec(t, ctx, pool,
		`SELECT 1 FROM identidad.rol WHERE codigo = 'monitorista'`,
		"svc_identidad debe poder leer el catálogo de roles")

	// Asignar y revocar un rol: INSERT y luego UPDATE de revocado_en (no DELETE).
	asignacionID := "22222222-2222-2222-2222-222222222222"
	mustExec(t, ctx, pool,
		fmt.Sprintf(`INSERT INTO identidad.asignacion_rol (id, sujeto_id, rol_codigo, asignado_por) VALUES ('%s', '%s', 'monitorista', gen_random_uuid())`, asignacionID, sujetoID),
		"svc_identidad debe poder asignar un rol")
	mustExec(t, ctx, pool,
		`UPDATE identidad.asignacion_rol SET revocado_en = now(), revocado_por = gen_random_uuid() WHERE id = '`+asignacionID+`'`,
		"svc_identidad debe poder revocar un rol marcando revocado_en")

	// Sesión: emitir (INSERT) y revocar marcando revocada_en (UPDATE), sin borrar.
	sesionID := "33333333-3333-3333-3333-333333333333"
	mustExec(t, ctx, pool,
		fmt.Sprintf(`INSERT INTO identidad.sesion (sesion_id, sujeto_id, expira_en) VALUES ('%s', '%s', now() + interval '1 hour')`, sesionID, sujetoID),
		"svc_identidad debe poder emitir una sesión")
	mustExec(t, ctx, pool,
		`UPDATE identidad.sesion SET revocada_en = now() WHERE sesion_id = '`+sesionID+`'`,
		"svc_identidad debe poder revocar una sesión marcando revocada_en")

	// cambio_identidad: append-only.
	mustExec(t, ctx, pool,
		`INSERT INTO identidad.cambio_identidad (tipo, sujeto_id) VALUES ('sesion.revocada', '`+sujetoID+`')`,
		"svc_identidad debe poder registrar un cambio de identidad")

	// alcance_tenant: alta y lectura, sin versionado todavía (DEC-022 #2).
	mustExec(t, ctx, pool,
		fmt.Sprintf(`INSERT INTO identidad.alcance_tenant (sujeto_id, tenant_id) VALUES ('%s', gen_random_uuid())`, sujetoID),
		"svc_identidad debe poder registrar el alcance de tenant")

	// outbox: publicar y marcar publicado, sin borrar.
	mustExec(t, ctx, pool,
		`INSERT INTO identidad.outbox (evento_id, tipo, agregado_id, ocurrido_en, payload)
		 VALUES (gen_random_uuid(), 'sesion.revocada', '`+sujetoID+`', now(), '{}'::jsonb)`,
		"svc_identidad debe poder publicar en su outbox")
	mustExec(t, ctx, pool,
		`UPDATE identidad.outbox SET publicado = true, publicado_en = now() WHERE evento_id IS NOT NULL`,
		"svc_identidad debe poder marcar el outbox como publicado")

	// --- Lo que su flujo NO necesita: la base lo rechaza. ---
	denied := []struct{ sql, wantErr, msg string }{
		// rol: solo lectura para el servicio.
		{`INSERT INTO identidad.rol (codigo) VALUES ('cliente')`, "permission denied for table rol",
			"el servicio no siembra roles: los siembra la migración"},
		{`DELETE FROM identidad.rol`, "permission denied for table rol", "el catálogo de roles no se borra"},
		{`UPDATE identidad.rol SET codigo = 'x'`, "permission denied for table rol", "el catálogo de roles no se edita"},
		// sesion: tombstone de retención larga, sin purga.
		{`DELETE FROM identidad.sesion`, "permission denied for table sesion",
			"la sesión no se borra: es tombstone de retención larga (ADR 26)"},
		// cambio_identidad: append-only.
		{`UPDATE identidad.cambio_identidad SET motivo = 'x'`, "permission denied for table cambio_identidad",
			"la auditoría local no se reescribe (append-only)"},
		{`DELETE FROM identidad.cambio_identidad`, "permission denied for table cambio_identidad",
			"la auditoría local no se borra (append-only)"},
		// sujeto y asignacion_rol: sin DELETE.
		{`DELETE FROM identidad.sujeto`, "permission denied for table sujeto", "el sujeto no se borra"},
		{`DELETE FROM identidad.asignacion_rol`, "permission denied for table asignacion_rol",
			"la asignación de rol no se borra: se revoca por marca"},
		// alcance_tenant: sin UPDATE ni DELETE (versionado pendiente, DEC-022 #2).
		{`UPDATE identidad.alcance_tenant SET tenant_id = gen_random_uuid()`, "permission denied for table alcance_tenant",
			"el alcance de tenant no se versiona todavía (DEC-022 #2)"},
		{`DELETE FROM identidad.alcance_tenant`, "permission denied for table alcance_tenant",
			"el alcance de tenant no se borra"},
		// outbox: sin DELETE.
		{`DELETE FROM identidad.outbox`, "permission denied for table outbox", "el outbox no se borra"},
		// tabla futura y esquema ajeno.
		{`SELECT 1 FROM identidad.tabla_futura`, "permission denied for table tabla_futura",
			"una tabla futura no hereda privilegios por defecto"},
		{fmt.Sprintf(`SELECT 1 FROM %s.dato`, foreignSchema), "permission denied for schema " + foreignSchema,
			"svc_identidad no alcanza el esquema de otro módulo (I1)"},
	}
	for _, d := range denied {
		mustDeny(t, ctx, pool, d.sql, d.wantErr, d.msg)
	}

	// --- Ningún otro rol alcanza identidad: no hay GRANT a PUBLIC ni a terceros. ---
	foreignPool, closeForeign := dbtest.PoolAs(t, ctx, foreignRole, "")
	defer closeForeign()
	mustDeny(t, ctx, foreignPool, `SELECT 1 FROM identidad.sujeto`, "permission denied for schema identidad",
		"un rol ajeno no alcanza el esquema identidad (I1)")

	// --- svc_identidad tiene USAGE sobre un solo esquema de módulo: el suyo. ---
	assertUsageOnlyOn(t, ctx, admin, role, schema)
}

// applyMigrations aplica, como admin, todos los .sql del esquema en orden de
// nombre: 0001 (estructura) y luego 0002 (grants).
func applyMigrations(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	files, err := fs.Glob(migrations.FS, schema+"/*.sql")
	if err != nil {
		t.Fatalf("listar migraciones: %v", err)
	}
	sort.Strings(files)
	if len(files) < 2 {
		t.Fatalf("se esperaban al menos 0001 y 0002, hay: %v", files)
	}
	for _, f := range files {
		sql, err := migrations.FS.ReadFile(f)
		if err != nil {
			t.Fatalf("leer %s: %v", f, err)
		}
		if _, err := admin.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("aplicar %s: %v", f, err)
		}
	}
}

// assertCanLogin afirma que el rol quedó con LOGIN (lo pone 0002, decisión del Lead).
func assertCanLogin(t *testing.T, ctx context.Context, admin *pgx.Conn, roleName string) {
	t.Helper()
	var canLogin bool
	if err := admin.QueryRow(ctx, `SELECT rolcanlogin FROM pg_roles WHERE rolname = $1`, roleName).Scan(&canLogin); err != nil {
		t.Fatalf("consultar LOGIN de %s: %v", roleName, err)
	}
	if !canLogin {
		t.Fatalf("%s debe quedar con LOGIN tras 0002 (ALTER ROLE ... LOGIN)", roleName)
	}
}

// assertRolesSembrados afirma que 0002 sembró exactamente los ocho roles de DEC-053.
func assertRolesSembrados(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	rows, err := admin.Query(ctx, `SELECT codigo FROM identidad.rol ORDER BY codigo`)
	if err != nil {
		t.Fatalf("consultar roles sembrados: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("leer roles sembrados: %v", err)
	}
	want := []string{
		"administrador", "cliente", "coordinador", "ejecutivo",
		"jefe_operativo", "monitorista", "reaccion", "supervisor",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roles sembrados: se esperaban %v, hay %v", want, got)
	}
}

// assertUsageOnlyOn afirma que el rol tiene USAGE solo sobre wantSchema entre los
// esquemas no de sistema. Se excluye `public`: su USAGE llega por el pseudo-rol
// PUBLIC (default de Postgres); su REVOKE es tarea del bootstrap de base (R12).
func assertUsageOnlyOn(t *testing.T, ctx context.Context, admin *pgx.Conn, roleName, wantSchema string) {
	t.Helper()
	rows, err := admin.Query(ctx, `
		SELECT n.nspname FROM pg_namespace n
		 WHERE has_schema_privilege($1, n.oid, 'USAGE')
		   AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
		   AND n.nspname <> 'public'
		 ORDER BY 1`, roleName)
	if err != nil {
		t.Fatalf("consultar USAGE de %s: %v", roleName, err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("leer USAGE de %s: %v", roleName, err)
	}
	if len(got) != 1 || got[0] != wantSchema {
		t.Fatalf("%s debe tener USAGE solo sobre [%s], tiene: %v", roleName, wantSchema, got)
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql, msg string) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

func mustDeny(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql, wantErr, msg string) {
	t.Helper()
	_, err := pool.Exec(ctx, sql)
	if err == nil {
		t.Fatalf("%s: la operación tuvo éxito pero debía ser denegada", msg)
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("%s: se esperaba un error con %q, se obtuvo: %v", msg, wantErr, err)
	}
}
