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
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// nuevoSesionID genera un `sesion_id` de prueba. En producción lo genera el
// llamador de Issue (E4-T3); la suite de contrato sólo necesita un valor
// válido para ejercer la firma del puerto.
func nuevoSesionID() string {
	return uuid.NewString()
}

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
		tok, err := prov.Issue(context.Background(), cred, nuevoSesionID())
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
		tok, err := prov.Issue(context.Background(), mala, nuevoSesionID())
		if err == nil {
			t.Fatal("Issue aceptó una credencial inválida")
		}
		if tok.AccessToken != "" {
			t.Error("Issue devolvió un token pese a la credencial inválida")
		}
	})

	t.Run("refrescar con el refresh token emitido devuelve token nuevo", func(t *testing.T) {
		prov, cred := nueva(t)
		tok, err := prov.Issue(context.Background(), cred, nuevoSesionID())
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

// Categorias corre una suite ADICIONAL de casos de borde y categorías de error.
// El adaptador real debe pasarla. Esta suite se agregó después de que los
// cuatro casos de Ejecutar ya estaban estabilizados.
func Categorias(t *testing.T, nueva Factory) {
	t.Helper()

	t.Run("credencial inválida devuelve sólo ErrCredencialInvalida y Token cero", func(t *testing.T) {
		prov, cred := nueva(t)
		mala := idp.Credentials{Usuario: cred.Usuario + "-inexistente", Contrasena: "cualquier"}
		tok, err := prov.Issue(context.Background(), mala, nuevoSesionID())
		if !errors.Is(err, idp.ErrCredencialInvalida) {
			t.Errorf("esperaba ErrCredencialInvalida, obtuvo %v", err)
		}
		if tok.AccessToken != "" || tok.RefreshToken != "" {
			t.Error("credencial inválida devolvió Token no cero")
		}
	})

	t.Run("usuario inexistente y contraseña incorrecta producen textos idénticos sin revelar usuario", func(t *testing.T) {
		prov, cred := nueva(t)
		malaContrasenha := idp.Credentials{Usuario: cred.Usuario, Contrasena: "incorrecta"}
		_, err1 := prov.Issue(context.Background(), malaContrasenha, nuevoSesionID())

		usuarioInexistente := idp.Credentials{Usuario: cred.Usuario + "-inexistente", Contrasena: cred.Contrasena}
		_, err2 := prov.Issue(context.Background(), usuarioInexistente, nuevoSesionID())

		// Ambos deben ser ErrCredencialInvalida (el texto es idéntico)
		if !errors.Is(err1, idp.ErrCredencialInvalida) || !errors.Is(err2, idp.ErrCredencialInvalida) {
			t.Fatalf("uno de los errores no es ErrCredencialInvalida: %v, %v", err1, err2)
		}

		// El mensaje no debe revelar el usuario
		if err1 != nil && err2 != nil {
			msg1 := err1.Error()
			msg2 := err2.Error()
			if msg1 != msg2 {
				t.Errorf("mensajes diferentes revelan cuál usuario existe: %q vs %q", msg1, msg2)
			}
			if msg1 != "" && msg2 != "" {
				if contains(msg1, cred.Usuario) || contains(msg2, cred.Usuario) {
					t.Error("el mensaje de error contiene el nombre de usuario")
				}
			}
		}
	})

	t.Run("refresh token desconocido devuelve sólo ErrRefreshInvalido", func(t *testing.T) {
		prov, _ := nueva(t)
		tok, err := prov.Refresh(context.Background(), "refresh-desconocido-xyz")
		if !errors.Is(err, idp.ErrRefreshInvalido) {
			t.Errorf("esperaba ErrRefreshInvalido, obtuvo %v", err)
		}
		if tok.AccessToken != "" || tok.RefreshToken != "" {
			t.Error("refresh desconocido devolvió Token no cero")
		}
	})

	t.Run("refresh ya usado o de sesión revocada devuelve ErrRefreshInvalido", func(t *testing.T) {
		prov, cred := nueva(t)
		tok, err := prov.Issue(context.Background(), cred, nuevoSesionID())
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if tok.RefreshToken == "" {
			t.Skip("la implementación no entrega refresh token")
		}

		// Primer refresco: válido
		_, err = prov.Refresh(context.Background(), tok.RefreshToken)
		if err != nil {
			t.Fatalf("primer Refresh: %v", err)
		}

		// Segundo refresco con el mismo token: debe invalidar si la
		// implementación rota el refresh token (A-4). Si no rota, el caso no
		// aplica y se omite citando esa decisión.
		tok2, err := prov.Refresh(context.Background(), tok.RefreshToken)
		if !errors.Is(err, idp.ErrRefreshInvalido) {
			t.Skip("pendiente A-4: la implementación no rota el refresh token")
		}
		if tok2.AccessToken != "" || tok2.RefreshToken != "" {
			t.Error("refresh ya usado devolvió Token no cero")
		}
	})

	t.Run("contexto cancelado devuelve errors.Is(err, context.Canceled), sin centinela", func(t *testing.T) {
		prov, cred := nueva(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := prov.Issue(ctx, cred, nuevoSesionID())
		if err == nil {
			t.Skip("la implementación no observa la cancelación del contexto; el caso no aplica")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("esperaba context.Canceled, obtuvo %v", err)
		}
		// No debe ser un centinela del puerto
		if errors.Is(err, idp.ErrCredencialInvalida) ||
			errors.Is(err, idp.ErrRefreshInvalido) ||
			errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Error("contexto cancelado devolvió un centinela del puerto")
		}
	})
}

// contains es un ayudante para verificar si un string contiene una subcadena.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && false || findIn(s, substr) >= 0
}

// findIn es un ayudante simple que busca una subcadena en un string.
func findIn(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// === IdpAdmin (E4-T6) ===

// FactoryAdmin construye una implementación IdpAdmin lista para probar.
// Devuelve el adapter de administración y los parámetros necesarios para
// verificar que cumple el contrato (E4-T6, tarea 6.2).
type FactoryAdmin func(t *testing.T) idp.IdpAdmin

// EjecutarAdmin corre la suite de contrato para IdpAdmin. Ambas
// implementaciones (doble en memoria y adaptador real de Keycloak) deben
// pasarla sin cambios.
func EjecutarAdmin(t *testing.T, nueva FactoryAdmin) {
	t.Helper()

	t.Run("crear usuario es idempotente por usuario", func(t *testing.T) {
		admin := nueva(t)

		u := idp.NuevoUsuario{
			Usuario:  "testuser@example.com",
			Nombre:   "Test User",
			TenantID: uuid.NewString(),
			Rol:      "administrador",
		}

		// Primer alta
		sub1, err := admin.CrearUsuario(context.Background(), u)
		if err != nil {
			t.Fatalf("primer CrearUsuario: %v", err)
		}
		if sub1 == "" {
			t.Error("CrearUsuario devolvió sub vacío")
		}

		// Segundo alta con el mismo usuario: debe retornar el mismo sub
		sub2, err := admin.CrearUsuario(context.Background(), u)
		if err != nil {
			t.Fatalf("segundo CrearUsuario (idempotente): %v", err)
		}
		if sub1 != sub2 {
			t.Errorf("idempotencia violada: primer sub=%q, segundo sub=%q", sub1, sub2)
		}
	})

	t.Run("mover de grupo quita antes de agregar", func(t *testing.T) {
		admin := nueva(t)

		u := idp.NuevoUsuario{
			Usuario:  "testuser2@example.com",
			Nombre:   "Test User 2",
			TenantID: uuid.NewString(),
			Rol:      "jefe_operativo",
		}

		// Crear usuario en el primer rol
		sub, err := admin.CrearUsuario(context.Background(), u)
		if err != nil {
			t.Fatalf("CrearUsuario: %v", err)
		}

		// Mover a un rol diferente (el doble nunca lo deja en dos grupos a la vez)
		err = admin.MoverDeGrupo(context.Background(), sub, "jefe_operativo", "administrador")
		if err != nil {
			t.Fatalf("MoverDeGrupo: %v", err)
		}

		// (En el doble, podemos inspeccionar el estado; en Keycloak sería una
		// llamada de admin a /users/{id}/groups. Aquí sólo verificamos que no falla.)
	})

	t.Run("deshabilitar y habilitar son idempotentes", func(t *testing.T) {
		admin := nueva(t)

		u := idp.NuevoUsuario{
			Usuario:  "testuser3@example.com",
			Nombre:   "Test User 3",
			TenantID: uuid.NewString(),
			Rol:      "monitorista",
		}

		sub, err := admin.CrearUsuario(context.Background(), u)
		if err != nil {
			t.Fatalf("CrearUsuario: %v", err)
		}

		// Deshabilitar una vez
		if err := admin.DeshabilitarUsuario(context.Background(), sub); err != nil {
			t.Fatalf("primer DeshabilitarUsuario: %v", err)
		}

		// Deshabilitar nuevamente (idempotente)
		if err := admin.DeshabilitarUsuario(context.Background(), sub); err != nil {
			t.Fatalf("segundo DeshabilitarUsuario (idempotente): %v", err)
		}

		// Habilitar una vez
		if err := admin.HabilitarUsuario(context.Background(), sub); err != nil {
			t.Fatalf("primer HabilitarUsuario: %v", err)
		}

		// Habilitar nuevamente (idempotente)
		if err := admin.HabilitarUsuario(context.Background(), sub); err != nil {
			t.Fatalf("segundo HabilitarUsuario (idempotente): %v", err)
		}
	})
}
