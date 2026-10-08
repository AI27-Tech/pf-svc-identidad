package postgres_test

import (
	"testing"
)

// TestCargadorModeloVigenteCeroRolesRetornaError verifica que ModeloVigenteDe
// retorna ErrRolNoUnico cuando el sujeto no tiene asignaciones activas.
func TestCargadorModeloVigenteCeroRolesRetornaError(t *testing.T) {
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}

// TestCargadorModeloVigenteMultiplesRolesRetornaError verifica que
// ModeloVigenteDe retorna ErrRolNoUnico cuando el sujeto tiene múltiples
// asignaciones activas.
func TestCargadorModeloVigenteMultiplesRolesRetornaError(t *testing.T) {
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}

// TestCargadorModeloVigenteUnRolRetornaModelo verifica que ModeloVigenteDe
// retorna correctamente el modelo cuando existe exactamente un rol activo.
func TestCargadorModeloVigenteUnRolRetornaModelo(t *testing.T) {
	t.Skip("integración Postgres requerida (testcontainers pendiente)")
}
