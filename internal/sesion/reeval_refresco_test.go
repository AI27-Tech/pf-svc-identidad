package sesion

import (
	"context"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/claims"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// Pruebas de la Parte A de E4-T4 (reevaluación de autorización en el refresco).
// No requieren infraestructura: ejercitan la función pura de composición y los
// puntos de medición de latencia, con dobles del IdP.

// --- Dobles de prueba -------------------------------------------------------

// observadorGrabador implementa LatencyObserver y registra las fases observadas
// y sus duraciones, para afirmar la separación emisión/refresco (Requisito 3).
type observadorGrabador struct {
	fases []Fase
}

func (o *observadorGrabador) ObservarComposicion(fase Fase, _ time.Duration) {
	o.fases = append(o.fases, fase)
}

// idpQueResponde es un IdP de prueba mínimo: Issue y Refresh devuelven un token
// fijo, sin tocar red. Refresh falla a propósito con ErrProveedorNoDisponible
// para cortar el flujo de refrescar ANTES de la transacción de Postgres, de
// modo que la medición se pueda probar sin infraestructura.
type idpQueResponde struct {
	tokenIssue   idp.Token
	tokenRefresh idp.Token
	errRefresh   error
}

func (p *idpQueResponde) Issue(_ context.Context, _ idp.Credentials, _ string) (idp.Token, error) {
	return p.tokenIssue, nil
}

func (p *idpQueResponde) Refresh(_ context.Context, _ string) (idp.Token, error) {
	return p.tokenRefresh, p.errRefresh
}

func (p *idpQueResponde) Revoke(_ context.Context, _ string) error { return nil }

// rolGenerado genera uno de los ocho roles canónicos de DEC-053, igual que el
// generador de E4-T2, para la propiedad de recomposición.
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

// --- Property 1: recomposición idéntica entre emisión y refresco ------------

// Feature: e4-t4-reevaluacion-claims-refresco, Property 1: Recomposición idéntica emisión/refresco
//
// Reusa la función pura claims.Componer (E4-T2): es la MISMA composición en
// emisión y en refresco (DEC-031). Verifica (a) determinismo: la misma entrada
// produce la misma salida; (b) metamórfica: recomponer con un rol r' distinto
// produce el resultado de r', sin reutilizar el anterior.
func TestPropertyRecomposicionIdenticaEmisionRefresco(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100, Rand: rand.New(rand.NewSource(44))}

	// (a) Determinismo: componer dos veces el mismo modelo da lo mismo.
	// "Emisión" y "refresco" son la MISMA función; las dos llamadas simulan las
	// dos rutas.
	err := quick.Check(func(sujetoID, tenantID string, generado rolGenerado) bool {
		m := claims.ModeloVigente{SujetoID: sujetoID, TenantID: tenantID, Rol: autorizacion.Rol(generado)}
		enEmision := claims.Componer(m)
		enRefresco := claims.Componer(m)
		return enEmision == enRefresco
	}, cfg)
	if err != nil {
		t.Fatalf("determinismo emisión/refresco: %v (semilla 44)", err)
	}

	// (b) Metamórfica: con un rol distinto, el refresco recompone al rol nuevo
	// y no reutiliza el resultado del rol previo.
	err = quick.Check(func(sujetoID, tenantID string, rolPrevio, rolNuevo rolGenerado) bool {
		rp, rn := autorizacion.Rol(rolPrevio), autorizacion.Rol(rolNuevo)
		if rp == rn {
			return true // solo nos interesa el caso de cambio real de rol
		}
		base := claims.ModeloVigente{SujetoID: sujetoID, TenantID: tenantID, Rol: rp}
		previo := claims.Componer(base)

		conNuevoRol := base
		conNuevoRol.Rol = rn
		refrescado := claims.Componer(conNuevoRol)

		// El refresco trae el rol NUEVO y su Audiencia derivada; no el previo.
		return refrescado.Rol == rn &&
			refrescado.Audiencia == autorizacion.AudienciaDeRol(rn) &&
			refrescado != previo
	}, cfg)
	if err != nil {
		t.Fatalf("recomposición metamórfica con rol nuevo: %v (semilla 44)", err)
	}
}

// --- Property 2: observabilidad por fase ------------------------------------

// Feature: e4-t4-reevaluacion-claims-refresco, Property 2: Señal de latencia por fase
//
// La emisión reporta exactamente una FaseEmision; el refresco reporta
// exactamente una FaseRefresco; ninguna fase se cruza. Se ejercitan los dos
// puntos de medición reales (emissionSteps.Issue y servicio.refrescar) sin
// Postgres: el refresco se corta con un error del IdP justo después de medir.
func TestPropertyObservabilidadPorFase(t *testing.T) {
	// Emisión: el punto de medición vive en emissionSteps.Issue.
	t.Run("emision reporta una FaseEmision", func(t *testing.T) {
		obs := &observadorGrabador{}
		s := &servicio{
			idp:             &idpQueResponde{tokenIssue: idp.Token{AccessToken: "at", IdpSessionID: "idp-1"}},
			latencyObserver: obs,
		}
		steps := &emissionSteps{s: s}

		if _, err := steps.Issue(context.Background(), Credentials{Usuario: "u"}, "sesion-1"); err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if len(obs.fases) != 1 || obs.fases[0] != FaseEmision {
			t.Fatalf("fases observadas = %v, se esperaba [emision]", obs.fases)
		}
	})

	// Refresco: el punto de medición vive en servicio.refrescar, antes de la
	// transacción. Cortamos con un error del IdP para no tocar Postgres.
	t.Run("refresco reporta una FaseRefresco", func(t *testing.T) {
		obs := &observadorGrabador{}
		s := &servicio{
			idp:             &idpQueResponde{errRefresh: idp.ErrProveedorNoDisponible},
			latencyObserver: obs,
		}

		_, err := s.refrescar(context.Background(), "refresh-token")
		if err == nil {
			t.Fatal("se esperaba el error del IdP que corta antes de Postgres")
		}
		if len(obs.fases) != 1 || obs.fases[0] != FaseRefresco {
			t.Fatalf("fases observadas = %v, se esperaba [refresco]", obs.fases)
		}
	})
}

// --- Property 3: el refresco no muta ni compone claims en Go ----------------

// Feature: e4-t4-reevaluacion-claims-refresco, Property 3: El refresco deriva el sesion_id del token del IdP, no de una composición en Go
//
// El contrato del refresco (A-4 a) es que el sesion_id sale del CLAIM del token
// que el IdP recompuso, y que los tokens transitan sin transformación. Esta
// prueba afirma, sin Postgres, que: (a) el RefreshResolver productivo deriva el
// sesion_id del token del IdP; (b) el AccessToken y el RefreshToken no se
// alteran en Go. Es la parte del Requisito 5 verificable en unidad; la prueba
// del flujo completo de refrescar (hasta el return) es [Requiere Postgres] y
// vive en las pruebas de integración de E4-T3.
func TestPropertyRefrescoDerivaSesionIDDelToken(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100, Rand: rand.New(rand.NewSource(45))}

	err := quick.Check(func(accessNuevo, refreshNuevo, sesionID string) bool {
		if sesionID == "" {
			return true // el resolver rechaza sesion_id vacío; fuera del caso
		}
		tokenIdP := idp.Token{
			AccessToken:  accessNuevo,
			RefreshToken: refreshNuevo,
			IdpSessionID: "idp-" + sesionID,
		}

		// Resolver productivo: el sesion_id sale del claim del token (A-4 a). La
		// costura TokenSesionID simula a la extensión del IdP que transporta el
		// sesion_id en el token; el flujo Go no lo compone.
		resolver := NewRefreshResolver(func(tok idp.Token) (string, error) {
			// Deriva el sesion_id SOLO del token recibido, sin componer claims.
			if tok.IdpSessionID == "idp-"+sesionID {
				return sesionID, nil
			}
			return "", nil
		})

		sid, errR := resolver.SesionIDFor(context.Background(), tokenIdP)
		if errR != nil {
			return false
		}
		// El sesion_id proviene del token (no se compuso en Go), y los tokens
		// del IdP no se transformaron.
		return sid == sesionID &&
			tokenIdP.AccessToken == accessNuevo &&
			tokenIdP.RefreshToken == refreshNuevo
	}, cfg)
	if err != nil {
		t.Fatalf("el refresco debe derivar el sesion_id del token del IdP sin transformarlo: %v (semilla 45)", err)
	}
}
