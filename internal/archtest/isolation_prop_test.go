package archtest

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

// escenarioRegla es un caso generado para la propiedad de aislamiento: un
// paquete y un import, clasificados por su origen para poder predecir el
// resultado esperado sin reusar la implementación de violations.
type escenarioRegla struct {
	// pkgEnImpl indica si el paquete está dentro de …/internal/idp/internal.
	pkgEnImpl bool
	// pkg es la ruta de importación del paquete evaluado.
	pkg string
	// imp es el import a evaluar.
	imp string
	// impEsIdP indica si imp proviene de la Lista_De_Bibliotecas_IdP.
	impEsIdP bool
	// impEsAWS indica si imp proviene de AWS (v1 o v2).
	impEsAWS bool
}

// Generate implementa quick.Generator: arma un escenario eligiendo un paquete
// (dentro o fuera de la implementación) y un import (de la lista de IdP, de AWS
// o neutro).
func (escenarioRegla) Generate(r *rand.Rand, _ int) reflect.Value {
	e := escenarioRegla{}

	// Paquete: dentro o fuera de la zona de implementación.
	e.pkgEnImpl = r.Intn(2) == 0
	if e.pkgEnImpl {
		// Un subpaquete arbitrario bajo la zona de implementación.
		e.pkg = PrefijoDImplementacionIdP + elegir(r, []string{"", "/kc", "/kc/sub", "/otro"})
	} else {
		e.pkg = elegir(r, []string{
			"github.com/AI27-Tech/pf-svc-identidad",
			"github.com/AI27-Tech/pf-svc-identidad/cmd/api",
			"github.com/AI27-Tech/pf-svc-identidad/internal/idp",
			"github.com/AI27-Tech/pf-svc-identidad/internal/idp/keycloak",
			"github.com/AI27-Tech/pf-svc-identidad/internal/sesion",
		})
	}

	// Import: de la lista de IdP, de AWS, o neutro.
	switch r.Intn(3) {
	case 0:
		e.imp = elegir(r, ListaDeBibliotecasIdP) + elegir(r, []string{"", "/v13", "/pkg"})
		e.impEsIdP = true
	case 1:
		e.imp = elegir(r, append(append([]string{}, PrefijosAWSV1...), PrefijosAWSV2...)) +
			elegir(r, []string{"", "/service/s3", "/aws"})
		e.impEsAWS = true
	default:
		e.imp = elegir(r, []string{
			"context",
			"net/http",
			"github.com/google/uuid",
			"go.opentelemetry.io/otel/metric",
			"github.com/AI27-Tech/pf-svc-identidad/internal/idp",
		})
	}

	return reflect.ValueOf(e)
}

// elegir toma un elemento aleatorio de la lista; devuelve "" si está vacía.
func elegir(r *rand.Rand, opciones []string) string {
	if len(opciones) == 0 {
		return ""
	}
	return opciones[r.Intn(len(opciones))]
}

// TestPropiedadAislamiento verifica la regla de aislamiento contra un oráculo
// independiente: hay violación si y sólo si el import es de AWS, o es de la
// Lista_De_Bibliotecas_IdP y el paquete está fuera de la zona de implementación.
// El mensaje de violación nombra el paquete y el import.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 12: Regla de aislamiento
func TestPropiedadAislamiento(t *testing.T) {
	propiedad := func(e escenarioRegla) bool {
		viols := violations(e.pkg, []string{e.imp})
		hubo := len(viols) > 0

		// Oráculo independiente de la implementación.
		esperado := e.impEsAWS || (e.impEsIdP && !e.pkgEnImpl)

		if hubo != esperado {
			return false
		}
		// Si hubo violación, el mensaje nombra el paquete y el import.
		if hubo {
			msg := strings.Join(viols, "\n")
			if !strings.Contains(msg, e.pkg) || !strings.Contains(msg, e.imp) {
				return false
			}
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}
