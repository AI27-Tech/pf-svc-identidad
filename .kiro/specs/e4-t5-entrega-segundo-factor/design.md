# Design Document

**E4-T5 — Entrega del segundo factor por el IdP (ADR 13, cerrada)** · Documento de diseño

| | |
| --- | --- |
| **Estado** | Borrador — pendiente de validación de PM |
| **Requisitos** | `requirements.md` (misma carpeta) |
| **Fuentes verificadas** | Mismas que `requirements.md`: contexto @ `3797f80`, contrato `v0.1.0-tracer.9`, `go.mod` en `tracer.5` |
| **Fecha** | 2026-10-19 |

## Overview

E4-T5 es una historia **de no construcción**. El diseño parte de un hecho verificado: el segundo factor ya está delegado al IdP por E4-T1, y lo que falta es **evidencia** (pruebas de rechazo por código incorrecto o vencido, a nivel servicio y HTTP) y la **resolución de tres decisiones** que cambiarían código (C-2, C-3, C-5). El código nuevo de la Parte A es, en esencia, pruebas. No se añaden tablas, migraciones, eventos, endpoints ni dependencias.

## El reparto Go ↔ IdP

```
cliente ──POST /api/v1/sesiones {usuario, contrasena, segundo_factor?}──▶ handler (internal/http)
                                                                          │ Credentials{Usuario, Contrasena, SegundoFactor}
                                                                          ▼
                                                        sesion.compensatingFlow.Emit
                                                          1. NewSesionID
                                                          2. steps.Issue ──▶ idp.Issue ──▶ kc.issueForm (&totp=<código>)
                                                                                              │
                                                                                              ▼
                                                                     Keycloak: ai27-direct-grant
                                                                     username → password → OTP (REQUIRED) → nota sesion_id
                                                          3. éxito → ResolveSujeto → Register → WarmCache
                                                             fallo  → mapearErrorIdP (401 / 503), sin Register ni Compensate
```

Quién hace qué:

| Responsabilidad | Dueño | Estado |
| --- | --- | --- |
| Generar, entregar y verificar el código TOTP; política de ventana y reuso | Keycloak (realm) | Hecho (E4-T1) |
| Exigir el factor para los ocho roles | `realm-ai27.json` (`direct-grant-validate-otp` `REQUIRED`) | Hecho (E4-T1) |
| Transportar el código sin interpretarlo | handler → `sesion.Credentials` → `idp.Credentials` → `issueForm` | Hecho (E4-T3 G1, E4-T1) |
| Clasificar el rechazo de factor como credencial inválida (no como IdP caído) | `kc/classify.go` | Hecho (E4-T1 R4.2) |
| Mapear a 401 uniforme / 503 | `sesion/emission.go` `mapearErrorIdP` | Hecho |
| Probar código incorrecto/vencido y ausencia de residuo | **E4-T5** | **Pendiente (Parte A)** |
| Rechazo local de `segundo_factor` vacío | `sesion.EmitirSesion` | Solo si **C-2** lo aprueba (Parte B) |
| IdP en memoria que exija factor | `internal/idp/memory` | Solo si **C-3** lo aprueba (Parte B) |

Consecuencia honesta de alcance: **`identidad` no valida el código nunca**. Si Keycloak aceptara una emisión sin factor, el servicio la entregaría. Por eso la garantía real descansa en la configuración del realm y en la prueba de integración, no en código Go.

## Components and Interfaces

### 1. Sin cambios en `idp.IdentityProvider`, `kc` ni `internal/http`

`Credentials.SegundoFactor` ya existe en `internal/idp/port.go` y en `internal/sesion/port.go`. El handler llena el campo desde `gen.CredencialesEntrada.SegundoFactor` (puntero: nil = ausente). No se cambia ninguna firma.

### 2. Doble de prueba local con factor obligatorio (código nuevo, solo de prueba)

Un doble `IdentityProvider` **local a las pruebas** (`internal/sesion/sesiontest`, bajo la regla E de `archtest`) que:

- Guarda `(usuario, contrasena, codigoVigente)` por sujeto.
- Devuelve `idp.ErrCredencialInvalida` si falta el código, es distinto del vigente, o es el del periodo anterior fuera de tolerancia.
- Devuelve `idp.ErrProveedorNoDisponible` en modo caída.
- Cuenta llamadas a `Issue`, `Revoke`, y expone el código recibido para verificar que se reenvió **sin transformarse**.

**Por qué un doble propio y no `memory.Provider`:** `memory` hoy ignora el factor y cambiarlo es la decisión **C-3**. El doble de prueba permite cubrir el comportamiento del servicio sin resolver C-3 y sin tocar código de producción.

### 3. Prueba de integración [Requiere Keycloak]

Se añade `integration_segundofactor_test.go` en `internal/idp/internal/kc/` (mismo paquete y etiqueta que `integration_realm_test.go`), reutilizando `kc.adaptador(t)`, `contrasenaFactory` y `totpAhora`. Casos:

- **Código incorrecto:** contraseña válida + código válido del sujeto equivocado o cadena de seis dígitos que no coincide con el vigente. Espera `ErrCredencialInvalida`, `Token{}` vacío.
- **Código vencido:** contraseña válida + código calculado con `time.Now().Add(-N·periodo)` con `N` mayor que la tolerancia del realm.
- **Variable de entorno de prueba:** ninguna nueva.

**Nota de verificación:** `realm-ai27.json` **no declara `otpPolicy`**, así que la tolerancia y el reuso de código son los **valores por omisión de Keycloak 26.7.4**. El caso "vencido" no debe asumir un valor: la tarea 3.1 primero lee la política efectiva del realm importado (API de administración con el cliente de prueba) y calcula el desfase a partir de ella. Si la política efectiva no se puede leer, el caso se escribe con un desfase holgado (varios periodos) y se documenta como tal. También aplica `codigoTOTPFresco`, que evita reutilizar un código dentro de la misma ventana.

### 4. Pruebas de servicio y HTTP

- **Servicio:** `EmitirSesion` con el doble del punto 2. Verifica el Requisito 3 (401, sin `Register`, sin `Compensate`, sin fila), el 4 (el valor del código no aparece en el error devuelto) y el 5 (503 sin emisión).
- **HTTP:** `POST /api/v1/sesiones` con `segundo_factor` ausente, `null`, vacío, incorrecto y válido. Verifica códigos de estado y que el cuerpo del 401 es idéntico para contraseña errónea, usuario inexistente y código erróneo (Requisito 3.3).

## Data Models

**Sin tablas, migraciones ni columnas.** `identidad.sesion` no guarda el código ni un indicador de factor. Si PM quisiera auditar el uso del segundo factor, existe una intención local (DEC-087 local, "auditoría de OTP en `cambio_identidad` con `segundo_factor_utilizado`") que **no está en el contexto** y que E4-T6 define; este spec **no la implementa** (ver C-6).

## Error Handling

Sin errores nuevos. Tabla de comportamiento que las pruebas fijan:

| Entrada | Resultado del IdP | Resultado HTTP | Residuo |
| --- | --- | --- | --- |
| Credencial + código válido | Token | 201 | Fila de `sesion` |
| Credencial válida, código ausente / incorrecto / vencido | `ErrCredencialInvalida` | 401 uniforme | Ninguno |
| Contraseña errónea o usuario inexistente | `ErrCredencialInvalida` | 401 uniforme (mismo cuerpo) | Ninguno |
| IdP caído | `ErrProveedorNoDisponible` | 503 | Ninguno |

## Testing Strategy

Convención del repo: `testing/quick`, `MaxCount: 100`, semilla fija registrada; comentario `// Feature: e4-t5-entrega-segundo-factor, Property N`.

- **Property 1 — Sin factor válido, no hay sesión.** Para toda combinación de `(usuario, contraseña válida, código)` donde el código ≠ vigente (incluyendo vacío), `EmitirSesion` devuelve 401, nunca crea fila ni llama a `Register`. Valida Requisitos 2 y 3.
- **Property 2 — El código viaja intacto y solo al IdP.** Para todo código generado, el doble de prueba recibe exactamente el valor enviado y ese valor no aparece en el error devuelto ni en el log capturado. Valida Requisitos 4.1 y 6.1.
- **Ejemplos:** la tabla de comportamiento de arriba, uno por fila.
- **[Requiere Keycloak]** `integration_segundofactor_test.go` (código incorrecto, código vencido). _Sin Docker/JAR solo compila (`go vet -tags integration`); no se presenta como ejecutada._
- **Ya cubiertas, solo se re-ejecutan:** `TestSinFactorRechazado`, `verifyNoOTPArtifacts`, `realm_test.go`, `integration_sesion_test.go`.

### Verificación estándar Go (por tarea de código)

`gofmt -l .` sin salida · `go vet ./...` · `go vet -tags integration ./...` · `go build ./...` · `go test ./...`, en la raíz de `repos/pf-svc-identidad`. Nota heredada de E4-T4: en Windows con `core.autocrlf=true`, `gofmt -l` puede listar archivos por CRLF del working tree y no por formato de código.

## Decisiones pendientes que el diseño respeta

- **C-1 (enrolamiento, DEC-076):** fuera de alcance. Con grant directo, Keycloak rechaza al usuario con acción requerida pendiente (`CONFIGURE_TOTP` es acción por omisión en el realm), así que **hoy un usuario nuevo sin TOTP enrolado no puede iniciar sesión**. Los usuarios de prueba `factory-*` entran porque su credencial OTP se importa ya enrolada. E4-T5 no resuelve cómo se enrola un usuario real.
- **C-2, C-3:** el diseño deja el hueco documentado (Parte B), sin cablear.
- **C-4:** el diseño implementa la respuesta uniforme.
- **C-5:** el diseño no toca el contrato.

## Scope and Deferrals

- **Fuera de alcance:** enrolamiento y recuperación de TOTP (DEC-076); política de contraseñas (DEC-077); fuerza bruta y límite por IP (DEC-081 #2); aceptación formal de ROPC (DEC-075); exposición de `auth.<dominio>` (DEC-080); cambios a `pf-pkg-contracts`; verificación de que `mensajeria` no tiene OTP (repo ajeno); subir `go.mod` a otro tracer.
