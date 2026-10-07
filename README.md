# pf-svc-identidad

Módulo de servicio **`identidad`** de AI27. Es dueño del **modelo de autorización**
(los ocho roles de DEC-045 con los identificadores de DEC-053, y las dos
dimensiones de alcance —`unidad` y `cliente`— resueltas **en servidor**, DEC-053)
y del **ciclo de vida de sesión observable** (ADR 26), sobre un **IdP on-premise
con OIDC** que emite y firma los tokens (DEC-062; propuesta ADR 32: Keycloak).

`identidad` **no** es dueño de usuarios, credenciales, política de contraseña,
MFA ni firma: eso es del IdP. **No** decide autorización por petición; **emite
claims**. Es **hoja** del grafo de arquitectura (`reglas.yaml`: `identidad: []`):
no tiene aristas salientes hacia AI27.

Fuentes: [`docs/arquitectura/services/identidad.md`], [ADR 26] (revocación local),
[DEC-045] (ocho roles y dos dimensiones de alcance), [DEC-053] (alcance en
servidor), [DEC-054] (`Audiencia`), [DEC-031] (segundo factor del IdP; sin OTP
propio), [DEC-062] / [ADR 32] (on-premise, sin AWS ni Cognito).

## Estado

**Scaffolding inicial** (DEC-049: `identidad` avanza en paralelo a `catalogos` +
`operacion`). La estructura y las costuras están; la lógica de negocio real
todavía no. El adaptador concreto del IdP y el artefacto de composición de
claims están **EN ESPERA de ADR 32**.

## Superficie pública (`identidad.go`)

Las cuatro piezas de `00-indice.md`:

- `New(Pools, Deps) (*Module, error)` — construye el módulo. **No lee entorno**:
  recibe pools y dependencias ya resueltas (el IdP tras su puerto, el publicador
  del bus, la caché opcional).
- `Routes(mux, verificador, rev)` — monta las cinco operaciones de sesiones.
  `emitirSesion` y `refrescarSesion` son públicas; `revocarSesion`,
  `obtenerSesion` y `listarSesionesRevocadas` van protegidas con
  `pf-pkg-platform-go/auth` (el verificador JWT **se consume, no se
  reimplementa**).
- `Workers() []worker.Worker` — el Relay de `outbox` de `identidad` (ADR 29).
  Sin consumidores: `identidad` consume cero eventos.
- `Consultas() ConsultasAPI` — casos de uso que `bff` consume en proceso
  (arista `bff → identidad`).

## Estructura

```
identidad.go            New, Routes, Workers, Consultas
ports.go                vacío: identidad es hoja, sin aristas salientes
cmd/api/                arranque standalone (I5): /health, /ready
migrations/identidad/   0001_init.sql (esquema identidad, rol svc_identidad, tablas)
internal/
  idp/                  puerto IdentityProvider — ÚNICO punto que conoce al proveedor
    memory/             doble en memoria (pruebas y desarrollo)
    idptest/            suite de contrato del puerto
    internal/           adaptador real del IdP — EN ESPERA de ADR 32
  autorizacion/         ocho roles, dos dimensiones de alcance, derivación de Audiencia
  claims/               composición de claims (función pura; emisión Y refresco)
  sesion/               ciclo de vida de sesión; revocación en dos pasos (ADR 26)
  gobierno/             /sujetos y /roles — BRECHA contractual, puerto vacío
  http/                 handlers de las cinco operaciones de sesiones
```

## Sesión: dos superficies

`identidad.yaml` usa `/api/v1/sesiones` (plural): la colección administrable de
sesiones desde el servicio dueño. La superficie pública de `bff` usa
`/api/v1/sesion` (singular) para la sesión propia del llamante. Coexisten a
propósito.

## Revocación en dos pasos (ADR 26, identidad.md (h))

El orden es deliberado y está en el **código** (`internal/sesion/servicio.go`),
no sólo en un comentario:

1. **Local + evento en una sola transacción de Postgres**: se compromete la
   revocación en `identidad.sesion` y la fila `sesion.revocada` en `outbox`
   juntas (P17). Si falla, "no ocurrió".
2. **Revocar en el IdP** (idempotente). La revocación ya es efectiva en AI27 tras
   el paso 1; si el IdP falla, se reintenta.

`sesion.revocada` lleva payload mínimo (`sesion_id`, `revocada_en`, `expira_en`);
el motivo, sujeto, actor y claims quedan en `cambio_identidad` (auditoría local).

## Desarrollo local

Módulos privados del org, sin proxy público. Con el `go.work` **no versionado**
de `repos/`:

```sh
export GOPRIVATE='github.com/AI27-Tech/*'
export GOSUMDB=off
go build ./...
go test ./...
go run ./cmd/api   # requiere IDENTIDAD_DATABASE_URL (rol svc_identidad)
```

Variables de `cmd/api`: `IDENTIDAD_ADDR` (default `:8080`),
`IDENTIDAD_DATABASE_URL`, `IDENTIDAD_JWKS_URL`, `IDENTIDAD_AUDIENCIA`.

## Convenciones

Identificadores en inglés salvo sustantivos de dominio (`sesion`, `sujeto`,
`asignacion_rol`, `alcance_tenant`, `Audiencia`, `unidad`, `cliente`);
comentarios, docstrings, README y commits en español (México)
(`naming-conventions.md`).

## Brechas contractuales reportadas

Ver `REPORTE-BRECHAS.md`: faltan `/api/v1/sujetos`, `/api/v1/roles` y los eventos
`sujeto.*` / `rol.*` en `pf-pkg-contracts`; el esquema de seguridad aún se llama
`bearerCognito`; el alcance no viaja en el token (DEC-053) y falta su lectura/
eventos, que dependen de la mecánica pendiente de DEC-045.

[`docs/arquitectura/services/identidad.md`]: ../../docs/arquitectura/services/identidad.md
[ADR 26]: ../../docs/adr/0026-revocacion-local-de-sesiones.md
[ADR 32]: ../../docs/adr/0032-despliegue-v1-on-premise.md
[DEC-045]: ../../.kiro/steering/decision-log.md
[DEC-053]: ../../.kiro/steering/decision-log.md
[DEC-054]: ../../.kiro/steering/decision-log.md
[DEC-031]: ../../.kiro/steering/decision-log.md
[DEC-062]: ../../.kiro/steering/decision-log.md
