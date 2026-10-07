package identidad

import (
	"context"

	gen "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/identidad"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
	"github.com/google/uuid"
)

// ConsultasAPI es la fachada EN PROCESO de identidad para los módulos co-ubicados
// en `core`. identidad es destino de dos aristas síncronas DISTINTAS, con
// contratos distintos (regla 2 de 00: la interfaz la declara cada consumidor):
//
//   - bff → identidad: superficie de sesión completa, tipada con el contrato
//     (api-client-go/identidad). La expone Sesion().
//   - operacion/catalogos/reportes → identidad: arista RESTRINGIDA a revocación
//     (DEC-051/058), solo dos operaciones con tipos primitivos. La expone
//     Revocacion().
//
// Se separan en dos sub-fachadas en vez de aplanar los siete métodos en una, por
// dos razones: (1) `ObtenerSesion` tiene firma distinta en cada arista (el
// contrato de bff devuelve *EstadoSesion; la revocación devuelve un bool), así
// que no pueden coexistir con el mismo nombre; (2) los consumidores de revocación
// deben ver SOLO sus dos operaciones (DEC-051), no la superficie de sesión.
//
// Ambas sub-fachadas delegan en el mismo sesion.Servicio y traducen a/desde los
// tipos del contrato o primitivos; nunca filtran tipos de internal/sesion.
//
// SCAFFOLD: el servicio de sesión todavía devuelve "no implementado" en varias
// operaciones (internal/sesion/servicio.go). La fachada compila y delega con la
// forma correcta; en runtime propaga ese "no implementado" hasta que el servicio
// se rellene. El objetivo de esta superficie es cerrar la costura de composición
// (que core pueda cablear los adaptadores en proceso con paridad I3), no entregar
// la lógica de sesión.
type ConsultasAPI struct {
	svc sesion.Servicio
}

// Sesion devuelve la sub-fachada de la arista bff → identidad, tipada con el
// contrato. Su forma es exactamente el puerto que bff declara (bff.IdentidadPort).
func (c ConsultasAPI) Sesion() SesionConsultas {
	return SesionConsultas{svc: c.svc}
}

// Revocacion devuelve la sub-fachada de la arista restringida a identidad
// (DEC-051): solo listar revocadas y consultar una sesión puntual. Su forma es
// exactamente el puerto RevocacionPort que declaran operacion, catalogos y
// reportes (misma firma primitiva en los tres).
func (c ConsultasAPI) Revocacion() RevocacionConsultas {
	return RevocacionConsultas{svc: c.svc}
}

// --- Sub-fachada de sesión (arista bff → identidad) -------------------------

// SesionConsultas expone la superficie de sesión tipada con el contrato. Cada
// método corresponde 1:1 con un método de bff.IdentidadPort; bff la consume en
// proceso a través del adaptador de core.
type SesionConsultas struct {
	svc sesion.Servicio
}

// EmitirSesion reenvía credenciales al servicio de sesión (login). bff no valida
// credenciales (ruta pública).
func (s SesionConsultas) EmitirSesion(ctx context.Context, body gen.CredencialesEntrada) (*gen.SesionEmitida, error) {
	emitida, err := s.svc.EmitirSesion(ctx, body.Usuario, body.Contrasena)
	if err != nil {
		return nil, err
	}
	out := aSesionEmitida(emitida)
	return &out, nil
}

// RefrescarSesion renueva la sesión conservando su sesion_id (ADR 26 §1).
func (s SesionConsultas) RefrescarSesion(ctx context.Context, body gen.RefrescoEntrada) (*gen.SesionEmitida, error) {
	emitida, err := s.svc.RefrescarSesion(ctx, body.RefreshToken)
	if err != nil {
		return nil, err
	}
	out := aSesionEmitida(emitida)
	return &out, nil
}

// RevocarSesion revoca la sesión dada (dos pasos, identidad.md (h)).
func (s SesionConsultas) RevocarSesion(ctx context.Context, id gen.Id) error {
	return s.svc.RevocarSesion(ctx, id.String())
}

// ListarSesionesRevocadas devuelve el snapshot de tombstones para la
// reconstrucción de la proyección del consumidor (ADR 26 §4).
func (s SesionConsultas) ListarSesionesRevocadas(ctx context.Context) (*gen.PaginaRevocaciones, error) {
	// La fachada en proceso de bff pide el snapshot inicial (cursor vacío); la
	// paginación por cursor es del adaptador HTTP, no de esta arista.
	pagina, err := s.svc.ListarRevocadas(ctx, "")
	if err != nil {
		return nil, err
	}
	out := aPaginaRevocaciones(pagina)
	return &out, nil
}

// ObtenerSesion devuelve el estado durable de una sesión (fallback ADR 26 §5).
func (s SesionConsultas) ObtenerSesion(ctx context.Context, id gen.Id) (*gen.EstadoSesion, error) {
	estado, err := s.svc.ObtenerSesion(ctx, id.String())
	if err != nil {
		return nil, err
	}
	out := aEstadoSesion(estado)
	return &out, nil
}

// --- Sub-fachada de revocación (arista restringida, DEC-051) ----------------

// RevocacionConsultas expone SOLO las dos operaciones de la arista restringida
// a identidad (DEC-051/058): el snapshot al arrancar y el fallback puntual. Su
// forma coincide con el puerto RevocacionPort de operacion, catalogos y reportes
// (ListarRevocadas []string + ObtenerSesion bool). No expone emisión, refresco
// ni revocación: esos consumidores no deben ver la superficie de sesión.
type RevocacionConsultas struct {
	svc sesion.Servicio
}

// ListarRevocadas devuelve los sesion_id revocados para el snapshot inicial de
// la proyección de tombstones del consumidor (ADR 26 §4).
func (r RevocacionConsultas) ListarRevocadas(ctx context.Context) ([]string, error) {
	pagina, err := r.svc.ListarRevocadas(ctx, "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(pagina.Items))
	for _, t := range pagina.Items {
		ids = append(ids, t.SesionID)
	}
	return ids, nil
}

// ObtenerSesion consulta si una sesión puntual está revocada, en modo degradado
// (ADR 26 §5). Devuelve true si la sesión consta como revocada de forma durable.
func (r RevocacionConsultas) ObtenerSesion(ctx context.Context, sesionID string) (revocada bool, err error) {
	estado, err := r.svc.ObtenerSesion(ctx, sesionID)
	if err != nil {
		return false, err
	}
	return estado.Revocada, nil
}

// --- Mapeo dominio → contrato ----------------------------------------------
//
// Estas funciones traducen los tipos de internal/sesion a los del contrato
// generado. Son análogas a las de internal/http (que sirven al transporte HTTP),
// pero viven aquí porque sirven a la fachada EN PROCESO: cada frontera traduce lo
// suyo, sin acoplar la fachada al paquete de transporte. Son triviales y de un
// solo sentido (dominio → contrato); la forma de los tipos de sesion coincide a
// propósito con el contrato (ver internal/sesion/port.go).

func aSesionEmitida(e sesion.Emitida) gen.SesionEmitida {
	out := gen.SesionEmitida{
		AccessToken: e.AccessToken,
		ExpiraEn:    e.ExpiraEn,
		SesionId:    aUUID(e.SesionID),
	}
	if e.RefreshToken != "" {
		rt := e.RefreshToken
		out.RefreshToken = &rt
	}
	return out
}

func aEstadoSesion(e sesion.Estado) gen.EstadoSesion {
	return gen.EstadoSesion{
		SesionId:   aUUID(e.SesionID),
		Revocada:   e.Revocada,
		RevocadaEn: e.RevocadaEn,
		ExpiraEn:   e.ExpiraEn,
	}
}

func aPaginaRevocaciones(p sesion.PaginaRevocadas) gen.PaginaRevocaciones {
	items := make([]gen.Revocacion, 0, len(p.Items))
	for _, t := range p.Items {
		items = append(items, gen.Revocacion{
			SesionId:   aUUID(t.SesionID),
			RevocadaEn: t.RevocadaEn,
			ExpiraEn:   t.ExpiraEn,
		})
	}
	return gen.PaginaRevocaciones{Items: items, CursorSiguiente: p.CursorSiguiente}
}

// aUUID convierte un sesion_id de dominio al tipo UUID del contrato. En el
// scaffold el id puede venir vacío; Parse devuelve el cero y la lógica real aún
// no entra (el servicio devuelve "no implementado").
func aUUID(s string) gen.Id {
	u, _ := uuid.Parse(s)
	return u
}
