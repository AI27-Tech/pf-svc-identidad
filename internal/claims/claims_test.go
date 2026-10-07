package claims_test

import (
	"testing"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/claims"
)

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
