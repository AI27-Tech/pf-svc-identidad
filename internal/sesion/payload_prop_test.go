package sesion

import (
	"encoding/json"
	"math/rand"
	"testing"
	"testing/quick"
	"time"
)

// Property 8: Payload mínimo exacto y decodificable (Requisito 5.1, R10.6).
// Para todo Tombstone (con sesion_id UUID y marcas de tiempo arbitrarias en UTC),
// la salida del PayloadEncoder tiene exactamente las claves {sesion_id, revocada_en, expira_en}
// y protojson.Unmarshal lo decodifica sin error.
func TestPropertyPayloadMinimalAndDecodable(t *testing.T) {
	// Feature comment as required by design.
	t.Log("Feature: e4-t3-registro-sesion-revocacion, Property 8: Payload mínimo exacto")

	f := func() bool {
		// Generar un UUID válido para sesion_id
		sesionID := DefaultIDGenerator()

		// Generar marcas de tiempo en UTC con precisión de microsegundos
		now := time.Now().UTC()
		revocadaEn := now
		expiraEn := now.Add(time.Hour)

		ts := Tombstone{
			SesionID:   sesionID,
			RevocadaEn: revocadaEn,
			ExpiraEn:   expiraEn,
		}

		// Codificar con el codificador por omisión
		payload, err := DefaultPayloadEncoder(ts)
		if err != nil {
			t.Logf("FALLO: DefaultPayloadEncoder devolvió error: %v", err)
			return false
		}

		// Decodificar el JSON y verificar que tiene exactamente las claves esperadas
		var m map[string]interface{}
		err = json.Unmarshal(payload, &m)
		if err != nil {
			t.Logf("FALLO: json.Unmarshal falló: %v", err)
			return false
		}

		// Verificar conjunto exacto de claves
		expectedKeys := map[string]bool{
			"sesion_id":   true,
			"revocada_en": true,
			"expira_en":   true,
		}

		// Verificar que no hay claves extra
		for key := range m {
			if !expectedKeys[key] {
				t.Logf("FALLO: Clave inesperada en payload: %s", key)
				return false
			}
		}

		// Verificar que no faltan claves
		for key := range expectedKeys {
			if _, ok := m[key]; !ok {
				t.Logf("FALLO: Falta clave esperada: %s", key)
				return false
			}
		}

		// Verificar que los valores están presentes
		if _, ok := m["sesion_id"].(string); !ok || m["sesion_id"] == "" {
			t.Logf("FALLO: sesion_id no es un string no vacío")
			return false
		}

		// revocada_en y expira_en deben ser strings de timestamp (RFC 3339)
		if _, ok := m["revocada_en"].(string); !ok || m["revocada_en"] == "" {
			t.Logf("FALLO: revocada_en no es un string no vacío")
			return false
		}
		if _, ok := m["expira_en"].(string); !ok || m["expira_en"] == "" {
			t.Logf("FALLO: expira_en no es un string no vacío")
			return false
		}

		return true
	}

	// Configurar testing/quick con MaxCount: 100
	err := quick.Check(f, &quick.Config{
		MaxCount: 100,
		Rand:     rand.New(rand.NewSource(42)), // Semilla fija para reproducibilidad
	})

	if err != nil {
		t.Fatalf("Property 8 falló: %v", err)
	}
}
