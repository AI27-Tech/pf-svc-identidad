package gobierno_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
	det, err := consultaGob(pool).SujetoPorID(ctx, creado.ID)
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
	det, err = consultaGob(pool).SujetoPorID(ctx, previo)
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
