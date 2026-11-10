package identidad_test

import (
	"context"
	"io/fs"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"
	platformpgxpool "github.com/AI27-Tech/pf-pkg-platform-go/pgxpool"

	identidad "github.com/AI27-Tech/pf-svc-identidad"
	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
	"github.com/AI27-Tech/pf-svc-identidad/migrations"
)

// TestFachadaGobiernoEnProceso recorre Consultas().Gobierno() como lo hará el
// bff en core: el actor viaja en los claims del contexto, los tipos son los del
// contrato y los errores llevan el mismo `codigo` que el HTTP. Se salta sin
// IDENTIDAD_TEST_DB.
func TestFachadaGobiernoEnProceso(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolFachada(t, ctx)
	const tenantAI = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const adminID = "11111111-1111-4111-8111-111111111111"
	for _, sql := range []string{
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('` + tenantAI + `','AI27','interno')`,
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id, nombre) VALUES ('` + adminID + `','sub-admin','` + tenantAI + `','Admin')`,
		`INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por) VALUES ('` + adminID + `','administrador','` + adminID + `')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("sembrar: %v", err)
		}
	}
	prov := memory.New()
	m, err := identidad.New(identidad.Pools{Identidad: &platformpgxpool.Pool{Pool: pool, Role: "svc_identidad"}}, identidad.Deps{IDP: prov, IDPAdmin: prov})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	g := m.Consultas().Gobierno()
	comoAdmin := auth.ContextWithClaims(ctx, &auth.Claims{Subject: "sub-admin", Rol: "administrador"})

	// Alta, lectura, búsqueda y edición con los tipos del contrato.
	alta, err := g.AltaSujeto(comoAdmin, gen.AltaSujetoEntrada{
		Nombre: "Lalo", IdentificadorUsuario: "lalo@ai27.mx", Rol: gen.RolMonitorista, TenantId: uuid.MustParse(tenantAI),
	})
	if err != nil {
		t.Fatalf("AltaSujeto: %v", err)
	}
	if alta.Nombre == nil || *alta.Nombre != "Lalo" {
		t.Errorf("alta sin nombre local: %+v", alta)
	}
	q := "LALO"
	pagina, err := g.ListarSujetos(comoAdmin, gen.ListarSujetosParams{Q: &q})
	if err != nil || len(pagina.Items) != 1 || pagina.Items[0].Rol == nil || *pagina.Items[0].Rol != gen.RolMonitorista {
		t.Fatalf("ListarSujetos: %+v, %v", pagina, err)
	}
	editado, err := g.EditarSujeto(comoAdmin, alta.Id, gen.EdicionSujetoEntrada{Nombre: "Eduardo"})
	if err != nil || *editado.Sujeto.Nombre != "Eduardo" {
		t.Fatalf("EditarSujeto: %+v, %v", editado, err)
	}
	if det, err := g.ObtenerSujeto(comoAdmin, alta.Id); err != nil || *det.Sujeto.Nombre != "Eduardo" {
		t.Errorf("ObtenerSujeto: %+v, %v", det, err)
	}
	res, err := g.RevocarSesionesSujeto(comoAdmin, alta.Id, gen.RevocacionSesionesEntrada{Motivo: "prueba"})
	if err != nil || res.SesionesRevocadas != 0 {
		t.Errorf("RevocarSesionesSujeto: %+v, %v", res, err)
	}

	// Rechazos con su status y su codigo de máquina, como el HTTP.
	comoLalo := auth.ContextWithClaims(ctx, &auth.Claims{Subject: *alta.IdpSubject, Rol: "monitorista"})
	rechazos := []struct {
		nombre string
		err    error
		status int
		codigo string
	}{
		{"rol igual", g.FijarRolSujeto(comoAdmin, alta.Id, gen.AsignacionRolEntrada{Rol: gen.RolMonitorista}), http.StatusConflict, "rol_igual"},
		{"autoescalamiento", g.FijarRolSujeto(comoLalo, alta.Id, gen.AsignacionRolEntrada{Rol: gen.RolAdministrador}), http.StatusForbidden, "autoescalamiento"},
		{"cruce de audiencia", g.FijarRolSujeto(comoAdmin, alta.Id, gen.AsignacionRolEntrada{Rol: gen.RolCliente}), http.StatusConflict, "rol_cruza_audiencia"},
		{"último administrador", g.DesactivarSujeto(comoAdmin, uuid.MustParse(adminID)), http.StatusConflict, "ultimo_administrador"},
		{"tenant interno", g.DesactivarTenant(comoAdmin, uuid.MustParse(tenantAI)), http.StatusConflict, "conflicto"},
		{"no admin lee", errDe(g.ObtenerSujeto(comoLalo, alta.Id)), http.StatusForbidden, "prohibido"},
		{"sin claims", errDe(g.ListarSujetos(ctx, gen.ListarSujetosParams{})), http.StatusUnauthorized, "no_autorizado"},
	}
	for _, c := range rechazos {
		if got, cod := platformerrors.AsCategory(c.err).HTTPStatus(), platformerrors.CodigoDe(c.err); got != c.status || cod != c.codigo {
			t.Errorf("%s: %d %q (err %v); quiere %d %q", c.nombre, got, cod, c.err, c.status, c.codigo)
		}
	}

	// Tenants.
	org := uuid.New()
	tenant, creado, err := g.CrearTenant(comoAdmin, org, gen.TenantEntrada{Nombre: "Demo SA"})
	if err != nil || !creado || tenant.Tipo != gen.TenantTipoCliente || !tenant.Activa {
		t.Fatalf("CrearTenant: %+v creado=%v err=%v", tenant, creado, err)
	}
	if _, creado, err := g.CrearTenant(comoAdmin, org, gen.TenantEntrada{Nombre: "Demo SA"}); err != nil || creado {
		t.Errorf("CrearTenant repetido: creado=%v err=%v; quiere 200 sin cambios", creado, err)
	}
	if p, err := g.ListarTenants(comoAdmin, gen.ListarTenantsParams{}); err != nil || len(p.Items) != 2 {
		t.Errorf("ListarTenants: %+v, %v", p, err)
	}
	if err := g.DesactivarTenant(comoAdmin, org); err != nil {
		t.Errorf("DesactivarTenant: %v", err)
	}

	// Desactivar y reactivar.
	if err := g.DesactivarSujeto(comoAdmin, alta.Id); err != nil {
		t.Fatalf("DesactivarSujeto: %v", err)
	}
	if err := g.ReactivarSujeto(comoAdmin, alta.Id); err != nil {
		t.Fatalf("ReactivarSujeto: %v", err)
	}

	// Sin IDPAdmin: lo que toca el IdP responde 503 sin escribir; leer sigue.
	sinAdmin, err := identidad.New(identidad.Pools{Identidad: &platformpgxpool.Pool{Pool: pool, Role: "svc_identidad"}}, identidad.Deps{IDP: prov})
	if err != nil {
		t.Fatalf("New sin IDPAdmin: %v", err)
	}
	gs := sinAdmin.Consultas().Gobierno()
	_, err = gs.AltaSujeto(comoAdmin, gen.AltaSujetoEntrada{Nombre: "X", IdentificadorUsuario: "x@ai27.mx", Rol: gen.RolMonitorista, TenantId: uuid.MustParse(tenantAI)})
	if platformerrors.AsCategory(err) != platformerrors.CategoryDegradado {
		t.Errorf("alta sin IDPAdmin: esperaba 503, obtuve %v", err)
	}
	if _, err := gs.ListarSujetos(comoAdmin, gen.ListarSujetosParams{}); err != nil {
		t.Errorf("listar sin IDPAdmin: %v", err)
	}

	// Una ConsultasAPI cero no entra en panic.
	if _, err := (identidad.ConsultasAPI{}).Gobierno().ListarSujetos(comoAdmin, gen.ListarSujetosParams{}); platformerrors.AsCategory(err) != platformerrors.CategoryDegradado {
		t.Errorf("fachada cero: esperaba 503, obtuve %v", err)
	}
}

// errDe descarta el valor y deja el error, para tabular rechazos.
func errDe[T any](_ T, err error) error { return err }

// TestFachadaAlcanceEnProceso recorre las tres operaciones de alcance de
// Consultas().Gobierno() con los tipos del contrato y el actor en los claims,
// como lo hará el bff: el jefe_operativo fija y lee "mis unidades" de un
// Monitorista, el administrador arma la jerarquía y los rechazos llevan el
// mismo `codigo` que el HTTP.
func TestFachadaAlcanceEnProceso(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolFachada(t, ctx)
	const tenantAI = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const adminID = "11111111-1111-4111-8111-111111111111"
	for _, sql := range []string{
		`INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ('` + tenantAI + `','AI27','interno')`,
		`INSERT INTO identidad.sujeto (id, idp_subject, tenant_id, nombre) VALUES ('` + adminID + `','sub-admin','` + tenantAI + `','Admin')`,
		`INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por) VALUES ('` + adminID + `','administrador','` + adminID + `')`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("sembrar: %v", err)
		}
	}
	prov := memory.New()
	m, err := identidad.New(identidad.Pools{Identidad: &platformpgxpool.Pool{Pool: pool, Role: "svc_identidad"}}, identidad.Deps{IDP: prov, IDPAdmin: prov})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	g := m.Consultas().Gobierno()
	comoAdmin := auth.ContextWithClaims(ctx, &auth.Claims{Subject: "sub-admin", Rol: "administrador"})
	alta := func(rol gen.Rol) *gen.Sujeto {
		t.Helper()
		s, err := g.AltaSujeto(comoAdmin, gen.AltaSujetoEntrada{
			Nombre: string(rol), IdentificadorUsuario: string(rol) + uuid.NewString()[:6] + "@ai27.mx", Rol: rol, TenantId: uuid.MustParse(tenantAI),
		})
		if err != nil {
			t.Fatalf("alta %s: %v", rol, err)
		}
		return s
	}
	mon, sup, sup2, jefe := alta(gen.RolMonitorista), alta(gen.RolSupervisor), alta(gen.RolSupervisor), alta(gen.RolJefeOperativo)
	comoJefe := auth.ContextWithClaims(ctx, &auth.Claims{Subject: *jefe.IdpSubject, Rol: "jefe_operativo"})
	comoMon := auth.ContextWithClaims(ctx, &auth.Claims{Subject: *mon.IdpSubject, Rol: "monitorista"})
	u1, u2 := uuid.New(), uuid.New()

	if err := g.FijarUnidadesSujeto(comoJefe, mon.Id, gen.UnidadesSujetoEntrada{UnidadesIds: []gen.Id{u1, u2}}); err != nil {
		t.Fatalf("jefe fija unidades: %v", err)
	}
	if err := g.FijarSubordinadosSujeto(comoAdmin, sup.Id, gen.SubordinadosSujetoEntrada{SujetosIds: []gen.Id{mon.Id}}); err != nil {
		t.Fatalf("admin fija subordinados: %v", err)
	}
	if a, err := g.ObtenerAlcanceSujeto(comoJefe, mon.Id); err != nil || len(a.UnidadesIds) != 2 || len(a.SubordinadosIds) != 0 || a.SubordinadosIds == nil {
		t.Errorf("jefe lee a mon: %+v, %v", a, err)
	}
	a, err := g.ObtenerAlcanceSujeto(comoAdmin, sup.Id)
	if err != nil || len(a.UnidadesIds) != 0 || a.UnidadesIds == nil || len(a.SubordinadosIds) != 1 || a.SubordinadosIds[0] != mon.Id || len(a.UnidadesEfectivasIds) != 2 {
		t.Errorf("admin lee a sup: %+v, %v", a, err)
	}

	rechazos := []struct {
		nombre string
		err    error
		status int
		codigo string
	}{
		{"jefe lee a un supervisor", errDe(g.ObtenerAlcanceSujeto(comoJefe, sup.Id)), http.StatusForbidden, "prohibido"},
		{"jefe arma la jerarquía", g.FijarSubordinadosSujeto(comoJefe, sup2.Id, gen.SubordinadosSujetoEntrada{SujetosIds: []gen.Id{}}), http.StatusForbidden, "prohibido"},
		{"su propio alcance", g.FijarUnidadesSujeto(comoMon, mon.Id, gen.UnidadesSujetoEntrada{UnidadesIds: []gen.Id{u1}}), http.StatusForbidden, "autoescalamiento"},
		{"unidades a un supervisor", g.FijarUnidadesSujeto(comoAdmin, sup.Id, gen.UnidadesSujetoEntrada{UnidadesIds: []gen.Id{u1}}), http.StatusConflict, "rol_sin_alcance"},
		{"supervisor bajo supervisor", g.FijarSubordinadosSujeto(comoAdmin, sup2.Id, gen.SubordinadosSujetoEntrada{SujetosIds: []gen.Id{sup.Id}}), http.StatusConflict, "subordinado_rol_invalido"},
		{"monitorista con otro superior", g.FijarSubordinadosSujeto(comoAdmin, sup2.Id, gen.SubordinadosSujetoEntrada{SujetosIds: []gen.Id{mon.Id}}), http.StatusConflict, "subordinado_con_superior"},
		{"sin claims", g.FijarUnidadesSujeto(ctx, mon.Id, gen.UnidadesSujetoEntrada{}), http.StatusUnauthorized, "no_autorizado"},
	}
	for _, c := range rechazos {
		if got, cod := platformerrors.AsCategory(c.err).HTTPStatus(), platformerrors.CodigoDe(c.err); got != c.status || cod != c.codigo {
			t.Errorf("%s: %d %q (err %v); quiere %d %q", c.nombre, got, cod, c.err, c.status, c.codigo)
		}
	}

	// Cambiar el rol de mon retira su alcance de inmediato (DEC-140 #3).
	if err := g.FijarRolSujeto(comoAdmin, mon.Id, gen.AsignacionRolEntrada{Rol: gen.RolEjecutivo}); err != nil {
		t.Fatalf("FijarRolSujeto: %v", err)
	}
	if a, err := g.ObtenerAlcanceSujeto(comoAdmin, sup.Id); err != nil || len(a.SubordinadosIds) != 0 || len(a.UnidadesEfectivasIds) != 0 {
		t.Errorf("sup tras el cambio de rol de mon: %+v, %v", a, err)
	}
}

func prepararPoolFachada(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	t.Cleanup(closeAdmin)
	limpiar := func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		dbtest.Exec(t, c, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{"identidad"}.Sanitize()+" CASCADE")
		dbtest.Exec(t, c, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{"svc_identidad"}.Sanitize())
	}
	limpiar()
	t.Cleanup(limpiar)
	files, err := fs.Glob(migrations.FS, "identidad/*.sql")
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
	pool, closePool := dbtest.PoolAs(t, ctx, "svc_identidad", "identidad")
	t.Cleanup(closePool)
	return pool
}
