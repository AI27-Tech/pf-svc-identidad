package gobierno_test

import (
	"context"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// TestEventosLlevanIdpSubjectPg verifica que rol.asignado, rol.revocado,
// sujeto.desactivado y sujeto.reactivado salen del outbox con el idp_subject
// del sujeto (tracer.46, DEC-148 #59), el mismo `sub` que guarda la tabla.
func TestEventosLlevanIdpSubjectPg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)

	svc := nuevoServicioGob(pool, pool, memory.New())
	// Un segundo administrador evita la guarda del último administrador.
	if _, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Admin", IdentificadorUsuario: "admin-sub@ai27.mx",
		Rol: autorizacion.RolAdministrador, TenantID: tenantAI, ActorID: admin1, ActorEsAdmin: true,
	}); err != nil {
		t.Fatalf("alta admin: %v", err)
	}
	suj, err := svc.AltaSujeto(ctx, gobierno.AltaSujeto{
		Nombre: "Sub", IdentificadorUsuario: "sub@ai27.mx",
		Rol: autorizacion.RolMonitorista, TenantID: tenantAI, ActorID: admin1, ActorEsAdmin: true,
	})
	if err != nil {
		t.Fatalf("alta: %v", err)
	}
	if suj.IdpSubject == "" {
		t.Fatal("el sujeto creado no trae idp_subject")
	}
	if err := svc.ReemplazarRol(ctx, gobierno.ReemplazoRol{
		SujetoID: suj.ID, RolNuevo: autorizacion.RolSupervisor, ActorID: admin1, ActorEsAdmin: true,
	}); err != nil {
		t.Fatalf("reemplazar: %v", err)
	}
	cambio := gobierno.CambioEstado{SujetoID: suj.ID, ActorID: admin1, ActorEsAdmin: true}
	if err := svc.DesactivarSujeto(ctx, cambio); err != nil {
		t.Fatalf("desactivar: %v", err)
	}
	if err := svc.ReactivarSujeto(ctx, cambio); err != nil {
		t.Fatalf("reactivar: %v", err)
	}

	// rol.asignado: alta + reemplazo; rol.revocado: reemplazo.
	for tipo, n := range map[string]int{"rol.asignado": 2, "rol.revocado": 1, "sujeto.desactivado": 1, "sujeto.reactivado": 1} {
		assertConteo(t, ctx, pool, n, `SELECT count(*) FROM identidad.outbox WHERE agregado_id=$1 AND tipo=$2 AND payload->>'idp_subject' = $3`, suj.ID, tipo, suj.IdpSubject)
		assertConteo(t, ctx, pool, n, `SELECT count(*) FROM identidad.outbox WHERE agregado_id=$1 AND tipo=$2`, suj.ID, tipo)
	}
}
