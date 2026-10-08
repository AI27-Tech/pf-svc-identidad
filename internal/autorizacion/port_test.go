package autorizacion_test

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/roles"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// rolGenerado genera únicamente los ocho roles canónicos de DEC-053.
type rolGenerado autorizacion.Rol

func (rolGenerado) Generate(r *rand.Rand, _ int) reflect.Value {
	catalogo := []autorizacion.Rol{
		autorizacion.RolCliente,
		autorizacion.RolJefeOperativo,
		autorizacion.RolMonitorista,
		autorizacion.RolSupervisor,
		autorizacion.RolCoordinador,
		autorizacion.RolEjecutivo,
		autorizacion.RolReaccion,
		autorizacion.RolAdministrador,
	}
	return reflect.ValueOf(rolGenerado(catalogo[r.Intn(len(catalogo))]))
}

// Feature: e4-t2-modelo-autorizacion-claims, Property 2: Mapeo total y correcto de rol → dimensión de alcance
func TestPropertyDimensionDeRol(t *testing.T) {
	esperadas := map[autorizacion.Rol]struct {
		dimension autorizacion.DimensionAlcance
		resuelto  bool
	}{
		autorizacion.RolCliente:       {autorizacion.AlcanceTenant, true},
		autorizacion.RolJefeOperativo: {"", false},
		autorizacion.RolMonitorista:   {autorizacion.AlcanceUnidad, true},
		autorizacion.RolSupervisor:    {autorizacion.AlcanceUnidad, true},
		autorizacion.RolCoordinador:   {autorizacion.AlcanceUnidad, true},
		autorizacion.RolEjecutivo:     {autorizacion.AlcanceCliente, true},
		autorizacion.RolReaccion:      {"", false},
		autorizacion.RolAdministrador: {autorizacion.AlcanceGlobal, true},
	}

	err := quick.Check(func(generado rolGenerado) bool {
		rol := autorizacion.Rol(generado)
		esperada := esperadas[rol]
		dimension, resuelto := autorizacion.DimensionDeRol(rol)
		return dimension == esperada.dimension && resuelto == esperada.resuelto
	}, &quick.Config{MaxCount: 128, Rand: rand.New(rand.NewSource(4))})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAlcancePendienteNoResuelveAlcance verifica que el marcador no resuelve
// ninguna dimensión mientras el vínculo de DEC-045 siga pendiente.
func TestAlcancePendienteNoResuelveAlcance(t *testing.T) {
	var resolver autorizacion.AlcancePendiente

	unidades, resuelto := resolver.UnidadesDe("sujeto-monitorista")
	if len(unidades) != 0 || resuelto {
		t.Fatalf("UnidadesDe() = (%v, %t), se esperaba (slice vacío, false)", unidades, resuelto)
	}

	clientes, resuelto := resolver.ClientesDe("sujeto-ejecutivo")
	if len(clientes) != 0 || resuelto {
		t.Fatalf("ClientesDe() = (%v, %t), se esperaba (slice vacío, false)", clientes, resuelto)
	}
}

// TestCatalogoDeRoles verifica que el catálogo de contrato contiene exactamente
// los ocho roles de DEC-053, sin duplicados ni el rol descartado `auditor`.
func TestCatalogoDeRoles(t *testing.T) {
	esperado := []roles.Rol{
		autorizacion.RolCliente,
		autorizacion.RolJefeOperativo,
		autorizacion.RolMonitorista,
		autorizacion.RolSupervisor,
		autorizacion.RolCoordinador,
		autorizacion.RolEjecutivo,
		autorizacion.RolReaccion,
		autorizacion.RolAdministrador,
	}
	catalogo := append([]roles.Rol(nil), roles.Todos...)
	if !reflect.DeepEqual(catalogo, esperado) {
		t.Fatalf("catálogo = %v, se esperaba exactamente %v", catalogo, esperado)
	}

	vistos := make(map[roles.Rol]struct{}, len(catalogo))
	for _, rol := range catalogo {
		if _, existe := vistos[rol]; existe {
			t.Fatalf("rol duplicado en el catálogo: %q", rol)
		}
		vistos[rol] = struct{}{}
		if !roles.EsRol(string(rol)) {
			t.Fatalf("el rol canónico %q no es reconocido por EsRol", rol)
		}
	}
	if roles.EsRol("auditor") {
		t.Fatal("el rol auditor no debe pertenecer al catálogo")
	}
	if _, existe := vistos[roles.Rol("auditor")]; existe {
		t.Fatal("el rol auditor no debe estar en el catálogo")
	}
}
