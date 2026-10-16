# Implementation Plan: E4-T6 — Gobierno y traza de cambios administrativos

> **E4-T6 comparte la rama `feature/e4-t3-event-snapshot` con E4-T3**, pero sus criterios son propios. Este plan no toca los archivos de T3.
>
> Verificado contra contexto `ai27-platform-context` @ `bd8901d` y contrato `pf-pkg-contracts` tag `v0.1.0-tracer.6`.

## Estado (2026-10-16)

**Parte A implementada y verificada en lo que no requiere infraestructura.** Las 10 tareas de código (1–10) están hechas: migraciones `0004`/`0005`/`0006`, puertos de gobierno e `IdpAdmin`, repos extendidos (`PorID`, `FijarActivo`, `AsignarInicial`, `Reemplazar`, `ContarActivosConRol`), repos de traza y tenant, doble en memoria de `IdpAdmin`, servicio de gobierno con el flujo transaccional, guardas de autorización/autoescalamiento, el `SujetoResolver` y el cableado en `identidad.New`.

Verificación corrida en verde: `gofmt`, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...`.

**Verificación contra Postgres real (2026-10-14).** Se levantó un Postgres 16 efímero (Docker) y se corrió la suite completa con `IDENTIDAD_TEST_DB` definido, en serie (`-p 1`, como `dbtest` espera: una base dedicada por corrida). Resultado: todo verde. Esto confirmó que las migraciones `0004`/`0005`/`0006` aplican en orden y que el índice único parcial `0005` produce `23505` ante un segundo rol activo (comportamiento que `AsignarInicial` traduce a `ErrAsignacionActivaExiste`). Dos regresiones que la nueva estructura de T6 introdujo en pruebas de otros specs se corrigieron como parte de esta verificación (ver abajo).

> Nota de ejecución en paralelo: las pruebas de integración de identidad comparten el esquema `identidad` y el rol `svc_identidad` sobre una sola base. Corridas en paralelo entre paquetes compiten (duplicado en `pg_namespace`, deadlock al aplicar grants). No es una regresión de T6: es cómo están escritas (asumen base exclusiva). Se corren en serie (`-p 1`) o con una base por paquete. En aislamiento, cada prueba pasa.

**Regresiones de T6 encontradas y corregidas al correr contra Postgres (2026-10-14):**
- **FK `sujeto_tenant_fk` de `0006` rompía las pruebas Postgres de E4-T2/E4-T3.** La FK es `NOT VALID`, pero en Postgres eso solo exime a las filas previas: los INSERT nuevos SÍ se validan. Las pruebas de sesión/grants insertaban `sujeto` con `tenant_id = gen_random_uuid()` (sin tabla destino antes). Se corrigió sembrando un `tenant` real antes de cada `sujeto` en `migrations/grants_integration_test.go`, `internal/sesion/{servicio,dod,store}_integration_test.go`. `svc_identidad` tiene INSERT sobre `tenant` (0006), así que la semilla es parte del flujo vivo.
- **El constraint `0005` (un solo rol activo) rompía `TestAsignacionRolRepoDuplicadoEnCodigoRetornaError` (E4-T2).** T6 (tarea 4.2) quitó la validación de duplicados en código y la delegó a `0005`, pero el esquema **inline** de ese test de integración seguía creando el índice NO único viejo (`asignacion_rol_sujeto_idx`), así que la segunda asignación no emergía como `23505`. Se alineó el esquema inline con `0005` (índice único parcial). El test, que ya esperaba `ErrAsignacionActivaExiste`, pasa.

**Tarea 11 (adaptador real de `IdpAdmin` sobre Keycloak): 11.1 y 11.3 HECHAS Y VERIFICADAS.** El adaptador vive en `internal/idp/internal/kc/admin.go` y satisface `idp.IdpAdmin`. Verificado el 2026-10-14 contra un contenedor real de Keycloak 26.7.4 (con el JAR de la extensión compilado con Maven 3.10.0 + JDK 24): alta con `tenant_id` y grupo del rol, idempotencia del alta, mover de grupo sin dejar dos grupos (DEC-084) y ciclo deshabilitar/habilitar idempotente. Las pruebas de integración de E4-T1 siguen en verde con la Config extendida. **11.2 (cuenta de servicio separada en el realm) NO está hecha:** hay una contradicción con lo que E4-T1 dejó (la cuenta de revocación ya tiene `manage-users` por el trade-off de FGAP v2 en Keycloak 26.7.4); separar de verdad las cuentas es una decisión de realm/seguridad pendiente del PO/Arquitectura. Ver el detalle en la tarea 11.

**Sub-tareas `[Requiere Postgres]` de T6 ESCRITAS Y VERIFICADAS (2026-10-14).** Las ocho (1.3, 2.2, 4.3, 5.3, 7.6, 7.7, 8.2, 9.2) ya tienen código y pasan contra Postgres real. Viven en dos archivos nuevos:
- `internal/gobierno/postgres/gobierno_integration_test.go` (caja negra `postgres_test`): smoke del esquema `0004`/`0005`/`0006` con sus índices, el 23505 del constraint de un solo rol activo, el 23514 del CHECK de `tenant.tipo` y el 23503 de la FK `sujeto_tenant_fk` (1.3, 2.2); reemplazo con actores correctos (4.3); inmutabilidad de la traza por privilegio (5.3); paginación del historial por cursor.
- `internal/gobierno/servicio_integration_test.go` (caja negra `gobierno_test`): alta atómica —estado + 2 trazas + 2 eventos—, con fallo de Begin y de Commit sin estado parcial (7.6, Property 1/3); reemplazo que deja exactamente dos trazas/eventos (7.6, Property 3); "local manda tras fallo del IdP" con `ErrIdpSyncPending` y el cambio local persistido (7.7, Property 6); autoescalamiento rechazado y último administrador protegido (8.2, Property 7); resolver con sub activo/inexistente/inactivo (9.2, Property 5).

Verificado en verde (serie, `-p 1`): los 2 paquetes nuevos, la suite completa con `IDENTIDAD_TEST_DB`, y la suite completa **sin** Postgres (modo CI, con `t.Skip`).

**Dos defectos de producción que estas pruebas destaparon y se corrigieron (2026-10-14):**
- **`SujetoRepo.Crear` rechazaba un `id` vacío.** `AltaSujeto` crea el sujeto con `ID=""` para que la BD genere el UUID, pero el INSERT bindeaba `""` como UUID (Postgres: `22P02`). Nunca se vio porque E4-T2 siempre pasaba un id explícito. Corregido: `Crear` omite la columna `id` cuando viene vacío y deja el `DEFAULT gen_random_uuid()`.
- **`AltaSujeto` creaba el sujeto FUERA de la transacción (viola atomicidad, Requisito 6 / Property 1).** `SujetoRepo.Crear` corría sobre el pool, no sobre la `tx` del cambio; si la transacción (asignación + traza + outbox) fallaba, el sujeto quedaba huérfano. Corregido elevando `Crear(ctx, s)` a `Crear(ctx, tx, s)` en el puerto `autorizacion.SujetoRepo`, su impl de Postgres y el sub-puerto `gobierno.SujetoRepoTx`; `AltaSujeto` ahora pasa la `tx`. El test de integración de E4-T2 se adaptó a la nueva firma (envuelve `Crear` en una tx puntual). La prueba de atomicidad (7.6) confirma que un commit fallido no deja sujeto.

**Observación registrada (no bug): el centinela `ErrAutoescalamiento` es inalcanzable.** En `ReemplazarRol`/`AsignarRolInicial`, `guardarAdministrador` se evalúa ANTES que `guardarAutoescalamientoRol`, así que un no-administrador se rechaza con `ErrNoAutorizado` y el centinela `ErrAutoescalamiento` nunca se alcanza. El comportamiento cumple el Requisito 9.2 (rechazo antes del IdP, sin escribir); la discrepancia es de consistencia código↔spec, no de seguridad. La prueba 8.2 afirma el **comportamiento** (rechazo + sin efecto), aceptando cualquiera de los dos centinelas. Pendiente de reconciliación DEC (ver la sección de reconciliación al final): decidir si se reordenan las guardas o el spec reconoce `ErrNoAutorizado` para ese caso.

**Decisiones de implementación tomadas (documentadas en el código):**
- Los repos de autorización pasaron de `*pgx.Conn` concreto a una interfaz `Querier`, para construirse con el pool del módulo ("accept interfaces"). No cambia su comportamiento; el test de integración de E4-T2 se adaptó a `AsignarInicial`/`tx`.
- `SujetoRepo.Crear` recibe ahora la `tx` del cambio (`Crear(ctx, tx, s)`), para que el alta de gobierno componga Crear + AsignarInicial + traza + outbox en un solo commit (atomicidad, Requisito 6). Antes corría sobre el pool y dejaba el sujeto fuera de la transacción. Afecta al puerto `autorizacion.SujetoRepo`, su impl de Postgres y el sub-puerto `gobierno.SujetoRepoTx`.
- El `RevocadorSesion` (cortar la sesión viva al desactivar, G-6) quedó como costura `nil`: el servicio de sesión no expone aún "la sesión viva de un sujeto". La desactivación corta el acceso local y deshabilita en el IdP; revocar la sesión viva en el mismo paso espera una pequeña extensión de E4-T3. Marcado como TODO(E4-T3) en el código.
- El `subDeToken` del resolver quedó como costura `nil`: cómo se obtiene el `sub` del token lo cierra el login de E4-T3 (nota de diseño del `design.md`).

## Overview

El plan sigue el estado real del repo (`internal/gobierno` vacío, `internal/autorizacion` con repos a medias, patrón transaccional de `internal/sesion`) y `design.md` al pie. Se divide en dos partes, igual que el plan de E4-T3:

- **Parte A — Se puede implementar ya.** Tareas con checkbox, sin dependencia de un dato de tercero ni del contrato. Cubren la migración del índice de la traza, el constraint de un solo rol activo, la tabla de tenants (con el UUID como placeholder documentado), los repos nuevos, el flujo transaccional del reemplazo/alta/desactivación contra un doble del IdP, y el `SujetoResolver`.
- **Parte B — Bloqueada por contrato o por dato de tercero.** Sin checkbox a propósito. Agrupada por lo que la desbloquea: el contrato (G-2) para endpoints y eventos serializados, y los datos de Arquitectura/Operaciones para las semillas (`0006`/`0007`).

Reglas que gobiernan el plan:

1. Estrictamente apegado a `requirements.md` y `design.md`. Nada de "Propuestas de Kiro (K)" entra a la ruta productiva.
2. Ninguna tarea rellena a mano un tipo de wire o un evento serializado: eso espera al contrato (regla 6 del scaffold; brecha G-2).
3. No se edita `0001`–`0003` ni los archivos del spec de E4-T3.
4. Las migraciones nuevas son `0004`+; cada objeto nuevo declara sus `GRANT` y su caso en `grants_integration_test.go`.
5. **Verificación estándar** por tarea de código (`golang-best-practices.md`): `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...`, todo en verde.

Convenciones:

- Lenguaje: **Go**. Prosa y comentarios en español (México); identificadores en inglés salvo sustantivos de dominio (`sujeto`, `rol`, `asignacion_rol`, `cambio_identidad`, `tenant`, `gobierno`), e `idp_subject`/`idp_session_id` en inglés por ser referencia al proveedor.
- **[Requiere Postgres]** = usa `internal/dbtest`, se omite con `t.Skip` si falta `IDENTIDAD_TEST_DB`; sin la variable, la tarea compila pero su comportamiento **no queda verificado**.
- **[Requiere Keycloak]** = `-tags integration`, necesita Docker y el JAR de E4-T1; sin ellos solo se verifica que compile.
- Sub-tareas de prueba con `*` (opcionales); varias son la DoD de E4-T6.

## Qué queda fuera de este plan (sin tarea)

| Tema | Motivo |
| --- | --- |
| Handlers HTTP de `/api/v1/sujetos` y `/api/v1/roles` | Brecha contractual G-2 |
| Serialización de los eventos `sujeto.*`/`rol.*` y su prueba de contrato | Brecha contractual G-2 |
| Semillas `0006` (tenant) y `0007` (primer admin) aplicables | Datos de tercero: UUID del tenant, `sub` del admin, actor de sistema |
| Alcance por `unidad`/`cliente` | Vínculo DEC-045 sin definir |
| Login/emisión de E4-T3 Parte B | E4-T3 (este spec solo entrega el resolver) |
| Consumo de los hechos por `mensajeria`/`reportes`/`catalogos` | Otros servicios; C6 a confirmar |

## Tasks

# PARTE A — SE PUEDE IMPLEMENTAR YA

- [x] 1. Migraciones de estructura `0004` y `0005`
  - Cierra con la verificación estándar.
  - [x] 1.1 Crear `migrations/identidad/0004_cambio_identidad_indice.sql`
    - Índice `cambio_identidad_sujeto_ts_idx` sobre `(sujeto_id, ocurrido_en DESC)`, con `CREATE INDEX IF NOT EXISTS`, encabezado en español.
    - No editar `0001`–`0003`; sin `GRANT` nuevos (un índice hereda los de la tabla).
    - Confirmar que `migrations/migrations.go` lo aplica en orden, después de `0003`.
    - _Requisitos: 1.7_
  - [x] 1.2 Crear `migrations/identidad/0005_asignacion_rol_un_activo.sql`
    - Índice único parcial `asignacion_rol_un_activo_idx` sobre `(sujeto_id) WHERE revocado_en IS NULL` (un solo rol activo por sujeto, C2/G-5).
    - Encabezado que explique que reemplaza la validación en código de `AsignacionRolRepo.Asignar`.
    - _Requisitos: 4.5_
  - [x]* 1.3 Smoke de esquema **[Requiere Postgres]**
    - Patrón de `grants_integration_test.go`/`schema_integration_test.go`: verificar que ambos índices existen con la definición esperada (`pg_indexes.indexdef`), y que insertar un segundo rol activo al mismo sujeto falla con `23505`.
    - _Requisitos: 1.7, 4.5_

- [x] 2. Tabla de tenants `0006` (estructura, UUID como placeholder)
  - Cierra con la verificación estándar.
  - [x] 2.1 Crear `migrations/identidad/0006_tenant.sql`
    - Tabla `identidad.tenant (id UUID PK, nombre, tipo CHECK IN ('interno','cliente'), creado_en)`.
    - FK `sujeto_tenant_fk`: `sujeto.tenant_id → tenant(id)`.
    - `GRANT SELECT, INSERT ON identidad.tenant TO svc_identidad` y su caso en `grants_integration_test.go`.
    - La **semilla** del tenant interno va con el UUID como placeholder `<<UUID_TENANT_AI27>>` y un comentario que marque que es dato de tercero: la migración de estructura se puede aplicar, pero la fila semilla **no** hasta tener el UUID. Separar estructura (aplicable) de semilla (pendiente) si hace falta en archivos distintos.
    - _Requisitos: 13.1, 13.5_
  - [x]* 2.2 Smoke de la tabla y la FK **[Requiere Postgres]**
    - Verificar columnas, el `CHECK` de `tipo`, y que un `sujeto` con `tenant_id` inexistente viola la FK.
    - _Requisitos: 13.1_

- [x] 3. Puertos del dominio de gobierno
  - Cierra con la verificación estándar.
  - [x] 3.1 Poblar `internal/gobierno/port.go`
    - Declarar `Gobierno` y `Consulta` con las firmas de `design.md` (`AltaSujeto`, `DesactivarSujeto`, `ReactivarSujeto`, `ReemplazarRol`, `AsignarRolInicial`; `SujetoPorID`, `HistorialDe`). Tipos `AltaSujeto`, `CambioEstado`, `ReemplazoRol`, `AsignacionInicial`, `SujetoDetalle`, `PaginaHistorial`.
    - Docstrings en español que citen la decisión de cada operación (C1, C2, C5, G-3). Quitar el `type Gobierno interface{}` vacío.
    - _Requisitos: 2, 3, 4, 5, 10_
  - [x] 3.2 Declarar `IdpAdmin` en `internal/idp/port.go`
    - `CrearUsuario`, `DeshabilitarUsuario`, `HabilitarUsuario`, `MoverDeGrupo`, con los docstrings de `design.md` (cuenta separada C4/G-4; mover de grupo sin exponer dos grupos, DEC-084).
    - Reemplazar el `TODO(contrato)` del puerto por estas firmas; mantener el aislamiento (nadie fuera de `internal/idp` importa el SDK).
    - _Requisitos: 7.2, 7.3, 7.6_
  - [x] 3.3 Declarar centinelas en `internal/gobierno/errors.go`
    - `ErrSujetoSinRol`, `ErrSujetoYaTieneRol`, `ErrUltimoAdministrador`, `ErrIdpSyncPending`, con docstrings. Reusar `autorizacion.ErrSujetoDuplicado`, `ErrAsignacionDuplicada`, `ErrRolFueraDeCatalogo` y el `ErrSujetoUnresolved` de E4-T3.
    - _Requisitos: 4.5, 5.4, 9.3, 7.7, 11.3_

- [x] 4. Repos: extender sujeto y asignación de rol
  - Cierra con la verificación estándar.
  - [x] 4.1 Extender `internal/autorizacion/postgres/sujeto.go`
    - Añadir `PorID` y `FijarActivo(ctx, tx, id, activo)` (recibe `tx` para componer en la transacción del cambio). No tocar `Crear` ni `PorIdpSubject`.
    - _Requisitos: 3.1, 11_
  - [x] 4.2 Reescribir `Asignar` como `Reemplazar` y `AsignarInicial` en `internal/autorizacion/postgres/asignacion_rol.go`
    - `AsignarInicial(ctx, tx, a)`: inserta cuando el sujeto no tiene rol activo; mapea `23505` del constraint `0005` a `ErrSujetoYaTieneRol` y `23503/23514` a `ErrRolFueraDeCatalogo`.
    - `Reemplazar(ctx, tx, in)`: en la misma `tx`, `UPDATE` de la activa a revocada (`revocado_por`, `revocado_en`) e `INSERT` de la nueva; si la nueva es igual a la activa, `ErrAsignacionDuplicada`.
    - **Quitar** la validación en código de duplicados (ahora la hace el constraint `0005`). Corregir el `asignado_por` (hoy pone `a.SujetoID`, debe ser el `ActorID`).
    - _Requisitos: 4.1, 4.3, 4.5, 4.6_
  - [x]* 4.3 Pruebas de los repos **[Requiere Postgres]**
    - Reemplazo: una activa + una revocada tras reemplazar; segundo activo rechazado; `asignado_por`/`revocado_por` = actor. Desactivar/reactivar: `activo` cambia, fila conservada.
    - _Requisitos: 3.1, 4.3, 4.5_

- [x] 5. Traza y tenant: repos nuevos
  - Cierra con la verificación estándar.
  - [x] 5.1 Crear `internal/gobierno/postgres/traza.go`
    - `TrazaRepo.Registrar(ctx, tx, c)` (INSERT en `cambio_identidad`, nombre canónico en `tipo`, K-1) y `Historial(ctx, sujetoID, cursor)` (SELECT ordenado por `ocurrido_en`, paginado por cursor como `listarSesionesRevocadas`, K-5), apoyado en el índice `0004`.
    - _Requisitos: 1.1–1.5, 10.2_
  - [x] 5.2 Crear `internal/gobierno/postgres/tenant.go`
    - `TenantRepo.Existe(ctx, tenantID)`. Lectura simple sobre `identidad.tenant`.
    - _Requisitos: 13.1_
  - [x]* 5.3 Prueba de inmutabilidad de la traza **[Requiere Postgres]**
    - Verificar que `UPDATE`/`DELETE` sobre `cambio_identidad` con `svc_identidad` falla por privilegio (Property 4).
    - _Requisitos: 1.6_

- [x] 6. Doble en memoria de `IdpAdmin` y suite de contrato
  - Cierra con la verificación estándar.
  - [x] 6.1 Extender `internal/idp/memory` con `IdpAdmin`
    - Implementar `CrearUsuario` (devuelve un `idpSubject` determinista; idempotente por el identificador), `Deshabilitar`/`Habilitar`, `MoverDeGrupo` (quita antes de agregar). Guardar estado en memoria para que las pruebas lo inspeccionen.
    - _Requisitos: 7.6, 12.1_
  - [x] 6.2 Añadir el contrato de `IdpAdmin` a `internal/idp/idptest`
    - Casos que ambas implementaciones deben cumplir: alta idempotente, mover de grupo sin dejar dos grupos, habilitar/deshabilitar idempotente.
    - _Requisitos: 7.6, 12.1_

- [x] 7. Servicio de gobierno: flujo transaccional
  - Cierra con la verificación estándar. Copia el patrón de `internal/sesion/servicio.go` (`tx := Begin`, estado + traza + outbox en la misma tx, commit, luego IdP).
  - [x] 7.1 Crear `internal/gobierno/servicio.go` con `NewServicio` y las costuras
    - `servicio` no exportado con los colaboradores (`tx BeginTx`, `publisher *outbox.Publisher`, `idpAdmin IdpAdmin`, repos). `NewServicio(...)` variádico con opciones, como `sesion.NewServicio`.
    - _Requisitos: 6_
  - [x] 7.2 Implementar `ReemplazarRol`
    - Secuencia de `design.md`: validar rol nuevo ≠ actual; `tx` con `Reemplazar` + dos filas de traza + dos `outbox.Publish`; commit; luego `IdpAdmin.MoverDeGrupo` reintentable (máx 3). Un fallo del IdP tras agotar no revierte el commit (devuelve `ErrIdpSyncPending`).
    - _Requisitos: 4.3, 4.5, 6, 7.2, 7.6, 7.7_
  - [x] 7.3 Implementar `AltaSujeto` y `AsignarRolInicial`
    - Alta (Otorgamiento): `IdpAdmin.CrearUsuario` primero; `tx` con `SujetoRepo.Crear` + `AsignarInicial` + dos filas de traza (`sujeto.creado`, `rol.asignado`) + dos `outbox.Publish`; commit. Validar rol en catálogo y organización si `cliente`.
    - _Requisitos: 2.1–2.8, 6_
  - [x] 7.4 Implementar `DesactivarSujeto` y `ReactivarSujeto`
    - Desactivar (Retiro: local primero): `tx` con `FijarActivo(false)` + traza + outbox + revocar la sesión viva reusando el Paso 1 de `internal/sesion` en la misma tx (K-6); commit; luego `IdpAdmin.Deshabilitar` y el Paso 2 de la sesión, reintentables.
    - Reactivar (Otorgamiento): `IdpAdmin.Habilitar` primero; `tx` con `FijarActivo(true)` + traza + `outbox` (`sujeto.reactivado`); commit.
    - _Requisitos: 3.1–3.8, 6_
  - [x] 7.5 Implementar `Consulta` (`SujetoPorID`, `HistorialDe`)
    - Lectura sobre los repos; `HistorialDe` usa el cursor de `TrazaRepo`.
    - _Requisitos: 10.2_
  - [x]* 7.6 Prueba de atomicidad **[Requiere Postgres]**
    - Con un `BeginTx` que falla inyectado (patrón `FaultyBeginTx` de E4-T3), verificar que ningún cambio deja estado, traza ni outbox a medias (Property 1). Reemplazo deja exactamente dos filas y dos eventos (Property 3).
    - _Requisitos: 4.3, 6_
  - [x]* 7.7 Prueba de "local manda tras fallo del IdP" **[Requiere Postgres]**
    - Con un `IdpAdmin` doble que falla, verificar que el cambio local permanece y la operación devuelve `ErrIdpSyncPending` (Property 6).
    - _Requisitos: 7.7_

- [x] 8. Autorización y autoescalamiento
  - Cierra con la verificación estándar.
  - [x] 8.1 Guardas de autorización en el servicio
    - Rechazar antes de tocar el IdP y sin escribir: invocante no administrador (9.1); no administrador que cambia su propio rol (9.2); administrador que es el último activo y se quita el rol o se desactiva (9.3, con conteo de administradores activos resistente a concurrencia).
    - _Requisitos: 9.1, 9.2, 9.3_
  - [x]* 8.2 Prueba de autoescalamiento y último admin **[Requiere Postgres]**
    - Property 7: no admin autoescalando → rechazo sin efecto; último admin desactivándose → rechazo.
    - _Requisitos: 9.2, 9.3_

- [x] 9. `SujetoResolver` productivo (habilitador de E4-T3)
  - Cierra con la verificación estándar.
  - [x] 9.1 Implementar `resolverSujeto` en `internal/gobierno`
    - `ResolveSujeto` consulta `SujetoRepo.PorIdpSubject`; devuelve `SujetoRef{SujetoID, TenantID}`, o `ErrSujetoUnresolved` si no existe o `activo == false` (G-6).
    - Resolver `subDelToken` según la nota de diseño: dejar la obtención del `sub` detrás de una pequeña función/costura, para cerrarla al cablear el login de E4-T3 sin reabrir este archivo.
    - **No** cablear `EmitirSesion`/`RefrescarSesion`: eso es E4-T3 Parte B.
    - _Requisitos: 11.1–11.4_
  - [x]* 9.2 Prueba del resolver **[Requiere Postgres]**
    - sub existente/activo → `SujetoRef` correcto; sub inexistente → `ErrSujetoUnresolved`; sujeto inactivo → `ErrSujetoUnresolved` (Property 5).
    - _Requisitos: 11.3, 11.4_

- [x] 10. Composición en la raíz del módulo
  - Cierra con la verificación estándar.
  - [x] 10.1 Cablear el servicio de gobierno en `identidad.New` / `cmd/api/main.go`
    - Construir `gobierno.NewServicio` con los repos, `BeginTx`, `outbox.Publisher` y el `IdpAdmin` real sobre Keycloak (tarea 11); el doble en memoria se conserva para pruebas.
    - Registrar el `SujetoResolver` donde el servicio de sesión lo espera (sin activar el login).
    - **No** registrar rutas HTTP de gobierno (no hay contrato).
    - _Requisitos: 7.1, 11.2_

- [-] 11. Adaptador real de `IdpAdmin` sobre Keycloak
  - **E4-T1 está cerrado y ya dejó todo lo necesario:** el realm editable (`deploy/keycloak/realm-ai27.json`), el patrón de adaptador que ya habla con la Admin API de Keycloak (`servicetoken.go`, `revoke.go`) y el JAR de integración. Esta tarea **extiende ese patrón**; no espera a E4-T1.
  - Cierra con la verificación estándar.
  - [x] 11.1 Implementar `IdpAdmin` sobre Keycloak en `internal/idp/internal/kc`
    - `CrearUsuario`, `DeshabilitarUsuario`, `HabilitarUsuario`, `MoverDeGrupo` por HTTP a la Admin API, en `internal/idp/internal/kc/admin.go`. `MoverDeGrupo` quita el grupo anterior antes de agregar el nuevo (DEC-084, Requisito 7.6). El adaptador delega en el objeto `admin` y satisface `idp.IdpAdmin` (`var _ idp.IdpAdmin = (*adapter)(nil)`). Config extendida con `AdminUsersURL`/`AdminGroupsURL` y una cuenta de servicio de administración propia (`AdminClientID`/`AdminClientSecret`), separada de la de revocación; `cmd/api/config.go` carga sus variables de entorno. El atributo del usuario es `tenant_id` (verificado contra el realm) y los query params se escapan (`url.QueryEscape`, `exact=true` en usuario, filtro de nombre exacto en grupo).
    - _Requisitos: 7.2, 7.6_
  - [ ] 11.2 Agregar la cuenta de servicio de administración al realm
    - **NO HECHA — bloqueada por una contradicción con E4-T1 que requiere decisión.** El plan pide una segunda cuenta de servicio separada cuyo correlato es que "la prueba de E4-T1 que verifica que la de revocación no puede modificar usuarios debe seguir pasando". Pero esa prueba (`integration_revoke_test.go::TestServicioNoModificaUsuarios`) verifica lo **contrario**: documenta que, por el trade-off de Keycloak 26.7.4 (FGAP v2 no cubre `/sessions`, obliga a `fullScopeAllowed: true`), la cuenta de revocación `ai27-identidad-svc` **sí** obtiene `manage-users` como efecto colateral. El realm hoy tiene una sola cuenta de servicio; el harness le asigna `manage-users` por API. Separar de verdad las cuentas (requisito 7.4 / C4 / G-4 / DEC-073) exige decidir cómo, dado ese trade-off, y es cambio de realm/seguridad. **Pendiente de decisión del PO/Arquitectura.** El código ya está preparado: la Config tiene `AdminClientID`/`AdminClientSecret` como cuenta aparte; solo falta crearla en el realm.
    - _Requisitos: 7.4_
  - [x]* 11.3 Pruebas `-tags integration` **[Requiere Keycloak]**
    - Verificadas en verde contra el contenedor de Keycloak 26.7.4 con el JAR de la extensión compilado (`internal/idp/internal/kc/integration_admin_test.go`): `TestAdminCrearUsuarioAltaYGrupo` (alta + atributo `tenant_id` + grupo del rol), `TestAdminCrearUsuarioIdempotente`, `TestAdminMoverDeGrupo` (un solo grupo tras el reemplazo, DEC-084) y `TestAdminDeshabilitarYHabilitar` (ciclo de estado idempotente). Las pruebas de E4-T1 (`TestServicioNoModificaUsuarios`, `TestRevocarUnaSesion`, `TestLecturaDeGrupos`) siguen pasando con la Config extendida.
    - **Pendiente (depende de 11.2):** la aserción de que la cuenta de administración y la de revocación están separadas (la de revocación falla 403 al modificar un usuario). No se puede escribir hasta que exista la segunda cuenta en el realm; hoy, por el trade-off documentado, la de revocación sí puede modificar usuarios.
    - _Requisitos: 7.4, 12.3_

# PARTE B — ESPERA ENTREGAS DE OTROS EQUIPOS (contrato y datos de Arquitectura)

> Sin checkbox a propósito: no son ejecutables hasta que llegue lo que las desbloquea. **No dependen de otra tarea de desarrollo de identidad** (E4-T1, E4-T2, E4-T3 están cerradas y ya dejaron lo necesario): dependen de una entrega externa. Cuando llegue, se convierten en tareas en este archivo.

## Espera: contrato de gobierno publicado (equipo de contratos / E0-T2)

Hasta que `pf-pkg-contracts` publique `/api/v1/sujetos`, `/api/v1/roles` y los eventos `sujeto.creado`/`sujeto.desactivado`/`sujeto.reactivado`/`rol.asignado`/`rol.revocado`:

- B1. Handlers HTTP de las operaciones de gobierno en `internal/http`, con los tipos generados, siguiendo el patrón de `handlers.go`. _Requisitos: 2, 3, 4, 5, 10.1_
- B2. Serializar los Hechos_Gobierno con la forma del contrato (no una variante local) en el `outbox.Publish` de cada flujo. _Requisitos: 8.1, 8.2_
- B3. Prueba de contrato que reconcilia el nombre canónico de cada hecho. _Requisitos: 8.5_
- B4. Stub de `mensajeria` para la idempotencia del consumo de `rol.revocado` (P13). _Requisitos: 8.6_
- B5. Confirmar con el dueño de contratos si `catalogos` consume `sujeto.*` (C6, recomendación B). _Requisitos: 8.4_

## Espera: tres valores de Arquitectura/Operaciones para las semillas

> **Estado real (2026-10-16).** Las migraciones de **semilla todavía NO existen como
> archivo.** `0006_tenant.sql` es **solo estructura** (tabla + FK `NOT VALID` +
> grants), sin ningún `INSERT` de semilla. La semilla del tenant interno y la del
> primer administrador están **redactadas en `design.md`** con placeholders, pero
> aún no se han escrito a un archivo de migración, y **su numeración está sin
> reconciliar** (ver el aviso "PENDIENTE DE DECISIÓN — numeración de las migraciones
> de semilla" en `design.md`: el comentario de `0006` dice `0007_tenant_seed.sql`
> para el tenant; `design.md` llama `0007` al primer admin; ninguno existe).
>
> Por tanto, antes de sustituir valores hay que: (1) decidir el esquema de
> numeración de las semillas, (2) crear los archivos, (3) sustituir los tres
> valores de tercero.

- B6. Crear la migración de **semilla del tenant** y sustituir el UUID real del **tenant interno**. _Requisitos: 13.1_
- B7. Crear la migración de **semilla del primer admin** y sustituir el **`sub` fijo** del admin (del realm construido en la tarea 11.2) y el **identificador de actor de sistema**. _Requisitos: 13.2, 13.3_
- B8. Prueba de arranque **[Requiere Postgres]**: tras aplicar las migraciones de semilla, existe el tenant interno, el primer admin con rol `administrador` y su fila de traza con actor no nulo. _Requisitos: 13.1–13.3_

> **Nota.** El adaptador de administración del IdP está en la **tarea 11 (Parte A)**. Su implementación (11.1) y sus pruebas de integración (11.3) ya están HECHAS y verificadas contra Keycloak real. Lo único pendiente de esa tarea es **11.2**: crear la **segunda cuenta de servicio** en el realm, separada de la de revocación. No es "construible ya" sin más: choca con el trade-off de FGAP v2 que E4-T1 documentó (la cuenta de revocación ya tiene `manage-users`), y resolverlo es una decisión de realm/seguridad del PO/Arquitectura.

## Reconciliación aparte (no es tarea de T6)

- DEC-088 A-1 (workspace) quedó obsoleta por DEC-085 (contexto): la emisión de E4-T3 va con "fila provisional primero". Afecta a quien implemente el login de E4-T3, no a este plan.
- Registrar las entradas DEC propuestas (`ENTRADAS-DEC-PROPUESTAS.md`) y reconciliar la numeración DEC-087/088 entre repos.
- **DEC propuesta (nueva, 2026-10-14): centinela `ErrAutoescalamiento` inalcanzable.** En el servicio de gobierno, `guardarAdministrador` se evalúa antes que `guardarAutoescalamientoRol`; un no-administrador que intenta cambiar su propio rol se rechaza con `ErrNoAutorizado` y el centinela específico `ErrAutoescalamiento` nunca se alcanza. El comportamiento cumple el Requisito 9.2 (rechazo antes del IdP, sin escribir en ningún almacén); no hay hueco de seguridad. Decisión pendiente del dueño del spec/decision-log: (a) reordenar las guardas para que `ErrAutoescalamiento` sea alcanzable y el motivo del rechazo sea el específico, o (b) reconocer en el spec que, para un no-administrador, `ErrNoAutorizado` cubre 9.2 y reservar `ErrAutoescalamiento` para una lectura futura. No es decisión de Kiro.
