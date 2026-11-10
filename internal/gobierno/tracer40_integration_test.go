package gobierno_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	autzpg "github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	gobpg "github.com/AI27-Tech/pf-svc-identidad/internal/gobierno/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// Pruebas de la tanda A de tracer.40 (E8-T19) contra Postgres real. Se saltan
// sin IDENTIDAD_TEST_DB.

// TestDatosLocalesDelSujetoPg: el alta guarda nombre e identificador en local y
// la lectura los devuelve; un sujeto previo a tracer.40 (columnas NULL) se lee
// con los dos vacíos (B-15).
func TestDatosLocalesDelSujetoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())

	creado, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Ana López", IdentificadorUsuario: "ana@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("AltaSujeto: %v", err)
	}
	if creado.Nombre != "Ana López" || creado.IdentificadorUsuario != "ana@ai27.mx" {
		t.Errorf("alta devolvió nombre=%q identificador=%q", creado.Nombre, creado.IdentificadorUsuario)
	}
	det, err := consultaGob(pool).SujetoPorID(ctx, gobierno.LecturaSujeto{SujetoID: creado.ID, ActorEsAdmin: true})
	if err != nil {
		t.Fatalf("SujetoPorID: %v", err)
	}
	if det.Sujeto.Nombre != "Ana López" || det.Sujeto.IdentificadorUsuario != "ana@ai27.mx" {
		t.Errorf("lectura devolvió nombre=%q identificador=%q", det.Sujeto.Nombre, det.Sujeto.IdentificadorUsuario)
	}

	const previo = "44444444-4444-4444-8444-444444444444"
	if _, err := pool.Exec(ctx, `INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ($1, 'sub-previo', $2)`, previo, tenantAI); err != nil {
		t.Fatalf("sembrar sujeto previo: %v", err)
	}
	det, err = consultaGob(pool).SujetoPorID(ctx, gobierno.LecturaSujeto{SujetoID: previo, ActorEsAdmin: true})
	if err != nil {
		t.Fatalf("SujetoPorID previo: %v", err)
	}
	if det.Sujeto.Nombre != "" || det.Sujeto.IdentificadorUsuario != "" {
		t.Errorf("sujeto previo: nombre=%q identificador=%q; quiere vacíos", det.Sujeto.Nombre, det.Sujeto.IdentificadorUsuario)
	}
}

// consultaGob construye el servicio de gobierno sobre `pool` y devuelve su
// superficie de lectura.
func consultaGob(pool *pgxpool.Pool) gobierno.Consulta {
	return gobierno.NewServicio(pool, outbox.NewPublisher(), memory.New(),
		autzpg.NewSujetoRepo(pool), autzpg.NewAsignacionRolRepo(pool), gobpg.NewTrazaRepo(pool), nil)
}

// TestLecturaRestringidaAAdministradorPg: obtenerSujeto y el historial los lee
// solo un administrador (G-10). Antes el handler resolvía el actor pero no
// miraba su rol.
func TestLecturaRestringidaAAdministradorPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	creado, err := nuevoServicioGob(pool, pool, memory.New()).AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Eva", IdentificadorUsuario: "eva@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("AltaSujeto: %v", err)
	}
	lectura := gobierno.LecturaSujeto{SujetoID: creado.ID, ActorEsAdmin: false}
	if _, err := consultaGob(pool).SujetoPorID(ctx, lectura); !errors.Is(err, gobierno.ErrNoAutorizado) {
		t.Errorf("SujetoPorID sin admin: esperaba ErrNoAutorizado, obtuve %v", err)
	}
	if _, err := consultaGob(pool).HistorialDe(ctx, lectura, ""); !errors.Is(err, gobierno.ErrNoAutorizado) {
		t.Errorf("HistorialDe sin admin: esperaba ErrNoAutorizado, obtuve %v", err)
	}
}

// TestListarSujetosPg cubre listarSujetos (B-2): filtros combinables, búsqueda
// sin distinguir mayúsculas con comodines literales, sujeto previo sin datos
// locales solo sin `q`, paginación por cursor y permisos (administrador; el
// jefe_operativo solo con rol=monitorista).
func TestListarSujetosPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	const tenantCli = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := pool.Exec(ctx, `INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1,'Cliente SA','cliente')`, tenantCli); err != nil {
		t.Fatalf("sembrar tenant cliente: %v", err)
	}
	svc := nuevoServicioGob(pool, pool, memory.New())
	alta := func(nombre, ident string, rol autorizacion.Rol, tenant string) autorizacion.Sujeto {
		t.Helper()
		s, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
			Nombre: nombre, IdentificadorUsuario: ident, Rol: rol, TenantID: tenant,
			ActorID: admin1, ActorEsAdmin: true,
		})
		if err != nil {
			t.Fatalf("alta %s: %v", nombre, err)
		}
		return s
	}
	ana := alta("Ana López", "ana@ai27.mx", autorizacion.RolMonitorista, tenantAI)
	beto := alta("Beto 100%", "beto@ai27.mx", autorizacion.RolSupervisor, tenantAI)
	carla := alta("Carla", "carla@cliente.mx", autorizacion.RolCliente, tenantCli)
	if _, err := pool.Exec(ctx, `UPDATE identidad.sujeto SET activo = false WHERE id = $1`, beto.ID); err != nil {
		t.Fatalf("desactivar beto: %v", err)
	}
	const previo = "00000000-0000-4000-8000-000000000001"
	if _, err := pool.Exec(ctx, `INSERT INTO identidad.sujeto (id, idp_subject, tenant_id) VALUES ($1, 'sub-previo', $2)`, previo, tenantAI); err != nil {
		t.Fatalf("sembrar sujeto previo: %v", err)
	}

	listar := gobierno.Consulta(consultaGob(pool)).ListarSujetos
	ids := func(f autorizacion.FiltroSujetos) []string {
		t.Helper()
		p, err := listar(ctx, gobierno.ListadoSujetos{Filtro: f, ActorRol: autorizacion.RolAdministrador})
		if err != nil {
			t.Fatalf("ListarSujetos(%+v): %v", f, err)
		}
		out := []string{}
		for _, d := range p.Sujetos {
			out = append(out, d.Sujeto.ID)
		}
		sort.Strings(out)
		return out
	}
	falso := false
	casos := []struct {
		nombre string
		filtro autorizacion.FiltroSujetos
		quiere []string
	}{
		{"sin filtro trae todos, incluido el previo", autorizacion.FiltroSujetos{}, []string{ana.ID, beto.ID, carla.ID, previo}},
		{"q sin distinguir mayúsculas", autorizacion.FiltroSujetos{Q: "ANA"}, []string{ana.ID}},
		{"q busca también en el identificador", autorizacion.FiltroSujetos{Q: "@cliente"}, []string{carla.ID}},
		{"q con % es literal", autorizacion.FiltroSujetos{Q: "100%"}, []string{beto.ID}},
		{"q con _ es literal", autorizacion.FiltroSujetos{Q: "a_a"}, []string{}},
		{"rol activo", autorizacion.FiltroSujetos{Rol: autorizacion.RolMonitorista}, []string{ana.ID}},
		{"tenant", autorizacion.FiltroSujetos{TenantID: tenantCli}, []string{carla.ID}},
		{"solo desactivados", autorizacion.FiltroSujetos{Activo: &falso}, []string{beto.ID}},
		{"filtros combinados", autorizacion.FiltroSujetos{Q: "a", TenantID: tenantAI, Rol: autorizacion.RolMonitorista}, []string{ana.ID}},
	}
	for _, c := range casos {
		want := append([]string{}, c.quiere...)
		sort.Strings(want)
		if got := ids(c.filtro); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: got %v, quiere %v", c.nombre, got, want)
		}
	}
	// El detalle trae el rol activo y los datos locales.
	p, err := listar(ctx, gobierno.ListadoSujetos{Filtro: autorizacion.FiltroSujetos{Q: "ana"}, ActorRol: autorizacion.RolAdministrador})
	if err != nil || len(p.Sujetos) != 1 {
		t.Fatalf("listar ana: %v, %d filas", err, len(p.Sujetos))
	}
	if d := p.Sujetos[0]; !d.TieneRol || d.RolActivo != autorizacion.RolMonitorista || d.Sujeto.Nombre != "Ana López" {
		t.Errorf("detalle de ana: %+v", d)
	}

	// Permisos.
	permisos := []struct {
		actor autorizacion.Rol
		rol   autorizacion.Rol
		ok    bool
	}{
		{autorizacion.RolJefeOperativo, autorizacion.RolMonitorista, true},
		{autorizacion.RolJefeOperativo, "", false},
		{autorizacion.RolJefeOperativo, autorizacion.RolSupervisor, false},
		{autorizacion.RolMonitorista, autorizacion.RolMonitorista, false},
		{autorizacion.RolSupervisor, "", false},
	}
	for _, c := range permisos {
		_, err := listar(ctx, gobierno.ListadoSujetos{Filtro: autorizacion.FiltroSujetos{Rol: c.rol}, ActorRol: c.actor})
		if c.ok && err != nil {
			t.Errorf("%s con rol=%q: esperaba éxito, obtuve %v", c.actor, c.rol, err)
		}
		if !c.ok && !errors.Is(err, gobierno.ErrNoAutorizado) {
			t.Errorf("%s con rol=%q: esperaba ErrNoAutorizado, obtuve %v", c.actor, c.rol, err)
		}
	}
	if _, err := listar(ctx, gobierno.ListadoSujetos{Filtro: autorizacion.FiltroSujetos{Rol: "auditor"}, ActorRol: autorizacion.RolAdministrador}); !errors.Is(err, autorizacion.ErrRolFueraDeCatalogo) {
		t.Errorf("rol fuera de catálogo: esperaba ErrRolFueraDeCatalogo, obtuve %v", err)
	}

	// Paginación: 4 sujetos + 55 más = 59, en dos páginas sin repetir.
	for i := 0; i < 55; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO identidad.sujeto (idp_subject, tenant_id, nombre) VALUES ($1, $2, 'relleno')`, fmt.Sprintf("sub-relleno-%d", i), tenantAI); err != nil {
			t.Fatalf("sembrar relleno: %v", err)
		}
	}
	vistos := map[string]bool{}
	cursor := ""
	paginas := 0
	for {
		p, err := listar(ctx, gobierno.ListadoSujetos{Cursor: cursor, ActorRol: autorizacion.RolAdministrador})
		if err != nil {
			t.Fatalf("página %d: %v", paginas, err)
		}
		paginas++
		for _, d := range p.Sujetos {
			if vistos[d.Sujeto.ID] {
				t.Fatalf("el sujeto %s se repitió entre páginas", d.Sujeto.ID)
			}
			vistos[d.Sujeto.ID] = true
		}
		if p.CursorSiguiente == nil {
			break
		}
		cursor = *p.CursorSiguiente
	}
	if paginas != 2 || len(vistos) != 59 {
		t.Errorf("paginación: %d páginas y %d sujetos; quiere 2 y 59", paginas, len(vistos))
	}
	_, err = listar(ctx, gobierno.ListadoSujetos{Cursor: "no-es-un-cursor", ActorRol: autorizacion.RolAdministrador})
	if platformerrors.AsCategory(err) != platformerrors.CategoryValidacion {
		t.Errorf("cursor inválido: esperaba validación, obtuve %v", err)
	}
}

// TestEditarSujetoPg cubre editarSujeto (B-8): cambia el nombre local y el del
// IdP, no toca el identificador, no escribe traza ni outbox (el contrato no
// publica un hecho de edición) y, si el IdP falla, el cambio local se queda
// (local manda, C1).
func TestEditarSujetoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	creado, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Gabo", IdentificadorUsuario: "gabo@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("AltaSujeto: %v", err)
	}
	trazas := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.cambio_identidad`)
	eventos := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.outbox`)

	det, err := svc.EditarSujeto(ctx, gobierno.EdicionSujeto{SujetoID: creado.ID, Nombre: "Gabriel", ActorID: admin1, ActorEsAdmin: true})
	if err != nil {
		t.Fatalf("EditarSujeto: %v", err)
	}
	if det.Sujeto.Nombre != "Gabriel" || det.Sujeto.IdentificadorUsuario != "gabo@ai27.mx" || det.RolActivo != autorizacion.RolMonitorista {
		t.Errorf("detalle tras editar: %+v", det)
	}
	if got := prov.NombreAdmin(creado.IdpSubject); got != "Gabriel" {
		t.Errorf("nombre en el IdP = %q; quiere Gabriel", got)
	}
	assertConteo(t, ctx, pool, trazas, `SELECT count(*) FROM identidad.cambio_identidad`)
	assertConteo(t, ctx, pool, eventos, `SELECT count(*) FROM identidad.outbox`)

	rechazos := []struct {
		nombre string
		in     gobierno.EdicionSujeto
		cat    platformerrors.Category
	}{
		{"no admin", gobierno.EdicionSujeto{SujetoID: creado.ID, Nombre: "X", ActorID: creado.ID}, platformerrors.CategoryProhibido},
		{"nombre en blanco", gobierno.EdicionSujeto{SujetoID: creado.ID, Nombre: "   ", ActorID: admin1, ActorEsAdmin: true}, platformerrors.CategoryValidacion},
		{"sujeto inexistente", gobierno.EdicionSujeto{SujetoID: "55555555-5555-4555-8555-555555555555", Nombre: "X", ActorID: admin1, ActorEsAdmin: true}, platformerrors.CategoryNoEncontrado},
	}
	for _, c := range rechazos {
		_, err := svc.EditarSujeto(ctx, c.in)
		if got := platformerrors.AsCategory(gobierno.Classify(err)); got != c.cat {
			t.Errorf("%s: categoría %v (err %v); quiere %v", c.nombre, got, err, c.cat)
		}
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.sujeto WHERE id = $1 AND nombre = 'Gabriel'`, creado.ID)

	// IdP caído: el nombre local cambia igual y se avisa que el IdP quedó pendiente.
	prov.SimularCaida(true)
	_, err = svc.EditarSujeto(ctx, gobierno.EdicionSujeto{SujetoID: creado.ID, Nombre: "Gabi", ActorID: admin1, ActorEsAdmin: true})
	prov.SimularCaida(false)
	if !errors.Is(err, gobierno.ErrIdpSyncPending) {
		t.Errorf("IdP caído: esperaba ErrIdpSyncPending, obtuve %v", err)
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.sujeto WHERE id = $1 AND nombre = 'Gabi'`, creado.ID)
}
