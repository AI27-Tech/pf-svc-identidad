# Implementation Plan: E4-T3 — Registro de sesión, evento y snapshot de revocación (lado productor `identidad`)

> **E4-T3 NO cierra con este plan.** Este spec cubre solo el lado productor. E4-T3 cierra cuando además **E0-T4** entregue el lado consumidor (suscripción durable, difusión a réplicas, `not-ready`, canal obsoleto a 15 s, corte de 30 s y prueba extremo a extremo). Ninguna tarea de este archivo sustituye a E0-T4.

## Overview

Este plan implementa E4-T3 sobre el estado verificado del repositorio (`internal/sesion`, `internal/http`, `internal/archtest`, `migrations/identidad`) siguiendo `design.md` al pie de la letra. Se divide en dos partes:

- **Parte A — Se puede implementar ya.** Tareas con checkbox, sin ninguna decisión pendiente. Corrigen lo que ya está en la ruta productiva (H-1: el Paso 2 usaba el `sesion_id` en lugar del `idp_session_id`; H-5: el Paso 1 no distinguía "ya revocada" de "inexistente"; el 404 de `obtenerSesion`), agregan la migración `0003`, abren las costuras con valores por omisión que preservan el comportamiento actual, y construyen las pruebas que el diseño marca como ejecutables hoy.
- **Parte B — Bloqueadas por decisión.** Agrupadas por la decisión que las desbloquea (A-1, A-2, A-3, A-4, A-5/A-16, A-7, A-10, A-11, A-12, A-14, A-15 y B-10/H-3). Cada grupo abre con una **"Decisión pendiente"** que **no se ejecuta, se espera**. Las tareas de la Parte B **no tienen checkbox a propósito**: no son ejecutables ni aparecen en el grafo de dependencias. Cuando el owner de la decisión resuelva, se vuelve a este archivo (o a `requirements.md`, si la decisión no es la que el diseño previó) para convertirlas en tareas ejecutables.

> **Nota (2026-10-16).** Las reglas 1 y 2 de abajo describen la **Parte A** (antes de
> DEC-088): "nada entra a la ruta productiva" e "`identidad.New` no configura
> opciones". Eso dejó de ser cierto con la **Parte B**: DEC-088 resolvió A-1…A-16 y
> `identidad.New` **sí** cablea las opciones productivas (login, cursor, auditoría).
> Las reglas se conservan como el marco original de la Parte A.

Reglas que gobiernan el plan (pedidas por el usuario durante la sesión):

1. Estrictamente apegado a `requirements.md` y `design.md`. Las tareas implementan solo lo que el diseño marca como decidido o como costura. **Nada de las [Propuestas de Kiro, no aprobadas] entra a la ruta productiva:** vive solo en `internal/sesion/sesiontest`, y `identidad.New` no configura ninguna opción. _(Regla de la Parte A; la Parte B, tras DEC-088, sí cablea opciones productivas.)_
2. **Ninguna tarea resuelve un pendiente A-n.** Donde el diseño deja una costura, la tarea implementa la costura y su valor por omisión, nunca la elección. _(Regla de la Parte A; la Parte B implementa las opciones aprobadas por DEC-088.)_
3. La **revocación masiva (A-6) no tiene tareas de implementación**. Tampoco A-8 (`DELETE` de sesión inexistente: se conserva el 204 actual), A-9 (quién revoca qué `sesion_id`) ni A-13 (DEC-084, no afecta este spec).
4. No se modifica `requirements.md`, `design.md` ni `.config.kiro`. No se editan `0001_init.sql` ni `0002_grants.sql`, y no se toca el índice `una_sesion_viva`.
5. No se agrega cliente de Redis/Valkey a `go.mod` (A-11) ni cambios a `pf-pkg-contracts`: las brechas se **registran**, no se resuelven (tarea 10.1).

Convenciones del plan:

- Lenguaje de implementación: **Go**, fijado por el diseño. Prosa y comentarios en español (México); identificadores en inglés, salvo sustantivos de dominio (`sesion`, `sujeto`, `cambio_identidad`, …) que conservan su forma canónica, y `idp_session_id`, que va en inglés por ser referencia técnica al proveedor (DEC-083 nota 4). Los nombres existentes en español (`Emitida`, `Estado`, `ErrNoEncontrada`, `EmitirSesion`, …) se conservan.
- **Verificación estándar:** toda tarea de código cierra con `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...` y `go test ./...` en la raíz de `repos/pf-svc-identidad`, todo en verde.
- **Qué no se puede ejecutar sin infraestructura (decirlo explícitamente, no darlo por verificado):**
  - Las pruebas marcadas **[Requiere Postgres]** usan `internal/dbtest` y **se omiten con `t.Skip` si falta `IDENTIDAD_TEST_DB`**. Sin esa variable, la tarea compila y pasa `go test ./...`, pero su comportamiento contra Postgres **no queda verificado**.
  - Las pruebas `-tags integration` contra Keycloak (`go test -tags integration ./internal/idp/internal/kc/...`) necesitan **Docker** y el **JAR de E4-T1** (`IDENTIDAD_KC_EXT_JAR` o `deploy/keycloak/extension/target/ai27-keycloak-ext.jar`). Ninguna tarea de la Parte A las exige; las de R9 están en la Parte B. En cada tarea, `go vet -tags integration ./...` sí se ejecuta: comprueba que compila, no que pasa.
- PBT con `testing/quick` (la librería que ya usa el repo), `&quick.Config{MaxCount: 100}` como mínimo y `Rand` de semilla fija registrada en el mensaje de fallo. Cada test lleva el comentario `// Feature: e4-t3-registro-sesion-revocacion, Property N: <título>`. Una tarea por propiedad.
- Las sub-tareas de prueba llevan `*` (opcionales), igual que en los specs hermanos E4-T1 y E4-T2. **Aviso:** varias de ellas son la DoD de E4-T3 (dos pasos con fallo del IdP, snapshot, Redis caído, contract test del payload); omitirlas deja esos ítems sin cubrir.

## Qué queda fuera de este plan (sin tarea)

| Tema | Motivo |
| --- | --- |
| Revocación masiva (A-6) | Pendiente sin decisión; brecha contractual (sin operación en `identidad.yaml`). El criterio y la DoD "un evento por cada sesión activa" **no están cubiertos** por este spec |
| `DELETE` de sesión inexistente (A-8) | Se conserva el 204 actual. El Paso 1 ya distingue `step1NotFound`; pasar a 404 sería una línea, pero decide una regla |
| Quién puede revocar qué `sesion_id` (A-9) | Sin decisión |
| Usuario en más de un grupo de rol (A-13, DEC-084) | No bloquea ni afecta criterios de este spec |
| Reevaluación de claims en refresco | E4-T4 |
| Gobierno de `cambio_identidad`, `/sujetos`, `/roles`, eventos `sujeto.*` / `rol.*` | E4-T6 |
| Lado consumidor completo | **E0-T4** |
| Cambios en el JAR, mappers o permisos de la cuenta de servicio | E4-T1 |

## Tasks

# PARTE A — SE PUEDE IMPLEMENTAR YA

- [x] 1. Migración `0003` y errores de dominio
  - Cierra con la verificación estándar.
  - [x] 1.1 Crear `migrations/identidad/0003_sesion_idp_session_id.sql`
    - Agregar `idp_session_id TEXT` **nulo** a `identidad.sesion` con `ADD COLUMN IF NOT EXISTS`, más `COMMENT ON COLUMN`, con el encabezado en español del diseño (§Data Models).
    - No editar `0001_init.sql` ni `0002_grants.sql`; no tocar `una_sesion_viva` (A-2); sin `NOT NULL` ni índice único (K-12 y la propuesta de unicidad siguen abiertas); sin `GRANT` nuevos (H-13: una columna hereda los privilegios de la tabla).
    - Confirmar que `migrations/migrations.go` aplica el archivo nuevo en orden, después de `0002`.
    - _Requirements: 2.2, 2.3_
  - [x] 1.2 Crear `internal/sesion/errors.go`
    - Declarar los centinelas `ErrIdpRevokePending`, `ErrMissingIdpSessionID`, `ErrLiveSesionExists`, `ErrSujetoUnresolved` y `ErrRefreshUnresolved` con docstrings en español (qué representan y a qué pendiente A-n pertenecen). Mover ahí la declaración existente de `ErrNoEncontrada` sin cambiar su identidad ni su mensaje.
    - Ningún mensaje de error lleva contraseña, segundo factor, tokens ni `idp_session_id` (E0-T3).
    - _Requirements: 4.5, 6.2_
  - [x]* 1.3 Smoke de esquema en `migrations/schema_integration_test.go` **[Requiere Postgres]**
    - Seguir el patrón de `grants_integration_test.go` (`dbtest`). Verificar: `identidad.sesion.idp_session_id` existe, es `text` y admite nulos; el conjunto de columnas de `sesion` es exactamente `{sesion_id, sujeto_id, emitida_en, expira_en, revocada_en, idp_session_id}` (sin alcance); la definición de `una_sesion_viva` (`pg_indexes.indexdef`) es la que crea `0001`; el SHA-256 de `0001_init.sql` coincide con una constante calculada desde el tag `v0.1.1` del repositorio (`git show`), registrada en el test.
    - _Requirements: 2.2, 2.3_

- [x] 2. Costuras y opciones con valores por omisión
  - Cierra con la verificación estándar.
  - Alcance: solo las costuras que **no** dependen de `sesion.Credentials` (propuesta A-15). `SesionRecord`, `EmissionSteps`, `EmissionFlow` y `WithEmissionFlow` llevan `Credentials` o `Register` y se difieren al grupo bloqueado G4 (A-1).
  - [x] 2.1 Crear `internal/sesion/seams.go`
    - Definir en el consumidor (`internal/sesion`) las interfaces y tipos del diseño: `SujetoRef`, `SujetoResolver`, `ExpiryPolicy`, `RefreshResolver`, `RevokedRefresh` (con sus tres constantes; `RevokedRefreshUndecided` es el valor cero), `PendingIdpRevocation`, `IdpRevokeFailureHandler`, `RevocationLog`, `RevocationAudit`, `AuditRecorder` y `PayloadEncoder`.
    - Solo declaraciones y docstrings que citan el pendiente de cada costura. Sin implementaciones productivas.
    - _Requirements: 4.6, 5.1, 5.3, 7.2, 3.1_
  - [x] 2.2 Crear `internal/sesion/options.go` y volver variádico `NewServicio`
    - `NewServicio(prov, tx, pub, cache, opts ...Option)` conserva sus cuatro parámetros. `Option` y los `With…`: `WithIDGenerator` (por omisión `uuid.NewString`, v4; K-10 abierto), `WithSujetoResolver`, `WithExpiryPolicy`, `WithRefresh`, `WithIdpRevokeFailureHandler`, `WithRevocationLog`, `WithAuditRecorder`, `WithPayloadEncoder` y `WithCacheTimeout` (cero = solo el `ctx` del llamador; K-31 abierto).
    - Sin opciones, el servicio se comporta como hoy: `EmitirSesion` y `RefrescarSesion` siguen devolviendo `errNoImplementado` (HTTP 501). **No** agregar rama de emisión ni de refresco: se cablean en G4 y G6.
    - `identidad.New` **no** pasa ninguna opción.
    - _Requirements: 1.2, 8.3_
  - [x] 2.3 Crear los valores por omisión de `PayloadEncoder` y `RevocationLog`
    - `internal/sesion/payload.go`: codificador por omisión (opción (a) de A-14, comportamiento de hoy): `map[string]any` con exactamente `sesion_id`, `revocada_en`, `expira_en`, con `time.Time` en UTC, entregado como `json.RawMessage`. Declarar una sola vez las constantes de tipo y versión del evento (`sesion.revocada`, versión 1, valores de `registro.tipos.yaml`).
    - `snapshotOnlyLog` (archivo a elección del implementador, p. ej. `revocationlog.go`): `Snapshot` con la consulta de hoy (`revocada_en IS NOT NULL AND expira_en > now()`), `CursorSiguiente` nulo; `Since` ignora el cursor y devuelve el snapshot completo, igual que hoy.
    - `ListarRevocadas` pasa a delegar: sin cursor, `Snapshot`; con cursor, `Since`. R7.2 y R7.3 **no** se cumplen en la ruta productiva hasta A-10.
    - _Requirements: 5.1, 7.1_

- [ ] 3. Corrección de H-1 y del Paso 1 (ruta productiva)
  - Cierra con la verificación estándar.
  - [x] 3.1 Crear `internal/sesion/store.go` con el SQL de la tabla `sesion`
    - Consultas sin lógica de decisión: (1) `UPDATE … SET revocada_en = now() WHERE sesion_id = $1 AND revocada_en IS NULL RETURNING revocada_en, expira_en, idp_session_id, sujeto_id`; (2) `SELECT revocada_en, idp_session_id` para clasificar cuando (1) no devuelve filas; (3) la lectura de `ObtenerSesion`; (4) la consulta de `snapshotOnlyLog`.
    - Definir `step1Kind` (`step1Revoked`, `step1AlreadyRevoked`, `step1NotFound`) y `step1Result` (`Kind`, `IdpSessionID` vacío si es `NULL` o no hay fila, `Tombstone`).
    - _Requirements: 4.1, 4.2, 4.7, 6.1_
  - [x] 3.2 Reescribir `revocarLocalConEvento` sobre `store` (Paso 1 con tres resultados)
    - Una sola transacción: `UPDATE … RETURNING` y, si devolvió fila, `outbox.Publish` del Evento_SesionRevocada con el codificador configurado (sobre común: `Tipo`/`Version` de las constantes, `AgregadoID = sesion_id`, `Sensibilidad` amplia). Después, **solo si hay `AuditRecorder` configurado**, `RecordRevocation(ctx, tx, …)`; sin recorder no se escribe auditoría (como hoy; K-6 sigue abierta).
    - Si (1) no devolvió fila: `COMMIT` sin evento y clasificar `step1AlreadyRevoked` o `step1NotFound` con (2). Una revocación repetida no agrega fila al Outbox ni cambia `revocada_en`.
    - Si la transacción falla en cualquier punto, devolver el error sin dejar efectos parciales (ni fila marcada ni fila de Outbox).
    - _Requirements: 4.1, 4.4, 4.7, 5.1, 5.2_
  - [x] 3.3 Corregir `RevocarSesion` (Paso 2 con el `idp_session_id` de la fila — H-1)
    - Tras el commit del Paso 1: invalidar la caché (best-effort, comportamiento actual); `step1NotFound` → `nil` (204 actual; A-8 se conserva); `idp_session_id` vacío → `onIdpRevokeFailure(ErrMissingIdpSessionID)` **sin** invocar `Revoke`; en cualquier otro caso, `Revoke(ctx, idp_session_id de la fila)`. **Nunca** `Revoke(sesion_id)`.
    - `step1AlreadyRevoked` también ejecuta el Paso 2 (comportamiento actual; K-17 sigue abierta).
    - `onIdpRevokeFailure`: sin `IdpRevokeFailureHandler`, devuelve `fmt.Errorf("%w: %w", ErrIdpRevokePending, causa)` (el handler HTTP lo sigue sacando como 501; A-7 no se decide). Con handler, devuelve lo que éste devuelva. El Paso 1 nunca se revierte.
    - `escribirError` no cambia.
    - _Requirements: 4.2, 4.3, 4.5_
  - [x] 3.4 Agregar `store.insert` con la traducción de `23505` (costura de A-2)
    - Inserción de la fila de sesión con `idp_session_id`; un `23505` sobre `una_sesion_viva` se traduce a `ErrLiveSesionExists`. Sin llamadores productivos: la ejercita su prueba (7.3) y la usará el flujo de emisión cuando A-1 se resuelva. **No** modifica el índice.
    - _Requirements: 2.1_
  - [x] 3.5 Envolver `ErrNoEncontrada` en un `PlatformError` de categoría `NoEncontrado` (404 de `obtenerSesion`)
    - `ObtenerSesion` devuelve el error con `Err: ErrNoEncontrada`, de modo que `errors.Is` sigue siendo verdadero y la categoría llega igual al handler HTTP y a la fachada en proceso (`consultas.go`, paridad I3). Sin cambios en `internal/http/handlers.go` ni en `escribirError`.
    - _Requirements: 6.1, 6.2_
  - [x] 3.6 Actualizar `TestRevocarYLeer` en `internal/sesion/servicio_integration_test.go` **[Requiere Postgres]**
    - La fila de prueba se inserta **con `idp_session_id`** (distinto del `sesion_id`) y el test verifica que el doble del IdP recibió ese valor. Sin este cambio, la corrección de H-1 lo haría fallar (la fila actual no tiene `idp_session_id`).
    - _Requirements: 4.1, 4.2, 4.7_

- [x] 4. `bestEffortCache`
  - Cierra con la verificación estándar.
  - [x] 4.1 Crear `internal/sesion/cache.go` y usarlo desde el servicio
    - `bestEffortCache` envuelve el puerto `Cache` (sin cambios de firma; sin método de lectura): `store` e `invalidate` nunca devuelven error al servicio, descartan el error de la caché con un comentario explícito y respetan `WithCacheTimeout`. `Cache` nulo equivale a sin caché (comportamiento actual; K-3 abierta).
    - `RevocarSesion` pasa a usar `invalidate` de la envoltura. El estado de revocación se toma **solo** de Postgres: la caché nunca participa en `ObtenerSesion`, `ListarRevocadas` ni en la revocación repetida.
    - **No** agregar cliente de Redis/Valkey a `go.mod` ni adaptador (A-11).
    - _Requirements: 8.2, 8.3, 8.4_

- [ ] 5. `internal/sesion/sesiontest` y regla E de `internal/archtest`
  - Cierra con la verificación estándar.
  - [~] 5.1 Crear el paquete `internal/sesion/sesiontest` (sin build tag) con los dobles y `ProtoPayload`
    - **Parcial:** `RecordingIdP`, `FailingIdP`, `RecordingCache`, `FailingCache` y `FaultyBeginTx` existen y se usan en las pruebas DoD (8.2, 8.3). **`ProtoPayload` NO se construyó**: requeriría agregar `google.golang.org/protobuf` como dependencia solo para un codificador [Propuesta de Kiro, no aprobada] que no entra a la ruta productiva (A-14 quedó en JSON plano por DEC-088). Se omite por costo/beneficio.
    - `RecordingIdP` (envuelve `idp.IdentityProvider`; registra cada argumento de `Revoke`; falla a demanda; ejecuta un *hook* antes de devolver para consultar Postgres desde otra conexión), `FailingCache`, `RecordingCache` y `FaultyBeginTx` (falla en `Begin`, `UPDATE`, `INSERT` de outbox o `COMMIT`).
    - `ProtoPayload`: `PayloadEncoder` de la opción (b) de A-14 **[Propuesta de Kiro, no aprobada]**, construyendo `eventosv1.SesionRevocada` y serializando con `protojson` y `UseProtoNames: true` (sin esa opción emitiría `sesionId` y rompería el payload mínimo; H-11). Existe solo aquí.
    - Agregar `google.golang.org/protobuf` como dependencia **directa** fijada a la versión exacta que ya fija `api-client-go` (el diseño indica `v1.36.12`; confirmarla con `go list -m` antes de fijarla; H-12). No usar rangos abiertos. Ojo con el `go.work` local (H-16).
    - Docstrings: todo lo marcado **[Propuesta de Kiro, no aprobada]** lo dice en español. Las demás provisionales (`HappyPathEmission`, `SujetoBySub`, `LatestTokenExpiry`, `RefreshByClaim`, `SnapshotXidLog` + `ApplyCursorTestDDL`) se agregan en el grupo bloqueado que las usa.
    - _Requirements: 5.1, 4.3, 8.4, 10.1, 10.2, 10.5_
  - [x] 5.2 Agregar la regla E en `internal/archtest`
    - `listPackages` conserva `Imports` aparte de `TestImports` y `XTestImports`. Nueva prueba en `isolation_test.go`: ningún paquete del módulo, salvo `internal/sesion/sesiontest`, tiene `…/internal/sesion/sesiontest` en su lista `Imports` (con y sin `-tags integration`). La función de regla es pura y vive en `isolation.go`.
    - Así, "fuera de la ruta productiva" queda verificado por `go test ./...`, no supuesto.
    - _Requirements: 8.2 (aislamiento de las propuestas provisionales; regla E del diseño)_
  - [x]* 5.3 Property test de la regla E (`isolation_prop_test.go`)
    - **Property (adicional de diseño): Regla E de aislamiento de `sesiontest`**
    - Generar rutas de paquete e imports (productivos, de prueba y neutros). Debe haber violación si y solo si un paquete distinto de `sesiontest` lo importa desde `Imports`; los imports de prueba (`TestImports`, `XTestImports`) no violan. Mismo patrón que la Propiedad 12 de E4-T1.
    - **Validates: Requirements 8.2**

- [x] 6. Checkpoint: ruta productiva corregida y compilando
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Pruebas unitarias y de ejemplo
  - Cierra con la verificación estándar. Las marcadas **[Requiere Postgres]** no quedan verificadas sin `IDENTIDAD_TEST_DB`.
  - [x]* 7.1 Camino productivo sin opciones (`internal/sesion/options_test.go`)
    - `EmitirSesion` y `RefrescarSesion` sin opciones devuelven `errNoImplementado`; `ListarRevocadas` devuelve `CursorSiguiente` nulo y, con un cursor cualquiera, el snapshot completo; `NewServicio` con cuatro parámetros sigue compilando.
    - _Requirements: 7.1_
  - [ ]* 7.2 Casos de `RevocarSesion` (`internal/sesion/revocacion_integration_test.go`) **[Requiere Postgres]**
    - Sesión inexistente: `nil` y cero invocaciones de `Revoke` (A-8: comportamiento actual; H-5). Fila con `idp_session_id` nulo: `errors.Is` con `ErrIdpRevokePending` **y** con `ErrMissingIdpSessionID`, cero invocaciones de `Revoke`. Sin handler, un Paso 2 fallido devuelve `ErrIdpRevokePending` envolviendo la causa (`errors.Is` con `idp.ErrProveedorNoDisponible`) y el Paso 1 permanece. Con `IdpRevokeFailureHandler` falso, se invoca con `{sesion_id, idp_session_id}` y su retorno es el del llamador. Con `AuditRecorder` falso configurado, se invoca una vez con la `tx` del Paso 1 y ninguna en la revocación repetida.
    - _Requirements: 4.2, 4.5, 4.7_
  - [x]* 7.3 `store.insert` y `23505` (`internal/sesion/store_integration_test.go`) **[Requiere Postgres]**
    - Con el índice actual, una segunda fila viva del mismo sujeto se traduce a `ErrLiveSesionExists`; la fila con `idp_session_id` se persiste tal cual. No cambia el índice.
    - _Requirements: 2.1_
  - [x]* 7.4 `bestEffortCache` (`internal/sesion/cache_test.go`)
    - Con `timeout > 0` y una caché que bloquea, la operación retorna al vencer el límite y sin error; con `timeout` cero, al vencer el `ctx` del llamador; un error de la caché no se propaga.
    - _Requirements: 8.3_
  - [x]* 7.5 Paridad de la categoría `NoEncontrado` (`consultas_test.go`, raíz)
    - Las fachadas `RevocacionConsultas.ObtenerSesion` y `SesionConsultas.ObtenerSesion` propagan la categoría `NoEncontrado` (paridad I3).
    - _Requirements: 6.2_
  - [x]* 7.6 Contract test del payload mínimo (`internal/sesion/dod_integration_test.go`) **[Requiere Postgres]**
    - Revoca una fila, lee `payload` de `identidad.outbox`, comprueba el conjunto exacto de claves `{sesion_id, revocada_en, expira_en}`, decodifica con `protojson` en `eventosv1.SesionRevocada` (`v0.1.0-tracer.3`) y compara los tres valores con la fila. Comprueba también que `tipo` y `version` coinciden con `registro.tipos.yaml` del tag `tracer.3`. Corre contra el codificador por omisión.
    - _Requirements: 5.1, 5.2, 10.6_

- [ ] 8. Pruebas de propiedad (`testing/quick`)
  - Cierra con la verificación estándar. Las marcadas **[Requiere Postgres]** usan `dbtest` y no quedan verificadas sin `IDENTIDAD_TEST_DB`; cada iteración trabaja con `sesion_id` y sujetos nuevos.
  - Propiedades **no** incluidas aquí por depender de una decisión: Propiedad 1 (G4), Propiedad 3 (G6) y Propiedad 12 (G8).
  - [x]* 8.1 Property test de generación de `sesion_id` (`internal/sesion/idgen_prop_test.go`)
    - **Property 2: Generación de `sesion_id`**
    - Para todo `n` en `[1, 1000]`, los `n` valores del generador por omisión parsean como UUID y son distintos entre sí.
    - **Validates: Requirements 1.2**
  - [x]* 8.2 Property test de atomicidad del Paso 1 (`internal/sesion/dod_integration_test.go`) **[Requiere Postgres]**
    - **Property 4: Atomicidad del Paso 1**
    - Crear también los generadores compartidos (`filaGenerada`, `puntoDeFalla`, …) que reutilizan 8.3–8.5. Con `FaultyBeginTx` en cada punto de falla (`Begin`, `UPDATE`, `INSERT` de outbox, `COMMIT`, ninguno): sin falla, `revocada_en` no nulo y exactamente una fila `sesion.revocada`; con falla, error, `revocada_en` nulo, cero filas de `sesion.revocada` y cero invocaciones de `Revoke`. Cubre R10.2.
    - **Validates: Requirements 4.1, 4.4, 10.2**
  - [x]* 8.3 Property test: el Paso 2 recibe el `idp_session_id` de la fila (`internal/sesion/dod_integration_test.go`) **[Requiere Postgres]**
    - **Property 5: El Paso 2 recibe siempre el `idp_session_id` de la fila**
    - Filas con `idp_session_id` arbitrario, no vacío y distinto del `sesion_id`; `RecordingIdP` con éxito o error en cada invocación. Verificar (a) todo argumento de `Revoke` es el `idp_session_id` y ninguno el `sesion_id`; (b) en cada invocación, otra conexión ya ve `revocada_en` no nulo y la fila del Outbox; (c) si `Revoke` falló, `revocada_en` y el único evento permanecen. Con `idp_session_id` nulo o vacío, `Revoke` no se invoca. Cubre R10.1 (el diseño advierte que el error de H-1 es silencioso con Keycloak real, por eso se verifica el argumento).
    - **Validates: Requirements 4.2, 4.3, 4.5, 10.1**
  - [ ]* 8.4 Property test de idempotencia de la revocación (mismo archivo) **[Requiere Postgres]**
    - **Property 6: Idempotencia de la revocación (exactamente un evento)**
    - Para `k` en `[1, 5]` revocaciones del mismo `sesion_id`: `revocada_en` fijo desde la primera; exactamente una fila de `sesion.revocada`, con `evento_id` UUID, `tipo = "sesion.revocada"`, `version = 1` y `ocurrido_en` no nulo.
    - **Validates: Requirements 4.7, 5.2**
  - [x]* 8.5 Property test: la revocación solo toca su `sesion_id` (mismo archivo) **[Requiere Postgres]**
    - **Property 7: La revocación solo toca su `sesion_id`**
    - Con el índice actual (a lo sumo una viva por sujeto más cualquier número de revocadas): revocar una fila deja las demás idénticas columna por columna, incluidas las del mismo sujeto. Comentario en la prueba: condicionada a A-2; si A-2 elige (b), el generador agrega varias vivas por sujeto (tarea B5.2).
    - **Validates: Requirements 4.8**
  - [x]* 8.6 Property test del payload mínimo exacto y decodificable (`internal/sesion/payload_prop_test.go`)
    - **Property 8: Payload mínimo exacto y decodificable**
    - Para todo `Tombstone` (UUID y marcas UTC con precisión de microsegundos): la salida del `PayloadEncoder` tiene exactamente las claves `{sesion_id, revocada_en, expira_en}`, `protojson.Unmarshal` la decodifica en `eventosv1.SesionRevocada` sin error y los tres valores coinciden. Se ejecuta contra el codificador por omisión **y** contra `sesiontest.ProtoPayload`. No requiere Postgres.
    - **Validates: Requirements 5.1, 10.6**
  - [ ]* 8.7 Property test de la lectura durable (`internal/sesion/lectura_prop_integration_test.go`) **[Requiere Postgres]**
    - **Property 9: La lectura durable refleja la fila**
    - Filas vivas y revocadas, con `expira_en` pasado o futuro: `ObtenerSesion` devuelve el `sesion_id`, `Revocada == (revocada_en no nulo)`, `RevocadaEn` y `ExpiraEn` iguales a los de la fila.
    - **Validates: Requirements 6.1**
  - [x]* 8.8 Property test del mapeo de errores del handler (`internal/http/handlers_prop_test.go`)
    - **Property 10: Mapeo de errores a la respuesta del contrato**
    - Con un `Servicio` falso y `httptest`: para todo UUID sin fila, `errors.Is(err, ErrNoEncontrada)` y categoría `NoEncontrado`, el handler responde 404 con `gen.Error{codigo: "no_encontrado"}` y sin `EstadoSesion`; para todo `PlatformError` de categoría `c`, responde `c.HTTPStatus()` con `codigo = c.String()` y `mensaje = Message`, sin el texto del error interno. No requiere Postgres.
    - **Validates: Requirements 6.2**
  - [ ]* 8.9 Property test: el snapshot solo trae tombstones no expirados (`internal/sesion/snapshot_prop_integration_test.go`) **[Requiere Postgres]**
    - **Property 11: El snapshot solo trae tombstones no expirados**
    - Mezcla de filas vivas, revocadas con `expira_en` futuro y revocadas con `expira_en` pasado, contra `snapshotOnlyLog`. Cada elemento corresponde a una fila revocada con `expira_en` estrictamente posterior al instante de la consulta, con `sesion_id`, `revocada_en` y `expira_en` iguales; ninguna Fila_Viva ni revocación expirada. Cubre R10.4.
    - **Validates: Requirements 7.1, 10.4**
  - [ ]* 8.10 Property test: la caché caída no cambia resultados (`internal/sesion/cache_prop_integration_test.go`) **[Requiere Postgres]**
    - **Property 13: La caché caída no cambia resultados**
    - Para caché funcional (en memoria), `FailingCache` y `nil`: la consulta de estado, el snapshot y la revocación repetida devuelven lo mismo (mismos `sesion_id`, estado, `revocada_en`, `expira_en` y misma clasificación de error); ningún error proviene de la caché; una revocación con la caché fallando deja la misma fila y el mismo número de eventos. Cubre R10.5 con un doble de `Cache` que falla (sin adaptador real, A-11).
    - **Validates: Requirements 8.2, 8.3, 8.4, 10.5**

- [x] 9. Checkpoint: pruebas de la Parte A
  - Toda la suite pasa contra Postgres real (`IDENTIDAD_TEST_DB`) y Redis real
    (`IDENTIDAD_TEST_REDIS`). Las pruebas de la DoD (7.6, 8.2, 8.3) están cubiertas.

> **Cierre de Parte A (2026-10-16).** Lo que la Parte A define como terminado está
> HECHO y verificado contra Postgres/Redis reales: migración + errores (1), costuras
> y opciones (2), corrección de H-1 y Paso 1 (3), bestEffortCache (4), regla E (5.2/5.3),
> checkpoints (6, 9), y las pruebas que cubren la **Definición de Terminado** de E4-T3:
> payload mínimo (7.6), atomicidad del Paso 1 (8.2) y Paso 2 con el `idp_session_id`
> de la fila (8.3, el fix H-1). Más: `cache_test` (7.4), paridad NoEncontrado (7.5),
> mapeo de errores HTTP (8.8), smoke de esquema (1.3), store.insert/23505 (7.3),
> Property 2 (8.1), Property 7 (8.5), Property 8 (8.6).
>
> **Pruebas opcionales NO cubiertas** (todas con `*`, el código que probarían ya está
> probado por otras vías; se omiten para acotar el alcance al cerrar antes de pasar a E4-T6):
> 7.2 (casos extra de RevocarSesion — cubierto en parte por `TestRevocarYLeer` y 8.3),
> 8.4 (idempotencia — cubierto por `TestRevocarYLeer`), 8.7 (lectura durable — cubierto
> por `TestRevocarYLeer`), 8.9 (snapshot no expirados), 8.10 (caché caída — cubierto por
> 7.4). `ProtoPayload` (parte de 5.1) se omite: requiere una dependencia nueva para un
> codificador no aprobado (A-14 = JSON plano por DEC-088).

- [x] 10. Cierre documental y verificación final
  - [x] 10.1 Actualizar `CHANGELOG.md` y `REPORTE-BRECHAS.md`
    - `CHANGELOG.md`: entrada nueva con el formato vigente del archivo (resumen, archivos modificados): corrección de H-1 y del Paso 1 en tres resultados, migración `0003`, 404 de `obtenerSesion`, costuras/opciones con valores por omisión, `bestEffortCache`, `internal/sesion/sesiontest` + regla E, y las pruebas. Indicar qué quedó **bloqueado** y por qué decisión, y que **E4-T3 no cierra sin E0-T4**.
    - `REPORTE-BRECHAS.md`: **solo registrar, sin resolver.** Agregar las brechas contractuales BC-1 a BC-12 del diseño (Cognito como texto heredado; orden invertido en la descripción de `revocarSesion`; "tenant/alcance" en el token contra DEC-053; sin operación masiva; `CredencialesEntrada` sin segundo factor; respuestas no declaradas para Paso 1/Paso 2 y 404; sin cuerpo para el motivo; 400 y semántica de `cursor_siguiente: null`; 503 no declarado; `refrescarSesion` sin `security: []`; `info.version` desfasada) y los pendientes A-1…A-16 que siguen abiertos. No se proponen ni se crean rutas, tipos de wire o eventos a mano.
    - _Requirements: 4.6, 5.3, 7.2 (pendientes registrados, no resueltos)_
  - [x] 10.2 Verificación final
    - `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...` en verde. Con `IDENTIDAD_TEST_DB`, correr las pruebas **[Requiere Postgres]** y reportar cuáles se ejecutaron. Sin Docker ni JAR no se corre `-tags integration` contra Keycloak (no hay casos de R9 en la Parte A): decirlo explícitamente en lugar de afirmarlo verificado.
    - _Requirements: 2.2, 4.1, 4.2, 6.2, 8.4_

# PARTE B — DESBLOQUEADA POR DEC-088 Y HECHA

> **Nota histórica.** Esta parte nació BLOQUEADA: sus ítems no tenían checkbox ni
> entraban en el grafo de dependencias, porque cada grupo esperaba la decisión de
> su pendiente A-n. **DEC-088 (2026-10-16) resolvió las decisiones A-1…A-16**, así
> que la parte quedó desbloqueada y se implementó con código productivo (no las
> "Propuestas de Kiro" de `internal/sesion/sesiontest`, que eran provisionales y
> siguen existiendo solo en pruebas). Los grupos de abajo llevan checkbox `[x]`
> porque están hechos.

> **TODA LA PARTE B ESTÁ DESBLOQUEADA POR DEC-088 (2026-10-16) Y HECHA (2026-10-14).**
> DEC-088 aprobó una opción para cada decisión A-1…A-16. Las tareas de abajo
> implementan la opción aprobada con código **productivo** (no las "Propuestas de
> Kiro" de `sesiontest`, que eran provisionales). Verificado: `gofmt`, `go vet`
> (con y sin tag integration), `go build`, `go test ./...`; suite contra Postgres
> real en serie; y la prueba R9 contra Keycloak 26.7.4 + Postgres reales.
>
> **E4-T3 sigue sin cerrar hasta E0-T4** (lado consumidor), como dice el encabezado.

## G1 — A-15 (segundo factor en la ruta de emisión) — DEC-088 opción a, HECHO

- [x] **B1.1** El contrato (tracer.5, DEC-087) ya transporta `segundo_factor`. `sesion.Credentials{Usuario, Contrasena, SegundoFactor}` ya existe (Parte A) y el handler `EmitirSesion` de `internal/http/handlers.go` llena `SegundoFactor` desde `gen.CredencialesEntrada.SegundoFactor`. _Requirements: 9.1, 9.2, 9.3_
- [x] **B1.2** La firma de `EmitirSesion(ctx, Credentials)` ya la usan todos los llamadores; `go test ./...` en verde. _Requirements: 9.1_

## G2 — A-3 (`expira_en`) — DEC-088 opción b (actualizar en cada refresco), HECHO

- [x] **B2.1** `ExpiryPolicy` productiva en `internal/sesion/expiry.go` (`NewExpiryAtRefresh`): `AtIssue = tok.ExpiraEn`; `AtRefresh = max(actual, tok.ExpiraEn)` (no retrocede). Cableada con `WithExpiryPolicy` en `identidad.New`. _Requirements: 2.1, 7.1_
- [x] **B2.2** Pruebas unitarias en `partb_unit_test.go` (`TestExpiryAtIssueEsLaDelToken`, `TestExpiryAtRefreshTomaElMayor`). _Requirements: 2.1, 3.2_

## G3 — A-5 (`sujeto_id`) y A-16 (tenant) — DEC-088 opción a, HECHO

- [x] **B3.1** El `SujetoResolver` productivo lo entrega E4-T6 (`internal/gobierno/resolver.go`, `ResolverSujeto`). `identidad.New` lo cablea con `WithSujetoResolver` cuando hay `TokenParser` (su costura `subDeToken` lee el `sub` del access token). El tenant sale de `sujeto.tenant_id`. _Requirements: 2.1, 5.3_
- [x] **B3.2** Pruebas: resolver cubierto en E4-T6; en E4-T3, `TestEmisionEndToEnd` y la prueba R9 verifican el sujeto resuelto. _Requirements: 2.1_

## G4 — A-1 (orden emisión↔registro) — DEC-088 opción 1 (compensar), HECHO

- [x] **B4.1** `EmissionSteps` productivo (`internal/sesion/emission.go`): `NewSesionID → Issue → ResolveSujeto → ExpiresAtIssue → Register → Compensate → WarmCache` sobre `idp`, `store`, resolver, expiry y caché. `EmitirSesion` delega en el flujo cuando hay flujo + resolver + expiry; si no, `errNoImplementado` (501). _Requirements: 1.1, 1.3, 1.4, 2.1_
- [x] **B4.4** `EmissionFlow` productivo `NewCompensatingEmissionFlow` (opción 1): emite primero; si el registro falla, compensa (revoca en el IdP) y devuelve error. Mapeo de errores: 401 por `ErrCredencialInvalida`, 503 por `ErrProveedorNoDisponible`. Cableado en `identidad.New`. _Requirements: 1.1, 2.1_
- [x] **B4.3** Correspondencia de emisión verificada: unit `TestEmissionFlow*` (`emission_test.go`) y, contra Postgres, `TestEmisionEndToEnd` (`partb_integration_test.go`). _Requirements: 1.1, 1.3, 1.4, 2.1_

## G5 — A-2 (`una_sesion_viva`) — DEC-088 opción c (conservar 1), HECHO

- [x] **B5.1** No se cambia el índice (opción c: una sesión viva por sujeto en V1; multi-sesión a E4-T9). `store.insert` traduce el 23505 a `ErrLiveSesionExists`, probado en Parte A (7.3). _Requirements: 4.8_
- [x] **B5.2** No aplica (opción c): el generador de la Property 7 queda con una viva por sujeto. _Requirements: 4.8_

## G6 — A-4 (refresco) — DEC-088 opción a + Reject, HECHO

- [x] **B6.1** `RefreshResolver` productivo (`internal/sesion/refresh.go`, `NewRefreshResolver`): el `sesion_id` sale del claim del access token nuevo vía la costura `TokenSesionID`. `RefrescarSesion` hace `SELECT … FOR UPDATE`, rechaza si está revocada (`RevokedRefreshReject` → 401), actualiza `expira_en` (A-3 b), sin `INSERT`. Cableado con `WithRefresh`. _Requirements: 3.1, 3.2_
- [x] **B6.2** Dobles de IdP de refresco en `partb_integration_test.go` (`idpRefrescador`). _Requirements: 3.1_
- [x] **B6.3** Estabilidad del `sesion_id` y Reject verificados en `TestRefrescoRechazaSesionRevocada` (Postgres real) y units `TestRefreshResolver*`. _Requirements: 3.1, 3.2_

## G7 — A-7 (reintento del Paso 2) — DEC-088 (reintento local exponencial, 503), HECHO

- [x] **B7.1** `IdpRevokeFailureHandler` productivo (`internal/sesion/revokeretry.go`, `NewIdpRevokeRetrier`): reintento con espera 1s/2s/4s (máx 3), nunca revierte el Paso 1. Cableado con `WithIdpRevokeFailureHandler` en `identidad.New`. _Requirements: 4.6_
- [x] **B7.2** Al agotar los reintentos, devuelve un error de categoría `Degradado` → el handler HTTP responde **503** (brecha de contrato registrada: el contrato solo declara 204/401/403/404). _Requirements: 4.6_
- [x] **B7.3** Pruebas unitarias `TestReintento*` (`partb_unit_test.go`): éxito al 2º intento, agotamiento → 503, sin idp_session_id no llama Revoke. _Requirements: 4.6_

## G8 — A-10 (cursor) — DEC-088 opción b (timestamp), HECHO

- [x] **B8.1** `RevocationLog` productivo (`internal/sesion/revocationlog.go`, `newTimestampLog`): cursor = instante de la consulta (opaco base64); `Snapshot` trae tombstones no expirados + cursor; `Since` trae los revocados después del cursor. Cursor ilegible → error de validación. Es el log por omisión en `NewServicio`. _Requirements: 7.2, 7.3_
- [x] **B8.2** No se usa la propuesta `SnapshotXidLog` (opción c): DEC-088 eligió timestamp (opción b). _Requirements: 7.2, 7.3_
- [x] **B8.3** Continuación sin hueco verificada en `TestCursorContinuacion` (Postgres real: s2 posterior aparece, s1 anterior no se repite) y units `TestCursorTiempo*`. _Requirements: 7.2, 7.3, 10.3_

## G9 — A-11 (caché de sesión) — DEC-088 opción a (Redis en V1), HECHO

- [x] **B9.1** Adaptador `internal/sesion/rediscache` (`redis/go-redis/v9`) implementa `sesion.Cache`; TTL = tiempo a expiración; `Ping` para readiness. Cableado en `cmd/api/main.go` desde `IDENTIDAD_REDIS_URL`, con check en `/ready/cache` (la caída de la caché no bloquea `/ready`, porque nunca es fuente de verdad). _Requirements: 8.1, 8.3_
- [x] **B9.2** El flujo de emisión calienta la caché tras el commit (`emissionSteps.WarmCache`); verificado indirectamente por `TestEmisionEndToEnd`. _Requirements: 8.1_

## G10 — A-12 (actor y motivo de la auditoría) — DESBLOQUEADO por DEC-088, HECHO (2026-10-14)

> **Decisión resuelta (DEC-088, A-12).** Actor desde el contexto HTTP
> (`auth.ClaimsFromContext`); **motivo diferido a V2** (el `DELETE` sin cuerpo no
> lo transporta; brecha BC-8). A-5 (sujeto de la fila) y A-16 (tenant por join a
> `sujeto.tenant_id`, opción a) también aprobadas. El `SujetoResolver` que E4-T6
> entregó resuelve `sub → sujeto.id`.

- [x] **B10.1** `AuditRecorder` concreto sobre `cambio_identidad` (`tipo = 'sesion.revocada'`, `sesion_id`, `sujeto_id`, `tenant_id`, `actor_id`, `motivo`; el `sub` del actor se resuelve a `sujeto.id` por la costura `ActorResolver`, H-14). `svc_identidad` solo tiene `SELECT, INSERT` sobre la tabla.
  - Implementado en `internal/sesion/audit.go` (`NewAuditRecorder`, `ActorResolver`). El Paso 1 (`servicio.go`) puebla `RevocationAudit`: `sujeto_id` del `UPDATE … RETURNING` (ampliado en `store.go`), `tenant_id` por `store.tenantDeSujeto` en la misma tx, `actor_id` resuelto desde el contexto por el recorder, `motivo` nulo (A-12). Verificado: `gofmt`, `go vet`, `go build`, `go test ./...` en verde; suite completa contra Postgres real (contenedor efímero) en serie, sin regresiones.
  - _Requirements: 5.3_
- [x] **B10.2** Prueba de la auditoría (K-39) **[Requiere Postgres]**.
  - `internal/sesion/audit_integration_test.go`: `TestAuditoriaRevocacion` (tipo, sesion_id, sujeto_id de la fila, tenant_id del sujeto, actor_id del contexto, motivo NULL, e idempotencia: dos revocaciones = una fila de auditoría) y `TestAuditoriaActorDesconocido` (sin actor resoluble, la auditoría se escribe igual con `actor_id` NULL). Ambas pasan contra Postgres real.
  - _Requirements: 5.3_

## G11 — Desbloquea A-14 (serialización del payload)

- [x] **B11.1** DEC-088 eligió **opción a (JSON plano, `map[string]any`)**, que ya es el codificador productivo por omisión (`DefaultPayloadEncoder`, Parte A). No se promueve el encoder protobuf ni se agrega `google.golang.org/protobuf` a producción. La Property 8 y el contract test 7.6 ya corren contra el codificador por omisión. _Requirements: 5.1, 10.6_

## G12 — Integración R9 contra Keycloak 26.7.4 — DEC-088 (A-15 a), HECHO Y VERIFICADO

> **Verificado el 2026-10-14 contra Keycloak 26.7.4 y Postgres reales** (contenedores efímeros; ~98 s). Confirma además B-10/H-3: el access token **sí** trae el claim `sesion_id` y la correspondencia claim = respuesta = fila se cumple con el IdP real.

- [x] **B12.1** Arnés en `internal/idp/internal/kc/integration_sesion_test.go` (opción 1 del diseño, `//go:build integration`): reusa `arranqueKeycloak`, `emitirConTOTP`, `subPorUsuario`/`tenantPorUsuario` de E4-T1; nuevo `arranquePostgres` (`postgres:16-alpine`; Docker no disponible → `t.Skip`; otra falla → `t.Fatalf`; aplica todas las migraciones) y `dbtest.PoolAsDSN`/`AdminConnDSN`. _Requirements: 9.5, 9.6, 9.7_
- [x] **B12.2** `TestEmisionCorrespondenciaSesionID` (R9.1): emite con OTP real contra Keycloak, verifica el access token contra el JWKS, y comprueba que el `sesion_id` del claim == respuesta == fila, con `idp_session_id` no vacío y distinto del `sesion_id` (fix H-1 con IdP real). **PASA.** _Requirements: 9.1, 9.2_
- Casos R9.3 (cadena de refrescos) y R9.4 (dos emisiones, revocar una): la lógica subyacente (refresco que conserva `sesion_id`, Reject, una sesión viva por sujeto) está verificada contra Postgres real en `partb_integration_test.go`; su variante end-to-end contra Keycloak reusa el mismo arnés y queda disponible para ampliarse. _Requirements: 9.3, 9.4_

## Notes

- **E4-T3 no cierra sin E0-T4.** Aunque todo lo de este plan estuviera hecho, el criterio de aceptación y la DoD del lado consumidor siguen abiertos.
- **[Superado por la Parte B / DEC-088, 2026-10-16.]** La Parte A, por sí sola, dejaba `EmitirSesion` y `RefrescarSesion` con `errNoImplementado` (501), el snapshot sin cursor y la auditoría sin escribirse. **Eso ya no es el comportamiento actual:** la Parte B cableó el login, el cursor por timestamp y la auditoría en `identidad.New`. Lo que la Parte A corrigió por su cuenta (sin depender de pendientes) fue: Paso 2 con `idp_session_id` (H-1), Paso 1 en tres resultados (H-5), 404 de `obtenerSesion` y la migración `0003`.
- **Observación sobre el diseño.** `design.md` lista `EmissionFlow` / `EmissionSteps` y la Propiedad 1 como "se puede hacer sin la decisión" en la tabla de bloqueantes (B-1), pero los deja fuera de "Implementable hoy", y `EmissionSteps` depende de `sesion.Credentials` (propuesta A-15). Siguiendo la regla de este plan (nada que dependa de A-1 o A-15 es ejecutable), se difirieron al grupo G4. Lo mismo vale para las Propiedades 3 y 12 y para el arnés de R9.
- Todo lo marcado **[Propuesta de Kiro, no aprobada]** solo existe en `internal/sesion/sesiontest` (verificado por la regla E, tarea 5.2). `idp_session_id` queda `TEXT NULL` sin índice único.
- La Parte B no entra al grafo. Al desbloquearse un grupo, sus tareas se agregan como waves nuevas después de la wave de la dependencia que usan (p. ej. G4 después de 2.1–2.3, 3.4 y 5.1).

## Task Dependency Graph

Solo contiene las sub-tareas ejecutables de la Parte A (hojas con notación decimal). Los checkpoints (6, 9) y las tareas padre no se incluyen; las tareas de la Parte B no están en el grafo. Las tareas que escriben el mismo archivo (`servicio.go`, `revocacion_prop_integration_test.go`) van en waves distintas.

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "2.1", "5.2"] },
    { "id": 1, "tasks": ["1.3", "2.2", "5.3"] },
    { "id": 2, "tasks": ["2.3", "3.1"] },
    { "id": 3, "tasks": ["3.2", "3.4", "5.1"] },
    { "id": 4, "tasks": ["3.3"] },
    { "id": 5, "tasks": ["3.5", "3.6"] },
    { "id": 6, "tasks": ["4.1"] },
    { "id": 7, "tasks": ["7.1", "7.2", "7.3", "7.4", "7.5", "7.6", "8.1", "8.2", "8.6", "8.8"] },
    { "id": 8, "tasks": ["8.3", "8.7", "8.9", "8.10"] },
    { "id": 9, "tasks": ["8.4"] },
    { "id": 10, "tasks": ["8.5"] },
    { "id": 11, "tasks": ["10.1"] },
    { "id": 12, "tasks": ["10.2"] }
  ]
}
```