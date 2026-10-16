# Requirements Document

**E4-T4 — Reevaluación de autorización en el refresco (ADR 16)** · Documento de requisitos

| | |
| --- | --- |
| **Estado** | Borrador — pendiente de validación de PM (ver `DECISIONES-PENDIENTES.md`) |
| **Historia técnica** | E4-T4 — Reevaluación de autorización en refresco (`docs/historias-tecnicas/E4-identidad-y-acceso.md`, repo de contexto) |
| **HU funcional** | E4-HU-020 — Gestión de sesión y revocación |
| **Épica** | E4 — Identidad y acceso |
| **Repositorio** | `repos/pf-svc-identidad` |
| **Fuentes verificadas** | Contexto `ai27-platform-context` @ `bd8901d` (`main`); contrato `pf-pkg-contracts` fijado en `go.mod` a `v0.1.0-tracer.5` (último publicado `v0.1.0-tracer.8`). El `identidad.yaml` entre tracer.5 y tracer.8 cambió 2 líneas; `/api/v1/sesiones/refresh` y `segundo_factor` ya existen en tracer.5 |
| **Fecha** | 2026-10-16 |

## Introduction

E4-T4 cierra la garantía de ADR 16 (cerrada por DEC-031 #2, enmendada 2026-10-02): **la composición de claims corre también en el flujo de refresco, no solo en la emisión.** Al refrescar, el rol, el tenant y la `Audiencia` se recalculan desde el modelo de autorización **vigente**; nunca se copian los claims del token anterior. El `sesion_id` se conserva (ya lo garantiza E4-T3). El efecto de seguridad es acotar la exposición máxima de un cambio de **rol** a la vida de un token (Property P11): un cambio de rol surte efecto en el **siguiente refresco**.

El **alcance** (unidad/cliente) no entra en esta tarea: no viaja en el token y se resuelve en servidor (DEC-053). Un cambio de alcance no espera al refresco. E4-T4 trata solo de los claims que sí viajan: identidad, tenant, rol y `Audiencia`.

### Dónde corre realmente la composición (hecho arquitectónico, no decisión de este spec)

Verificado en el código y en el diseño de E4-T2 (`internal/claims/claims.go`; `e4-t2/design.md` líneas 62, 70, 346):

- En **producción**, la composición de claims es un **artefacto que corre dentro del IdP** (mapper o extensión Java, ADR 32 §4), en las rutas de emisión **y** de refresco. El token que `identidad` (Go) recibe del IdP en `Refresh` **ya trae** los claims recompuestos.
- En el **proceso Go**, `internal/claims.Componer` es la **misma función pura** `(tenant, rol) → (tenant, rol, Audiencia)`, idéntica en emisión y refresco (DEC-031). Vive en Go para poder **probarla y versionarla con el servicio**; la envoltura que la expone al IdP elegido queda EN ESPERA de ADR 32.

Consecuencia para el alcance: **E4-T4 no "añade" un recálculo de claims al flujo Go del refresco.** El recálculo lo hace el artefacto del IdP. Lo que E4-T4 debe garantizar y verificar del lado de `identidad` es: (1) que el artefacto de composición corre en refresco con el modelo vigente y es idéntico a emisión (prueba de la función pura + prueba e2e contra el IdP); (2) la **señal de observabilidad** de latencia de composición, medida por separado en emisión y refresco; (3) que el flujo Go de refresco **no reintroduce** claims del token anterior ni los fija él mismo. Ver `design.md` para el reparto exacto Go ↔ IdP.

### Cómo leer las etiquetas

Cada criterio lleva su fuente y una de dos etiquetas normativas:

| Etiqueta | Significado |
| --- | --- |
| **(T)** | Sale literalmente de E4-T4 ("Qué construir", criterios de aceptación técnicos o definición de terminado) |
| **(F)** | Se deriva directamente de otra fuente vigente citada (ADR 16, DEC-031, DEC-053, DEC-054, E4-HU-020, el código de E4-T2/E4-T3) y es necesaria para cumplir un punto de E4-T4 |

Las propuestas de Kiro **(K)** no son criterios: viven en "Propuestas de Kiro (no aprobadas)".

### Decisiones pendientes que condicionan este spec

E4-T4 arrastra **una decisión de negocio sin resolver** y **dos decisiones técnicas** que necesitan validación de PM/Arquitectura antes de cerrar la tarea. Están desarrolladas en `DECISIONES-PENDIENTES.md`. Cada requisito afectado lo marca con el identificador **B-n**:

| ID | Título | Dueño | Bloquea |
| --- | --- | --- | --- |
| **B-1** | Plazo de la quita de rol: ¿el "siguiente refresco" (DEC-031 #2) satisface el "≤30 s" de E4-HU-020/Q63, o quitar un rol debe además revocar sesiones? | PM + Arquitectura | El criterio de corte de acceso tras quita de rol (Requisito 4) |
| **B-2** | Cómo se lee el claim de latencia / qué backend de métricas usa V1 (la señal de observabilidad de ADR 16) | Arquitectura | La forma concreta de la señal (Requisito 3), no su existencia |
| **B-3** | Qué significa "modelo vigente" en el instante del refresco cuando hay un cambio de rol en curso (consistencia de lectura del artefacto del IdP contra el modelo de `identidad`) | Arquitectura | La garantía fuerte del Requisito 2.3; la garantía débil se puede implementar ya |

## Requirements

### Requisito 1 — El refresco conserva el `sesion_id` y no reautentica

**Historia de usuario:** Como consumidor de un token de AI27, quiero que al refrescar mi sesión se conserve el mismo `sesion_id`, para que la identidad durable de la sesión no cambie entre refrescos.

#### Criterios de aceptación

1. CUANDO un refresco tiene éxito, el `sesion_id` del token nuevo ES EL MISMO que el del token anterior. **(F, E4-T3 / ADR 26 §1)** _Ya implementado en E4-T3 (`internal/sesion/refresh.go`); E4-T4 lo conserva como invariante._
2. CUANDO un refresco tiene éxito, `identidad` NO exige credencial ni segundo factor: el refresco no es un login. **(F, contrato `identidad.yaml`: `/sesiones/refresh` es pública y solo recibe `refresh_token`)**

### Requisito 2 — La composición de claims corre en el refresco desde el modelo vigente

**Historia de usuario:** Como equipo de seguridad, quiero que al refrescar se recalculen rol, tenant y `Audiencia` desde el modelo de autorización vigente, para que un token refrescado nunca arrastre claims obsoletos del token anterior (Property P11).

#### Criterios de aceptación

1. La composición de claims (mapper/extensión del IdP, E4-T2) se ejecuta en la ruta de **refresco**, no solo en la de emisión. **(T)**
2. La composición en refresco produce **exactamente** identidad, tenant, rol y `Audiencia`, sin alcance (DEC-053). La `Audiencia` se deriva del rol (DEC-054) y nunca se acepta como parámetro. **(F, E4-T2 / DEC-053 / DEC-054)**
3. CUANDO el rol de un sujeto cambia en el modelo vigente ENTONCES el token producido por el siguiente refresco trae el rol **nuevo**, no el anterior, conservando el `sesion_id`. **(T)** _La garantía fuerte de "modelo vigente en el instante exacto del refresco" depende de **B-3**._
4. La composición en refresco es **idéntica** a la de emisión para la misma entrada (misma función pura `Componer`): misma entrada → misma salida. **(F, DEC-031 / `internal/claims` / E4-T2 Property 5)**

### Requisito 3 — Señal de observabilidad de la latencia de composición

**Historia de usuario:** Como equipo de operación, quiero medir por separado la latencia de la composición de claims en emisión y en refresco, para vigilar la carga del refresco (que llega a ráfagas) sin confundirla con la de emisión.

#### Criterios de aceptación

1. La latencia de la composición de claims se mide **en emisión** y **en refresco** como dos señales distinguibles. **(T)**
2. La señal permite **comparar** la latencia de refresco contra la de emisión. **(T)**
3. La forma concreta de la señal (nombre, etiquetas, backend de métricas) la fija **B-2**; su existencia y la separación emisión/refresco no dependen de B-2. **(F)**

### Requisito 4 — Efecto de una quita de rol sobre el acceso vivo _(condicionado a B-1)_

**Historia de usuario:** Como equipo de seguridad, quiero saber en cuánto tiempo deja de tener efecto un rol que se le quita a un sujeto, para cumplir el compromiso de desalojo de acceso de E4-HU-020.

#### Criterios de aceptación

> **Conflicto abierto — sin criterio normativo hasta resolver B-1.**
> E4-HU-020 (criterio y escenario 3, audit Q63) exige que **quitar** un rol o una
> membresía corte el acceso afectado en un máximo de **30 s**. DEC-031 #2 dice que un
> cambio de rol surte efecto en el **siguiente refresco** (que puede tardar más que
> la vida del token). Las dos reglas no se pueden cumplir a la vez solo con la
> reevaluación en refresco. Este spec **no elige**: la decisión es de PM/Arquitectura
> (**B-1**). Mientras no se resuelva, no se escribe un criterio de "≤30 s" para la
> quita de rol, y la reevaluación en refresco (Requisito 2) es lo único garantizado.

1. _(En espera de B-1.)_ Si B-1 resuelve que el "siguiente refresco" basta, este requisito se cierra señalando a E4-HU-020 que la expectativa de ≤30 s para **cambio de rol** se acota a la vida del token, no al evento de revocación. **(F)**
2. _(En espera de B-1.)_ Si B-1 resuelve que la quita de rol debe cortar en ≤30 s, entonces quitar un rol debe **además** disparar la revocación de las sesiones vivas del sujeto (mecanismo de E4-T3 / E4-T6), y este spec remite esa parte a E4-T6, no la implementa aquí. **(F)**

### Requisito 5 — El flujo de refresco no fija ni copia claims por su cuenta

**Historia de usuario:** Como mantenedor de `identidad`, quiero que el flujo Go de refresco trate el token del IdP como la fuente de los claims recompuestos, para que la composición tenga un solo dueño (el artefacto del IdP) y no haya una segunda ruta que pueda divergir.

#### Criterios de aceptación

1. El flujo Go de refresco (`internal/sesion/refrescar`) NO copia los claims del token anterior ni los compone él mismo: toma el token que el IdP devolvió en `Refresh`. **(F, código `internal/sesion/refresh.go`)**
2. No existe en `identidad` una segunda ruta de composición de claims en refresco distinta del artefacto de E4-T2. **(T, "no código muerto")**

### Requisito 6 — Verificación y definición de terminado

#### Criterios de aceptación

1. **Prueba de la función pura:** recomponer con un rol `r'` distinto del previo produce el resultado de `r'` sin reutilizar el anterior (metamórfica + determinismo), reusando la Property 5 ya existente de E4-T2. **(F)**
2. **Prueba de observabilidad:** la señal de latencia de composición se emite de forma distinguible en emisión y en refresco (prueba de unidad del punto de instrumentación). **(T)**
3. **[Requiere Keycloak] DoD e2e (compuerta G4 del audit):** contra el IdP elegido, cambiar el rol de un sujeto → refrescar → verificar que el token nuevo trae el rol nuevo y el mismo `sesion_id`. **(T)** _Necesita Docker + Keycloak 26.7.4 + el JAR de E4-T1; sin ellos la prueba `-tags integration` solo compila, no se ejecuta._
4. **Verificación estándar Go** en cada tarea de código: `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...`, todo en verde. **(F, `golang-best-practices.md`)**

## Propuestas de Kiro (no aprobadas)

No son requisitos. Se listan para que PM/Arquitectura las acepte o descarte; mientras tanto no entran a la ruta productiva.

- **(K-1)** Emitir la señal de latencia como histograma con etiqueta `fase=emision|refresco` sobre el stack de métricas que E0 defina. Depende de B-2.
- **(K-2)** Añadir un claim o log de "versión del artefacto de composición" para poder correlacionar un token con la versión del mapper/extensión que lo produjo (trazabilidad de ADR 32 §4). No pedido por E4-T4.
- **(K-3)** Prueba de contrato que afirme que el `identidad.yaml` del tag fijado en `go.mod` mantiene `/sesiones/refresh` sin exigir credencial (candado de regresión del Requisito 1.2).
