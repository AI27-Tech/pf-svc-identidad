# Reporte de brechas contractuales y de implementación — `identidad`

> Estado verificado: 2026-10-08 (post-E4-T2: modelo de autorización completado).
>
> Este reporte distingue las brechas que siguen abiertas de los elementos que ya
> están resueltos. No convierte propuestas ni pendientes de decisión en reglas
> de producto.
>
> **Versiones verificadas:**
> - `github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go` `v0.1.0-tracer.3`
> - `github.com/AI27-Tech/pf-pkg-platform-go` `v0.1.1`
>
> **Validación ejecutada:** `go mod verify`, `go build ./...`, `go vet ./...`,
> `go test ./...`, `go build ./cmd/api` y
> `go run github.com/AI27-Tech/pf-pkg-platform-go/tools/arch-test@v0.1.1 .`.
> Todas pasan en un checkout aislado del `go.work`.

## 1. Brechas contractuales abiertas

Estas brechas no se resuelven desde `pf-svc-identidad`: requieren ampliar y
publicar `pf-pkg-contracts` antes de cablear las superficies correspondientes.

| Superficie o capacidad | Fuente | Estado actual | Impacto en este servicio |
|---|---|---|---|
| `POST/DELETE /api/v1/sujetos/...` para alta y baja | `identidad.md` (f), superficie administrativa | **FALTA** en `api-client-go/identidad` | `internal/gobierno/port.go` permanece vacío; no se montan rutas ni casos de uso. |
| `/api/v1/roles/...` para asignación y revocación de roles con alcance | `identidad.md` (f), DEC-045 #8 | **FALTA** en el contrato | No se implementan rutas ni comandos de gobierno. |
| Eventos `sujeto.creado`, `sujeto.desactivado`, `rol.asignado` y `rol.revocado` | `identidad.md` (g); proyección de `mensajeria` | **FALTA** en `.proto`, `INVENTARIO.md` y `registro.tipos.yaml` | No se publican esos eventos ni se agregan filas de outbox para ellos. |
| Lectura y cambios del alcance por `unidad`/`cliente` | DEC-045 #8 y DEC-053 | **PENDIENTE**: el alcance no viaja en el token y la mecánica Monitorista↔`unidad` sigue sin decisión | `internal/autorizacion/port.go` conserva `TODO(DEC-045)`; no se modelan tablas, rutas ni eventos nuevos de alcance. |

### Efecto delimitado

El módulo sí contiene la forma inicial de `sujeto`, `rol`, `asignacion_rol`,
`alcance_tenant`, `sesion`, `cambio_identidad` y `outbox` en la migración, pero
la ausencia de contratos para administración y eventos impide exponer esos
casos de uso. No se deben crear tipos de wire, rutas ni eventos manualmente
para llenar esta brecha.

## 2. Brechas de nomenclatura abiertas

| Elemento | Estado actual | Acción |
|---|---|---|
| Esquema de seguridad `bearerCognito` | **DESALINEADO**: el contrato conserva ese identificador y el generado expone `identidad.BearerCognitoScopes`, aunque V1 no usa Cognito | Usarlo tal cual mientras exista en `pf-pkg-contracts`; corregirlo en el contrato cuando se defina una versión neutral. No renombrarlo desde este servicio. |

## 3. Brechas de implementación abiertas

### 3.1 Adaptador real del IdP — EN ESPERA DE ADR 32

El puerto `internal/idp/IdentityProvider`, el doble en memoria, la suite de
contrato y la composición de claims están **implementados y verificados en E4-T2**.

El adaptador concreto para el IdP on-premise aún no se implementa hasta que
ADR 32 seleccione el producto y la integración. En V1, `cmd/api` usa el doble
en memoria para desarrollo standalone.

No se importa ningún SDK de proveedor desde este módulo.

**Archivos implementados:**
- `internal/idp/port.go` — Puerto `IdentityProvider`
- `internal/idp/memory/` — Doble en memoria
- `internal/idp/idptest/` — Suite de contrato
- `internal/autorizacion/model.go` — `Sujeto`, `ModeloVigente` (listo para recibir)
- `internal/autorizacion/postgres/` — Persistencia de modelo (Decisiones A/B confirmadas)

### 3.2 Composición de claims del IdP — ✅ IMPLEMENTADO EN E4-T2

La composición de claims de AI27 está **implementada, testeada y verificada**:
- Función pura `internal/claims.Componer(ModeloVigente) Compuestos`
- 6 property tests (P1, P3, P5) + 3 unit tests de ejemplo (≥128 iteraciones c/u)
- Determinismo garantizado: `{SujetoID, TenantID, Rol, Audiencia}` sin alcance
- Derivación de `Audiencia` vía `autorizacion.AudienciaDeRol` (total sobre enum de 8 roles)

Falta: Integración con el **IdP real** cuando ADR 32 se resuelva. En V1, el
modo standalone usa el doble en memoria (`internal/idp/memory`).

**Archivos asociados:**
- `internal/claims/claims.go` — Composición pura
- `internal/claims/claims_test.go` — 6 tests + 3 properties
- `internal/autorizacion/port.go` — `AudienciaDeRol` (función determinista total)

### 3.3 Broker y despliegue on-premise — EN ESPERA DE ADR 32

El servicio conserva puertos agnósticos para publicación y usa el bus en memoria
en standalone. Falta seleccionar y empaquetar el broker real y el despliegue
correspondiente cuando ADR 32 cierre esas decisiones.

## 4. Elementos resueltos en E4-T2 (modelo de autorización)

### 4.1 Modelo de datos y puertos de persistencia

**Completado:**
- Tipos de dominio: `Sujeto`, `ModeloVigente`, `AsignacionRol` en `internal/autorizacion/model.go`
- Puertos: `SujetoRepo`, `AsignacionRolRepo`, `CargadorModeloVigente` en `internal/autorizacion/port.go`
- Errores de dominio: `ErrSujetoDuplicado`, `ErrAsignacionDuplicada`, `ErrRolFueraDeCatalogo`, `ErrRolNoUnico`
- Implementación: Repositorios sobre Postgres en `internal/autorizacion/postgres/`

**Decisiones A/B confirmadas (validación en código, no constraints en BD):**
- Decisión A: No agregar `UNIQUE(tenant_id, id)` en `sujeto` (criterio 1.3 cubierto por PK + `idp_subject` UNIQUE)
- Decisión B: Validación de `(sujeto_id, rol_codigo)` en código, no en BD (constraint final será `UNIQUE(..., alcance_id)` cuando DEC-045 resuelva)
- Documentado en: `internal/autorizacion/postgres/NOTAS.md`

### 4.2 Composición de claims — ✅ IMPLEMENTADO

**Completado:**
- Función pura `Componer(ModeloVigente) Compuestos` (determinista, sin alcance)
- 6 property tests: P1, P2, P3, P5 (≥128 iteraciones c/u)
- 3 unit tests de ejemplo + validación determinismo
- Mapeo rol → audiencia total sobre enum de 8 roles: `cliente → Audiencia.Cliente`, resto → `Audiencia.Interno`
- Marcador `AlcancePendiente` explícito (TODO(DEC-045))

**Archivos:**
- `internal/claims/claims.go`, `internal/claims/claims_test.go`
- `internal/autorizacion/port.go` — `AudienciaDeRol`, `DimensionDeRol`
- `internal/autorizacion/port_test.go` — Property 2, catálogo

### 4.3 Infraestructura de tests de integración

**Completado:**
- Testcontainers agregado a `go.mod` (v0.44.0)
- 3 tests de integración con BD Postgres dinámica (build tag: `integration`)
  - `TestAsignacionRolRepoDuplicadoEnCodigoRetornaError` (P4: Unicidad en código)
  - `TestAsignacionRolRepoRolFueraDeCatalogo` (P7: Validación catálogo)
  - `TestPropertyAislamientoEsquemaP1` (P6: Aislamiento DB)
- Manejo graceful: Si Docker no disponible, tests se skippean elegantemente
- Ejecución: `go test -tags=integration ./internal/autorizacion/postgres/...`

### 4.4 Verificación final

**Completado:**
- ✓ `go fmt` — Formateado
- ✓ `go vet` — Sin errores
- ✓ `go build ./internal/autorizacion/...`, `./internal/claims/...`, `./internal/idp/...` — Compilación limpia
- ✓ `go test ./internal/autorizacion ./internal/claims` — 32 tests OK, 0 regresiones
- ✓ Tests integración — 3 skippean (sin Docker) o ejecutan (con Docker)

---

## 5. Elementos resueltos que ya no son brechas

### 5.1 Metadata reproducible de Go

`go.mod` fija versiones exactas y `go.sum` está versionado. La dependencia de
platform Go quedó actualizada a `v0.1.1` y las sumas fueron regeneradas.

### 5.2 Acceso del CI a módulos privados

El workflow valida la presencia de `GH_PAT`, configura Git para resolver los
módulos privados `AI27-Tech/*` y declara `GOPRIVATE`.

El secreto debe existir en la configuración del repositorio de GitHub; su
configuración no forma parte de este código.

### 5.3 Validación de arquitectura

`pf-pkg-platform-go v0.1.1` corrige la detección del paquete compartido
`api-client-go/roles`. El workflow ejecuta `arch-test@v0.1.1` y la validación
actual devuelve:

```text
arch-test: OK
```

La antigua brecha de `arch-test` ya no bloquea este repositorio.

## 6. Estado de cierre

**Post-E4-T2 (2026-10-08):**

El scaffold compila, pasa sus validaciones técnicas y contiene **implementación completa del modelo de autorización**:
- ✅ Ola pura (composición claims + mapeos rol) — 9 tests probados (6 property + 3 unit)
- ✅ Ola persistencia (repositorios + validación código) — Decisiones A/B implementadas y confirmadas
- ✅ Ola integración (tests con testcontainers) — 3 tests dinámicos (P4, P7, P6)

**Cobertura actual:** 32 tests unitarios OK + 3 tests integración (skippean si no Docker) + 0 regresiones

Las brechas restantes son de contrato (E4-T6: gobierno + eventos), de decisión
de producto/arquitectura (ADR 32: IdP real, DEC-045: vínculo alcance) o de
integración con componentes todavía no seleccionados. No deben ocultarse
implementando superficies o tipos fuera de `pf-pkg-contracts`.

**Listo para:** Mergear E4-T2 → Comenzar E4-T6 (eventos + rutas de gobierno).
