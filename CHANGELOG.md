# CHANGELOG

## 2026:10:09 — Respuesta a C/D: cambios acotados (D parcial, versión)

**Resumen:** Product Management respondió las decisiones C y D de la integración (documento de respuestas del 2026-10-09). Se aplican los dos cambios acotados y de bajo riesgo que esas respuestas habilitan sin depender de ratificación de Arquitectura: (D) el `rol` y la `audiencia` dejan de vivir como atributo del usuario en el `testdata` y pasan a resolverse únicamente desde el grupo; (5) la imagen del contenedor se fija en Keycloak **26.7.4** exacta (A-7 provisional).

**Para quién:** para QA/Arquitectura (acerca `TestCambioRolRefresco` a verde al eliminar la precedencia del atributo de usuario sobre el del grupo) y para despliegue (fija la versión contra la que se verifica).

**Cambios principales:**

- **`testdata/realm-ai27-users-0.json` (D, punto 4 de la respuesta de PM):** se quitan `rol` y `audiencia` del bloque `attributes` de los 17 usuarios; sólo queda `tenant_id` (que sí es atributo propio del usuario). El `rol` y la `audiencia` ahora provienen exclusivamente del grupo del usuario, de modo que mover de grupo cambie el claim al refrescar (R6.7). El mapper del realm sigue siendo `oidc-usermodel-attribute-mapper`, que según PM resuelve también atributos heredados del grupo.
- **`integration_test.go` (punto 5):** `imagenKeycloak` pasa de `26.7.0` a `26.7.4` exacta (A-7 provisional, en la línea de DEC-070 / DEC-081).

**Verificación:**
- ✓ `go build ./...`
- ✓ `go vet ./...` y `go vet -tags integration ./...` (ambos exit 0)
- ✓ `go test ./...` — unitario en verde
- ✓ JSON del `testdata` válido (ConvertFrom-Json)
- ⚠ No re-ejecutada la suite `-tags integration` (levanta Keycloak ~7-15 min). Pendiente correrla contra 26.7.4.

**Pendiente (requiere ratificación de Arquitectura, no aplicado aquí):**
- **C (puntos 1-3):** `sesion_id` vía authenticator Java propio (JAR) que escribe la nota de sesión `SESSION_ID`, parámetro en `Issue`, y permiso FGAP v2 de la cuenta de servicio para `Revoke`. Nada de esto está en el repo todavía (no hay JAR ni paso que lo monte en el contenedor; `issueForm` no envía `sesion_id`; el realm no concede el permiso de terminación de sesión).
- **D (fallback):** si el perfil de usuario declarativo de 26.7.4 filtra la herencia del atributo del grupo, se requiere el mapper Java del paso 2 de PM.
- **Caso dos grupos de rol:** emisión que falle cerrada + prueba (abierto por PM, depende de Producto/Arquitectura).

**Archivos modificados:**
- `internal/idp/internal/kc/testdata/realm-ai27-users-0.json`
- `internal/idp/internal/kc/integration_test.go`

---

## 2026:10:09 23:30

**Resumen:** Primera **ejecución real** de la suite `-tags integration` de E4-T1 contra un Keycloak 26.7.0 (testcontainers + Docker), que hasta ahora sólo compilaba. Se corrigieron cuatro defectos del artefacto del realm y del arnés que impedían el arranque y la emisión, más la clasificación del rechazo por falta de OTP y la robustez del segundo factor TOTP en las pruebas. Quedan dos decisiones de diseño abiertas (`sesion_id` y rol dinámico por grupo), documentadas aparte.

**Para quién:** para QA/Arquitectura (la integración ahora corre de verdad y revela el estado real del adaptador contra Keycloak) y para despliegue (el realm `ai27` ya importa en Keycloak 26.7).

**Cambios principales:**

- **Import del realm (`deploy/keycloak/realm-ai27.json`):** el binding del flujo de grant directo del cliente `ai27-login` ahora referencia el `id` (UUID) del flujo `ai27-direct-grant`, no su alias; el flujo declara ese `id`. Sin esto Keycloak abortaba el arranque con "Unable to resolve auth flow binding override for: direct_grant".
- **Longitud de descripción:** se acortó la `description` del cliente `ai27-identidad-svc` (393 → <255 caracteres) para no exceder la columna `VARCHAR(255)` de Keycloak, que hacía fallar el import.
- **Mappers de claims del scope `ai27-claims`:** Keycloak 26.7 no tiene `oidc-group-attribute-mapper` ni `oidc-sessionid-mapper`. Se reescribieron `rol` y `audiencia` como `oidc-usermodel-attribute-mapper` (emiten string único, no lista) y se agregó `sub` con `oidc-sub-mapper`. Con esto el token trae `sub`, `rol` (string), `tenant_id`, `audiencia` y `aud` correctos. El `testdata` agrega los atributos de usuario `rol` y `audiencia`.
- **Secreto TOTP (`testdata/realm-ai27-users-0.json`):** Keycloak usa `secretData.value` **verbatim** como clave HMAC (no lo decodifica de base32). Se cambió la semilla a una cadena ASCII (`ai27-totp-secret`) usada igual por el `testdata` y por `totpAhora`.
- **Nombres de import (arnés `integration_test.go`):** los archivos se montan como `ai27-realm.json` y `ai27-users-0.json` (convención `<realm>-realm.json` / `<realm>-users-<n>.json`); con el nombre anterior Keycloak ignoraba los usuarios (`user_not_found`).
- **Clasificación "sin OTP" (A, `classify.go`):** un `Issue` con 500 `unknown_error` (usuario sin OTP ante flujo OTP REQUIRED) ahora mapea a `ErrCredencialInvalida`, no a `ErrProveedorNoDisponible`. Acotado a `Issue` + ese código OAuth para no enmascarar caídas reales (un Keycloak caído cae en transporte/timeout). Property 1 y los casos por fila actualizados.
- **Robustez del segundo factor (B, `integration_test.go`):** TOTP es de un solo uso por ventana (RFC 6238 §5.2; `otpPolicyCodeReusable=false` por omisión). Como todos los usuarios comparten semilla, dos emisiones en los mismos 30 s reutilizaban el código y la segunda fallaba. Se serializó el consumo de códigos (`codigoTOTPFresco`) para que cada emisión use una ventana fresca con margen.

**Verificación:**
- ✓ `gofmt` sobre los archivos modificados (el resto del repo queda en LF, como lo guarda git; `gofmt -l` marca CRLF del working copy de Windows, preexistente).
- ✓ `go vet ./...` y `go vet -tags integration ./...`
- ✓ `go build ./...`
- ✓ `go test ./...` — unitario en verde (incluye `classify_test.go` con la nueva fila)
- ✓ `go test -tags integration` ejecutado contra Keycloak 26.7.0 real: de 9 fallas iniciales (el contenedor ni arrancaba) a 4, todas de las dos decisiones abiertas. `TestSinFactorRechazado` (A) y la antigua flakiness de "credencial inválida" (B) quedan en verde.

**Alcance / pendiente (decisión, no ejecución):** dos decisiones de diseño abiertas, documentadas en `.kiro/specs/e4-t1-adaptador-idp-keycloak/DECISIONES-PENDIENTES-INTEGRACION.md`:
- **C:** de dónde sale `sesion_id` (Keycloak sólo da `sid` nativo; el verificador lee `sesion_id`) y el permiso mínimo de la cuenta de servicio para revocar (A-16).
- **D:** si el `rol` del token es dinámico por grupo (R6.7) o atributo del usuario reasignado por el servicio (DEC-053).
Las pruebas `TestPropiedadCoherenciaClaims`, `TestRevocarUnaSesion` y `TestCambioRolRefresco` quedan en rojo a propósito hasta que se decidan C y D.

**Archivos modificados:**
- `deploy/keycloak/realm-ai27.json`
- `internal/idp/internal/kc/testdata/realm-ai27-users-0.json`
- `internal/idp/internal/kc/classify.go`
- `internal/idp/internal/kc/classify_test.go`
- `internal/idp/internal/kc/integration_test.go`
- `internal/idp/internal/kc/integration_claims_test.go`
- `internal/idp/internal/kc/integration_realm_test.go`
- `internal/idp/internal/kc/integration_revoke_test.go`
- `internal/idp/internal/kc/realm_test.go`
- `internal/idp/internal/kc/realm_prop_test.go`
- `.kiro/specs/e4-t1-adaptador-idp-keycloak/DECISIONES-PENDIENTES-INTEGRACION.md` (nuevo)

---

## 2026:10:09 18:00

**Resumen:** Se desbloquean y ejecutan las tareas 14 (artefacto del realm) y 15 (prueba de integración) de E4-T1, tras asentarse DEC-071 (TOTP), DEC-072 (identidad del sujeto opción c), DEC-073 (privilegio mínimo de la cuenta de servicio) y DEC-074 (`t.Skip` como excepción con caducidad). Se agrega el artefacto versionado del realm `ai27`, los usuarios de prueba, las pruebas puras del artefacto (Property 7) y la suite de integración con testcontainers (Property 13, contrato, segundo factor, revocación). Se cablea el segundo factor TOTP en el formulario de `Issue`.

**Para quién:** para despliegue (el realm `ai27` versionado es la base del realm de producción) y para QA/arquitectura (la suite `-tags integration` verifica el adaptador contra un Keycloak real). Cierra el grueso de E4-T1; queda como ejecución pendiente correr la integración contra la versión fijada de Keycloak (A-7).

**Cambios principales:**

- **Segundo factor TOTP (A-3/DEC-071):** `adapter.issueForm` agrega el parámetro `totp` al grant directo cuando `Credentials.SegundoFactor` no está vacío. El flujo `ai27-direct-grant` del realm pone OTP en REQUIRED.

- **Artefacto del realm (tarea 14):** `deploy/keycloak/realm-ai27.json` — un solo realm `ai27` (A-1), RS256, rotación del refresh token (`revokeRefreshToken`, `refreshTokenMaxReuse: 0`), ocho grupos de DEC-053 con atributos `rol`/`audiencia` (sin `auditor`), user profile con `tenant_id` sólo admin, cliente `ai27-login` (grant directo + flujo OTP REQUIRED), cliente `ai27-identidad-svc` de privilegio mínimo (A-16/DEC-073, sin `manage-users`), scope `ai27-claims` con mappers `tenant_id`/`rol`/`audiencia`/`sesion_id` y audience mapper `ai27-internal`, secretos como marcadores `${...}`. Por DEC-072 (c) NO se agrega `sujeto_id`. Usuarios de prueba en `internal/idp/internal/kc/testdata/realm-ai27-users-0.json` (`factory-<rol>` ×8 con TOTP de semilla conocida, `sin-factor-<rol>` ×8, `cambio-rol`).

- **Pruebas puras del artefacto (14.3/14.4):** `realm_prop_test.go` (Property 7: nombres de grupo = roles del contrato, `audiencia == AudienciaDeRol(rol)`, mappers con claims permitidos y `multivalued=false`, secretos como marcadores) y `realm_test.go` (RS256 + rotación, grant directo con OTP REQUIRED sin subflujos, `tenant_id` sólo admin, cuenta de servicio mínima, versionado sin usuarios).

- **Integración (tarea 15, `//go:build integration`):** `integration_test.go` (arnés testcontainers con `start-dev --import-realm`, secretos aleatorios de `crypto/rand`, `t.Skip` sólo si Docker no está disponible, `factorPara` que calcula TOTP RFC 6238, `esperadoSujeto` por DEC-072 c), `integration_contract_test.go` (`idptest.Ejecutar`/`Categorias` + emisión exitosa con refresh token), `integration_realm_test.go` (R4.4 sin-factor rechazados, lectura de grupos, token de login rechazado en la API de administración), `integration_claims_test.go` (Property 13 emisión+refresco verificados contra el JWKS, R6.7 cambio de grupo), `integration_revoke_test.go` (R7.4 la cuenta de servicio no modifica usuarios, R7.7, R7.1/R7.5).

**Verificación:**
- ✓ `gofmt` sobre los archivos nuevos
- ✓ `go vet ./...` y `go vet -tags integration ./...`
- ✓ `go build ./...`
- ✓ `go test ./...` — en verde, incluidas las pruebas puras del realm y el aislamiento de `internal/archtest`
- ✓ El binario de integración compila (`go test -tags integration ...`); su ejecución real requiere Docker

**Alcance / pendiente (ejecución, no decisión):**
- Correr la suite `-tags integration` contra la versión fijada de Keycloak (A-7) para confirmar la forma del flujo de grant directo con OTP y del user profile en esa versión.
- Finalizar el permiso mínimo de terminación de sesión de `ai27-identidad-svc` (A-16/DEC-073: FGAP v2 o Logout OIDC): hoy la cuenta no lleva `manage-users` (R7.4/R7.7 pasan por construcción); R7.1/R7.5 (revocar una de dos sesiones por el adaptador) necesita ese permiso acotado y la prueba falla con mensaje claro si falta.

**Archivos modificados:**
- `deploy/keycloak/realm-ai27.json` (nuevo)
- `internal/idp/internal/kc/testdata/realm-ai27-users-0.json` (nuevo)
- `internal/idp/internal/kc/realm_test.go` (nuevo)
- `internal/idp/internal/kc/realm_prop_test.go` (nuevo)
- `internal/idp/internal/kc/integration_test.go` (nuevo)
- `internal/idp/internal/kc/integration_contract_test.go` (nuevo)
- `internal/idp/internal/kc/integration_realm_test.go` (nuevo)
- `internal/idp/internal/kc/integration_claims_test.go` (nuevo)
- `internal/idp/internal/kc/integration_revoke_test.go` (nuevo)
- `internal/idp/internal/kc/adapter.go` (segundo factor TOTP en `issueForm`)
- `.kiro/specs/e4-t1-adaptador-idp-keycloak/tasks.md`

---

## 2026:10:09 00:00

**Resumen:** Se avanzan las tareas no bloqueadas del adaptador de Keycloak (E4-T1): telemetría OTel del adaptador (tarea 10), cableado en `cmd/api` tras `IDENTIDAD_IDP` con `/ready/idp` (tarea 12) y documentación en código y repo (tarea 17). Las tareas 14 (artefacto del realm) y 15 (integración) siguen bloqueadas por decisiones de producto/arquitectura (A-7, A-13, A-16, y la aclaración de A-1/A-2).

**Para quién:** para la raíz de composición y la operación. `cmd/api` ya puede arrancar con Keycloak real o con el doble en memoria según el entorno, con una señal de alcanzabilidad separada que no contamina `/ready`.

**Cambios principales:**

- **Telemetría (tarea 10):** `internal/idp/internal/kc/telemetry.go` con el histograma único `ai27_idp_duracion_segundos` (unidad `s`, atributos `dependencia=keycloak`, `operacion`, `resultado` y `causa` sólo en `proveedor_no_disponible`) y el span cliente `idp.keycloak.<operacion>`. Si `MeterProvider` es nil se usa el global (H6); un error al crear instrumentos degrada a no-op (R10.6). `Issue`/`Refresh`/`Revoke` registran una sola medición por llamada con la categoría devuelta; la cancelación cae en `resultado=otro`, nunca en `proveedor_no_disponible`. Property 9 y 11 y la ampliación de Property 3 (sin secretos en métricas ni spans) con `sdkmetric.NewManualReader` y `tracetest.SpanRecorder`.

- **Cableado (tarea 12):** `cmd/api/config.go` con `cargarConfig(lookup, readFile)` inyectable: selección por `IDENTIDAD_IDP` (`memory` por omisión, `keycloak`, u otro valor = error sin repetir el valor), config de Keycloak desde `IDENTIDAD_KEYCLOAK_*`, secretos `_FILE` o directos (ambos = error), `REFRESH_URL` por omisión igual a `TOKEN_URL`, `TIMEOUT` con `time.ParseDuration` sin default, y todas las faltantes en un solo error sin valores. `main.go` resuelve el IdP antes del pool, lo inyecta en `Deps.IDP`, loguea el proveedor y monta `/ready/idp` en un registro aparte (A-12 opción 1). Property 10 y unit tests de `_FILE`, log y `/ready` vs `/ready/idp`.

- **Documentación (tarea 17):** `internal/idp/internal/doc.go` y el comentario de paquete de `internal/idp/port.go` describen la fachada `keycloak` y el árbol `kc`, sin "EN ESPERA de ADR 32" ni Cognito (P-0: "Keycloak, confirmado 2026-10-08, pendiente de asentar en decision-log"). README documenta `IDENTIDAD_IDP`, `IDENTIDAD_KEYCLOAK_*`, `/ready/idp` y cómo correr `-tags integration`. REPORTE-BRECHAS §3.1 pasa a "parcialmente implementado" con los pendientes A-7, A-13, A-16 y la aclaración de A-1/A-2.

**Verificación:**
- ✓ `go build ./...`
- ✓ `go vet ./...`
- ✓ `go test ./...` — todo en verde (incluye las nuevas pruebas de telemetría y de `cmd/api`)

**Alcance / pendiente:** tareas 14 (realm) y 15 (integración) bloqueadas por A-7, A-13, A-16 y la aclaración de A-1 (dos realms) y A-2 (segundo factor por correo, no nativo en Keycloak; reabre DEC-031). `SessionResolver` en el código es `func(string) (string, error)`; el diseño lo describe con `ctx` — se conservó la firma ya implementada y probada.

**Archivos modificados:**
- `internal/idp/internal/kc/telemetry.go` (nuevo)
- `internal/idp/internal/kc/telemetry_test.go` (nuevo)
- `internal/idp/internal/kc/adapter.go` (telemetría real en New, Issue, Refresh, Revoke)
- `cmd/api/config.go` (nuevo)
- `cmd/api/config_test.go` (nuevo)
- `cmd/api/config_prop_test.go` (nuevo)
- `cmd/api/ready_test.go` (nuevo)
- `cmd/api/main.go` (resolución y cableado del IdP, `/ready/idp`)
- `internal/idp/internal/doc.go`
- `internal/idp/port.go` (comentario de paquete)
- `README.md`
- `REPORTE-BRECHAS.md`
- `go.mod`, `go.sum` (otel/sdk/metric como dependencia directa de prueba)

---

## 2026:10:08 11:30

**Resumen:** Se implementa el modelo de autorización de AI27 (E4-T2): capas pura (composición de claims), persistencia (repositorios sobre Postgres) e integración (tests dinámicos con testcontainers). Decisiones A/B sobre unicidad confirmadas (validación en código, no constraints en BD). Código compilable, probado, cero regresiones.

**Para quién:** para E4-T6 (gobierno) y ADR 32 (IdP real). El modelo está listo para recibir eventos de autorización y cambios de sesión. Composición de claims es determinista y sin alcance (alcance pendiente DEC-045).

**Cambios principales:**

- **Ola pura:** Función `Componer(ModeloVigente) Compuestos` determinista (sin alcance). 6 property tests (P1, P2, P3, P5) + 3 unit tests, ≥128 iteraciones c/u. Mapeos puros `AudienciaDeRol()`, `DimensionDeRol()` total sobre enum de 8 roles.

- **Ola persistencia:** Tipos `Sujeto`, `ModeloVigente`, `AsignacionRol` en `internal/autorizacion/model.go`. Puertos `SujetoRepo`, `AsignacionRolRepo`, `CargadorModeloVigente`. Implementación sobre Postgres en `internal/autorizacion/postgres/` con validación de unicidad en código (Decisión B confirmada).

- **Ola integración:** 3 tests dinámicos con testcontainers (build tag: `integration`): unicidad en código (P4), rechazo rol inválido (P7), aislamiento esquema DB (P6). Manejo graceful de Docker no disponible (skipea en lugar de fallar).

- **Decisiones confirmadas:** A: No agregar `UNIQUE(tenant_id, id)` en `sujeto` (criterio 1.3 cubierto); B: Validación `(sujeto_id, rol_codigo)` en código, no BD (constraint final será `UNIQUE(..., alcance_id)` con DEC-045). Documentado en `NOTAS.md`.

- **Documentación:** Specs actualizadas (tasks, design). Reportes de brechas: composición claims ✅ resuelta, IdP real aún en espera ADR 32. Artefactos nuevos (IMPLEMENTACION-T6-T7.md, IMPLEMENTACION-TESTS-INTEGRACION.md).

**Verificación:**
- ✓ `go fmt` limpio (todos archivos formateados)
- ✓ `go vet ./...` sin errores
- ✓ `go build ./internal/{autorizacion,claims,idp}/...` compilación limpia
- ✓ `go test ./internal/autorizacion ./internal/claims` — 32 tests OK (6 property, 3 unit, 23 helpers)
- ✓ `go test -tags=integration ./internal/autorizacion/postgres/...` — 3 tests integración (skippean si no Docker, ejecutan si Docker)
- ✓ 0 regresiones en suite existente

**Alcance / brecha reportada:** Alcance por unidad/cliente (DEC-045) sigue pendiente. Campo `Dimension` existe; constraint final será `UNIQUE(..., alcance_id)` cuando se resuelva. Adaptador real IdP (ADR 32) aún en espera. Rutas de gobierno (`/sujetos`, `/roles`) dependen de contrato E4-T6.

**Archivos modificados:**
- `internal/autorizacion/model.go` (nuevo)
- `internal/autorizacion/port.go` (extensión)
- `internal/autorizacion/port_test.go` (nuevo)
- `internal/autorizacion/postgres/sujeto.go` (nuevo)
- `internal/autorizacion/postgres/sujeto_test.go` (nuevo, stubs)
- `internal/autorizacion/postgres/asignacion_rol.go` (nuevo)
- `internal/autorizacion/postgres/asignacion_rol_test.go` (nuevo, stubs)
- `internal/autorizacion/postgres/cargador.go` (nuevo)
- `internal/autorizacion/postgres/cargador_test.go` (nuevo, stubs)
- `internal/autorizacion/postgres/integration_test.go` (nuevo, 3 tests)
- `internal/autorizacion/postgres/NOTAS.md` (nuevo)
- `internal/claims/claims.go` (consolidación)
- `internal/claims/claims_test.go` (extensión)
- `.kiro/specs/e4-t2-modelo-autorizacion-claims/tasks.md` (todas tareas ✅)
- `.kiro/specs/e4-t2-modelo-autorizacion-claims/design.md` (actualización)
- `.kiro/specs/e4-t2-modelo-autorizacion-claims/IMPLEMENTACION-T6-T7.md` (nuevo)
- `.kiro/specs/e4-t2-modelo-autorizacion-claims/IMPLEMENTACION-TESTS-INTEGRACION.md` (nuevo)
- `REPORTE-BRECHAS.md` (actualización)
- `go.mod` (testcontainers v0.44.0)
- `go.sum` (regenerado)

---

## 2026:10:07 16:38

**Resumen:** Se reemplaza el `ConsultasAPI` stub por una fachada EN PROCESO tipada, para que `pf-svc-core` pueda cablear los adaptadores de las dos aristas entrantes de identidad con paridad I3. El puerto consumidor que faltaba ya existe (bff lo declaró en el prompt 05); esta fachada lo satisface. No cambia la lógica del servicio de sesión, que sigue siendo scaffold.

**Para quién:** para `core`, que compone identidad. Antes `Consultas()` era `struct { sesion sesion.Servicio }` sin métodos públicos y no había de dónde colgar los adaptadores en proceso. Ahora `core` envuelve `Consultas().Sesion()` para la arista `bff → identidad` y `Consultas().Revocacion()` para la restringida `operacion/catalogos/reportes → identidad`.

**Cambios:**
- `consultas.go` (nuevo): `ConsultasAPI` con dos sub-fachadas. `Sesion()` expone los cinco métodos tipados con el contrato (`EmitirSesion`, `RefrescarSesion`, `RevocarSesion`, `ListarSesionesRevocadas`, `ObtenerSesion`), con la forma exacta de `bff.IdentidadPort`. `Revocacion()` expone solo las dos operaciones primitivas de la arista restringida (`ListarRevocadas []string`, `ObtenerSesion bool`), con la forma de los `RevocacionPort` de operacion/catalogos/reportes (DEC-051/058). Se separan en dos porque `ObtenerSesion` tiene firma distinta en cada arista y porque los consumidores de revocación no deben ver la superficie de sesión.
- `consultas.go`: mapeo dominio→contrato (`aSesionEmitida`, `aEstadoSesion`, `aPaginaRevocaciones`, `aUUID`), análogo al de `internal/http` pero propio de la fachada en proceso (cada frontera traduce lo suyo, sin acoplar la fachada al transporte).
- `identidad.go`: `Consultas()` ahora devuelve la fachada real; se retira el `ConsultasAPI` stub y su comentario de "cuando bff fije el puerto" (ya está fijado).
- `consultas_test.go` (nuevo): pruebas de conformación de superficie. Replican la firma de `bff.IdentidadPort` y del `RevocacionPort` (identidad es hoja: no puede importar esos paquetes) y afirman que cada sub-fachada las satisface. Verificado por mutación: cambiar una firma rompe el test.

**Alcance / brecha reportada:** la lógica del servicio de sesión sigue siendo scaffold (`internal/sesion/servicio.go`: `ObtenerSesion`/`ListarRevocadas`/`EmitirSesion`/`RefrescarSesion` devuelven "no implementado"). La fachada compila y delega con la forma correcta; en runtime propaga ese "no implementado" hasta que el servicio se rellene. Esta superficie cierra la costura de composición (paridad I3 en core), no entrega la lógica de sesión.

**Verificado:** gofmt limpio, `go vet ./...`, `go build ./...`, `go test ./...` (el paquete raíz pasó de 0 pruebas a 3 de conformación), build `-mod=readonly`, `go build ./cmd/api`, arch-test@v0.1.1 OK (identidad sigue siendo hoja: la fachada no agrega aristas salientes).

**Archivos modificados:**
- `consultas.go` (nuevo)
- `consultas_test.go` (nuevo)
- `identidad.go`

## 2026:10:07 00:00

**Resumen:** Se actualizó `pf-pkg-platform-go` a `v0.1.1` y se alineó el CI con la versión corregida de `arch-test`. La validación del módulo vuelve a pasar completa, incluyendo la comprobación de arquitectura para el import compartido de `api-client-go/roles`.

**Cambios:**
- Se actualizó `github.com/AI27-Tech/pf-pkg-platform-go` de `v0.1.0` a `v0.1.1` en `go.mod`.
- Se regeneró `go.sum` con los hashes correspondientes a `v0.1.1`.
- Se actualizó el paso `arch-test` del workflow a `@v0.1.1`.
- Se verificó `go mod verify`, `go build ./...`, `go vet ./...`, `go test ./...`, `go build ./cmd/api`, `arch-test` y `go mod tidy -diff`.

**Archivos modificados:**
- `go.mod`
- `go.sum`
- `.github/workflows/ci.yml`
- `CHANGELOG.md`
- `REPORTE-BRECHAS.md`

## 2026:10:06 17:45

**Resumen:** Scaffolding inicial del módulo `identidad` (`pf-svc-identidad`) según `03-identidad.md`: superficie pública, puerto del IdP aislado, modelo de autorización de los ocho roles, composición de claims, ciclo de vida de sesión con revocación en dos pasos, esquema de base de datos y arranque standalone. Solo estructura y costuras; sin lógica de negocio real.

**Cambios:**
- Se agregó `go.mod` fijando `pf-pkg-contracts/packages/api-client-go` `v0.1.0-tracer.3` y `pf-pkg-platform-go` `v0.1.0` por versión exacta, sin `replace`.
- Se agregó `identidad.go` con la superficie pública (`New`, `Routes`, `Workers`, `Consultas`) de `00-indice.md`; `New` no lee entorno. `ports.go` se dejó vacío (es hoja del grafo).
- Se agregó `internal/idp/` (puerto `IdentityProvider` aislado, doble en memoria, suite de contrato `idptest`, adaptador real EN ESPERA de ADR 32).
- Se agregó `internal/autorizacion/` (ocho roles reexportados del contrato, dos dimensiones de alcance resueltas en servidor, derivación de `Audiencia`, `TODO(DEC-045)` del vínculo Monitorista↔unidades).
- Se agregó `internal/claims/` (composición de claims como función pura, sin alcance; igual en emisión y refresco) con pruebas.
- Se agregó `internal/sesion/` (ciclo de vida y revocación en dos pasos: local+outbox en una transacción, luego IdP; Redis nunca es fuente de verdad).
- Se agregó `internal/gobierno/` (puerto vacío, `TODO(contrato)`: `/sujetos`, `/roles` y sus eventos no existen en el contrato).
- Se agregó `internal/http/` (handlers de las cinco operaciones de sesiones con los tipos generados).
- Se agregaron `migrations/identidad/0001_init.sql` (esquema `identidad`, rol `svc_identidad`, tablas `sujeto`, `rol`/`asignacion_rol`, `alcance_tenant`, `sesion` con el parcial `una_sesion_viva`, `cambio_identidad`, `outbox`) y `migrations/migrations.go` (`embed.FS`).
- Se agregó `cmd/api/main.go` (arranque standalone con `/health` y `/ready`, I5) y `adapters/http/README.md`.
- Se agregó `.github/workflows/ci.yml` (gofmt, build, vet, test, build de `cmd/api`, arch-test) y `README.md`.
- Se agregó `REPORTE-BRECHAS.md`: brechas de `/sujetos`, `/roles` y sus eventos; nombre `bearerCognito`; alcance fuera del token; y un **bloqueo del CI** por `arch-test`, que marca el import correcto de `api-client-go/roles` porque el tool no honra `shared_packages` (brecha de `pf-pkg-platform-go`, a resolver en `01-cimientos`).

**Archivos modificados:**
- `.gitignore`
- `go.mod`
- `identidad.go`
- `ports.go`
- `internal/idp/port.go`
- `internal/idp/memory/memory.go`
- `internal/idp/memory/memory_test.go`
- `internal/idp/idptest/idptest.go`
- `internal/idp/internal/doc.go`
- `internal/autorizacion/port.go`
- `internal/claims/claims.go`
- `internal/claims/claims_test.go`
- `internal/sesion/port.go`
- `internal/sesion/deps.go`
- `internal/sesion/servicio.go`
- `internal/gobierno/port.go`
- `internal/http/handlers.go`
- `migrations/identidad/0001_init.sql`
- `migrations/migrations.go`
- `cmd/api/main.go`
- `adapters/http/README.md`
- `.github/workflows/ci.yml`
- `README.md`
- `REPORTE-BRECHAS.md`

## 2026:10:06 12:55

**Resumen:** Se agregó la plantilla de Pull Request del backend, adaptada al stack Go de AI27.

**Cambios:**
- Se agregó `.github/pull_request_template.md` con secciones de invariantes de arquitectura (I1-I5, contrato primero), base de datos, eventos/bus, verificación (go build/vet/test, arch-test, gofmt), CHANGELOG y checklist de convenciones.

**Archivos modificados:**
- `.github/pull_request_template.md`

## 2026:10:06 12:48

**Resumen:** Se agregó la regla de steering de CHANGELOG y este archivo, antes de que el repo tenga código, para que la convención esté vigente desde el inicio.

**Cambios:**
- Se agregó `.kiro/steering/changelog.md` (`inclusion: always`): toda modificación del repo debe registrarse aquí, en español, con marca de tiempo, resumen, cambios y archivos modificados.
- Se creó `CHANGELOG.md`.

**Archivos modificados:**
- `.kiro/steering/changelog.md`
- `CHANGELOG.md`
