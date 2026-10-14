# Requirements Document

**E4-T6 — Gobierno y traza de cambios administrativos (`identidad`)** · Documento de requisitos

| | |
| --- | --- |
| **Estado** | Borrador — decisiones G-1 a G-11 y cuestiones C1–C6 incorporadas (`DECISIONES-TOMADAS.md`). Solo quedan datos de terceros (UUID del tenant, `sub` y actor de sistema del primer admin) y el wire diferido por contrato (G-2) |
| **Historia técnica** | E4-T6 — Gobierno y traza de cambios administrativos (`docs/historias-tecnicas/E4-identidad-y-acceso.md`) |
| **HU funcional** | E4-HU-018 — Administrar usuarios, roles y alcances (`docs/hu/E4-HU-018-usuarios-roles-y-alcances.md`) |
| **Épica** | E4 — Identidad y acceso |
| **Repositorio / rama** | `repos/pf-svc-identidad` · `feature/e4-t3-event-snapshot` (el PM decidió trabajar E4-T3 y E4-T6 en la misma rama; los specs y sus criterios permanecen **separados**) |
| **Contexto consultado** | `repos/ai27-platform-context` @ `913380e` (al día con `origin/main`) |
| **Fecha** | 2026-10-16 |

## Introduction

E4-T6 hace que `identidad` deje una **traza inmutable de los cambios administrativos de identidad** (alta, baja de sujeto; asignación y revocación de rol) y que esos cambios se apliquen en el IdP (usuario, grupo) y en el modelo local (`sujeto`, `asignacion_rol`, `cambio_identidad`), publicando por outbox los hechos `sujeto.*` y `rol.*`. El objetivo de negocio es poder responder, ante un incidente, **"quién otorgó este acceso y cuándo"**.

E4-T6 además **diseña el resolver `IdpSubject → SujetoID`** (DEC-072), que es el habilitador del login de E4-T3 Parte B.

> **Este spec no modifica E4-T3.** Los criterios de E4-T3 viven en `../e4-t3-registro-sesion-revocacion/`. Los puntos de contacto son el `SujetoResolver` (Requisito 11) y la revocación de sesión por desactivación (Requisito 3.4), y ambos son criterios propios de T6. Compartir rama no significa compartir criterios.

### Cómo leer las etiquetas

Mismo esquema que los specs hermanos E4-T1, E4-T2 y E4-T3, con una etiqueta más:

| Etiqueta | Significado |
| --- | --- |
| **(T)** | Sale literalmente de E4-T6 ("Qué construir", criterios de aceptación técnicos o definición de terminado) |
| **(F)** | Se deriva directamente de otra fuente vigente citada (E4-HU-018, `identidad.md`, ADR, DEC, specs hermanos, migraciones) y es necesaria para cumplir un punto de E4-T6 |
| **(D)** | Sale de una decisión del usuario (G-1 a G-11 y las cuestiones C1–C6 de `DECISIONES-TOMADAS.md`). Hasta que se registren como DEC, se citan como "decidida, pendiente de registro" |
| **Diferido a contrato (G-2)** | El criterio está definido, pero se implementa solo cuando `pf-pkg-contracts` publique la operación o el evento |
| **Dato de tercero** | El criterio está decidido, pero falta un valor que fija Arquitectura/Operaciones/contratos; el criterio se cumple en cuanto llegue ese dato |

Las propuestas de Kiro **(K)** no son criterios: están en "Propuestas de Kiro (no aprobadas)".

### Cobertura de E4-T6

| Ítem de E4-T6 | Origen | Cobertura |
| --- | --- | --- |
| Tabla `cambio_identidad` con índice `(sujeto afectado, ts)` y retención larga | Qué construir | Requisito 1. La tabla **ya existe** (`0001`); falta el índice. Retención: indefinida (G-10) |
| Endpoints `GET`/`POST`/`PATCH` bajo `/api/v1/sujetos/...` y `/api/v1/roles/...` | Qué construir | Requisitos 2, 3, 4, 5 y 10. **Diferido a contrato (G-2):** la lógica local avanza sin wire |
| Escritura en el IdP (usuario, grupo) **y** en el modelo local, "de forma consistente" | Qué construir | Requisito 7. Orden por operación (G-3); Reemplazo local primero + reintento máx. 3 (C1, respaldado por DEC-085) |
| Estado local + traza + outbox en la misma transacción | Qué construir | Requisito 6 |
| Eventos `sujeto.creado`, `sujeto.desactivado`, `sujeto.reactivado`, `rol.asignado`, `rol.revocado` (nombres confirmados) | Qué construir | Requisito 8. Serialización: **diferido a contrato**. Consumidor `catalogos`: **C6, a confirmar** |
| Consumo por `mensajeria` y `reportes` | Qué construir | **Fuera de este spec** (otros servicios). Solo el stub de la DoD, Requisito 8.6 |
| Criterio: cada asignación/revocación de rol = exactamente una fila de `cambio_identidad` con actor y marca de tiempo + outbox en la misma transacción | Criterio | Requisitos 1.1–1.2 y 6 |
| Criterio: desactivar un sujeto produce el hecho que `mensajeria` consume; nombre tomado de `pf-pkg-contracts` | Criterio | Requisitos 3.2 y 8.2. Diferido a contrato |
| Criterio: el intento de autoescalar el propio rol se rechaza antes de llegar al adaptador del IdP | Criterio | Requisito 9.2 |
| DoD: prueba de alta/baja/asignación con traza + outbox atómicas | DoD | Requisitos 1, 6 |
| DoD: prueba de rechazo de autoescalamiento | DoD | Requisito 9.2 |
| DoD: contrato que reconcilia el nombre canónico | DoD | Requisito 8.5. Diferido a contrato |
| DoD: prueba de consumo idempotente del hecho de revocación de rol en `mensajeria` (stub) | DoD | Requisito 8.6 |
| (Handoff / DEC-072) Resolver `IdpSubject → SujetoID` que desbloquea el login de E4-T3 | Handoff 2026-10-16; DEC-072; DEC-088 A-5/A-16 | Requisito 11 |
| (Decisiones G-9/C3, G-11/C4) Tenant AI27 y primer administrador sembrados | Decisiones | Requisito 13. Forma decidida (tabla de tenants; realm con admin fijo); **datos de tercero:** UUID, `sub`, actor de sistema |

### Estado actual del repositorio (verificado leyendo el código)

| Elemento | Estado |
| --- | --- |
| `internal/gobierno/port.go` | `type Gobierno interface{}` **vacío a propósito** (brecha contractual). Sin rutas ni outbox |
| Contrato `identidad.yaml` (`api-client-go v0.1.0-tracer.5`, copia local) | Solo cinco operaciones de sesión. **Sin `/api/v1/sujetos` ni `/api/v1/roles`** |
| Eventos (`INVENTARIO.md`, `registro.tipos.yaml`) | Del dominio identidad solo `sesion.revocada`. **Sin `sujeto.*` ni `rol.*`** |
| `internal/idp/port.go` | `IdentityProvider` con `Issue`, `Refresh`, `Revoke`. **Sin administración de usuarios ni grupos** (`TODO(contrato)`) |
| `migrations/identidad/0001_init.sql` | `sujeto (id, idp_subject UNIQUE, tenant_id NOT NULL sin FK, activo, …)`; `asignacion_rol (id, sujeto_id, rol_codigo, asignado_por NOT NULL, asignado_en, revocado_por, revocado_en)` con índice **parcial no único** `(sujeto_id) WHERE revocado_en IS NULL`, sin `tenant_id` ni recurso de alcance; `cambio_identidad (id, tipo, sujeto_id, tenant_id, actor_id, sesion_id, motivo, detalle, ocurrido_en)` **sin índice por sujeto y tiempo**; `outbox`. **No existe tabla de tenants** |
| `migrations/identidad/0002_grants.sql` | `cambio_identidad`: `SELECT, INSERT`. `sujeto` y `asignacion_rol`: `SELECT, INSERT, UPDATE`, sin `DELETE`. `rol`: `SELECT` (ocho roles sembrados por la migración). Toda tabla nueva declara sus `GRANT` y su caso en `grants_integration_test.go` |
| `internal/autorizacion/postgres` | `SujetoRepo`: `Crear`, `PorIdpSubject`. `AsignacionRolRepo`: `Asignar`, `ActivasDe`. **No hay** desactivar, revocar asignación ni escritura en `cambio_identidad` |
| `internal/sesion/seams.go` | `SujetoResolver.ResolveSujeto(ctx, idp.Token) (SujetoRef, error)` declarada; sin implementación productiva. `idp.Token` no expone el `sub` |
| `internal/sesion/servicio.go` | La revocación escribe `ActorID: nil` (`TODO(A-12)`): hoy ningún camino sabe quién es el actor autenticado |
| Migraciones `0004`/`0005` | No existen (se eliminaron en E4-T3). Los cambios a estas tablas van en migraciones nuevas |

### Fuentes

- `docs/historias-tecnicas/E4-identidad-y-acceso.md`, E4-T6 (principal) y preámbulo.
- `docs/hu/E4-HU-018-usuarios-roles-y-alcances.md` y `docs/hu/E4-HU-020-sesion-y-revocacion.md` (Escenarios 3 y 9 de HU-018/020).
- `docs/arquitectura/services/identidad.md` (a), (e), (f), (g).
- `docs/adr/0026`, `docs/adr/0029` (outbox).
- Repo de contexto, `.kiro/steering/decision-log.md`: **DEC-031** (enmendada), **DEC-036**, **DEC-045**, **DEC-053**, **DEC-062**, **DEC-072**, **DEC-073**, **DEC-083**, **DEC-084** (PENDIENTE), **DEC-085** (PENDIENTE).
- Specs hermanos: `e4-t1-adaptador-idp-keycloak` (A-5, A-8, §Mapper de claims), `e4-t2-modelo-autorizacion-claims` (unicidad abierta en Data Models), `e4-t3-registro-sesion-revocacion` (`seams.go`; DEC-088 de este workspace: A-2, A-6, A-9).
- `docs/historias-tecnicas/E0-fundamentos-plataforma.md`: E0-T2, E0-T10.
- `repos/pf-pkg-contracts`: `identidad.yaml`, `INVENTARIO.md`, `registro.tipos.yaml`.
- Decisiones G-1 a G-11 y cuestiones C1–C6 de `DECISIONES-TOMADAS.md`.

## Glossary

- **Servicio_Gobierno**: componente de `identidad` (`internal/gobierno`) que ejecuta los cambios administrativos de identidad.
- **API_Gobierno**: handlers HTTP de `/api/v1/sujetos/...` y `/api/v1/roles/...`. No existen en el contrato (G-2).
- **Puerto_IdP**: la interfaz `idp.IdentityProvider`; para este spec incorpora operaciones de usuario y grupo.
- **IdP**: Keycloak con la extensión de E4-T1. El `rol` del token sale del atributo `rol` del **grupo** del usuario (DEC-083 #4).
- **sujeto**: entidad local que correlaciona el `sub` opaco del IdP (`idp_subject`) con el `sujeto_id` de AI27 y su tenant.
- **asignacion_rol**: asignación de un rol a un sujeto con ciclo de vida (activa/revocada) y actor.
- **Traza_Gobierno**: la tabla `identidad.cambio_identidad`, de solo inserción por privilegio.
- **Cambio_Administrativo**: alta o desactivación de sujeto; asignación, reemplazo o revocación de rol.
- **Actor**: el sujeto autenticado que ejecuta el Cambio_Administrativo, identificado por su `sujeto_id`.
- **Actor_Sistema**: identificador fijo de sistema/bootstrap, actor del primer alta (G-11).
- **Tenant_AI27**: tenant único de los siete roles internos (G-9).
- **Outbox**: tabla `identidad.outbox`, drenada por el relay de `pf-pkg-platform-go/outbox` (ADR 29).
- **Hechos_Gobierno**: `sujeto.creado`, `sujeto.desactivado`, `sujeto.reactivado`, `rol.asignado`, `rol.revocado`.
- **Retiro_De_Acceso**: revocar rol o desactivar sujeto. **Otorgamiento**: alta de sujeto o asignación de rol (G-3).
- **Reemplazo**: asignar un rol a un sujeto que ya tiene otro activo (revoca el anterior y asigna el nuevo, G-5).
- **SujetoResolver**: costura de `internal/sesion` que obtiene `SujetoID` y `TenantID` a partir de lo emitido por el IdP.
- **IdP_En_Memoria**: doble `idp/memory`; debe cumplir la suite de contrato `idptest`.

## Requirements

### Requirement 1: Traza inmutable de cambios administrativos

**User Story:** Como equipo de desarrollo de `identidad`, quiero una traza inmutable de alta, baja, asignación y revocación de rol, para que un incidente pueda responder "quién otorgó este acceso y cuándo".

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Gobierno asigna o revoca un rol, THE Servicio_Gobierno SHALL registrar en la Traza_Gobierno **exactamente una fila** por cada asignación y por cada revocación, con el Actor y la marca de tiempo.
   *Fuente:* E4-T6, criterio 1. Un Reemplazo genera por tanto **dos filas** (G-5).
2. **(D)** WHEN el Servicio_Gobierno da de alta un sujeto con rol inicial, THE Servicio_Gobierno SHALL registrar **dos filas**: la del alta (`sujeto.creado`) y la de la asignación del rol (`rol.asignado`).
   *Fuente:* G-8; coherente con 1.1.
3. **(T)** WHEN el Servicio_Gobierno desactiva un sujeto, THE Servicio_Gobierno SHALL registrar una fila con el sujeto afectado, el Actor y la marca de tiempo.
   *Fuente:* E4-T6, historia técnica ("alta, baja, asignación y revocación de rol").
4. **(D)** THE Servicio_Gobierno SHALL escribir en la Traza_Gobierno **solo** los cambios que afectan el acceso: alta, cambio de estado (desactivación y reactivación) y cambio de rol; SHALL NOT escribir fila por la edición del nombre ni del identificador.
   *Fuente:* G-8 (traza solo para rol y estado); C5 (la reactivación es un cambio de estado y deja su propia fila).
5. **(F)** THE Traza_Gobierno SHALL registrar en cada fila el tipo de cambio, el sujeto afectado, su tenant, el Actor y la marca de tiempo.
   *Fuente:* `0001_init.sql` (`tipo`, `sujeto_id`, `tenant_id`, `actor_id`, `ocurrido_en`).
6. **(F)** IF un rol de base de datos intenta `UPDATE` o `DELETE` sobre la Traza_Gobierno, THEN THE Traza_Gobierno SHALL rechazar la operación por falta de privilegio.
   *Fuente:* "inmutable" (E4-T6); `0002_grants.sql`; ADR 26 §7. *Nota:* ya se cumple; el requisito exige que siga cumpliéndose tras las migraciones de este spec.
7. **(T)** THE Traza_Gobierno SHALL contar con un índice por (sujeto afectado, marca de tiempo).
   *Fuente:* E4-T6, Qué construir. *Nota:* en el esquema las columnas son `sujeto_id` y `ocurrido_en`; el texto "sujeto_afectado, ts" se lee como descriptivo.
8. **(D)** THE Traza_Gobierno SHALL conservarse **de forma indefinida**, sin purga automática ni plazo de retención.
   *Fuente:* G-10; E4-T6 ("retención larga"); `identidad.md` (retención).
9. **(F)** THE Servicio_Gobierno SHALL registrar como Actor el `sujeto_id` del sujeto autenticado; para el alta del primer administrador SHALL registrar el Actor_Sistema. THE Traza_Gobierno SHALL NOT contener filas con Actor nulo.
   *Fuente:* E4-T6, criterio 1 ("con actor"); G-11/C4 (Actor_Sistema). Hoy la revocación de sesión escribe `actor_id` nulo (`TODO(A-12)`). *Dato de tercero:* el valor del Actor_Sistema lo fija Arquitectura/Operaciones.

### Requirement 2: Alta de sujeto

**User Story:** Como administrador, quiero dar de alta a un usuario con uno de los ocho roles, para que acceda a la plataforma con las capacidades que le corresponden (E4-HU-018).

#### Acceptance Criteria

1. **(D)** WHEN el Administrador da de alta un sujeto, THE Servicio_Gobierno SHALL crear primero el usuario en el IdP (Otorgamiento: IdP primero) y después el `sujeto` local, cuyo `idp_subject` es el `sub` que el IdP asignó al usuario.
   *Fuente:* G-3, G-1/DEC-072 (el `sub` es opaco; `SujetoID` es independiente). **Diferido a contrato (G-2)** en cuanto al endpoint.
2. **(F)** THE Servicio_Gobierno SHALL aceptar en el alta, como mínimo, los campos de E4-HU-018: nombre, identificador de usuario, rol, organización cliente (condicional) y estado. Tipo, formato y longitud los fija el contrato.
   *Fuente:* E4-HU-018, tabla de Campos. Diferido a contrato.
3. **(F)** IF el rol del alta es `cliente`, THEN THE Servicio_Gobierno SHALL exigir la organización cliente, que determina el tenant del sujeto; IF falta, THEN THE Servicio_Gobierno SHALL rechazar el alta sin escribir en el IdP, el modelo local, la Traza_Gobierno ni el Outbox.
   *Fuente:* E4-HU-018, Campos.
4. **(D)** IF el rol del alta es uno de los siete roles internos, THEN THE Servicio_Gobierno SHALL asignar al sujeto el Tenant_AI27.
   *Fuente:* G-9/C3. Se siembra en una tabla de tenants (Requisito 13). *Dato de tercero:* el UUID del Tenant_AI27 lo fija Arquitectura.
5. **(F)** IF el rol del alta no es uno de los ocho de DEC-053 (incluido `auditor`), THEN THE Servicio_Gobierno SHALL rechazar el alta sin escribir en el IdP, el modelo local, la Traza_Gobierno ni el Outbox.
   *Fuente:* E4-HU-018, criterio; E4-T2 Requisitos 2.2–2.3; DEC-036.
6. **(F)** IF el identificador del IdP ya corresponde a un `sujeto`, THEN THE Servicio_Gobierno SHALL rechazar el alta (`ErrSujetoDuplicado`) sin modificar el sujeto existente.
   *Fuente:* E4-T2 Requisito 1.2.
7. **(F)** THE Servicio_Gobierno SHALL fijar en el IdP el atributo `tenant_id` del usuario y su pertenencia al grupo del rol inicial.
   *Fuente:* E4-T1 (`tenant_id` como atributo inmutable por el usuario y el cliente; un grupo por rol); DEC-083 #4 (el `rol` sale del grupo).
8. **(T)** WHEN el alta termina con éxito, THE Servicio_Gobierno SHALL publicar `sujeto.creado` y `rol.asignado` mediante el Outbox, en la misma transacción que el estado local y las dos filas de la Traza_Gobierno (Requisito 6).
   *Fuente:* E4-T6; G-8. Serialización: diferido a contrato.

### Requirement 3: Desactivación de sujeto

**User Story:** Como administrador, quiero desactivar a un usuario sin perder su historia, para retirarle el acceso de inmediato y que quede constancia de quién lo hizo y cuándo.

#### Acceptance Criteria

1. **(F)** WHEN el Administrador desactiva un sujeto activo, THE Servicio_Gobierno SHALL marcar `activo = false` y conservar la fila.
   *Fuente:* E4-HU-018, criterio; `0002_grants.sql` (`sujeto` sin `DELETE`).
2. **(T)** WHEN la desactivación termina con éxito, THE Servicio_Gobierno SHALL publicar `sujeto.desactivado` mediante el Outbox, de modo que `mensajeria` pueda retirar la membresía de pool.
   *Fuente:* E4-T6, criterio 2. Diferido a contrato.
3. **(D)** THE Servicio_Gobierno SHALL aplicar la desactivación **primero en el modelo local** (estado + traza + outbox, Requisito 6) y **después** en el IdP.
   *Fuente:* G-3 (Retiro_De_Acceso: local primero); C1 (sincronización del IdP reintentable, espera creciente, máximo 3 intentos; respaldado por DEC-085).
4. **(D)** WHEN el Servicio_Gobierno desactiva un sujeto con sesión viva, THE Servicio_Gobierno SHALL revocar esa sesión, de modo que el acceso HTTP y SSE termine en **máximo 30 segundos** desde el commit local.
   *Fuente:* G-6; E4-HU-020 (plazo contractual de 30 s). *Nota:* en V1 hay a lo sumo una sesión viva por sujeto (DEC-088 A-2), por lo que no se necesita revocación masiva.
5. **(D)** IF un sujeto desactivado intenta iniciar sesión, THEN THE `identidad` SHALL rechazar el intento: el IdP no autentica al usuario y el `SujetoResolver` no resuelve el sujeto.
   *Fuente:* G-6 ("un sujeto desactivado no puede volver a logarse").
6. **(T)** THE Servicio_Gobierno SHALL registrar la desactivación en la Traza_Gobierno (Requisito 1.3).

7. **(D)** WHEN el Administrador reactiva un sujeto desactivado, THE Servicio_Gobierno SHALL marcar `activo = true`, publicar el hecho `sujeto.reactivado` por el Outbox y registrar su propia fila en la Traza_Gobierno (Requisito 1.4).
   *Fuente:* C5 (hecho nuevo con su propia fila; no reusa `sujeto.creado`). Serialización del hecho: **diferido a contrato (G-2)**.
8. **(D)** THE Servicio_Gobierno SHALL aplicar la reactivación como un Otorgamiento (IdP primero), por simetría con el alta.
   *Fuente:* C5; G-3 (la reactivación devuelve acceso).

### Requirement 4: Asignación de rol y Reemplazo

**User Story:** Como administrador, quiero asignar un rol a un sujeto, para que su acceso efectivo corresponda a ese rol.

#### Acceptance Criteria

1. **(T)** WHEN el Administrador asigna un rol a un sujeto activo que no tiene rol activo, THE Servicio_Gobierno SHALL crear la `asignacion_rol` activa con el Actor (`asignado_por`) y la marca de tiempo, y reflejar la pertenencia al grupo del rol en el IdP.
   *Fuente:* E4-T6, Qué construir (`POST` bajo `/api/v1/roles/...`; "grupo"). Diferido a contrato en cuanto al endpoint.
2. **(D)** THE Servicio_Gobierno SHALL aplicar la asignación **primero en el IdP** y después en el modelo local (Otorgamiento: IdP primero).
   *Fuente:* G-3.
3. **(D)** WHEN el Administrador asigna un rol a un sujeto activo que ya tiene otro rol activo distinto, THE Servicio_Gobierno SHALL ejecutar un **Reemplazo**: revocar la asignación anterior y crear la nueva, en una sola operación, con **dos filas** de la Traza_Gobierno y dos hechos (`rol.revocado` y `rol.asignado`) en el Outbox.
   *Fuente:* G-5 #1; C1 (el Reemplazo escribe todo local en una sola transacción —revoca + asigna + dos filas de traza + dos eventos— y después sincroniza el IdP).
4. **(D)** IF el sujeto está desactivado, THEN THE Servicio_Gobierno SHALL rechazar la asignación sin escribir en el IdP, el modelo local, la Traza_Gobierno ni el Outbox.
   *Fuente:* G-5 #2.
5. **(D)** THE modelo local SHALL garantizar por constraint de base de datos **a lo sumo un rol activo por sujeto**, y no solo unicidad por `(sujeto_id, rol_codigo)`.
   *Fuente:* G-5 #3; DEC-053 (el token lleva un solo rol). *Nota:* es más estricto que la opción que el diseño de E4-T2 dejó abierta; cierra esa duda.
6. **(F)** IF la asignación pide el rol que el sujeto ya tiene activo, THEN THE Servicio_Gobierno SHALL rechazarla (`ErrAsignacionDuplicada`) sin crear fila de asignación, de traza ni de outbox.
   *Fuente:* E4-T2 Requisito 3.2.
7. **(F)** IF el rol no es uno de los ocho de DEC-053, THEN THE Servicio_Gobierno SHALL rechazar la asignación (`ErrRolFueraDeCatalogo`) sin escribir en ningún almacén.
   *Fuente:* E4-T2 Requisito 2.2; E4-HU-018.
8. **(T)** WHEN la asignación termina con éxito, THE Servicio_Gobierno SHALL publicar `rol.asignado` mediante el Outbox y registrar exactamente una fila en la Traza_Gobierno (Requisitos 1.1 y 6).
9. **(F)** THE Servicio_Gobierno SHALL asignar roles **sin** recurso de alcance (a qué `unidad` o `cliente`).
   *Fuente:* DEC-045 notas (vínculo Monitorista↔`unidad` sin definir); `0001_init.sql` (`TODO(DEC-045)`). El alcance como recurso editable queda **fuera de este spec** mientras DEC-045 no defina el vínculo.
10. **(D)** WHEN un Reemplazo cambia el rol, THE sesión viva del sujeto SHALL conservar el rol anterior hasta el siguiente refresco, en el que se recomponen los claims.
    *Fuente:* G-6; DEC-031 #2 (ADR 16).

### Requirement 5: Revocación de rol (como parte de un Reemplazo)

**User Story:** Como administrador, quiero que al cambiar el rol de un usuario se revoque el anterior conservando su historia, para que quede constancia sin dejar al usuario sin ningún rol.

> **Nota (C2).** No existe la revocación de rol como acción suelta: siempre acompaña a una nueva asignación (Reemplazo, Requisito 4.3). Para retirar el acceso por completo se desactiva al sujeto (Requisito 3).

#### Acceptance Criteria

1. **(T)** WHEN el Administrador revoca una asignación activa, THE Servicio_Gobierno SHALL marcar la `asignacion_rol` como revocada con el Actor (`revocado_por`) y la marca de tiempo (`revocado_en`), sin borrar la fila, y retirar al sujeto del grupo del rol en el IdP.
   *Fuente:* E4-T6, Qué construir; `0002_grants.sql` (sin `DELETE`).
2. **(D)** THE Servicio_Gobierno SHALL aplicar la revocación **primero en el modelo local** y después en el IdP (Retiro_De_Acceso), con la sincronización del IdP reintentable (C1).
   *Fuente:* G-3; C1.
3. **(T)** WHEN la revocación termina con éxito, THE Servicio_Gobierno SHALL publicar `rol.revocado` mediante el Outbox y registrar exactamente una fila en la Traza_Gobierno (Requisitos 1.1 y 6).
4. **(D)** THE revocación de rol SHALL ocurrir **únicamente** como parte de un Reemplazo (Requisito 4.3); el Servicio_Gobierno SHALL NOT dejar a un sujeto activo sin rol.
   *Fuente:* **C2** (no se permite "activo sin rol"; para retirar el acceso se desactiva al sujeto, Requisito 3). Esto cierra la contradicción E4-HU-020 Q63 / DEC-031 #2: al no existir "quitar sin reemplazo", no hay un caso de corte en 30 s por quita de rol.

### Requirement 6: Atomicidad local — estado, traza y outbox

**User Story:** Como equipo de desarrollo de `identidad`, quiero que cada cambio administrativo comprometa estado, traza y evento juntos, para que nunca exista un cambio sin traza ni un evento sin cambio (P17).

#### Acceptance Criteria

1. **(T)** WHEN el Servicio_Gobierno compromete un Cambio_Administrativo publicable, THE Servicio_Gobierno SHALL escribir el estado local, las filas de la Traza_Gobierno y las del Outbox en **una sola transacción de Postgres**.
   *Fuente:* E4-T6; ADR 29; precedente `revocarLocalConEvento` en `internal/sesion`.
2. **(F)** IF cualquiera de las escrituras falla, THEN THE Servicio_Gobierno SHALL revertirlas todas.
   *Fuente:* consecuencia de 6.1; DoD ("traza + outbox atómicas").
3. **(F)** THE Servicio_Gobierno SHALL publicar mediante `pf-pkg-platform-go/outbox.Publisher`, con un `evento_id` único por hecho.
   *Fuente:* `0001_init.sql` (`outbox.evento_id UNIQUE`); ADR 29.
4. **(F)** WHILE el contrato no publique los Hechos_Gobierno, THE Servicio_Gobierno SHALL ser verificable en atomicidad de estado y traza sin serializar los eventos.
   *Fuente:* G-2 (avanzar sin wire).

### Requirement 7: Consistencia con el IdP

**User Story:** Como equipo de desarrollo de `identidad`, quiero que el IdP y el modelo local reflejen el mismo estado tras un cambio administrativo, para que el token que el IdP firma corresponda a lo que `identidad` gobierna.

#### Acceptance Criteria

1. **(T)** THE Servicio_Gobierno SHALL escribir cada Cambio_Administrativo en el IdP (usuario, grupo) y en el modelo local de forma consistente.
2. **(D)** THE Servicio_Gobierno SHALL aplicar el orden según el tipo: **Retiro_De_Acceso** (desactivar) → local primero; **Otorgamiento** (alta, asignar, reactivar) → IdP primero; **Reemplazo** → **todo local en una sola transacción, luego sincronizar el IdP** (C1).
   *Fuente:* G-3; C1. El Reemplazo no se parte en "retiro + otorgamiento" con órdenes opuestos: se resuelve local primero, respaldado por el mismo principio de DEC-085.
3. **(F)** THE Puerto_IdP SHALL incorporar las operaciones de administración de usuario y de grupo, y ningún módulo fuera del adaptador del IdP SHALL importar el SDK o la API del proveedor.
   *Fuente:* E4-T1 (interfaz propia y criterio 1); A-5 de `e4-t1/requirements.md`.
4. **(D)** THE `identidad` SHALL usar para administrar usuarios y grupos una **cuenta de servicio separada** de la que revoca sesiones.
   *Fuente:* G-4; DEC-073 (la cuenta de revocación no tiene `manage-users`). *Nota:* el realm es artefacto de E4-T1; el cambio se coordina ahí y la prueba que exige que la cuenta de revocación **no** pueda modificar usuarios debe seguir pasando.
5. **(F)** AFTER un alta o asignación exitosa, THE IdP SHALL reflejar la pertenencia al grupo del rol (de cuyo atributo `rol` sale el claim) y el atributo `tenant_id`.
   *Fuente:* DEC-083 #4; E4-T1 R6.
6. **(F)** WHILE se hace un Reemplazo, THE Servicio_Gobierno SHALL NOT dejar al usuario en dos grupos de rol a la vez.
   *Fuente:* DEC-084 (el mapper de un solo valor tomaría uno sin criterio); G-5.

7. **(D)** THE sincronización del IdP posterior al commit local SHALL reintentarse con espera creciente, **máximo 3 intentos**; un fallo tras agotarlos SHALL NOT revertir el cambio local ya comprometido.
   *Fuente:* C1; mismo patrón que DEC-085 (local manda; el corte en el IdP es reintentable e idempotente).

### Requirement 8: Hechos publicados y contrato

**User Story:** Como equipo de desarrollo de los consumidores, quiero hechos de gobierno con nombre y forma definidos en el contrato, para consumirlos sin adivinar variantes locales.

#### Acceptance Criteria

1. **(D)** THE Servicio_Gobierno SHALL publicar los Hechos_Gobierno con exactamente estos nombres: `sujeto.creado`, `sujeto.desactivado`, `sujeto.reactivado`, `rol.asignado`, `rol.revocado`.
   *Fuente:* E4-T6; G-2 (nombres confirmados); C5 (`sujeto.reactivado` es un hecho nuevo).
2. **(T)** THE Servicio_Gobierno SHALL tomar la forma serializada de cada hecho de `pf-pkg-contracts`, no de una variante local.
   *Fuente:* E4-T6, criterio 2. **Diferido a contrato (G-2).**
3. **(T)** THE inventario de E0-T2 SHALL congelar, antes de implementar el consumo de cada hecho: productor, consumidores, agregado, clave de orden y nombre canónico.
   *Fuente:* E4-T6. Diferido a contrato.
4. **(D)** THE Servicio_Gobierno SHALL tratar a `mensajeria` y `reportes` como consumidores de los hechos. **A confirmar (C6):** que `catalogos` **no** es consumidor de `sujeto.*`, dado que su único motivo (sujeto referenciado por un custodio) no aplica en V1 (los custodios no son usuarios de la plataforma; subsistema de evidencia aparte, DEC-048). Lo cierra el dueño de contratos + PM, no este spec.
   *Fuente:* E4-T6; `identidad.md` (f); C6 (recomendación Opción B).
5. **(T)** THE contrato SHALL contar con una prueba que reconcilie el nombre canónico de cada hecho entre `pf-pkg-contracts` y lo que publica el Servicio_Gobierno.
   *Fuente:* E4-T6, DoD. Diferido a contrato.
6. **(T)** THE verificación del consumo SHALL incluir una prueba, con un stub de `mensajeria`, de que reentregar el hecho de revocación de rol no produce efecto adicional (P13).
   *Fuente:* E4-T6, DoD.

### Requirement 9: Autorización de la superficie y autoescalamiento

**User Story:** Como equipo de seguridad, quiero que solo el administrador ejecute cambios administrativos y que nadie escale su propio privilegio, para que la traza no registre accesos obtenidos por la puerta equivocada.

#### Acceptance Criteria

1. **(F)** IF quien invoca un Cambio_Administrativo no tiene el rol `administrador`, THEN THE API_Gobierno SHALL rechazar la solicitud sin invocar al Puerto_IdP y sin escribir en el modelo local, la Traza_Gobierno ni el Outbox.
   *Fuente:* E4-HU-018 (matriz: solo Administrador administra usuarios y roles).
2. **(T)** IF un sujeto que no es administrador intenta modificar su propio rol, THEN THE Servicio_Gobierno SHALL rechazar la solicitud **antes** de invocar al Puerto_IdP, sin escribir en el modelo local, la Traza_Gobierno ni el Outbox.
   *Fuente:* E4-T6, criterio 3; E4-HU-018, **Escenario 9**. *Discrepancia de referencia:* E4-T6 cita el "escenario 4", que en E4-HU-018 es "Jefe Operativo arma y asigna". Este spec usa el 9.
3. **(D)** WHERE un administrador modifica su propio rol o se desactiva, THE Servicio_Gobierno SHALL permitirlo, **salvo** que sea el último administrador activo; en ese caso SHALL rechazar la solicitud sin escribir en ningún almacén.
   *Fuente:* G-7. *Nota de diseño:* el conteo de administradores activos debe resistir dos solicitudes concurrentes.
4. **(F)** THE API_Gobierno SHALL ser accesible solo para `Audiencia` Interno.
   *Fuente:* DEC-054; consecuencia de 9.1.

### Requirement 10: Consulta y respuesta a "quién otorgó este acceso y cuándo"

**User Story:** Como administrador, quiero consultar los sujetos, sus roles y su historia de cambios, para saber quién otorgó un acceso y cuándo.

#### Acceptance Criteria

1. **(T)** THE API_Gobierno SHALL exponer operaciones `GET` bajo `/api/v1/sujetos/...` y `/api/v1/roles/...` para leer sujetos y asignaciones. **Diferido a contrato (G-2).**
2. **(F)** WHEN se consulta la historia de un sujeto, THE Servicio_Gobierno SHALL poder devolver sus Cambios_Administrativos ordenados por marca de tiempo, con su Actor, apoyándose en el índice del Requisito 1.7.
   *Fuente:* objetivo de E4-T6; `identidad.md` ("Consulta de gobierno"). La forma de la consulta la fija el contrato.
3. **(D)** IF quien consulta no es `administrador`, THEN THE API_Gobierno SHALL rechazar la consulta.
   *Fuente:* G-10; DEC-036 (no existe rol de auditoría en V1).

### Requirement 11: Resolver `IdpSubject → SujetoID` (habilitador del login de E4-T3)

**User Story:** Como equipo de desarrollo de `identidad`, quiero resolver en servidor el `SujetoID` y el `TenantID` a partir del `sub` opaco del IdP, para completar el registro de sesión del login de E4-T3.

#### Acceptance Criteria

1. **(D)** THE Servicio_Gobierno SHALL ser el dueño de la correspondencia entre el `sub` opaco del IdP (`idp_subject`) y el `SujetoID`; el `sujeto` local es su fuente.
   *Fuente:* **DEC-072** (opción c); G-1.
2. **(D)** THE `identidad` SHALL proveer una implementación de `SujetoResolver` que, dado el `sub`, devuelva el `SujetoID` y el `TenantID` del `sujeto` con ese `idp_subject`, **sin** claim nuevo, **sin** alinear identificadores y **sin** cambios en `pf-pkg-platform-go`.
   *Fuente:* DEC-072 #1; handoff 2026-10-16. *Nota de diseño:* `idp.Token` no expone el `sub`; cómo lo obtiene el resolver (por ejemplo, del propio access token verificado) se resuelve en el diseño.
3. **(F)** IF el `sub` no corresponde a ningún `sujeto`, THEN THE `SujetoResolver` SHALL devolver `ErrSujetoUnresolved` en lugar de un sujeto vacío.
   *Fuente:* `internal/sesion/errors.go` (E4-T3, Parte A).
4. **(D)** IF el `sujeto` correspondiente está desactivado, THEN THE `SujetoResolver` SHALL NOT resolverlo.
   *Fuente:* G-6.

### Requirement 12: Verificación sin IdP real

**User Story:** Como equipo de desarrollo, quiero poder probar el gobierno de identidad sin un Keycloak real, para validar consistencia y atomicidad en pruebas rápidas.

#### Acceptance Criteria

1. **(F)** THE IdP_En_Memoria SHALL implementar las operaciones de administración de usuario y grupo del Puerto_IdP y cumplir la misma suite de contrato (`idptest`) que el adaptador real.
2. **(F)** THE pruebas de atomicidad y de rechazo SHALL ejecutarse contra Postgres real (`IDENTIDAD_TEST_DB`) y un doble del Puerto_IdP, y SHALL declararse **no verificadas** cuando la variable falte (`t.Skip`).
3. **(F)** THE pruebas contra Keycloak (`-tags integration`) SHALL ejecutarse con Docker y el JAR de E4-T1; sin ellos solo se verifica que compilen (`go vet -tags integration ./...`).

### Requirement 13: Datos semilla — Tenant AI27 y primer administrador

**User Story:** Como equipo de operaciones, quiero que un despliegue nuevo nazca con el tenant interno y el primer administrador, para poder usar el endpoint de alta sin depender de un administrador previo.

#### Acceptance Criteria

1. **(D)** THE migración de despliegue SHALL crear una **tabla de tenants** y sembrar en ella el Tenant_AI27, de forma idempotente, como se siembran los ocho roles; `sujeto.tenant_id` SHALL referenciar esa tabla.
   *Fuente:* C3 (tabla de tenants con integridad referencial; el tenant vive en `identidad`, no en `catalogos`). *Dato de tercero:* el UUID del Tenant_AI27 lo fija Arquitectura.
2. **(D)** THE migración de despliegue SHALL sembrar el primer administrador (sujeto activo con rol `administrador` en el Tenant_AI27), de forma idempotente, usando el `sub` fijo del administrador que trae el realm importado de Keycloak.
   *Fuente:* C4 (realm con admin fijo; enlace determinista). *Dato de tercero:* el valor del `sub` fijo lo fijan Arquitectura/Operaciones al construir el realm.
3. **(D)** THE siembra del primer administrador SHALL registrar en la Traza_Gobierno el Actor_Sistema como actor.
   *Fuente:* C4; Requisito 1.9. *Dato de tercero:* el identificador del Actor_Sistema.
4. **(F)** THE objetos que cree la migración SHALL declarar sus `GRANT` mínimos y su caso en `grants_integration_test.go`.
   *Fuente:* nota de `0002_grants.sql`.
5. **(D)** THE tabla de tenants SHALL poder alojar también el tenant de cada cliente (el que su organización determina, E4-HU-018), para no duplicar la frontera entre `identidad` y `catalogos`.
   *Fuente:* C3 ("a confirmar" que el tenant de clientes vive en esta misma tabla); el repo de contexto fija que el tenant del cliente vive en `identidad`.

## Qué queda fuera de este spec

| Tema | Motivo / dónde vive |
| --- | --- |
| Alcance por `unidad` y `cliente`: recurso asignado, jerarquías Supervisor/Coordinador, eventos de alcance, "alcance" como campo editable | Vínculo Monitorista↔`unidad` sin definir (DEC-045 notas, DEC-053 #3) |
| Revocar rol como acción suelta | **Cerrado por C2:** no existe; cambiar = reemplazo, retirar = desactivar |
| Login, refresco, emisión, `ExpiryPolicy`, auditoría con actor de sesión | E4-T3 Parte B. Este spec entrega el habilitador (Requisito 11) |
| Revocación masiva de sesiones | Pospuesta a E4-T8 (DEC-088 A-6). No se necesita para desactivar (una sesión viva por sujeto, A-2) |
| Consumo de `rol.*` / `sujeto.*` por `mensajeria`, `reportes`, `catalogos` | Otros servicios; aquí solo el stub (8.6) |
| Cambios en el JAR, mappers o realm de Keycloak | E4-T1 (salvo la cuenta separada de 7.4, que se coordina ahí) |
| Publicar la superficie en `pf-pkg-contracts` | Otro repo (G-2); se registra, no se hace desde aquí |
| Textos de mensajes de error | Pendiente de producto |

## Propuestas de Kiro (no aprobadas)

Ninguna entra a la ruta productiva sin aprobación. No son criterios.

- **K-1.** Que `tipo` de `cambio_identidad` use los mismos nombres canónicos que los Hechos_Gobierno (el comentario de `0001` ya cita `'rol.asignado'`).
- **K-2.** Índice `(sujeto_id, ocurrido_en)` en migración nueva, y constraint de un solo rol activo como `UNIQUE (sujeto_id) WHERE revocado_en IS NULL` en otra, cada una con su caso de grants si aplica.
- **K-3.** Desactivar a un sujeto ya inactivo, o revocar una asignación inexistente o ya revocada, idempotente: sin fila de traza ni evento nuevos (P13).
- **K-4.** Operaciones de administración del Puerto_IdP idempotentes y reintentables, como `Revoke`.
- **K-5.** Historia de un sujeto paginada por cursor, como `listarSesionesRevocadas`.
- **K-6.** Que la revocación de sesión por desactivación (3.4) comparta transacción con el estado y la traza, con un camino interno distinto de `DELETE /sesiones/{id}` (que solo permite al dueño, DEC-088 A-9).
- **K-7.** Para la sincronización del IdP (Requisito 7.7), concretar la espera creciente como 1 s, 2 s, 4 s (patrón de DEC-088 A-7) y, en el Reemplazo, quitar el grupo anterior **antes** de agregar el nuevo en el IdP para no exponer dos grupos (7.6).

## Dependencias y límites

- **Depende de:** E4-T1 (puerto y realm), E4-T2 (modelo; este spec cierra la duda de unicidad), E4-T3 (outbox, `SujetoResolver`, revocación de sesión, centinelas), E0-T2, E0-T10.
- **Bloquea:** el login de E4-T3 Parte B (Requisito 11) y el consumo de `rol.*`/`sujeto.*`.
- **Punto de partida:** se puede construir ya lo decidido y sin wire (índice de la traza, constraint de un solo rol activo, tabla de tenants con su forma, repos de desactivar/reactivar/revocar, escritura de la traza, atomicidad estado + traza + outbox, resolver `IdpSubject → SujetoID`, lógica del Reemplazo local). No se construye endpoints ni serialización de eventos (G-2); las semillas (Requisito 13) esperan el UUID del tenant, el `sub` fijo del admin y el identificador de Actor_Sistema (datos de tercero).
- **Reconciliación aparte:** DEC-088 A-1 (workspace) quedó obsoleta por DEC-085 (contexto); afecta la emisión de E4-T3, no a T6. Ver `DECISIONES-TOMADAS.md`.
- **Regla de oro del discovery:** ante cualquier duda, preguntar antes de definir.
