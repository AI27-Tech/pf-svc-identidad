package sesion

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
)

// TestStoreInsertTraduce23505 verifica store.insert (Tarea 7.3, Requisito 2.1):
// la fila se persiste con su idp_session_id, y una segunda sesión viva del mismo
// sujeto viola el índice una_sesion_viva (23505), que se traduce a
// ErrLiveSesionExists. No modifica el índice. Se salta sin IDENTIDAD_TEST_DB.
func TestStoreInsertTraduce23505(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := prepararPoolSesion(t, ctx)
	defer pool.Close()

	st := newStore(poolBeginTx{pool})

	sujetoID := "11111111-1111-1111-1111-111111111111"
	// 0006 agregó la FK sujeto.tenant_id → tenant(id): sembrar el tenant antes
	// del sujeto (que a su vez es FK de sesion).
	tenantID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	mustExecPool(t, ctx, pool,
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1,'AI27','interno')`,
		tenantID)
	mustExecPool(t, ctx, pool,
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ($1,'idp-1',$2)`,
		sujetoID, tenantID)

	// Primera fila viva: se persiste con su idp_session_id.
	primera := SesionRecord{
		SesionID:     "33333333-3333-3333-3333-333333333333",
		SujetoID:     sujetoID,
		EmitidaEn:    time.Now(),
		ExpiraEn:     time.Now().Add(time.Hour),
		IdpSessionID: "idp-session-primera",
	}
	if err := conTx(ctx, pool, func(tx pgx.Tx) error { return st.insert(ctx, tx, primera) }); err != nil {
		t.Fatalf("primera inserción debería tener éxito: %v", err)
	}

	// Verificar que el idp_session_id se persistió tal cual.
	var idpGuardado string
	if err := pool.QueryRow(ctx,
		`SELECT idp_session_id FROM identidad.sesion WHERE sesion_id = $1`, primera.SesionID,
	).Scan(&idpGuardado); err != nil {
		t.Fatalf("leer idp_session_id: %v", err)
	}
	if idpGuardado != primera.IdpSessionID {
		t.Errorf("idp_session_id guardado = %q; se esperaba %q", idpGuardado, primera.IdpSessionID)
	}

	// Segunda fila viva del MISMO sujeto: viola una_sesion_viva (23505).
	segunda := SesionRecord{
		SesionID:     "44444444-4444-4444-4444-444444444444",
		SujetoID:     sujetoID,
		EmitidaEn:    time.Now(),
		ExpiraEn:     time.Now().Add(time.Hour),
		IdpSessionID: "idp-session-segunda",
	}
	err := conTx(ctx, pool, func(tx pgx.Tx) error { return st.insert(ctx, tx, segunda) })
	if err == nil {
		t.Fatal("la segunda sesión viva del mismo sujeto debería fallar (una_sesion_viva)")
	}
	if !stderrors.Is(err, ErrLiveSesionExists) {
		t.Errorf("error = %v; se esperaba que envolviera ErrLiveSesionExists", err)
	}
}

// --- Utilidades de prueba de este paquete ----------------------------------

// poolBeginTx adapta *pgxpool.Pool a la interfaz BeginTx.
type poolBeginTx struct{ pool *pgxpool.Pool }

func (p poolBeginTx) Begin(ctx context.Context) (pgx.Tx, error) { return p.pool.Begin(ctx) }

// conTx abre una tx, ejecuta fn y hace commit si no hubo error.
func conTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
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

// prepararPoolSesion aplica migraciones a una base limpia y devuelve un pool
// con el rol svc_identidad. Se salta sin IDENTIDAD_TEST_DB.
func prepararPoolSesion(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	t.Cleanup(closeAdmin) // cerrar al final del test, no al salir de esta función

	const esquema = "identidad"
	const rol = "svc_identidad"
	// La limpieza al final del test usa un contexto fresco: el ctx del test
	// puede estar ya cancelado (su timeout/defer) cuando corren los t.Cleanup.
	limpiar := func() {
		limpCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		dbtest.Exec(t, limpCtx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{esquema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, limpCtx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{rol}.Sanitize())
	}
	limpiar()
	t.Cleanup(limpiar)

	// Aplicar todas las migraciones con el admin.
	aplicarMigracionesSesion(t, ctx, admin)

	pool, closePool := dbtest.PoolAs(t, ctx, rol, esquema)
	t.Cleanup(closePool)
	return pool
}

func aplicarMigracionesSesion(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	entradas, err := migrations.FS.ReadDir("identidad")
	if err != nil {
		t.Fatalf("listar migraciones: %v", err)
	}
	nombres := make([]string, 0, len(entradas))
	for _, e := range entradas {
		nombres = append(nombres, e.Name())
	}
	// ReadDir ya devuelve orden lexicográfico, pero lo garantizamos.
	for i := 0; i < len(nombres); i++ {
		for j := i + 1; j < len(nombres); j++ {
			if nombres[j] < nombres[i] {
				nombres[i], nombres[j] = nombres[j], nombres[i]
			}
		}
	}
	for _, n := range nombres {
		sql, err := migrations.FS.ReadFile("identidad/" + n)
		if err != nil {
			t.Fatalf("leer %s: %v", n, err)
		}
		if _, err := admin.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("aplicar %s: %v", n, err)
		}
	}
}

func mustExecPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
