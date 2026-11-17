package gobierno_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// Pruebas de tracer.41 (alcance por cliente, DEC-149 #18 a #20) contra
// Postgres real: "mis clientes", `alcance.cambiado` en el outbox de la misma
// tx, retiro por cambio de rol y el snapshot con cursor de listarAlcances. Se
// saltan sin IDENTIDAD_TEST_DB.

const admin = autorizacion.RolAdministrador

// eventoAlcance es un `alcance.cambiado` leído del outbox.
type eventoAlcance struct {
	Agregado string
	Sujeto   string   `json:"sujeto_id"`
	Clientes []string `json:"clientes_efectivos_ids"`
	Cambiado string   `json:"cambiado_en"`
}

// clave resume un evento como "sujeto=c1,c2" para comparar listas literales.
func (e eventoAlcance) clave() string { return e.Sujeto + "=" + strings.Join(e.Clientes, ",") }

// ultimoOutbox devuelve el id más alto del outbox, para leer solo lo nuevo.
func ultimoOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM identidad.outbox`).Scan(&id); err != nil {
		t.Fatalf("leer outbox: %v", err)
	}
	return id
}

// eventosAlcanceDesde devuelve, ordenados por sujeto, los `alcance.cambiado`
// escritos después del id `desde`.
func eventosAlcanceDesde(t *testing.T, ctx context.Context, pool *pgxpool.Pool, desde int64) []eventoAlcance {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT agregado_id, payload::text FROM identidad.outbox
		 WHERE tipo = 'alcance.cambiado' AND id > $1
		 ORDER BY payload->>'sujeto_id'`, desde)
	if err != nil {
		t.Fatalf("leer alcance.cambiado: %v", err)
	}
	defer rows.Close()
	var out []eventoAlcance
	for rows.Next() {
		var e eventoAlcance
		var payload string
		if err := rows.Scan(&e.Agregado, &payload); err != nil {
			t.Fatalf("escanear outbox: %v", err)
		}
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			t.Fatalf("payload de alcance.cambiado: %v", err)
		}
		if e.Clientes == nil {
			t.Errorf("payload sin clientes_efectivos_ids (null): %s", payload)
		}
		out = append(out, e)
	}
	return out
}

func claves(es []eventoAlcance) string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.clave())
	}
	return strings.Join(out, " ")
}

// efectivosSQL calcula los clientes efectivos con un CTE recursivo, como
// oráculo independiente del cálculo en Go del servicio.
func efectivosSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sujeto string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, `
		WITH RECURSIVE equipo(id) AS (
		  SELECT $1::uuid
		  UNION
		  SELECT j.subordinado_id FROM identidad.jerarquia j JOIN equipo e ON j.superior_id = e.id
		)
		SELECT coalesce(string_agg(DISTINCT c.cliente_id::text, ',' ORDER BY c.cliente_id::text), '')
		  FROM identidad.alcance_cliente c JOIN equipo e ON c.monitorista_id = e.id`, sujeto).Scan(&s); err != nil {
		t.Fatalf("efectivos por SQL: %v", err)
	}
	return s
}

func TestFijarClientesSujetoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())
	m1 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	m2 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	sup := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	jefe := altaConRol(t, ctx, svc, autorizacion.RolJefeOperativo)
	c := unidadesOrdenadas(3) // ids de cliente: solo importa que sean UUID

	fijar := func(sujeto string, clientes []string, actor string, rol autorizacion.Rol) error {
		return svc.FijarClientesSujeto(ctx, gobierno.FijacionClientes{SujetoID: sujeto, ClientesIDs: clientes, ActorID: actor, ActorRol: rol})
	}
	clientesDe := func(m string) string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx,
			`SELECT coalesce(string_agg(cliente_id::text, ',' ORDER BY cliente_id), '') FROM identidad.alcance_cliente WHERE monitorista_id = $1`, m).Scan(&s); err != nil {
			t.Fatalf("leer clientes: %v", err)
		}
		return s
	}

	// Reemplazo idempotente, con un repetido normalizado en silencio.
	for i := 0; i < 2; i++ {
		if err := fijar(m1, []string{c[1], c[0], c[1]}, admin1, admin); err != nil {
			t.Fatalf("fijar (vuelta %d): %v", i+1, err)
		}
		if got, want := clientesDe(m1), c[0]+","+c[1]; got != want {
			t.Fatalf("vuelta %d: clientes %q; quiere %q", i+1, got, want)
		}
	}
	if err := fijar(m1, []string{c[1], c[2]}, admin1, admin); err != nil {
		t.Fatalf("reemplazar: %v", err)
	}
	if got, want := clientesDe(m1), c[1]+","+c[2]; got != want {
		t.Errorf("tras reemplazar: %q; quiere %q", got, want)
	}
	// El jefe_operativo asigna el mismo cliente a otro Monitorista: lo comparten.
	if err := fijar(m2, []string{c[1]}, jefe, autorizacion.RolJefeOperativo); err != nil {
		t.Fatalf("jefe fija: %v", err)
	}
	assertConteo(t, ctx, pool, 2, `SELECT count(*) FROM identidad.alcance_cliente WHERE cliente_id = $1`, c[1])

	desactivado := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	if err := svc.DesactivarSujeto(ctx, gobierno.CambioEstado{SujetoID: desactivado, ActorID: admin1, ActorEsAdmin: true}); err != nil {
		t.Fatalf("desactivar: %v", err)
	}
	antes := conteo(t, ctx, pool, `SELECT count(*) FROM identidad.alcance_cliente`)
	desde := ultimoOutbox(t, ctx, pool)
	rechazos := []struct {
		nombre        string
		sujeto, actor string
		rol           autorizacion.Rol
		clientes      []string
		status        int
		codigo        string
	}{
		{"monitorista no escribe", m1, m2, autorizacion.RolMonitorista, []string{c[0]}, 403, "prohibido"},
		{"supervisor no escribe", m1, sup, autorizacion.RolSupervisor, []string{c[0]}, 403, "prohibido"},
		{"su propio alcance", jefe, jefe, autorizacion.RolJefeOperativo, []string{c[0]}, 403, "autoescalamiento"},
		{"objetivo sin rol monitorista", sup, admin1, admin, []string{c[0]}, 409, "rol_sin_alcance"},
		{"jefe sobre un no Monitorista", sup, jefe, autorizacion.RolJefeOperativo, []string{c[0]}, 409, "rol_sin_alcance"},
		{"objetivo desactivado", desactivado, admin1, admin, []string{c[0]}, 409, "sujeto_desactivado"},
		{"objetivo inexistente", uuid.NewString(), admin1, admin, []string{c[0]}, 404, "no_encontrado"},
		{"id de cliente inválido", m1, admin1, admin, []string{"no-es-uuid"}, 400, "validacion"},
	}
	for _, r := range rechazos {
		status, cod := codigoDe(fijar(r.sujeto, r.clientes, r.actor, r.rol))
		if status != r.status || cod != r.codigo {
			t.Errorf("%s: %d %q; quiere %d %q", r.nombre, status, cod, r.status, r.codigo)
		}
	}
	assertConteo(t, ctx, pool, antes, `SELECT count(*) FROM identidad.alcance_cliente`)
	if got := eventosAlcanceDesde(t, ctx, pool, desde); len(got) != 0 {
		t.Errorf("los rechazos publicaron alcance.cambiado: %s", claves(got))
	}
}

// TestAlcanceCambiadoPg cubre cuándo se publica `alcance.cambiado` y con qué
// conjunto, sobre la cadena Monitorista → Supervisor → Coordinador.
func TestAlcanceCambiadoPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())
	consulta := svc.(gobierno.Consulta)
	coord := altaConRol(t, ctx, svc, autorizacion.RolCoordinador)
	sup := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	mon := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	mon2 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	c := unidadesOrdenadas(3)

	mustSubs := func(superior string, subs ...string) {
		t.Helper()
		if err := svc.FijarSubordinadosSujeto(ctx, gobierno.FijacionSubordinados{SujetoID: superior, SubordinadosIDs: subs, ActorID: admin1, ActorRol: admin}); err != nil {
			t.Fatalf("fijar subordinados: %v", err)
		}
	}
	mustClientes := func(m string, clientes ...string) {
		t.Helper()
		if err := svc.FijarClientesSujeto(ctx, gobierno.FijacionClientes{SujetoID: m, ClientesIDs: clientes, ActorID: admin1, ActorRol: admin}); err != nil {
			t.Fatalf("fijar clientes: %v", err)
		}
	}
	// paso corre f y compara los eventos que escribió con `quiere`.
	paso := func(nombre string, f func(), quiere ...string) {
		t.Helper()
		desde := ultimoOutbox(t, ctx, pool)
		f()
		got := eventosAlcanceDesde(t, ctx, pool, desde)
		if claves(got) != strings.Join(quiere, " ") {
			t.Errorf("%s: eventos %q; quiere %q", nombre, claves(got), strings.Join(quiere, " "))
		}
		for _, e := range got {
			if e.Agregado != e.Sujeto {
				t.Errorf("%s: agregado %q ≠ sujeto %q", nombre, e.Agregado, e.Sujeto)
			}
		}
	}
	// Las claves se ordenan por sujeto_id, como las devuelve eventosAlcanceDesde.
	evs := func(pares ...string) []string {
		out := make([]string, 0, len(pares)/2)
		for i := 0; i < len(pares); i += 2 {
			out = append(out, pares[i]+"="+pares[i+1])
		}
		sort.Strings(out)
		return out
	}

	paso("jerarquía sin clientes no publica", func() { mustSubs(sup, mon, mon2); mustSubs(coord, sup) })
	paso("fijar clientes publica al Monitorista y a su cadena", func() { mustClientes(mon, c[1], c[0]) },
		evs(mon, c[0]+","+c[1], sup, c[0]+","+c[1], coord, c[0]+","+c[1])...)
	paso("PUT repetido no publica", func() { mustClientes(mon, c[0], c[1]) })
	paso("fijar unidades (deprecada) no publica", func() {
		if err := svc.FijarUnidadesSujeto(ctx, gobierno.FijacionUnidades{SujetoID: mon, UnidadesIDs: []string{uuid.NewString()}, ActorID: admin1, ActorRol: admin}); err != nil {
			t.Fatalf("fijar unidades: %v", err)
		}
	})
	paso("cliente ya cubierto por la cadena solo cambia al Monitorista", func() { mustClientes(mon2, c[0], c[2]) },
		evs(mon2, c[0]+","+c[2], sup, c[0]+","+c[1]+","+c[2], coord, c[0]+","+c[1]+","+c[2])...)

	// La lectura devuelve los mismos clientes efectivos que el último evento y
	// coincide con el cálculo recursivo en SQL.
	for _, s := range []string{mon, mon2, sup, coord} {
		a, err := consulta.ObtenerAlcanceSujeto(ctx, gobierno.LecturaAlcance{SujetoID: s, ActorRol: admin})
		if err != nil {
			t.Fatalf("obtener alcance: %v", err)
		}
		if got, want := strings.Join(a.ClientesEfectivosIDs, ","), efectivosSQL(t, ctx, pool, s); got != want {
			t.Errorf("efectivos de %s: %q; el CTE dice %q", s, got, want)
		}
	}
	a, _ := consulta.ObtenerAlcanceSujeto(ctx, gobierno.LecturaAlcance{SujetoID: sup, ActorRol: admin})
	if a.ClientesIDs == nil || len(a.ClientesIDs) != 0 || a.UnidadesIDs == nil {
		t.Errorf("supervisor: clientes directos %v (no nil y vacía), unidades %v (no nil)", a.ClientesIDs, a.UnidadesIDs)
	}

	paso("cambio de rol del Monitorista: vacío para él y recalcula a sus superiores", func() {
		if err := svc.FijarRol(ctx, gobierno.ReemplazoRol{SujetoID: mon, RolNuevo: autorizacion.RolSupervisor, ActorID: admin1, ActorEsAdmin: true}); err != nil {
			t.Fatalf("fijar rol: %v", err)
		}
	}, evs(mon, "", sup, c[0]+","+c[2], coord, c[0]+","+c[2])...)
	assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.alcance_cliente WHERE monitorista_id = $1`, mon)
	d, _ := detalleRolRevocado(t, ctx, pool, mon)["alcance_retirado"].(map[string]any)
	if idsDeDetalle(d["clientes_ids"]) != ordenados(c[0], c[1]) || d["superior_id"] != sup {
		t.Errorf("traza del retiro: %v", d)
	}

	paso("mover a mon2: quitarlo de sup vacía a sup y a coord", func() { mustSubs(sup) },
		evs(sup, "", coord, "")...)
	sup2 := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	paso("y asignarlo a sup2 publica solo a sup2", func() { mustSubs(sup2, mon2) },
		evs(sup2, c[0]+","+c[2])...)
}

// TestAlcanceCambiadoMismaTxPg: si el commit falla, no queda ni el cliente ni
// el alcance guardado ni el evento (estado y outbox en la misma tx).
func TestAlcanceCambiadoMismaTxPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	mon := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	desde := ultimoOutbox(t, ctx, pool)

	falla := nuevoServicioGob(&faultyBegin{real: pool, punto: fallaCommit}, pool, prov)
	err := falla.FijarClientesSujeto(ctx, gobierno.FijacionClientes{SujetoID: mon, ClientesIDs: []string{uuid.NewString()}, ActorID: admin1, ActorRol: admin})
	if err == nil {
		t.Fatal("el commit fallido no devolvió error")
	}
	assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.alcance_cliente`)
	assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.alcance_efectivo`)
	if got := eventosAlcanceDesde(t, ctx, pool, desde); len(got) != 0 {
		t.Errorf("quedó alcance.cambiado sin commit: %s", claves(got))
	}
}

// TestListarAlcancesPg cubre el snapshot (solo sujetos con clientes), la
// continuación por cursor (incluye los que quedaron vacíos) y que una lectura
// espera a la escritura de alcance en curso, así el cursor no pierde cambios.
func TestListarAlcancesPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	prov := memory.New()
	svc := nuevoServicioGob(pool, pool, prov)
	consulta := svc.(gobierno.Consulta)
	sup := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	m1 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	m2 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	c := unidadesOrdenadas(2)
	fijar := func(s gobierno.Gobierno, m string, clientes ...string) error {
		return s.FijarClientesSujeto(ctx, gobierno.FijacionClientes{SujetoID: m, ClientesIDs: clientes, ActorID: admin1, ActorRol: admin})
	}
	resumen := func(p gobierno.PaginaAlcances) string {
		out := make([]string, 0, len(p.Items))
		for _, it := range p.Items {
			if it.ClientesEfectivosIDs == nil || it.CambiadoEn.IsZero() {
				t.Errorf("item incompleto: %+v", it)
			}
			out = append(out, it.SujetoID+"="+strings.Join(it.ClientesEfectivosIDs, ","))
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	listar := func(cursor string) gobierno.PaginaAlcances {
		t.Helper()
		p, err := consulta.ListarAlcances(ctx, cursor)
		if err != nil {
			t.Fatalf("listar alcances (%q): %v", cursor, err)
		}
		if p.CursorSiguiente == nil || p.Items == nil {
			t.Fatalf("página sin cursor o con items nil: %+v", p)
		}
		return p
	}
	esperar := func(s ...string) string { sort.Strings(s); return strings.Join(s, " ") }

	if got := resumen(listar("")); got != "" {
		t.Errorf("snapshot vacío: %q", got)
	}
	if err := svc.FijarSubordinadosSujeto(ctx, gobierno.FijacionSubordinados{SujetoID: sup, SubordinadosIDs: []string{m1}, ActorID: admin1, ActorRol: admin}); err != nil {
		t.Fatalf("fijar subordinados: %v", err)
	}
	if err := fijar(svc, m1, c[0]); err != nil {
		t.Fatalf("fijar m1: %v", err)
	}
	snap := listar("")
	if got, want := resumen(snap), esperar(m1+"="+c[0], sup+"="+c[0]); got != want {
		t.Errorf("snapshot: %q; quiere %q", got, want)
	}
	if got := resumen(listar(*snap.CursorSiguiente)); got != "" {
		t.Errorf("continuación sin cambios: %q; quiere vacío", got)
	}

	// m1 se queda sin clientes y m2 gana uno: la continuación trae los dos,
	// el vacío incluido; el snapshot nuevo ya no trae a m1 ni a sup.
	if err := fijar(svc, m1); err != nil {
		t.Fatalf("vaciar m1: %v", err)
	}
	if err := fijar(svc, m2, c[1]); err != nil {
		t.Fatalf("fijar m2: %v", err)
	}
	cont := listar(*snap.CursorSiguiente)
	if got, want := resumen(cont), esperar(m1+"=", sup+"=", m2+"="+c[1]); got != want {
		t.Errorf("continuación: %q; quiere %q", got, want)
	}
	if got, want := resumen(listar("")), m2+"="+c[1]; got != want {
		t.Errorf("snapshot tras vaciar: %q; quiere %q", got, want)
	}

	if _, err := consulta.ListarAlcances(ctx, "no-es-un-cursor"); err == nil {
		t.Error("cursor inválido: sin error")
	} else if status, _ := codigoDe(err); status != 400 {
		t.Errorf("cursor inválido: %d; quiere 400", status)
	}

	// Una escritura queda sin comprometer; la lectura espera su candado y, al
	// seguir, la ve. Sin el candado, el cambio caería antes del cursor y se
	// perdería.
	pausa := beginConPausa{pool: pool, enCommit: make(chan struct{}), seguir: make(chan struct{}), once: &sync.Once{}}
	errA := make(chan error, 1)
	go func() { errA <- fijar(nuevoServicioGob(pausa, pool, prov), m2, c[0], c[1]) }()
	select {
	case <-pausa.enCommit:
	case err := <-errA:
		t.Fatalf("A terminó antes de comprometer: %v", err)
	case <-ctx.Done():
		t.Fatal("A no llegó a Commit")
	}
	type resultado struct {
		p   gobierno.PaginaAlcances
		err error
	}
	lectura := make(chan resultado, 1)
	go func() {
		p, err := consulta.ListarAlcances(ctx, *cont.CursorSiguiente)
		lectura <- resultado{p, err}
	}()
	adminConn, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()
	esperando := false
	for i := 0; i < 200 && !esperando; i++ {
		var n int
		if err := adminConn.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%LOCK TABLE identidad.alcance_efectivo IN SHARE MODE%'`).Scan(&n); err != nil {
			t.Fatalf("mirar pg_stat_activity: %v", err)
		}
		if esperando = n == 1; !esperando {
			time.Sleep(25 * time.Millisecond)
		}
	}
	close(pausa.seguir)
	if !esperando {
		t.Fatal("la lectura no esperó a la escritura en curso")
	}
	if err := <-errA; err != nil {
		t.Fatalf("A: %v", err)
	}
	r := <-lectura
	if r.err != nil {
		t.Fatalf("lectura: %v", r.err)
	}
	if got, want := resumen(r.p), m2+"="+c[0]+","+c[1]; got != want {
		t.Errorf("lectura tras la escritura en curso: %q; quiere %q", got, want)
	}
}
