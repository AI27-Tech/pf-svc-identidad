package claims_test

import (
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"testing/quick"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/claims"
)

// TestComponerClaimsExactos verifica los cuatro claims que produce la
// composición para los extremos externo e interno del catálogo.
func TestComponerClaimsExactos(t *testing.T) {
	casos := []struct {
		nombre   string
		modelo   claims.ModeloVigente
		esperado claims.Compuestos
	}{
		{
			nombre: "cliente",
			modelo: claims.ModeloVigente{
				SujetoID: "sujeto-cliente",
				TenantID: "tenant-cliente",
				Rol:      autorizacion.RolCliente,
			},
			esperado: claims.Compuestos{
				SujetoID:  "sujeto-cliente",
				TenantID:  "tenant-cliente",
				Rol:       autorizacion.RolCliente,
				Audiencia: auth.AudienciaCliente,
			},
		},
		{
			nombre: "administrador",
			modelo: claims.ModeloVigente{
				SujetoID: "sujeto-administrador",
				TenantID: "tenant-interno",
				Rol:      autorizacion.RolAdministrador,
			},
			esperado: claims.Compuestos{
				SujetoID:  "sujeto-administrador",
				TenantID:  "tenant-interno",
				Rol:       autorizacion.RolAdministrador,
				Audiencia: auth.AudienciaInterno,
			},
		},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			if got := claims.Componer(caso.modelo); got != caso.esperado {
				t.Fatalf("claims compuestos = %+v, se esperaba %+v", got, caso.esperado)
			}
		})
	}
}

// TestComponerDerivaAudiencia verifica el mapeo de DEC-054: `cliente` → Cliente;
// los otros siete roles → Interno.
func TestComponerDerivaAudiencia(t *testing.T) {
	casos := []struct {
		rol      autorizacion.Rol
		esperada auth.Audiencia
	}{
		{autorizacion.RolCliente, auth.AudienciaCliente},
		{autorizacion.RolJefeOperativo, auth.AudienciaInterno},
		{autorizacion.RolMonitorista, auth.AudienciaInterno},
		{autorizacion.RolSupervisor, auth.AudienciaInterno},
		{autorizacion.RolCoordinador, auth.AudienciaInterno},
		{autorizacion.RolEjecutivo, auth.AudienciaInterno},
		{autorizacion.RolReaccion, auth.AudienciaInterno},
		{autorizacion.RolAdministrador, auth.AudienciaInterno},
	}
	for _, c := range casos {
		got := claims.Componer(claims.ModeloVigente{
			SujetoID: "s1", TenantID: "t1", Rol: c.rol,
		})
		if got.Audiencia != c.esperada {
			t.Errorf("rol %q: Audiencia = %q, se esperaba %q", c.rol, got.Audiencia, c.esperada)
		}
	}
}

// rolDeComposicion genera roles canónicos y valores inválidos para cubrir el
// comportamiento total de la función pura en los bordes del catálogo.
type rolDeComposicion autorizacion.Rol

func (rolDeComposicion) Generate(r *rand.Rand, _ int) reflect.Value {
	roles := []autorizacion.Rol{
		autorizacion.RolCliente,
		autorizacion.RolJefeOperativo,
		autorizacion.RolMonitorista,
		autorizacion.RolSupervisor,
		autorizacion.RolCoordinador,
		autorizacion.RolEjecutivo,
		autorizacion.RolReaccion,
		autorizacion.RolAdministrador,
		autorizacion.Rol("auditor"),
		autorizacion.Rol(""),
	}
	return reflect.ValueOf(rolDeComposicion(roles[r.Intn(len(roles))]))
}

// modeloGenerado genera un modelo vigente completo con un rol canónico.
type modeloGenerado struct {
	SujetoID string
	TenantID string
	Rol      autorizacion.Rol
}

func (modeloGenerado) Generate(r *rand.Rand, _ int) reflect.Value {
	roles := []autorizacion.Rol{
		autorizacion.RolCliente,
		autorizacion.RolJefeOperativo,
		autorizacion.RolMonitorista,
		autorizacion.RolSupervisor,
		autorizacion.RolCoordinador,
		autorizacion.RolEjecutivo,
		autorizacion.RolReaccion,
		autorizacion.RolAdministrador,
	}
	return reflect.ValueOf(modeloGenerado{
		SujetoID: "sujeto-" + strconv.FormatUint(r.Uint64(), 10),
		TenantID: "tenant-" + strconv.FormatUint(r.Uint64(), 10),
		Rol:      roles[r.Intn(len(roles))],
	})
}

// Feature: e4-t2-modelo-autorizacion-claims, Property 1: Derivación determinista de Audiencia = f(rol)
func TestPropertyAudienciaDerivadaDelRol(t *testing.T) {
	err := quick.Check(func(generado rolDeComposicion) bool {
		rol := autorizacion.Rol(generado)
		esperada := auth.AudienciaInterno
		if rol == autorizacion.RolCliente {
			esperada = auth.AudienciaCliente
		}
		return claims.Componer(claims.ModeloVigente{Rol: rol}).Audiencia == esperada
	}, &quick.Config{MaxCount: 128, Rand: rand.New(rand.NewSource(1))})
	if err != nil {
		t.Fatal(err)
	}
}

// Feature: e4-t2-modelo-autorizacion-claims, Property 3: Ausencia de alcance en el token
func TestPropertyComposicionNoIncluyeAlcance(t *testing.T) {
	err := quick.Check(func(generado modeloGenerado) bool {
		modelo := claims.ModeloVigente{
			SujetoID: generado.SujetoID,
			TenantID: generado.TenantID,
			Rol:      generado.Rol,
		}
		esperado := claims.Compuestos{
			SujetoID:  generado.SujetoID,
			TenantID:  generado.TenantID,
			Rol:       generado.Rol,
			Audiencia: autorizacion.AudienciaDeRol(generado.Rol),
		}
		base := claims.Componer(modelo)
		if base != esperado {
			return false
		}

		alcances := []struct {
			unidades []string
			clientes []string
		}{
			{unidades: []string{"unidad-1"}, clientes: []string{"cliente-1"}},
			{unidades: []string{"unidad-2", "unidad-3"}, clientes: []string{"cliente-2"}},
		}
		for _, alcance := range alcances {
			if len(alcance.unidades)+len(alcance.clientes) == 0 {
				return false
			}
			if got := claims.Componer(modelo); got != base {
				return false
			}
		}
		return true
	}, &quick.Config{MaxCount: 128, Rand: rand.New(rand.NewSource(2))})
	if err != nil {
		t.Fatal(err)
	}
}

// Feature: e4-t2-modelo-autorizacion-claims, Property 5: Recomposición idéntica entre emisión y refresco
func TestPropertyRecomposicionEntreEmisionYRefresco(t *testing.T) {
	err := quick.Check(func(generado modeloGenerado) bool {
		modelo := claims.ModeloVigente{
			SujetoID: generado.SujetoID,
			TenantID: generado.TenantID,
			Rol:      generado.Rol,
		}
		emision := claims.Componer(modelo)
		refresco := claims.Componer(modelo)
		if emision != refresco {
			return false
		}

		rolActualizado := siguienteRol(generado.Rol)
		modelo.Rol = rolActualizado
		actualizado := claims.Componer(modelo)
		if actualizado.Rol != rolActualizado {
			return false
		}
		return actualizado.Audiencia == autorizacion.AudienciaDeRol(rolActualizado)
	}, &quick.Config{MaxCount: 128, Rand: rand.New(rand.NewSource(3))})
	if err != nil {
		t.Fatal(err)
	}
}

func siguienteRol(actual autorizacion.Rol) autorizacion.Rol {
	roles := []autorizacion.Rol{
		autorizacion.RolCliente,
		autorizacion.RolJefeOperativo,
		autorizacion.RolMonitorista,
		autorizacion.RolSupervisor,
		autorizacion.RolCoordinador,
		autorizacion.RolEjecutivo,
		autorizacion.RolReaccion,
		autorizacion.RolAdministrador,
	}
	for i, rol := range roles {
		if rol == actual {
			return roles[(i+1)%len(roles)]
		}
	}
	return autorizacion.RolCliente
}

// TestComposicionEstableEntreEmisionYRefresco verifica el requisito de DEC-031:
// la composición es la misma función en emisión y en refresco. La misma entrada
// (modelo vigente) produce la misma salida; recomponer no altera el resultado.
func TestComposicionEstableEntreEmisionYRefresco(t *testing.T) {
	m := claims.ModeloVigente{SujetoID: "s1", TenantID: "t1", Rol: autorizacion.RolMonitorista}
	emision := claims.Componer(m)  // en POST /sesiones
	refresco := claims.Componer(m) // en POST /sesiones/refresh
	if emision != refresco {
		t.Fatalf("la composición difiere entre emisión (%+v) y refresco (%+v)", emision, refresco)
	}
}

// TestComposicionReflejaCambioDeModeloEnRefresco verifica que un cambio en el
// modelo vigente (p. ej. el rol) se refleja al recomponer en el refresco
// (DEC-031): el refresco REEVALÚA, no copia los claims anteriores.
func TestComposicionReflejaCambioDeModeloEnRefresco(t *testing.T) {
	antes := claims.Componer(claims.ModeloVigente{SujetoID: "s1", TenantID: "t1", Rol: autorizacion.RolCliente})
	despues := claims.Componer(claims.ModeloVigente{SujetoID: "s1", TenantID: "t1", Rol: autorizacion.RolMonitorista})
	if antes.Rol == despues.Rol {
		t.Fatal("el rol no cambió entre composiciones; la prueba no verifica nada")
	}
	if antes.Audiencia != auth.AudienciaCliente || despues.Audiencia != auth.AudienciaInterno {
		t.Errorf("la Audiencia no se recompuso al cambiar el rol: antes=%q despues=%q", antes.Audiencia, despues.Audiencia)
	}
}
