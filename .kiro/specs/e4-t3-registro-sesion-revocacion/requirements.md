# Requirements Document

**E4-T3 — Registro de sesión, evento y snapshot de revocación (lado productor `identidad`)** · Documento de requisitos

| | |
| --- | --- |
| **Estado** | Borrador — pendiente de validación |
| **Historia técnica** | E4-T3 — Registro de sesión, evento y snapshot de revocación (`docs/historias-tecnicas/E4-identidad-y-acceso.md`) |
| **HU funcional** | E4-HU-020 — Gestión de sesión y revocación (`docs/hu/E4-HU-020-sesion-y-revocacion.md`) |
| **Épica** | E4 — Identidad y acceso |
| **Repositorio** | `repos/pf-svc-identidad` (rama verificada: `feature/e4-t3-event-snapshot`) |
| **Fecha** | 2026-10-12 |

## Introduction

E4-T3 hace de `identidad` la **autoridad durable del `sesion_id`** propio de AI27: lo genera antes de la emisión, registra la sesión con el identificador de sesión del IdP (`idp_session_id`), revoca en dos pasos (local + outbox, luego IdP), publica `sesion.revocada` con payload mínimo y expone la lectura durable y el snapshot de tombstones con cursor que los consumidores usan para reconstruir su proyección (ADR 26; DEC-083).

Este spec cubre **solo el lado productor** (`identidad`). Por decisión del usuario, todo lo de los consumidores (suscripción durable por módulo, difusión a réplicas, `not-ready`, detección de canal obsoleto a 15 s, corte de 30 s y prueba extremo a extremo) queda en **E0-T4**.

> **Este spec no cierra E4-T3 por sí solo. E4-T3 cierra cuando además E0-T4 entregue el lado consumidor.**

### Cómo leer las etiquetas

Cada criterio de aceptación lleva su fuente y una de dos etiquetas normativas:

| Etiqueta | Significado |
| --- | --- |
| **(T)** | Sale literalmente de E4-T3 ("Qué construir", criterios de aceptación técnicos o definición de terminado) |
| **(F)** | Se deriva directamente de otra fuente vigente citada (contrato `identidad.yaml`, ADR 26, DEC-083, `identidad.md`, IDN-*) y es necesaria para cumplir un punto de E4-T3 |

Las propuestas de Kiro **(K)** no son criterios: están en la sección "Propuestas de Kiro (no aprobadas)", fuera de los requisitos.

### Cobertura de E4-T3

Cada ítem de E4-T3 queda en uno de tres destinos: **cubierto por este spec** (requisito), **pendiente de decisión** (A-n, sin criterio hasta resolverse) o **E0-T4** (lado consumidor).

| Ítem de E4-T3 | Origen | Cobertura |
| --- | --- | --- |
| Generación del `sesion_id` en `identidad` antes de la emisión; el IdP no lo genera | Qué construir | Cubierto: Requisito 1. Orden emisión↔registro: **A-1** |
| Tabla `sesion` con `sesion_id`, sujeto, emisión, expiración, revocación e `idp_session_id`; sin alcance; `sesion_id` estable en refresh | Qué construir | Cubierto: Requisitos 2 y 3. Valor de expiración: **A-3**; sujeto: **A-5** |
| Registro caliente `sesion:{sesion_id}`, acelerador, nunca fuente de verdad | Qué construir | Cubierto: Requisito 8. Tecnología, contenido y TTL: **A-11** |
| `DELETE /api/v1/sesiones/{id}` en dos pasos, local primero, Paso 2 con el `idp_session_id` de la fila | Qué construir | Cubierto: Requisito 4. Reintento y respuesta HTTP: **A-7**; sesión inexistente: **A-8** |
| Logout revoca la sesión actual | Qué construir | Cubierto: Requisito 4.8. Autorización: **A-9**; varias sesiones por sujeto: **A-2** |
| Revocación administrativa masiva (una revocación y un evento por `sesion_id`) | Qué construir | **Pendiente A-6**. Sin requisito de implementación |
| `POST /api/v1/sesiones/refresh` conserva el `sesion_id` | Qué construir | Cubierto: Requisito 3. Resolución de la fila: **A-4** |
| `refresh` reevalúa los claims | Qué construir | Fuera de alcance: **E4-T4** |
| Lectura durable del estado para fallback degradado | Qué construir | Cubierto: Requisito 6 |
| "Si el canal está stale y `identidad` no responde, el consumidor rechaza" | Qué construir | **E0-T4** |
| Snapshot de tombstones no expirados con `cursor`, sin ventana | Qué construir | Cubierto: Requisito 7. Semántica del cursor: **A-10** |
| Evento `sesion.revocada`: sobre común y payload mínimo; motivo, sujeto, tenant y actor en auditoría local | Qué construir | Cubierto: Requisito 5. Serialización: **A-14**; actor y motivo: **A-12** |
| Suscripción durable por módulo, difusión interna a réplicas, ACK tras el broadcast | Qué construir | **E0-T4** |
| Tombstones por réplica, snapshot + cursor, `not-ready`, fallback a 15 s, corte a 30 s | Qué construir | **E0-T4** |
| Criterio: el `sesion_id` del token es el que generó `identidad`, coincide con la fila, que registra el `idp_session_id` | Criterio | Cubierto: Requisitos 1, 2 y 9.1–9.2 |
| Criterio: el Paso 2 termina en el IdP la sesión del `idp_session_id` de la fila, y no otra del mismo sujeto | Criterio | Cubierto: Requisitos 4.2 y 9.4, **condicionado a A-2** |
| Criterio: pendiente DEC-085 (token emitido y fila no registrada) | Criterio | **Pendiente A-1**. Sin criterio |
| Criterio: fila y outbox en la misma transacción (P17); fallo del IdP no revierte el corte local | Criterio | Cubierto: Requisito 4.1, 4.5 y 10.1 |
| Criterio: todos los refresh conservan el `sesion_id` | Criterio | Cubierto: Requisitos 3 y 9.3 |
| Criterio: **revocar todas las sesiones genera exactamente un evento por cada sesión activa** | Criterio | **NO cubierto por este spec** hasta resolver **A-6** |
| Criterio: el snapshot incluye solo tombstones no expirados y un cursor sin hueco | Criterio | Cubierto: Requisitos 7 y 10.3–10.4, condicionado a **A-10** |
| Criterio: con Redis caído, Postgres responde el estado correcto para snapshot y fallback | Criterio | Cubierto: Requisitos 8.4 y 10.5 |
| Criterio: reentregar el mismo evento no produce efecto adicional (P13) | Criterio | **E0-T4** (deduplicación del consumidor). Contraparte del productor, revocación repetida sin evento nuevo: Requisito 4.7 |
| Criterio: todas las réplicas aplican la revocación; réplica nueva sin tráfico antes de converger | Criterio | **E0-T4** |
| Criterio: ACK de la suscripción después del broadcast; difusión duplicada sin efectos dobles | Criterio | **E0-T4** |
| Criterio: fallback a 15 s y máximo de 30 s | Criterio | **E0-T4** |
| DoD: prueba de emisión con correspondencia `sesion_id` ↔ `idp_session_id` en la fila | DoD | Cubierto: Requisito 9.1–9.2 |
| DoD: prueba de dos pasos con fallo del IdP | DoD | Cubierto: Requisito 10.1 |
| DoD: prueba de estabilidad del `sesion_id` en refresh | DoD | Cubierto: Requisito 9.3 |
| DoD: prueba de revocación masiva | DoD | **NO cubierto** hasta resolver **A-6** |
| DoD: prueba snapshot/cursor concurrente | DoD | Cubierto: Requisito 10.3, condicionado a **A-10** |
| DoD: prueba con Redis caído | DoD | Cubierto: Requisito 10.5 |
| DoD: contract test del payload mínimo | DoD | Cubierto: Requisito 10.6, condicionado a **A-14** |
| DoD: pruebas de suscripción por módulo y broadcast (duplicado, pérdida y alta de réplica) | DoD | **E0-T4** |
| DoD: prueba de canal obsoleto a 15 s | DoD | **E0-T4** |
| DoD: prueba extremo a extremo del máximo de 30 s | DoD | **E0-T4** |

### Estado actual del repositorio (verificado leyendo el código el 2026-10-12)

| Elemento | Estado |
| --- | --- |
| `internal/idp/port.go` | `Issue(ctx, cred Credentials, sesionID string) (Token, error)`; `Refresh(ctx, refreshToken) (Token, error)`; `Revoke(ctx, idpSessionID string) error`. `Token` = `AccessToken`, `RefreshToken`, `ExpiraEn`, `IdpSessionID`. `Credentials` incluye `SegundoFactor`. `Token` no expone el `sub` |
| `internal/idp/memory` | El doble ignora el `sesionID` y devuelve `IdpSessionID = "memory-sid-<uuid>"` |
| `internal/sesion/port.go` | `EmitirSesion(ctx, usuario, contrasena)` sin segundo factor; `RevocarSesion(ctx, sesionID)` sin actor ni motivo |
| `internal/sesion/servicio.go` | `revocarLocalConEvento` con SQL real: `UPDATE ... WHERE sesion_id = $1 AND revocada_en IS NULL RETURNING revocada_en, expira_en` y `outbox.Publish` en la misma tx; idempotente; auditoría como `TODO`. `ObtenerSesion` con SQL real y `ErrNoEncontrada`. `ListarRevocadas` con SQL real (`revocada_en IS NOT NULL AND expira_en > now()`), sin cursor (`CursorSiguiente` nulo). `EmitirSesion` y `RefrescarSesion` son scaffold (`errNoImplementado`; `sesionID` vacío con `TODO(E4-T3/DEC-085)`). Payload del evento como `map[string]any` |
| **H-1** (`RevocarSesion`) | El Paso 2 llama `s.idp.Revoke(ctx, sesionID)` con el `sesion_id` de AI27, no con el `idp_session_id` de la fila. Si el Paso 2 falla, devuelve el error sin reintento. **Se corrige en este spec** (Requisito 4.2) |
| `internal/sesion/deps.go` | Puertos `BeginTx` y `Cache` (`GuardarSesion`, `InvalidarSesion`) |
| `internal/http/handlers.go` | Handlers de las cinco operaciones de `tracer.3` con tipos generados. `RevocarSesion` y `ObtenerSesion` ya validan el `id` como UUID. `escribirError` traduce un error sin categoría a 501, así que `ErrNoEncontrada` y un fallo del Paso 2 salen como **501** |
| `migrations/identidad/0001_init.sql` | `sesion (sesion_id UUID PK, sujeto_id NOT NULL FK, emitida_en, expira_en NOT NULL, revocada_en)` **sin `idp_session_id`**; índice `una_sesion_viva ON sesion (sujeto_id) WHERE revocada_en IS NULL`; `cambio_identidad`; `outbox` con `evento_id UNIQUE` |
| `migrations/identidad/0002_grants.sql` | Existe; la migración nueva de este spec va después |
| `internal/sesion/servicio_integration_test.go` | `TestRevocarYLeer` contra Postgres (inserta la sesión directo, usa el doble): Paso 1, idempotencia, un solo evento, snapshot |
| `internal/idp/internal/kc/integration_*.go` | Suite `-tags integration` contra `quay.io/keycloak/keycloak:26.7.4` con el JAR de E4-T1 (`IDENTIDAD_KC_EXT_JAR` o `deploy/keycloak/extension/target/ai27-keycloak-ext.jar`). `arranqueKeycloak` y `emitirConTOTP` viven en el paquete `kc`. `TestRevocarUnaSesion` abre dos sesiones del mismo usuario |
| `go.mod` | `api-client-go v0.1.0-tracer.3`, `pf-pkg-platform-go v0.1.1`, `pgx/v5`, `testcontainers-go v0.44.0`. **Sin cliente de Redis** |
| `cmd/api/main.go` | `Cache: nil`; check de Redis en `/ready` como `TODO` |
| Contrato `identidad.yaml` (tag `v0.1.0-tracer.3`; `info.version` aún dice `0.1.0-tracer.2`) | Cinco operaciones; **sin operación masiva**. `revocarSesion`: 204/401/403/404, descripción con Cognito y orden invertido. `obtenerSesion`: 200/401/404. `PaginaRevocaciones.cursor_siguiente` requerido y *nullable*. `CredencialesEntrada {usuario, contrasena}` con `additionalProperties: false` |
| Evento (`sesion.proto`, `registro.tipos.yaml`) | `SesionRevocada {sesion_id, revocada_en, expira_en}`; tipo Go generado `eventosv1.SesionRevocada`; `sesion.revocada` v1, productor `identidad`, agregado `sesion_id` |

### Fuentes

- `docs/historias-tecnicas/E4-identidad-y-acceso.md`, E4-T3 (fuente principal).
- `docs/adr/0026-revocacion-local-de-sesiones.md` (ADR 26) §1, §2, §4, §7 e invariantes.
- `docs/arquitectura/services/identidad.md` (c), (e), (f), (g), (h), (i), (j).
- `.kiro/specs/ai27-microservices-architecture/requirements-identidad.md` IDN-1 a IDN-4 (el archivo existe; verificado el 2026-10-12).
- `docs/hu/E4-HU-020-sesion-y-revocacion.md`.
- `.kiro/steering/decision-log.md`: DEC-053, DEC-072, DEC-073, DEC-074, DEC-078, DEC-083, DEC-084, DEC-085.
- `docs/historias-tecnicas/E0-fundamentos-plataforma.md`: E0-T2, E0-T4, E0-T10.
- `pf-pkg-contracts` `v0.1.0-tracer.3`: `contracts/openapi/identidad.yaml`, `_comunes.yaml`, `contracts/events/ai27/eventos/v1/sesion.proto`, `registro.tipos.yaml`, `INVENTARIO.md`.
- `pf-svc-identidad/REPORTE-BRECHAS.md` §1.

## Glossary

- **Servicio_Sesion**: implementación de `sesion.Servicio` en `internal/sesion`. Dueño del ciclo de vida de sesión observable.
- **API_Sesiones**: handlers HTTP de `internal/http` para las cinco operaciones de sesiones de `identidad.yaml` (`tracer.3`).
- **Puerto_IdP**: la interfaz `idp.IdentityProvider` de `internal/idp/port.go`.
- **IdP**: Keycloak `26.7.4` exacta con la extensión JAR de E4-T1 (DEC-070, DEC-083 #5). Emite y firma los tokens y transporta el `sesion_id` al claim.
- **sesion_id**: identificador de sesión propio de AI27, generado por `identidad`, estable durante todos los refrescos (ADR 26 §1).
- **idp_session_id**: identificador nativo de la sesión en el IdP (`Token.IdpSessionID`, el `session_state` del grant, igual al claim `sid` de Keycloak). Va en inglés por ser referencia técnica al proveedor (DEC-083 nota 4).
- **Tabla_Sesion**: la tabla `identidad.sesion` en Postgres. Fuente de verdad de la revocación (identidad.md (e)).
- **Fila_Viva**: fila de la Tabla_Sesion con `revocada_en` nulo.
- **Outbox**: la tabla `identidad.outbox`, drenada por el relay de `pf-pkg-platform-go/outbox` (ADR 29).
- **Evento_SesionRevocada**: el evento `sesion.revocada` v1 de `pf-pkg-contracts`.
- **Auditoria_Local**: registro local de `identidad` donde quedan motivo, sujeto, tenant y actor de cada revocación (ADR 26 §7). En el esquema actual es la tabla `cambio_identidad`, cuyo gobierno es de E4-T6.
- **Registro_Caliente**: entrada `sesion:{sesion_id}` en la caché de sesión (Redis/Valkey según identidad.md (c)). Acelerador, nunca fuente de verdad.
- **Paso_1**: commit, en una transacción de Postgres, de la marca de revocación de la fila y de la fila del Evento_SesionRevocada en el Outbox.
- **Paso_2**: invocación de `Revoke` del Puerto_IdP con el `idp_session_id` de la fila, después del commit del Paso_1.
- **Tombstone**: revocación no expirada, con `sesion_id`, `revocada_en` y `expira_en` (ADR 26 §3, §7).
- **Snapshot_Revocaciones**: respuesta de `listarSesionesRevocadas`: tombstones no expirados más un cursor (ADR 26 §4).
- **Cursor**: valor opaco que devuelve el Snapshot_Revocaciones para continuar sin hueco (`_comunes.yaml#Cursor`). Su semántica está pendiente (A-10).
- **Keycloak_De_Prueba**: contenedor efímero de Keycloak `26.7.4` con el realm versionado y el JAR de E4-T1, levantado por testcontainers.
- **Prueba_Integracion_Keycloak**: prueba con build tag `integration` que ejercita el Servicio_Sesion contra el Keycloak_De_Prueba y un Postgres de prueba.
- **Prueba_Productor**: prueba con Postgres de prueba y un doble del Puerto_IdP, sin Keycloak.

## Requirements

### Requirement 1: Generación del `sesion_id` antes de la emisión

**User Story:** Como equipo de desarrollo de `identidad`, quiero que `identidad` genere el `sesion_id` antes de pedir el token al IdP, para que la clave de la sesión sea propia de AI27 y no del proveedor (ADR 26 §1; DEC-083 #1).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion recibe una invocación de `EmitirSesion`, THE Servicio_Sesion SHALL generar exactamente un `sesion_id` nuevo antes de invocar `Issue` del Puerto_IdP, y pasar ese mismo valor como argumento `sesionID` de esa invocación de `Issue`.
   *Fuente:* E4-T3, Qué construir ("Generación del `sesion_id` en `identidad` antes de la emisión. El adaptador del IdP lo recibe y lo transporta al token"); DEC-083 #1 y #3.
2. **(F)** THE Servicio_Sesion SHALL generar cada `sesion_id` como un valor con formato `uuid` (`_comunes.yaml#Id`) distinto de todo `sesion_id` presente en la Tabla_Sesion, sin importar si esa fila está viva, revocada o expirada.
   *Fuente:* `identidad.yaml` `SesionEmitida.sesion_id` → `_comunes.yaml#Id` (`format: uuid`); identidad.md (e) ("Único por `sesion_id`"). Necesario para el punto anterior: el id generado es el que devuelve el contrato.
3. **(T)** THE Servicio_Sesion SHALL usar como `sesion_id` de la sesión únicamente el valor generado en el criterio 1, sin sustituirlo por el `IdpSessionID` del `Token` que devuelve `Issue` ni por ningún otro identificador entregado por el IdP.
   *Fuente:* E4-T3, Qué construir ("el IdP no lo genera"); ADR 26 §1 (rechaza el identificador del proveedor como clave canónica).
4. **(F)** WHEN `EmitirSesion` termina sin error, THE Servicio_Sesion SHALL devolver en el campo `sesion_id` de la respuesta de emisión (`SesionEmitida`) el mismo valor generado en el criterio 1 y pasado a `Issue`.
   *Fuente:* `identidad.yaml` `emitirSesion` → `SesionEmitida.sesion_id` ("Identificador de sesión propio de AI27"). Necesario para el criterio de E4-T3 "El `sesion_id` del token es el que generó `identidad`".

> **Sin criterio por pendiente A-1 (DEC-085).** El orden entre `Issue` y el registro de la fila, qué hace `identidad` si el IdP emite y el registro falla, y por tanto qué cuenta como "`EmitirSesion` termina sin error", no se fijan aquí. Hasta que se decida, `EmitirSesion` no se cablea en la ruta productiva.

### Requirement 2: Registro durable de la sesión con `idp_session_id`

**User Story:** Como equipo de desarrollo de `identidad`, quiero que cada emisión deje una fila durable con el `sesion_id` de AI27 y el identificador de sesión del IdP, para que la revocación local tenga autoridad y el Paso 2 sepa qué sesión terminar en el IdP (ADR 26 §1; DEC-083 #2).

#### Acceptance Criteria

1. **(T)** WHEN `Issue` del Puerto_IdP devuelve éxito, THE Servicio_Sesion SHALL registrar en la Tabla_Sesion una sola Fila_Viva cuyo `sesion_id` es el generado en el Requisito 1.1 y que contiene el sujeto, la marca de emisión (`emitida_en`), la expiración (`expira_en`) y un `idp_session_id` idéntico al `IdpSessionID` del `Token` devuelto por esa invocación de `Issue`.
   *Fuente:* E4-T3, Qué construir (tabla `sesion`: "`sesion_id` propio, sujeto, emisión, expiración, estado de revocación e `idp_session_id` ... registrado en la emisión"); DEC-083 #2; identidad.md (e) ("Único por `sesion_id`"). *Condicionado a:* A-1 (orden y falla), A-3 (qué expiración se registra), A-5 (cómo se obtiene el sujeto).
2. **(T)** THE Tabla_Sesion SHALL contar con la columna `idp_session_id`, agregada por una migración nueva que se aplica después de `0002_grants.sql`, sin cambios en el contenido de `0001_init.sql`.
   *Fuente:* E4-T3, Qué construir; DEC-083, Impacto ("la tabla `sesion` suma `idp_session_id`"). La forma de entrega (migración nueva, sin editar `0001`) es indicación del usuario en esta sesión.
3. **(T)** THE Tabla_Sesion SHALL carecer de columnas que almacenen alcance de autorización, en particular unidad, cliente, monitoristas asignados o supervisores asignados.
   *Fuente:* E4-T3, Qué construir ("El alcance no se fija en la sesión (DEC-053)"); identidad.md (e).
4. **(T)** WHEN el Servicio_Sesion registra la fila de una emisión, THE Servicio_Sesion SHALL lograr que el claim `sesion_id` del access token emitido, el `sesion_id` de la respuesta de emisión y el `sesion_id` de la fila registrada sean el mismo valor.
   *Fuente:* E4-T3, criterio ("El `sesion_id` del token es el que generó `identidad` y coincide con la fila de `sesion`, que registra también el `idp_session_id` de esa emisión").

> **Interacción con A-2.** Con el índice actual `una_sesion_viva ON sesion (sujeto_id) WHERE revocada_en IS NULL`, el registro de una segunda emisión del mismo sujeto falla por unicidad **después** de que el IdP ya emitió el token, que es el caso de A-1. Este spec no cambia el índice hasta que A-2 se decida.

### Requirement 3: Refresco que conserva el `sesion_id`

**User Story:** Como módulo protegido, quiero que el `sesion_id` no cambie al refrescar, para que una revocación por `sesion_id` alcance a todos los tokens de la sesión (ADR 26 §1).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion devuelve una respuesta de éxito de `refrescarSesion` (`SesionEmitida`) para un `refresh_token`, THE Servicio_Sesion SHALL incluir en esa respuesta un `sesion_id` igual al `sesion_id` que devolvió la emisión (Requisito 1, criterio 4) de la que proviene ese `refresh_token`, tanto si el token salió directamente de la emisión como si salió de uno o más refrescos anteriores de la misma sesión.
   *Fuente:* E4-T3, Qué construir ("`POST /api/v1/sesiones/refresh` conserva el mismo `sesion_id`") y criterio ("Todos los refresh de una sesión conservan el mismo `sesion_id`"); `identidad.yaml` `refrescarSesion` → 200 `SesionEmitida.sesion_id` ("estable entre refrescos").
2. **(F)** WHEN el Servicio_Sesion devuelve una respuesta de éxito de `refrescarSesion`, THE Servicio_Sesion SHALL mantener registrada en la Tabla_Sesion la fila cuyo `sesion_id` es el devuelto en el criterio 1, sin insertar ninguna fila nueva en la Tabla_Sesion por ese refresco.
   *Fuente:* ADR 26 §1 ("Un refresh emite nuevos tokens, no una nueva sesión"). Necesario para que el `sesion_id` conservado corresponda a una sola fila revocable.

> **Sin criterio por pendiente A-4:** cómo identifica el Servicio_Sesion la fila de la sesión refrescada (`Refresh` no recibe ni devuelve `sesion_id`) y si `identidad` rechaza el refresco de una sesión revocada. Los criterios 1 y 2 fijan solo el resultado observable, no ese mecanismo. **Pendiente A-3:** si el refresco actualiza la expiración de la fila; el criterio 2 no exige que la fila quede sin cambios. La reevaluación de claims en refresco es E4-T4.

### Requirement 4: Revocación de una sesión en dos pasos

**User Story:** Como administrador o usuario de AI27, quiero que revocar una sesión la corte primero en AI27 y después en el IdP, para que la revocación sea efectiva aunque el IdP falle (ADR 26; identidad.md (h)).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion recibe la revocación de un `sesion_id` con Fila_Viva, THE Servicio_Sesion SHALL comprometer en una sola transacción de Postgres la asignación de `revocada_en` en esa fila y una fila del Evento_SesionRevocada para ese `sesion_id` en el Outbox, de modo que ambas queden registradas o ninguna (Paso_1).
   *Fuente:* E4-T3, Qué construir ("(1) comprometer revocación local y outbox") y criterio ("Revocar una sesión compromete la fila y el outbox en la misma transacción (P17)"). Ya implementado en `revocarLocalConEvento`.
2. **(T)** WHEN el Paso_1 se compromete, THE Servicio_Sesion SHALL invocar `Revoke` del Puerto_IdP con el `idp_session_id` registrado en la fila de ese `sesion_id`, y no con el `sesion_id` de AI27 (Paso_2).
   *Fuente:* E4-T3, Qué construir ("(2) revocar en el IdP ... con el `idp_session_id` de la fila (el adaptador no conoce la tabla; DEC-083 #2)"). Corrige **H-1**.
3. **(T)** THE Servicio_Sesion SHALL invocar el Paso_2 únicamente con el Paso_1 ya comprometido, de modo que, en el momento de invocar `Revoke`, el `revocada_en` de la fila y la fila del Evento_SesionRevocada en el Outbox sean legibles desde otra transacción de Postgres.
   *Fuente:* E4-T3, Qué construir ("Local primero"); identidad.md (h).
4. **(F)** IF el Paso_1 falla, THEN THE Servicio_Sesion SHALL devolver al llamante un error que indique que la revocación no ocurrió, sin invocar `Revoke` del Puerto_IdP, con `revocada_en` nulo en la fila y sin fila del Evento_SesionRevocada para ese `sesion_id` en el Outbox.
   *Fuente:* identidad.md (h), tabla de pasos ("La revocación no ocurrió. El cliente reintenta. Estado consistente"). Necesario para el orden de dos pasos de E4-T3.
5. **(T)** IF el Paso_2 falla después del commit del Paso_1, THEN THE Servicio_Sesion SHALL conservar sin revertir el `revocada_en` comprometido en el Paso_1 y la fila del Evento_SesionRevocada en el Outbox.
   *Fuente:* E4-T3, criterio ("un fallo posterior del IdP no revierte el corte local").
6. **(T)** IF el Paso_2 falla después del commit del Paso_1, THEN THE Servicio_Sesion SHALL volver a invocar `Revoke` con el mismo `idp_session_id` de la fila mediante el mecanismo de reintento que fije A-7.
   *Fuente:* E4-T3, Qué construir ("revocar en el IdP de forma reintentable e idempotente"). *Condicionado a:* A-7.
7. **(F)** WHEN el Servicio_Sesion recibe la revocación de un `sesion_id` cuya fila tiene `revocada_en` no nulo, THE Servicio_Sesion SHALL conservar sin cambios ese `revocada_en` y no agregar al Outbox otra fila del Evento_SesionRevocada para ese `sesion_id`.
   *Fuente:* identidad.md (h) ("la revocación es idempotente en ambos pasos, y su reintento no produce efecto adicional (P13)"). Necesario para que el reintento de E4-T3 sea idempotente. Ya implementado y probado en `TestRevocarYLeer`.
8. **(T)** WHEN el Servicio_Sesion recibe la revocación de un `sesion_id`, THE Servicio_Sesion SHALL asignar `revocada_en` solo en la fila de ese `sesion_id` y dejar sin cambios el `revocada_en` de las demás filas del mismo sujeto en la Tabla_Sesion.
   *Fuente:* E4-T3, Qué construir ("Logout revoca la sesión actual"); ADR 26 §2 ("Logout revoca únicamente el `sesion_id` actual"); E4-HU-020, escenario 1. *Condicionado a:* A-2 (con el índice actual no puede existir otra Fila_Viva del mismo sujeto) y A-9 (quién puede revocar qué `sesion_id`).

> **Pendientes de este requisito:** respuesta HTTP cuando el Paso_1 se comprometió y el Paso_2 falló (A-7; hoy sale 501); `DELETE` de una sesión inexistente (A-8); revocación masiva (A-6).

### Requirement 5: Evento `sesion.revocada` y auditoría local

**User Story:** Como módulo consumidor, quiero recibir solo lo necesario para mantener mis tombstones, y como equipo de seguridad quiero que el detalle de cada revocación quede en `identidad`, para no distribuir datos de sujeto a todos los consumidores (ADR 26 §7).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion escribe en el Outbox la fila del Evento_SesionRevocada durante el Paso_1, THE Servicio_Sesion SHALL incluir en su payload exactamente los campos `sesion_id`, `revocada_en` y `expira_en`, sin ningún campo adicional; en particular, sin motivo, sujeto, tenant, actor ni claims.
   *Fuente:* E4-T3, Qué construir ("payload mínimo `sesion_id`, `revocada_en`, `expira_en`. Motivo, sujeto, tenant y actor permanecen en auditoría local"); ADR 26 §7 ("incluye únicamente"; "`motivo`, `sujeto_id`, `tenant_id`, actor y claims anteriores permanecen en la auditoría de `identidad`"); `sesion.proto`. *Condicionado a:* A-14 (forma de serialización).
2. **(T)** WHEN el Servicio_Sesion escribe en el Outbox la fila del Evento_SesionRevocada durante el Paso_1, THE Servicio_Sesion SHALL dejarla dentro del sobre común, con un `evento_id`, el tipo de evento `sesion.revocada`, la versión de esquema 1 y la fecha/hora del evento.
   *Fuente:* E4-T3, Qué construir ("sobre común"); los campos del sobre son **(F)** de ADR 26 §7; el tipo y la versión son **(F)** de `registro.tipos.yaml` (`sesion.revocada`, `version: 1`, productor `identidad`).
3. **(T)** WHEN el Paso_1 de la revocación de un `sesion_id` se compromete, THE Servicio_Sesion SHALL registrar en la Auditoria_Local, asociados a ese `sesion_id`, el sujeto, el tenant, el actor y el motivo de esa revocación.
   *Fuente:* E4-T3, Qué construir ("permanecen en auditoría local"); ADR 26 §7; E4-HU-020 ("Toda revocación queda registrada"); IDN-3.9. *Condicionado a:* A-12 (el puerto no recibe actor ni motivo; si el motivo es obligatorio está pendiente), A-5 (sujeto) y A-16 (tenant).

> **Pendientes de este requisito:** serialización del payload (A-14); actor y motivo (A-12); origen del sujeto (A-5) y del tenant (A-16). Que la auditoría vaya en la misma transacción del Paso_1 y que sea append-only no se exige aquí (K-6 y K-7).

### Requirement 6: Lectura durable del estado de una sesión

**User Story:** Como módulo protegido en modo degradado, quiero consultar en `identidad` el estado durable de un `sesion_id`, para decidir sin depender del canal de eventos (ADR 26 §5).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion recibe la consulta de estado de un `sesion_id` con fila en la Tabla_Sesion, THE Servicio_Sesion SHALL devolver, leídos de esa fila de la Tabla_Sesion, el `sesion_id` consultado, `revocada` en verdadero si la fila tiene `revocada_en` no nulo y en falso si es una Fila_Viva, el `revocada_en` de la fila (nulo si es una Fila_Viva) y el `expira_en` de la fila.
   *Fuente:* E4-T3, Qué construir ("Lectura durable del estado de una sesión para fallback degradado"); `identidad.yaml` `obtenerSesion` → `EstadoSesion` (requeridos `sesion_id`, `revocada`, `expira_en`; `revocada_en` *nullable*, "null si no aplica"; descripción "Fuente durable de un sesion_id desde Postgres"). Ya implementado en `ObtenerSesion`.
2. **(F)** IF la API_Sesiones recibe la consulta de estado de un `sesion_id` con formato UUID que no tiene fila en la Tabla_Sesion, THEN THE API_Sesiones SHALL responder con la respuesta `NoEncontrado` (404) del contrato, sin cuerpo `EstadoSesion`.
   *Fuente:* `identidad.yaml` `obtenerSesion`, respuesta `"404"`. Necesario para la lectura durable de E4-T3: hoy `ErrNoEncontrada` sale como 501 y el consumidor en fallback no distingue "no existe" de "no implementado". Qué hace el consumidor con un 404 es de E0-T4 y DEC-085.

### Requirement 7: Snapshot de tombstones con cursor

**User Story:** Como módulo protegido que arranca o se reconstruye, quiero un snapshot de las revocaciones vigentes y un cursor para continuar, para no reabrir una sesión revocada durante la reconstrucción (ADR 26 §4).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion recibe una solicitud de Snapshot_Revocaciones sin cursor, THE Servicio_Sesion SHALL devolver como Tombstones únicamente las filas de la Tabla_Sesion que tienen marca de revocación y cuya `expira_en` es estrictamente posterior al instante de la consulta, cada uno con `sesion_id`, `revocada_en` y `expira_en`, sin incluir Filas_Vivas ni revocaciones con `expira_en` igual o anterior a ese instante.
   *Fuente:* E4-T3, Qué construir ("Snapshot consistente de tombstones no expirados") y criterio ("El snapshot incluye sólo tombstones no expirados"); ADR 26 §7. Ya implementado sin cursor. *Condicionado a:* A-3 (qué `expira_en` lleva la fila).
2. **(T)** WHEN el Servicio_Sesion responde una solicitud de Snapshot_Revocaciones, con o sin Cursor, THE Servicio_Sesion SHALL incluir en la respuesta un Cursor, que es el valor con el que el consumidor solicita la continuación descrita en el criterio 3.
   *Fuente:* E4-T3, Qué construir ("con `cursor`; permite continuar el canal sin ventana"). Hoy `CursorSiguiente` es nulo. *Condicionado a:* A-10 (incluido qué significa `cursor_siguiente: null`).
3. **(T)** WHEN el Paso_1 de una revocación se compromete después de iniciada una solicitud de Snapshot_Revocaciones y antes de que el consumidor solicite la continuación con el Cursor de esa respuesta, THE Servicio_Sesion SHALL entregar esa revocación, mientras su `expira_en` sea posterior al instante de la solicitud de continuación, en el snapshot o en la respuesta obtenida con ese Cursor.
   *Fuente:* E4-T3, criterio ("un cursor que permite aplicar sin hueco las revocaciones comprometidas durante la reconstrucción"); ADR 26 §4. *Condicionado a:* A-10.

> ADR 26 §4 también pide que snapshot y cursor impidan "una doble aplicación dañina", y aclara que la reentrega es inocua por `evento_id` y `sesion_id`. Este spec no exige al productor evitar duplicados.

### Requirement 8: Registro caliente como acelerador

**User Story:** Como equipo de operación, quiero que la caché de sesión acelere lecturas sin decidir nunca el estado, para que una caída de la caché no abra ni cierre sesiones (identidad.md (e), (i)).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Sesion registra en la Tabla_Sesion la fila de una sesión emitida (Requisito 2), THE Servicio_Sesion SHALL escribir el Registro_Caliente cuya clave es `sesion:{sesion_id}`, con el `sesion_id` de esa fila.
   *Fuente:* E4-T3, Qué construir ("Registro caliente en Redis `sesion:{sesion_id}`"); IDN-1.6. *Condicionado a:* A-11 (tecnología, contenido, TTL y si se entrega adaptador o solo puerto).
2. **(T)** THE Servicio_Sesion SHALL tomar el estado de revocación de una sesión únicamente de la Tabla_Sesion, de modo que la presencia, la ausencia o el contenido del Registro_Caliente de esa sesión no cambien el resultado de la consulta de estado (Requisito 6), del Snapshot_Revocaciones (Requisito 7) ni de la revocación repetida (Requisito 4.7); en particular, la ausencia del Registro_Caliente no se interpreta como sesión viva.
   *Fuente:* E4-T3, Qué construir ("acelerador, nunca fuente de verdad; ausencia de clave no significa 'sesión viva'"); identidad.md (e).
3. **(F)** IF una operación sobre el Registro_Caliente devuelve error o no responde durante una emisión, un refresco o una revocación, THEN THE Servicio_Sesion SHALL completar esa operación con el mismo resultado observable que sin la falla, sin revertir lo comprometido en la Tabla_Sesion y sin devolver al llamante un error causado por el Registro_Caliente.
   *Fuente:* identidad.md (i), tabla por dependencia ("Redis / Valkey — Qué sigue funcionando: Emisión, refresco y revocación comprometida"). Necesario para "nunca fuente de verdad" de E4-T3. *Condicionado a:* A-1 y A-4 (emisión y refresco aún sin cablear) y A-11.
4. **(T)** WHILE la caché de sesión rechaza o no responde las operaciones sobre el Registro_Caliente, THE Servicio_Sesion SHALL responder la consulta de estado (Requisito 6) y el Snapshot_Revocaciones (Requisito 7) desde la Tabla_Sesion, sin error atribuible a la caché y con el mismo resultado que con el Registro_Caliente disponible.
   *Fuente:* E4-T3, criterio ("Con Redis caído, Postgres responde el estado correcto para snapshot y fallback"); IDN-4.6.

> **Pendientes de este requisito (sin criterio):** contenido, TTL y tecnología de la entrada, qué hace la revocación con ella (K-8) y si se entrega adaptador o solo puerto (A-11). Operar con `Cache` nulo es propuesta K-3, no criterio.

### Requirement 9: Prueba de integración contra Keycloak 26.7.4

**User Story:** Como equipo de desarrollo, quiero verificar contra el IdP real la correspondencia entre `sesion_id`, fila e `idp_session_id`, para cerrar en E4-T3 los criterios que dependen del IdP sin delegarlos a E4-T1.

#### Acceptance Criteria

1. **(T)** WHEN la Prueba_Integracion_Keycloak emite una sesión mediante el Servicio_Sesion contra el Keycloak_De_Prueba y la emisión devuelve éxito, THE Prueba_Integracion_Keycloak SHALL verificar que el claim `sesion_id` del access token emitido es igual al `sesion_id` de la respuesta de emisión y al `sesion_id` de la fila registrada en la Tabla_Sesion para esa emisión.
   *Fuente:* E4-T3, criterio 1 y DoD ("Prueba de emisión que verifica la correspondencia `sesion_id` ↔ `idp_session_id` en la fila"). *Condicionado a:* A-15 (la ruta de emisión no transporta el segundo factor que exige el realm).
2. **(T)** WHEN la Prueba_Integracion_Keycloak emite la sesión del criterio 1, THE Prueba_Integracion_Keycloak SHALL verificar que el `idp_session_id` de la fila registrada para esa emisión es igual al claim `sid` del mismo access token.
   *Fuente:* E4-T3, criterio 1; DEC-083 #2. *Condicionado a:* A-15.
3. **(T)** WHEN la Prueba_Integracion_Keycloak refresca la sesión del criterio 1 dos o más veces consecutivas mediante el Servicio_Sesion, THE Prueba_Integracion_Keycloak SHALL verificar que (a) cada respuesta de refresco trae el `sesion_id` de la fila, (b) el claim `sesion_id` de cada access token refrescado es igual a ese mismo valor, y (c) el número de filas del sujeto en la Tabla_Sesion después del último refresco es igual al que había antes del primero.
   *Fuente:* E4-T3, criterio ("Todos los refresh de una sesión conservan el mismo `sesion_id`") y DoD ("prueba de estabilidad de `sesion_id` en refresh"); el inciso (c) verifica el Requisito 3.2 (ADR 26 §1, "no una nueva sesión"). *Condicionado a:* A-4, A-15.
4. **(T)** WHEN la Prueba_Integracion_Keycloak emite dos sesiones del mismo sujeto mediante el Servicio_Sesion y la revocación de una de ellas mediante el Servicio_Sesion devuelve éxito, THE Prueba_Integracion_Keycloak SHALL verificar que el Keycloak_De_Prueba rechaza el refresco con el refresh token de la sesión revocada y acepta el refresco con el refresh token de la otra sesión.
   *Fuente:* E4-T3, criterio ("El paso 2 de la revocación termina en el IdP la sesión identificada por el `idp_session_id` de la fila, y no otra del mismo sujeto"). *Condicionado a:* A-2 (con el índice actual la segunda emisión no se puede registrar), A-15.
5. **(T)** THE Prueba_Integracion_Keycloak SHALL ejecutarse solo con el build tag `integration`, contra el Keycloak_De_Prueba en la versión exacta `26.7.4` con la extensión JAR de E4-T1 montada, y contra un Postgres de prueba con todas las migraciones del esquema `identidad` aplicadas.
   *Fuente:* E4-T3, DoD; DEC-083 #5 (versión exacta). Reutilizar el arranque de la suite existente (`internal/idp/internal/kc/integration_*`) es indicación del usuario.
6. **(F)** IF Docker no está disponible al iniciar la Prueba_Integracion_Keycloak, THEN THE Prueba_Integracion_Keycloak SHALL omitirse con `t.Skip` antes de ejecutar cualquier verificación de los criterios 1 a 4, con un mensaje que indique que Docker no está disponible.
   *Fuente:* DEC-074 (`t.Skip` solo por Docker no disponible).
7. **(F)** IF el arranque del Keycloak_De_Prueba o del Postgres de prueba falla por una causa distinta de Docker no disponible, THEN THE Prueba_Integracion_Keycloak SHALL marcarse como fallida, sin omitirse con `t.Skip`, con un mensaje que indique cuál de los dos arranques falló.
   *Fuente:* DEC-074. Necesario para que la DoD de E4-T3 no quede omitida en silencio.

> **Notas para diseño.** (1) `arranqueKeycloak` y `emitirConTOTP` están en el paquete `kc` bajo `internal/idp/internal`, que por la regla `internal` de Go solo se importa desde `internal/idp/...`; además E4-T1 R2.1 concentra en ese árbol todo código que construye peticiones a Keycloak, incluidas las pruebas. Dónde vive esta prueba o su arnés se decide en diseño. (2) El realm exige OTP (DEC-071) y `sesion.Servicio.EmitirSesion` no recibe segundo factor (A-15). (3) Los criterios 1 y 2 prueban el camino exitoso, que es el mismo en las opciones 1 y 2 de DEC-085; no prejuzgan A-1.

### Requirement 10: Pruebas del productor contra Postgres

**User Story:** Como equipo de desarrollo, quiero pruebas del lado productor con Postgres real y un doble del IdP, para cubrir los ítems de la DoD de E4-T3 que no necesitan Keycloak.

#### Acceptance Criteria

1. **(T)** WHEN la Prueba_Productor revoca, mediante el Servicio_Sesion, una Fila_Viva cuyo `idp_session_id` es distinto de su `sesion_id`, con un doble del Puerto_IdP cuyo `Revoke` devuelve error en cada invocación, THE Prueba_Productor SHALL verificar, después de que la revocación retorna, que la fila tiene `revocada_en` no nulo, que el Outbox contiene exactamente una fila del Evento_SesionRevocada para ese `sesion_id`, que el doble registró al menos una invocación de `Revoke` y que cada invocación registrada recibió el `idp_session_id` de la fila.
   *Fuente:* E4-T3, DoD ("prueba de dos pasos con fallo del IdP") y criterio (P17; "un fallo posterior del IdP no revierte el corte local").
2. **(F)** WHEN la Prueba_Productor provoca que la transacción del Paso_1 no se comprometa al revocar una Fila_Viva, THE Prueba_Productor SHALL verificar que la revocación devuelve error, que la fila conserva `revocada_en` nulo, que el Outbox contiene cero filas del Evento_SesionRevocada para ese `sesion_id` y que el doble del Puerto_IdP registró cero invocaciones de `Revoke`.
   *Fuente:* identidad.md (h), tabla de pasos. Complementa la prueba de dos pasos de la DoD.
3. **(T)** WHEN la Prueba_Productor compromete al menos una revocación entre el inicio de la solicitud de un Snapshot_Revocaciones y la última respuesta que obtiene con el Cursor de ese snapshot, THE Prueba_Productor SHALL verificar que cada revocación comprometida en ese intervalo aparece, con su `sesion_id`, en el snapshot o en al menos una de las respuestas obtenidas con el Cursor.
   *Fuente:* E4-T3, DoD ("prueba snapshot/cursor concurrente"). *Condicionado a:* A-10.
4. **(T)** WHEN la Prueba_Productor solicita un Snapshot_Revocaciones con al menos una sesión revocada con `expira_en` posterior al instante de la consulta, al menos una sesión revocada con `expira_en` anterior a ese instante y al menos una Fila_Viva en la Tabla_Sesion, THE Prueba_Productor SHALL verificar que el snapshot contiene únicamente sesiones revocadas con `expira_en` posterior al instante de la consulta, cada una con `sesion_id`, `revocada_en` y `expira_en` iguales a los de su fila, y que no contiene ninguna sesión revocada expirada ni ninguna Fila_Viva.
   *Fuente:* E4-T3, criterio ("El snapshot incluye sólo tombstones no expirados").
5. **(T)** WHEN la Prueba_Productor ejecuta la consulta de estado y el Snapshot_Revocaciones sobre el mismo conjunto de filas de la Tabla_Sesion, una vez con el Registro_Caliente disponible y otra con toda operación sobre el Registro_Caliente fallando, THE Prueba_Productor SHALL verificar que ambas ejecuciones devuelven los mismos `sesion_id` y los mismos valores de estado de revocación, `revocada_en` y `expira_en`.
   *Fuente:* E4-T3, DoD ("prueba con Redis caído") y criterio. *Condicionado a:* A-11 (si no hay adaptador real, la prueba usa un doble de `Cache` que falla).
6. **(T)** WHEN la Prueba_Productor lee la fila del Evento_SesionRevocada que el Paso_1 dejó en el Outbox, THE Prueba_Productor SHALL verificar que el payload contiene exactamente los campos `sesion_id`, `revocada_en` y `expira_en`, que se decodifica sin error en el tipo generado `eventosv1.SesionRevocada` de `pf-pkg-contracts` `v0.1.0-tracer.3` y que los tres valores decodificados son iguales al `sesion_id`, `revocada_en` y `expira_en` de la fila revocada en la Tabla_Sesion.
   *Fuente:* E4-T3, DoD ("contract test del payload mínimo"); `sesion.proto`. *Condicionado a:* A-14.

## Propuestas de Kiro (no aprobadas)

No son criterios ni reglas. Se registran para que Product Management o Arquitectura decidan si alguna pasa a requisito.

| # | Propuesta | Por qué se propone | Relación |
| --- | --- | --- | --- |
| K-1 | Rechazar la emisión si el IdP no devuelve `IdpSessionID` | Sin ese valor el Paso_2 no puede terminar la sesión en el IdP | Requisito 2.1; A-1 |
| K-2 | Persistir el reintento pendiente del Paso_2 para que sobreviva a un reinicio del proceso | Un reintento solo en memoria se pierde al reiniciar; identidad.md (j) pide que estos fallos "se drenen" | A-7 |
| K-3 | Operar sin adaptador de caché configurado (`Cache` nulo) | El código y `cmd/api` ya lo hacen; ninguna fuente lo exige como comportamiento | Requisito 8; A-11 |
| K-4 | Dos revocaciones concurrentes del mismo `sesion_id` dejan un solo Evento_SesionRevocada | identidad.md (h) (P13) habla de reintento, no de concurrencia; ADR 26 no lo cita | Requisito 4.7 |
| K-5 | Responder error de validación (400) ante un Cursor inválido o desconocido | El contrato no declara 400 para `listarSesionesRevocadas` | A-10 |
| K-6 | Registrar la Auditoria_Local en la misma transacción del Paso_1 | El `TODO` de `revocarLocalConEvento` lo sugiere; E4-T3 y ADR 26 §7 no lo exigen | Requisito 5.3 |
| K-7 | Tratar la Auditoria_Local (`cambio_identidad`) como append-only | Es gobierno de E4-T6; en E4-T3 solo cabe que guarde motivo, sujeto, tenant y actor | Requisito 5.3; E4-T6 |
| K-8 | Invalidar o marcar el Registro_Caliente al revocar | El código lo hace como best-effort; ninguna fuente de E4-T3 lo pide | A-11 |
| K-9 | Si `Issue` devuelve error, no devolver ningún `sesion_id` ni reutilizar el valor generado | Toca A-1 | R1; A-1 |
| K-10 | Fijar la versión de UUID del `sesion_id` (v4 o v7) | El contrato solo fija `format: uuid` | R1.2 |
| K-11 | Que el doble `internal/idp/memory` registre el `sesionID` recibido y lo refleje en el claim `sesion_id`, para verificar R1/R2.4 sin Keycloak | Hoy el doble ignora ese argumento | R1, R2.4, R9 |
| K-12 | Declarar `idp_session_id` como `NOT NULL` en la migración nueva | Ninguna fuente fija nulabilidad ni tipo | R2.2; K-1 |
| K-13 | Fijar qué instante es `emitida_en` (registro de la fila o respuesta de `Issue`) | E4-T3 solo dice "emisión"; roza A-1 | R2.1; A-1 |
| K-14 | El claim `sesion_id` del access token refrescado es igual al `sesion_id` de la respuesta de refresco | E4-T3 habla del `sesion_id` de la respuesta; el reemitido depende de la extensión de E4-T1 | R3.1; R9.3 |
| K-15 | Si `Refresh` falla, responder error sin insertar ni modificar filas | Ninguna fuente lo fija; se cruza con A-4 | R3; A-4 |
| K-16 | El refresco no cambia el `idp_session_id` de la fila | No verificado si Keycloak conserva `session_state` al refrescar | R3; R4.2 |
| K-17 | Revocar de nuevo un `sesion_id` ya revocado vuelve a invocar el Paso_2 con su `idp_session_id` | identidad.md (h) no aclara si repetir el `DELETE` reejecuta el Paso_2 | R4.7; A-7 |
| K-18 | La revocación repetida responde igual que la primera (éxito) | P13 habla de efecto, no de respuesta | R4.7; A-8 |
| K-19 | Definir la respuesta HTTP de `DELETE` cuando falla el Paso_1 | El contrato solo declara 204/401/403/404 | R4.4; A-7 |
| K-20 | Los valores `revocada_en` y `expira_en` del payload son iguales a los de la fila marcada | ADR 26 §7 pide los campos, no la igualdad de valores literalmente | R5.1; R10.6 |
| K-21 | Cada Paso_1 comprometido genera un `evento_id` distinto | La deduplicación del consumidor depende del `evento_id` | R5.2 |
| K-22 | Cada Paso_1 deja exactamente una entrada de Auditoria_Local y la revocación repetida ninguna | "Toda revocación queda registrada" no fija cuántas | R5.3; R4.7 |
| K-23 | La fecha/hora del sobre es igual al `revocada_en` | Ninguna fuente fija el instante del sobre | R5.2 |
| K-24 | Una fila con `expira_en` vencido y `revocada_en` nulo responde 200 con `revocada` falso, no 404 | Ninguna fuente lo explicita | R6.1; E0-T4 |
| K-25 | Definir la respuesta ante `id` no UUID en `obtenerSesion` | El contrato solo declara 200/401/404 | R6.2 |
| K-26 | Precisar qué llamador puede consultar el estado (401) | E4-T3 no fija la autenticación servicio a servicio | R6; A-9 |
| K-27 | El Cursor de una continuación sirve para otra continuación y la garantía sin hueco se mantiene en la cadena | Fuentes hablan de un solo cursor | R7.3; R10.3; A-10 |
| K-28 | Una continuación excluye revocaciones ya expiradas, igual que el snapshot inicial | R7.1 solo lo pide sin cursor | R7.1; R7.3; A-3; A-10 |
| K-29 | Escribir el Registro_Caliente solo después del commit de la fila | Ninguna fuente fija el orden | R8.1; A-1; A-11 |
| K-30 | Reescribir el Registro_Caliente en cada refresco | Ninguna fuente lo dice; depende del TTL | R8.1; A-3; A-4; A-11 |
| K-31 | Tiempo máximo de espera para operaciones sobre el Registro_Caliente | Sin umbral, "no responde" no es medible | R8.3; R8.4; A-11 |
| K-32 | `/ready` reporta la caché caída sin marcar `identidad` como no lista | failure-modes.md clasifica Redis caído como D1; el check es `TODO` | R8; A-11 |
| K-33 | En R9.3 cada refresco usa el refresh token del refresco anterior (cadena) | "Consecutivas" admite dos lecturas con rotación | R9.3; A-4 |
| K-34 | En R9.2 verificar que `idp_session_id` y `sid` no estén vacíos | Dos vacíos pasan la igualdad | R9.2; K-1 |
| K-35 | Si falla aplicar las migraciones, la prueba de integración falla sin `t.Skip` e indica la etapa | R9.7 solo cubre arranque de contenedores | R9.5; R9.7 |
| K-36 | En R9.4 intentar el refresco directo contra el Keycloak_De_Prueba, no vía Servicio_Sesion | Así el rechazo prueba el Paso_2 y no depende de A-4 | R9.4; A-4 |
| K-37 | R10.4 verifica también completitud del snapshot | E4-T3 dice "sólo"; completitud depende del cursor | R7.1; R10.4; A-10 |
| K-38 | Fijar carga y repeticiones de la prueba concurrente 10.3 | Sin eso la carrera puede no ocurrir | R10.3; A-10 |
| K-39 | Verificar la Auditoria_Local en la Prueba_Productor | La DoD no lista prueba de auditoría | R5.3; A-5; A-12 |
| K-40 | Extender el contract test 10.6 al sobre común | La DoD pide solo el payload | R5.2; R10.6 |

## Pendientes de decisión (no resueltos por este spec)

| # | Pendiente | Fuente | DEC / owner | Qué condiciona |
| --- | --- | --- | --- | --- |
| A-1 | **Orden emisión↔registro.** Si el IdP emite y falla el registro de la fila, queda un token válido cuya sesión no es revocable | DEC-085; E4-T3, criterio "Pendiente (DEC-085)" | DEC-085 PENDIENTE; Arquitectura (propuesto) | Bloquea el cableado productivo de `EmitirSesion`. Sin criterio |
| A-2 | **`una_sesion_viva` por sujeto.** `0001_init.sql` crea el índice único sobre `(sujeto_id)`. identidad.md (e) lo escribe sobre `identidad.sesion (id)` y describe la tabla como "Índice por `sujeto_id`. Único por `sesion_id`"; no fija una sola sesión viva por sujeto. Lo contradicen E4-T3 ("y no otra del mismo sujeto"), ADR 26 §2, E4-HU-020 escenario 2 ("una o varias sesiones activas") y `TestRevocarUnaSesion` de E4-T1 | `0001_init.sql`; identidad.md (e); E4-T3; ADR 26 §2; E4-HU-020 | Sin DEC; Arquitectura (propuesto: Fabricio Fuentes, owner de ADR 26) | Requisitos 4.8 y 9.4; además empuja una segunda emisión al caso de A-1 |
| A-3 | **`expira_en` de la sesión.** La columna es `NOT NULL`; `Token.ExpiraEn` es la expiración del access token y cada refresco emite uno más tardío. `sesion.proto` dice que el tombstone "puede descartarse después de este instante". Si la fila guarda la primera expiración y no se actualiza, el snapshot deja de listar la revocación mientras un token refrescado sigue vigente | `0001_init.sql`; `sesion.proto`; `idp.Token`; DEC-078 (duración máxima de sesión, PENDIENTE) | Sin DEC; Arquitectura (propuesto) | Requisitos 2.1 y 7.1 |
| A-4 | **Refresco.** `Refresh(ctx, refreshToken)` no recibe ni devuelve `sesion_id`: falta definir cómo se resuelve la fila (p. ej. desde el claim del token nuevo). **Pregunta abierta:** ¿`identidad` rechaza el refresco de una sesión revocada, o lo deja al IdP tras el Paso_2? | `idp/port.go`; E4-T3; ADR 26 §1 | Sin DEC; Arquitectura (propuesto) | Requisitos 3 y 9.3 |
| A-5 | **`sujeto_id` de la fila.** `sesion.sujeto_id` es `NOT NULL` con FK a `sujeto`; `idp.Token` no expone `sub`. DEC-072 resuelve `SujetoID` en servidor por `IdpSubject` y deja el mecanismo a E4-T6. Falta definir de dónde sale el `sub` en la emisión y qué pasa si el `sujeto` no existe | DEC-072; `0001_init.sql`; `idp/port.go` | DEC-072 (owner Arquitectura) no lo fija | Requisitos 2.1 y 5.3 |
| A-6 | **Revocación masiva.** (a) **Brecha contractual:** `identidad.yaml` `tracer.3` solo tiene `DELETE /api/v1/sesiones/{id}`; REPORTE-BRECHAS.md §1: "No se deben crear tipos de wire, rutas ni eventos manualmente". (b) **Interpretación sin confirmar:** E4-T3 dice "Una revocación administrativa masiva enumera sesiones activas" y "Revocar todas las sesiones genera exactamente un evento por cada sesión activa", sin decir si es por sujeto o global. ADR 26 §2, literal: "Una acción administrativa o desactivación que deba cortar todas las sesiones identifica las sesiones activas y compromete una revocación por cada una"; invariante: "Revocar todas las sesiones produce un evento por cada sesión activa". E4-HU-020, literal: "El administrador puede **revocar todas las sesiones activas** de un usuario; el sistema publica un `sesion.revocada` por cada una". `sesion.proto` y E0-T4 dicen "de un sujeto". Ninguna fuente habla de una revocación global; la lectura "por sujeto" tiene apoyo en E4-HU-020, `sesion.proto` y E0-T4, pero falta confirmarla | E4-T3; ADR 26 §2; E4-HU-020; `sesion.proto`; E0-T4; IDN pendiente 11 | Sin DEC; PM + Arquitectura (propuesto); contrato: owner de `pf-pkg-contracts` | Criterio y DoD de revocación masiva de E4-T3: **NO cubiertos** |
| A-7 | **Reintento del Paso_2 y respuesta HTTP.** Mecanismo (síncrono, worker, outbox propio) no definido. Si el Paso_1 se comprometió y el Paso_2 falló, hoy sale 501; el contrato solo declara 204/401/403/404 | E4-T3; identidad.md (h), (j); `identidad.yaml` `revocarSesion` | Sin DEC; Arquitectura (propuesto) | Requisito 4.6 |
| A-8 | **`DELETE` de sesión inexistente.** El código lo trata como no-op exitoso (204); el contrato declara 404. Además, la descripción de `revocarSesion` en el contrato invierte el orden ("revoca en Cognito y compromete la revocación local") y menciona Cognito, contra identidad.md (h) | `servicio.go`; `identidad.yaml` | Sin DEC; Arquitectura + owner de `pf-pkg-contracts` | Requisito 4 (caso no cubierto) |
| A-9 | **Quién puede revocar qué `sesion_id`.** El contrato declara 403 ("fuera de alcance/rol"); identidad.md (f) lista audiencia "cliente · interno · admin"; E4-HU-020 distingue logout propio y revocación administrativa. No está definido si un no administrador puede revocar un `sesion_id` distinto del propio ni cómo se obtiene el `sesion_id` del llamante | `identidad.yaml`; identidad.md (f); E4-HU-020 | Sin DEC; PM + Seguridad (propuesto) | Requisito 4.8 |
| A-10 | **Semántica del Cursor.** ADR 26 §4 paso 3 pide consumir "desde el punto posterior al cursor hasta alcanzar la posición vigente del canal", pero E0-T10 dice "V1 no ofrece replay general del broker por offset o tiempo" y E0-T2 pide distinguir "el cursor o revisión de cambios del cursor de paginación". El contrato lo define como "opaco de paginación o de catch-up" y no dice qué significa `cursor_siguiente: null` | ADR 26 §4; E0-T2; E0-T10; `_comunes.yaml#Cursor` | Sin DEC; Arquitectura (owner de E0-T2/E0-T10) | Requisitos 7.2–7.3 y 10.3 |
| A-11 | **Caché de sesión.** Tecnología (Redis o Valkey), contenido de la entrada, TTL, qué hace la revocación con la entrada (K-8) y si E4-T3 entrega adaptador o solo puerto. Hoy no hay cliente en `go.mod`, `Cache` es nulo y el check de `/ready` es `TODO`. identidad.md (c) dice que `realtime` lee esta entrada, y ADR 26 descarta Redis compartido en la ruta normal: falta aclarar ese uso | E4-T3; identidad.md (c), (e); ADR 26 (alternativas); `go.mod`; `cmd/api/main.go` | Sin DEC; Arquitectura (propuesto) | Requisitos 8.1 y 10.5 |
| A-12 | **Actor y motivo de la auditoría.** `RevocarSesion(ctx, sesionID)` no los recibe; `DELETE` no tiene cuerpo para el motivo. Si el motivo es obligatorio está pendiente en E4-HU-020 | `sesion/port.go`; `identidad.yaml`; E4-HU-020; requirements-identidad pendiente 9 | Sin DEC; PM (motivo) + Arquitectura (actor) | Requisito 5.3 |
| A-13 | **Usuario en más de un grupo de rol.** Afecta la emisión (qué `rol` lleva el token), no la fila de sesión | DEC-084 | DEC-084 PENDIENTE; PM + Arquitectura (propuesto) | Dependencia; no bloquea criterios de este spec |
| A-14 | **Serialización del payload.** `servicio.go` arma el payload como `map[string]any` (JSON con `time.Time`), aunque existe el tipo generado `eventosv1.SesionRevocada` (proto con `Timestamp`) y la regla del repo prohíbe tipos de wire a mano | `servicio.go`; `sesion.proto`; `handlers.go` (regla 6); REPORTE-BRECHAS.md §1 | Sin DEC; Arquitectura (propuesto) | Requisitos 5.1 y 10.6 |
| A-15 | **Segundo factor en la ruta de emisión.** `sesion.Servicio.EmitirSesion(ctx, usuario, contrasena)` y `CredencialesEntrada {usuario, contrasena}` (`additionalProperties: false`) no transportan `SegundoFactor`, y el realm exige OTP (DEC-071). Ampliar el puerto interno es decisión de diseño; el campo en el contrato es brecha contractual | `sesion/port.go`; `identidad.yaml`; DEC-071; spec E4-T1 A-3 | Sin DEC; Arquitectura + owner de `pf-pkg-contracts` | Requisito 9 (la emisión por el Servicio_Sesion falla sin factor) |
| A-16 | **Tenant de la auditoría.** R5.3 pide registrar el tenant en la Auditoria_Local, pero ningún pendiente dice de dónde sale: A-5 cubre solo el sujeto, A-12 actor y motivo, y la Tabla_Sesion no tiene tenant | E4-T3; ADR 26 §7; `0001_init.sql` | Sin DEC; Arquitectura (propuesto) | Requisito 5.3 |

## Trazabilidad

| Requisito | Fuente principal |
| --- | --- |
| R1 Generación del `sesion_id` | E4-T3 (Qué construir); DEC-083 #1, #3; ADR 26 §1; `identidad.yaml` `SesionEmitida` |
| R2 Registro con `idp_session_id` | E4-T3 (Qué construir, criterio 1); DEC-083 #2; DEC-053; identidad.md (e) |
| R3 Refresco | E4-T3 (Qué construir, criterio); ADR 26 §1 |
| R4 Revocación en dos pasos | E4-T3 (Qué construir, criterios); identidad.md (h); ADR 26 §2; E4-HU-020 |
| R5 Evento y auditoría | E4-T3 (Qué construir); ADR 26 §7; `sesion.proto`; E4-HU-020; IDN-3.9 |
| R6 Lectura durable | E4-T3 (Qué construir); `identidad.yaml` `obtenerSesion` |
| R7 Snapshot y cursor | E4-T3 (Qué construir, criterio); ADR 26 §4, §7 |
| R8 Registro caliente | E4-T3 (Qué construir, criterio); identidad.md (e), (i); IDN-1.6, IDN-4.6 |
| R9 Integración con Keycloak | E4-T3 (criterios, DoD); DEC-083 #2, #5; DEC-074 |
| R10 Pruebas del productor | E4-T3 (criterios, DoD); identidad.md (h); `sesion.proto` |

## Dependencias

- **E4-T1** — adaptador del IdP: `Issue` con transporte del `sesion_id`, `Token.IdpSessionID`, `Revoke(idp_session_id)` idempotente y el JAR de extensión (DEC-083 #3). Observación sin resolver aquí: la suite de E4-T1 documenta que la cuenta de servicio termina con `manage-users` (`fullScopeAllowed`), y DEC-073 descarta ese permiso. El Requisito 9.4 depende de la configuración que E4-T1 deje.
- **E4-T2** — modelo de autorización (`sujeto`, roles); base de A-5.
- **E0-T1** — plantilla de servicio (health, readiness).
- **E0-T2** — paquete contractual (`identidad.yaml`, `sesion.proto`, registro de tipos); semántica del cursor (A-10).
- **E0-T4** — lado consumidor de la revocación. **Necesario para cerrar E4-T3.**
- **E0-T10** — outbox, relay y bus V1 (sin replay por offset; A-10).

## Fuera de alcance

- Todo el lado consumidor: suscripción durable por módulo, difusión a réplicas y ACK, proyección de tombstones, `not-ready`, frescura y fallback a 15 s, corte de 30 s y prueba extremo a extremo (**E0-T4**).
- Reevaluación de claims en refresco (**E4-T4**).
- Revocación masiva, mientras A-6 no se resuelva.
- Gobierno de `cambio_identidad`, eventos `sujeto.*` / `rol.*` y rutas `/api/v1/sujetos` y `/api/v1/roles` (**E4-T6**).
- Cambios en el lado del IdP: JAR, mappers, `Issue`, `Revoke` y permisos de la cuenta de servicio (**E4-T1**). No se implementa un `SessionResolver` concreto: con DEC-083 #2, `Revoke` recibe el `idp_session_id` directamente.
- Cambios en `pf-pkg-contracts` (A-6, A-8, A-15): se reportan como brecha, no se crean rutas ni tipos de wire a mano.
- Plazo de la quita de rol (conflicto E4-HU-020/Q63 frente a DEC-031 #2).
