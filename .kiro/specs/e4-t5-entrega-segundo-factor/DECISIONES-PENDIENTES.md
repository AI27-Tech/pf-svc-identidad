# E4-T5 — Decisiones pendientes

Decisiones que **no puede tomar Kiro** y que condicionan el cierre de E4-T5. Cada una sigue el formato que el repo de contexto exige: **título, dueño, estado y fecha**, más impacto y opciones. Ninguna es un hecho hasta que su dueño la resuelva y se registre como DEC en `ai27-platform-context`.

> **Regla de oro del discovery:** ante duda, preguntar antes de definir. Este documento es la lista de preguntas, no decisiones tomadas.

**Numeración (verificada contra `ai27-platform-context` @ `3797f80`, 2026-10-19).** El índice de la tabla está al día con DEC-091, DEC-092 y DEC-093. El siguiente libre es **DEC-094**. Cualquier DEC propuesto abajo usa ese número; confirmar al registrar.

---

## Decisiones ya tomadas que este spec respeta (no se reabren)

| Decisión | Qué fija para E4-T5 | Dónde |
| --- | --- | --- |
| DEC-031 #1 (ADR 13) | El IdP entrega el segundo factor; sin `otp_desafio`, `otp.solicitado` ni despacho en `mensajeria` | contexto, DECIDIDA |
| **DEC-071** | El canal de V1 es **TOTP** por app autenticadora; correo y WebAuthn descartados. **Resuelve la nota pendiente 1 de DEC-031** | contexto, DECIDIDA |
| DEC-070 / DEC-083 #5 | Keycloak 26.7.4 exacta, provisional | contexto, DECIDIDA |
| Q59/Q60 | MFA para **todo** usuario, cliente e interno | audit, E4-HU-017 |

---

## C-1 — Enrolamiento de TOTP y recuperación de acceso

| | |
| --- | --- |
| **Dueño** | Product Management + Seguridad |
| **Estado** | **PENDIENTE** — es **DEC-076**, ya registrada como PENDIENTE en el contexto. No es una decisión nueva de este spec |
| **Fecha** | 2026-10-09 (registro en contexto) |
| **Bloquea** | Que un usuario real nuevo llegue a tener factor. **No** bloquea los requisitos de E4-T5 |

Con grant directo, Keycloak rechaza al usuario con acción requerida pendiente. El realm marca `CONFIGURE_TOTP` como acción por omisión, el flujo de navegador está deshabilitado y el restablecimiento de contraseña también. Resultado: **hoy un usuario nuevo sin TOTP enrolado no puede iniciar sesión**, y no existe camino para quien pierde su dispositivo. Los usuarios `factory-*` de prueba funcionan porque su credencial OTP se importa ya enrolada.

Este spec **no resuelve ni asume** una opción. Se señala porque E4-T5 "cerrado" no equivale a "MFA usable en producción".

---

## C-2 — ¿`identidad` rechaza localmente un `segundo_factor` vacío?

| | |
| --- | --- |
| **Dueño** | Product Management + Arquitectura |
| **Estado** | **RESUELTA** — **DEC-094** (DECIDIDA, opción **b**, 2026-10-19) en `ai27-platform-context`. Implementada en este repo (Parte B, grupo C-2) |
| **Fecha** | 2026-10-19 |
| **DEC** | DEC-094 |
| **Desbloqueó** | Requisito 2.3; Parte B, grupo C-2 |

> **Resolución (DEC-094, opción b):** `EmitirSesion` rechaza un `segundo_factor` vacío (o solo espacios) con 401 uniforme indistinguible del de credencial inválida, antes de llamar al IdP. Campo obligatorio de facto en V1; el contrato no cambia. Implementado en `compensatingFlow.Emit` (`internal/sesion/emission.go`) con pruebas de servicio y HTTP.

### El hecho

El contrato (`identidad.yaml` @ tracer.9) declara `segundo_factor` como **opcional y nullable**, con la nota "opcional en V1; requerido en V2+". La razón registrada localmente (DEC-087 del `decision-log` del workspace) es evitar desincronización entre servicios durante la adopción. E4-T5 exige "ninguna sesión se emite sin segundo factor, para ningún rol".

Hoy la garantía la da **solo Keycloak**: `identidad` omite el parámetro `totp` si el código viene vacío y Keycloak rechaza. No es una contradicción mientras el IdP sea el garante, pero `identidad` no tiene defensa propia.

### Opciones

- **Opción a — Solo el IdP (comportamiento de hoy).** Sin cambios. Un `segundo_factor` vacío viaja a Keycloak, que rechaza con respuesta indistinguible de contraseña errónea. Costo: una llamada de red por intento, y la garantía depende de que el realm no se reconfigure.
- **Opción b — Rechazo local.** `EmitirSesion` devuelve 401 uniforme ante código vacío, sin llamar al IdP. Defensa en profundidad y menos carga. Costo: el campo pasa a ser **obligatorio en la práctica** aunque el contrato lo siga declarando opcional, lo que se aleja de la intención de DEC-087 local de tolerar clientes que aún no lo envían. Esa tolerancia podría ya no hacer falta si el contrato nunca permitió emitir sin factor contra el realm vigente.

### Qué necesita Kiro

Con cuál opción cerrar el Requisito 2.3. Con la **a** no hay trabajo. Con la **b** se habilita el grupo C-2 de la Parte B.

> **Propuesta de Kiro (K), no aprobada:** opción b, por defensa en profundidad y porque no hay hoy ningún flujo válido que emita sin factor.

---

## C-3 — El IdP en memoria emite sin segundo factor y es el valor por omisión

| | |
| --- | --- |
| **Dueño** | Arquitectura |
| **Estado** | **RESUELTA** — **DEC-095** (DECIDIDA, **b+d**, 2026-10-19) en `ai27-platform-context`. Implementada en este repo (Parte B, grupo C-3) |
| **Fecha** | 2026-10-19 |
| **DEC** | DEC-095 |
| **Desbloqueó** | Requisito 2.4; Parte B, grupo C-3 |

> **Resolución (DEC-095, b+d):** el doble `memory` exige factor (código esperado en `memory.Sujeto`, validado en `Issue`) y avisa al arrancar con log de nivel alto (`construirIDP`, `cmd/api/main.go`); la demo del 11-nov corre contra Keycloak real. Implementado con la suite de contrato `idptest` ampliada para cubrir el factor en ambas implementaciones.

### El hecho

`idpconfig.Cargar` selecciona el doble en memoria cuando `IDENTIDAD_IDP` está **ausente o vacío**. `memory.Provider.Issue` solo compara contraseña e ignora `SegundoFactor`. Por tanto, **un despliegue sin esa variable emite sesiones sin segundo factor**, y falla en abierto por omisión. El paquete se declara "SOLO para pruebas y desarrollo"; nada impide arrancarlo en otro entorno. Además, DEC-088 del contexto fija que la demo del 11-nov corre en local, y no está definido con qué IdP.

### Opciones

- **a — Aceptar.** Documentar que `memory` nunca es un entorno de seguridad. Costo: la garantía universal de MFA tiene una excepción por configuración.
- **b — El doble exige factor.** `memory.Sujeto` gana un código esperado (configurable). Mantiene la suite de contrato `idptest` coherente con el adaptador real.
- **c — Sin valor por omisión.** Si `IDENTIDAD_IDP` falta, el arranque falla. Cambia el comportamiento de desarrollo standalone.
- **d — Aviso de arranque.** Log de nivel alto cuando se usa `memory`.

Las opciones b a d no son excluyentes.

### Qué necesita Kiro

Qué opción(es) aplicar, y si la demo local usará `memory` o Keycloak.

---

## C-4 — ¿El rechazo distingue contraseña inválida de factor inválido?

| | |
| --- | --- |
| **Dueño** | Product Management |
| **Estado** | **PENDIENTE** (el spec adopta la opción conservadora en lo que sigue) |
| **Fecha** | 2026-10-19 |
| **Bloquea** | Requisito 3.3 |

### El hecho

E4-HU-017 escenario 2 exige no revelar si el usuario existe; escenario 3 pide "permitir reintentar". Hoy el adaptador mapea todo `invalid_grant` a `ErrCredencialInvalida` y la fila 9 de `classify.go` deja la distinción como "Pendiente A-3/A-14". La HU describe un flujo escalonado ("tras validar la contraseña, exige un segundo factor"), pero DEC-071 elige grant directo: **un solo envío** con usuario, contraseña y código. Un cliente no puede saber si debe pedir el código en una segunda pantalla.

### Opciones

- **a — Respuesta uniforme (hoy).** Máxima protección de enumeración. El frontend debe mostrar siempre el campo de código.
- **b — Distinguir "falta o es inválido el segundo factor".** Mejor UX; revela que la contraseña fue aceptada, lo que filtra información a quien intenta forzar credenciales.

> **Propuesta de Kiro (K), no aprobada:** opción a, coherente con E4-HU-017. El texto exacto del mensaje sigue siendo una propuesta pendiente de validación en la HU.

---

## C-5 — Brecha contractual en `bff` y textos desactualizados

| | |
| --- | --- |
| **Dueño** | Arquitectura / dueños de `pf-pkg-contracts` |
| **Estado** | **PENDIENTE** — fuera de este repo |
| **Fecha** | 2026-10-19 |

1. **`bff.yaml` @ tracer.9: `CredencialesEntrada` solo tiene `usuario` y `contrasena`.** Sin `segundo_factor`, un login a través de `bff` no puede completar MFA. Ya lo anticipa la nota de la resolución local de A-15 ("el contrato `bff.yaml` todavía NO transporta `segundo_factor` — brecha a cerrar"); sigue abierta en el último tag.
2. **Descripción obsoleta en `identidad.yaml`:** dice "TOTP de 6 dígitos, WebAuthn, SMS, etc." y el encabezado menciona Cognito y `bearerCognito`. DEC-071 fija TOTP y descarta WebAuthn y correo.
3. **Rama del checkout:** el repo de contratos local está en `feature/a15-segundo-factor-contrato` con contenido igual al tag `tracer.9`; no está en la rama de promoción. No cambia lo leído, pero conviene saberlo antes de usar `go.work`.

Por regla, esto se **reporta, no se rellena**. Este spec no edita `pf-pkg-contracts`.

---

## C-6 — Numeración DEC y orden de emisión

| | |
| --- | --- |
| **Dueño** | PM / Lead |
| **Estado** | **PENDIENTE** (ya señalada como R-8 en E4-T6 y como "colisión" en E4-T4) |
| **Fecha** | 2026-10-19 |

1. **Mismo número, dos significados.** En el `decision-log` del workspace, DEC-087 es "Segundo factor en contrato de emisión (A-15)" y DEC-088 es la resolución de las decisiones de E4-T3. En el contexto, DEC-087 es "Dueño de `reportes`" y DEC-088 es "la demo corre en local". **La decisión sobre `segundo_factor` en el contrato no está registrada en el contexto.** Mientras no se registre, se trata como "decidida, pendiente de registro como DEC", y este spec la cita como "DEC-087 local".
2. **Contradicción de fondo con DEC-085 (contexto, DECIDIDA, 2026-10-14).** DEC-085 fija la **opción 2**: fila `sesion` provisional **antes** de llamar al IdP. La resolución local de A-1 y `emission.go` implementan la **opción 1**: emitir primero y compensar. No elijo una en silencio. Efecto sobre E4-T5: con la opción 1 (hoy), un intento rechazado por factor **no deja residuo**; con la opción 2, cada intento fallido dejaría una fila provisional que luego barre un proceso. Los Requisitos 3.1 y 3.4 están redactados para la opción 1 y habría que revisarlos si se adopta la 2.
3. **Auditoría de uso del segundo factor.** La resolución local prevé registrar `sesion.emitida` con `segundo_factor_utilizado` en `cambio_identidad`. No está en el contexto y toca E4-T6. Este spec **no la implementa**.

---

## Dependencias abiertas del contexto que este spec no decide

| DEC | Tema | Estado en contexto | Relación con E4-T5 |
| --- | --- | --- | --- |
| DEC-075 | Aceptación formal de ROPC como riesgo | PENDIENTE | DEC-071 depende de ROPC |
| DEC-076 | Enrolamiento y recuperación de TOTP | PENDIENTE | C-1 |
| DEC-077 | Política de contraseñas | PENDIENTE | Sin efecto en el código de E4-T5 |
| DEC-081 #2 | Fuerza bruta, TOTP fallido en grant directo | PENDIENTE | Reintentos del Requisito 3.5; confirmar que un código TOTP fallido cuenta como fallo |
| DEC-080 | Un realm o dos; exposición de `auth.<dominio>` | PENDIENTE | Superficie de ataque del endpoint que recibe el código |

## Discrepancias de documentación en el repo de contexto (no se editan desde aquí)

- `E4-HU-017` y `E4-identidad-y-acceso.md` (E4-T5) siguen listando el **canal** del segundo factor como pendiente, aunque DEC-071 lo resolvió el 2026-10-09. La nota 1 de DEC-031 sí está actualizada.
- `E4-HU-017` y el caso `CP-017-03` dicen "la forma del campo depende del canal"; con TOTP el campo es un código de 6 dígitos.
