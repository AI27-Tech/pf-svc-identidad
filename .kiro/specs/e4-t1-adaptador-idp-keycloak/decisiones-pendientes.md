# E4-T1 — Decisiones pendientes para el adaptador de Keycloak

**Fecha:** 2026-10-09 (actualizado)
**Fuente:** `design.md` (§Decisiones abiertas), `tasks.md` y `decision-log.md` (DEC-065, DEC-066)
**Para:** Product Management y Arquitectura

Este documento lista **solo lo que falta decidir** para cerrar E4-T1 (el adaptador del IdP contra Keycloak). Las opciones salen del diseño ya revisado; **Kiro no decide ninguna**: el diseño queda preparado para cualquiera de las opciones listadas.

---

## Panorama rápido

| Estado | Decisiones |
| --- | --- |
| ✅ Ya decididas (no bloquean) | A-1, A-3, A-4, A-7, A-15 |
| ⚠️ Recibida, EN EVALUACIÓN (salvedad técnica) | A-2 |
| ⛔ Abiertas, de producto/arquitectura (bloquean el cierre) | **A-13, A-16** |
| 🟡 Abiertas pero "blandas" (el diseño ya tomó una opción por defecto) | A-6, A-8, A-12, A-14 |
| ⚪ Resuelta por Kiro (propuesta técnica, no es de producto) | A-11 |

**Ya ejecutado (2026-10-09):** tareas 10 (telemetría OTel), 12 (cableado en `cmd/api`) y 17 (documentación), verificadas con `go build`, `go vet` y `go test` en verde.

**Qué queda bloqueado:** tarea 14 (artefacto del realm) y tarea 15 (prueba de integración), más sus checkpoints (16, 18).

---

## ⚠️ A-2 — Canal del segundo factor: "por correo" (EN EVALUACIÓN)

**Recibido de PM:** el segundo factor se entregaría **por correo**.

**No se asienta como hecho todavía**, por una salvedad técnica que hay que resolver antes:

- El envío por correo **no es nativo** en Keycloak: requiere una extensión de Keycloak más un proveedor de correo.
- **Reabre DEC-031 #1**, que delegó el segundo factor al IdP asumiendo los canales nativos (TOTP / WebAuthn). Correo y SMS no vienen integrados.

**Qué hay que aclarar:**

| Opción | Compatibilidad | Qué implica |
| --- | --- | --- |
| **TOTP** (app autenticadora) | Nativo, compatible con el grant directo ya elegido (A-3) | Camino directo; sin extensión ni proveedor |
| **Correo** _(lo recibido)_ | No nativo | Extensión de Keycloak + proveedor de correo; reabre DEC-031 |
| **WebAuthn** | Solo flujo de navegador | Forzaría a reabrir A-3 (Authorization Code en vez de ROPC) |

**Decisión que se necesita:** confirmar si se acepta el costo de la extensión + proveedor de correo (y la reapertura de DEC-031), o si el canal pasa a TOTP. Hasta que se resuelva, **no se escribe el flujo del segundo factor en el artefacto del realm** (tarea 14).

**Quién decide:** Product Management + Seguridad.

---

## Decisiones que bloquean el cierre (de producto / arquitectura)

### A-13 — Nombres de claims y relación `sub` ↔ `SujetoID`

- **Qué decide:** el contrato de nombres de claims y si el `sub` del token de Keycloak es el `SujetoID` de AI27.
- **Qué bloquea:** tarea 14 (mapper `sujeto_id` en el realm) y tarea 15 (aserción `esperadoSujeto` en integración).
- **Quién decide:** Arquitectura (ADR/DEC), con relación a E4-T2/E4-T6.

**Opciones para la identidad del sujeto:**

| Opción | Cómo | Qué cambia |
| --- | --- | --- |
| **(a) Alinear ids** | Crear el usuario de Keycloak con `id = sujeto.id`, de modo que `sub == SujetoID == IdpSubject` | Condiciona el alta de usuarios en E4-T6 |
| **(b) Claim propio `sujeto_id`** | Un claim `sujeto_id` desde un atributo de usuario (solo admin); `sub` = id de Keycloak | **Exige una versión nueva de `pf-pkg-platform-go`** para que `auth` lea ese claim |
| **(c) Resolver en servidor** | Los consumidores usan el `sub` de Keycloak; el `SujetoID` se resuelve por `IdpSubject` en servidor | R6.6 compara `sub` con `IdpSubject`, no con `SujetoID` |

Sobre los **nombres de claims**: el diseño usa los que hoy lee `pf-pkg-platform-go/auth` (`sub`, `rol`, `tenant_id`, `sesion_id`, `audiencia`). Ratificarlos como contrato es tarea de ADR/DEC. Si cambian, solo cambian los `claim.name` del artefacto; la prueba pura los detecta. Toda la regla del valor esperado de `sub` está concentrada en un solo lugar (`esperadoSujeto(m)`), así que el diseño queda listo para cualquiera de las tres opciones.

---

### A-16 — Permisos mínimos de la cuenta de servicio para `Revoke`

- **Qué decide:** qué permisos exactos tiene el cliente de servicio `ai27-identidad-svc` para terminar sesiones.
- **Qué bloquea:** tarea 14 (configuración del cliente de servicio en el realm) y tarea 15 (verificación de permisos).
- **Quién decide:** Seguridad.

**Opciones:**

| Opción | Nota |
| --- | --- |
| **FGAP v2** (permisos de grano fino) | La opción más acotada; depende de soporte de la versión (A-7) |
| **`manage-users`** | Más amplio de lo necesario; la prueba verifica que aun así no modifique usuarios |
| **Logout OIDC** | Alternativa por protocolo en vez de API de administración |

El principio es **privilegio mínimo**: la cuenta de servicio solo debe poder cerrar sesiones, nunca modificar usuarios. La prueba de integración 15.5 **falla si la cuenta puede modificar un usuario** (intenta un `PUT` y espera 403). El diseño declara hoy la opción mínima que el import acepte. Pendiente además: registrar A-16 en los pendientes de `requirements.md`.

---

## Pendiente de proceso (no es una decisión técnica nueva)

Aunque **A-3** y **A-4** ya están decididas, quedan omisiones (`t.Skip`) en las pruebas de integración para los casos que exigen una emisión exitosa con OTP y `SesionID != ""`. **PM debe aceptar explícitamente esas omisiones como excepción temporal a R11.4**, o el spec no cierra del todo aun con A-13 y A-16 resueltas.

---

## Decisiones abiertas pero "blandas" (no frenan el avance)

El diseño ya eligió una opción por defecto para cada una. Se listan para que PM/Arquitectura puedan ratificarlas o cambiarlas, pero **no bloquean** porque la costura ya está puesta.

| Decisión | Qué decide | Opción por defecto del diseño | Si se cambia |
| --- | --- | --- | --- |
| **A-6** | De qué historia es dueño el empaquetado del mapper de claims (E4-T1 o E4-T2) | Los mappers viven en el artefacto del realm de este spec | Si es de E4-T2, el scope `ai27-claims` se mueve a un artefacto propio de E4-T2; este spec conserva solo la Propiedad 13 |
| **A-8** | De dónde lee Keycloak el modelo vigente al emitir (réplica en Keycloak o consulta a `identidad`) | (a) réplica: la sincronización queda en E4-T6; la costura está en el artefacto, no en Go | (b) exige un JAR SPI, dependencia de emisión hacia `identidad` y un endpoint interno nuevo |
| **A-12** | Si Keycloak caído debe bloquear `/ready` | Check en registro aparte `/ready/idp`; `/ready` intacto (ya implementado en tarea 12) | Si debe bloquear, el check pasa al registro principal (una línea). Si se quieren checks informativos, versión nueva de platform-go |
| **A-14** | Categorías de error extra (cuenta deshabilitada, cliente mal configurado, falla de factor, cancelación) | Hoy caen en `credencialInvalida` o `proveedorNoDisponible`; `resultado=otro` es provisional | Agregar un centinela o un valor de `resultado` es local a `classify` y `telemetry`; la Propiedad 1 se actualiza con la fila nueva |

---

## Decisión ya resuelta por Kiro (no es de producto)

- **A-11 — ¿Dónde se verifica el aislamiento del IdP?** Resuelta como propuesta técnica: la prueba vive localmente en `internal/archtest` y corre con `go test ./...`. No cambia `arch-test`. Si en el futuro `arch-test` cubre esta regla, la prueba local se borra. No requiere decisión de producto.

---

## Decisiones ya tomadas (contexto, no bloquean)

| Decisión | Qué se decidió | Registro |
| --- | --- | --- |
| **A-1** | Dos realms de Keycloak (clientes e internos) | DEC-066 |
| **A-3** | Login ROPC con usuario + contraseña + OTP | DEC-065 |
| **A-4** | `Config.SessionResolver` inyectable (nil = identidad) | DEC-065 |
| **A-7** | Última versión estable de Keycloak (nunca `latest`) | DEC-066 |
| **A-15** | Nombres de variables `IDENTIDAD_KEYCLOAK_*` | DEC-065 |

---

## Qué se desbloquea con qué

| Tarea | Estado | Decisiones que la frenan |
| --- | --- | --- |
| 10 — Telemetría OTel | ✅ **hecha** | — |
| 12 — Cableado en `cmd/api` | ✅ **hecha** | — |
| 17 — Documentación | ✅ **hecha** | — |
| 14 — Artefacto del realm | ⛔ bloqueada | **A-13, A-16** + aclaración de **A-2** (A-1 ya decidida: dos realms) |
| 15 — Prueba de integración | ⛔ bloqueada | **A-13, A-16** + aceptación de los `t.Skip` por A-3/A-4 (A-7 ya decidida) |

---

## Resumen de pedido

**A Product Management / Arquitectura, para desbloquear el cierre de E4-T1:**

1. **A-2** — confirmar el canal del segundo factor: ¿se acepta correo (extensión + proveedor, reabre DEC-031) o se pasa a TOTP (nativo)?
2. **A-13** — ¿qué opción de identidad del sujeto (a/b/c) y se ratifican los nombres de claims?
3. **A-16** — ¿qué permisos mínimos tiene la cuenta de servicio para revocar?
4. **Proceso** — aceptar formalmente los `t.Skip` de integración como excepción temporal a R11.4.

**Opcional (ratificar o cambiar, no urgente):** A-6, A-8, A-12, A-14.
