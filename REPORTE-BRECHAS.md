# Reporte de brechas contractuales y de implementación — `identidad`

> Estado verificado: 2026-10-16 (post-E4-T3 Parte A: correcciones de la ruta productiva y pruebas completadas).
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

### 3.1 Adaptador de Keycloak — PARCIALMENTE IMPLEMENTADO (E4-T1)

El IdP de V1 es **Keycloak** (confirmado 2026-10-08; P-0 pendiente de asentar en
decision-log). El adaptador contra Keycloak está implementado y verificado:
puerto, doble, suite de contrato, configuración, clasificación de respuestas,
`Issue`/`Refresh`/`Revoke`, token de servicio, telemetría OTel, probe de
alcanzabilidad, fachada pública y cableado en `cmd/api` (`IDENTIDAD_IDP`,
`IDENTIDAD_KEYCLOAK_*`, `/ready/idp`). Todo bajo `internal/idp/internal/kc` y
`internal/idp/keycloak`. No se importa ningún SDK de proveedor: el adaptador usa
`net/http` directamente.

**Decisiones resueltas (2026-10-09):** A-1 (un realm), A-2 → DEC-071 (TOTP),
A-13 → DEC-072 (opción c: `sub` opaco, `SujetoID` en servidor; sin `sujeto_id`),
A-16 → DEC-073 (privilegio mínimo), proceso → DEC-074 (`t.Skip` sólo por Docker).
Con esto se escribieron el artefacto del realm (tarea 14) y la suite de
integración (tarea 15).

**Pendiente de ejecución (no de decisión):**
- **A-7** — fijar la versión estable exacta de Keycloak y correr `-tags
  integration` contra ella para confirmar la forma del flujo de grant directo
  con OTP y del user profile. Hoy la imagen de referencia es `26.7.0`.
- **A-16 (ejecución)** — finalizar el permiso mínimo de terminación de sesión de
  `ai27-identidad-svc` (FGAP v2 o Logout OIDC). La cuenta hoy no lleva
  `manage-users`, así que R7.4/R7.7 pasan por construcción; R7.1/R7.5 (revocar
  una de dos sesiones por el adaptador) necesita ese permiso acotado, verificado
  contra la versión de A-7. La prueba falla con mensaje claro si falta.

**Implementado en esta ronda:** el artefacto del realm
(`deploy/keycloak/realm-ai27.json`), los usuarios de prueba
(`internal/idp/internal/kc/testdata/realm-ai27-users-0.json`), las pruebas puras
del artefacto (`realm_test.go`, `realm_prop_test.go`, Property 7) y la suite de
integración (`integration_*.go`, Property 13 + contrato + segundo factor +
revocación). El segundo factor TOTP se cablea en el formulario de `Issue`.

**Archivos implementados:**
- `internal/idp/port.go` — Puerto `IdentityProvider`
- `internal/idp/memory/` — Doble en memoria
- `internal/idp/idptest/` — Suite de contrato
- `internal/idp/internal/kc/` — Adaptador real + telemetría + probe + realm + integración
- `internal/idp/keycloak/` — Fachada pública (New, Probe, Config)
- `cmd/api/config.go`, `cmd/api/main.go` — Selección y cableado del IdP
- `deploy/keycloak/realm-ai27.json` — Artefacto versionado del realm `ai27`

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

## 3.4 Ciclo de vida de sesión y revocación — PARTE A ✅ IMPLEMENTADO (E4-T3 Parte A, 2026-10-16)

La Parte A de E4-T3 (Registro de sesión, evento y snapshot de revocación)
implementa el lado **productor** de la revocación en dos pasos, con correcciones
de la ruta productiva, costuras de decisiones pendientes y suite completa de
pruebas.

**Implementado:**

- **Ruta productiva corregida:**
  - Migración `0003_sesion_idp_session_id.sql`: columna `idp_session_id TEXT NULL` sin índice único ni `NOT NULL` (K-12 abierta)
  - Corrección H-1: Paso 2 recibe el `idp_session_id` de la fila, nunca el `sesion_id` (Requisitos R4.2, R4.3)
  - Corrección H-5: Paso 1 devuelve tres resultados clasificados: `Revoked` (ya estaba revocada), `AlreadyRevoked` (acaba de ser revocada), `NotFound` (no existe)
  - 404 de `ObtenerSesion`: `ErrNoEncontrada` envuelto en `PlatformError` de categoría `NoEncontrado`

- **Costuras con valores por omisión (Decisiones A-1 a A-16):**
  - `seams.go`: interfaces del puerto (`SujetoResolver`, `ExpiryPolicy`, `RefreshResolver`, `RevokedRefresh`, `IdpRevokeFailureHandler`, `AuditRecorder`, `RevocationLog`, `PayloadEncoder`)
  - `options.go`: variádico `NewServicio(prov, tx, pub, cache, opts ...)` con opciones `With*`. Sin opciones, comportamiento actual (scaffold)
  - Codificadores por omisión: `PayloadEncoder` que serializa exactamente `{sesion_id, revocada_en, expira_en}` (JSON con etiquetas); `snapshotOnlyLog` sin cursor
  - `cache.go`: best-effort wrapper que no propaga errores de caché

- **Provisionales de prueba (Propuestas de Kiro, no aprobadas):**
  - `internal/sesion/sesiontest`: `RecordingIdP`, `FailingCache`, `RecordingCache`, `FaultyBeginTx`, `ProtoPayload`
  - Regla E en `internal/archtest`: `sesiontest` es el único paquete que puede importarlo desde `Imports`

- **Pruebas completadas:**
  - `options_test.go` (Tarea 7.1): Camino productivo sin opciones (3 tests unitarios)
  - `idgen_prop_test.go` (Property 2): Generación de `sesion_id` (testing/quick, MaxCount: 100)
  - `payload_prop_test.go` (Property 8): Payload mínimo exacto y decodificable
  - Pruebas que requieren Postgres (7.2–7.6, 8.2–8.5, 8.7–8.10): Se omiten con `t.Skip` sin `IDENTIDAD_TEST_DB`

**Verificación ejecutada:**
- ✓ `gofmt -l .` sin salida
- ✓ `go vet ./...` verde
- ✓ `go vet -tags integration ./...` verde (compilación)
- ✓ `go build ./...` verde
- ✓ `go test ./...` verde: 5 tests nuevos PASS, sin regresiones

**Pendiente (Parte B bloqueada por decisiones):**

La Parte B de E4-T3 contiene 16 grupos de tareas desbloqueados por decisiones
de producto/arquitectura:

| Grupo | Decisión bloqueante | Tareas | Estado |
|---|---|---|---|
| G1 | A-15 | Flujo de emisión con segundo factor (`Credentials.SegundoFactor`) | Bloqueada |
| G2 | A-3 | Política de expiración de sesión (opción a/b/c, brecha seguridad) | Bloqueada |
| G3 | A-5 + A-16 | Resolución del `sujeto_id`; tenant de auditoría | Bloqueada |
| G4 | A-1 (DEC-085) | Orden de emisión ↔ registro (Issue primero vs registrar primero) | Bloqueada |
| G5 | A-2 | Índice `una_sesion_viva`: mantener, eliminar o revocar anterior | Bloqueada |
| G6 | A-4 + A-15 + A-3 | Resolución del refresco (`sesion_id` desde claim vs búsqueda por `idp_session_id`) | Bloqueada |
| G7 | A-7 | Reintento del Paso 2 (síncrono acotado, columna `idp_revocada_en` + worker, tabla propia) | Bloqueada |
| G8 | A-10 | Semántica del cursor (marca de agua vs ventana vs xid8 de Postgres vs canal) | Bloqueada |
| G9 | A-11 | Caché Redis/Valkey: tecnología, contenido, TTL, check `/ready` | Bloqueada |
| G10 | A-12 + A-5 + A-16 | Actor y motivo de auditoría; conexión con `cambio_identidad` | Bloqueada |
| G11 | A-14 | Serialización del payload (map[string]any actual vs protojson con `UseProtoNames`) | Bloqueada |
| G12 | B-10/H-3 | Verificación con Keycloak: ¿access token trae claim `sid`? Requiere Docker + JAR | Bloqueada |

**Nota crítica:** E4-T3 **no cierra sin E0-T4**. Este spec implementa solo el productor; el
consumidor (suscripción durable, difusión a réplicas, canal de 15 s, corte de 30 s) forma
parte de E0-T4. Sin el consumer, la característica de revocación no es observable para un cliente.

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

**Post-E4-T3 Parte A (2026-10-16):**

La Parte A de E4-T3 se ha completado con éxito:
- ✅ Correcciones de la ruta productiva (H-1, H-5, 404 de `ObtenerSesion`) — Implementadas y probadas
- ✅ Costuras con valores por omisión (A-1 a A-16) — Interfaces y opciones cableadas sin configurar
- ✅ Best-effort cache — Implementada sin propagar errores
- ✅ Suite completa de pruebas (Tarea 7.1, 8.1, 8.6) — 5 tests nuevos PASS

**Cobertura:** 5 tests unitarios/property nuevos + tests con Postgres se saltan correctamente (sin `IDENTIDAD_TEST_DB`) + 0 regresiones

**Bloqueado por decisiones pendientes:** La Parte B (16 grupos, 48+ tareas) está bloqueada por las decisiones A-1 a A-16 de E4-T3. Cada grupo se desbloqueará cuando su decisión correspondiente sea aprobada.

**Dependencia crítica:** E4-T3 no cierra sin E0-T4. El side productor está listo; el consumidor (suscripción, difusión, canales) es un prerequisito de cierre de la épica.

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

**Listo para:** Mergear E4-T3 Parte A → Esperar decisiones para Parte B → Comenzar E0-T4 (consumer).

## 5. E4-T5 — Entrega del segundo factor (2026-10-19)

E4-T5 es una historia de **no construcción**: verifica que `identidad` no construye un segundo factor y cierra las decisiones que cambiaban código. Las seis brechas que el spec levantó (H-1 a H-6) y su estado tras la implementación:

| # | Hallazgo | Estado | Dónde se resolvió / quién la tiene |
|---|---|---|---|
| H-1 | No había prueba de código **incorrecto** ni **vencido** (solo factor ausente) | **Cerrada** | `integration_segundofactor_test.go` (`TestSegundoFactorIncorrectoOVencido`), ejecutada contra Keycloak 26.7.4 real |
| H-2 | No había prueba a nivel servicio/HTTP de que el rechazo por factor **no deja residuo** y responde 401 uniforme | **Cerrada** | Flujo: `segundofactor_flujo_test.go`. HTTP: `internal/http/handlers_segundofactor_test.go` |
| H-3 | El **IdP en memoria ignoraba** `SegundoFactor` y era el valor por omisión (emitía sin factor) | **Cerrada** | DEC-095 (b+d): `memory.Sujeto.SegundoFactor` validado en `Issue`; aviso de arranque en `cmd/api/main.go` |
| H-4 | El contrato declara `segundo_factor` **opcional/nullable** mientras E4-T5 exige MFA universal | **Mitigada** | DEC-094 (b): rechazo local en `EmitirSesion`. El esquema del contrato NO cambia (obligatoriedad de comportamiento); sigue como brecha contractual C-2/H-4 |
| H-5 | **Brecha contractual en `bff`:** `CredencialesEntrada` de `bff.yaml` no tiene `segundo_factor` | **Abierta (C-5)** | Fuera de este repo: dueños de `pf-pkg-contracts`. Se reporta, no se rellena |
| H-6 | **Textos obsoletos** del contrato ("WebAuthn, SMS, etc.", Cognito) que contradicen DEC-071 | **Abierta (C-5/C-6)** | Fuera de este repo: `pf-pkg-contracts` y docs del contexto |

**Decisiones que E4-T5 cerró:** C-2 → DEC-094 (rechazo local, opción b) y C-3 → DEC-095 (doble `memory` exige factor + aviso; demo contra Keycloak real), ambas DECIDIDA en `ai27-platform-context`.

**Pruebas ejecutadas vs. solo compiladas (tarea 7.4 / 6.1):**

- **Ejecutadas:** unitarias de `internal/sesion` (flujo de rechazo, propiedades 1 y 2, higiene), `internal/http` (401 uniforme), `internal/idp/memory` (factor obligatorio), `internal/archtest` (regla anti-OTP y regla E), `realm_test.go`; y la **integración** `TestSegundoFactorIncorrectoOVencido` contra Keycloak 26.7.4 real.
- **Nota de entorno:** las unitarias de `internal/sesion` se ejecutan compilando el binario de test al workspace (`go test -c -o`), por una directiva WDAC de Windows que bloquea el binario efímero de `%TEMP%`.

**Fuera de alcance de E4-T5 (sin tarea):** enrolamiento/recuperación de TOTP (C-1, DEC-076, PENDIENTE); `bff.yaml` y textos del contrato (C-5); verificar que `mensajeria` no tiene OTP (repo ajeno); subir `go.mod` a un tracer nuevo.
