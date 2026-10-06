# CHANGELOG

## 2026:10:06 17:45

**Resumen:** Scaffolding inicial del módulo `identidad` (`pf-svc-identidad`) según `03-identidad.md`: superficie pública, puerto del IdP aislado, modelo de autorización de los ocho roles, composición de claims, ciclo de vida de sesión con revocación en dos pasos, esquema de base de datos y arranque standalone. Solo estructura y costuras; sin lógica de negocio real.

**Cambios:**
- Se agregó `go.mod` fijando `pf-pkg-contracts/packages/api-client-go` `v0.1.0-tracer.3` y `pf-pkg-platform-go` `v0.1.0` por versión exacta, sin `replace`.
- Se agregó `identidad.go` con la superficie pública (`New`, `Routes`, `Workers`, `Consultas`) de `00-indice.md`; `New` no lee entorno. `ports.go` se dejó vacío (es hoja del grafo).
- Se agregó `internal/idp/` (puerto `IdentityProvider` aislado, doble en memoria, suite de contrato `idptest`, adaptador real EN ESPERA de ADR 32).
- Se agregó `internal/autorizacion/` (ocho roles reexportados del contrato, dos dimensiones de alcance resueltas en servidor, derivación de `Audiencia`, `TODO(DEC-045)` del vínculo Monitorista↔unidades).
- Se agregó `internal/claims/` (composición de claims como función pura, sin alcance; igual en emisión y refresco) con pruebas.
- Se agregó `internal/sesion/` (ciclo de vida y revocación en dos pasos: local+outbox en una transacción, luego IdP; Redis nunca es fuente de verdad).
- Se agregó `internal/gobierno/` (puerto vacío, `TODO(contrato)`: `/sujetos`, `/roles` y sus eventos no existen en el contrato).
- Se agregó `internal/http/` (handlers de las cinco operaciones de sesiones con los tipos generados).
- Se agregaron `migrations/identidad/0001_init.sql` (esquema `identidad`, rol `svc_identidad`, tablas `sujeto`, `rol`/`asignacion_rol`, `alcance_tenant`, `sesion` con el parcial `una_sesion_viva`, `cambio_identidad`, `outbox`) y `migrations/migrations.go` (`embed.FS`).
- Se agregó `cmd/api/main.go` (arranque standalone con `/health` y `/ready`, I5) y `adapters/http/README.md`.
- Se agregó `.github/workflows/ci.yml` (gofmt, build, vet, test, build de `cmd/api`, arch-test) y `README.md`.
- Se agregó `REPORTE-BRECHAS.md`: brechas de `/sujetos`, `/roles` y sus eventos; nombre `bearerCognito`; alcance fuera del token; y un **bloqueo del CI** por `arch-test`, que marca el import correcto de `api-client-go/roles` porque el tool no honra `shared_packages` (brecha de `pf-pkg-platform-go`, a resolver en `01-cimientos`).

**Archivos modificados:**
- `.gitignore`
- `go.mod`
- `identidad.go`
- `ports.go`
- `internal/idp/port.go`
- `internal/idp/memory/memory.go`
- `internal/idp/memory/memory_test.go`
- `internal/idp/idptest/idptest.go`
- `internal/idp/internal/doc.go`
- `internal/autorizacion/port.go`
- `internal/claims/claims.go`
- `internal/claims/claims_test.go`
- `internal/sesion/port.go`
- `internal/sesion/deps.go`
- `internal/sesion/servicio.go`
- `internal/gobierno/port.go`
- `internal/http/handlers.go`
- `migrations/identidad/0001_init.sql`
- `migrations/migrations.go`
- `cmd/api/main.go`
- `adapters/http/README.md`
- `.github/workflows/ci.yml`
- `README.md`
- `REPORTE-BRECHAS.md`

## 2026:10:06 12:55

**Resumen:** Se agregó la plantilla de Pull Request del backend, adaptada al stack Go de AI27.

**Cambios:**
- Se agregó `.github/pull_request_template.md` con secciones de invariantes de arquitectura (I1-I5, contrato primero), base de datos, eventos/bus, verificación (go build/vet/test, arch-test, gofmt), CHANGELOG y checklist de convenciones.

**Archivos modificados:**
- `.github/pull_request_template.md`

## 2026:10:06 12:48

**Resumen:** Se agregó la regla de steering de CHANGELOG y este archivo, antes de que el repo tenga código, para que la convención esté vigente desde el inicio.

**Cambios:**
- Se agregó `.kiro/steering/changelog.md` (`inclusion: always`): toda modificación del repo debe registrarse aquí, en español, con marca de tiempo, resumen, cambios y archivos modificados.
- Se creó `CHANGELOG.md`.

**Archivos modificados:**
- `.kiro/steering/changelog.md`
- `CHANGELOG.md`
