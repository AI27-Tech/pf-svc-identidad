package rediscache_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion/rediscache"
)

// envRedis es la variable que activa las pruebas de integración de Redis. Sin
// ella, las pruebas se saltan, de modo que `go test ./...` sigue en verde en CI
// sin Redis (mismo patrón que dbtest con IDENTIDAD_TEST_DB).
const envRedis = "IDENTIDAD_TEST_REDIS"

// nuevoClienteDePrueba abre un cliente contra el Redis de la variable de
// entorno, o salta la prueba si no está definida. Limpia la base al entrar.
func nuevoClienteDePrueba(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv(envRedis)
	if url == "" {
		t.Skipf("se salta la prueba de integración de Redis: %s no está definido", envRedis)
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("URL de Redis inválida: %v", err)
	}
	cliente := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cliente.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis no alcanzable: %v", err)
	}
	// Limpiar la base de prueba para aislar cada corrida.
	if err := cliente.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FlushDB: %v", err)
	}
	t.Cleanup(func() { _ = cliente.Close() })
	return cliente
}

// TestGuardarYLeer verifica que GuardarSesion escribe la clave con el valor y un
// TTL positivo, y que InvalidarSesion la borra.
func TestGuardarYLeer(t *testing.T) {
	cliente := nuevoClienteDePrueba(t)
	cache := rediscache.New(cliente)
	ctx := context.Background()

	const sesionID = "11111111-1111-1111-1111-111111111111"
	expiraEn := time.Now().Add(1 * time.Hour)

	if err := cache.GuardarSesion(ctx, sesionID, expiraEn); err != nil {
		t.Fatalf("GuardarSesion: %v", err)
	}

	// La clave existe con el prefijo esperado y un TTL positivo cercano a 1 h.
	ttl, err := cliente.TTL(ctx, "sesion:"+sesionID).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > time.Hour+time.Minute {
		t.Errorf("TTL fuera de rango: %v (esperado ~1h)", ttl)
	}

	// Invalidar borra la clave.
	if err := cache.InvalidarSesion(ctx, sesionID); err != nil {
		t.Fatalf("InvalidarSesion: %v", err)
	}
	n, err := cliente.Exists(ctx, "sesion:"+sesionID).Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if n != 0 {
		t.Errorf("la clave debería estar borrada tras InvalidarSesion; existe=%d", n)
	}
}

// TestGuardarSesionExpiradaEsNoOp verifica que guardar una sesión con expira_en
// en el pasado no escribe nada (no tiene sentido cachear una sesión muerta) y
// no devuelve error.
func TestGuardarSesionExpiradaEsNoOp(t *testing.T) {
	cliente := nuevoClienteDePrueba(t)
	cache := rediscache.New(cliente)
	ctx := context.Background()

	const sesionID = "22222222-2222-2222-2222-222222222222"
	expiraEn := time.Now().Add(-1 * time.Hour) // ya expiró

	if err := cache.GuardarSesion(ctx, sesionID, expiraEn); err != nil {
		t.Fatalf("GuardarSesion de sesión expirada no debería dar error: %v", err)
	}
	n, err := cliente.Exists(ctx, "sesion:"+sesionID).Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if n != 0 {
		t.Errorf("una sesión expirada no debería escribirse; existe=%d", n)
	}
}

// TestInvalidarInexistenteEsNoOp verifica que invalidar una sesión que nunca se
// cacheó no es error (DEL es idempotente).
func TestInvalidarInexistenteEsNoOp(t *testing.T) {
	cliente := nuevoClienteDePrueba(t)
	cache := rediscache.New(cliente)
	ctx := context.Background()

	if err := cache.InvalidarSesion(ctx, "no-existe"); err != nil {
		t.Errorf("InvalidarSesion de clave inexistente no debería dar error: %v", err)
	}
}

// TestPing verifica que Ping tiene éxito contra un Redis alcanzable.
func TestPing(t *testing.T) {
	cliente := nuevoClienteDePrueba(t)
	cache := rediscache.New(cliente)
	ctx := context.Background()

	if err := cache.Ping(ctx); err != nil {
		t.Errorf("Ping contra Redis alcanzable no debería fallar: %v", err)
	}
}
