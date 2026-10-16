# E4-T4 — Decisiones pendientes

Decisiones que **no puede tomar Kiro** y que condicionan el cierre de E4-T4. Cada una sigue el formato que el repo de contexto exige para registrar una intención: **título, dueño, estado y fecha**, más el impacto y las opciones. Ninguna es un hecho hasta que su dueño la resuelva y se registre como DEC en `ai27-platform-context`.

> **Regla de oro del discovery:** ante duda, preguntar antes de definir. Este documento es la lista de preguntas para PM/Arquitectura, no decisiones ya tomadas.

**Estado de la numeración (verificado contra `ai27-platform-context` @ `138f7e7`, 2026-10-16).** El último DEC registrado en el contexto es **DEC-090**. Los números DEC-088/089/090 **ya están ocupados** por decisiones ajenas (demo local, partición de la ola 2 de contratos, superficie SSE de realtime). Los siguientes libres son **DEC-091, DEC-092, DEC-093**, que es lo que usan B-1/B-2/B-3 abajo.

> **Colisión de número señalada (deuda de E4-T3, no de E4-T4).** Las specs de E4-T3 en este repo citan "DEC-088" como "resuelve A-1…A-16". Esas decisiones A-n **sí** se tomaron y el código las implementa, pero el **número 088 está mal**: en el contexto DEC-088 es la decisión de la demo. Es una discrepancia de trazabilidad que E4-T3 debe reconciliar al registrar sus decisiones con números libres; aquí solo se registra para no propagarla.

---

## B-1 — Plazo de la quita de rol: ¿basta el "siguiente refresco" o hay que revocar sesiones?

| | |
| --- | --- |
| **Dueño** | Product Management + Arquitectura de seguridad |
| **Estado** | **RESUELTA — opción a** (decidida 2026-10-16; pendiente de registro como DEC en contexto) |
| **Fecha** | 2026-10-16 |
| **DEC propuesto** | DEC-091 (siguiente libre tras DEC-090; confirmar al registrar) |
| **Bloquea** | Requisito 4 de E4-T4; cierre de la cobertura de E4-HU-020 en identidad |

> **Resolución (opción a).** El "≤30 s" aplica a la **revocación de sesión** (ADR 26), no al **cambio de rol**, que se acota a la vida del token vía el siguiente refresco (DEC-031 #2). La reevaluación en refresco de E4-T4 **es** el comportamiento correcto; no se dispara revocación por quita de rol y **no hay dependencia hacia E4-T6**. E4-HU-020 debe reconciliar su redacción del "conflicto abierto" cuando esto se registre.

### El conflicto

Dos fuentes vigentes se contradicen. **E4-HU-020** (fuente de verdad, verificada @ `bd8901d`) lo marca explícitamente como "conflicto abierto" en su criterio, en su escenario 3 y en su sección de Pendientes: *"Se conserva el comportamiento de esta HU hasta que se decida si una quita de rol revoca además las sesiones."*

- **E4-HU-020** (criterio y escenario 3; audit Q63): quitar un rol **o una membresía de pool** debe **terminar el acceso afectado en un máximo de 30 s**.
- **DEC-031 #2** (ADR 16, enmendada 2026-10-02): un cambio de **rol** surte efecto en el **siguiente refresco**.

La reevaluación en refresco (lo que E4-T4 construye) hace que el rol nuevo entre al refrescar. Pero la vida de un token puede ser mayor a 30 s, así que un token ya emitido con el rol viejo sigue siendo válido hasta que caduque o se refresque. Las dos reglas no se cumplen a la vez **solo** con reevaluar en refresco.

### Opciones

- **Opción a — El "siguiente refresco" basta.** Se acepta que un **cambio de rol** se acota a la vida del token, no al evento. El "≤30 s" de E4-HU-020 se reinterpreta como aplicable a la **revocación de sesión** (que E4-T3 sí garantiza en 30 s), no a la quita de rol. E4-T4 se cierra con la reevaluación en refresco y una nota que reconcilia la HU. **No añade trabajo.**
- **Opción b — La quita de rol debe cortar en ≤30 s.** Entonces quitar un rol debe **además** disparar la revocación de las sesiones vivas del sujeto, usando el mecanismo de dos pasos de E4-T3. Eso es alcance de **E4-T6** (gobierno: la operación que quita el rol es la que dispararía la revocación), no de E4-T4. E4-T4 quedaría dependiente de que E4-T6 implemente ese disparo.
- **Opción c — Mixta por criticidad del rol.** Algunos roles (p. ej. `administrador`, `reaccion`) cortan en ≤30 s vía revocación; el resto espera al refresco. Añade una regla de negocio nueva (qué roles son críticos) que hoy no existe.

### Qué necesita Kiro para avanzar

Con cuál opción cerrar el Requisito 4. Con la **a**, E4-T4 cierra sin trabajo extra. Con la **b** o la **c**, E4-T4 documenta la dependencia hacia E4-T6 y no implementa la revocación aquí.

---

## B-2 — Forma de la señal de latencia de composición (observabilidad de ADR 16)

| | |
| --- | --- |
| **Dueño** | Arquitectura (plataforma / observabilidad) |
| **Estado** | **RESUELTA — opción a** (decidida 2026-10-16; pendiente de registro como DEC en contexto) |
| **Fecha** | 2026-10-16 |
| **DEC propuesto** | DEC-092 (siguiente libre; confirmar al registrar) |
| **Bloquea** | La **forma** concreta de la señal del Requisito 3 (no su existencia) |

> **Resolución (opción a).** Medir en el adaptador Go el tiempo de `idp.Issue`/`idp.Refresh` con etiqueta de fase, detrás de la costura `LatencyObserver`. **Ya implementado** en la Parte A (default no-op + puntos de medición). Si V1 decide un backend de métricas concreto, se sustituye la implementación de la costura sin tocar los puntos de llamada.

### El punto

E4-T4 pide "medir la latencia de la composición de claims, por separado en emisión y refresco". Que la señal **exista** y **distinga** emisión de refresco no depende de nadie: se puede implementar ya. Lo que falta decidir es **cómo se expone**: nombre de métrica, etiquetas, y sobre qué backend (el stack de observabilidad que E0 defina para V1).

Hay además una sutileza arquitectónica: la composición real corre **dentro del IdP** (mapper/extensión, ADR 32 §4), no en el proceso Go. La latencia "de la composición" vista desde `identidad` es la del `Issue`/`Refresh` del IdP, que incluye más que la composición. Hay que decidir si la señal mide (i) el `Refresh` completo del adaptador del IdP, (ii) un tramo que la extensión del IdP exponga, o (iii) ambas con etiquetas distintas.

### Opciones

- **Opción a — Medir en el adaptador Go** el tiempo de `idp.Issue` y `idp.Refresh` con etiqueta `fase=emision|refresco`. Simple, no depende del IdP, pero mide "IdP completo", no solo composición.
- **Opción b — Exponer un tramo desde la extensión del IdP** (requiere trabajo en el JAR de E4-T1) y recogerlo. Más fiel, más caro, acopla a E4-T1.
- **Opción c — Ambas**, con etiquetas que las separen.

### Qué necesita Kiro para avanzar

El backend de métricas de V1 y cuál de a/b/c. Mientras tanto, Kiro implementa la **opción a como valor por omisión** (medición en el adaptador Go, distinguible por fase), detrás de una costura, para no bloquear; si B-2 elige otra, se sustituye la costura.

---

## B-3 — Qué significa "modelo vigente" en el instante del refresco

| | |
| --- | --- |
| **Dueño** | Arquitectura (seguridad / consistencia) |
| **Estado** | **RESUELTA — opción a** (decidida 2026-10-16; pendiente de registro como DEC en contexto) |
| **Fecha** | 2026-10-16 |
| **DEC propuesto** | DEC-093 (siguiente libre; confirmar al registrar) |
| **Bloquea** | La **garantía fuerte** del Requisito 2.3 (la débil se puede implementar ya) |

> **Resolución (opción a).** Consistencia eventual: el refresco trae el rol que el IdP tenga tras converger. La prueba e2e G4 se escribe para "tras el cambio comprometido". La garantía fuerte queda como mejora futura; **no cambia E4-T2**.

### El punto

El Requisito 2.3 dice: un cambio de rol se refleja en el siguiente refresco. La composición corre en el IdP y lee el modelo de autorización. Pero el modelo "dueño" del rol vive en **dos sitios** que deben estar sincronizados: el **grupo de rol en el IdP** (Keycloak) y la tabla `asignacion_rol` de `identidad` (E4-T2). Un cambio de rol (E4-T6) escribe en ambos "de forma consistente", pero no instantánea.

La pregunta: si un refresco llega **mientras** un cambio de rol está a medio comprometer, ¿qué rol debe traer el token? ¿El del IdP (lo que el mapper ve) o el de `identidad`? ¿Se exige lectura consistente, o se acepta una ventana de inconsistencia acotada entre ambos?

### Opciones

- **Opción a — Garantía débil (eventual).** El refresco trae lo que el IdP tenga en ese instante; la sincronización IdP ↔ `identidad` converge poco después. Simple, es lo que el diseño actual permite. La prueba e2e se escribe para "tras converger".
- **Opción b — Garantía fuerte (lectura consistente).** El artefacto de composición lee el rol desde una sola fuente autoritativa en el momento del refresco (p. ej. siempre `identidad`, no el grupo del IdP), evitando la ventana. Más caro; puede requerir que el mapper consulte a `identidad` en línea.

### Qué necesita Kiro para avanzar

Si la garantía débil (opción a) es aceptable para V1. Si lo es, E4-T4 implementa y prueba la débil y cierra; la fuerte quedaría como mejora futura. Si se exige la fuerte, cambia el diseño del artefacto de composición (E4-T2) y E4-T4 depende de ese cambio.

---

## Resumen para PM — las tres RESUELTAS con opción a (2026-10-16)

| ID | Pregunta | Decisión | Efecto en E4-T4 |
| --- | --- | --- | --- |
| **B-1** | ¿Quitar un rol corta en ≤30 s o basta el siguiente refresco? | **a** — basta el siguiente refresco | Sin trabajo extra; sin dependencia a E4-T6 |
| **B-2** | ¿Cómo se expone la señal de latencia? | **a** — adaptador Go por fase, tras costura | Ya implementado en Parte A |
| **B-3** | ¿Basta consistencia eventual del rol? | **a** — eventual | Ya cubierto; G4 prueba "tras converger" |

Las tres están **decididas** pero **pendientes de registro** como DEC en `ai27-platform-context` (números libres: DEC-091/092/093, a confirmar al registrar). Con las tres en opción a, **la Parte A ya cubre todo lo implementable en Go**; para cerrar E4-T4 solo falta la prueba e2e G4 contra Keycloak (infra) y el registro de las decisiones.
