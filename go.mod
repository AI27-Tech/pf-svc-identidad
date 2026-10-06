// Módulo de servicio `identidad` de AI27 (pf-svc-identidad).
//
// Depende SÓLO de pf-pkg-contracts (api-client-go) y pf-pkg-platform-go, ambos
// fijados por versión exacta (00-indice.md, regla 1 y 7). Sin `replace` hacia
// rutas locales: el trabajo en conjunto se hace con un `go.work` NO versionado
// en la carpeta contenedora `repos/`, que este repo nunca commitea.
//
// `identidad` es hoja del grafo (reglas.yaml: `identidad: []`): no importa el
// cliente generado de ningún otro `pf-svc-*`.
module github.com/AI27-Tech/pf-svc-identidad

go 1.27

require (
	github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go v0.1.0-tracer.3
	github.com/AI27-Tech/pf-pkg-platform-go v0.1.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
)
