package gobierno_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
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

// subordinadosDe lee de la tabla los subordinados del superior, ordenados.
func subordinadosDe(t *testing.T, ctx context.Context, pool *pgxpool.Pool, superiorID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(string_agg(subordinado_id::text, ',' ORDER BY subordinado_id), '') FROM identidad.jerarquia WHERE superior_id = $1`,
		superiorID).Scan(&s); err != nil {
		t.Fatalf("leer subordinados: %v", err)
	}
	return s
}

// ordenados une ids en el orden de la tabla.
func ordenados(ids ...string) string {
	sort.Strings(ids)
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}

// TestFijarSubordinadosSujetoPg cubre fijarSubordinadosSujeto: reemplazo
// idempotente, Supervisor → Monitoristas y Coordinador → Supervisores,
// exclusividad (DEC-145 #2), solo administrador y rechazos sin efectos.
func TestFijarSubordinadosSujetoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())
	sup := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	sup2 := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	coord := altaConRol(t, ctx, svc, autorizacion.RolCoordinador)
	jefe := altaConRol(t, ctx, svc, autorizacion.RolJefeOperativo)
	m1 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	m2 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	m3 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)

	fijar := func(superior string, subs []string, rol autorizacion.Rol) error {
		return svc.FijarSubordinadosSujeto(ctx, gobierno.FijacionSubordinados{SujetoID: superior, SubordinadosIDs: subs, ActorID: admin1, ActorRol: rol})
	}
	const admin = autorizacion.RolAdministrador

	// Idempotente, con un repetido normalizado.
	for i := 0; i < 2; i++ {
		if err := fijar(sup, []string{m1, m2, m1}, admin); err != nil {
			t.Fatalf("fijar (vuelta %d): %v", i+1, err)
		}
		if got, want := subordinadosDe(t, ctx, pool, sup), ordenados(m1, m2); got != want {
			t.Fatalf("vuelta %d: %q; quiere %q", i+1, got, want)
		}
	}
	// Reemplazar suelta a m1; repetir a los que ya son suyos no es error.
	for i := 0; i < 2; i++ {
		if err := fijar(sup, []string{m2, m3}, admin); err != nil {
			t.Fatalf("reemplazar (vuelta %d): %v", i+1, err)
		}
	}
	if got, want := subordinadosDe(t, ctx, pool, sup), ordenados(m2, m3); got != want {
		t.Errorf("tras reemplazar: %q; quiere %q", got, want)
	}
	// m1 quedó libre: otro Supervisor lo toma.
	if err := fijar(sup2, []string{m1}, admin); err != nil {
		t.Errorf("sup2 toma a m1: %v", err)
	}
	// Coordinador → Supervisores.
	if err := fijar(coord, []string{sup}, admin); err != nil {
		t.Errorf("coordinador → supervisor: %v", err)
	}

	antes := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.jerarquia`)
	rechazos := []struct {
		nombre         string
		superior       string
		subs           []string
		rol            autorizacion.Rol
		status         int
		codigoEsperado string
	}{
		{"jefe_operativo no escribe", sup, []string{m1}, autorizacion.RolJefeOperativo, 403, "prohibido"},
		{"supervisor no escribe", sup, []string{m1}, autorizacion.RolSupervisor, 403, "prohibido"},
		{"superior monitorista", m1, []string{m2}, admin, 409, "rol_sin_alcance"},
		{"superior jefe_operativo", jefe, []string{m1}, admin, 409, "rol_sin_alcance"},
		{"monitorista bajo coordinador", coord, []string{m1}, admin, 409, "subordinado_rol_invalido"},
		{"supervisor bajo supervisor", sup2, []string{sup}, admin, 409, "subordinado_rol_invalido"},
		{"ya tiene otro superior", sup2, []string{m1, m2}, admin, 409, "subordinado_con_superior"},
		{"subordinado inexistente", sup, []string{uuid.NewString()}, admin, 400, "validacion"},
		{"id inválido", sup, []string{"x"}, admin, 400, "validacion"},
		{"superior inexistente", uuid.NewString(), []string{m1}, admin, 404, "no_encontrado"},
	}
	for _, c := range rechazos {
		status, cod := codigoDe(fijar(c.superior, c.subs, c.rol))
		if status != c.status || cod != c.codigoEsperado {
			t.Errorf("%s: %d %q; quiere %d %q", c.nombre, status, cod, c.status, c.codigoEsperado)
		}
	}
	assertConteo(t, ctx, pool, antes, `SELECT count(*) FROM identidad.jerarquia`)
	if got, want := subordinadosDe(t, ctx, pool, sup), ordenados(m2, m3); got != want {
		t.Errorf("sup tras los rechazos: %q; quiere %q", got, want)
	}

	// Lista vacía: le quita todos.
	if err := fijar(sup, []string{}, admin); err != nil {
		t.Fatalf("vaciar: %v", err)
	}
	if got := subordinadosDe(t, ctx, pool, sup); got != "" {
		t.Errorf("sup tras vaciar: %q", got)
	}
}

// beginConPausa abre transacciones reales que, al llegar a Commit, avisan por
// enCommit y esperan a `seguir`. Sirve para dejar una tx con sus filas
// insertadas sin comprometer mientras corre otra.
type beginConPausa struct {
	pool     *pgxpool.Pool
	enCommit chan struct{}
	seguir   chan struct{}
	once     *sync.Once
}

func (b beginConPausa) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return txConPausa{Tx: tx, b: b}, nil
}

type txConPausa struct {
	pgx.Tx
	b beginConPausa
}

func (t txConPausa) Commit(ctx context.Context) error {
	t.b.once.Do(func() { close(t.b.enCommit) })
	<-t.b.seguir
	return t.Tx.Commit(ctx)
}

// TestSubordinadoConSuperiorCarreraPg: dos administradores asignan el mismo
// Monitorista a Supervisores distintos a la vez. La segunda tx pasa la
// validación (aún no ve la fila de la primera) y la unicidad de la tabla la
// detiene; el servicio responde 409 subordinado_con_superior, no un 5xx.
func TestSubordinadoConSuperiorCarreraPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	sup1 := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	sup2 := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	mon := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)

	pausa := beginConPausa{pool: pool, enCommit: make(chan struct{}), seguir: make(chan struct{}), once: &sync.Once{}}
	svcA := nuevoServicioGob(pausa, pool, prov)
	in := func(superior string) gobierno.FijacionSubordinados {
		return gobierno.FijacionSubordinados{SujetoID: superior, SubordinadosIDs: []string{mon}, ActorID: admin1, ActorRol: autorizacion.RolAdministrador}
	}

	errA := make(chan error, 1)
	go func() { errA <- svcA.FijarSubordinadosSujeto(ctx, in(sup1)) }()
	select {
	case <-pausa.enCommit:
	case err := <-errA:
		t.Fatalf("A terminó antes de comprometer: %v", err)
	case <-ctx.Done():
		t.Fatal("A no llegó a Commit")
	}

	errB := make(chan error, 1)
	go func() { errB <- svc.FijarSubordinadosSujeto(ctx, in(sup2)) }()
	// B debe quedar esperando el candado del índice único de A. Se mira como
	// admin: svc_identidad no ve las sesiones de pg_stat_activity.
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()
	esperando := false
	for i := 0; i < 200 && !esperando; i++ {
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%INSERT INTO identidad.jerarquia%'`).Scan(&n); err != nil {
			t.Fatalf("mirar pg_stat_activity: %v", err)
		}
		if esperando = n == 1; !esperando {
			time.Sleep(25 * time.Millisecond)
		}
	}
	if !esperando {
		close(pausa.seguir)
		t.Fatalf("B no quedó esperando a A (¿terminó antes?): %v", <-errB)
	}
	close(pausa.seguir)

	if err := <-errA; err != nil {
		t.Fatalf("A: %v", err)
	}
	if status, cod := codigoDe(<-errB); status != 409 || cod != "subordinado_con_superior" {
		t.Errorf("B: %d %q; quiere 409 subordinado_con_superior", status, cod)
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.jerarquia WHERE subordinado_id = $1 AND superior_id = $2`, mon, sup1)
	assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.jerarquia WHERE superior_id = $1`, sup2)
}
