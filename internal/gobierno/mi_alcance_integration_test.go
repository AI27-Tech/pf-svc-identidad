package gobierno_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// TestObtenerMiAlcancePg cubre obtenerMiAlcance (DEC-149 #24) contra Postgres
// real: el Monitorista lee sus clientes, Supervisor y Coordinador la unión de
// sus subordinados, un sujeto sin clientes recibe [] (no nil) y cualquier otro
// rol recibe 403 sin leer nada.
func TestObtenerMiAlcancePg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := prepararPoolGob(t, ctx)
	sembrarTenantGob(t, ctx, pool)
	svc := nuevoServicioGob(pool, pool, memory.New())
	consulta := svc.(gobierno.Consulta)
	coord := altaConRol(t, ctx, svc, autorizacion.RolCoordinador)
	sup := altaConRol(t, ctx, svc, autorizacion.RolSupervisor)
	m1 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	m2 := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	sinClientes := altaConRol(t, ctx, svc, autorizacion.RolMonitorista)
	const admin = autorizacion.RolAdministrador
	c := unidadesOrdenadas(3) // ids de cliente: solo importa que sean UUID distintos

	for _, f := range []gobierno.FijacionClientes{
		{SujetoID: m1, ClientesIDs: []string{c[0], c[1]}},
		{SujetoID: m2, ClientesIDs: []string{c[1], c[2]}},
	} {
		f.ActorID, f.ActorRol = admin1, admin
		if err := svc.FijarClientesSujeto(ctx, f); err != nil {
			t.Fatalf("fijar clientes: %v", err)
		}
	}
	for _, f := range []gobierno.FijacionSubordinados{
		{SujetoID: sup, SubordinadosIDs: []string{m1, m2}},
		{SujetoID: coord, SubordinadosIDs: []string{sup}},
	} {
		f.ActorID, f.ActorRol = admin1, admin
		if err := svc.FijarSubordinadosSujeto(ctx, f); err != nil {
			t.Fatalf("fijar subordinados: %v", err)
		}
	}

	lee := []struct {
		nombre string
		sujeto string
		rol    autorizacion.Rol
		quiere string
	}{
		{"monitorista", m1, autorizacion.RolMonitorista, strings.Join([]string{c[0], c[1]}, ",")},
		{"supervisor: unión sin repetidos", sup, autorizacion.RolSupervisor, strings.Join(c, ",")},
		{"coordinador: transitiva", coord, autorizacion.RolCoordinador, strings.Join(c, ",")},
		{"monitorista sin clientes", sinClientes, autorizacion.RolMonitorista, ""},
	}
	for _, k := range lee {
		got, err := consulta.ObtenerMiAlcance(ctx, gobierno.LecturaMiAlcance{SujetoID: k.sujeto, ActorRol: k.rol})
		if err != nil {
			t.Fatalf("%s: %v", k.nombre, err)
		}
		if got == nil {
			t.Errorf("%s: la lista es nil; el contrato exige []", k.nombre)
		}
		if s := strings.Join(got, ","); s != k.quiere {
			t.Errorf("%s: %q; quiere %q", k.nombre, s, k.quiere)
		}
	}

	for _, rol := range []autorizacion.Rol{
		admin, autorizacion.RolJefeOperativo, autorizacion.RolEjecutivo, autorizacion.RolReaccion,
	} {
		_, err := consulta.ObtenerMiAlcance(ctx, gobierno.LecturaMiAlcance{SujetoID: m1, ActorRol: rol})
		if status, _ := codigoDe(err); status != 403 {
			t.Errorf("rol %s: %d (err %v); quiere 403", rol, status, err)
		}
	}
}
