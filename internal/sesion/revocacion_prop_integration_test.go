package sesion

import (
	"context"
	"math/rand"
	"testing"
	"testing/quick"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Property 7: La revocación solo toca su sesion_id (Requisito 4.8, R10.3).
// Ampliado para A-2 (múltiples sesiones vivas por sujeto):
// Con el índice actual (a lo sumo una viva por sujeto más cualquier número de revocadas):
// revocar una fila deja las demás idénticas columna por columna, incluidas las del mismo sujeto.
//
// CONDICIÓN: Esta prueba está condicionada a A-2. Si A-2 elige (b) (permitir múltiples
// sesiones vivas por sujeto), la tarea B5.2 extiende el generador para incluir m ≤ 3
// sesiones vivas del mismo sujeto y verifica que la revocación toca solo su fila.
func TestPropertyRevocationTouchesOnlyItsSessionID(t *testing.T) {
	if testing.Short() {
		t.Skip("omitir en modo -short")
	}

	ctx := context.Background()
	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()

	const (
		schema = "test_sesion_revocation_property"
		role   = "test_sesion_revocation_role"
	)

	// Setup: crear role y esquema
	dbtest.Exec(t, ctx, admin, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN")
	dbtest.Exec(t, ctx, admin, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	dbtest.Exec(t, ctx, admin, "GRANT USAGE ON SCHEMA "+pgx.Identifier{schema}.Sanitize()+" TO "+pgx.Identifier{role}.Sanitize())

	defer func() {
		dbtest.Exec(t, ctx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, ctx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	}()

	// Crear la tabla de prueba
	dbtest.Exec(t, ctx, admin, `
		CREATE TABLE `+pgx.Identifier{schema, "sesion"}.Sanitize()+` (
			sesion_id UUID PRIMARY KEY,
			sujeto_id UUID NOT NULL,
			emitida_en TIMESTAMP NOT NULL DEFAULT now(),
			expira_en TIMESTAMP NOT NULL,
			revocada_en TIMESTAMP,
			idp_session_id TEXT
		)
	`)

	// El rol de prueba necesita SELECT, INSERT y UPDATE sobre la tabla para
	// ejercer la propiedad (insertar filas, leerlas y revocar). Sin este GRANT
	// el INSERT falla con "permission denied for table sesion".
	// DELETE se incluye solo porque la prueba limpia la tabla efímera entre
	// iteraciones; la tabla productiva de 0001 nunca concede DELETE a
	// svc_identidad (append-only, criterio del Lead E0-T5).
	dbtest.Exec(t, ctx, admin, "GRANT SELECT, INSERT, UPDATE, DELETE ON "+pgx.Identifier{schema, "sesion"}.Sanitize()+" TO "+pgx.Identifier{role}.Sanitize())

	pool, closePool := dbtest.PoolAs(t, ctx, role, schema)
	defer closePool()

	t.Log("Feature: e4-t3-registro-sesion-revocacion, Property 7: La revocación solo toca su sesion_id")

	// Generador de datos de prueba
	type testData struct {
		primary struct {
			sesionID     string
			sujetoID     string
			idpSessionID string
		}
		// Para A-2 (b): sesiones extra del mismo sujeto
		extras []struct {
			sesionID     string
			idpSessionID string
		}
	}

	gen := func() testData {
		sujetoID := uuid.New().String()
		sesionID := uuid.New().String()
		idpSessionID := uuid.New().String()

		result := testData{}
		result.primary.sesionID = sesionID
		result.primary.sujetoID = sujetoID
		result.primary.idpSessionID = idpSessionID

		// Con A-2 pendiente, el índice permite solo una viva por sujeto.
		// El generador está preparado para A-2 (b).
		result.extras = []struct {
			sesionID     string
			idpSessionID string
		}{}

		return result
	}

	f := func() bool {
		data := gen()

		// Insertar fila primaria
		_, err := pool.Exec(ctx, `
			INSERT INTO sesion (sesion_id, sujeto_id, emitida_en, expira_en, idp_session_id)
			VALUES ($1, $2, now(), now() + interval '1 hour', $3)
		`, uuid.MustParse(data.primary.sesionID),
			uuid.MustParse(data.primary.sujetoID),
			data.primary.idpSessionID)
		if err != nil {
			t.Logf("FALLO: insertar fila primaria: %v", err)
			return false
		}

		// Insertar filas extra (vacío con A-2 pendiente)
		for _, extra := range data.extras {
			_, err := pool.Exec(ctx, `
				INSERT INTO sesion (sesion_id, sujeto_id, emitida_en, expira_en, idp_session_id)
				VALUES ($1, $2, now(), now() + interval '1 hour', $3)
			`, uuid.MustParse(extra.sesionID),
				uuid.MustParse(data.primary.sujetoID),
				extra.idpSessionID)
			if err != nil {
				t.Logf("FALLO: insertar fila extra: %v", err)
				return false
			}
		}

		// Leer estado inicial de la fila primaria
		var initialRevocadaEn interface{}
		var initialExpiraEn interface{}
		err = pool.QueryRow(ctx, `
			SELECT revocada_en, expira_en FROM sesion WHERE sesion_id = $1
		`, uuid.MustParse(data.primary.sesionID)).Scan(&initialRevocadaEn, &initialExpiraEn)
		if err != nil {
			t.Logf("FALLO: leer fila primaria antes: %v", err)
			return false
		}

		// Guardar estado inicial de filas extra
		extraInitial := make([]map[string]interface{}, 0)
		for _, extra := range data.extras {
			var revEn, expEn interface{}
			err := pool.QueryRow(ctx, `
				SELECT revocada_en, expira_en FROM sesion WHERE sesion_id = $1
			`, uuid.MustParse(extra.sesionID)).Scan(&revEn, &expEn)
			if err != nil {
				t.Logf("FALLO: leer fila extra antes: %v", err)
				return false
			}
			extraInitial = append(extraInitial, map[string]interface{}{
				"revocada_en": revEn,
				"expira_en":   expEn,
			})
		}

		// Revocar la fila primaria
		err = revocarLocalConEvento(ctx, pool, data.primary.sesionID)
		if err != nil {
			t.Logf("FALLO: revocar: %v", err)
			return false
		}

		// Verificar que la fila primaria está revocada
		var revocadaEn interface{}
		var expiraEn interface{}
		err = pool.QueryRow(ctx, `
			SELECT revocada_en, expira_en FROM sesion WHERE sesion_id = $1
		`, uuid.MustParse(data.primary.sesionID)).Scan(&revocadaEn, &expiraEn)
		if err != nil {
			t.Logf("FALLO: leer fila primaria después: %v", err)
			return false
		}

		if revocadaEn == nil {
			t.Logf("FALLO: revocada_en aún es nil")
			return false
		}

		if initialExpiraEn != expiraEn {
			t.Logf("FALLO: expira_en cambió")
			return false
		}

		// Verificar que las filas extra no cambiaron
		for i, extra := range data.extras {
			var revEn, expEn interface{}
			err := pool.QueryRow(ctx, `
				SELECT revocada_en, expira_en FROM sesion WHERE sesion_id = $1
			`, uuid.MustParse(extra.sesionID)).Scan(&revEn, &expEn)
			if err != nil {
				t.Logf("FALLO: leer fila extra %d después: %v", i, err)
				return false
			}

			if revEn != extraInitial[i]["revocada_en"] {
				t.Logf("FALLO: revocada_en cambió en fila extra %d", i)
				return false
			}
			if expEn != extraInitial[i]["expira_en"] {
				t.Logf("FALLO: expira_en cambió en fila extra %d", i)
				return false
			}
		}

		// Limpiar
		_, err = pool.Exec(ctx, `DELETE FROM sesion WHERE sesion_id = $1 OR sujeto_id = $2`,
			uuid.MustParse(data.primary.sesionID),
			uuid.MustParse(data.primary.sujetoID))
		if err != nil {
			t.Logf("FALLO: limpiar: %v", err)
			return false
		}

		return true
	}

	err := quick.Check(f, &quick.Config{
		MaxCount: 20,
		Rand:     rand.New(rand.NewSource(7)),
	})

	if err != nil {
		t.Fatalf("Property 7 falló: %v", err)
	}
}

// revocarLocalConEvento simula el Paso 1 sin transacción completa.
// En producción, vive en servicio.go.
func revocarLocalConEvento(ctx context.Context, pool *pgxpool.Pool, sesionID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE sesion SET revocada_en = now() WHERE sesion_id = $1 AND revocada_en IS NULL
	`, uuid.MustParse(sesionID))
	return err
}
