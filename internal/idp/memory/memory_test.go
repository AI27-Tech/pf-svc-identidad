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
		// El doble exige segundo factor (DEC-095): el sujeto se registra con un
		// código esperado y la credencial válida lo envía.
		cred := idp.Credentials{Usuario: "sujeto@ai27.test", Contrasena: "secreto", SegundoFactor: "123456"}
		prov.Registrar(memory.Sujeto{Usuario: cred.Usuario, Contrasena: cred.Contrasena, SegundoFactor: cred.SegundoFactor})
		return prov, cred
	}
	idptest.Ejecutar(t, factory)
	idptest.Categorias(t, factory)
}

// TestSimularCaida verifica que el doble devuelve ErrProveedorNoDisponible
// cuando está en modo caída.
func TestSimularCaida(t *testing.T) {
	prov := memory.New()
	cred := idp.Credentials{Usuario: "test", Contrasena: "pass", SegundoFactor: "123456"}
	prov.Registrar(memory.Sujeto{Usuario: cred.Usuario, Contrasena: cred.Contrasena, SegundoFactor: cred.SegundoFactor})

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

// TestContratoAdmin corre la suite de contrato para IdpAdmin contra el doble en
// memoria. El adaptador real de Keycloak (E4-T6, tarea 11) deberá pasar esta
// misma suite.
func TestContratoAdmin(t *testing.T) {
	factory := func(t *testing.T) idp.IdpAdmin {
		t.Helper()
		return memory.New()
	}
	idptest.EjecutarAdmin(t, factory)
}

// TestSegundoFactorObligatorio verifica DEC-095 (C-3, opción b): el doble en
// memoria EXIGE segundo factor. Una credencial con usuario y contraseña
// correctos pero factor ausente o incorrecto se rechaza con
// ErrCredencialInvalida y Token cero. Solo el factor exacto emite.
func TestSegundoFactorObligatorio(t *testing.T) {
	prov := memory.New()
	prov.Registrar(memory.Sujeto{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: "123456"})

	base := func(factor string) idp.Credentials {
		return idp.Credentials{Usuario: "u@ai27.test", Contrasena: "secreto", SegundoFactor: factor}
	}

	t.Run("sin factor rechaza", func(t *testing.T) {
		tok, err := prov.Issue(context.Background(), base(""), "sesion-1")
		if !errors.Is(err, idp.ErrCredencialInvalida) {
			t.Fatalf("esperaba ErrCredencialInvalida, obtuvo %v", err)
		}
		if tok.AccessToken != "" || tok.RefreshToken != "" {
			t.Error("emitió token pese a faltar el factor")
		}
	})

	t.Run("factor incorrecto rechaza", func(t *testing.T) {
		tok, err := prov.Issue(context.Background(), base("000000"), "sesion-2")
		if !errors.Is(err, idp.ErrCredencialInvalida) {
			t.Fatalf("esperaba ErrCredencialInvalida, obtuvo %v", err)
		}
		if tok.AccessToken != "" {
			t.Error("emitió token con factor incorrecto")
		}
	})

	t.Run("factor correcto emite", func(t *testing.T) {
		tok, err := prov.Issue(context.Background(), base("123456"), "sesion-3")
		if err != nil {
			t.Fatalf("Issue con factor correcto: %v", err)
		}
		if tok.AccessToken == "" {
			t.Error("Issue devolvió token vacío con factor correcto")
		}
	})
}

// TestSujetoSinFactorNoEmite verifica que un sujeto registrado SIN código
// esperado nunca puede iniciar sesión (DEC-095: el doble deja de emitir sin
// factor). Es la vía de falla en abierto que la decisión cierra.
func TestSujetoSinFactorNoEmite(t *testing.T) {
	prov := memory.New()
	prov.Registrar(memory.Sujeto{Usuario: "sin-factor@ai27.test", Contrasena: "secreto"})

	cred := idp.Credentials{Usuario: "sin-factor@ai27.test", Contrasena: "secreto", SegundoFactor: "123456"}
	tok, err := prov.Issue(context.Background(), cred, "sesion-x")
	if !errors.Is(err, idp.ErrCredencialInvalida) {
		t.Fatalf("esperaba ErrCredencialInvalida para sujeto sin factor configurado, obtuvo %v", err)
	}
	if tok.AccessToken != "" {
		t.Error("un sujeto sin factor configurado emitió sesión")
	}
}
