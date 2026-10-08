package postgres_test

import (
	"testing"
)

// TestSujetoRepoCrearDuplicadoRetornaError verifica que Crear retorna
// ErrSujetoDuplicado cuando el idp_subject ya existe.
func TestSujetoRepoCrearDuplicadoRetornaError(t *testing.T) {
	// Esta prueba requiere una conexión a Postgres. Para MVP, se saltará la
	// integración real y se documentará como bloqueada en 7.1.
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}

// TestSujetoRepoPorIdpSubjectExistente verifica que PorIdpSubject devuelve
// (sujeto, true, nil) cuando existe.
func TestSujetoRepoPorIdpSubjectExistente(t *testing.T) {
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}

// TestSujetoRepoPorIdpSubjectNoExistente verifica que PorIdpSubject devuelve
// (cero, false, nil) cuando no existe.
func TestSujetoRepoPorIdpSubjectNoExistente(t *testing.T) {
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}

// TestSujetoRepoCrearGuardaNuevoSujeto verifica que Crear persiste un nuevo
// sujeto correctamente.
func TestSujetoRepoCrearGuardaNuevoSujeto(t *testing.T) {
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}
