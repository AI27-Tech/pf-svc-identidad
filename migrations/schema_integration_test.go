package migrations_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-svc-identidad/internal/dbtest"
)

// TestEsquemaSesion es el smoke de esquema de E4-T3 (Tarea 1.3): tras aplicar
// todas las migraciones, verifica que la tabla identidad.sesion tiene la forma
// que E4-T3 espera, en particular la columna idp_session_id que agrega 0003.
// Se salta sin IDENTIDAD_TEST_DB (CI sin Postgres).
func TestEsquemaSesion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, closeAdmin := dbtest.AdminConn(t, ctx)
	defer closeAdmin()

	cleanup := func() {
		dbtest.Exec(t, ctx, admin, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		dbtest.Exec(t, ctx, admin, "DROP ROLE IF EXISTS "+pgx.Identifier{role}.Sanitize())
	}
	cleanup()
	defer cleanup()
	applyMigrations(t, ctx, admin)

	// --- idp_session_id existe, es text y admite nulos (0003) ---------------
	var tipo string
	var nullable string
	err := admin.QueryRow(ctx, `
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'identidad' AND table_name = 'sesion' AND column_name = 'idp_session_id'`,
	).Scan(&tipo, &nullable)
	if err != nil {
		t.Fatalf("idp_session_id debería existir en identidad.sesion: %v", err)
	}
	if tipo != "text" {
		t.Errorf("idp_session_id: tipo = %q; se esperaba text", tipo)
	}
	if nullable != "YES" {
		t.Errorf("idp_session_id: is_nullable = %q; se esperaba YES (sin NOT NULL, K-12)", nullable)
	}

	// --- El conjunto de columnas de sesion es exactamente el esperado -------
	rows, err := admin.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'identidad' AND table_name = 'sesion'
		ORDER BY column_name`)
	if err != nil {
		t.Fatalf("consultar columnas de sesion: %v", err)
	}
	var columnas []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan columna: %v", err)
		}
		columnas = append(columnas, c)
	}
	rows.Close()

	esperadas := []string{
		"emitida_en", "expira_en", "idp_session_id", "revocada_en", "sesion_id", "sujeto_id",
	}
	sort.Strings(esperadas)
	if !iguales(columnas, esperadas) {
		t.Errorf("columnas de sesion = %v; se esperaba %v (sin alcance)", columnas, esperadas)
	}

	// --- El índice una_sesion_viva existe y es el que crea 0001 -------------
	var indexdef string
	err = admin.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'identidad' AND indexname = 'una_sesion_viva'`,
	).Scan(&indexdef)
	if err != nil {
		t.Fatalf("el índice una_sesion_viva debería existir (0001): %v", err)
	}
	// Debe ser único, sobre sujeto_id, parcial para filas vivas (revocada_en IS NULL).
	for _, frag := range []string{"UNIQUE", "sujeto_id", "revocada_en IS NULL"} {
		if !strings.Contains(indexdef, frag) {
			t.Errorf("una_sesion_viva no contiene %q; indexdef = %q", frag, indexdef)
		}
	}
}

func iguales(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
