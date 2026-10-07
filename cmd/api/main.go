// Command api arranca `identidad` como servicio STANDALONE (invariante I5 de
// ADR 17): compila y sirve /health y /ready SIN `core`. Producción no usa este
// binario para los módulos de `core`; existe para desarrollo y para que el
// módulo demuestre que arranca por sí solo.
//
// A diferencia de New (que NO lee entorno), este cmd SÍ lee configuración del
// entorno: es el borde del proceso. Para desarrollo cablea los dobles en
// memoria (IdP y bus), ambos EN ESPERA de que ADR 32 elija los componentes
// reales. /ready verifica que Postgres sea alcanzable (y Redis cuando se
// cablee la caché).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
	"github.com/AI27-Tech/pf-pkg-platform-go/bus"
	busmemory "github.com/AI27-Tech/pf-pkg-platform-go/bus/memory"
	"github.com/AI27-Tech/pf-pkg-platform-go/health"
	"github.com/AI27-Tech/pf-pkg-platform-go/pgxpool"
	"github.com/AI27-Tech/pf-pkg-platform-go/worker"

	identidad "github.com/AI27-Tech/pf-svc-identidad"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("identidad/cmd/api: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Configuración del entorno (sólo en el borde del proceso) ------------
	addr := getenv("IDENTIDAD_ADDR", ":8080")
	dsn := os.Getenv("IDENTIDAD_DATABASE_URL") // rol svc_identidad
	jwksURL := os.Getenv("IDENTIDAD_JWKS_URL")
	audiencia := getenv("IDENTIDAD_AUDIENCIA", "ai27-internal")

	// --- Pool del rol dueño --------------------------------------------------
	pool, err := pgxpool.New(ctx, "svc_identidad", dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// --- Dobles en memoria para desarrollo (EN ESPERA de ADR 32) -------------
	// El IdP real (internal/idp/internal) y el broker real esperan a que ADR 32
	// elija componente. En standalone se usan los dobles para poder arrancar.
	idpDoble := memory.New()
	busDoble := busmemory.Nuevo(registroPermisivo{}, busmemory.Opciones{})

	// --- Construcción del módulo (New NO lee entorno) ------------------------
	mod, err := identidad.New(
		identidad.Pools{Identidad: pool},
		identidad.Deps{
			IDP:          idpDoble,
			BusPublisher: busDoble,
			Cache:        nil, // sin Redis en standalone; nunca es fuente de verdad
		},
	)
	if err != nil {
		return err
	}

	// --- Verificador JWT (se CONSUME de platform-go, no se reimplementa) ------
	// En standalone sin JWKS configurado, el verificador se crea igual; las
	// rutas protegidas responderán 401 hasta que haya un JWKS alcanzable.
	verificador := auth.NewVerifier(jwksURL, audiencia, 10*time.Minute)
	verificador.IniciarRefrescoProactivo(ctx)

	// --- Mux: health/ready + rutas del módulo --------------------------------
	reg := health.NewRegistry()
	reg.RegisterCheck("postgres:svc_identidad", pool.CheckFunc())
	// TODO: cuando se cablee la caché, registrar aquí el check de Redis.

	mux := http.NewServeMux()
	health.RegisterRoutes(mux, reg)
	mod.Routes(mux, verificador, nil)

	reg.SetReady(true)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Arrancar los workers del módulo (Relay de outbox) junto al servidor HTTP.
	go func() {
		if err := worker.RunGroup(ctx, reg, mod.Workers()...); err != nil {
			log.Printf("identidad/cmd/api: workers: %v", err)
			stop()
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("identidad/cmd/api escuchando en %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// registroPermisivo es el Registry que el bus en memoria exige. En standalone
// acepta todo y ordena por agregado. El registro REAL se genera desde el
// contrato (C5) y lo inyecta la raíz de composición; aquí sólo basta para
// arrancar el doble.
type registroPermisivo struct{}

func (registroPermisivo) Allowed(string, int) bool       { return true }
func (registroPermisivo) OrderingKey(s bus.Sobre) string { return s.AgregadoID }
