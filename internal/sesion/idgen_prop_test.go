package sesion

import (
	"math/rand"
	"testing"
	"testing/quick"

	"github.com/google/uuid"
)

// Property 2: Generación de sesion_id (Requisito 1.2, R1).
// Para todo n en [1, 1000], los n valores del generador por omisión
// parsean como UUID y son distintos entre sí.
func TestPropertyIDGenerationUniquenessAndValidity(t *testing.T) {
	// Feature comment as required by design.
	t.Log("Feature: e4-t3-registro-sesion-revocacion, Property 2: Generación de sesion_id")

	f := func(n uint16) bool {
		// Acotar n a [1, 1000]
		n = (n % 1000) + 1

		// Generar n UUIDs
		generados := make(map[string]bool)
		for i := 0; i < int(n); i++ {
			id := DefaultIDGenerator()

			// Verificar que es un UUID válido
			_, err := uuid.Parse(id)
			if err != nil {
				t.Logf("FALLO: DefaultIDGenerator devolvió un valor que no es UUID: %s (error: %v)", id, err)
				return false
			}

			// Verificar que no se repite
			if generados[id] {
				t.Logf("FALLO: DefaultIDGenerator devolvió un UUID duplicado: %s", id)
				return false
			}
			generados[id] = true
		}

		return true
	}

	// Configurar testing/quick con MaxCount: 100
	err := quick.Check(f, &quick.Config{
		MaxCount: 100,
		Rand:     rand.New(rand.NewSource(42)), // Semilla fija para reproducibilidad
	})

	if err != nil {
		t.Fatalf("Property 2 falló: %v", err)
	}
}
