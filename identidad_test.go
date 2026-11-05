package identidad

import (
	"strings"
	"testing"
)

// TestNewFallaSinIDP fija el fail-fast de New (B2 de E4-T1): construir el módulo
// sin Deps.IDP debe devolver un error al ARRANCAR, no dejar un IdP nil que haga
// panic después, a mitad del Paso 2 de la revocación (ADR 26 (h)). El error debe
// nombrar Deps.IDP para que la raíz de composición sepa qué inyectar.
func TestNewFallaSinIDP(t *testing.T) {
	_, err := New(Pools{}, Deps{})
	if err == nil {
		t.Fatal("New sin Deps.IDP debería fallar al arrancar, no devolver un módulo con IdP nil")
	}
	if !strings.Contains(err.Error(), "IDP") {
		t.Errorf("el error no nombra Deps.IDP: %q", err.Error())
	}
}
