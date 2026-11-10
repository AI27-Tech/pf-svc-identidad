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
	"errors"
	"net/http"

	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
	"github.com/AI27-Tech/pf-pkg-platform-go/bus"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"
	"github.com/AI27-Tech/pf-pkg-platform-go/pgxpool"
	"github.com/AI27-Tech/pf-pkg-platform-go/worker"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion/postgres"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
	gobpostgres "github.com/AI27-Tech/pf-svc-identidad/internal/gobierno/postgres"
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

	// IDPAdmin administra usuarios y grupos en el IdP (gobierno de E4-T6). En
	// desarrollo es el mismo doble en memoria que IDP; en producción, el
	// adaptador de administración de Keycloak con su cuenta de servicio separada
	// (tarea 11, C4/G-4). Opcional: si es nil, no se montan las rutas HTTP de
	// gobierno y, en la fachada en proceso, las operaciones que tocan el IdP
	// responden 503; las lecturas, la revocación y los tenants siguen.
	IDPAdmin idp.IdpAdmin

	// TokenParser lee los claims `sub` y `sesion_id` de un access token emitido
	// por el IdP. Es la costura que activa el LOGIN de E4-T3 (emisión + refresco):
	// el flujo de emisión resuelve el sujeto por el `sub`, y el refresco resuelve
	// la fila por el `sesion_id` del token nuevo (A-4, A-5). En producción se
	// construye sobre pf-pkg-platform-go/auth (verificando contra el JWKS). Si es
	// nil, EmitirSesion y RefrescarSesion devuelven 501 (comportamiento de la
	// Parte A): el servicio sigue revocando y leyendo estado, pero no emite.
	TokenParser TokenParser
}

// TokenParser extrae los claims que el login de E4-T3 necesita de un access
// token del IdP. Lo implementa la raíz de composición sobre
// pf-pkg-platform-go/auth. Mantener la costura aquí evita acoplar el módulo y el
// paquete sesion al verificador concreto.
type TokenParser interface {
	// SubDelAccessToken devuelve el claim `sub` (id opaco del IdP) del token.
	SubDelAccessToken(accessToken string) (string, error)
	// SesionIDDelAccessToken devuelve el claim `sesion_id` (propio de AI27) del token.
	SesionIDDelAccessToken(accessToken string) (string, error)
}

// Module es la instancia del módulo construida por New.
type Module struct {
	pools    Pools
	handlers *svchttp.Handlers
	svc      sesion.Servicio
	relay    worker.Worker

	// gobiernoEnProceso es la sub-fachada de gobierno para core (Gobierno()).
	// Existe aunque no haya IDPAdmin; ver GobiernoConsultas.
	gobiernoEnProceso GobiernoConsultas
	// gobhandlers son los handlers HTTP de gobierno (E4-T6, tracer.11). nil si
	// no se cableó el servicio de gobierno (sin IDPAdmin).
	gobhandlers *svchttp.GobiernoHandlers
	// resolverSujeto correlaciona el `sub` del IdP con el SujetoID de AI27
	// (DEC-072). Lo consume el login de E4-T3 cuando se cablee; aquí solo se
	// construye y queda disponible.
	resolverSujeto *gobierno.ResolverSujeto
}

// New construye el módulo. NO lee entorno (00-indice.md, I5): recibe pools y
// dependencias ya resueltas. Cablea el servicio de sesión (que controla la
// frontera transaccional del paso 1 de la revocación) y el Relay de outbox.
func New(p Pools, d Deps) (*Module, error) {
	// Fail-fast: `Deps.IDP` es obligatorio. Sin él, el Paso 2 de la revocación
	// (el corte en el IdP, ADR 26 (h)) entraría en panic en caliente, DESPUÉS de
	// comprometer el Paso 1 local. Fallar aquí, al arrancar, obliga a la raíz de
	// composición a inyectar un proveedor (el doble en memoria en desarrollo, el
	// adaptador real en producción) antes de servir tráfico, en vez de descubrir
	// el nil a mitad de una revocación (B2 de E4-T1).
	if d.IDP == nil {
		return nil, errors.New("identidad: Deps.IDP es obligatorio (la revocación en dos pasos de ADR 26 lo exige); la raíz de composición debe inyectar un idp.IdentityProvider")
	}

	publisher := outbox.NewPublisher()

	// Repos de sujeto, usados tanto por el gobierno como por el resolver de
	// sujeto y la resolución del actor de la auditoría.
	sujetoRepo := postgres.NewSujetoRepo(p.Identidad)

	// El servicio de sesión controla el commit del paso 1 (local + evento en
	// una sola tx). Recibe el IdP aislado, la frontera transaccional (el pool),
	// el publicador de outbox y la caché.
	//
	// Las opciones de la Parte B (E4-T3) se cablean según DEC-088:
	//   - ExpiryPolicy (A-3 b): expiración actualizada en cada refresco.
	//   - IdpRevokeFailureHandler (A-7): reintento local del Paso 2, 503 si agota.
	//   - RevocationLog (A-10 b): cursor por timestamp (por omisión en NewServicio).
	//   - AuditRecorder (A-12/A-5/A-16): auditoría de revocación en cambio_identidad.
	//   - EmissionFlow + SujetoResolver + Refresh (A-1/A-5/A-4): LOGIN, solo si
	//     hay TokenParser (la raíz lo construye sobre el verificador JWT).
	opts := []sesion.Option{
		sesion.WithExpiryPolicy(sesion.NewExpiryAtRefresh()),
		sesion.WithIdpRevokeFailureHandler(sesion.NewIdpRevokeRetrier(d.IDP)),
	}

	// resolverSujeto se construye siempre; su costura subDeToken queda nil hasta
	// que haya TokenParser. Lo consume el login y la auditoría del actor.
	var resolver *gobierno.ResolverSujeto

	if d.TokenParser != nil {
		// subDeToken: el `sub` del access token (A-5). El resolver mapea sub →
		// SujetoID+TenantID consultando el sujeto por idp_subject.
		subDeToken := func(tok idp.Token) (string, error) {
			return d.TokenParser.SubDelAccessToken(tok.AccessToken)
		}
		resolver = gobierno.NewResolverSujeto(sujetoRepo, subDeToken)

		// Refresco (A-4 a): el sesion_id del claim del access token nuevo.
		sesionIDDeToken := func(tok idp.Token) (string, error) {
			return d.TokenParser.SesionIDDelAccessToken(tok.AccessToken)
		}

		opts = append(opts,
			sesion.WithSujetoResolver(resolver),
			sesion.WithEmissionFlow(sesion.NewCompensatingEmissionFlow()),
			sesion.WithRefresh(sesion.NewRefreshResolver(sesionIDDeToken), sesion.RevokedRefreshReject),
		)
	} else {
		resolver = gobierno.NewResolverSujeto(sujetoRepo, nil)
	}

	// Auditoría de revocación (A-12): el actor sale del contexto. La resolución
	// sub → sujeto.id reusa el resolver de sujeto; si no hay TokenParser, la
	// auditoría se escribe con actor desconocido.
	opts = append(opts, sesion.WithAuditRecorder(sesion.NewAuditRecorder(nil)))

	svc := sesion.NewServicio(d.IDP, p.Identidad, publisher, d.Cache, opts...)

	handlers := svchttp.NewHandlers(svc)

	// Worker de outbox (ADR 29): drena la tabla `outbox` del esquema `identidad`
	// con el pool y rol de ESTE módulo. No hay consumidores (identidad consume
	// cero eventos, identidad.md (g)).
	relay := outbox.NewRelay("outbox-relay:identidad", p.Identidad, d.BusPublisher, outbox.RelayOpciones{})

	m := &Module{
		pools:    p,
		handlers: handlers,
		svc:      svc,
		relay:    relay,
	}

	// El resolver de sujeto (DEC-072) ya se construyó arriba (con o sin
	// subDeToken según haya TokenParser). Lo consume el login de E4-T3.
	m.resolverSujeto = resolver

	// Gobierno de identidad (E4-T6, E8-T19). Los repos de autorización y
	// gobierno se construyen sobre el pool del módulo (aceptan un Querier). El
	// servicio de sesión es el revocador del gobierno: corta la sesión viva al
	// desactivar (G-6) y en la revocación administrativa con motivo (B-5).
	//
	// El servicio se construye siempre, también sin IDPAdmin: así la fachada en
	// proceso (Consultas().Gobierno()) existe en core aunque core todavía no
	// cablee la cuenta de servicio de Keycloak. Sin IDPAdmin, las operaciones
	// que tocan el IdP responden 503 (ErrIdpAdminNoCableado) antes de escribir.
	gob := gobierno.NewServicio(
		p.Identidad,
		publisher,
		d.IDPAdmin,
		sujetoRepo,
		postgres.NewAsignacionRolRepo(p.Identidad),
		gobpostgres.NewTrazaRepo(p.Identidad),
		svc,
		gobpostgres.NewAlcanceRepo(p.Identidad),
	)
	tenants := gobierno.NewTenants(gobpostgres.NewTenantRepo(p.Identidad))
	m.gobiernoEnProceso = GobiernoConsultas{gob: gob, consulta: gob, tenants: tenants, actores: sujetoRepo}

	// Las rutas HTTP de gobierno (tracer.11, tracer.40) se montan solo con
	// IDPAdmin, como hasta ahora. El actor se resuelve en servidor por
	// `idp_subject` con el repo de sujetos (DEC-072).
	if d.IDPAdmin != nil {
		m.gobhandlers = svchttp.NewGobiernoHandlers(gob, gob, tenants, sujetoRepo)
	}

	return m, nil
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

	// Gobierno de identidad (E4-T6, tracer.11). Las seis operaciones van
	// protegidas y restringidas a `administrador` (G-10); la restricción por rol
	// la aplica cada handler desde los claims. Solo se montan si el servicio de
	// gobierno se cableó (IDPAdmin presente).
	if m.gobhandlers != nil {
		mux.Handle("GET /api/v1/sujetos", protegido(http.HandlerFunc(m.gobhandlers.ListarSujetos)))
		mux.Handle("POST /api/v1/sujetos", protegido(http.HandlerFunc(m.gobhandlers.AltaSujeto)))
		mux.Handle("GET /api/v1/sujetos/{id}", protegido(http.HandlerFunc(m.gobhandlers.ObtenerSujeto)))
		mux.Handle("PATCH /api/v1/sujetos/{id}", protegido(http.HandlerFunc(m.gobhandlers.EditarSujeto)))
		mux.Handle("DELETE /api/v1/sujetos/{id}", protegido(http.HandlerFunc(m.gobhandlers.DesactivarSujeto)))
		mux.Handle("POST /api/v1/sujetos/{id}/revocaciones", protegido(http.HandlerFunc(m.gobhandlers.RevocarSesionesSujeto)))
		mux.Handle("POST /api/v1/sujetos/{id}/reactivacion", protegido(http.HandlerFunc(m.gobhandlers.ReactivarSujeto)))
		mux.Handle("GET /api/v1/sujetos/{id}/historial", protegido(http.HandlerFunc(m.gobhandlers.ListarHistorialSujeto)))
		mux.Handle("PUT /api/v1/sujetos/{id}/rol", protegido(http.HandlerFunc(m.gobhandlers.FijarRolSujeto)))
		mux.Handle("GET /api/v1/tenants", protegido(http.HandlerFunc(m.gobhandlers.ListarTenants)))
		mux.Handle("PUT /api/v1/tenants/{id}", protegido(http.HandlerFunc(m.gobhandlers.CrearTenant)))
		mux.Handle("DELETE /api/v1/tenants/{id}", protegido(http.HandlerFunc(m.gobhandlers.DesactivarTenant)))
	}
}

// Workers devuelve los workers embebidos del módulo (ADR 29 §6): sólo el Relay
// de outbox de `identidad`, con su propio pool y rol. No hay consumidores: la
// raíz decide arrancarlos dentro de un worker.RunGroup.
func (m *Module) Workers() []worker.Worker {
	return []worker.Worker{m.relay}
}

// Consultas expone las aristas síncronas entrantes que los módulos co-ubicados
// consumen en proceso. identidad es destino de dos aristas con contratos
// distintos: la de bff (superficie de sesión y de gobierno, tipadas con el
// contrato) y la restringida a revocación de operacion/catalogos/reportes
// (DEC-051). La fachada las separa en Sesion(), Gobierno() y Revocacion(); ver
// consultas.go y consultas_gobierno.go.
//
// La forma la fijan los puertos que ya declararon los consumidores (bff.IdentidadPort
// y los RevocacionPort de operacion/catalogos/reportes), tipados con
// api-client-go/identidad y con primitivos respectivamente; nunca con tipos de
// internal/.
func (m *Module) Consultas() ConsultasAPI {
	return ConsultasAPI{svc: m.svc, gobierno: m.gobiernoEnProceso}
}
