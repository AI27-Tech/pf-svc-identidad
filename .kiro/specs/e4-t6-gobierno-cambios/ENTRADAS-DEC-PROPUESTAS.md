# Entradas DEC propuestas — gobierno de identidad (E4-T6)

**Fecha:** 2026-10-16
**Para registrar en:** `repos/ai27-platform-context/.kiro/steering/decision-log.md`

Estas entradas formalizan las decisiones G-1 a G-11 y las cuestiones C1–C6 que aún **no** tienen un DEC propio. Kiro no escribe en el repo de contexto sin que lo pidas: aquí quedan redactadas para que las registres.

## Antes de registrar: reconciliar la numeración

- El repo de contexto (fuente de verdad) llega a **DEC-087** ("Dueño de `reportes`").
- El workspace local (`.kiro/steering/decision-log.md`) usa **DEC-087** ("Segundo factor") y **DEC-088** ("Resolución de E4-T3 Parte B") para cosas **distintas**. Ese choque hay que resolverlo (renumerar las del workspace o fusionarlas) **antes** de asignar números nuevos.
- Las propuestas de abajo usan números tentativos **DEC-088 en adelante sobre la base del contexto**. Ajustar al numerar.

---

## DEC-088 (propuesto) — Correlación `IdpSubject → SujetoID` resuelta por E4-T6

**Estado:** propuesto · **Owner:** Arquitectura · **Fecha:** 2026-10-16
**Fuente:** DEC-072 (opción c, `sub` opaco); handoff E4-T3→E4-T6; G-1.

**Decisión.** El mecanismo que DEC-072 dejó pendiente ("vive con el alta/sincronización de usuarios de E4-T6") es: `identidad` resuelve el `SujetoID` consultando el `sujeto` local por su `idp_subject`. Sin claim nuevo, sin alinear identificadores y sin tocar `pf-pkg-platform-go`. Es el `SujetoResolver` que desbloquea el login de E4-T3.

**Impacto.** E4-T6 Requisito 11; E4-T3 Parte B (login).

---

## DEC-089 (propuesto) — Un solo rol activo por sujeto; cambio de rol = reemplazo (cierra DEC-084)

**Estado:** propuesto · **Owner:** Product Management + Arquitectura · **Fecha:** 2026-10-16
**Fuente:** DEC-084 (PENDIENTE); DEC-053 (un rol en el token); G-5; C2.

**Decisión.**
1. Un sujeto tiene **a lo sumo un rol activo**, garantizado por constraint de base de datos.
2. Cambiar de rol es un **reemplazo**: revoca el anterior y asigna el nuevo, con dos filas de traza y dos hechos.
3. **No se asigna** rol a un sujeto desactivado.
4. **No se permite** dejar a un sujeto activo sin rol: para retirar el acceso se desactiva (C2). Así no se reabre la contradicción E4-HU-020 Q63 / DEC-031 #2.

**Cierra DEC-084**, que estaba PENDIENTE. Deja sin efecto su "propuesta de Kiro" de fallar cerrado por doble grupo (queda como defensa en profundidad).

**Impacto.** E4-T6 Requisitos 4 y 5; E4-T2 (constraint de unicidad más estricto que el que su diseño dejó abierto).

---

## DEC-090 (propuesto) — Orden de escritura IdP ↔ local en gobierno; reemplazo local primero

**Estado:** propuesto · **Owner:** Arquitectura · **Fecha:** 2026-10-16
**Fuente:** DEC-085 (emisión, local primero); G-3; C1.

**Decisión.** El orden entre IdP y modelo local en los cambios de gobierno:
- **Retiro de acceso** (desactivar): local primero, IdP después.
- **Otorgamiento** (alta, asignar, reactivar): IdP primero, local después.
- **Reemplazo de rol**: todo local en una sola transacción (revoca + asigna + traza + eventos), luego sincroniza el IdP. Mismo principio que DEC-085.
- La sincronización del IdP es reintentable con espera creciente, **máximo 3 intentos**; un fallo no revierte el cambio local.

**Impacto.** E4-T6 Requisitos 4, 5, 7.

---

## DEC-091 (propuesto) — Cuenta de servicio separada para gobierno del IdP

**Estado:** propuesto · **Owner:** Seguridad · **Fecha:** 2026-10-16
**Fuente:** DEC-073 (la cuenta de revocación no tiene `manage-users`); G-4.

**Decisión.** `identidad` usa una **cuenta de servicio separada** para administrar usuarios y grupos, distinta de la que revoca sesiones. La prueba de E4-T1 que exige que la cuenta de revocación no pueda modificar usuarios debe seguir pasando.

**Impacto.** E4-T6 Requisito 7.4; E4-T1 (realm).

---

## DEC-092 (propuesto) — Tenant AI27 de usuarios internos y tabla de tenants

**Estado:** propuesto · **Owner:** Arquitectura · **Fecha:** 2026-10-16
**Fuente:** E4-HU-018 (el cliente determina su tenant); G-9; C3.

**Decisión.** Los siete roles internos pertenecen a un **tenant AI27 único**, sembrado por migración en una **tabla de tenants** de `identidad` (el tenant del cliente también vive ahí; `catalogos` guarda el expediente comercial, no el tenant). **Pendiente:** el UUID concreto, que fija Arquitectura.

**Impacto.** E4-T6 Requisitos 2.4 y 13.

---

## DEC-093 (propuesto) — Reactivación de sujeto como hecho propio

**Estado:** propuesto · **Owner:** Product Management · **Fecha:** 2026-10-16
**Fuente:** append-only de la traza (ADR 26 §7); G-8; C5.

**Decisión.** Reactivar un sujeto desactivado publica el hecho **`sujeto.reactivado`** (nuevo) y deja su propia fila de traza. No reusa `sujeto.creado` ni obliga a un alta duplicada.

**Impacto.** E4-T6 Requisito 3.7–3.8 y 8.1; contrato (evento nuevo).

---

## DEC-094 (propuesto) — Primer administrador por semilla con `sub` fijo del realm

**Estado:** propuesto · **Owner:** Arquitectura + Operaciones · **Fecha:** 2026-10-16
**Fuente:** G-11; C4; DEC-072 (`sub` opaco).

**Decisión.** El primer administrador se siembra por migración de despliegue, usando el `sub` fijo del administrador que trae el **realm importado** de Keycloak. La traza de ese primer alta usa un **identificador de actor de sistema** fijo. **Pendiente:** el valor del `sub` fijo y el del actor de sistema.

**Impacto.** E4-T6 Requisito 13.

---

## DEC-095 (propuesto) — Autoescalamiento y automodificación de administrador

**Estado:** propuesto · **Owner:** Product Management · **Fecha:** 2026-10-16
**Fuente:** E4-HU-018 (Escenario 9); G-7.

**Decisión.** Un no administrador no puede modificar su propio rol (rechazo antes del IdP). Un administrador **sí** puede modificar su propio rol o desactivarse, **salvo** que sea el último administrador activo.

**Impacto.** E4-T6 Requisito 9.

---

## Pendiente de otro dueño (no es DEC de identidad)

- **Consumidores de `sujeto.*`/`rol.*` (C6):** lo resuelve el dueño de contratos + PM al publicar la superficie de gobierno. Recomendación: solo `mensajeria` y `reportes` (los custodios no son usuarios en V1, DEC-048), a confirmar.
- **Reconciliar DEC-088 A-1 (workspace) con DEC-085 (contexto):** la emisión de E4-T3 va con "fila provisional primero" (DEC-085), no "emitir primero, compensar" (DEC-088 A-1, obsoleta).
