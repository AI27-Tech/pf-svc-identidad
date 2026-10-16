package identidad

import (
	"context"
	"testing"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// Estas pruebas fijan la FORMA de las sub-fachadas de Consultas() contra la
// firma que declaran los consumidores, sin importar sus paquetes (identidad es
// hoja: no puede depender de bff ni de operacion). Replican aquí la firma de
// cada puerto consumidor; si una sub-fachada deja de satisfacerla, el test no
// compila. La conformación contra los puertos REALES la prueba core cuando
// cablea los adaptadores en proceso (ahí ve ambos extremos de la arista).

// puertoSesionBFF replica bff.IdentidadPort (pf-svc-bff/internal/agregacion):
// la arista bff → identidad, tipada con el contrato api-client-go/identidad.
type puertoSesionBFF interface {
	EmitirSesion(ctx context.Context, body gen.CredencialesEntrada) (*gen.SesionEmitida, error)
	RefrescarSesion(ctx context.Context, body gen.RefrescoEntrada) (*gen.SesionEmitida, error)
	RevocarSesion(ctx context.Context, id gen.Id) error
	ListarSesionesRevocadas(ctx context.Context) (*gen.PaginaRevocaciones, error)
	ObtenerSesion(ctx context.Context, id gen.Id) (*gen.EstadoSesion, error)
}

// puertoRevocacion replica el RevocacionPort que declaran, con firma idéntica,
// operacion (Revocacion), catalogos (RevocacionPort) y reportes (RevocacionPort):
// la arista restringida a identidad (DEC-051), solo dos operaciones primitivas.
type puertoRevocacion interface {
	ListarRevocadas(ctx context.Context) ([]string, error)
	ObtenerSesion(ctx context.Context, sesionID string) (revocada bool, err error)
}

// TestSesionConsultasSatisfaceElPuertoDeBFF comprueba, en tiempo de compilación,
// que la sub-fachada de sesión tiene exactamente la forma que bff espera.
func TestSesionConsultasSatisfaceElPuertoDeBFF(t *testing.T) {
	var _ puertoSesionBFF = SesionConsultas{}
}

// TestRevocacionConsultasSatisfaceElPuertoRestringido comprueba que la
// sub-fachada de revocación tiene la forma que esperan operacion/catalogos/reportes.
func TestRevocacionConsultasSatisfaceElPuertoRestringido(t *testing.T) {
	var _ puertoRevocacion = RevocacionConsultas{}
}

// TestConsultasExponeAmbasSubfachadas comprueba que Consultas() entrega las dos
// aristas y que cada una tiene su forma, partiendo de un Module (zero value del
// servicio: la forma se verifica, no la lógica, que es scaffold).
func TestConsultasExponeAmbasSubfachadas(t *testing.T) {
	var m Module
	c := m.Consultas()
	var _ puertoSesionBFF = c.Sesion()
	var _ puertoRevocacion = c.Revocacion()
}

// --- Paridad de la categoría NoEncontrado (Tarea 7.5, paridad I3) -----------

// servicioFalso implementa sesion.Servicio para probar que las fachadas
// propagan errores sin cambiarlos. Solo ObtenerSesion tiene comportamiento; el
// resto no se usa en esta prueba.
type servicioFalso struct {
	obtenerErr error
}

func (s servicioFalso) EmitirSesion(ctx context.Context, cred sesion.Credentials) (sesion.Emitida, error) {
	return sesion.Emitida{}, nil
}
func (s servicioFalso) RefrescarSesion(ctx context.Context, refreshToken string) (sesion.Emitida, error) {
	return sesion.Emitida{}, nil
}
func (s servicioFalso) RevocarSesion(ctx context.Context, sesionID string) error { return nil }
func (s servicioFalso) ObtenerSesion(ctx context.Context, sesionID string) (sesion.Estado, error) {
	return sesion.Estado{}, s.obtenerErr
}
func (s servicioFalso) ListarRevocadas(ctx context.Context, cursor string) (sesion.PaginaRevocadas, error) {
	return sesion.PaginaRevocadas{}, nil
}

// TestParidadNoEncontrado verifica que, cuando el servicio devuelve un error de
// categoría NoEncontrado (ObtenerSesion de una sesión inexistente, envuelto por
// WrapNotFound), AMBAS fachadas lo propagan con la misma categoría, sin
// aplanarlo ni perder la clasificación (paridad I3, Requisito 6.2).
func TestParidadNoEncontrado(t *testing.T) {
	ctx := context.Background()
	// El servicio envuelve ErrNoEncontrada en un PlatformError de categoría
	// NoEncontrado, igual que lo hace la ruta productiva.
	errNoEnc := sesion.WrapNotFound(sesion.ErrNoEncontrada)
	svc := servicioFalso{obtenerErr: errNoEnc}

	// Sub-fachada de bff: ObtenerSesion debe propagar el error con su categoría.
	bff := SesionConsultas{svc: svc}
	_, errBFF := bff.ObtenerSesion(ctx, gen.Id{})
	if errBFF == nil {
		t.Fatal("SesionConsultas.ObtenerSesion debería propagar el error NoEncontrado")
	}
	if cat := platformerrors.AsCategory(errBFF); cat != platformerrors.CategoryNoEncontrado {
		t.Errorf("SesionConsultas: categoría = %v; se esperaba NoEncontrado", cat)
	}

	// Sub-fachada restringida: ObtenerSesion devuelve (bool, error); el error
	// debe conservar la categoría NoEncontrado.
	rev := RevocacionConsultas{svc: svc}
	_, errRev := rev.ObtenerSesion(ctx, "33333333-3333-3333-3333-333333333333")
	if errRev == nil {
		t.Fatal("RevocacionConsultas.ObtenerSesion debería propagar el error NoEncontrado")
	}
	if cat := platformerrors.AsCategory(errRev); cat != platformerrors.CategoryNoEncontrado {
		t.Errorf("RevocacionConsultas: categoría = %v; se esperaba NoEncontrado", cat)
	}
}
