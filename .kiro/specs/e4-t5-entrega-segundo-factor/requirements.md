# Requirements Document

**E4-T5 — Entrega del segundo factor por el IdP (ADR 13, cerrada)** · Documento de requisitos

| | |
| --- | --- |
| **Estado** | Borrador — pendiente de validación de PM (ver `DECISIONES-PENDIENTES.md`) |
| **Historia técnica** | E4-T5 — Entrega del segundo factor (`docs/historias-tecnicas/E4-identidad-y-acceso.md`, repo de contexto) |
| **HU funcional** | E4-HU-017 — Autenticación y MFA |
| **Épica** | E4 — Identidad y acceso |
| **Repositorio** | `repos/pf-svc-identidad` (rama `pf-id-d` @ `edf6a84`) |
| **Fuentes verificadas** | Contexto `ai27-platform-context` @ `3797f80` (`main`, al día con `origin/main`). Contrato `pf-pkg-contracts` @ `c5bdeca` = tag `v0.1.0-tracer.9` (último publicado; el checkout local está en la rama `feature/a15-segundo-factor-contrato`, no en `main`, pero su contenido es el del tag). `go.mod` de identidad fija `api-client-go v0.1.0-tracer.5`. Entre `tracer.5` y `tracer.9` el `identidad.yaml` solo cambió la línea de versión (`git diff`), así que lo que identidad compila **es** la superficie vigente de identidad |
| **Fecha** | 2026-10-19 |

## Introduction

E4-T5 no construye un segundo factor: **decide y garantiza que `identidad` no lo construye.** El IdP (Keycloak 26.7.4, provisional por DEC-070/DEC-083 #5) genera, entrega y verifica el código; el canal es **TOTP por app autenticadora** (DEC-071, que resolvió la nota pendiente 1 de DEC-031). Que **todo** usuario, cliente e interno, supera MFA ya estaba decidido (audit Q59/Q60, E4-HU-017).

### Estado real verificado: E4-T5 ya está casi toda implementada dentro de E4-T1

Antes de pedir trabajo nuevo, esto es lo que **ya existe** en `pf-svc-identidad` (leído en el repo, no inferido):

| Garantía de E4-T5 | Dónde vive hoy | Evidencia |
| --- | --- | --- |
| Sin `otp_desafio`, `otp.solicitado` ni `/api/v1/otp*` | `internal/archtest/isolation_test.go` (`verifyNoOTPArtifacts`) | E4-T1 R4.3, tarea 3.2 `[x]` |
| Flujo `ai27-direct-grant` con `direct-grant-validate-otp` en `REQUIRED`, sin subflujos condicionales, para los ocho roles | `deploy/keycloak/realm-ai27.json` | E4-T1 R4.1, tarea 14 `[x]` |
| Usuario sin factor es rechazado con `Token` vacío y **no** como proveedor no disponible (ocho roles) | `integration_realm_test.go` → `TestSinFactorRechazado` | E4-T1 R4.4, tarea 15.3 `[x]` |
| El adaptador reenvía el código a Keycloak como parámetro `totp` y no lo registra en logs | `internal/idp/internal/kc/adapter.go` (`issueForm`) | E4-T1 R4.2/R4.5 |
| Un fallo de OTP se clasifica como credencial inválida y **nunca** como `ErrProveedorNoDisponible` | `internal/idp/internal/kc/classify.go` (fila 9 y caso 500 `unknown_error`) | E4-T1 R4.2 |
| El contrato transporta `segundo_factor` y el handler lo propaga a `idp.Credentials` | `identidad.yaml` (`CredencialesEntrada`), `internal/http/handlers.go`, `internal/sesion/emission.go` | E4-T3 Parte B, G1 `[x]` |
| Login E2E con TOTP real contra Keycloak | `integration_sesion_test.go`, `integration_claims_test.go` (`codigoTOTPFresco`) | E4-T3 G12 `[x]` |

### Qué deja realmente abierto E4-T5 (hallazgos de esta verificación)

| # | Hallazgo | Evidencia | Tratamiento |
| --- | --- | --- | --- |
| H-1 | **No hay prueba de código incorrecto ni vencido.** Solo se prueba el factor **ausente** (`sin-factor-*`) y el válido. E4-HU-017 escenario 3 y CP-017-03 piden ambas variantes | búsqueda en `internal/idp/internal/kc/*_test.go`: sin casos de código erróneo/vencido | Requisito 3 (Parte A) |
| H-2 | **No hay prueba a nivel servicio/HTTP** de que el rechazo por factor no deja residuo (sin fila de `sesion`, sin `Compensate`) y responde 401 uniforme | `segundo_factor` solo aparece en pruebas del adaptador `kc` | Requisitos 3 y 4 (Parte A) |
| H-3 | **El IdP en memoria ignora `SegundoFactor`** y es el valor por omisión cuando `IDENTIDAD_IDP` está vacío (`idpconfig.Cargar`, `cmd/api/main.go`). Con él se emite sesión sin segundo factor | `internal/idp/memory/memory.go` `Issue` solo compara contraseña | **C-3** (decisión de Arquitectura); sin tarea hasta resolverla |
| H-4 | **Contrato: `segundo_factor` es opcional y nullable en `identidad.yaml`** ("Opcional en V1; requerido en V2+") mientras E4-T5 exige "ninguna sesión sin segundo factor, para ningún rol". No es contradicción mientras el IdP sea quien rechaza, pero hoy **solo** el IdP lo garantiza | `identidad.yaml` @ tracer.9; DEC-087 local | **C-2** |
| H-5 | **Brecha contractual en `bff`:** `CredencialesEntrada` de `bff.yaml` @ tracer.9 **no tiene `segundo_factor`**. El login por `bff` no puede completar MFA de extremo a extremo | `bff.yaml` `components.schemas.CredencialesEntrada` (solo `usuario`, `contrasena`) | **C-5**. Se reporta; no se rellena (`contexto-plataforma.md`, regla 6 del scaffold) |
| H-6 | **Textos desactualizados** que contradicen DEC-071: el contrato dice "TOTP, WebAuthn, SMS, etc." y menciona Cognito; E4-HU-017 y E4-T5 siguen listando el canal como pendiente | `identidad.yaml`; `E4-HU-017`; `E4-identidad-y-acceso.md` (E4-T5, "Pendiente" y "Dependencias") | **C-5** y **C-6**; no se editan repos compartidos |

### Cómo leer las etiquetas

| Etiqueta | Significado |
| --- | --- |
| **(T)** | Sale literalmente de E4-T5 ("Qué construir", criterios técnicos o definición de terminado) |
| **(F)** | Se deriva de otra fuente vigente citada (ADR 13, DEC-031, DEC-071, E4-HU-017, E4-T1) y es necesaria para cumplir un punto de E4-T5 |

Las propuestas de Kiro **(K)** no son criterios: viven en `DECISIONES-PENDIENTES.md` como propuesta no aprobada.

### Decisiones que condicionan este spec

| ID | Título | Dueño | Bloquea |
| --- | --- | --- | --- |
| **C-1** | Enrolamiento de TOTP y recuperación de acceso con grant directo (**DEC-076, PENDIENTE en el contexto**) | PM + Seguridad | Que un usuario nuevo pueda llegar a tener factor; **no** bloquea los requisitos de abajo |
| **C-2** | ¿`identidad` rechaza localmente un `segundo_factor` vacío o confía solo en el IdP? | PM + Arquitectura | Requisito 2.3 (opcional), Parte B |
| **C-3** | El IdP en memoria emite sin segundo factor y es el valor por omisión | Arquitectura | Requisito 2.4 (opcional), Parte B |
| **C-4** | ¿El rechazo distingue contraseña inválida de factor inválido? | PM | Requisito 3.3 (se redacta con la opción conservadora de E4-HU-017) |
| **C-5** | Brecha contractual `bff` + descripción obsoleta del contrato | Arquitectura / dueños de contratos | Requisito 6; fuera del repo |
| **C-6** | Colisión de numeración DEC y orden de emisión (DEC-085 vs. resolución local de A-1) | PM / Lead | Registro; no el código |

## Requirements

### Requisito 1 — `identidad` no tiene tramo propio de segundo factor

**Historia de usuario:** Como equipo de desarrollo, quiero que el segundo factor lo resuelva por completo el IdP, para no mantener generación, almacenamiento ni despacho de códigos.

#### Criterios de aceptación

1. `pf-svc-identidad` carece de tabla `otp_desafio`, de evento `otp.solicitado` y de rutas `/api/v1/otp/*` (incluidas `/desafios` y `/verificaciones`). **(T)** _Ya garantizado por `verifyNoOTPArtifacts` (E4-T1 R4.3); E4-T5 lo conserva como invariante y lo verifica._
2. El contrato publicado en `pf-pkg-contracts` (`v0.1.0-tracer.9`) no define ninguna operación ni evento de OTP propio; el único rastro es el campo `segundo_factor` de `CredencialesEntrada`. **(F, contrato)** _Verificado el 2026-10-19 con búsqueda en `contracts/**`._
3. No existe código muerto de un camino alterno de OTP en `identidad`: ningún paquete genera, almacena ni valida códigos; el único consumidor del valor es `issueForm`, que lo reenvía a Keycloak. **(T)**
4. Respecto de `mensajeria` ("ni en `mensajeria`", criterio 3 de E4-T5): **este repo no puede verificarlo.** La verificación vive en `pf-svc-mensajeria` y queda como dependencia de cierre de la historia técnica, no de este spec. **(T)** _Declarado explícitamente para no darlo por verificado._

### Requisito 2 — Ninguna sesión se emite sin segundo factor, para ningún rol

**Historia de usuario:** Como equipo de seguridad, quiero que no exista ruta de emisión que conceda sesión sin un segundo factor válido, para que MFA sea universal (Q59/Q60).

#### Criterios de aceptación

1. El flujo de grant directo del realm exige OTP en `REQUIRED`, sin condición, grupo, atributo ni rol que lo vuelva omitible, para los ocho roles de DEC-053. **(T, F)** _Ya garantizado (E4-T1 R4.1, `realm_test.go`); se verifica, no se reescribe._
2. CUANDO `Issue` recibe una credencial válida sin segundo factor, ENTONCES el IdP rechaza la emisión y el resultado es un `Token` con `AccessToken`, `RefreshToken` y `ExpiraEn` vacíos o en cero, con error distinto de proveedor no disponible, para cada uno de los ocho roles. **(T)** _Ya garantizado (`TestSinFactorRechazado`)._
3. **[Condicionado a C-2]** Si PM elige rechazo local, `EmitirSesion` rechaza un `segundo_factor` vacío antes de llamar al IdP y antes de generar `sesion_id`. Sin esa decisión, el rechazo lo hace solo el IdP. **(K)**
4. **[Condicionado a C-3]** Ninguna implementación del puerto `idp.IdentityProvider` que pueda quedar activa fuera de pruebas emite sesión sin validar el segundo factor. **(F, E4-T5 criterio 2)**

### Requisito 3 — Un segundo factor ausente, incorrecto o vencido no concede acceso

**Historia de usuario:** Como usuario de AI27, quiero que un código de segundo factor inválido me niegue el acceso y me permita reintentar, para que mi cuenta no se abra con solo la contraseña.

#### Criterios de aceptación

1. CUANDO la contraseña es válida y el código es **incorrecto**, ENTONCES `EmitirSesion` devuelve 401 (no 503), `Emitida` vacía, y no queda fila en `identidad.sesion`. **(F, E4-HU-017 escenario 3; CP-017-03)** _Hoy sin prueba (H-1, H-2)._
2. CUANDO la contraseña es válida y el código es **vencido** (fuera de la ventana de tolerancia del realm), ENTONCES el resultado es idéntico al del criterio 1. **(F, E4-HU-017 escenario 3; CP-017-03)** _Hoy sin prueba (H-1)._
3. El rechazo **no revela** cuál elemento falló: el cuerpo de respuesta de contraseña inválida, usuario inexistente, código incorrecto y código vencido es el mismo ("credencial inválida"). **(F, E4-HU-017 escenario 2 y criterio de no revelar existencia de usuario)** _Redactado con la opción conservadora; ver C-4._
4. El rechazo por factor **no** invoca `Compensate`, `Register` ni `ResolveSujeto`: no hay token que compensar. **(F, `emission.go`: `Issue` falla primero)**
5. El usuario puede reintentar según la política de reintentos del proveedor (E4-HU-017). `identidad` no implementa contador de intentos, bloqueo ni espera propios. **(F)** _Los umbrales de fuerza bruta de Keycloak están pendientes en DEC-081 #2; este spec no los fija._

### Requisito 4 — El código de segundo factor no se persiste ni se registra

**Historia de usuario:** Como equipo de seguridad, quiero que el código jamás salga de la petición, para que un log o un error no lo filtre.

#### Criterios de aceptación

1. El valor de `segundo_factor` no aparece en ningún log, traza, mensaje de error, atributo de telemetría ni respuesta HTTP, igual que la contraseña y los tokens. **(F, E4-T1 R4.5 / E0-T3)**
2. `identidad` no persiste el código en ninguna tabla ni en la caché. **(F, E4-T5 "no se construye … almacena")**

### Requisito 5 — Falla del IdP no concede ni degrada MFA

**Historia de usuario:** Como usuario, quiero que si el IdP cae no se me conceda acceso sin verificar mi segundo factor.

#### Criterios de aceptación

1. CUANDO el IdP no está disponible, `EmitirSesion` devuelve 503 y no emite sesión; las sesiones ya emitidas siguen válidas hasta expirar. **(F, E4-HU-017 escenario 4; Q64)**
2. No existe ruta de degradación que omita el segundo factor ante una caída del IdP. **(F)**

### Requisito 6 — El contrato es la única fuente del campo `segundo_factor`

**Historia de usuario:** Como equipo de desarrollo, quiero que el campo salga del contrato y no de una variante local, para que los tipos Go y TypeScript coincidan.

#### Criterios de aceptación

1. `identidad` lee `segundo_factor` desde `gen.CredencialesEntrada` (tipo generado del contrato); no declara un tipo de wire propio. **(F, regla 6 del scaffold)**
2. Las brechas H-4, H-5 y H-6 se **reportan**, no se rellenan: este spec no edita `pf-pkg-contracts`, no escribe a mano un tipo de `bff` y no sube `go.mod` de `tracer.5`. **(F, `contexto-plataforma.md`)** _Subir el tracer es decisión aparte; `identidad.yaml` no cambió entre tracer.5 y tracer.9._

### Requisito 7 — Definición de terminado: login con MFA de extremo a extremo

**Historia de usuario:** Como equipo de desarrollo, quiero una prueba que demuestre el login con MFA contra el IdP real, para cerrar la historia técnica con evidencia.

#### Criterios de aceptación

1. Existe una prueba `-tags integration` contra Keycloak 26.7.4 que emite sesión con credencial + TOTP válido y verifica el `sesion_id` del claim = respuesta = fila. **(T)** _Ya existe (E4-T3 G12)._
2. Existen pruebas `-tags integration` de rechazo con código **incorrecto** y **vencido** (Requisito 3). **(T)** _Nuevas._
3. Existe una verificación automatizada de que no hay rutas ni tablas de OTP propio (Requisito 1.1). **(T)** _Ya existe._
4. Si no hay Docker/JAR de E4-T1 disponibles, las pruebas de integración solo se compilan (`go vet -tags integration`) y **se dice explícitamente que no se ejecutaron**. **(F, convención de E4-T4)**

## Trazabilidad

| Requisito | Fuente |
| --- | --- |
| R1 | E4-T5 "No se construye" y criterio 3; DEC-031 #1; E4-T1 R4.3 |
| R2 | E4-T5 criterio 2; Q59/Q60; E4-HU-017; E4-T1 R4.1/R4.4 |
| R3 | E4-HU-017 escenarios 2 y 3; CP-017-03 (`E4-identidad-casos.md`) |
| R4 | E4-T1 R4.5; E0-T3 |
| R5 | E4-HU-017 escenario 4; Q64 |
| R6 | `contexto-plataforma.md`; DEC-087 y DEC-088 (numeración local, ver C-6) |
| R7 | E4-T5 "Definición de terminado" |
