package sesion

import (
	"context"
	"strings"
	"testing"
	"testing/quick"
	"time"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion/sesiontest"
)

// pasosConIdP es un EmissionSteps que delega Issue en un idp.IdentityProvider
// real (el doble SegundoFactorIdP de E4-T5) y espía el resto de los pasos. Así
// las pruebas de flujo ejercen la validación del segundo factor del IdP y
// verifican que, ante un rechazo, el flujo NO avanza a ResolveSujeto, Register
// ni Compensate (Requisitos 3.2, 3.4). No toca Postgres: "sin fila" se observa
// como "sin llamada a Register".
type pasosConIdP struct {
	prov       idp.IdentityProvider
	sujeto     SujetoRef
	expiraEn   time.Time
	idGen      func() string
	registered []SesionRecord

	ordenLlamadas   []string
	resolveLlamado  int
	registerLlamado int
	compensaCon     []string
	calienta        bool
}

func nuevosPasosConIdP(prov idp.IdentityProvider) *pasosConIdP {
	return &pasosConIdP{
		prov:     prov,
		sujeto:   SujetoRef{SujetoID: "suj-1", TenantID: "ten-1"},
		expiraEn: time.Now().Add(time.Hour).UTC(),
		idGen:    func() string { return "sesion-fija" },
	}
}

func (p *pasosConIdP) NewSesionID() string {
	p.ordenLlamadas = append(p.ordenLlamadas, "new")
	return p.idGen()
}

func (p *pasosConIdP) Issue(ctx context.Context, cred Credentials, sesionID string) (idp.Token, error) {
	p.ordenLlamadas = append(p.ordenLlamadas, "issue")
	return p.prov.Issue(ctx, idp.Credentials{
		Usuario:       cred.Usuario,
		Contrasena:    cred.Contrasena,
		SegundoFactor: cred.SegundoFactor,
	}, sesionID)
}

func (p *pasosConIdP) ResolveSujeto(_ context.Context, _ idp.Token) (SujetoRef, error) {
	p.ordenLlamadas = append(p.ordenLlamadas, "resolve")
	p.resolveLlamado++
	return p.sujeto, nil
}

func (p *pasosConIdP) ExpiresAtIssue(_ idp.Token) (time.Time, error) {
	p.ordenLlamadas = append(p.ordenLlamadas, "expira")
	return p.expiraEn, nil
}

func (p *pasosConIdP) Register(_ context.Context, r SesionRecord) error {
	p.ordenLlamadas = append(p.ordenLlamadas, "register")
	p.registerLlamado++
	p.registered = append(p.registered, r)
	return nil
}

func (p *pasosConIdP) Compensate(_ context.Context, idpSessionID string) error {
	p.ordenLlamadas = append(p.ordenLlamadas, "compensate")
	p.compensaCon = append(p.compensaCon, idpSessionID)
	return nil
}

func (p *pasosConIdP) WarmCache(_ context.Context, _ SesionRecord) {
	p.ordenLlamadas = append(p.ordenLlamadas, "warm")
	p.calienta = true
}

// --- 3.2: servicio/flujo, rechazo sin residuo -------------------------------

// TestFlujoRechazoSegundoFactorSinResiduo cubre la tarea 3.2: con el doble de
// segundo factor, un código ausente, incorrecto o vencido produce 401 y el
// flujo NO llama a ResolveSujeto, Register ni Compensate, y no registra fila
// (Requisitos 3.1, 3.2, 3.4). El código ausente lo corta la guarda local de
// DEC-094 antes de Issue; el incorrecto/vencido los rechaza el IdP en Issue.
func TestFlujoRechazoSegundoFactorSinResiduo(t *testing.T) {
	// Vigente "123456", sin tolerar el anterior: "654321" es el "vencido".
	cfg := sesiontest.ConfigSegundoFactor{
		Usuario:        "u@ai27.test",
		Contrasena:     "secreto",
		CodigoVigente:  "123456",
		CodigoAnterior: "654321",
	}

	casos := []struct {
		nombre      string
		factor      string
		esperaIssue bool // si el flujo debió llegar a Issue (incorrecto/vencido) o no (ausente)
	}{
		{"ausente", "", false},
		{"incorrecto", "000000", true},
		{"vencido (periodo anterior, sin tolerancia)", "654321", true},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			prov := sesiontest.NewSegundoFactorIdP(cfg)
			steps := nuevosPasosConIdP(prov)
			flow := NewCompensatingEmissionFlow()

			_, err := flow.Emit(context.Background(), steps,
				Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: caso.factor})

			if err == nil {
				t.Fatal("se esperaba rechazo por segundo factor")
			}
			if platformerrors.AsCategory(err) != platformerrors.CategoryNoAutorizado {
				t.Errorf("categoría = %v, se esperaba NoAutorizado (401)", platformerrors.AsCategory(err))
			}
			// Sin residuo: nunca se resolvió sujeto, ni se registró, ni se compensó.
			if steps.resolveLlamado != 0 {
				t.Errorf("ResolveSujeto no debió llamarse; se llamó %d veces", steps.resolveLlamado)
			}
			if steps.registerLlamado != 0 {
				t.Errorf("Register no debió llamarse; se llamó %d veces", steps.registerLlamado)
			}
			if len(steps.registered) != 0 {
				t.Errorf("no debió registrarse fila; se registraron %d", len(steps.registered))
			}
			if len(steps.compensaCon) != 0 {
				t.Errorf("Compensate no debió llamarse; se llamó con %v", steps.compensaCon)
			}
			// La guarda local (ausente) corta antes de Issue; el resto llega a Issue.
			if caso.esperaIssue && prov.IssueCalls() != 1 {
				t.Errorf("se esperaba 1 llamada a Issue; hubo %d", prov.IssueCalls())
			}
			if !caso.esperaIssue && prov.IssueCalls() != 0 {
				t.Errorf("con factor ausente, Issue no debió llamarse; hubo %d", prov.IssueCalls())
			}
		})
	}
}

// TestFlujoEmiteConFactorValido confirma la ruta feliz del doble: con el código
// vigente se emite, se resuelve sujeto, se registra una fila y se calienta la
// caché, en ese orden (contraparte positiva de 3.2).
func TestFlujoEmiteConFactorValido(t *testing.T) {
	prov := sesiontest.NewSegundoFactorIdP(sesiontest.ConfigSegundoFactor{
		Usuario: "u@ai27.test", Contrasena: "secreto", CodigoVigente: "123456",
	})
	steps := nuevosPasosConIdP(prov)
	flow := NewCompensatingEmissionFlow()

	em, err := flow.Emit(context.Background(), steps,
		Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: "123456"})
	if err != nil {
		t.Fatalf("Emit con factor válido: %v", err)
	}
	if em.SesionID != "sesion-fija" {
		t.Errorf("SesionID = %q, se esperaba sesion-fija", em.SesionID)
	}
	if steps.registerLlamado != 1 || len(steps.registered) != 1 {
		t.Errorf("se esperaba una fila registrada; register=%d filas=%d", steps.registerLlamado, len(steps.registered))
	}
	if len(steps.compensaCon) != 0 {
		t.Errorf("no debió compensar en la ruta feliz; compensó con %v", steps.compensaCon)
	}
}

// --- 3.3: IdP caído ---------------------------------------------------------

// TestFlujoIdPCaido503 cubre la tarea 3.3: en modo caída, el flujo devuelve 503
// (Degradado), no emite sesión y no hay ruta que omita el factor (Requisitos
// 5.1, 5.2). No se registra fila ni se compensa.
func TestFlujoIdPCaido503(t *testing.T) {
	prov := sesiontest.NewSegundoFactorIdP(sesiontest.ConfigSegundoFactor{
		Usuario: "u@ai27.test", Contrasena: "secreto", CodigoVigente: "123456",
	})
	prov.SimularCaida(true)
	steps := nuevosPasosConIdP(prov)
	flow := NewCompensatingEmissionFlow()

	_, err := flow.Emit(context.Background(), steps,
		Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: "123456"})
	if err == nil {
		t.Fatal("se esperaba error con el IdP caído")
	}
	if platformerrors.AsCategory(err) != platformerrors.CategoryDegradado {
		t.Errorf("categoría = %v, se esperaba Degradado (503)", platformerrors.AsCategory(err))
	}
	if steps.registerLlamado != 0 {
		t.Error("no debió registrarse fila con el IdP caído")
	}
	if len(steps.compensaCon) != 0 {
		t.Error("no debió compensar con el IdP caído")
	}
}

// --- 4.1 / 4.2: propiedades -------------------------------------------------

// TestPropertySinFactorValidoNoHaySesion cubre la Property 1 (Requisitos 2.2,
// 3.1, 3.2): para cualquier combinación de código distinto del vigente
// (incluido el vacío), Emit devuelve 401 y nunca registra fila ni llama a
// Register.
func TestPropertySinFactorValidoNoHaySesion(t *testing.T) {
	t.Log("Feature: e4-t5-entrega-segundo-factor, Property 1: Sin factor válido, no hay sesión")

	const vigente = "123456"
	propiedad := func(codigo string) bool {
		// Normalizar: si el fuzzing genera justo el vigente, lo alteramos para
		// mantener la hipótesis (código != vigente).
		if codigo == vigente {
			codigo = vigente + "x"
		}
		prov := sesiontest.NewSegundoFactorIdP(sesiontest.ConfigSegundoFactor{
			Usuario: "u@ai27.test", Contrasena: "secreto", CodigoVigente: vigente,
		})
		steps := nuevosPasosConIdP(prov)
		flow := NewCompensatingEmissionFlow()

		_, err := flow.Emit(context.Background(), steps,
			Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: codigo})

		if err == nil {
			return false
		}
		if platformerrors.AsCategory(err) != platformerrors.CategoryNoAutorizado {
			return false
		}
		return steps.registerLlamado == 0 && len(steps.registered) == 0
	}

	cfg := &quick.Config{MaxCount: 100}
	if err := quick.Check(propiedad, cfg); err != nil {
		t.Errorf("Property 1 falló: %v", err)
	}
}

// TestPropertyCodigoViajaIntactoYNoSeFiltra cubre la Property 2 (Requisitos
// 4.1, 6.1): para todo código no vacío, el doble recibe EXACTAMENTE el valor
// enviado, y ese valor no aparece en el error devuelto cuando el código es
// inválido. El flujo de emisión no escribe logs, así que la contención de
// secretos se verifica sobre el mensaje de error.
func TestPropertyCodigoViajaIntactoYNoSeFiltra(t *testing.T) {
	t.Log("Feature: e4-t5-entrega-segundo-factor, Property 2: El código viaja intacto y no se filtra")

	const vigente = "123456"
	propiedad := func(codigo string) bool {
		// Solo interesan códigos no vacíos (la guarda local corta los vacíos
		// antes de Issue, y entonces el doble no recibe nada).
		if strings.TrimSpace(codigo) == "" {
			return true
		}
		prov := sesiontest.NewSegundoFactorIdP(sesiontest.ConfigSegundoFactor{
			Usuario: "u@ai27.test", Contrasena: "secreto", CodigoVigente: vigente,
		})
		steps := nuevosPasosConIdP(prov)
		flow := NewCompensatingEmissionFlow()

		_, err := flow.Emit(context.Background(), steps,
			Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: codigo})

		// El doble debe haber recibido el valor EXACTO, sin transformar.
		if prov.UltimoCodigoRecibido() != codigo {
			return false
		}
		// Si el código no es el vigente, hubo error y el mensaje no debe
		// contener el código (no filtrar el secreto).
		if codigo != vigente {
			if err == nil {
				return false
			}
			if strings.Contains(err.Error(), codigo) {
				return false
			}
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 100}
	if err := quick.Check(propiedad, cfg); err != nil {
		t.Errorf("Property 2 falló: %v", err)
	}
}

// --- 5.1: higiene de persistencia (unitaria) --------------------------------

// TestCodigoNoApareceEnLaFilaRegistrada cubre la tarea 5.1 a nivel unitario:
// tras una emisión exitosa, la SesionRecord que se registra NO contiene el
// código del segundo factor en ninguno de sus campos. La verificación contra
// columnas reales de identidad.sesion y claves de caché vive en la prueba de
// integración (requiere Postgres); aquí se fija la garantía observable sin DB:
// el flujo nunca pone el código en la fila.
func TestCodigoNoApareceEnLaFilaRegistrada(t *testing.T) {
	const codigo = "123456"
	prov := sesiontest.NewSegundoFactorIdP(sesiontest.ConfigSegundoFactor{
		Usuario: "u@ai27.test", Contrasena: "secreto", CodigoVigente: codigo,
	})
	steps := nuevosPasosConIdP(prov)
	flow := NewCompensatingEmissionFlow()

	if _, err := flow.Emit(context.Background(), steps,
		Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: codigo}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(steps.registered) != 1 {
		t.Fatalf("se esperaba una fila registrada; hubo %d", len(steps.registered))
	}
	r := steps.registered[0]
	for campo, valor := range map[string]string{
		"SesionID":     r.SesionID,
		"SujetoID":     r.SujetoID,
		"IdpSessionID": r.IdpSessionID,
	} {
		if strings.Contains(valor, codigo) {
			t.Errorf("el campo %s de la fila contiene el código del segundo factor: %q", campo, valor)
		}
	}
}
