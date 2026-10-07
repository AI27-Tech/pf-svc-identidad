// Package identidad es el módulo de servicio `identidad` de AI27: dueño del
// modelo de autorización (ocho roles de DEC-045/053 y las dos dimensiones de
// alcance, resueltas en servidor) y del ciclo de vida de sesión observable
// (ADR 26), sobre un IdP on-premise que emite y firma los tokens (DEC-062;
// propuesta ADR 32: Keycloak).
//
// Superficie pública = las cuatro piezas de 00-indice.md: New, Routes, Workers,
// Consultas. `New` NO lee entorno: recibe pools y dependencias ya resueltas.
//
// `identidad` es HOJA del grafo (reglas.yaml: `identidad: []`): no tiene aristas
// salientes hacia AI27. El IdP on-premise es dependencia EXTERNA, no una arista
// (no aparece en reglas.yaml). Por eso ports.go está vacío (ver ports.go).
package identidad

import (
	"net/http"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
	"github.com/AI27-Tech/pf-pkg-platform-go/bus"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"
	"github.com/AI27-Tech/pf-pkg-platform-go/pgxpool"
	"github.com/AI27-Tech/pf-pkg-platform-go/worker"

	svchttp "github.com/AI27-Tech/pf-svc-identidad/internal/http"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// Pools agrupa los pools de BD que el módulo recibe, uno por rol (invariante I1
// de ADR 17). `identidad` usa un único rol: `svc_identidad`, con USAGE sólo
// sobre el esquema `identidad` (identidad.md (e), §A.5 R1).
type Pools struct {
	// Identidad es el pool del rol `svc_identidad`.
	Identidad *pgxpool.Pool
}

// Deps agrupa las dependencias ya resueltas que `New` recibe. No incluye
// configuración de entorno: la raíz de composición las construye y las inyecta.
type Deps struct {
	// IDP es el proveedor de identidad detrás de su adaptador aislado. En
	// desarrollo es el doble en memoria (internal/idp/memory); en producción, el
	// adaptador del IdP on-premide, EN ESPERA de ADR 32. El módulo nunca lo
	// construye ni conoce su producto: lo recibe ya resuelto.
	IDP idp.IdentityProvider

	// BusPublisher es el publicador del bus que usa el Relay de outbox para
	// drenar `sesion.revocada`. Agnóstico al transporte (bus/memory en
	// desarrollo; broker en espera de ADR 32).
	BusPublisher bus.Publisher

	// Cache es la caché caliente de sesión en Redis/Valkey. Opcional: NUNCA es
	// fuente de verdad (identidad.md (e)); si es nil, el módulo opera sólo
	// contra Postgres.
	Cache sesion.Cache
}

// Module es la instancia del módulo construida por New.
type Module struct {
	pools    Pools
	handlers *svchttp.Handlers
	svc      sesion.Servicio
	relay    worker.Worker
}

// New construye el módulo. NO lee entorno (00-indice.md, I5): recibe pools y
// dependencias ya resueltas. Cablea el servicio de sesión (que controla la
// frontera transaccional del paso 1 de la revocación) y el Relay de outbox.
func New(p Pools, d Deps) (*Module, error) {
	publisher := outbox.NewPublisher()

	// El servicio de sesión controla el commit del paso 1 (local + evento en
	// una sola tx). Recibe el IdP aislado, la frontera transaccional (el pool),
	// el publicador de outbox y la caché.
	svc := sesion.NewServicio(d.IDP, p.Identidad, publisher, d.Cache)

	handlers := svchttp.NewHandlers(svc)

	// Worker de outbox (ADR 29): drena la tabla `outbox` del esquema `identidad`
	// con el pool y rol de ESTE módulo. No hay consumidores (identidad consume
	// cero eventos, identidad.md (g)).
	relay := outbox.NewRelay("outbox-relay:identidad", p.Identidad, d.BusPublisher, outbox.RelayOpciones{})

	return &Module{
		pools:    p,
		handlers: handlers,
		svc:      svc,
		relay:    relay,
	}, nil
}

// Routes monta las rutas HTTP del módulo bajo /api/v1/... en el mux dado.
//
// `emitirSesion` y `refrescarSesion` son públicas (identidad.yaml: aún no hay
// token). Las otras tres van protegidas por el esquema bearer del contrato
// (hoy llamado `bearerCognito` en _comunes.yaml — nombre reportado como brecha
// neutral de proveedor). El verificador JWT se consume de
// pf-pkg-platform-go/auth; NO se reimplementa (restricción del scaffold).
func (m *Module) Routes(mux *http.ServeMux, verificador auth.Authenticator, rev auth.RevocationChecker) {
	// Públicas.
	mux.HandleFunc("POST /api/v1/sesiones", m.handlers.EmitirSesion)
	mux.HandleFunc("POST /api/v1/sesiones/refresh", m.handlers.RefrescarSesion)

	// Protegidas con el middleware de auth de la plataforma.
	protegido := auth.Middleware(verificador, rev)
	mux.Handle("DELETE /api/v1/sesiones/{id}", protegido(http.HandlerFunc(m.handlers.RevocarSesion)))
	mux.Handle("GET /api/v1/sesiones/revocadas", protegido(http.HandlerFunc(m.handlers.ListarSesionesRevocadas)))
	mux.Handle("GET /api/v1/sesiones/{id}", protegido(http.HandlerFunc(m.handlers.ObtenerSesion)))
}

// Workers devuelve los workers embebidos del módulo (ADR 29 §6): sólo el Relay
// de outbox de `identidad`, con su propio pool y rol. No hay consumidores: la
// raíz decide arrancarlos dentro de un worker.RunGroup.
func (m *Module) Workers() []worker.Worker {
	return []worker.Worker{m.relay}
}

// Consultas expone los casos de uso que `bff` consume en proceso (arista
// `bff → identidad` declarada en 00-indice.md). Tipados con la superficie de
// sesión; nunca con tipos de internal/.
//
// SCAFFOLD: la forma exacta la fija el puerto que `bff` declare (05-bff.md:
// login, refresh, logout, bff.md (a)). Se expone el servicio de sesión, que ya
// cubre esos casos de uso; cuando 05-bff.md fije el puerto consumidor, se
// recorta a esos métodos tipados con api-client-go/identidad.
func (m *Module) Consultas() ConsultasAPI {
	return ConsultasAPI{sesion: m.svc}
}

// ConsultasAPI es la fachada en proceso para `bff`. Expone sólo lo que la
// arista declarada necesita (login/refresh/logout), sin filtrar tipos de
// internal/ hacia fuera.
type ConsultasAPI struct {
	sesion sesion.Servicio
}
