package gobierno_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// Pruebas de la tanda B de tracer.40 (alcance de monitoreo, DEC-140, DEC-145)
// contra Postgres real. Se saltan sin IDENTIDAD_TEST_DB.

// altaConRol da de alta un sujeto interno con el rol dado y devuelve su id.
func altaConRol(t *testing.T, ctx context.Context, svc gobierno.Gobierno, rol autorizacion.Rol) string {
	t.Helper()
	ident := string(rol) + "-" + uuid.NewString()[:8] + "@ai27.mx"
	s, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: ident, IdentificadorUsuario: ident, Rol: rol, TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta %s: %v", rol, err)
	}
	return s.ID
}

// unidadesDe lee de la tabla las unidades del Monitorista, ordenadas.
func unidadesDe(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoristaID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(string_agg(unidad_id::text, ',' ORDER BY unidad_id), '') FROM identidad.alcance_unidad WHERE monitorista_id = $1`,
		monitoristaID).Scan(&s); err != nil {
		t.Fatalf("leer unidades: %v", err)
	}
	return s
}

// codigoDe devuelve el status y el `codigo` que vería el cliente.
func codigoDe(err error) (int, string) {
	c := gobierno.Classify(err)
	return platformerrors.AsCategory(c).HTTPStatus(), platformerrors.CodigoDe(c)
}

// unidadesOrdenadas devuelve n ids de unidad ordenados, para comparar con la
// tabla sin depender del orden de generación.
func unidadesOrdenadas(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuid.NewString()
	}
	sort.Strings(out)
	return out
}

// TestFijarUnidadesSujetoPg cubre fijarUnidadesSujeto: reemplazo idempotente,
// repetidos normalizados, unidad compartida entre dos Monitoristas (DEC-145
// #1), permisos (administrador y jefe_operativo) y rechazos sin efectos.
func TestFijarUnidadesSujetoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())
	m1 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	m2 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	sup := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	jefe := altaConRol(t, ctx, svc, autorizacion.RolJefeOperativo)
	u := unidadesOrdenadas(3)

	fijar := func(sujeto string, unidades []string, actor string, rol autorizacion.Rol) error {
		return svc.FijarUnidadesSujeto(ctx, gobierno.FijacionUnidades{SujetoID: sujeto, UnidadesIDs: unidades, ActorID: actor, ActorRol: rol})
	}

	// Reemplazo idempotente, con un repetido que se normaliza en silencio.
	for i := 0; i < 2; i++ {
		if err := fijar(m1, []string{u[1], u[0], u[1]}, admin1, autorizacion.RolAdministrador); err != nil {
			t.Fatalf("fijar (vuelta %d): %v", i+1, err)
		}
		if got, want := unidadesDe(t, ctx, pool, m1), u[0]+","+u[1]; got != want {
			t.Fatalf("vuelta %d: unidades %q; quiere %q", i+1, got, want)
		}
	}
	// Reemplazar quita las que no vienen.
	if err := fijar(m1, []string{u[1], u[2]}, admin1, autorizacion.RolAdministrador); err != nil {
		t.Fatalf("reemplazar: %v", err)
	}
	if got, want := unidadesDe(t, ctx, pool, m1), u[1]+","+u[2]; got != want {
		t.Errorf("tras reemplazar: %q; quiere %q", got, want)
	}

	// El jefe_operativo asigna la misma unidad a otro Monitorista: la comparten.
	if err := fijar(m2, []string{u[1]}, jefe, autorizacion.RolJefeOperativo); err != nil {
		t.Fatalf("jefe fija: %v", err)
	}
	assertConteo(t, ctx, pool, 2, `SELECT count(*) FROM identidad.alcance_unidad WHERE unidad_id = $1`, u[1])
	// Lista vacía: le quita todas a m2 y no toca a m1.
	if err := fijar(m2, []string{}, jefe, autorizacion.RolJefeOperativo); err != nil {
		t.Fatalf("vaciar: %v", err)
	}
	if got := unidadesDe(t, ctx, pool, m2); got != "" {
		t.Errorf("m2 tras vaciar: %q; quiere vacío", got)
	}

	desactivado := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	if err := svc.DesactivarSujeto(ctx, gobierno.CambioEstado{SujetoID: desactivado, ActorID: admin1, ActorEsAdmin: true}); err != nil {
		t.Fatalf("desactivar: %v", err)
	}
	antes := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.alcance_unidad`)
	rechazos := []struct {
		nombre         string
		sujeto, actor  string
		rol            autorizacion.Rol
		unidades       []string
		status         int
		codigoEsperado string
	}{
		{"monitorista no escribe", m1, m2, autorizacion.RolMonitorista, []string{u[0]}, 403, "prohibido"},
		{"supervisor no escribe", m1, sup, autorizacion.RolSupervisor, []string{u[0]}, 403, "prohibido"},
		{"su propio alcance", jefe, jefe, autorizacion.RolJefeOperativo, []string{u[0]}, 403, "autoescalamiento"},
		{"objetivo sin rol monitorista", sup, admin1, autorizacion.RolAdministrador, []string{u[0]}, 409, "rol_sin_alcance"},
		{"objetivo desactivado", desactivado, admin1, autorizacion.RolAdministrador, []string{u[0]}, 409, "sujeto_desactivado"},
		{"objetivo inexistente", uuid.NewString(), admin1, autorizacion.RolAdministrador, []string{u[0]}, 404, "no_encontrado"},
		{"id de unidad inválido", m1, admin1, autorizacion.RolAdministrador, []string{"no-es-uuid"}, 400, "validacion"},
	}
	for _, c := range rechazos {
		status, cod := codigoDe(fijar(c.sujeto, c.unidades, c.actor, c.rol))
		if status != c.status || cod != c.codigoEsperado {
			t.Errorf("%s: %d %q; quiere %d %q", c.nombre, status, cod, c.status, c.codigoEsperado)
		}
	}
	assertConteo(t, ctx, pool, antes, `SELECT count(*) FROM identidad.alcance_unidad`)
	if got, want := unidadesDe(t, ctx, pool, m1), u[1]+","+u[2]; got != want {
		t.Errorf("m1 tras los rechazos: %q; quiere %q", got, want)
	}
}
