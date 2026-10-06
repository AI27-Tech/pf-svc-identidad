// Package idptest es la suite de pruebas de CONTRATO del puerto
// idp.IdentityProvider. Define el comportamiento observable que cualquier
// implementación debe cumplir: el doble en memoria hoy, y el adaptador del IdP
// on-premise cuando ADR 32 lo elija (DEC-062, Impacto).
//
// La suite no asume firma, red ni persistencia: sólo el contrato del puerto
// (emitir, refrescar, revocar idempotente). Se invoca desde el _test del doble
// y, en su día, desde el _test del adaptador real.
package idptest

import (
	"context"
	"testing"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// Factory construye una implementación lista para probar, junto con una
// credencial válida ya registrada en ella. Cada implementación aporta su
// Factory; la suite no sabe cómo se registra un sujeto.
type Factory func(t *testing.T) (prov idp.IdentityProvider, credValida idp.Credentials)

// Ejecutar corre la suite de contrato completa contra la implementación que
// produce la Factory. El adaptador real deberá pasarla sin cambios.
func Ejecutar(t *testing.T, nueva Factory) {
	t.Helper()

	t.Run("emitir con credencial válida devuelve token", func(t *testing.T) {
		prov, cred := nueva(t)
		tok, err := prov.Issue(context.Background(), cred)
		if err != nil {
			t.Fatalf("Issue devolvió error con credencial válida: %v", err)
		}
		if tok.AccessToken == "" {
			t.Error("Issue devolvió un access token vacío")
		}
		if tok.ExpiraEn.IsZero() {
			t.Error("Issue devolvió ExpiraEn en cero: el token debe tener expiración")
		}
	})

	t.Run("emitir con credencial inválida no devuelve token", func(t *testing.T) {
		prov, cred := nueva(t)
		mala := idp.Credentials{Usuario: cred.Usuario, Contrasena: cred.Contrasena + "-mala"}
		tok, err := prov.Issue(context.Background(), mala)
		if err == nil {
			t.Fatal("Issue aceptó una credencial inválida")
		}
		if tok.AccessToken != "" {
			t.Error("Issue devolvió un token pese a la credencial inválida")
		}
	})

	t.Run("refrescar con el refresh token emitido devuelve token nuevo", func(t *testing.T) {
		prov, cred := nueva(t)
		tok, err := prov.Issue(context.Background(), cred)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if tok.RefreshToken == "" {
			t.Skip("la implementación no entrega refresh token; Refresh no aplica")
		}
		refrescado, err := prov.Refresh(context.Background(), tok.RefreshToken)
		if err != nil {
			t.Fatalf("Refresh devolvió error con un refresh token válido: %v", err)
		}
		if refrescado.AccessToken == "" {
			t.Error("Refresh devolvió un access token vacío")
		}
	})

	t.Run("revocar es idempotente", func(t *testing.T) {
		prov, _ := nueva(t)
		const sesionID = "sesion-de-prueba"
		if err := prov.Revoke(context.Background(), sesionID); err != nil {
			t.Fatalf("primer Revoke devolvió error: %v", err)
		}
		if err := prov.Revoke(context.Background(), sesionID); err != nil {
			t.Fatalf("segundo Revoke (idempotente) devolvió error: %v", err)
		}
	})
}
