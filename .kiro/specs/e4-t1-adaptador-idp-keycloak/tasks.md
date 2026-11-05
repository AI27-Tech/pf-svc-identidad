# Implementation Plan: E4-T1 — Adaptador del IdP (Keycloak)

## Overview

Plan incremental para implementar `idp.IdentityProvider` contra Keycloak según `design.md`, en Go (golang-best-practices), dentro de `repos/pf-svc-identidad`. El orden hace que cada paso se pruebe solo: primero el puerto y el doble, luego la red de seguridad de aislamiento y, después, el adaptador por capas (configuración → clasificación pura → Issue/Refresh → Revoke → telemetría → probe y fachada). Al final van el cableado en `cmd/api`, el artefacto del realm y la prueba de integración.

Convenciones del plan:

- Prosa y comentarios en español (México); identificadores en inglés, salvo sustantivos de dominio (`sesion`, `rol`, `tenant`, `audiencia`, `sujeto`, …).
- **Verificación estándar:** toda tarea de código cierra con `gofmt -l .` sin salida, `go vet ./...`, `go build ./...` y `go test ./...` en la raíz del repo, todo en verde. Las tareas de integración también corren `go vet -tags integration ./...` y `go test -tags integration ./internal/idp/internal/kc/...`.
- PBT con `testing/quick`, `&quick.Config{MaxCount: 100}` como mínimo. Cada test lleva el comentario `// Feature: e4-t1-adaptador-idp-keycloak, Property N: <título>`.
- **Decisiones abiertas:** las tareas marcadas con **[Pendiente A-n]** implementan sólo la costura que deja el diseño. El caso de prueba que depende de la decisión queda omitido con `t.Skip("pendiente A-n: <qué falta decidir>")`. Ninguna tarea decide una regla de negocio.
- Fuera de este plan: despliegue (k3s, proxy de la DMZ), cambios a `pf-pkg-contracts` o `pf-pkg-platform-go`, y todo lo de E4-T3, E4-T4 y E4-T6.

## Decisiones confirmadas (2026-10-09)

✅ **A-3: Factor de dos pasos (OTP)** — Decisión: cliente envía usuario + contraseña + código OTP a Keycloak. Impacto: Tarea 1.1 amplía `Credentials` con campo `SegundoFactor`; Tarea 7.1 agrega OTP al formulario; Tarea 14.1 configura flujo OTP REQUIRED.

✅ **A-4: SessionResolver inyectable** — Decisión: función `Config.SessionResolver` que traduce sesionID_ai27 → kcSessionID. Si es nil, asume identidad (IDs iguales). Impacto: Tarea 5.1 define campo; Tarea 8.2 lo usa; E4-T3 implementará resolver concreto.

✅ **A-15: Variables IDENTIDAD_KEYCLOAK_* — Decisión: nombres propuestos son correctos (`IDENTIDAD_KEYCLOAK_TOKEN_URL`, `IDENTIDAD_KEYCLOAK_CLIENT_ID`, etc.). Impacto: Tarea 12.1 usa esos nombres exactos; Tarea 12.2 cablean en main(); README documenta.

## Decisiones resueltas (2026-10-09, segunda ronda PM)

✅ **A-2 → DEC-071:** segundo factor por **TOTP** nativo de Keycloak (correo y WebAuthn descartados). El formulario de `Issue` envía el parámetro `totp`; el flujo del realm pone OTP en REQUIRED.

✅ **A-13 → DEC-072:** opción **(c)** — el `sub` es el id opaco de Keycloak y el `SujetoID` se resuelve en servidor por `IdpSubject`. Claims ratificados: `sub`, `rol`, `tenant_id`, `sesion_id`, `audiencia`. NO se agrega atributo/claim `sujeto_id`.

✅ **A-16 → DEC-073:** **privilegio mínimo** para la cuenta de servicio (sin `manage-users`); FGAP v2 como primera opción, Logout OIDC como alternativa. La prueba 15.5 verifica que no pueda modificar usuarios.

✅ **Proceso → DEC-074:** los `t.Skip` por decisión pendiente se removieron; sólo queda la omisión por Docker no disponible (R11.4).

✅ **A-1:** un solo realm `ai27` (default del diseño, ratificado).

🟡 **A-7: Última versión estable de Keycloak** — la constante de imagen usa `26.7.0` como referencia (nunca `latest`); fijar la etiqueta exacta al verificar contra la versión estable elegida.

✅ **A-4 (parcial) → DEC-083, ratificado por Arquitectura 2026-10-12:** el `sesion_id` lo genera `identidad` y Keycloak sólo lo transporta. En el adaptador ya está aplicado: `Issue(ctx, cred, sesionID string)` envía el form param `sesion_id` y expone el id nativo del IdP como `idp.Token.IdpSessionID` (el antes "decodificado sin usar" `session_state`); `Revoke` revoca por ese `idpSessionID`. El realm cambió el note-mapper a la nota `SESION_ID` y agregó el executor REQUIRED `ai27-sesion-id-note`. DEC-084 y DEC-085 siguen pendientes y no bloquean E4-T1 (DEC-085 toca el orden `Issue`↔registro en `sesion` de E4-T3, a cerrar antes de producción).

✅ **JAR de la extensión + permiso FGAP v2 construidos (2026-10-12):** se agregó el módulo Maven `deploy/keycloak/extension/` (authenticator `ai27-sesion-id-note` + factory + registro SPI, deps `provided` de Keycloak 26.7.4), el permiso mínimo `manage-members` en el realm (cliente `admin-permissions`, FGAP v2), la prueba pura `TestRealmPermisoTerminarSesion`, el montaje del JAR prebuilt en el arnés (`rutaJARExtension`, con `t.Skip` si falta) y el job de CI `integration-keycloak` que compila el JAR y corre `-tags integration`. **Pendiente de ejecución en vivo (no de decisión):** compilar el JAR y correr la integración contra Keycloak 26.7.4 para confirmar que el claim `sesion_id` se puebla y que el permiso FGAP v2 termina sesiones. No se pudo correr en desarrollo (sin Docker ni Maven); corre en CI. Ver `DISENO-JAR-SESION-ID.md` §5–§8.

**Tareas desbloqueadas y ejecutadas (2026-10-09):** 10, 12, 17 (ronda previa) y 14, 15, 16, 18 (esta ronda).
**Pendiente de ejecución real (no de decisión):** la terminación de sesión del adaptador (R7.1/R7.5 en 15.5) depende del permiso mínimo que se finaliza al correr contra la versión fijada de Keycloak (A-16/A-7); hoy el binario de integración compila y veta, pero su ejecución requiere Docker.

## Tasks

- [x] 1. Errores centinela del puerto y migración del doble en memoria
  - Cierra con la verificación estándar.
  - [x] 1.1 Declarar los errores centinela en `internal/idp/port.go`
    - Agregar `ErrCredencialInvalida`, `ErrRefreshInvalido` y `ErrProveedorNoDisponible` con los comentarios del diseño (§Puerto). Las firmas de `IdentityProvider`, `Refresh` y `Token` no cambian.
    - Ampliar `Credentials` en `internal/idp/port.go` con campo `SegundoFactor string` (decidido A-3: factor OTP obligatorio).
    - No crear `ErrSegundoFactorInvalido` **[Pendiente A-3 refinamiento]** ni una categoría de modelo incompleto **[Pendiente A-14]**.
    - _Requirements: 1.1, 3.6, 4.1_
  - [x] 1.2 Migrar `internal/idp/memory` a los centinelas del puerto
    - `Issue` devuelve `idp.ErrCredencialInvalida` y `Refresh` devuelve `idp.ErrRefreshInvalido`.
    - Borrar `memory.ErrCredencialInvalida` y `memory.ErrRefreshInvalido`, sin alias ni reexportación. Antes de borrar, confirmar con grep que no tienen llamadores fuera del paquete (H2).
    - Agregar `(*Provider).SimularCaida(bool)`, protegido para uso concurrente: mientras esté activo, `Issue`, `Refresh` y `Revoke` devuelven `idp.ErrProveedorNoDisponible`.
    - _Requirements: 1.3, 3.3, 3.6_
  - [x]* 1.3 Unit tests del doble migrado
    - `errors.Is` contra los centinelas del puerto. Con `SimularCaida(true)`, las tres operaciones devuelven `ErrProveedorNoDisponible` y `Token{}`; con `SimularCaida(false)`, vuelve el comportamiento normal.
    - _Requirements: 1.3, 3.3_

- [x] 2. Suite aditiva `idptest.Categorias`
  - Cierra con la verificación estándar.
  - [x] 2.1 Implementar `Categorias(t *testing.T, nueva Factory)` en `internal/idp/idptest`
    - No tocar `Ejecutar`, sus cuatro casos ni la firma de `Factory`.
    - Agregar los cinco casos del diseño: (1) credencial inválida → sólo `ErrCredencialInvalida` y `Token` cero completo; (2) usuario inexistente y contraseña incorrecta producen textos idénticos que no contienen el usuario; (3) refresh desconocido → `ErrRefreshInvalido`; (4) refresh ya usado o de una sesión revocada → `ErrRefreshInvalido`; (5) contexto cancelado → `errors.Is(err, context.Canceled)` sin centinela.
    - Caso 4 **RESUELTO (A-4)**: usa la rotación del refresh token (reusar el refresh ya canjeado). Si la implementación no rota, se omite con `t.Skip` y un mensaje explícito que cita A-4.
    - Los casos se amplían con `SegundoFactor` en `Credentials` (decidido A-3).
    - _Requirements: 1.2, 1.3, 3.1, 3.2, 3.4, 3.5, 3.6_
  - [x] 2.2 Ejecutar `Ejecutar` y `Categorias` contra el doble en `internal/idp/memory/memory_test.go`
    - Los cuatro casos originales siguen aprobados, sin omitidos nuevos (R1.3). Agregar un caso aparte que use `SimularCaida` para la categoría de proveedor no disponible.
    - _Requirements: 1.3, 3.3_

- [x] 3. Verificación local de aislamiento en `internal/archtest`
  - Cierra con la verificación estándar; `arch-test@v0.1.1` del CI sigue sin cambios (R2.7).
  - [x] 3.1 Implementar la función de regla pura y las listas
    - Lista_De_Bibliotecas_IdP (prefijos del diseño), prefijos de AWS v1 y v2, y el prefijo permitido `…/internal/idp/internal`.
    - `violations(pkg string, imports []string) []string` produce mensajes `"%s importa %s (prohibido fuera de internal/idp/internal)"`. Para AWS la regla es la misma en cualquier paquete.
    - _Requirements: 2.2, 2.3_
  - [x] 3.2 Implementar `isolation_test.go` sobre el módulo real
    - Ejecutar `go list -deps=false -test -json ./...` dos veces, sin tags y con `-tags integration`, leyendo `Imports`, `TestImports` y `XTestImports`. Aplicar la regla de 3.1.
    - Regla C: recorrer con `go/parser` los `.go` fuera de `internal/idp/internal` y fallar si un literal de cadena contiene `/protocol/openid-connect/` o `/admin/realms/`. Se excluye el propio archivo de la prueba.
    - Fallar si `internal/idp/internal` usa `os.Getenv`, `os.LookupEnv` u `os.Environ` (R2.5).
    - Fallar si aparecen `otp_desafio` en `migrations/`, `otp.solicitado` o `/api/v1/otp` en el código o en las migraciones (R4.3).
    - _Requirements: 2.1, 2.2, 2.3, 2.5, 2.7, 4.3_
  - [x]* 3.3 Property test de la regla de aislamiento
    - **Property 12: Regla de aislamiento**
    - Generar rutas de paquete e imports a partir de la Lista_De_Bibliotecas_IdP, de AWS y de rutas neutras. Debe haber violación si y sólo si el import es de AWS, o es de la lista y el paquete está fuera de `…/internal/idp/internal`. El mensaje nombra el paquete y el import.
    - **Validates: Requirements 2.2, 2.3**

- [x] 4. Checkpoint: puerto, doble y aislamiento
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Configuración del adaptador `kc.Config`
  - Cierra con la verificación estándar.
  - [x] 5.1 Crear `internal/idp/internal/kc/config.go`
    - Definir `Config` con los campos del diseño: `TokenURL`, `RefreshURL`, `AdminSessionsURL`, `ClientID`, `ClientSecret`, `ServiceClientID`, `ServiceClientSecret`, `Timeout`, `HTTPClient`, `MeterProvider` y `SessionResolver`.
    - `Validate()` junta en un solo error todos los campos obligatorios faltantes, `Timeout <= 0` y las URLs que no parsean o cuyo esquema no es `http`/`https`. Nombra los campos y nunca sus valores.
    - El paquete no lee entorno ni archivos.
    - _Requirements: 2.5, 2.6, 3.8, 3.9, 7.4, 8.4_

- [x] 6. Clasificación pura de respuestas
  - Cierra con la verificación estándar.
  - [x] 6.1 Crear `internal/idp/internal/kc/classify.go`
    - Definir el enum interno `categoria` (`exito`, `credencialInvalida`, `refreshInvalido`, `proveedorNoDisponible`, `cancelado`) y la entrada de `classify(op, statusCode, oauthCode, transportErr, callerCtxErr, callCtxErr, bodyOK)`.
    - Aplicar la tabla de clasificación del diseño con precedencia por fila (1–11). Las filas 8, 10 y 11 son propuestas técnicas; la fila 9 lleva un comentario que cita **[Pendiente A-3/A-14]**.
    - `errorDe(op, categoria, ...)` construye `fmt.Errorf("idp/keycloak: %s: %w", op, centinela)` o envuelve `ctx.Err()` en el caso `cancelado`. Sólo agrega el código HTTP y un código OAuth de la lista cerrada; nunca el error de transporte, el cuerpo ni `error_description`.
    - Agregar el clasificador de causa de transporte (`timeout`, `dns`, `conn_refused`, `http_5xx`, `cuerpo_invalido`) para la telemetría posterior.
    - _Requirements: 3.1, 3.3, 3.4, 3.5, 3.6, 3.7, 7.3, 7.6_
  - [x]* 6.2 Property test de clasificación
    - **Property 1: Clasificación total, exclusiva y conforme a la tabla**
    - Generador `escenarioRespuesta` sobre operación, código HTTP, código OAuth (de la lista cerrada o arbitrario), cuerpo, error de transporte y estado de los contextos. Para cada escenario se verifica exactamente una condición (un solo centinela, sólo `ctx.Err()`, o `nil`) y que la categoría coincida con la primera fila aplicable.
    - **Validates: Requirements 3.1, 3.3, 3.4, 3.5, 3.6, 7.3, 7.6, 10.5**
  - [x]* 6.3 Unit tests por fila en `classify_test.go`
    - Un ejemplo con nombre por cada fila de la tabla, incluido el 404 idempotente de `Revoke` y el 401/403 de `Revoke` como no-éxito.
    - _Requirements: 3.3, 7.2, 7.6_

- [x] 7. Adaptador: `New`, `Issue` y `Refresh`
  - Cierra con la verificación estándar.
  - [x] 7.1 Crear `internal/idp/internal/kc/adapter.go`
    - `New(cfg)` llama a `Validate` antes de crear recursos y no hace llamadas de red. Arma el `adapter` con `http`, `now` inyectable y un `telemetry` interno con implementación no-op, que la tarea 10 sustituye por OTel.
    - Decodificar `tokenResponse` y `oauthError` (no exportados) con `io.LimitReader` de 64 KiB.
    - `issueForm(cred)` es el único lugar que arma el formulario de `grant_type=password`. **[Pendiente A-3]**: no lleva parámetro de segundo factor; un comentario indica que la costura para agregarlo es esta función.
    - `Issue` y `Refresh` usan `context.WithTimeout(ctx, cfg.Timeout)` y `http.NewRequestWithContext`. `ExpiraEn = recibidoEn.UTC().Add(expires_in·s)`; `expires_in <= 0` o un `access_token` vacío se tratan como respuesta no interpretable. Cada salida pasa por `classify`/`errorDe` y llama una vez a `tel.record` con `defer`.
    - Decodificar `SessionState` sin usarlo **[Pendiente A-4]**.
    - Agregar `var _ idp.IdentityProvider = (*adapter)(nil)` como verificación en compilación. Mientras no exista `Revoke` (tarea 8), éste devuelve `ErrProveedorNoDisponible`, nunca éxito.
    - _Requirements: 1.1, 1.4, 1.5, 2.4, 2.6, 3.1, 3.3, 3.4, 3.5, 3.7, 8.5_
  - [x]* 7.2 Property test: credencial rechazada sin causa ni usuario (`issue_prop_test.go`)
    - **Property 2: Credencial rechazada no revela causa ni usuario**
    - Generar usuarios arbitrarios (vacíos, Unicode) y `error_description` arbitrarios contra un `httptest.Server` que responde `invalid_grant`.
    - **Validates: Requirements 3.1, 3.2**
  - [x]* 7.3 Property test: ningún secreto en errores (`secrets_prop_test.go`)
    - **Property 3: Ningún secreto llega a errores ni a telemetría** (alcance de esta tarea: el texto de error; la tarea 10.4 agrega métricas y spans)
    - Generar contraseña y tokens aleatorios; el servidor los repite en `error_description` y en el cuerpo.
    - **Validates: Requirements 3.7, 4.5**
  - [x]* 7.4 Property test: mapeo de la respuesta de token (`token_prop_test.go`)
    - **Property 4: Mapeo de la respuesta de token**
    - `expires_in ∈ [1, 10^7]` con `now` fijo inyectado; `Location() == time.UTC`; `refresh_token` ausente produce `RefreshToken == ""` sin error.
    - **Validates: Requirements 1.4, 1.5**
  - [x]* 7.5 Property test: validación de configuración (`config_prop_test.go`)
    - **Property 5: Validación de configuración**
    - Generador `configParcial`: subconjunto vaciado de campos y `Timeout` arbitrario. `New` falla si y sólo si el subconjunto no está vacío o `Timeout <= 0`. El texto nombra exactamente esos campos, sin valores, y el servidor de prueba registra cero peticiones.
    - **Validates: Requirements 2.6, 3.9**
  - [x]* 7.6 Unit tests del adaptador (`adapter_test.go`)
    - Cancelación con un handler bloqueante: el retorno ocurre antes de `Timeout` y el error es `context.Canceled`. Vencimiento del `Timeout` propio → `ErrProveedorNoDisponible`. Cubrir también 5xx, cuerpo no JSON y conexión rechazada.
    - _Requirements: 3.3, 3.5_

- [x] 8. Token de servicio y `Revoke`
  - Cierra con la verificación estándar.
  - [x] 8.1 Crear `internal/idp/internal/kc/servicetoken.go`
    - `client_credentials` con `ServiceClientID`/`ServiceClientSecret` contra `TokenURL`. Caché hasta `expires_in − 10 s` protegida con `sync.Mutex`, sin goroutines, más `invalidate()`.
    - _Requirements: 7.4, 7.6, 8.5_
  - [x] 8.2 Implementar `Revoke` en `internal/idp/internal/kc/revoke.go`
    - Resolver el id de la sesión de Keycloak con `cfg.SessionResolver`; si es nil, se usa la identidad **[Pendiente A-4]**. Un error del resolvedor se devuelve envuelto, nunca como éxito.
    - `DELETE {AdminSessionsURL}/{id}` con el bearer de servicio. 204, 200 y 404 → `nil`. Ante un 401, se invalida la caché y se reintenta una sola vez; un 401 tras el reintento o un 403 → `ErrProveedorNoDisponible`. Timeout, red o 5xx → `ErrProveedorNoDisponible`.
    - Quitar la implementación provisional de la tarea 7.1.
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.6, 8.4, 8.5_
  - [x]* 8.3 Property test: `Revoke` idempotente y acotado (`revoke_prop_test.go`)
    - **Property 6: `Revoke` idempotente y acotado**
    - Servidor de administración falso con un conjunto de sesiones. Para un `sesionID` arbitrario y `n ≥ 1` repeticiones, todas devuelven `nil`, el conjunto final es el inicial menos a lo sumo esa sesión y sólo hay `DELETE` a esa ruta.
    - **Validates: Requirements 7.1, 7.2, 7.5**
  - [x]* 8.4 Property test: sólo URLs configuradas (`urls_prop_test.go`)
    - **Property 8: El adaptador sólo llama a las URLs configuradas**
    - Usar un `RoundTripper` grabador con hosts y rutas arbitrarios y secuencias de `Issue`/`Refresh`/`Revoke`, incluido el token de servicio.
    - **Validates: Requirements 8.4, 8.5**
  - [x]* 8.5 Unit tests de `Revoke` (`revoke_test.go`)
    - Reintento único ante 401, 403 como no-éxito, error del resolvedor, reutilización de la caché del token de servicio y renovación cuando vence.
    - _Requirements: 7.3, 7.6_

- [x] 9. Checkpoint: adaptador completo sin telemetría real
  - Ensure all tests pass, ask the user if questions arise.

- [x] 10. Telemetría OTel del adaptador
  - Cierra con la verificación estándar.
  - [x] 10.1 Crear `internal/idp/internal/kc/telemetry.go` y cablearlo en `New`
    - `Float64Histogram` `ai27_idp_duracion_segundos` (unidad `s`) con atributos `dependencia=keycloak`, `operacion` y `resultado`. `resultado=otro` es provisional, para la cancelación **[Pendiente A-14]**. El atributo `causa` sólo aparece con `proveedor_no_disponible`.
    - Agregar el span cliente `idp.keycloak.<operacion>` con los mismos atributos, sin otros.
    - Si `Config.MeterProvider` es nil, usar `otel.GetMeterProvider()` (H6).
    - Agregar `go.opentelemetry.io/otel/sdk/metric v1.46.0` como dependencia de prueba fijada (la misma versión que usa platform-go).
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_
  - [x]* 10.2 Property test: una medición por llamada (`telemetry_test.go`)
    - **Property 9: Una medición por llamada, con la categoría devuelta**
    - Usar `sdkmetric.NewManualReader()` sobre secuencias generadas de resultados. Un `Revoke` idempotente se registra como `exito` y la cancelación como `otro`, nunca como `proveedor_no_disponible`.
    - **Validates: Requirements 10.1, 10.2, 10.4, 10.5**
  - [x]* 10.3 Property test: exportación fallida (`telemetry_test.go`)
    - **Property 11: La exportación de telemetría no altera el resultado**
    - Comparar un adaptador con un exportador que siempre falla contra uno funcional en el mismo escenario: el mismo `Token` y el mismo centinela según `errors.Is`.
    - **Validates: Requirements 10.6**
  - [x]* 10.4 Ampliar la Property 3 a métricas y spans (`telemetry_test.go`)
    - **Property 3: Ningún secreto llega a errores ni a telemetría**
    - Revisar los atributos del `ManualReader` y de un `tracetest.SpanRecorder`.
    - **Validates: Requirements 3.7, 4.5, 10.3**

- [x] 11. Probe de alcanzabilidad y fachada `internal/idp/keycloak`
  - Cierra con la verificación estándar.
  - [x] 11.1 Crear `internal/idp/internal/kc/probe.go`
    - `NewProbe(cfg)` valida la configuración y devuelve `func(ctx) error` que hace `GET` a `.well-known/openid-configuration`, derivado de `TokenURL`, con `context.WithTimeout(ctx, 5*time.Second)`.
    - Sólo devuelve errores saneados: `idp:keycloak no alcanzable` o `idp:keycloak respondió <código>`.
    - _Requirements: 9.5_
  - [x] 11.2 Crear la fachada `internal/idp/keycloak/keycloak.go`
    - `type Config = kc.Config`, `New(cfg) (idp.IdentityProvider, error)` y `Probe(cfg)`. Sin lógica ni tipos de terceros. La fachada queda sujeta a la prueba de aislamiento de la tarea 3.
    - _Requirements: 2.4_
  - [x]* 11.3 Unit tests del probe (`probe_test.go`)
    - Servidor caído → error saneado en ≤ 5 s. Respuesta 500 → `respondió 500` sin cuerpo. Respuesta 200 → `nil`.
    - _Requirements: 9.5_

- [x] 12. Cableado en `cmd/api`
  - Cierra con la verificación estándar.
  - [x] 12.1 Extraer `cargarConfig(lookup func(string) (string, bool), readFile func(string) ([]byte, error))` en `cmd/api/config.go`
    - Valores de `IDENTIDAD_IDP`: `""` o `memory` → doble; `keycloak` → `keycloak.Config` desde `IDENTIDAD_KEYCLOAK_*` (nombres propuestos, A-15). Cualquier otro valor → error que nombra `IDENTIDAD_IDP` y los valores aceptados, sin repetir el valor recibido.
    - Juntar todas las variables obligatorias faltantes en un solo error, sin valores.
    - `REFRESH_URL` toma por omisión `TOKEN_URL` (propuesta). `TIMEOUT` se lee con `time.ParseDuration` y no tiene valor por omisión.
    - Secretos: `X_FILE` (archivo leído, con espacios finales recortados) o `X`; definir ambos es error.
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.6_
  - [x] 12.2 Integrar en `run()` de `cmd/api/main.go`
    - Resolver el IdP **antes** de `pgxpool.New` y `ListenAndServe`; ante error, `log.Fatalf`.
    - Inyectar el IdP en `identidad.Deps.IDP` y escribir en el log `proveedor de identidad: keycloak|memory`.
    - Con Keycloak, montar un segundo `health.Registry` en `/ready/idp` con el check `idp:keycloak` (`keycloak.Probe`). `/ready` queda intacto, con `postgres:svc_identidad` como único check.
    - Mantener sin cambios el comportamiento standalone: `IDENTIDAD_ADDR`, `IDENTIDAD_AUDIENCIA`, `IDENTIDAD_DATABASE_URL` y `IDENTIDAD_JWKS_URL`.
    - _Requirements: 8.6, 8.7, 9.1, 9.2, 9.3, 9.5, 9.6, 9.7_
  - [x]* 12.3 Property test de la carga de configuración (`cmd/api/config_prop_test.go`)
    - **Property 10: Carga de configuración en la raíz**
    - Generador `entornoGenerado` que cubre los casos (i), (ii) y (iii) del diseño. Ningún valor del entorno aparece en el texto del error.
    - **Validates: Requirements 9.2, 9.3, 9.6**
  - [x]* 12.4 Unit tests de cableado (`cmd/api/config_test.go`, `cmd/api/ready_test.go`)
    - `_FILE` frente a la variable directa frente a ambas, y el log de la implementación elegida, sin secretos.
    - Con el probe fallando, `/ready` responde 200 y `/ready/idp` responde 503 con un cuerpo saneado.
    - _Requirements: 9.4, 9.5, 9.7_

- [x] 13. Checkpoint: adaptador cableado
  - Ensure all tests pass, ask the user if questions arise.

- [x] 14. Artefacto del realm y coherencia con `claims.Componer`
  - Cierra con la verificación estándar.
  - [x] 14.1 Crear `deploy/keycloak/realm-ai27.json`
    - Un solo realm `ai27` (A-1: un solo realm), RS256, `revokeRefreshToken: true` y `refreshTokenMaxReuse: 0`.
    - User profile con `tenant_id` editable y visible sólo para admin. Por A-13 (DEC-072 opción c) NO se agrega `sujeto_id`: el `sub` es el id opaco de Keycloak.
    - Los ocho grupos de DEC-053 con atributos `rol` y `audiencia`; sin `auditor`.
    - Cliente `ai27-login`, confidencial, sin cuenta de servicio, con grant directo y el flujo `ai27-direct-grant` (Username, Password y OTP REQUIRED, sin subflujos condicionales). TOTP por DEC-071.
    - Cliente `ai27-identidad-svc`, sólo con cuenta de servicio, privilegio mínimo (A-16/DEC-073): sin `manage-users`. El permiso acotado de terminación de sesión (FGAP v2 o Logout OIDC) se finaliza al verificar contra la versión fijada de Keycloak (A-7); la prueba 15.5 exige que no pueda modificar usuarios.
    - Scope `ai27-claims` con los mappers `tenant_id`, `rol`, `audiencia` (`multivalued=false`), `sesion_id` y el audience mapper `ai27-internal`.
    - Secretos `${KC_AI27_LOGIN_SECRET}` y `${KC_AI27_SVC_SECRET}`; sin usuarios.
    - _Requirements: 4.1, 5.1, 5.2, 5.3, 5.4, 5.7, 5.9, 5.10, 6.1, 6.3, 6.5, 7.4_
  - [x] 14.2 Crear `internal/idp/internal/kc/testdata/realm-ai27-users-0.json`
    - `factory-<rol>` ×8 con contraseña y OTP de semilla conocida (`JBSWY3DPEHPK3PXP`) e id fijo (sub conocido, A-13/c), `sin-factor-<rol>` ×8, `cambio-rol`, y `tenant_id` con UUID fijos. Las contraseñas son constantes de prueba.
    - _Requirements: 1.6, 4.4, 5.5, 6.6, 6.7_
  - [x]* 14.3 Property test del artefacto (`realm_prop_test.go`)
    - **Property 7: Coherencia del artefacto del realm con el modelo**
    - Prueba pura que recorre todo el dominio finito: los nombres de grupo son igual al conjunto de los `Rol` del contrato; `audiencia == AudienciaDeRol(rol)`; los mappers tienen claims permitidos y `multivalued=false`; todo `secret` es un marcador `${…}`.
    - **Validates: Requirements 5.3, 5.4, 5.10, 6.3, 6.5**
  - [x]* 14.4 Unit tests del artefacto (`realm_test.go`)
    - El flujo de grant directo tiene OTP REQUIRED y ningún subflujo condicional (R4.1). `tenant_id` tiene `edit: ["admin"]` (R5.1). El archivo versionado no contiene usuarios.
    - _Requirements: 4.1, 5.1, 5.7_

- [x] 15. Prueba de integración contra Keycloak (`//go:build integration`)
  - Cierra con la verificación estándar y además `go vet -tags integration ./...` y `go test -tags integration ./internal/idp/internal/kc/...`.
  - Nota (DEC-074): se removieron los `t.Skip` por decisión pendiente; la única omisión que queda es Docker no disponible (R11.4). El binario de integración compila y veta en verde (`go vet -tags integration ./...`). La ejecución real contra el contenedor requiere Docker (no disponible en este entorno).
  - [x] 15.1 Crear el arnés `internal/idp/internal/kc/integration_test.go`
    - Constante de imagen `quay.io/keycloak/keycloak:26.7.0`, con un comentario **[Pendiente A-7]**: es el ejemplo de referencia, no la versión fijada, y nunca `latest`.
    - `testcontainers.NewDockerProvider()` + `Health`: si falla, `t.Skip("Docker no disponible: …")`. Cualquier error posterior es `t.Fatalf` con la etapa (`arranque` o `import`).
    - `GenericContainer` con `start-dev --import-realm`, montando los dos JSON, el admin bootstrap y los secretos aleatorios de `crypto/rand`. Esperar con `wait.ForHTTP(".../.well-known/openid-configuration")` y un `WithStartupTimeout` de 120 s. Registrar `testcontainers.CleanupContainer` justo después de crear el contenedor.
    - `esperadoSujeto(m)` devuelve el `sub` esperado (A-13/DEC-072 c: id opaco de Keycloak) y `factorPara(usuario)` calcula el TOTP vigente (A-3/DEC-071, RFC 6238).
    - `testcontainers-go` ya es dependencia directa (`v0.44.0`).
    - _Requirements: 11.1, 11.3, 11.4, 11.5, 11.7, 11.8_
  - [x]* 15.2 Suite de contrato contra Keycloak (`integration_contract_test.go`)
    - La Factory devuelve un adaptador contra el contenedor y la credencial de `factory-monitorista` con su OTP vigente (R1.6). Ejecuta `idptest.Ejecutar` e `idptest.Categorias`.
    - Con A-3 (DEC-071) resuelto y el formulario de `Issue` enviando `totp`, el grant directo con OTP REQUIRED emite. Verificación directa de emisión exitosa con refresh token (R1.7). Sin `t.Skip` por decisión (DEC-074).
    - _Requirements: 1.2, 1.6, 1.7, 11.2_
  - [x]* 15.3 Casos de realm y segundo factor (`integration_realm_test.go`)
    - R4.4: los 8 usuarios `sin-factor-*` son rechazados con `Token{}` y sin `ErrProveedorNoDisponible`.
    - R5.3 y R5.4: lectura de grupos. R5.5, R5.6 y R5.9: la actualización de perfil y la API de administración con el token de `ai27-login` rechazan los cambios, y una lectura administrativa posterior conserva `tenant_id`.
    - _Requirements: 4.2, 4.4, 5.3, 5.4, 5.5, 5.6, 5.8, 5.9, 11.6_
  - [x]* 15.4 Property test de claims en integración (`integration_claims_test.go`)
    - **Property 13: Coherencia de claims en emisión y refresco (integración)**
    - Por cada `factory-<rol>`: emitir, refrescar y verificar con `auth.NewVerifier` contra el JWKS del contenedor, comparando con `claims.Componer(m)`. `Audiencia` se lee del claim en `Raw`; el token no contiene identificadores de alcance.
    - R6.7: mover `cambio-rol` de grupo con la credencial de `master` y refrescar.
    - Sin omisiones por A-3/A-4/A-13: con DEC-071/072 resueltas, `sub == esperadoSujeto(m)` y `SesionID != ""` se afirman directamente (DEC-074).
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8**
  - [x]* 15.5 Casos de revocación (`integration_revoke_test.go`)
    - R7.4 (A-16/DEC-073): la cuenta de servicio intenta un `PUT` a un usuario y debe recibir 403; la prueba falla si puede modificarlo.
    - R7.7: la API de administración con la credencial de `ai27-login` rechaza cerrar la sesión.
    - R7.1 y R7.5: dos sesiones del mismo usuario; revocar una por el adaptador deja la otra viva (idempotente). **Pendiente de ejecución (A-16/A-7):** depende del permiso mínimo de terminación de sesión que se finaliza al verificar la versión fijada de Keycloak; la prueba falla con mensaje claro si falta.
    - _Requirements: 7.1, 7.4, 7.5, 7.7_

- [x] 16. Checkpoint: integración
  - Compilación y análisis en verde con y sin `-tags integration`. Ejecución real contra el contenedor pendiente de un entorno con Docker.

- [x] 17. Documentación en código y del repositorio
  - Cierra con la verificación estándar.
  - [x] 17.1 Actualizar `internal/idp/internal/doc.go` y el comentario de paquete de `internal/idp/port.go`
    - Quitar "EN ESPERA de ADR 32" y las menciones a Cognito, y describir la fachada `internal/idp/keycloak` y el árbol `kc`. P-0 sigue abierto: la redacción dice "Keycloak (confirmado 2026-10-08, pendiente de asentar en decision-log)".
    - _Requirements: 2.1, 2.4_
  - [x] 17.2 Actualizar `README.md`, `REPORTE-BRECHAS.md` §3.1 y `CHANGELOG.md`
    - README: variables `IDENTIDAD_IDP` e `IDENTIDAD_KEYCLOAK_*` (marcadas como propuesta, A-15), `/ready/idp` y cómo correr `-tags integration`.
    - REPORTE-BRECHAS §3.1: estado del adaptador y pendientes A-3, A-4, A-7, A-13 y A-16.
    - CHANGELOG: entrada de la feature (centinelas del puerto, borrado de los errores de `memory` y adaptador Keycloak).
    - _Requirements: 9.2, 11.5_

- [x] 18. Checkpoint final
  - `gofmt`, `go vet ./...`, `go build ./...` y `go test ./...` en verde; `go vet -tags integration ./...` en verde. Las pruebas de integración compilan; su ejecución real contra Keycloak requiere Docker.

## Notes

- Las sub-tareas marcadas con `*` son pruebas (unitarias, de propiedad o de integración) y pueden omitirse para un MVP más rápido. El arnés de integración 15.1 no lleva `*` porque R11 es parte de la definición de terminado de E4-T1. Aun así, sin 15.2–15.5 el spec no cumple R11.2 ni R11.6.
- R8.1–R8.3 (proxy de la DMZ) se verifican en el despliegue; no hay tareas para ellos.
- A-3 y A-4 están resueltas (OTP por TOTP en el formulario de `Issue`; `SessionResolver` nil = identidad). Con DEC-071/072/073/074, el plan ya no tiene omisiones por decisión pendiente; la única omisión viva es Docker no disponible (R11.4).
- Falta de ejecución, no de decisión: correr la suite de integración contra la versión fijada de Keycloak (A-7) para (a) confirmar la forma del flujo de grant directo con OTP y el user profile en esa versión, y (b) finalizar el permiso mínimo de terminación de sesión de la cuenta de servicio (A-16/DEC-073) que hace pasar R7.1/R7.5.
- Agregar la Lista_De_Bibliotecas_IdP al Glossary de `requirements.md` y registrar A-16 en sus pendientes queda para cuando se apruebe el diseño; este plan no modifica `requirements.md` ni `design.md`.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "3.1", "5.1"] },
    { "id": 1, "tasks": ["1.2", "2.1", "3.2", "6.1"] },
    { "id": 2, "tasks": ["1.3", "3.3", "6.2", "6.3", "7.1"] },
    { "id": 3, "tasks": ["2.2", "7.2", "7.3", "7.4", "7.5", "7.6", "8.1"] },
    { "id": 4, "tasks": ["8.2"] },
    { "id": 5, "tasks": ["8.3", "8.4", "8.5", "10.1"] },
    { "id": 6, "tasks": ["10.2", "10.3", "10.4", "11.1"] },
    { "id": 7, "tasks": ["11.2", "11.3", "14.1", "14.2"] },
    { "id": 8, "tasks": ["12.1", "14.3", "14.4"] },
    { "id": 9, "tasks": ["12.2", "12.3"] },
    { "id": 10, "tasks": ["12.4", "15.1"] },
    { "id": 11, "tasks": ["15.2", "15.3", "15.4", "15.5"] },
    { "id": 12, "tasks": ["17.1", "17.2"] }
  ]
}
```
