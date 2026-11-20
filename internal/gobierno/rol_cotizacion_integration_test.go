package gobierno_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
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
