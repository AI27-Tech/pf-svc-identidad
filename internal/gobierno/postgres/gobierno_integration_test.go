package postgres_test

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	autzpg "github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	gobpg "github.com/AI27-Tech/pf-svc-identidad/internal/gobierno/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
)

// Este archivo cubre las sub-tareas [Requiere Postgres] de E4-T6 que tocan los
// repos y las migraciones de gobierno: el smoke del esquema (1.3, 2.2), el
// comportamiento del repo de asignación (4.3), la inmutabilidad de la traza
// (5.3) y la paginación del historial (5.x). Las pruebas del servicio
// transaccional, el autoescalamiento y el resolver viven en el paquete
// gobierno (caja negra), en otros archivos. Todas se saltan sin
// IDENTIDAD_TEST_DB.

const (
	esquema  = "identidad"
	rol      = "svc_identidad"
	tenantAI = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

// --- 1.3 / 2.2: smoke del esquema de las migraciones 0004/0005/0006 ---------

// TestEsquemaMigracionesGobierno verifica que 0004 (índice de la traza), 0005
// (índice de un solo rol activo) y 0006 (tabla tenant + FK) quedaron aplicados
// con la forma esperada, y que sus invariantes se cumplen contra Postgres real
// (Tareas 1.3, 2.2; Requisitos 1.7, 4.5, 13.1).
func TestEsquemaMigracionesGobierno(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPool(t, ctx)

	// 0004: el índice de la traza existe sobre (sujeto_id, ocurrido_en DESC).
	assertIndiceExiste(t, ctx, pool, "cambio_identidad_sujeto_ts_idx", "sujeto_id", "ocurrido_en")
	// 0005: el índice único parcial de un solo rol activo existe.
	assertIndiceExiste(t, ctx, pool, "asignacion_rol_un_activo_idx", "sujeto_id")

	// 0005: un segundo rol activo del mismo sujeto viola el índice único (23505).
	sembrarTenant(t, ctx, pool)
	sujetoID := sembrarSujeto(t, ctx, pool, "idp-esquema")
	mustExec(t, ctx, pool,
		`INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por) VALUES ($1,'monitorista',$2)`,
		sujetoID, tenantAI)
	_, err := pool.Exec(ctx,
		`INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por) VALUES ($1,'supervisor',$2)`,
		sujetoID, tenantAI)
	assertCodigoPg(t, err, "23505", "un segundo rol activo debe violar asignacion_rol_un_activo_idx")

	// 0006: el CHECK de tipo rechaza un tipo fuera de ('interno','cliente').
	_, err = pool.Exec(ctx, `INSERT INTO identidad.tenant (id, nombre, tipo) VALUES (gen_random_uuid(),'X','otro')`)
	assertCodigoPg(t, err, "23514", "tenant.tipo debe rechazar un valor fuera del CHECK")

	// 0006: un sujeto con tenant_id inexistente viola la FK sujeto_tenant_fk.
	_, err = pool.Exec(ctx,
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES (gen_random_uuid(),'idp-huerfano',gen_random_uuid())`)
	assertCodigoPg(t, err, "23503", "un sujeto con tenant inexistente debe violar sujeto_tenant_fk")
}

// --- 4.3: comportamiento del repo de asignación (reemplazo e inicial) -------

// TestAsignacionReemplazoYActorPg verifica, contra Postgres, que Reemplazar
// deja exactamente una asignación activa y una revocada, con los actores
// correctos; que AsignarInicial rechaza un segundo rol activo; y que un segundo
// AsignarInicial emerge como ErrAsignacionActivaExiste (Tarea 4.3; Requisitos
// 3.1, 4.3, 4.5).
func TestAsignacionReemplazoYActorPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPool(t, ctx)
	sembrarTenant(t, ctx, pool)
	sujetoID := sembrarSujeto(t, ctx, pool, "idp-asignacion")

	repo := autzpg.NewAsignacionRolRepo(pool)
	actorA := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	actorB := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"

	// Asignación inicial: monitorista por el actor A.
	enTx(t, ctx, pool, func(tx pgx.Tx) error {
		return repo.AsignarInicial(ctx, tx, autorizacion.AsignacionRol{
			SujetoID: sujetoID, Rol: autorizacion.RolMonitorista, TenantID: tenantAI, AsignadoPor: actorA,
		})
	})

	// Un segundo AsignarInicial choca con el índice: ErrAsignacionActivaExiste.
	errDup := enTxErr(ctx, pool, func(tx pgx.Tx) error {
		return repo.AsignarInicial(ctx, tx, autorizacion.AsignacionRol{
			SujetoID: sujetoID, Rol: autorizacion.RolSupervisor, TenantID: tenantAI, AsignadoPor: actorA,
		})
	})
	if !errors.Is(errDup, autorizacion.ErrAsignacionActivaExiste) {
		t.Errorf("segundo AsignarInicial: esperaba ErrAsignacionActivaExiste, obtuve %v", errDup)
	}

	// Reemplazo a supervisor por el actor B.
	enTx(t, ctx, pool, func(tx pgx.Tx) error {
		return repo.Reemplazar(ctx, tx, autorizacion.Reemplazo{
			SujetoID: sujetoID, RolNuevo: autorizacion.RolSupervisor, ActorID: actorB,
		})
	})

	// Exactamente una activa, y es supervisor asignada por B.
	activas, err := repo.ActivasDe(ctx, sujetoID)
	if err != nil {
		t.Fatalf("ActivasDe: %v", err)
	}
	if len(activas) != 1 {
		t.Fatalf("esperaba 1 asignación activa tras el reemplazo, hubo %d", len(activas))
	}
	if activas[0].Rol != autorizacion.RolSupervisor {
		t.Errorf("rol activo = %s; esperaba supervisor", activas[0].Rol)
	}

	// Debe quedar exactamente una fila revocada (la monitorista) con revocado_por = B.
	var revocadas int
	var rolRevocado, revocadoPor string
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND revocado_en IS NOT NULL`,
		sujetoID).Scan(&revocadas); err != nil {
		t.Fatalf("contar revocadas: %v", err)
	}
	if revocadas != 1 {
		t.Fatalf("esperaba 1 asignación revocada, hubo %d", revocadas)
	}
	if err := pool.QueryRow(ctx,
		`SELECT rol_codigo, revocado_por::text FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND revocado_en IS NOT NULL`,
		sujetoID).Scan(&rolRevocado, &revocadoPor); err != nil {
		t.Fatalf("leer revocada: %v", err)
	}
	if rolRevocado != "monitorista" {
		t.Errorf("rol revocado = %s; esperaba monitorista", rolRevocado)
	}
	if revocadoPor != actorB {
		t.Errorf("revocado_por = %s; esperaba el actor B %s", revocadoPor, actorB)
	}

	// La nueva activa fue asignada por B.
	var asignadoPor string
	if err := pool.QueryRow(ctx,
		`SELECT asignado_por::text FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND revocado_en IS NULL`,
		sujetoID).Scan(&asignadoPor); err != nil {
		t.Fatalf("leer activa: %v", err)
	}
	if asignadoPor != actorB {
		t.Errorf("asignado_por de la nueva activa = %s; esperaba el actor B %s", asignadoPor, actorB)
	}
}

// --- 5.3: inmutabilidad de la traza (Property 4) ----------------------------

// TestTrazaInmutablePg verifica que svc_identidad puede insertar en
// cambio_identidad pero NO puede UPDATE ni DELETE: la traza es append-only por
// privilegio (Tarea 5.3; Requisito 1.6, ADR 26 §7).
func TestTrazaInmutablePg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPool(t, ctx)
	sembrarTenant(t, ctx, pool)
	sujetoID := sembrarSujeto(t, ctx, pool, "idp-traza")

	traza := gobpg.NewTrazaRepo(pool)
	actorID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	// Insertar una fila de traza dentro de una tx (como el servicio).
	enTx(t, ctx, pool, func(tx pgx.Tx) error {
		return traza.Registrar(ctx, tx, gobierno.CambioAdministrativo{
			Tipo: "sujeto.creado", SujetoID: sujetoID, TenantID: tenantAI, ActorID: actorID,
		})
	})

	// La traza exige actor no vacío (Requisito 1.9): sin actor, error de dominio.
	errSinActor := enTxErr(ctx, pool, func(tx pgx.Tx) error {
		return traza.Registrar(ctx, tx, gobierno.CambioAdministrativo{
			Tipo: "sujeto.creado", SujetoID: sujetoID, TenantID: tenantAI, ActorID: "",
		})
	})
	if errSinActor == nil {
		t.Error("Registrar con actor vacío debería fallar")
	}

	// UPDATE y DELETE sobre cambio_identidad: denegados por privilegio.
	_, err := pool.Exec(ctx, `UPDATE identidad.cambio_identidad SET motivo='x' WHERE sujeto_id=$1`, sujetoID)
	assertDenegado(t, err, "la traza no se reescribe (append-only)")
	_, err = pool.Exec(ctx, `DELETE FROM identidad.cambio_identidad WHERE sujeto_id=$1`, sujetoID)
	assertDenegado(t, err, "la traza no se borra (append-only)")
}

// --- 5.x: paginación del historial de la traza ------------------------------

// TestHistorialPaginadoPg verifica que Historial devuelve los cambios de un
// sujeto en orden descendente por tiempo y pagina por cursor, apoyado en el
// índice 0004 (Tarea 5.1; Requisito 10.2).
func TestHistorialPaginadoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPool(t, ctx)
	sembrarTenant(t, ctx, pool)
	sujetoID := sembrarSujeto(t, ctx, pool, "idp-historial")

	traza := gobpg.NewTrazaRepo(pool)
	actorID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	// 120 cambios para forzar al menos tres páginas (tamanoPagina = 50).
	const total = 120
	for i := 0; i < total; i++ {
		enTx(t, ctx, pool, func(tx pgx.Tx) error {
			return traza.Registrar(ctx, tx, gobierno.CambioAdministrativo{
				Tipo: "rol.asignado", SujetoID: sujetoID, TenantID: tenantAI, ActorID: actorID,
			})
		})
	}

	vistos := 0
	cursor := ""
	paginas := 0
	for {
		pag, err := traza.Historial(ctx, sujetoID, cursor)
		if err != nil {
			t.Fatalf("Historial (página %d): %v", paginas, err)
		}
		paginas++
		vistos += len(pag.Cambios)
		for _, c := range pag.Cambios {
			if c.ActorID != actorID {
				t.Errorf("actor del cambio = %q; esperaba %q", c.ActorID, actorID)
			}
			if c.Tipo != "rol.asignado" {
				t.Errorf("tipo del cambio = %q; esperaba rol.asignado", c.Tipo)
			}
		}
		if pag.CursorSiguiente == nil {
			break
		}
		cursor = *pag.CursorSiguiente
		if paginas > 10 {
			t.Fatal("demasiadas páginas: el cursor no termina")
		}
	}
	if vistos != total {
		t.Errorf("el historial paginado trajo %d cambios; esperaba %d", vistos, total)
	}
	if paginas < 3 {
		t.Errorf("esperaba al menos 3 páginas para %d cambios, hubo %d", total, paginas)
	}
}

// --- Utilidades compartidas de este paquete ---------------------------------

// prepararPool limpia el esquema, aplica TODAS las migraciones reales en orden
// y devuelve un pool que asume el rol svc_identidad (privilegios de producción).
func prepararPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	t.Cleanup(closeAdmin)

	limpiar := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		dbtest.Exec(t, c, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{esquema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, c, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{rol}.Sanitize())
	}
	limpiar()
	t.Cleanup(limpiar)

	aplicarMigraciones(t, ctx, admin)

	pool, closePool := dbtest.PoolAs(t, ctx, rol, esquema)
	t.Cleanup(closePool)
	return pool
}

func aplicarMigraciones(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	files, err := fs.Glob(migrations.FS, esquema+"/*.sql")
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

// sembrarTenant inserta el tenant interno de AI27 (satisface sujeto_tenant_fk).
func sembrarTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	mustExec(t, ctx, pool, `INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1,'AI27','interno')`, tenantAI)
}

// sembrarSujeto inserta un sujeto activo con el tenant interno y devuelve su id.
func sembrarSujeto(t *testing.T, ctx context.Context, pool *pgxpool.Pool, idpSubject string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO identidad.sujeto (idp_subject, tenant_id) VALUES ($1,$2) RETURNING id::text`,
		idpSubject, tenantAI).Scan(&id); err != nil {
		t.Fatalf("sembrar sujeto: %v", err)
	}
	return id
}

func enTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) {
	t.Helper()
	if err := enTxErr(ctx, pool, fn); err != nil {
		t.Fatalf("tx: %v", err)
	}
}

func enTxErr(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// assertIndiceExiste verifica que un índice existe y que su definición menciona
// las columnas esperadas (comparación textual sobre pg_indexes.indexdef).
func assertIndiceExiste(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nombre string, columnas ...string) {
	t.Helper()
	var def string
	err := pool.QueryRow(ctx,
		`SELECT indexdef FROM pg_indexes WHERE schemaname='identidad' AND indexname=$1`, nombre).Scan(&def)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("el índice %s no existe", nombre)
	}
	if err != nil {
		t.Fatalf("consultar índice %s: %v", nombre, err)
	}
	for _, col := range columnas {
		if !strings.Contains(def, col) {
			t.Errorf("la definición de %s no menciona %q: %s", nombre, col, def)
		}
	}
}

// assertCodigoPg verifica que err es un error de Postgres con el SQLSTATE dado.
func assertCodigoPg(t *testing.T, err error, codigo, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: se esperaba un error %s, la operación tuvo éxito", msg, codigo)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("%s: se esperaba un *pgconn.PgError %s, se obtuvo: %v", msg, codigo, err)
	}
	if pgErr.Code != codigo {
		t.Errorf("%s: SQLSTATE = %s; se esperaba %s", msg, pgErr.Code, codigo)
	}
}

// assertDenegado verifica que err es un "permission denied" de Postgres.
func assertDenegado(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: la operación tuvo éxito pero debía denegarse", msg)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("%s: se esperaba 'permission denied', se obtuvo: %v", msg, err)
	}
}
