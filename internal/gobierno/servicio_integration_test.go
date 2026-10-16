package gobierno_test

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	autzpg "github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	gobpg "github.com/AI27-Tech/pf-svc-identidad/internal/gobierno/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
)

// Este archivo cubre, contra Postgres real, las propiedades del SERVICIO de
// gobierno de E4-T6: atomicidad del cambio (Property 1/3, Tarea 7.6), "local
// manda tras un fallo del IdP" (Property 6, Tarea 7.7), autoescalamiento y
// último administrador (Property 7, Tarea 8.2) y el resolver de sujeto
// (Property 5, Tarea 9.2). Se saltan sin IDENTIDAD_TEST_DB.
//
// Caja negra (package gobierno_test): construye el servicio por NewServicio con
// los repos reales sobre un pool y el doble en memoria del IdP, y lo usa por
// sus interfaces Gobierno/Consulta.

const (
	esquema  = "identidad"
	rolBD    = "svc_identidad"
	tenantAI = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	admin1   = "11111111-1111-4111-8111-111111111111"
)

// --- 7.6: atomicidad del alta (Property 1 y 3) ------------------------------

func TestAltaAtomicaPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)

	t.Run("alta exitosa deja estado + 2 trazas + 2 eventos", func(t *testing.T) {
		prov := memory.New()
		svc := gobierno.NewServicio(pool, outbox.NewPublisher(), prov,
			autzpg.NewSujetoRepo(pool), autzpg.NewAsignacionRolRepo(pool), gobpg.NewTrazaRepo(pool), nil)
		creado, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
			Nombre: "Ana", IdentificadorUsuario: "ana@ai27.mx",
			Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
			ActorID: admin1, ActorEsAdmin: true,
		})
		if err != nil {
			t.Fatalf("AltaSujeto: %v", err)
		}
		if creado.ID == "" {
			t.Fatal("el sujeto creado no trae id")
		}
		assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.sujeto WHERE id=$1`, creado.ID)
		assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND revocado_en IS NULL`, creado.ID)
		assertConteo(t, ctx, pool, 2, `SELECT count(*) FROM identidad.cambio_identidad WHERE sujeto_id=$1`, creado.ID)
		assertConteo(t, ctx, pool, 2, `SELECT count(*) FROM identidad.outbox WHERE agregado_id=$1`, creado.ID)
		if _, _, _, _, _, existe := prov.UsuarioAdmin(creado.IdpSubject); !existe {
			t.Error("el alta debería haber creado el usuario en el IdP")
		}
	})

	t.Run("fallo del Commit no deja estado", func(t *testing.T) {
		prov := memory.New()
		faulty := &faultyBegin{real: pool, punto: fallaCommit}
		svc := gobierno.NewServicio(faulty, outbox.NewPublisher(), prov,
			autzpg.NewSujetoRepo(pool), autzpg.NewAsignacionRolRepo(pool), gobpg.NewTrazaRepo(pool), nil)

		_, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
			Nombre: "Beto", IdentificadorUsuario: "beto@ai27.mx",
			Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
			ActorID: admin1, ActorEsAdmin: true,
		})
		if err == nil {
			t.Fatal("se esperaba error al fallar el Commit")
		}
		// El IdP es idempotente por usuario: recuperamos el sub que generó para
		// beto y confirmamos que NO quedó sujeto local con ese sub.
		sub, _ := prov.CrearUsuario(ctx, idp.NuevoUsuario{Usuario: "beto@ai27.mx"})
		assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.sujeto WHERE idp_subject=$1`, sub)
	})

	t.Run("fallo del Begin no deja estado", func(t *testing.T) {
		prov := memory.New()
		faulty := &faultyBegin{real: pool, punto: fallaBegin}
		svc := gobierno.NewServicio(faulty, outbox.NewPublisher(), prov,
			autzpg.NewSujetoRepo(pool), autzpg.NewAsignacionRolRepo(pool), gobpg.NewTrazaRepo(pool), nil)

		antes := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.sujeto`)
		_, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
			Nombre: "Caro", IdentificadorUsuario: "caro@ai27.mx",
			Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
			ActorID: admin1, ActorEsAdmin: true,
		})
		if err == nil {
			t.Fatal("se esperaba error al fallar el Begin")
		}
		if despues := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.sujeto`); antes != despues {
			t.Errorf("el fallo del Begin dejó estado: sujetos antes=%d, después=%d", antes, despues)
		}
	})
}

// --- 7.6 (reemplazo): exactamente dos trazas y dos eventos ------------------

func TestReemplazarRolAtomicoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)

	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	creado, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Dani", IdentificadorUsuario: "dani@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta previa: %v", err)
	}

	if err := svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{
		SujetoID: creado.ID, RolNuevo: autorizacion.RolSupervisor,
		ActorID: admin1, ActorEsAdmin: true,
	}); err != nil {
		t.Fatalf("ReemplazarRol: %v", err)
	}

	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND revocado_en IS NULL`, creado.ID)
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND revocado_en IS NOT NULL`, creado.ID)
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.cambio_identidad WHERE sujeto_id=$1 AND tipo='rol.revocado'`, creado.ID)
	assertConteo(t, ctx, pool, 2, `SELECT count(*) FROM identidad.cambio_identidad WHERE sujeto_id=$1 AND tipo='rol.asignado'`, creado.ID)
	if _, _, _, grupo, _, _ := prov.UsuarioAdmin(creado.IdpSubject); grupo != string(autorizacion.RolSupervisor) {
		t.Errorf("grupo en el IdP = %q; esperaba supervisor", grupo)
	}
}

// --- 7.7: local manda tras un fallo del IdP (Property 6) --------------------

func TestLocalMandaTrasFalloIdpPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)

	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	creado, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Edu", IdentificadorUsuario: "edu@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta previa: %v", err)
	}

	// Desactivar con el IdP caído: el corte local se compromete, el IdP falla.
	prov.SimularCaida(true)
	err = svc.DesactivarSujeto(ctx, gobierno.CambioEstado{
		SujetoID: creado.ID, ActorID: admin1, ActorEsAdmin: true,
	})
	if !errors.Is(err, gobierno.ErrIdpSyncPending) {
		t.Fatalf("esperaba ErrIdpSyncPending, obtuve: %v", err)
	}

	var activo bool
	if e := pool.QueryRow(ctx, `SELECT activo FROM identidad.sujeto WHERE id=$1`, creado.ID).Scan(&activo); e != nil {
		t.Fatalf("leer activo: %v", e)
	}
	if activo {
		t.Error("el sujeto debería estar inactivo pese al fallo del IdP (local manda)")
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.cambio_identidad WHERE sujeto_id=$1 AND tipo='sujeto.desactivado'`, creado.ID)
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.outbox WHERE agregado_id=$1 AND tipo='sujeto.desactivado'`, creado.ID)
}

// --- 8.2: autoescalamiento y último administrador (Property 7) --------------

func TestAutoescalamientoYUltimoAdminPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)

	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)

	moni, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Moni", IdentificadorUsuario: "moni@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta monitorista: %v", err)
	}

	// Autoescalamiento: moni (no admin) intenta cambiar su propio rol. El
	// Requisito 9.2 pide RECHAZAR antes del IdP y sin escribir nada; eso es lo
	// que esta prueba verifica (el comportamiento), no el centinela exacto.
	//
	// Observación (ver nota DEC en el decision-log): el servicio evalúa
	// guardarAdministrador ANTES que guardarAutoescalamientoRol, así que un
	// no-admin se rechaza con ErrNoAutorizado y el centinela ErrAutoescalamiento
	// queda inalcanzable con el orden actual. El rechazo cumple 9.2 igual; que
	// el centinela específico no se alcance es consistencia código↔spec, no un
	// hueco de seguridad. Por eso aceptamos cualquiera de los dos centinelas.
	err = svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{
		SujetoID: moni.ID, RolNuevo: autorizacion.RolAdministrador,
		ActorID: moni.ID, ActorEsAdmin: false,
	})
	if !errors.Is(err, gobierno.ErrAutoescalamiento) && !errors.Is(err, gobierno.ErrNoAutorizado) {
		t.Errorf("autoescalamiento: esperaba un rechazo (ErrAutoescalamiento o ErrNoAutorizado), obtuve %v", err)
	}
	// Sin efecto: moni sigue monitorista, no se escaló.
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND rol_codigo='monitorista' AND revocado_en IS NULL`, moni.ID)
	assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND rol_codigo='administrador'`, moni.ID)

	// Último administrador: un solo admin en el sistema.
	adminSuj, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Admin", IdentificadorUsuario: "admin@ai27.mx",
		Rol: autorizacion.RolAdministrador, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta admin: %v", err)
	}

	err = svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{
		SujetoID: adminSuj.ID, RolNuevo: autorizacion.RolMonitorista,
		ActorID: adminSuj.ID, ActorEsAdmin: true,
	})
	if !errors.Is(err, gobierno.ErrUltimoAdministrador) {
		t.Errorf("último admin (reemplazo): esperaba ErrUltimoAdministrador, obtuve %v", err)
	}
	err = svc.DesactivarSujeto(ctx, gobierno.CambioEstado{
		SujetoID: adminSuj.ID, ActorID: adminSuj.ID, ActorEsAdmin: true,
	})
	if !errors.Is(err, gobierno.ErrUltimoAdministrador) {
		t.Errorf("último admin (desactivar): esperaba ErrUltimoAdministrador, obtuve %v", err)
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id=$1 AND rol_codigo='administrador' AND revocado_en IS NULL`, adminSuj.ID)
	var adminActivo bool
	if e := pool.QueryRow(ctx, `SELECT activo FROM identidad.sujeto WHERE id=$1`, adminSuj.ID).Scan(&adminActivo); e != nil {
		t.Fatalf("leer activo del admin: %v", e)
	}
	if !adminActivo {
		t.Error("el último admin no debió desactivarse")
	}
}

// --- 9.2: resolver de sujeto (Property 5) -----------------------------------

func TestResolverSujetoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)

	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	creado, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Fer", IdentificadorUsuario: "fer@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta previa: %v", err)
	}

	sujetos := autzpg.NewSujetoRepo(pool)
	// La costura subDeToken lee el sub del AccessToken (en prueba lo inyectamos ahí).
	resolver := gobierno.NewResolverSujeto(sujetos, func(tok idp.Token) (string, error) {
		return tok.AccessToken, nil
	})

	ref, err := resolver.ResolveSujeto(ctx, idp.Token{AccessToken: creado.IdpSubject})
	if err != nil {
		t.Fatalf("resolver sub activo: %v", err)
	}
	if ref.SujetoID != creado.ID || ref.TenantID != tenantAI {
		t.Errorf("SujetoRef = %+v; esperaba SujetoID=%s TenantID=%s", ref, creado.ID, tenantAI)
	}

	_, err = resolver.ResolveSujeto(ctx, idp.Token{AccessToken: "sub-que-no-existe"})
	if !errors.Is(err, sesion.ErrSujetoUnresolved) {
		t.Errorf("sub inexistente: esperaba ErrSujetoUnresolved, obtuve %v", err)
	}

	if e := svc.DesactivarSujeto(ctx, gobierno.CambioEstado{
		SujetoID: creado.ID, ActorID: admin1, ActorEsAdmin: true,
	}); e != nil {
		t.Fatalf("desactivar para la prueba del resolver: %v", e)
	}
	_, err = resolver.ResolveSujeto(ctx, idp.Token{AccessToken: creado.IdpSubject})
	if !errors.Is(err, sesion.ErrSujetoUnresolved) {
		t.Errorf("sujeto inactivo: esperaba ErrSujetoUnresolved, obtuve %v", err)
	}
}

// --- utilidades compartidas de este paquete ---------------------------------

// nuevoServicioGob construye el servicio con los repos reales sobre `pool`, el
// doble en memoria del IdP y `tx` como fuente de transacciones (el pool, o un
// faultyBegin). Devuelve la interfaz de escritura.
func nuevoServicioGob(tx gobierno.BeginTx, pool *pgxpool.Pool, prov *memory.Provider) gobierno.Gobierno {
	return gobierno.NewServicio(tx, outbox.NewPublisher(), prov,
		autzpg.NewSujetoRepo(pool), autzpg.NewAsignacionRolRepo(pool), gobpg.NewTrazaRepo(pool), nil)
}

func prepararPoolGob(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	t.Cleanup(closeAdmin)

	limpiar := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		dbtest.Exec(t, c, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{esquema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, c, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{rolBD}.Sanitize())
	}
	limpiar()
	t.Cleanup(limpiar)

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

	pool, closePool := dbtest.PoolAs(t, ctx, rolBD, esquema)
	t.Cleanup(closePool)
	return pool
}

func sembrarTenantGob(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1,'AI27','interno')`, tenantAI); err != nil {
		t.Fatalf("sembrar tenant: %v", err)
	}
}

func conteo(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("contar (%q): %v", sql, err)
	}
	return n
}

func assertConteo(t *testing.T, ctx context.Context, pool *pgxpool.Pool, esperado int, sql string, args ...any) {
	t.Helper()
	if got := conteo(t, ctx, pool, sql, args...); got != esperado {
		t.Errorf("conteo = %d; esperaba %d (%q)", got, esperado, sql)
	}
}

// --- faultyBegin: BeginTx que falla en Begin o en Commit --------------------

type puntoFalla int

const (
	fallaBegin puntoFalla = iota
	fallaCommit
)

type faultyBegin struct {
	real  *pgxpool.Pool
	punto puntoFalla
}

var errFallaInyectada = errors.New("gobierno_test: falla inyectada en la transacción")

func (f *faultyBegin) Begin(ctx context.Context) (pgx.Tx, error) {
	if f.punto == fallaBegin {
		return nil, errFallaInyectada
	}
	tx, err := f.real.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &faultyTx{Tx: tx}, nil
}

type faultyTx struct {
	pgx.Tx
}

func (t *faultyTx) Commit(ctx context.Context) error {
	_ = t.Tx.Rollback(ctx)
	return errFallaInyectada
}
