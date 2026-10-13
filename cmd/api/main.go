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
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/keycloak"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp/memory"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion/rediscache"
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

	// --- Proveedor de identidad (se resuelve ANTES del pool para fallar
	// rápido, sin efectos de red ni de base de datos) -------------------------
	cfgIDP, err := cargarConfig(os.LookupEnv, os.ReadFile)
	if err != nil {
		return err
	}
	proveedor, probeIDP, err := construirIDP(cfgIDP)
	if err != nil {
		return err
	}
	log.Printf("identidad/cmd/api: proveedor de identidad: %s", cfgIDP.Seleccion)

	// --- Pool del rol dueño --------------------------------------------------
	pool, err := pgxpool.New(ctx, "svc_identidad", dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// --- Doble del bus para desarrollo (EN ESPERA de ADR 32) -----------------
	// El broker real espera a que ADR 32 elija componente. En standalone se usa
	// el doble para poder arrancar.
	busDoble := busmemory.Nuevo(registroPermisivo{}, busmemory.Opciones{})

	// --- Caché de sesión en Redis (A-11, DEC-088: Redis en V1) ---------------
	// Si IDENTIDAD_REDIS_URL está definida, se cablea el adaptador de Redis como
	// caché caliente. La caché NUNCA es fuente de verdad (ADR 26 §5): si la URL
	// no está, el servicio arranca con Cache: nil y lee todo de Postgres. Su
	// alcanzabilidad se expone en /ready/cache, no bloquea /ready de Postgres.
	var cache sesion.Cache
	var pingCache func(context.Context) error
	if redisURL := os.Getenv("IDENTIDAD_REDIS_URL"); redisURL != "" {
		rc, closeRedis, err := rediscache.NewFromURL(redisURL)
		if err != nil {
			return err
		}
		defer func() { _ = closeRedis() }()
		cache = rc
		pingCache = rc.Ping
		log.Printf("identidad/cmd/api: caché de sesión: redis (%s)", redisURL)
	} else {
		log.Printf("identidad/cmd/api: caché de sesión: ninguna (Cache: nil); lecturas van a Postgres")
	}

	// --- Construcción del módulo (New NO lee entorno) ------------------------
	mod, err := identidad.New(
		identidad.Pools{Identidad: pool},
		identidad.Deps{
			IDP:          proveedor,
			BusPublisher: busDoble,
			Cache:        cache,
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
	// /ready queda intacto: postgres:svc_identidad como único check (R9.5).
	reg := health.NewRegistry()
	reg.RegisterCheck("postgres:svc_identidad", pool.CheckFunc())

	mux := http.NewServeMux()
	health.RegisterRoutes(mux, reg)

	// Check de la caché de Redis en un registro aparte en /ready/cache: su caída
	// no bloquea /ready (la caché nunca es fuente de verdad, ADR 26 §5), pero
	// operación puede observar su estado. Solo se monta si Redis está cableado.
	if pingCache != nil {
		regCache := health.NewRegistry()
		regCache.RegisterCheck("cache:redis", health.CheckFunc(pingCache))
		mux.Handle("/ready/cache", health.ReadinessHandler(regCache))
		regCache.SetReady(true)
	}

	// Con Keycloak, un registro aparte en /ready/idp con el check idp:keycloak
	// (A-12 opción 1): su caída no bloquea /ready, y el proceso arranca aunque
	// Keycloak no sea alcanzable.
	if probeIDP != nil {
		regIDP := health.NewRegistry()
		regIDP.RegisterCheck("idp:keycloak", health.CheckFunc(probeIDP))
		mux.Handle("/ready/idp", health.ReadinessHandler(regIDP))
		regIDP.SetReady(true)
	}

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

// construirIDP crea el proveedor de identidad a partir de la configuración ya
// resuelta. Para Keycloak devuelve también la función de alcanzabilidad que se
// monta en /ready/idp; para el doble en memoria, el probe es nil.
func construirIDP(cfg configIDP) (idp.IdentityProvider, func(context.Context) error, error) {
	switch cfg.Seleccion {
	case idpKeycloak:
		prov, err := keycloak.New(cfg.Keycloak)
		if err != nil {
			return nil, nil, err
		}
		probe, err := keycloak.Probe(cfg.Keycloak)
		if err != nil {
			return nil, nil, err
		}
		return prov, probe, nil
	default:
		// Doble en memoria para desarrollo standalone (EN ESPERA de ADR 32).
		return memory.New(), nil, nil
	}
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
