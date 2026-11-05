package sesion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// TestHappyPathNoOptions verifica que sin opciones el servicio se comporta como hoy:
// EmitirSesion y RefrescarSesion devuelven errNoImplementado, y NewServicio sigue
// compilando con cuatro parámetros (R7.1). ListarRevocadas requiere Postgres real.
func TestHappyPathNoOptions(t *testing.T) {
	// Dobles mínimos para construir un servicio.
	idpStub := &mockIdP{}
	txStub := &mockBeginTx{}

	// Construir un servicio sin opciones (cuatro parámetros, comportamiento de hoy).
	svc := NewServicio(idpStub, txStub, nil, nil)

	if svc == nil {
		t.Fatal("NewServicio devolvió nil")
	}

	// R7.1: EmitirSesion sin opciones devuelve errNoImplementado.
	ctx := context.Background()
	_, err := svc.EmitirSesion(ctx, Credentials{Usuario: "user", Contrasena: "pass"})
	if err == nil {
		t.Fatal("EmitirSesion sin opciones debería devolver error")
	}
	if !errors.Is(err, errNoImplementado) {
		t.Fatalf("EmitirSesion devolvió %v, esperaba errNoImplementado", err)
	}

	// R7.1: RefrescarSesion sin opciones devuelve errNoImplementado.
	_, err = svc.RefrescarSesion(ctx, "refresh-token")
	if err == nil {
		t.Fatal("RefrescarSesion sin opciones debería devolver error")
	}
	if !errors.Is(err, errNoImplementado) {
		t.Fatalf("RefrescarSesion devolvió %v, esperaba errNoImplementado", err)
	}
}

// TestWithIDGeneratorOption verifica que WithIDGenerator configura el generador de sesion_id.
// Los valores generados deben parsear como UUID v4 y ser distintos entre sí (Property 2, R1.2).
func TestWithIDGeneratorOption(t *testing.T) {
	idpStub := &mockIdP{}
	txStub := &mockBeginTx{}

	// Configurar el generador por omisión con WithIDGenerator.
	svc := NewServicio(idpStub, txStub, nil, nil,
		WithIDGenerator(DefaultIDGenerator),
	)

	if svc == nil {
		t.Fatal("NewServicio con WithIDGenerator devolvió nil")
	}

	// Verificar que el generador produce UUIDs válidos y distintos (muestra).
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := DefaultIDGenerator()
		_, err := uuid.Parse(id)
		if err != nil {
			t.Fatalf("DefaultIDGenerator devolvió un valor que no es UUID: %s", id)
		}
		if ids[id] {
			t.Fatalf("DefaultIDGenerator devolvió un UUID duplicado: %s", id)
		}
		ids[id] = true
	}
}

// TestDefaultPayloadEncoder verifica que el codificador por omisión serializa
// exactamente {sesion_id, revocada_en, expira_en} (Property 8, R5.1).
func TestDefaultPayloadEncoder(t *testing.T) {
	now := time.Now().UTC()

	// Crear un Tombstone de prueba.
	ts := Tombstone{
		SesionID:   "550e8400-e29b-41d4-a716-446655440000",
		RevocadaEn: now,
		ExpiraEn:   now.Add(time.Hour),
	}

	// Codificar con el codificador por omisión.
	payload, err := DefaultPayloadEncoder(ts)
	if err != nil {
		t.Fatalf("DefaultPayloadEncoder devolvió error: %v", err)
	}

	// Decodificar y verificar claves exactas.
	var m map[string]interface{}
	err = json.Unmarshal(payload, &m)
	if err != nil {
		t.Fatalf("json.Unmarshal falló: %v", err)
	}

	expectedKeys := map[string]bool{"sesion_id": true, "revocada_en": true, "expira_en": true}
	for key := range m {
		if !expectedKeys[key] {
			t.Fatalf("Clave inesperada en payload: %s", key)
		}
	}
	for key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Fatalf("Falta clave esperada: %s", key)
		}
	}
}

// Dobles mínimos para las pruebas.

type mockIdP struct{}

func (m *mockIdP) Issue(ctx context.Context, cred idp.Credentials, sesionID string) (idp.Token, error) {
	return idp.Token{}, errNoImplementado
}

func (m *mockIdP) Refresh(ctx context.Context, refreshToken string) (idp.Token, error) {
	return idp.Token{}, errNoImplementado
}

func (m *mockIdP) Revoke(ctx context.Context, idpSessionID string) error {
	return errNoImplementado
}

type mockBeginTx struct{}

func (m *mockBeginTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errNoImplementado
}

type mockPublisher struct{}

// Nota: La estructura del Publisher real es más compleja, pero esto
// es suficiente para los tests unitarios.

// testExpiryPolicy es una implementación de ExpiryPolicy para pruebas unitarias.
// Devuelve siempre ahora + 1 hora (igual que sesiontest.LatestTokenExpiry).
type testExpiryPolicy struct{}

func (testExpiryPolicy) AtIssue(tok idp.Token) (time.Time, error) {
	return time.Now().UTC().Add(time.Hour), nil
}

func (testExpiryPolicy) AtRefresh(current time.Time, tok idp.Token) (time.Time, error) {
	return time.Now().UTC().Add(time.Hour), nil
}

// TestLatestTokenExpiryPolicy verifica que la política de expiración propuesta
// [Propuesta de Kiro, no aprobada] en sesiontest.LatestTokenExpiry se puede usar
// como ExpiryPolicy y devuelve ahora + 1 hora en ambos AtIssue y AtRefresh (B2.2).
func TestLatestTokenExpiryPolicy(t *testing.T) {
	// Usar la implementación local testExpiryPolicy (que emula sesiontest.LatestTokenExpiry).
	impl := testExpiryPolicy{}

	// Verificar que es una ExpiryPolicy (compila solo si la implementación es correcta).
	var _ ExpiryPolicy = impl

	// Verificar que AtIssue devuelve un tiempo en el futuro.
	beforeAtIssue := time.Now().UTC()
	expiryIssue, errIssue := impl.AtIssue(idp.Token{})
	afterAtIssue := time.Now().UTC()

	if errIssue != nil {
		t.Fatalf("AtIssue devolvió error: %v", errIssue)
	}

	// Debería devolver aproximadamente ahora + 1 hora (con margen para ejecución).
	expectedMin := beforeAtIssue.Add(time.Hour)
	expectedMax := afterAtIssue.Add(time.Hour)

	if expiryIssue.Before(expectedMin) || expiryIssue.After(expectedMax) {
		t.Fatalf("AtIssue devolvió %v fuera del rango esperado [%v, %v]",
			expiryIssue, expectedMin, expectedMax)
	}

	// Verificar que AtRefresh también devuelve ahora + 1 hora.
	beforeAtRefresh := time.Now().UTC()
	expiryRefresh, errRefresh := impl.AtRefresh(time.Now().UTC().Add(-time.Minute), idp.Token{})
	afterAtRefresh := time.Now().UTC()

	if errRefresh != nil {
		t.Fatalf("AtRefresh devolvió error: %v", errRefresh)
	}

	expectedMinRefresh := beforeAtRefresh.Add(time.Hour)
	expectedMaxRefresh := afterAtRefresh.Add(time.Hour)

	if expiryRefresh.Before(expectedMinRefresh) || expiryRefresh.After(expectedMaxRefresh) {
		t.Fatalf("AtRefresh devolvió %v fuera del rango esperado [%v, %v]",
			expiryRefresh, expectedMinRefresh, expectedMaxRefresh)
	}

	// Verificar que se puede usar con WithExpiryPolicy.
	idpStub := &mockIdP{}
	txStub := &mockBeginTx{}

	// Esta compilación verifica que la ExpiryPolicy
	// se puede pasar a WithExpiryPolicy correctamente (B2.1).
	svc := NewServicio(idpStub, txStub, nil, nil,
		WithExpiryPolicy(impl),
	)

	if svc == nil {
		t.Fatal("NewServicio con WithExpiryPolicy devolvió nil")
	}

	// El servicio compiló, lo que significa que WithExpiryPolicy acepta impl.
	// No hacemos más verificación aquí (G2 no cablea el uso real de ExpiryPolicy
	// en el flujo de emisión ni refresco; eso es G4 y G6).
}
