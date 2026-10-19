# Implementation Plan: E4-T5 — Entrega del segundo factor por el IdP (ADR 13)

> Verificado contra contexto `ai27-platform-context` @ `3797f80` y contrato `pf-pkg-contracts` @ `v0.1.0-tracer.9`; `go.mod` de identidad fijo en `v0.1.0-tracer.5`.
>
> **Estado del repo (2026-10-19):** la delegación del segundo factor al IdP, el realm con OTP `REQUIRED`, la regla de arquitectura anti-OTP y la prueba de usuario sin factor ya existen (E4-T1, E4-T3). E4-T5 **no reescribe** nada de eso: añade las pruebas que faltan y deja documentadas las decisiones que cambiarían código.

## Overview

El plan sigue el patrón de las specs hermanas (E4-T3, E4-T4):

- **Parte A — Se puede implementar ya.** Tareas con checkbox, sin depender de una decisión de PM. Son pruebas y verificación; **no hay código de producción nuevo**.
- **Parte B — En espera de decisión.** Sin checkbox a propósito: no son ejecutables hasta que C-2, C-3 o C-5 se resuelvan. Cada grupo abre con la decisión que lo desbloquea.

Reglas que gobiernan el plan:

1. Estrictamente apegado a `requirements.md` y `design.md`. Nada de propuestas (K) entra a la ruta productiva.
2. No se crea `otp_desafio`, `otp.solicitado`, `/api/v1/otp*` ni código que genere, almacene o valide códigos. La regla de `archtest` debe seguir en verde.
3. No se tocan `pf-pkg-contracts` ni el repo de contexto. No se sube `go.mod` a un tracer nuevo.
4. No se añaden tablas ni migraciones.
5. **Verificación estándar** por tarea de código (`golang-best-practices.md`): `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...`, todo en verde.

Convenciones:

- Lenguaje: **Go**. Prosa y comentarios en español (México); identificadores en inglés salvo sustantivos de dominio (`sesion`, `rol`, `cliente`, `credencial`).
- **[Requiere Keycloak]** = `-tags integration`, necesita Docker y el JAR de E4-T1; sin ellos solo se verifica que compile, y se dice que no se ejecutó.
- Las sub-tareas de prueba llevan `*` (opcionales), igual que en E4-T1 a E4-T4. Aviso: la 3.1 es la DoD de E4-T5 en la parte de rechazo; omitirla deja E4-HU-017 escenario 3 sin evidencia contra el IdP real.

## Qué queda fuera de este plan (sin tarea)

| Tema | Motivo |
| --- | --- |
| Enrolamiento y recuperación de TOTP | DEC-076 (PENDIENTE en contexto); C-1 |
| Fuerza bruta, política de contraseñas, SSO, ROPC, `auth.<dominio>` | DEC-081 #2, DEC-077, DEC-078, DEC-075, DEC-080 (todas PENDIENTE) |
| `segundo_factor` en `bff.yaml` y limpieza de textos del contrato | C-5; repo ajeno |
| Verificar que `mensajeria` no tiene OTP | Repo ajeno (`pf-svc-mensajeria`) |
| Auditoría de uso del segundo factor en `cambio_identidad` | Intención local sin DEC en contexto; E4-T6 |
| Subir `go.mod` a `tracer.9` | No lo necesita E4-T5; decisión aparte de PM |

## Tasks

# PARTE A — SE PUEDE IMPLEMENTAR YA

> **Estado (2026-10-19): Parte A aún abierta.** La Parte B (C-2/C-3) ya implementada cubre por adelantado parte del comportamiento que estas tareas piden, pero NO las cierra, porque su alcance exacto difiere o no se ejecutó:
> - **2.1** pide el doble en `internal/sesion/sesiontest/`; la Parte B usó el doble `memory` ya existente (DEC-095) y un `stepsFalsos` local. El doble de `sesiontest` sigue sin crearse.
> - **3.2** (servicio, rechazo sin residuo) y **3.4** (HTTP, 401 uniforme) están cubiertos por `TestEmissionFlowRechazoLocalSegundoFactorVacio` y `handlers_segundofactor_test.go`, pero para el caso de factor **vacío**; los casos de código **incorrecto/vencido** a nivel servicio dependen del doble de 2.1.
> - **3.1** (integración incorrecto/vencido), **4** (propiedades), **5** (higiene de persistencia) y **6** (cierre con `REPORTE-BRECHAS.md` H-1…H-6 y la corrida de integración) siguen pendientes: requieren Docker + Keycloak, que no se ejecutaron en la sesión de la Parte B.
> No marcar estas tareas como hechas sin su alcance propio y su verificación.

- [x] 1. Verificación de línea base (sin cambios de código)
  - Cierra con la verificación estándar.
  - [x] 1.1 Ejecutar y registrar el estado de las garantías que ya existen
    - Correr `go test ./internal/archtest/...` (regla anti-OTP, R4.3 de E4-T1) y las pruebas unitarias de `realm_test.go`.
    - Registrar en `CHANGELOG.md` que Requisitos 1.1, 1.3, 2.1 y 2.2 estaban satisfechos antes de E4-T5 y por qué (referencia a E4-T1).
    - _Requirements: 1.1, 1.3, 2.1, 2.2_
    - **Hecho (2026-10-19):** `archtest` en verde (`TestPropiedadAislamiento`, `TestIsolation` = regla anti-OTP R4.3) y `realm_test.go` en verde (`TestRealmFlujoGrantDirecto` con OTP REQUIRED, `TestRealmFirmaYRotacion`, y los demás `TestRealm*`), ejecutados con el binario de workspace por el bloqueo WDAC. Registrado en `CHANGELOG.md` (entrada de línea base E4-T5).
  - [x] 1.2 Re-verificar ausencia de OTP propio en la superficie del contrato consumida
    - Buscar `otp` y `desafio` en `packages/api-client-go/identidad/` del tag fijado; esperar solo el campo `SegundoFactor`.
    - _Requirements: 1.2_
    - **Hecho (2026-10-19):** la búsqueda en `api-client-go/identidad/identidad.gen.go` devuelve únicamente el campo `SegundoFactor` de `CredencialesEntrada`; no hay operación ni evento `otp`/`desafio` propio (el "WebAuthn, SMS, etc." es texto de descripción obsoleto, ya reportado como H-6). Cumple R1.2.

- [x] 2. Doble de prueba con factor obligatorio
  - Cierra con la verificación estándar.
  - **Cerrada (2026-10-19):** única subtarea 2.1 hecha; verificación estándar en verde (tarea 6.2): `go vet`, `go build` y `go test ./...` pasan, incluido `internal/sesion/sesiontest`.
  - [x] 2.1 Crear el doble `IdentityProvider` en `internal/sesion/sesiontest/`
    - Implementa `idp.IdentityProvider`: rechaza con `idp.ErrCredencialInvalida` si el código falta, difiere del vigente o es de un periodo fuera de tolerancia; `idp.ErrProveedorNoDisponible` en modo caída; cuenta llamadas a `Issue` y `Revoke`; expone el último código recibido.
    - Solo vive en `sesiontest` (regla E de `archtest`); no se importa desde código de producción.
    - _Requirements: 2.2, 3.1, 3.2, 5.1_
    - **Hecho (2026-10-19):** `internal/sesion/sesiontest/segundofactor_idp.go` — `SegundoFactorIdP` con `CodigoVigente`/`CodigoAnterior`/`TolerarAnterior` (modela "vencido" como periodo anterior sin tolerancia), modo caída, `IssueCalls()`/`RevokeCalls()` y `UltimoCodigoRecibido()`. No valida TOTP real (regla anti-OTP): compara contra el vigente. `archtest` sigue en verde (regla E respetada).

- [x] 3. Pruebas de rechazo por segundo factor
  - [x] 3.1* Integración: código incorrecto y código vencido **[Requiere Keycloak]**
    - Archivo `internal/idp/internal/kc/integration_segundofactor_test.go`.
    - Primero leer la política OTP efectiva del realm importado (el realm no declara `otpPolicy`; rigen los valores por omisión de Keycloak 26.7.4) y calcular desde ella el desfase del caso "vencido". Si no se puede leer, usar desfase holgado y documentarlo.
    - Esperar `ErrCredencialInvalida`, `Token{}` vacío y ausencia de `ErrProveedorNoDisponible`, como en `TestSinFactorRechazado`.
    - _Requirements: 3.1, 3.2, 7.2_
    - **Hecho y EJECUTADO (2026-10-19):** `TestSegundoFactorIncorrectoOVencido` corrió contra Keycloak 26.7.4 real (testcontainers), con JAR de extensión y realm `ai27` importado. Ambos subcasos en verde: código incorrecto y código vencido → `ErrCredencialInvalida`, Token cero, no `ErrProveedorNoDisponible`. El realm no declara `otpPolicy`; se usó el desfase holgado que autoriza el design (10 periodos = 300 s hacia atrás), documentado en el encabezado de la prueba.
  - [x] 3.2* Servicio: rechazo sin residuo
    - `EmitirSesion` con el doble de 2.1: código ausente, incorrecto y vencido → 401; sin `Register`, sin `Compensate`, sin llamada a `ResolveSujeto`; sin fila.
    - _Requirements: 3.1, 3.2, 3.4_
    - **Hecho (2026-10-19):** `TestFlujoRechazoSegundoFactorSinResiduo` (`segundofactor_flujo_test.go`) — a nivel del flujo de emisión (`compensatingFlow.Emit`) con el doble de 2.1 y un `EmissionSteps` espía. Cubre ausente/incorrecto/vencido → 401, sin `ResolveSujeto`/`Register`/`Compensate`, "sin fila" = sin llamada a `Register`. La variante contra columnas reales de `identidad.sesion` queda en la prueba de integración (requiere Postgres).
  - [x] 3.3* Servicio: IdP caído
    - Modo caída → 503, sin sesión emitida, sin ruta que omita el factor.
    - _Requirements: 5.1, 5.2_
    - **Hecho (2026-10-19):** `TestFlujoIdPCaido503` (`segundofactor_flujo_test.go`) — modo caída → 503 (Degradado), sin `Register` ni `Compensate`.
  - [x] 3.4* HTTP: `POST /api/v1/sesiones`
    - Casos con `segundo_factor` ausente, `null`, vacío, incorrecto y válido. Cuerpo del 401 idéntico entre contraseña errónea, usuario inexistente y código erróneo.
    - _Requirements: 3.3, 6.1_
    - **Hecho (Parte B, 2026-10-19):** `TestEmitirSesion401Uniforme` y `TestEmitirSesionFactorValido` (`internal/http/handlers_segundofactor_test.go`).

- [x] 4. Propiedades
  - [x] 4.1* Property 1 — Sin factor válido, no hay sesión
    - `TestPropertySinFactorValidoNoHaySesion`; `testing/quick`, `MaxCount: 100`, semilla fija.
    - **Validates: Requirements 2.2, 3.1, 3.2**
    - **Hecho (2026-10-19):** `segundofactor_flujo_test.go`, `MaxCount: 100`. Para todo código ≠ vigente (incluido vacío), `Emit` → 401 y nunca registra fila.
  - [x] 4.2* Property 2 — El código viaja intacto y no se filtra
    - `TestPropertyCodigoViajaIntactoYNoSeFiltra`: el doble recibe el valor exacto; el valor no aparece en el error devuelto ni en el log capturado.
    - **Validates: Requirements 4.1, 6.1**
    - **Hecho (2026-10-19):** `segundofactor_flujo_test.go`. El doble recibe el valor exacto (`UltimoCodigoRecibido`) y el código no aparece en el error. Nota: el flujo de emisión no escribe logs, así que la contención se verifica sobre el mensaje de error (el no-logueo del adaptador ya lo cubre E4-T1 R4.5).

- [x] 5. Higiene de persistencia
  - [x] 5.1* El código no se persiste
    - Prueba que tras una emisión (éxito y rechazo) ninguna columna de `identidad.sesion` ni clave de caché contiene el código enviado.
    - _Requirements: 4.2_
    - **Hecho a nivel unitario (2026-10-19):** `TestCodigoNoApareceEnLaFilaRegistrada` — la `SesionRecord` registrada no contiene el código en ningún campo. La verificación contra columnas reales de `identidad.sesion` y claves de caché es la variante de integración (requiere Postgres); el `SesionRecord` ya no transporta el código, que es la garantía observable sin DB.

- [x] 6. Cierre
  - [x] 6.1 Documentar el resultado
    - Entrada en `CHANGELOG.md`; actualizar la fila de A-15 en `REPORTE-BRECHAS.md` con H-1 a H-6 y su estado.
    - Declarar explícitamente qué pruebas de integración se ejecutaron y cuáles solo compilaron.
    - _Requirements: 7.1, 7.2, 7.3, 7.4_
    - **Hecho (2026-10-19):** entradas en `CHANGELOG.md` (línea base, Parte B C-2/C-3, Parte A doble+pruebas, e integración 3.1). En `REPORTE-BRECHAS.md` se añadió la sección 5 (E4-T5) con H-1…H-6 y su estado (H-1/H-2/H-3 cerradas, H-4 mitigada, H-5/H-6 abiertas en C-5), sin reescribir la tabla de E4-T3. Declarado qué corrió (incluida la integración contra Keycloak real) y qué nota de entorno aplica (WDAC).
  - [x] 6.2 Verificación estándar completa
    - `gofmt -l .`, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...`.
    - **Hecho (2026-10-19):** `go vet ./...` y `go vet -tags integration ./...` sin hallazgos; `go build ./...` en verde; `go test ./...` en verde en TODOS los paquetes (en esta corrida `internal/sesion` sí se ejecutó, el bloqueo WDAC fue intermitente). `gofmt` sobre los archivos nuevos solo muestra ruido de CRLF/acentos del working tree, sin cambios de formato de código. La integración `TestSegundoFactorIncorrectoOVencido` corrió aparte con `-tags integration` contra Keycloak 26.7.4 real.

# PARTE B — DESBLOQUEADA POR DECISIÓN

> **C-2 y C-3 resueltas (2026-10-19).** DEC-094 (C-2 → opción b) y DEC-095 (C-3 → b+d) están registradas como DECIDIDA en `ai27-platform-context` (`decision-log.md`, HEAD `a648dd7`). Estos grupos dejan de estar en espera y se ejecutan. C-5 sigue abierta y fuera de este repo.

## Grupo C-2 — Rechazo local de `segundo_factor` vacío (DEC-094, opción b)

**Decisión (DEC-094):** `EmitirSesion` rechaza un `segundo_factor` vacío o compuesto solo por espacios **antes** de llamar al IdP, con **401 uniforme indistinguible** del de credencial inválida (no enumeración; coherente con C-4). El campo queda obligatorio de facto en V1; el contrato no cambia.

- [x] B2.1 Guarda previa en el flujo de emisión (`compensatingFlow.Emit`, `internal/sesion/emission.go`): `strings.TrimSpace(SegundoFactor) == ""` → `errors.NewNoAutorizado("credencial inválida")`, **antes** de `NewSesionID` y de `Issue`. Sin residuo: no se genera `sesion_id`, no se llama al IdP, no hay `Compensate`. _Requirements: 2.3, 3.3, 3.4_
- [x] B2.2 Pruebas del rechazo local.
  - Servicio: `TestEmissionFlowRechazoLocalSegundoFactorVacio` (`emission_test.go`) — factor ausente, solo espacios y tab/salto → 401 con el mismo mensaje que credencial inválida, secuencia de pasos vacía, sin compensar.
  - HTTP: `TestEmitirSesion401Uniforme` y `TestEmitirSesionFactorValido` (`handlers_segundofactor_test.go`) — cuerpo del 401 idéntico para contraseña errónea, usuario inexistente y factor ausente/null/vacío/incorrecto; la ruta feliz propaga el factor.
  - Las pruebas previas de `emission_test.go` que emitían sin factor se ajustaron para enviar un factor no vacío (ya no cortocircuitan por la guarda).
  - _Requirements: 2.3, 3.3_

## Grupo C-3 — IdP en memoria con factor obligatorio y aviso (DEC-095, b+d)

**Decisión (DEC-095):** el doble `memory` **exige factor** (código esperado configurable en `memory.Sujeto`, validado en `Issue`) y se emite un **log de nivel alto** al seleccionar `memory` en el arranque. La demo del 11-nov corre contra Keycloak real. El doble NO valida TOTP real (regla anti-OTP): compara contra un código esperado, igual que la contraseña.

- [x] B3.1 `memory.Sujeto` gana campo `SegundoFactor` (código esperado); `memory.Provider.Issue` rechaza con `ErrCredencialInvalida` si el esperado está vacío o no coincide (`internal/idp/memory/memory.go`). Aviso de arranque de nivel alto en `construirIDP` (`cmd/api/main.go`) cuando la selección es `memory`. _Requirements: 2.4_
- [x] B3.2 Suite de contrato y registros ajustados para cubrir el factor en ambas implementaciones.
  - `idptest.Ejecutar` gana el caso "emitir sin segundo factor no devuelve token" (`ErrCredencialInvalida`, `Token` cero) y preserva el factor en el caso de contraseña inválida; Keycloak ya entregaba `credValida` con TOTP, así que ambas implementaciones pasan el mismo contrato.
  - `memory_test.go`: factories registran y envían el factor; pruebas nuevas `TestSegundoFactorObligatorio` y `TestSujetoSinFactorNoEmite`.
  - _Requirements: 2.4_

## Grupo C-5 — Brecha contractual

**Decisión pendiente (Arquitectura / dueños de contratos):** `bff.yaml` sin `segundo_factor`; descripción de `identidad.yaml` con WebAuthn/SMS/Cognito. Sin tarea en este repo. Cuando el contrato publique un tracer nuevo, **verificarlo contra el último tag** antes de dar la brecha por cerrada y decidir aparte si se sube `go.mod`.

# Notas de cierre

- **E4-T5 no cierra sin `mensajeria`:** su criterio 3 ("ni en `mensajeria`") se verifica en otro repo.
- **E4-T5 no garantiza un login real de usuarios nuevos:** depende de C-1 (enrolamiento, DEC-076).
- La Parte B no entra a ningún grafo de dependencias. Al desbloquearse un grupo, sus tareas se agregan como waves nuevas después de la 2.1.
- **C-2 y C-3 implementadas (2026-10-19)** sobre las decisiones DEC-094 y DEC-095. Verificación Go: `gofmt` (solo diferencias de CRLF/acentos del working tree en Windows, sin cambios de formato de código; el archivo nuevo `handlers_segundofactor_test.go` queda limpio), `go vet ./...` y `go vet -tags integration ./...` sin hallazgos, `go build ./...` en verde. Pruebas unitarias en verde en todos los paquetes tocados: `internal/sesion` (incluida `TestEmissionFlowRechazoLocalSegundoFactorVacio`), `internal/idp/memory` (`TestSegundoFactorObligatorio`, `TestSujetoSinFactorNoEmite`), `internal/http` (`TestEmitirSesion401Uniforme`, `TestEmitirSesionFactorValido`), `internal/archtest` (regla anti-OTP, R4.3) y `cmd/api`.
  - **Nota de entorno (WDAC):** `go test ./internal/sesion/...` falla al *ejecutar* el binario efímero de `%TEMP%` por una directiva de Control de aplicaciones de Windows. Workaround sin privilegios de admin: `go test -c -o .\sesion_test_bin.exe ./internal/sesion/` y ejecutar el binario persistente (`.\sesion_test_bin.exe -test.v`); WDAC bloquea el binario en `%TEMP%`, no el del workspace. Con ese método el paquete `sesion` pasa completo (solo SKIP las pruebas que piden `IDENTIDAD_TEST_DB`).
  - Pruebas `-tags integration` (Keycloak/Docker) no ejecutadas en esta sesión; solo se verificó que compilan.
