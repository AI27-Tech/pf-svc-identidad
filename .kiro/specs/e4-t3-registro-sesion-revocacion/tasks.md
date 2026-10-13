# Implementation Plan: E4-T3 — Registro de sesión, evento y snapshot de revocación (lado productor `identidad`)

> **E4-T3 NO cierra con este plan.** Este spec cubre solo el lado productor. E4-T3 cierra cuando además **E0-T4** entregue el lado consumidor (suscripción durable, difusión a réplicas, `not-ready`, canal obsoleto a 15 s, corte de 30 s y prueba extremo a extremo). Ninguna tarea de este archivo sustituye a E0-T4.

## Overview

Este plan implementa E4-T3 sobre el estado verificado del repositorio (`internal/sesion`, `internal/http`, `internal/archtest`, `migrations/identidad`) siguiendo `design.md` al pie de la letra. Se divide en dos partes:

- **Parte A — Se puede implementar ya.** Tareas con checkbox, sin ninguna decisión pendiente. Corrigen lo que ya está en la ruta productiva (H-1: el Paso 2 usaba el `sesion_id` en lugar del `idp_session_id`; H-5: el Paso 1 no distinguía "ya revocada" de "inexistente"; el 404 de `obtenerSesion`), agregan la migración `0003`, abren las costuras con valores por omisión que preservan el comportamiento actual, y construyen las pruebas que el diseño marca como ejecutables hoy.
- **Parte B — Bloqueadas por decisión.** Agrupadas por la decisión que las desbloquea (A-1, A-2, A-3, A-4, A-5/A-16, A-7, A-10, A-11, A-12, A-14, A-15 y B-10/H-3). Cada grupo abre con una **"Decisión pendiente"** que **no se ejecuta, se espera**. Las tareas de la Parte B **no tienen checkbox a propósito**: no son ejecutables ni aparecen en el grafo de dependencias. Cuando el owner de la decisión resuelva, se vuelve a este archivo (o a `requirements.md`, si la decisión no es la que el diseño previó) para convertirlas en tareas ejecutables.

Reglas que gobiernan el plan (pedidas por el usuario durante la sesión):

1. Estrictamente apegado a `requirements.md` y `design.md`. Las tareas implementan solo lo que el diseño marca como decidido o como costura. **Nada de las [Propuestas de Kiro, no aprobadas] entra a la ruta productiva:** vive solo en `internal/sesion/sesiontest`, y `identidad.New` no configura ninguna opción.
2. **Ninguna tarea resuelve un pendiente A-n.** Donde el diseño deja una costura, la tarea implementa la costura y su valor por omisión, nunca la elección.
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

# PARTE B — BLOQUEADAS POR DECISIÓN (no ejecutar)

> Todo lo que sigue está **BLOQUEADO**. Los ítems de esta parte **no tienen checkbox** y **no están en el grafo de dependencias**: son el registro de lo que se hará cuando el owner resuelva cada decisión. Cada **"Decisión pendiente"** no es una tarea de código: **se espera**. Ninguna tarea de esta parte resuelve el pendiente que la bloquea; implementan la opción que el owner apruebe, usando las costuras de la Parte A. Si la decisión sale distinta de las opciones que el diseño previó, se vuelve a la fase de requisitos antes de tocar código. Las implementaciones de `internal/sesion/sesiontest` siguen siendo **[Propuesta de Kiro, no aprobada]**: solo existen en pruebas.

## G1 — Desbloquea A-15 (segundo factor en la ruta de emisión)

- **D1. Decisión pendiente A-15.** Arquitectura + owner de `pf-pkg-contracts`: ampliar el puerto interno con `sesion.Credentials` (opción (a), propuesta de Kiro no aprobada), conservar `(usuario, contrasena)` o usar otro flujo. Sin DEC. **No se ejecuta; se espera.**
- **B1.1** 🔒 Si se aprueba (a): agregar `Credentials{Usuario, Contrasena, SegundoFactor}` en `internal/sesion/port.go` y cambiar la firma de `EmitirSesion`. Actualizar `consultas.go` y el handler `EmitirSesion` de `internal/http/handlers.go` (llena solo `Usuario` y `Contrasena`; `CredencialesEntrada` de `tracer.3` no transporta el segundo factor: queda como brecha BC-6). _Requirements: 9.1, 9.2, 9.3_
- **B1.2** 🔒 Actualizar llamadores y pruebas existentes a la firma nueva; cerrar con la verificación estándar. Si A-15 se rechaza, se conserva la firma y R9.1–R9.3 siguen bloqueados. _Requirements: 9.1_

## G2 — Desbloquea A-3 (`expira_en` de la sesión; DEC-078 relacionada)

- **D2. Decisión pendiente A-3.** Arquitectura: (a) expiración del primer token, sin actualizar (el diseño la marca como **hueco de seguridad**: el snapshot dejaría de listar la revocación mientras un token refrescado sigue vigente), (b) actualizar en cada refresco (solo segura junto con `RevokedRefreshReject` de A-4), o (c) duración máxima de la sesión (DEC-078, pendiente; requeriría ampliar el puerto de E4-T1). **No se ejecuta; se espera.**
- **B2.1** 🔒 Implementar la `ExpiryPolicy` aprobada y cablear `WithExpiryPolicy` en `identidad.New`. _Requirements: 2.1, 7.1_
- **B2.2** 🔒 `sesiontest.LatestTokenExpiry` (opción (b), **[Propuesta de Kiro, no aprobada]**), para las pruebas que dependen de G4 y G6. _Requirements: 2.1, 3.2_

## G3 — Desbloquea A-5 (`sujeto_id` de la fila) y A-16 (tenant de la auditoría)

- **D3.1. Decisión pendiente A-5.** Arquitectura (DEC-072 no lo fija): de dónde sale el `sub` en la emisión y qué pasa si el `sujeto` no existe (fallar, que cae en A-1, o crearlo, que es gobierno de E4-T6). **No se ejecuta; se espera.**
- **D3.2. Decisión pendiente A-16.** Arquitectura: origen del tenant de la auditoría (`sujeto.tenant_id` por *join*, tenant del token del actor, o columna nueva en `sesion`). **No se ejecuta; se espera.**
- **B3.1** 🔒 Implementar el `SujetoResolver` aprobado (devuelve `SujetoRef{SujetoID, TenantID}`; H-15: consultar con su propio `BeginTx`, no reutilizar `SujetoRepo` tal cual) y cablear `WithSujetoResolver`. _Requirements: 2.1, 5.3_
- **B3.2** 🔒 `sesiontest.SujetoBySub` (opción (a) más tenant de `sujeto.tenant_id`, **[Propuesta de Kiro, no aprobada]**). _Requirements: 2.1_

## G4 — Desbloquea A-1 (DEC-085, orden emisión↔registro)

Requiere además: A-15 (G1), A-3 (G2), A-5 (G3) y el trato de A-2 (G5).

- **D4. Decisión pendiente A-1 (DEC-085).** Arquitectura: (1) `Issue` primero y compensar de forma reintentable, (2) registrar antes de `Issue` (fila provisional; exige migración adicional y que snapshot y lectura excluyan filas provisionales), o (3) otra. **No se ejecuta; se espera.** Mientras tanto, `EmitirSesion` no se cablea en la ruta productiva.
- **B4.1** 🔒 Agregar a `seams.go` `SesionRecord`, `EmissionSteps` y `EmissionFlow`; agregar `WithEmissionFlow`; implementar `steps` interno sobre `idp`, `store` y `bestEffortCache`; `EmitirSesion` delega en el flujo cuando está configurado y devuelve `errNoImplementado` si falta flujo, resolvedor de sujeto o política de expiración. _Requirements: 1.1, 1.3, 1.4, 2.1_
- **B4.2** 🔒 `sesiontest.HappyPathEmission` (id → `Issue` → sujeto → expiración → `Register` → caché; devuelve el error sin compensar; **no es ninguna opción de DEC-085**), **[Propuesta de Kiro, no aprobada]**. _Requirements: 1.1, 2.1_
- **B4.3** 🔒 Property test 1 (`internal/sesion/emision_prop_integration_test.go`, **[Requiere Postgres]**). **Property 1: Correspondencia de emisión.** **Validates: Requirements 1.1, 1.3, 1.4, 2.1**
- **B4.4** 🔒 Implementar el `EmissionFlow` aprobado (compensación o fila provisional, más migración si corresponde y la decisión de K-1/K-12 sobre `NOT NULL`), cablearlo en `identidad.New` y aplicar el mapeo de errores de emisión de §Error Handling (401 por `ErrCredencialInvalida`, 503 por `ErrProveedorNoDisponible`) solo cuando se apruebe. _Requirements: 1.1, 2.1_

## G5 — Desbloquea A-2 (`una_sesion_viva` por sujeto)

- **D5. Decisión pendiente A-2.** Arquitectura (propuesto: owner de ADR 26): (a) conservar el índice, (b) quitarlo en una migración nueva (varias sesiones vivas por sujeto) o (c) conservarlo y revocar la sesión previa al emitir otra (regla de negocio nueva). **No se ejecuta; se espera.**
- **B5.1** 🔒 Si la opción lo exige, migración `0004` que aplique el cambio de índice o esquema (con su `GRANT` si agrega tabla) y actualización del smoke de esquema 1.3. _Requirements: 4.8_
- **B5.2** 🔒 Si se elige (b): ampliar el generador de la Property 7 (tarea 8.5) para agregar varias filas vivas por sujeto. _Requirements: 4.8_

## G6 — Desbloquea A-4 (resolución de la fila en el refresco)

Requiere además: A-15 (G1) y A-3 (G2).

- **D6. Decisión pendiente A-4.** Arquitectura: (a) `sesion_id` desde el claim del token nuevo, (b) búsqueda por `idp_session_id` (índice; K-16 sin verificar) o (c) hash del refresh token (el diseño lo descarta). Además: ¿`identidad` rechaza el refresco de una sesión revocada o lo deja al IdP tras el Paso 2? **No se ejecuta; se espera.**
- **B6.1** 🔒 Implementar el `RefreshResolver` aprobado y `RefrescarSesion` (`SELECT … FOR UPDATE` de la fila, sin `INSERT`, actualización de `expira_en` según `ExpiryPolicy`, modo `RevokedRefresh` aprobado). Cablear `WithRefresh`. Índice por `idp_session_id` solo si se elige (b). _Requirements: 3.1, 3.2_
- **B6.2** 🔒 `sesiontest.RefreshByClaim` (opción (a), **[Propuesta de Kiro, no aprobada]**) y un doble del IdP que rote refresh tokens y conozca su sesión. _Requirements: 3.1_
- **B6.3** 🔒 Property test 3 (`internal/sesion/refresco_prop_integration_test.go`, **[Requiere Postgres]**). **Property 3: Estabilidad del `sesion_id` en N refrescos.** **Validates: Requirements 3.1, 3.2**

## G7 — Desbloquea A-7 (reintento del Paso 2 y respuesta HTTP)

- **D7. Decisión pendiente A-7.** Arquitectura: (a) reintento síncrono acotado, (b) columna `idp_revocada_en` + worker, (c) tabla propia de pendientes (la descarta (d), outbox interno), y la respuesta HTTP con Paso 1 comprometido y Paso 2 fallido (204, 503 o 202; hoy 501). **No se ejecuta; se espera.**
- **B7.1** 🔒 Implementar el `IdpRevokeFailureHandler` aprobado y cablearlo con `WithIdpRevokeFailureHandler`. Si hay tabla nueva: `GRANT` explícitos y caso en `migrations/grants_integration_test.go` (H-13); si hay worker: entra a `Module.Workers()`. _Requirements: 4.6_
- **B7.2** 🔒 Mapeo HTTP aprobado del Paso 2 fallido (el contrato solo declara 204/401/403/404: registrar como brecha, no crear tipos de wire). _Requirements: 4.6_
- **B7.3** 🔒 Prueba de R4.6 (reintento con el mismo `idp_session_id` de la fila). _Requirements: 4.6_

## G8 — Desbloquea A-10 (semántica del cursor)

- **D8. Decisión pendiente A-10.** Arquitectura (owner de E0-T2 / E0-T10): (a) marca de agua por `(revocada_en, sesion_id)` (con huecos, H-6), (b) ventana de solapamiento, (c) visibilidad de snapshot de Postgres (`xid8` + `pg_current_snapshot`) o (d) posición del canal (choca con E0-T10: sin replay por offset). Además, qué significa `cursor_siguiente: null`. **No se ejecuta; se espera.**
- **B8.1** 🔒 Implementar el `RevocationLog` aprobado (con migración si corresponde), cablear `WithRevocationLog` y definir la respuesta ante cursor ilegible (K-5). _Requirements: 7.2, 7.3_
- **B8.2** 🔒 `sesiontest.SnapshotXidLog` + `ApplyCursorTestDDL` (opción (c); DDL aplicado **solo** en la base efímera de prueba, no es migración), **[Propuesta de Kiro, no aprobada]**. _Requirements: 7.2, 7.3_
- **B8.3** 🔒 Property test 12 (`internal/sesion/snapshot_prop_integration_test.go`, **[Requiere Postgres]**) con la carga de K-38 (`m ≤ 20`, `j ≤ 3`). **Property 12: Sin hueco entre snapshot y cursor.** Cubre R10.3. **Validates: Requirements 7.2, 7.3, 10.3**

## G9 — Desbloquea A-11 (caché de sesión)

- **D9. Decisión pendiente A-11.** Arquitectura: tecnología (Redis o Valkey), contenido y TTL de `sesion:{sesion_id}`, qué hace la revocación con la entrada (K-8), si se entrega adaptador o solo puerto, check de `/ready` (K-32) y el tiempo límite de K-31. **No se ejecuta; se espera.**
- **B9.1** 🔒 Adaptador del puerto `Cache` según la decisión, dependencia fijada en `go.mod`, check en `/ready` y cableado en `cmd/api/main.go` (hoy `Cache: nil`). _Requirements: 8.1, 8.3_
- **B9.2** 🔒 Ejemplo de R8.1 con `RecordingCache` y `HappyPathEmission` (un `GuardarSesion` con el `sesion_id` de la fila, después del commit; K-29). Requiere además G4. _Requirements: 8.1_

## G10 — Desbloquea A-12 (actor y motivo de la auditoría)

Requiere además: A-5 y A-16 (G3).

- **D10. Decisión pendiente A-12.** PM (motivo) + Arquitectura (actor): actor desde `auth.ClaimsFromContext` o parámetro explícito de `RevocarSesion`; el motivo no cabe en un `DELETE` sin cuerpo (brecha BC-8). **No se ejecuta; se espera.**
- **B10.1** 🔒 `AuditRecorder` concreto sobre `cambio_identidad` (`tipo = 'sesion.revocada'`, `sesion_id`, `sujeto_id`, `tenant_id`, `actor_id`, `motivo`; el `sub` del actor se resuelve a `sujeto.id`, H-14) y su cableado. `svc_identidad` solo tiene `SELECT, INSERT` sobre la tabla. _Requirements: 5.3_
- **B10.2** 🔒 Prueba de la auditoría (K-39). _Requirements: 5.3_

## G11 — Desbloquea A-14 (serialización del payload)

- **D11. Decisión pendiente A-14.** Arquitectura (debe coincidir con lo que decodifiquen los consumidores de E0-T4): (a) `map[string]any` (hoy), (b) `eventosv1.SesionRevocada` con `protojson` y `UseProtoNames: true`, o (c) proto binario (el diseño la descarta). **No se ejecuta; se espera.**
- **B11.1** 🔒 Si se aprueba (b), promover el encoder a código productivo (`google.golang.org/protobuf` como dependencia directa de producción) y cablear `WithPayloadEncoder`; re-ejecutar la Property 8 y el contract test 7.6 contra el codificador productivo. _Requirements: 5.1, 10.6_

## G12 — Integración R9 contra Keycloak 26.7.4 (desbloquea A-15, B-10/H-3, A-4 y A-2 según el caso)

- **D12.1. Decisión pendiente B-10 (H-3).** Owners de E4-T1: **no verificado** que el access token traiga `sid` (el cliente `ai27-login` no tiene el scope `basic`). Confirmarlo en la primera ejecución o agregar un mapper `sid` al realm (cambio de E4-T1, fuera de este spec). **No se ejecuta; se espera.**
- **B12.1** 🔒 Arnés en `internal/idp/internal/kc/` (opción (1) del diseño, `//go:build integration`): `rutaJARExtensionEstricta` (sin JAR hace `t.Fatalf`, R9.7), `arranquePostgres` (`postgres:16-alpine`; Docker no disponible → `t.Skip`; otra falla → `t.Fatalf` con la etapa; aplica todas las migraciones) y `dbtest.PoolAsDSN`. Desbloquea con la aprobación de usar las provisionales de `sesiontest` en pruebas y con A-15. Requiere Docker y el JAR. _Requirements: 9.5, 9.6, 9.7_
- **B12.2** 🔒 `integration_sesion_test.go`, caso R9.1 (`sesion_id` del claim = respuesta = fila). Desbloquea con A-15. _Requirements: 9.1_
- **B12.3** 🔒 Caso R9.2 (`idp_session_id` de la fila = claim `sid`, ambos no vacíos; K-34). Desbloquea con A-15 y D12.1. _Requirements: 9.2_
- **B12.4** 🔒 Caso R9.3 (tres refrescos en cadena; mismo `sesion_id`; conteo de filas sin cambio). Desbloquea con A-15 y A-4 (G6). _Requirements: 9.3_
- **B12.5** 🔒 Caso R9.4 (dos emisiones del mismo sujeto; revocar una; el refresco directo contra el Keycloak de prueba rechaza la revocada y acepta la otra; K-36). Desbloquea con A-15 y A-2 (G5). _Requirements: 9.4_
- Mientras un caso esté bloqueado **no se escribe con `t.Skip`** (DEC-074 y R9.6: solo se omite por Docker no disponible).

## Notes

- **E4-T3 no cierra sin E0-T4.** Aunque todo lo de este plan estuviera hecho, el criterio de aceptación y la DoD del lado consumidor siguen abiertos.
- La Parte A deja `EmitirSesion` y `RefrescarSesion` con `errNoImplementado` (501), el snapshot sin cursor y la auditoría sin escribirse: es el comportamiento de hoy. Lo único que cambia en la ruta productiva es lo que el diseño corrige sin depender de pendientes: Paso 2 con `idp_session_id` (H-1), Paso 1 en tres resultados (H-5), 404 de `obtenerSesion` y la migración `0003`.
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

---

# PARTE B — ESTADO (DEC-088 + decisión de alcance 2026-10-16)

> Las decisiones A-1…A-16 fueron tomadas por Product Management en **DEC-088**.
> Sin embargo, el **login (emisión y refresco de sesión) NO se construye en este
> spec**, por una decisión de alcance del PM el 2026-10-16: el flujo de emisión
> depende de **A-5** (resolver el `sub` del token a un `sujeto_id` de AI27), que
> es gobierno de **E4-T6** y aún no existe (su propia decisión A-13 sigue abierta).
> Construir el login aquí sería parchear sobre una dependencia inexistente.
>
> Por eso se revirtió el código de login que sesiones anteriores habían dejado a
> medias (emisión, refresco, auditoría y sus dobles de prueba): `EmitirSesion` y
> `RefrescarSesion` vuelven a devolver `errNoImplementado` (501), el comportamiento
> que el spec define para la Parte A. Lo único de la Parte B que **sí** se conserva
> es la caché Redis (GB9), porque es una pieza terminada, probada y aprobada
> (A-11, DEC-088) que no depende de E4-T6.
>
> **E4-T3 NO cierra con este spec:** el login espera a E4-T6, y el lado consumidor
> es E0-T4. Ambos son tareas aparte.

## GB9 — Caché de sesión con Redis (A-11, DEC-088 excepción)

- [x] GB9.1 Adaptador de Redis del puerto `sesion.Cache`
  - `internal/sesion/rediscache/rediscache.go`: `Cache` sobre `github.com/redis/go-redis/v9`
    (cliente oficial, compatible con Redis 7+ y Valkey). `GuardarSesion` escribe
    `sesion:{sesion_id}` con TTL = tiempo hasta `expira_en` (sesión ya expirada = no-op);
    `InvalidarSesion` hace `DEL` idempotente; `Ping` para readiness. Check de compilación
    `var _ sesion.Cache`. La caché nunca es fuente de verdad (ADR 26 §5).
  - `go.mod`: `github.com/redis/go-redis/v9 v9.23.0` fijado por versión exacta.
  - _Verificado:_ `go build`, `go vet`, y `rediscache_test.go` (4 pruebas) contra Redis real.
- [x] GB9.2 Cableado en `cmd/api/main.go` y check en `/ready/cache`
  - Lee `IDENTIDAD_REDIS_URL`; si está, construye el adaptador y lo pasa como `Deps.Cache`.
    Sin la variable, arranca con `Cache: nil` (lee de Postgres). El check `cache:redis` va en
    un registro aparte montado en `/ready/cache`: su caída no bloquea `/ready` de Postgres.
  - _Verificado:_ `go build`, `go vet ./cmd/...`, regla de aislamiento (archtest) y suite completa en verde.

## Pendientes de la Parte B — BLOQUEADAS por E4-T6 (no se construyen aquí)

> Estas piezas forman el login y su soporte. Todas dependen, directa o
> indirectamente, de A-5 (sujeto desde el token) y por tanto de **E4-T6**. No se
> implementan en este spec. Cuando E4-T6 exista y resuelva A-13, se retoman.

- [ ] GB1 — A-1: flujo de emisión con compensación (bloqueado por A-5/E4-T6).
- [ ] GB2 — A-3: `ExpiryPolicy` productiva + cablear (parte del login).
- [ ] GB3 — A-4: refresco con `RevokedRefreshReject` (parte del login).
- [ ] GB4 — A-5/A-16: `SujetoResolver` productivo — **lo entrega E4-T6**.
- [ ] GB5 — A-7: reintento exponencial del Paso 2 + 503.
- [ ] GB6 — A-9: autorización (solo el dueño revoca) en el handler. *(No depende de E4-T6; candidata a hacerse aparte.)*
- [ ] GB7 — A-10: `RevocationLog` con cursor por timestamp productivo.
- [ ] GB8 — A-12: `AuditRecorder` con actor desde claims (parte del login/auditoría).
- [ ] GB10 — Cableado global en `identidad.New` (cuando el login exista).

> **Ya resueltas por DEC-088 sin tarea de código:** A-2 (índice `una_sesion_viva` intacto),
> A-6 (sin revocación masiva en V1), A-8 (204 en sesión inexistente), A-14 (JSON plano por
> omisión), A-15 (segundo factor: contrato tracer.5 + handler ya propaga `segundo_factor`).
>
> **Removido el 2026-10-16 (decisión de alcance):** `emission.go`, `refresh_store.go`,
> `audit.go` y sus pruebas/dobles, que estaban a medias y bloqueados. `EmitirSesion` y
> `RefrescarSesion` devuelven `errNoImplementado`, como define la Parte A.
