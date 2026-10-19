package sesion

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// stepsFalsos es un EmissionSteps de prueba que registra la secuencia de pasos
// y permite inyectar fallos. No toca Postgres ni el IdP real.
type stepsFalsos struct {
	sesionID      string
	tokenEmitido  idp.Token
	sujeto        SujetoRef
	expiraEn      time.Time
	errIssue      error
	errResolve    error
	errExpira     error
	errRegister   error
	compensaCon   []string // idp_session_id con los que se llamó Compensate
	calienta      bool
	ordenLlamadas []string
}

func (f *stepsFalsos) NewSesionID() string {
	f.ordenLlamadas = append(f.ordenLlamadas, "new")
	return f.sesionID
}

func (f *stepsFalsos) Issue(_ context.Context, _ Credentials, sesionID string) (idp.Token, error) {
	f.ordenLlamadas = append(f.ordenLlamadas, "issue")
	if f.errIssue != nil {
		return idp.Token{}, f.errIssue
	}
	return f.tokenEmitido, nil
}

func (f *stepsFalsos) ResolveSujeto(_ context.Context, _ idp.Token) (SujetoRef, error) {
	f.ordenLlamadas = append(f.ordenLlamadas, "resolve")
	if f.errResolve != nil {
		return SujetoRef{}, f.errResolve
	}
	return f.sujeto, nil
}

func (f *stepsFalsos) ExpiresAtIssue(_ idp.Token) (time.Time, error) {
	f.ordenLlamadas = append(f.ordenLlamadas, "expira")
	if f.errExpira != nil {
		return time.Time{}, f.errExpira
	}
	return f.expiraEn, nil
}

func (f *stepsFalsos) Register(_ context.Context, _ SesionRecord) error {
	f.ordenLlamadas = append(f.ordenLlamadas, "register")
	return f.errRegister
}

func (f *stepsFalsos) Compensate(_ context.Context, idpSessionID string) error {
	f.ordenLlamadas = append(f.ordenLlamadas, "compensate")
	f.compensaCon = append(f.compensaCon, idpSessionID)
	return nil
}

func (f *stepsFalsos) WarmCache(_ context.Context, _ SesionRecord) {
	f.ordenLlamadas = append(f.ordenLlamadas, "warm")
	f.calienta = true
}

// TestEmissionFlowCaminoFeliz verifica la secuencia de la opción 1 de A-1:
// new → issue → resolve → expira → register → warm, sin compensar, y que la
// respuesta trae el sesion_id generado y los tokens del IdP.
func TestEmissionFlowCaminoFeliz(t *testing.T) {
	expira := time.Now().Add(time.Hour).UTC()
	steps := &stepsFalsos{
		sesionID:     "sesion-1",
		tokenEmitido: idp.Token{AccessToken: "at", RefreshToken: "rt", IdpSessionID: "idp-1", ExpiraEn: expira},
		sujeto:       SujetoRef{SujetoID: "suj-1", TenantID: "ten-1"},
		expiraEn:     expira,
	}
	flow := NewCompensatingEmissionFlow()

	em, err := flow.Emit(context.Background(), steps, Credentials{Usuario: "u", Contrasena: "p", SegundoFactor: "123456"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if em.SesionID != "sesion-1" {
		t.Errorf("SesionID = %q, se esperaba sesion-1", em.SesionID)
	}
	if em.AccessToken != "at" || em.RefreshToken != "rt" {
		t.Errorf("tokens inesperados: %+v", em)
	}
	if !em.ExpiraEn.Equal(expira) {
		t.Errorf("ExpiraEn = %v, se esperaba %v", em.ExpiraEn, expira)
	}
	if len(steps.compensaCon) != 0 {
		t.Errorf("no debió compensar en el camino feliz, compensó con %v", steps.compensaCon)
	}
	quiero := []string{"new", "issue", "resolve", "expira", "register", "warm"}
	if !iguales(steps.ordenLlamadas, quiero) {
		t.Errorf("orden = %v, se esperaba %v", steps.ordenLlamadas, quiero)
	}
}

// TestEmissionFlowCompensaSiFallaRegistro verifica la opción 1: si Register
// falla, se compensa (revoca en el IdP) con el idp_session_id del token y se
// devuelve error. No se calienta la caché.
func TestEmissionFlowCompensaSiFallaRegistro(t *testing.T) {
	steps := &stepsFalsos{
		sesionID:     "sesion-2",
		tokenEmitido: idp.Token{IdpSessionID: "idp-2"},
		sujeto:       SujetoRef{SujetoID: "suj-2"},
		expiraEn:     time.Now().Add(time.Hour),
		errRegister:  stderrors.New("fallo de postgres"),
	}
	flow := NewCompensatingEmissionFlow()

	// Factor no vacío: esta prueba ejercita el fallo de Register, no la guarda
	// local de segundo factor (DEC-094).
	_, err := flow.Emit(context.Background(), steps, Credentials{SegundoFactor: "123456"})
	if err == nil {
		t.Fatal("se esperaba error cuando Register falla")
	}
	if len(steps.compensaCon) != 1 || steps.compensaCon[0] != "idp-2" {
		t.Errorf("debió compensar con el idp_session_id del token; compensó con %v", steps.compensaCon)
	}
	if steps.calienta {
		t.Error("no debió calentar la caché tras fallar el registro")
	}
}

// TestEmissionFlowErrorIdPNoRegistra verifica que si Issue falla (credencial
// inválida), no se resuelve sujeto ni se registra, y el error se mapea a la
// categoría NoAutorizado (401).
func TestEmissionFlowErrorIdPNoRegistra(t *testing.T) {
	steps := &stepsFalsos{
		sesionID: "sesion-3",
		errIssue: idp.ErrCredencialInvalida,
	}
	flow := NewCompensatingEmissionFlow()

	// Factor no vacío: esta prueba ejercita el rechazo del IdP (credencial
	// inválida), no la guarda local de segundo factor (DEC-094).
	_, err := flow.Emit(context.Background(), steps, Credentials{SegundoFactor: "123456"})
	if err == nil {
		t.Fatal("se esperaba error de credencial")
	}
	if platformerrors.AsCategory(err) != platformerrors.CategoryNoAutorizado {
		t.Errorf("categoría = %v, se esperaba NoAutorizado (401)", platformerrors.AsCategory(err))
	}
	// No debió avanzar más allá de issue.
	quiero := []string{"new", "issue"}
	if !iguales(steps.ordenLlamadas, quiero) {
		t.Errorf("orden = %v, se esperaba %v", steps.ordenLlamadas, quiero)
	}
}

// TestEmissionFlowRechazoLocalSegundoFactorVacio verifica el rechazo local de
// DEC-094 (C-2, opción b): un segundo factor vacío o compuesto solo por
// espacios se rechaza con 401 (NoAutorizado) ANTES de generar el sesion_id y
// de llamar al IdP. No hay residuo: ni "new" ni "issue" en la secuencia, ni
// Compensate (R2.3, R3.3, R3.4). El error es indistinguible del de credencial
// inválida (C-4): misma categoría NoAutorizado y mismo mensaje.
func TestEmissionFlowRechazoLocalSegundoFactorVacio(t *testing.T) {
	// Mensaje de referencia del rechazo del IdP por credencial inválida, para
	// comprobar que el cuerpo del 401 local es idéntico (no enumeración).
	refIdP := mapearErrorIdP(idp.ErrCredencialInvalida)

	for _, caso := range []struct {
		nombre string
		factor string
	}{
		{"ausente", ""},
		{"solo espacios", "   "},
		{"tabulador y salto", "\t\n"},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			steps := &stepsFalsos{sesionID: "no-debe-generarse"}
			flow := NewCompensatingEmissionFlow()

			_, err := flow.Emit(context.Background(), steps,
				Credentials{Usuario: "u", Contrasena: "p", SegundoFactor: caso.factor})
			if err == nil {
				t.Fatal("se esperaba rechazo local por segundo factor vacío")
			}
			if platformerrors.AsCategory(err) != platformerrors.CategoryNoAutorizado {
				t.Errorf("categoría = %v, se esperaba NoAutorizado (401)", platformerrors.AsCategory(err))
			}
			// Cuerpo idéntico al de credencial inválida (C-4, no enumeración).
			if err.Error() != refIdP.Error() {
				t.Errorf("mensaje = %q, se esperaba el mismo que credencial inválida %q", err.Error(), refIdP.Error())
			}
			// Sin residuo: no se generó sesion_id, no se llamó al IdP, no se
			// compensó. La secuencia de pasos debe quedar vacía.
			if len(steps.ordenLlamadas) != 0 {
				t.Errorf("no debió ejecutar ningún paso; ejecutó %v", steps.ordenLlamadas)
			}
			if len(steps.compensaCon) != 0 {
				t.Errorf("no debió compensar; compensó con %v", steps.compensaCon)
			}
		})
	}
}

// TestEmissionFlowIdPNoDisponible verifica el mapeo a 503 (Degradado) cuando el
// IdP no responde al emitir.
func TestEmissionFlowIdPNoDisponible(t *testing.T) {
	steps := &stepsFalsos{errIssue: idp.ErrProveedorNoDisponible}
	// Factor no vacío: esta prueba ejercita el IdP caído, no la guarda local
	// de segundo factor (DEC-094).
	_, err := NewCompensatingEmissionFlow().Emit(context.Background(), steps, Credentials{SegundoFactor: "123456"})
	if platformerrors.AsCategory(err) != platformerrors.CategoryDegradado {
		t.Errorf("categoría = %v, se esperaba Degradado (503)", platformerrors.AsCategory(err))
	}
}

func iguales(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
