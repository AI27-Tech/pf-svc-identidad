package identidad

import (
	"context"
	"testing"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
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
