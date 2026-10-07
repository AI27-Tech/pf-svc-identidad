package memory_test

import (
	"testing"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/idptest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// TestContrato corre la suite de contrato del puerto contra el doble en
// memoria. El adaptador real del IdP (en espera de ADR 32) deberá pasar esta
// misma suite.
func TestContrato(t *testing.T) {
	idptest.Ejecutar(t, func(t *testing.T) (idp.IdentityProvider, idp.Credentials) {
		t.Helper()
		prov := memory.New()
		cred := idp.Credentials{Usuario: "sujeto@ai27.test", Contrasena: "secreto"}
		prov.Registrar(memory.Sujeto{Usuario: cred.Usuario, Contrasena: cred.Contrasena})
		return prov, cred
	})
}
