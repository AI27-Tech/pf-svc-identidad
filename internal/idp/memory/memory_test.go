package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/idptest"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

// TestContrato corre la suite de contrato del puerto contra el doble en
// memoria. El adaptador real del IdP (en espera de ADR 32) deberá pasar esta
// misma suite.
func TestContrato(t *testing.T) {
	factory := func(t *testing.T) (idp.IdentityProvider, idp.Credentials) {
		t.Helper()
		prov := memory.New()
		cred := idp.Credentials{Usuario: "sujeto@ai27.test", Contrasena: "secreto"}
		prov.Registrar(memory.Sujeto{Usuario: cred.Usuario, Contrasena: cred.Contrasena})
		return prov, cred
	}
	idptest.Ejecutar(t, factory)
	idptest.Categorias(t, factory)
}

// TestSimularCaida verifica que el doble devuelve ErrProveedorNoDisponible
// cuando está en modo caída.
func TestSimularCaida(t *testing.T) {
	prov := memory.New()
	cred := idp.Credentials{Usuario: "test", Contrasena: "pass"}
	prov.Registrar(memory.Sujeto{Usuario: cred.Usuario, Contrasena: cred.Contrasena})

	// Emitir normalmente
	token, err := prov.Issue(context.Background(), cred, "sesion-de-prueba")
	if err != nil {
		t.Fatalf("Issue sin caída: %v", err)
	}
	if token.AccessToken == "" {
		t.Fatal("Issue devolvió token vacío")
	}

	// Activar caída
	prov.SimularCaida(true)

	// Issue debe devolver ErrProveedorNoDisponible y Token vacío
	token, err = prov.Issue(context.Background(), cred, "sesion-de-prueba")
	if !errors.Is(err, idp.ErrProveedorNoDisponible) {
		t.Errorf("Issue en caída: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
	}
	if token.AccessToken != "" || token.RefreshToken != "" {
		t.Error("Issue en caída devolvió token no vacío")
	}

	// Refresh debe devolver ErrProveedorNoDisponible y Token vacío
	token, err = prov.Refresh(context.Background(), "token")
	if !errors.Is(err, idp.ErrProveedorNoDisponible) {
		t.Errorf("Refresh en caída: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
	}
	if token.AccessToken != "" || token.RefreshToken != "" {
		t.Error("Refresh en caída devolvió token no vacío")
	}

	// Revoke debe devolver ErrProveedorNoDisponible
	err = prov.Revoke(context.Background(), "sesion")
	if !errors.Is(err, idp.ErrProveedorNoDisponible) {
		t.Errorf("Revoke en caída: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
	}

	// Desactivar caída
	prov.SimularCaida(false)

	// Debe volver a funcionar
	token, err = prov.Issue(context.Background(), cred, "sesion-de-prueba")
	if err != nil {
		t.Fatalf("Issue tras desactivar caída: %v", err)
	}
	if token.AccessToken == "" {
		t.Fatal("Issue devolvió token vacío tras desactivar caída")
	}
}
