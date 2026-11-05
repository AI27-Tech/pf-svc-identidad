# DECISIONES — E4-T6
## Gobierno y traza de cambios administrativos

**Preparado para:** Product Manager, Tech Lead
**Spec:** E4-T6 (`identidad`) · rama compartida con E4-T3 (`feature/e4-t3-event-snapshot`)
**Fecha:** 2026-10-16
**Contexto consultado:** `repos/ai27-platform-context` en el commit `913380e` (`origin/main`, árbol limpio, al día)
**Estado:** las 11 decisiones G-1 a G-11 **ya están tomadas** (comunicadas por el usuario). Quedan **8 puntos residuales** (sección final) que las decisiones dejaron abiertos. `design.md` y `tasks.md` se pueden generar cuando se resuelvan los marcados "bloquea el diseño".

> Las decisiones usan el prefijo **G-** (gobierno) para no colisionar con las **A-** de E4-T1 y E4-T3.
>
> **Estado de registro.** Ninguna de las decisiones de este documento figura aún en `decision-log.md` del repo de contexto como DEC propia. Hasta que se registren, se citan como "decidida, pendiente de registro". Donde ya existe un DEC que las respalda, se indica.

---

## Corrección a la versión anterior de este documento

La versión anterior se apoyó en la copia de `decision-log.md` de este workspace, que **estaba desactualizada** (le faltaban DEC-067 a DEC-086). Con el repo de contexto al día se corrige:

- **G-1 no estaba abierta:** **DEC-072** (DECIDIDA, 2026-10-09) ya eligió la opción (c).
- **G-4 ya tenía respaldo:** **DEC-073** descartó `manage-users` para la cuenta de revocación.
- **G-5 es DEC-084**, que en el repo de contexto sigue **PENDIENTE** (sin decisión escrita). La decisión de abajo la resuelve, y debe registrarse ahí.
- **"A-13" se usó con dos significados** (correlación `sub` ↔ `SujetoID`, y "usuario en más de un grupo de rol"). Quedan separados como G-1 y G-5.

---

## Resumen de decisiones

| # | Tema | Decisión | Respaldo |
|---|------|----------|----------|
| G-1 | Correlación del sujeto | **(c)** resolver en servidor por `IdpSubject`. E4-T6 diseña el resolver `IdpSubject → SujetoID` | **DEC-072** (DECIDIDA) |
| G-2 | Brecha contractual | **Opción 2:** avanzar lógica local, repos, índice y traza sin wire; diferir endpoints y eventos hasta que salga el contrato. Nombres de eventos confirmados | Decidida, pendiente de registro |
| G-3 | Consistencia IdP ↔ local | **Por operación:** retiro de acceso (revocar rol, desactivar) → local primero; otorgamiento (alta, asignar) → IdP primero | Decidida, pendiente de registro. Caso de emisión: DEC-085 (opción 1) |
| G-4 | Cuenta de servicio del IdP | **(b)** cuenta separada para gobierno | Coherente con **DEC-073** |
| G-5 | Cardinalidad de rol | **(b) reemplaza**; no se asigna a desactivado; **un solo rol activo por sujeto** garantizado por constraint | Resuelve **DEC-084** (PENDIENTE); pendiente de registro |
| G-6 | Efecto en sesiones vivas | **(c) híbrida:** desactivación → cierre inmediato (≤30 s); cambio de rol → siguiente refresco. Desactivado no vuelve a loguearse | Decidida, pendiente de registro. Concilia E4-HU-020 con DEC-031 #2 |
| G-7 | Automodificación de admin | **(c)** permitido salvo que sea el último administrador activo | Decidida, pendiente de registro |
| G-8 | Edición y traza | Editables: nombre, identificador, estado, rol, alcance. Traza solo rol y estado. Alta con rol inicial = **dos filas** | Decidida, pendiente de registro |
| G-9 | Tenant de internos | **Un tenant AI27 único** para los 7 roles internos, sembrado en migración. UUID lo fija Arquitectura | Decidida, pendiente de registro |
| G-10 | Traza: retención y consulta | **Retención indefinida**; consulta solo `administrador` en V1 | Coherente con **DEC-036** |
| G-11 | Primer administrador | **(a)** semilla de despliegue (migración SQL). Actor de traza: identificador fijo de sistema/bootstrap | Decidida, pendiente de registro |

---

## Detalle por decisión

### G-1 — Correlación del sujeto (DEC-072)

**Decisión:** opción (c). El `sub` de Keycloak es **opaco**; `identidad` resuelve el `SujetoID` por `IdpSubject` en servidor. Ni se alinean ids (a) ni se agrega un claim `sujeto_id` (b).

**Qué implica para E4-T6:**
- No se toca `pf-pkg-platform-go`.
- El alta **no** fija el `id` del usuario en Keycloak. El `idp_subject` del sujeto local es el `sub` que el IdP asigna al crear el usuario.
- DEC-072 dice que **no fija el mecanismo** de `IdpSubject → SujetoID`: "vive con el alta/sincronización de usuarios de E4-T6". Ese mecanismo es lo que E4-T6 diseña (Requisito 11).
- Ese resolver es lo que desbloquea el login de E4-T3 Parte B.

---

### G-2 — Brecha contractual

**Decisión:** opción 2.
- **Se avanza ya, sin wire:** lógica local, repos, migración del índice, traza, atomicidad.
- **Se difiere hasta que salga el contrato:** endpoints `/api/v1/sujetos` y `/api/v1/roles`, y la serialización de eventos.
- **Nombres de eventos confirmados:** `sujeto.creado`, `sujeto.desactivado`, `rol.asignado`, `rol.revocado`.
- **Acción pendiente (no bloquea):** resolver **con el dueño de contratos** si `catalogos` es consumidor de `sujeto.*`. Las fuentes discrepan (E4-T6 lista `mensajeria` y `reportes`; `identidad.md` (f) añade `catalogos`). **No se elige en silencio.** → Residual R-1.

---

### G-3 — Consistencia IdP ↔ modelo local

**Decisión:** el orden depende del tipo de operación.

| Operación | Tipo | Orden |
|-----------|------|-------|
| Revocar rol | Retiro de acceso | **Local primero** (estado + traza + outbox), luego IdP |
| Desactivar sujeto | Retiro de acceso | **Local primero**, luego IdP |
| Alta de sujeto | Otorgamiento | **IdP primero**, luego local |
| Asignar rol | Otorgamiento | **IdP primero**, luego local |

**Razón:** quien pierde acceso debe perderlo ya, aunque el IdP tarde en enterarse; quien lo gana no debe aparecer como activo en AI27 si el IdP no lo aceptó.

**Emisión con registro fallido:** es DEC-085. Decisión: **opción 1** (no entregar el token y revocar en el IdP de forma reintentable). Es asunto de E4-T3; DEC-085 sigue **PENDIENTE** en el repo de contexto y debe registrarse.

**Lo que la decisión no dice** (→ Residual R-2): el orden en el **reemplazo de rol** (mezcla retiro y otorgamiento), y la política de reintento/compensación del segundo paso.

---

### G-4 — Cuenta de servicio del IdP

**Decisión:** opción (b), **cuenta separada** para gobierno. Es lo coherente con DEC-073: la cuenta de revocación no puede tener `manage-users`, y la opción (a) obligaba a reabrir DEC-073.

**Qué implica:** una segunda cuenta de servicio en el realm, con su propio secreto y permisos de administración de usuarios y grupos. El realm es artefacto de **E4-T1**: el cambio se coordina ahí. La prueba de E4-T1 que exige que la cuenta de revocación **no pueda** modificar usuarios debe seguir pasando.

---

### G-5 — Cardinalidad de rol (resuelve DEC-084)

**Decisión:**
1. **Reemplaza (b):** asignar un rol a quien ya tiene otro **revoca el anterior y asigna el nuevo**, con **dos filas de traza**. Mejor administración con auditoría completa. Se descarta (c) (varias filas con token fallido): deja usuarios sin acceso en silencio.
2. **No se asigna rol a un sujeto desactivado.**
3. **Un solo rol activo por sujeto**, garantizado por constraint de BD (no solo único por `(sujeto_id, rol_codigo)`), porque el token lleva un rol (DEC-053).

**Qué implica:**
- Este constraint **es más estricto** que el que el diseño de E4-T2 dejó abierto (`UNIQUE (sujeto_id, rol_codigo) WHERE revocado_en IS NULL`). Cierra esa duda del lado de la restricción fuerte.
- En el IdP, el rol sale del atributo `rol` del **grupo** (DEC-083 #4), así que un reemplazo es mover al usuario de grupo. Si en algún instante el usuario queda en **dos** grupos, el mapper de un solo valor toma uno sin criterio (el riesgo que describe DEC-084). → Residual R-2.
- DEC-084 debe pasar a DECIDIDA con este texto. La "propuesta de Kiro" que trae (que la emisión falle cerrada si hay dos grupos) sigue **sin aprobar**: sirve como defensa en profundidad.

---

### G-6 — Efecto sobre sesiones vivas

**Decisión:** opción (c) híbrida.
- **Desactivación → cierre inmediato** (acceso terminado en máximo 30 s).
- **Cambio de rol → siguiente refresco** (DEC-031 #2).
- **Un sujeto desactivado no puede volver a loguearse.**
- Criterio: "**retira acceso**" corta ya; "**cae permiso**" espera al token.

**Qué implica:**
- **No exige revocación masiva** (que DEC-088 A-6 pospuso a E4-T8): con **una sola sesión viva por sujeto** (DEC-088 A-2), desactivar revoca a lo sumo una sesión con el mecanismo de revocación de E4-T3. **Si E4-T9 permite varias sesiones, esto se reabre.** *(Derivación de Kiro, no decisión.)*
- `DELETE /sesiones/{id}` solo lo permite al dueño de la sesión (DEC-088 A-9). La revocación por desactivación es **administrativa**: necesita un camino interno, no el endpoint público. → decisión de diseño, no del PM.
- La contradicción que E4-HU-020 deja marcada como "conflicto abierto" (Escenario 3 frente a DEC-031 #2) queda resuelta **para desactivar y para cambio de rol**, pero no para el caso del residual R-3.

---

### G-7 — Automodificación del administrador

**Decisión:** opción (c). Un administrador puede modificar su propio rol o desactivarse, **salvo que sea el último administrador activo**.

**Qué implica:** cuenta de administradores activos antes de aplicar el cambio. Dos administradores que se desactivan a la vez pueden dejar el sistema sin ninguno si no hay control de concurrencia: el diseño debe cubrirlo.

---

### G-8 — Edición y traza

**Decisión:**
- **Editables:** nombre, identificador, estado, rol, alcance.
- **Traza solo para rol y estado** (los que afectan acceso). La edición de nombre no deja traza.
- **Alta con rol inicial = dos filas** de traza: `sujeto.creado` + `rol.asignado`, coherente con "exactamente una fila por asignación de rol".

**Qué implica:**
- El alta con rol produce **dos hechos** en el outbox (`sujeto.creado` y `rol.asignado`), en la misma transacción.
- El `idp_subject` **no es editable** (es el `sub` opaco del IdP; DEC-072). "Identificador" se refiere al identificador de acceso del usuario.
- El **tenant no figura entre los editables** (es inmutable en el IdP por E4-T1).
- Dos asuntos que la decisión deja abiertos → Residual R-4 (alcance editable y reactivación).

---

### G-9 — Tenant de internos

**Decisión:** sí, **un tenant AI27 único** para los 7 roles internos, **sembrado en migración** como los 8 roles. **El UUID lo fija Arquitectura.**

**Qué implica:** el alta de un rol interno usa ese tenant. Dos puntos abiertos → Residual R-5 (el valor, y dónde se siembra: hoy no existe tabla de tenants en `identidad`).

---

### G-10 — Traza: retención y consulta

**Decisión:** **retención indefinida** (es gobierno/auditoría, ya decidido que sea de solo inserción). **Consulta solo `administrador`** en V1; nadie más hasta que exista un rol de auditoría (no existe en V1, DEC-036).

**Qué implica:** sin purga ni plazo numérico. Sin cambios de privilegios (`0002` ya deja `SELECT, INSERT`).

---

### G-11 — Primer administrador

**Decisión:** opción (a), **semilla de despliegue** (migración SQL, como los 8 roles). El actor de la traza del primer alta es **un identificador fijo de sistema/bootstrap**, porque el Requisito 1.9 exige actor no nulo.

**Qué implica:** hay que **definir ese identificador** y resolver cómo la semilla se enlaza con el usuario real de Keycloak. → Residual R-6.

---

## Puntos residuales (lo que las decisiones dejaron abierto)

> **Actualización 2026-10-16.** Varios residuales que aquí figuraban como "bloquea
> el diseño" **ya fueron resueltos** en `DECISIONES-TOMADAS.md` (C1–C5) y están
> **implementados** en `internal/gobierno/servicio.go`. Lo que de verdad sigue
> abierto son sólo los **valores de tercero** de R-5/R-6 (UUIDs/`sub`/actor de
> sistema para las semillas), no la forma de la decisión. La columna "¿Bloquea?"
> se corrige abajo.

| # | Pregunta | Quién | ¿Bloquea? |
|---|----------|-------|-----------|
| **R-1** | ¿`catalogos` es consumidor de `sujeto.*`? Las fuentes discrepan | Dueño de contratos | No (afecta el contrato, no la lógica local) |
| **R-2** | **Reemplazo de rol y reintento.** (i) El reemplazo mezcla retiro (local primero) y otorgamiento (IdP primero): ¿qué orden sigue? (ii) ¿Política de reintento/compensación del segundo paso: cuántos intentos, qué ve el cliente si se agotan? | PM + Tech Lead | **CERRADO por C1** (todo local en una tx, luego IdP; reintento máx 3, 1s/2s/4s). Implementado en `servicio.go` (`ReemplazarRol` + `sincronizarIdp`). Ya no bloquea |
| **R-3** | **Revocar un rol sin reemplazo.** (i) ¿Se permite dejar a un sujeto activo sin rol? (ii) Si sí, ¿su sesión viva se corta ya (E4-HU-020 Escenario 3, ≤30 s) o espera al refresco? Con el criterio "retira acceso corta ya" parecería lo primero, pero G-6 solo nombra "desactivación" y "cambio de rol" | PM | **CERRADO por C2** (no se permite dejar a un sujeto activo sin rol; solo existe reemplazo). Ya no bloquea |
| **R-4** | **Edición.** (i) "Alcance" figura como editable, pero el vínculo Monitorista↔`unidad` sigue sin definir (DEC-045) y no hay tabla ni columna. ¿Se edita en E4-T6? (ii) "Estado" editable implica **reactivar**: ¿se permite, qué hecho publica (hoy solo existe `sujeto.desactivado`) y qué fila deja en la traza? (iii) Editar identificador queda sin traza por la regla "solo rol y estado": ¿está bien? | PM | (ii) **CERRADO por C5** (reactivar permitido; hecho `sujeto.reactivado`; implementado en `ReactivarSujeto`). (i) y (iii) no bloquean |
| **R-5** | **Tenant AI27.** (i) UUID (Arquitectura). (ii) ¿Dónde se siembra? | Arquitectura | **Forma CERRADA por C3** (tabla `identidad.tenant`, `0006`, con FK a `sujeto.tenant_id`). Sigue pendiente **solo el valor**: el UUID real del tenant interno (semilla `0007`, dato de Arquitectura) |
| **R-6** | **Primer administrador.** (i) Valor del identificador de actor de sistema. (ii) El `sub` del primer admin es opaco (DEC-072): ¿la semilla lo conoce porque el realm importado fija el usuario, o se registra al primer login? | Arquitectura + Operaciones | **Forma CERRADA por C4** (semilla de despliegue con `sub` fijo del realm). Sigue pendiente **solo los valores** de tercero (`sub`, actor de sistema, UUID bootstrap) para la semilla |
| **R-7** | **Registro de las decisiones.** G-2 a G-11 (menos las respaldadas) no figuran como DEC; **DEC-084 y DEC-085 siguen PENDIENTES** en el repo de contexto | PM | No |
| **R-8** | **Numeración de DEC.** En este workspace `DEC-087` es "Segundo factor en contrato de emisión (A-15)" y `DEC-088` es la resolución de E4-T3; en el repo de contexto `DEC-087` es "Dueño de `reportes`" y **no existe DEC-088**. El mismo número significa dos cosas | PM / Lead | No, pero hay que reconciliarlo antes de registrar |

---

## Orden sugerido

1. **R-2, R-3** (definen los flujos de rol y desactivación del diseño).
2. **R-5, R-6** (definen la migración de semillas).
3. **R-4(ii)** (reactivación).
4. **R-1, R-7, R-8** (se pueden resolver en paralelo).

## Qué hace Kiro mientras tanto

- **Sin tocar T3.** Sus criterios no cambian.
- **Se puede construir ya** (decidido y sin wire): migración del índice de la traza; constraint de un solo rol activo; repos de desactivar sujeto y revocar asignación; escritura de la traza; atomicidad estado + traza + outbox; el resolver `IdpSubject → SujetoID` (G-1).
- **No se construye** hasta que salga el contrato: endpoints y serialización de eventos.
- **No se construye** hasta resolver R-5/R-6: semillas de tenant y primer administrador.
