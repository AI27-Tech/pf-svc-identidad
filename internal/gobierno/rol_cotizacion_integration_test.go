package gobierno_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	idhttp "github.com/AI27-Tech/pf-svc-identidad/internal/http"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// TestRolCotizacionPg cubre el rol `cotizacion` (DEC-148 #1, #20) contra
// Postgres: se acepta al dar de alta y al cambiar de rol, no admite lista de
// clientes y un rol inventado sigue rechazándose.
func TestRolCotizacionPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())

	// (a) alta con el rol nuevo.
	cot := altaConRol(t, ctx, svc, autorizacion.RolCotizacion)
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id = $1 AND rol_codigo = 'cotizacion' AND revocado_en IS NULL`, cot)

	// (a) cambio de rol interno → cotizacion.
	m := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	if err := svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{SujetoID: m, RolNuevo: autorizacion.RolCotizacion, ActorID: admin1, ActorEsAdmin: true}); err != nil {
		t.Fatalf("cambiar a cotizacion: %v", err)
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.asignacion_rol WHERE sujeto_id = $1 AND rol_codigo = 'cotizacion' AND revocado_en IS NULL`, m)

	// (c) sin lista de clientes: fijarla es rol_sin_alcance.
	err := svc.FijarClientesSujeto(ctx, gobierno.FijacionClientes{SujetoID: cot, ClientesIDs: []string{"cccccccc-cccc-4ccc-8ccc-cccccccccccc"}, ActorID: admin1, ActorRol: autorizacion.RolAdministrador})
	if !errors.Is(err, gobierno.ErrRolSinAlcance) {
		t.Errorf("fijar clientes a cotizacion: esperaba ErrRolSinAlcance, obtuve %v", err)
	}

	// (d) un rol inventado sigue rechazándose, al alta y al cambio.
	_, err = svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "x", IdentificadorUsuario: "inventado@ai27.mx", Rol: "inventado", TenantID: tenantAI,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if !errors.Is(err, autorizacion.ErrRolFueraDeCatalogo) {
		t.Errorf("alta con rol inventado: esperaba ErrRolFueraDeCatalogo, obtuve %v", err)
	}
	err = svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{SujetoID: cot, RolNuevo: "inventado", ActorID: admin1, ActorEsAdmin: true})
	if !errors.Is(err, autorizacion.ErrRolFueraDeCatalogo) {
		t.Errorf("cambio a rol inventado: esperaba ErrRolFueraDeCatalogo, obtuve %v", err)
	}
}

// actorFijo resuelve cualquier `sub` al mismo sujeto, como lo haría el
// resolvedor real para un token válido.
type actorFijo string

func (a actorFijo) PorIdpSubject(_ context.Context, sub string) (autorizacion.Sujeto, bool, error) {
	return autorizacion.Sujeto{ID: string(a), IdpSubject: sub}, true, nil
}

// TestRolCotizacionTrazaOutboxAlcanceYHTTPPg completa la cobertura de
// `cotizacion` contra Postgres: traza y outbox del alta, retiro del alcance al
// pasar de monitorista, cruce de audiencia, lectura del alcance y los códigos
// HTTP de los handlers con el servicio real.
func TestRolCotizacionTrazaOutboxAlcanceYHTTPPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	const tenantCliente = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := pool.Exec(ctx, `INSERT INTO identidad.tenant (id, nombre, tipo) VALUES ($1,'Transportes Demo','cliente')`, tenantCliente); err != nil {
		t.Fatalf("sembrar tenant cliente: %v", err)
	}
	svc := nuevoServicioGob(pool, pool, memory.New())
	consulta := svc.(gobierno.Consulta)

	// (a) el alta deja traza y evento `rol.asignado` con rol=cotizacion.
	cot := altaConRol(t, ctx, svc, autorizacion.RolCotizacion)
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.cambio_identidad WHERE sujeto_id = $1 AND tipo = 'rol.asignado'`, cot)
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.outbox WHERE tipo = 'rol.asignado' AND agregado_id = $1 AND payload->>'rol' = 'cotizacion' AND payload->>'sujeto_id' = $1`, cot)

	// (b) monitorista con clientes → cotizacion: pierde el alcance y se publica
	// `alcance.cambiado` con la lista vacía.
	mon := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	c := unidadesOrdenadas(2) // solo importa que sean UUID distintos
	if err := svc.FijarClientesSujeto(ctx, gobierno.FijacionClientes{SujetoID: mon, ClientesIDs: c, ActorID: admin1, ActorRol: admin}); err != nil {
		t.Fatalf("fijar clientes: %v", err)
	}
	desde := ultimoOutbox(t, ctx, pool)
	if err := svc.FijarRol(ctx, gobierno.ReemplazoRol{SujetoID: mon, RolNuevo: autorizacion.RolCotizacion, ActorID: admin1, ActorEsAdmin: true}); err != nil {
		t.Fatalf("monitorista → cotizacion: %v", err)
	}
	assertConteo(t, ctx, pool, 0, `SELECT count(*) FROM identidad.alcance_cliente WHERE monitorista_id = $1`, mon)
	if got := claves(eventosAlcanceDesde(t, ctx, pool, desde)); got != mon+"=" {
		t.Errorf("alcance.cambiado = %q; quiere %q (lista vacía)", got, mon+"=")
	}
	assertConteo(t, ctx, pool, 1, `SELECT count(*) FROM identidad.outbox WHERE id > $1 AND tipo = 'rol.asignado' AND payload->>'rol' = 'cotizacion'`, desde)

	// (c) cotizacion no cruza entre audiencias, ni al alta ni al cambio.
	_, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "cot-en-cliente", IdentificadorUsuario: "cot-en-cliente@demo.mx", Rol: autorizacion.RolCotizacion, TenantID: tenantCliente,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if !errors.Is(err, gobierno.ErrCruceAudiencia) {
		t.Errorf("alta de cotizacion en un tenant cliente: err = %v, quiero ErrCruceAudiencia", err)
	}
	cli, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "cli", IdentificadorUsuario: "cli@demo.mx", Rol: autorizacion.RolCliente, TenantID: tenantCliente,
		ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta de cliente: %v", err)
	}
	if err := svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{SujetoID: cli.ID, RolNuevo: autorizacion.RolCotizacion, ActorID: admin1, ActorEsAdmin: true}); !errors.Is(err, gobierno.ErrCruceAudiencia) {
		t.Errorf("cliente → cotizacion: err = %v, quiero ErrCruceAudiencia", err)
	}

	// (d) obtenerAlcanceSujeto: el administrador lee un alcance vacío; el jefe
	// operativo solo lee Monitoristas, así que recibe 403.
	a, err := consulta.ObtenerAlcanceSujeto(ctx, gobierno.LecturaAlcance{SujetoID: cot, ActorRol: admin})
	if err != nil {
		t.Fatalf("admin lee a cotizacion: %v", err)
	}
	if a.ClientesIDs == nil || len(a.ClientesIDs) != 0 || len(a.ClientesEfectivosIDs) != 0 || len(a.UnidadesIDs) != 0 || len(a.SubordinadosIDs) != 0 {
		t.Errorf("alcance de cotizacion no vacío: %+v", a)
	}
	_, err = consulta.ObtenerAlcanceSujeto(ctx, gobierno.LecturaAlcance{SujetoID: cot, ActorRol: autorizacion.RolJefeOperativo})
	if status, _ := codigoDe(err); status != http.StatusForbidden {
		t.Errorf("jefe_operativo lee a cotizacion: status %d (err %v); quiere 403", status, err)
	}

	// (e) HTTP con el servicio real.
	// El administrador fija; el propio cotizacion lee (si fuera el mismo actor,
	// la guarda de autoescalamiento respondería 403 antes que rol_sin_alcance).
	hAdmin := idhttp.NewGobiernoHandlers(svc, consulta, nil, actorFijo(admin1))
	hCot := idhttp.NewGobiernoHandlers(svc, consulta, nil, actorFijo(cot))
	peticion := func(rol, cuerpo string, llama http.HandlerFunc) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/x", strings.NewReader(cuerpo))
		req.SetPathValue("id", cot)
		req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{Subject: "sub-actor", Rol: rol}))
		rec := httptest.NewRecorder()
		llama(rec, req)
		return rec
	}
	rec := peticion("administrador", `{"clientes_ids":["`+c[0]+`"]}`, hAdmin.FijarClientesSujeto)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "rol_sin_alcance") {
		t.Errorf("fijarClientesSujeto a cotizacion: %d %s; quiere 409 rol_sin_alcance", rec.Code, rec.Body)
	}
	if rec := peticion("cotizacion", "", hCot.ObtenerMiAlcance); rec.Code != http.StatusForbidden {
		t.Errorf("obtenerMiAlcance de cotizacion: %d %s; quiere 403", rec.Code, rec.Body)
	}
}
