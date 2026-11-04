# Decisiones tomadas — cuestiones abiertas de E4-T6

**Gobierno y traza de cambios administrativos** · `pf-svc-identidad`
**Fecha:** 2026-10-16
**Verificado contra:** contexto `ai27-platform-context` @ `bd8901d` (al día con `origin/main`) y contrato `pf-pkg-contracts` tag `v0.1.0-tracer.6` (último publicado).

Este documento cierra las seis cuestiones de `CUESTIONES-ABIERTAS.md`. Cada una lleva la decisión del usuario, la fuente que la respalda y lo que queda pendiente de un tercero.

---

## C1 — Orden y reintento en el reemplazo de rol

**Decisión.** Orden: **todo local en una sola transacción** (revoca + asigna + traza + eventos), y después sincronizar Keycloak. Reintento: **espera creciente, máximo 3 intentos** (1 s, 2 s, 4 s).

**Respaldo.** Es el mismo principio que **DEC-085** (DECIDIDA 2026-10-14): la emisión registra local primero y toca el IdP después, de forma idempotente y reintentable. El rol efectivo se resuelve en servidor y el token lleva uno solo (DEC-053), así que la ventana en Keycloak no afecta la autorización. Como ya se escribió local, un error al sincronizar el IdP no revierte el cambio local; la sincronización queda reintentable.

**Ya no bloquea el diseño.**

---

## C2 — Quitar un rol sin asignar otro

**Decisión.** **No se permite** dejar a un usuario activo sin rol. Cambiar de rol = reemplazo (C1); retirar el acceso = desactivar al usuario.

**Respaldo.** Evita el estado "activo sin rol", que rompe la composición de claims (el token exige exactamente un rol, DEC-053 / `ErrRolNoUnico`). Y evita reabrir la contradicción entre E4-HU-020 Q63 (30 s) y DEC-031 #2 (siguiente refresco), porque ya no existe el caso "quitar sin reemplazo". Decisión de negocio del usuario.

**Cierra la cuestión R-3.**

---

## C3 — Tenant de los usuarios internos

**Decisión.** **Tabla de tenants**, con el tenant interno sembrado por migración con un UUID fijo, igual que los ocho roles. Respeta el `NOT NULL` de `sujeto.tenant_id` y da integridad referencial.

**Respaldo.** Confirmado que el tenant vive en `identidad`, no en `catalogos`: el repo de contexto dice "Cliente vive en E4 (identidad/tenant), no en catálogos"; `catalogos` es dueño del **expediente comercial** del cliente, no del tenant. La tabla de tenants en `identidad` no duplica frontera.

**Pendiente de Arquitectura:** el **UUID concreto** del tenant interno. **A confirmar:** que el tenant de clientes vive en esta misma tabla (coherente con "el cliente determina su tenant", E4-HU-018), para no duplicar la frontera entre identidad y catálogos.

---

## C4 — Enlace del primer administrador

**Decisión.** El realm de Keycloak se **importa con el primer administrador ya creado** y un `sub` fijo conocido; la migración de despliegue usa ese valor.

**Respaldo.** Es la única opción determinista y sin acople de orden entre Keycloak y la migración. Encaja con que el realm de V1 ya se importa como artefacto (E4-T1). El `sub` es opaco (DEC-072), pero al fijarlo en el realm importado deja de ser desconocido.

**Pendiente de Arquitectura / Operaciones:** el **identificador del actor de sistema** para la traza del primer alta (el Requisito 1.9 exige actor no nulo) y el valor del `sub` fijo del admin en el realm.

---

## C5 — Reactivación de un usuario

**Decisión.** La reactivación publica un **hecho nuevo** ("usuario reactivado") con su **propia fila de traza**.

**Respaldo.** La traza es de solo inserción: reactivar no es crear (reusar `sujeto.creado` falsearía la historia) ni se puede prohibir (obligaría a altas duplicadas que chocan con el `idp_subject` existente). Decisión de negocio del usuario.

**Pendiente de contrato:** el hecho "usuario reactivado" es un evento nuevo; entra en el mismo paquete que `sujeto.creado` / `sujeto.desactivado` cuando el contrato publique la superficie de gobierno (brecha G-2).

**Cierra la parte de R-4(ii).**

---

## C6 — Consumidores del hecho de alta/baja

**Decisión.** No la cierra el usuario: es la discrepancia de fuentes que resuelven el **dueño de contratos + PM**. **Recomendación: Opción B** (solo `mensajeria` y `reportes`), marcada **"a confirmar"**, no en silencio.

**Razón de la recomendación.** `identidad.md` (f) mete a `catalogos` como consumidor de `sujeto.*` solo "cuando el sujeto está referenciado por un custodio". En V1 los custodios **no son usuarios de la plataforma** (el subsistema de evidencia app-custodio/panel-analista es un carril aparte sobre Firebase, DEC-048), así que esa condición hoy no se cumple y `catalogos` no tendría qué consumir.

**Nota de rigor:** la premisa "custodios no son usuarios en V1" se sostiene en el product-context y en DEC-048 (subsistema de evidencia adelantado, con la app de custodio diferida). Si esa premisa cambia, se reabre C6.

**Sigue abierta formalmente** hasta que el dueño de contratos confirme. No bloquea la lógica local de T6.

---

## Consecuencia transversal: DEC-088 (workspace) quedó obsoleta en su punto A-1

Al verificar C1 contra el contexto al día apareció una contradicción entre repos que hay que registrar:

- **DEC-085** (repo de contexto, DECIDIDA 2026-10-14): la emisión registra la fila **provisional primero** (local), luego llama al IdP, luego la completa. **Descarta por nombre** la opción "IdP primero, compensar".
- **DEC-088 A-1** (`.kiro/steering/decision-log.md` de este **workspace**): dice que la emisión va con "opción 1 — emitir primero, compensar si falla el registro". Eso es el orden **inverso** y el que DEC-085 descartó.

Como el repo de contexto es la fuente de verdad (steering `contexto-plataforma.md`), **prevalece DEC-085**: la emisión de E4-T3 Parte B va con fila provisional primero. **DEC-088 A-1 queda obsoleta** y debe reconciliarse. No afecta a E4-T6 salvo que confirma el principio "local primero" que usa C1; sí afecta a quien implemente el login de E4-T3.

---

## Choque de numeración de DEC entre repos

- El repo de contexto (fuente de verdad) llega a **DEC-087** ("Dueño de `reportes`").
- El workspace usa **DEC-087** ("Segundo factor en contrato") y **DEC-088** ("Resolución de E4-T3 Parte B") para cosas distintas.

Las entradas DEC nuevas que salgan de estas decisiones deben numerarse **desde DEC-088 en adelante sobre la base del contexto**, y antes de registrar hay que reconciliar el DEC-087/088 del workspace. Ver `ENTRADAS-DEC-PROPUESTAS.md`.

---

## Qué queda pendiente de terceros (no bloquea la lógica local)

| Dato | Quién | Para qué |
| --- | --- | --- |
| UUID del tenant interno | Arquitectura | Migración de semilla (C3) |
| `sub` fijo del primer admin en el realm | Arquitectura / Operaciones | Migración de semilla (C4) |
| Identificador de actor de sistema | Arquitectura / Operaciones | Traza del primer alta (C4, Requisito 1.9) |
| Confirmar consumidores de `sujeto.*`/`rol.*` | Dueño de contratos + PM | Contrato (C6) |
| Publicar la superficie de gobierno en el contrato | Dueño de contratos | Endpoints y eventos (brecha G-2) |
| Reconciliar DEC-088 A-1 con DEC-085 | PM / Arquitectura | Emisión de E4-T3 Parte B |
