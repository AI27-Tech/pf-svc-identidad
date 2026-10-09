//go:build integration
// +build integration

package kc

import (
	"context"
	"testing"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/idptest"
)

// TestContratoKeycloak corre la suite de contrato del puerto (idptest.Ejecutar
// e idptest.Categorias) contra un Keycloak real (R1.2, R1.6, R1.7).
//
// Con A-3 resuelto (DEC-071: TOTP) y el formulario de Issue enviando el factor,
// el grant directo con OTP REQUIRED emite correctamente. Por DEC-074 ya no hay
// omisiones por decisión pendiente: la única condición de omisión es que Docker
// no esté disponible (R11.4), que resuelve arranqueKeycloak.
func TestContratoKeycloak(t *testing.T) {
	kc := arranqueKeycloak(t)

	t.Run("Ejecutar", func(t *testing.T) {
		idptest.Ejecutar(t, kc.factory())
	})
	t.Run("Categorias", func(t *testing.T) {
		idptest.Categorias(t, kc.factory())
	})

	// Verificación directa de la emisión exitosa (R1.7): si el grant directo
	// con OTP REQUIRED no emitiera, esto fallaría en vez de omitirse, que es la
	// diferencia que DEC-074 pide respecto del t.Skip anterior.
	t.Run("emisión con usuario+contraseña+OTP entrega refresh token", func(t *testing.T) {
		prov, cred := kc.factory()(t)
		tok, err := prov.Issue(context.Background(), cred)
		if err != nil {
			t.Fatalf("Issue con credencial válida falló: %v", err)
		}
		if tok.AccessToken == "" {
			t.Error("Issue devolvió un access token vacío")
		}
		if tok.RefreshToken == "" {
			t.Error("Issue no devolvió refresh token; el realm debe entregarlo (R1.7)")
		}
	})
}
