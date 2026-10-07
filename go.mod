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
	github.com/AI27-Tech/pf-pkg-platform-go v0.1.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/oapi-codegen/runtime v1.7.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
