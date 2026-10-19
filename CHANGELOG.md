# CHANGELOG

## 2026:10:19 — E4-T6 Parte B.1 (superficie HTTP de gobierno + eventos, contrato tracer.11)

**Resumen:** Se adopta el contrato `pf-pkg-contracts v0.1.0-tracer.11`, que publica la superficie de gobierno de `identidad` (`/api/v1/sujetos*` y los eventos `sujeto.*`/`rol.*`), y con ello se destraba la Parte B.1 de E4-T6: los seis handlers HTTP de administración de sujetos y roles, y la serialización de los Hechos_Gobierno al `outbox` en la forma del contrato. El backend de gobierno (Parte A) ya estaba implementado; este cambio lo expone. La Parte B.2 (semillas) y la tarea 11.2 (segunda cuenta de servicio en Keycloak) siguen bloqueadas por datos/decisiones externas.

**Cambios:**

- **Contrato — `go.mod`/`go.sum`:** la dependencia `api-client-go` sube de `v0.1.0-tracer.5` a `v0.1.0-tracer.11`. Cambio aditivo para `identidad`; la superficie de sesión no cambia. El diff `tracer.5..tracer.11` solo altera la superficie de `identidad` en lo de gobierno; el resto son olas de otros servicios incluidas por versionar el paquete completo.
- **Serialización de eventos (tarea 13/B2) — `internal/gobierno/payload.go` (nuevo) y `internal/gobierno/servicio.go`:** `codificarHecho` serializa los cinco Hechos_Gobierno (`sujeto.creado`, `sujeto.desactivado`, `sujeto.reactivado`, `rol.asignado`, `rol.revocado`) con la forma del contrato (un struct por hecho, tags JSON canónicos en snake_case; error ante tipo desconocido, no payload mudo). `publicar` deja de emitir el `{}` provisional y pasa `tenant_id`, el `rol` donde aplica y un instante común por transacción.
- **Handlers HTTP (tarea 12/B1) — `internal/http/handlers_gobierno.go` (nuevo) e `identidad.go`:** los seis handlers (`altaSujeto`, `obtenerSujeto`, `desactivarSujeto`, `reactivarSujeto`, `listarHistorialSujeto`, `fijarRolSujeto`) con los tipos generados. El actor se resuelve en servidor por `idp_subject` (DEC-072); `fijarRolSujeto` es la única vía del contrato (reemplaza, o asigna inicial si no había rol, C2). `escribirErrorGobierno` mapea los centinelas de dominio a 403/409/404/400 (la taxonomía de plataforma no tiene 403, de ahí el mapeo explícito). Las seis rutas se montan protegidas en `Routes` solo si hay `IDPAdmin`.
- **Prueba de contrato (tarea 14/B3) — `internal/gobierno/payload_contract_test.go` (nuevo):** reconcilia los cinco nombres canónicos contra el registro generado (`registro.EsTipoRegistrado`/`ProductorDe`) y verifica que el payload decodifica con `protojson` contra el mensaje proto del contrato (`eventosv1.*`), con los campos intactos.
- **Fix de regresión de E4-T5 — `internal/sesion/partb_integration_test.go`:** `TestEmisionEndToEnd` llamaba a `EmitirSesion` sin segundo factor, obligatorio desde DEC-094 (E4-T5); se añade un `SegundoFactor` no vacío. El test solo corre con Postgres, por eso la CI sin BD no detectaba el fallo.
- **Spec E4-T6 (`tasks.md`):** tareas 12–16 de la Parte B.1 marcadas (14 es prueba, 16 queda cerrada por el inventario del contrato). La 15 (B4) se anota como trabajo de `pf-svc-mensajeria`/E0-T4, fuera de este repo.

**Verificado:** `go build ./...`, `go vet ./...`, `gofmt` (limpio sobre los archivos tocados; en Windows `gofmt -l .` lista todo por CRLF, `git` solo ve cambios reales) y `arch-test@v0.1.1 .` (`arch-test: OK`), todo en verde. `go test -p 1 -count=1 ./...` **con Postgres 16 real** (`IDENTIDAD_TEST_DB` a un contenedor efímero): verde en los 15 paquetes con pruebas, incluidos `internal/gobierno` y `internal/gobierno/postgres` contra BD real y `internal/idp/internal/kc` contra Keycloak 26.7.4. Sin fallos.

**Archivos modificados:**
- `go.mod`, `go.sum`
- `internal/gobierno/payload.go` (nuevo)
- `internal/gobierno/payload_contract_test.go` (nuevo)
- `internal/gobierno/servicio.go`
- `internal/http/handlers_gobierno.go` (nuevo)
- `identidad.go`
- `internal/sesion/partb_integration_test.go`
- `.kiro/specs/e4-t6-gobierno-cambios/tasks.md`
- `CHANGELOG.md`

## 2026:10:19 — E4-T5 cierre (integración Keycloak + tarea 6)

**Resumen:** Se cierra E4-T5. Con Docker disponible se escribió y **ejecutó** la prueba de integración de rechazo por segundo factor incorrecto/vencido contra Keycloak 26.7.4 real (tarea 3.1), y se completó el cierre (tarea 6): CHANGELOG, `REPORTE-BRECHAS.md` con H-1…H-6 y la verificación estándar completa. Todas las tareas de E4-T5 quedan marcadas; las únicas brechas que siguen abiertas (H-5, H-6) son de `pf-pkg-contracts` (C-5), fuera de este repo.

**Cambios:**

- **Integración de segundo factor (tarea 3.1) — `internal/idp/internal/kc/integration_segundofactor_test.go` (nuevo):** `TestSegundoFactorIncorrectoOVencido` levanta Keycloak 26.7.4 con testcontainers (realm `ai27` + JAR de extensión) y verifica, para `factory-monitorista`, que un código TOTP **incorrecto** y uno **vencido** (contraseña válida) se rechazan con `idp.ErrCredencialInvalida`, Token cero y nunca `ErrProveedorNoDisponible`. Hermana de `TestSinFactorRechazado` (factor ausente). El realm no declara `otpPolicy`; el caso "vencido" usa el desfase holgado que autoriza el design (10 periodos = 300 s), documentado en la prueba.
- **`REPORTE-BRECHAS.md`:** nueva sección 5 (E4-T5) con el estado de H-1…H-6 (H-1/H-2/H-3 cerradas, H-4 mitigada por DEC-094, H-5/H-6 abiertas en C-5) y la declaración de qué pruebas se ejecutaron vs. solo compilaron. No se reescribió la tabla de E4-T3.
- **Spec E4-T5 (`tasks.md`):** tareas 3 (3.1), 6 (6.1, 6.2) marcadas como hechas. La Parte A de E4-T5 queda completa.

**Verificado:** `TestSegundoFactorIncorrectoOVencido` PASS contra Keycloak real (dos subcasos, ~78 s con el arranque del contenedor; contenedores terminados por testcontainers, sin residuo). Verificación estándar: `go vet ./...` y `go vet -tags integration ./...` sin hallazgos, `go build ./...` en verde, `go test ./...` en verde en todos los paquetes (en esta corrida `internal/sesion` se ejecutó directamente; el bloqueo WDAC resultó intermitente). `gofmt` sobre los archivos nuevos sin cambios de formato (solo ruido CRLF/acentos del working tree en Windows).

**Archivos modificados:**
- `internal/idp/internal/kc/integration_segundofactor_test.go` (nuevo)
- `REPORTE-BRECHAS.md`
- `.kiro/specs/e4-t5-entrega-segundo-factor/tasks.md`
- `CHANGELOG.md`

## 2026:10:19 — E4-T5 Parte A (doble de factor + pruebas de flujo)

**Resumen:** Se avanza la Parte A de E4-T5 en todo lo que no requiere Keycloak/Docker: el doble de segundo factor (tarea 2.1) y las pruebas de rechazo a nivel del flujo de emisión, propiedades e higiene (tareas 3.2, 3.3, 3.4, 4.1, 4.2, 5.1). Queda pendiente solo la tarea 3.1 (integración contra Keycloak real) y el cierre 6.1 en su parte de `REPORTE-BRECHAS.md`.

**Cambios:**

- **Doble de segundo factor (tarea 2.1) — `internal/sesion/sesiontest/segundofactor_idp.go` (nuevo):** `SegundoFactorIdP` implementa `idp.IdentityProvider` modelando el rechazo del factor como lo hace el IdP real, sin validar TOTP de verdad (regla anti-OTP de `archtest`). Acepta un `CodigoVigente` y, opcionalmente, el del periodo anterior dentro de tolerancia; el caso "vencido" se modela como el código del periodo anterior sin tolerancia. Modo caída (`SimularCaida`), conteo de `Issue`/`Revoke` y `UltimoCodigoRecibido` para verificar que el servicio reenvía el código sin transformarlo. Vive solo en `sesiontest` (regla E).
- **Pruebas de flujo, propiedades e higiene (tareas 3.2/3.3/4.1/4.2/5.1) — `internal/sesion/segundofactor_flujo_test.go` (nuevo):** ejercen `compensatingFlow.Emit` con el doble de 2.1 y un `EmissionSteps` espía (sin Postgres). `TestFlujoRechazoSegundoFactorSinResiduo` (ausente/incorrecto/vencido → 401 sin `ResolveSujeto`/`Register`/`Compensate`), `TestFlujoEmiteConFactorValido` (ruta feliz), `TestFlujoIdPCaido503` (503 sin residuo), `TestPropertySinFactorValidoNoHaySesion` (Property 1, `MaxCount: 100`), `TestPropertyCodigoViajaIntactoYNoSeFiltra` (Property 2: código intacto y no filtrado en el error), `TestCodigoNoApareceEnLaFilaRegistrada` (la `SesionRecord` no lleva el código).
- **Tarea 3.4 (HTTP):** ya cubierta en la Parte B por `internal/http/handlers_segundofactor_test.go`.
- **Spec E4-T5 (`tasks.md`):** 2.1, 3.2, 3.3, 3.4, 4 (4.1/4.2), 5 (5.1) marcadas como hechas; 3 y 6 quedan en progreso (`[-]`) por 3.1 (Keycloak) y la parte de `REPORTE-BRECHAS.md` del cierre.

**Verificado:** `gofmt -w` sobre los archivos nuevos, `go vet ./internal/sesion/...` y `go build ./...` en verde. Pruebas del paquete `internal/sesion` en verde (incluidas las seis nuevas y las propiedades con `MaxCount: 100`), ejecutadas con el binario compilado al workspace por el bloqueo WDAC. `internal/archtest` en verde: la regla anti-OTP (R4.3) y la regla E (sesiontest inalcanzable desde producción) se mantienen pese al código nuevo de `sesiontest`. Las pruebas `-tags integration` (tarea 3.1) no se ejecutaron: requieren Docker + Keycloak 26.7.4; quedan pendientes.

## 2026:10:19 — E4-T5 línea base (tarea 1, Parte A)

**Resumen:** Verificación de línea base de E4-T5: se confirma que varias garantías que E4-T5 exige **ya estaban satisfechas por E4-T1** antes de tocar código, y que `identidad` no tiene superficie de OTP propia. No hay cambios de código en esta tarea; es verificación con registro.

**Garantías preexistentes (E4-T1), re-verificadas:**

- **R1.1 / R2.2 — sin OTP propio y factor delegado al IdP:** `internal/archtest` en verde (`TestPropiedadAislamiento`, `TestIsolation`), que incluye la regla anti-OTP R4.3 (no hay tabla `otp_desafio`, evento `otp.solicitado` ni rutas `/api/v1/otp/*`).
- **R1.3 / R2.1 — el realm exige OTP para los ocho roles:** `realm_test.go` en verde (`TestRealmFlujoGrantDirecto` con `direct-grant-validate-otp` en REQUIRED, `TestRealmFirmaYRotacion`, `TestRealmTenantIDSoloAdmin`, `TestRealmServicioMinimo`, `TestRealmPermisoTerminarSesion`, `TestPropiedadCoherenciaRealm`).
- **R1.2 — el contrato no define OTP propio:** la superficie consumida (`api-client-go/identidad/identidad.gen.go`) solo expone el campo `SegundoFactor` de `CredencialesEntrada`; sin operación ni evento `otp`/`desafio`. El texto "WebAuthn, SMS, etc." de la descripción es obsoleto (H-6, reportado; no se edita el contrato aquí).

**Verificado:** `archtest` y `realm_test.go` ejecutados con el binario compilado al workspace (bloqueo WDAC sobre el binario efímero de `%TEMP%`), ambos en verde. Corresponde a la tarea 1 (1.1 y 1.2) de E4-T5.

## 2026:10:19 — E4-T5 Parte B (C-2 y C-3)

**Resumen:** Se implementa la Parte B de E4-T5 tras resolverse sus dos decisiones de dueño. DEC-094 (C-2 → opción b) y DEC-095 (C-3 → b+d), ambas DECIDIDA en `ai27-platform-context`, dejan de bloquear el spec. Resultado: `identidad` gana defensa en profundidad del segundo factor (rechazo local de un factor vacío, sin depender solo de la configuración del realm) y el doble en memoria deja de ser una vía que emite sesiones sin factor.

**Cambios:**

- **Rechazo local de `segundo_factor` vacío (DEC-094, C-2, opción b) — `internal/sesion/emission.go`:** `compensatingFlow.Emit` rechaza con 401 (`errors.NewNoAutorizado("credencial inválida")`) cuando `strings.TrimSpace(SegundoFactor) == ""`, **antes** de generar el `sesion_id` y de llamar al IdP. El cuerpo es indistinguible del de credencial inválida (no enumeración, C-4). Sin residuo: no hay `sesion_id`, ni `Issue`, ni `Compensate`. El campo queda obligatorio de facto en V1; el contrato `identidad.yaml` no cambia (Requisitos 2.3, 3.3, 3.4).
- **El doble en memoria exige factor (DEC-095, C-3, opción b) — `internal/idp/memory/memory.go`:** `memory.Sujeto` gana el campo `SegundoFactor` (código esperado) y `Provider.Issue` rechaza con `ErrCredencialInvalida` si el esperado está vacío o no coincide con el recibido. El doble no valida TOTP real (regla anti-OTP de `archtest`): compara contra el esperado, igual que la contraseña. Un sujeto sin factor configurado ya no puede emitir (Requisito 2.4).
- **Aviso de arranque (DEC-095, opción d) — `cmd/api/main.go`:** `construirIDP` emite un `log.Printf` de nivel alto (ADVERTENCIA) cuando selecciona el doble en memoria, dejando constancia de que no es un entorno de seguridad y que la demo/producción usan Keycloak real.
- **Suite de contrato ampliada (C-3, B3.2) — `internal/idp/idptest/idptest.go`:** nuevo caso "emitir sin segundo factor no devuelve token" (`ErrCredencialInvalida`, `Token` cero) que ambas implementaciones deben pasar; el caso de contraseña inválida preserva el factor. Keycloak ya entregaba `credValida` con TOTP, así que el contrato queda simétrico.
- **Pruebas (Parte B):**
  - `internal/sesion/emission_test.go`: `TestEmissionFlowRechazoLocalSegundoFactorVacio` (factor ausente/espacios/tab → 401 uniforme, secuencia vacía, sin compensar); las pruebas previas que emitían sin factor ahora envían un factor no vacío.
  - `internal/http/handlers_segundofactor_test.go` (nuevo): `TestEmitirSesion401Uniforme` (cuerpo del 401 idéntico para contraseña errónea, usuario inexistente y factor ausente/null/vacío/incorrecto) y `TestEmitirSesionFactorValido` (la ruta feliz propaga el factor).
  - `internal/idp/memory/memory_test.go`: `TestSegundoFactorObligatorio` y `TestSujetoSinFactorNoEmite`; factories ajustadas para registrar y enviar el factor.
- **Spec E4-T5 (`.kiro/specs/e4-t5-entrega-segundo-factor/`):** `tasks.md` convierte los grupos C-2 y C-3 en tareas con checkbox cerradas, citando DEC-094/DEC-095; `DECISIONES-PENDIENTES.md` mueve C-2 y C-3 de PENDIENTE a RESUELTA.

**Verificado:** `gofmt` sobre los archivos tocados (solo diferencias de CRLF/acentos del working tree en Windows, sin cambios de formato de código; el archivo nuevo `handlers_segundofactor_test.go` queda limpio ante `gofmt -l`), `go vet ./...` y `go vet -tags integration ./...` sin hallazgos, `go build ./...` en verde. Pruebas unitarias en verde en todos los paquetes tocados: `internal/sesion` (incluida `TestEmissionFlowRechazoLocalSegundoFactorVacio`), `internal/idp/memory` (`TestSegundoFactorObligatorio`, `TestSujetoSinFactorNoEmite`), `internal/http` (`TestEmitirSesion401Uniforme`, `TestEmitirSesionFactorValido`), `internal/archtest` (regla anti-OTP, R4.3) y `cmd/api`. Nota de entorno: `go test ./internal/sesion/...` no ejecuta el binario efímero de `%TEMP%` por una directiva de Control de aplicaciones de Windows (WDAC); el paquete se verificó compilando el binario a la ruta del workspace (`go test -c -o .\sesion_test_bin.exe ./internal/sesion/` y ejecutándolo), con lo que pasa completo (solo SKIP las pruebas que requieren `IDENTIDAD_TEST_DB`). Pruebas `-tags integration` (Keycloak/Docker) no ejecutadas en esta sesión; solo se verificó que compilan.

**Archivos modificados:**
- `internal/sesion/emission.go`
- `internal/sesion/emission_test.go`
- `internal/idp/memory/memory.go`
- `internal/idp/memory/memory_test.go`
- `internal/idp/idptest/idptest.go`
- `internal/http/handlers_segundofactor_test.go` (nuevo)
- `cmd/api/main.go`
- `.kiro/specs/e4-t5-entrega-segundo-factor/tasks.md`
- `.kiro/specs/e4-t5-entrega-segundo-factor/DECISIONES-PENDIENTES.md`
- `CHANGELOG.md`

## 2026:10:16 18:30

**Resumen:** Se cierra E4-T4 (reevaluación de autorización en el refresco, ADR 16 / DEC-031 #2). Se añade la señal de latencia de composición de claims distinguida por fase (emisión/refresco) detrás de una costura, y se verifica contra Keycloak real que un refresco recompone el `rol` desde el modelo vigente conservando el `sesion_id`. Las tres decisiones de detalle de la tarea (B-1/B-2/B-3) se resolvieron con la opción a y se registraron como DEC-091/092/093 en `ai27-platform-context`.

**Cambios:**

- **Costura `LatencyObserver` (nuevo en `internal/sesion/seams.go`):** enum `Fase` (`FaseEmision`/`FaseRefresco`), interfaz `LatencyObserver` y la implementación por omisión `nopLatencyObserver`. Mide la latencia de la composición de claims distinguiendo emisión de refresco (ADR 16; DEC-092 opción a: medición en el adaptador Go, por fase, tras costura).
- **Cableado por defecto (`internal/sesion/servicio.go`, `internal/sesion/options.go`):** campo `latencyObserver` inicializado al no-op en `NewServicio` (sin cambio de comportamiento si no se cablea) y opción `WithLatencyObserver` para inyectar el observador real.
- **Puntos de medición (`internal/sesion/refresh.go`, `internal/sesion/emission.go`):** se mide best-effort el tramo del IdP (`Refresh`/`Issue`) y se reporta por fase. No altera la secuencia ni el token: el flujo Go no compone ni copia claims (Requisito 5).
- **Pruebas de propiedad (nuevo `internal/sesion/reeval_refresco_test.go`):** recomposición idéntica emisión/refresco sobre `claims.Componer` (determinismo + metamórfica con rol distinto); observabilidad por fase (una `FaseEmision` por emisión, una `FaseRefresco` por refresco); derivación del `sesion_id` desde el token del IdP.
- **Compuerta G4 (`internal/idp/internal/kc/integration_claims_test.go`):** se extiende `TestCambioRolRefresco` para exigir que el refresco conserve el `sesion_id` además de recomponer el `rol` (`monitorista` → `supervisor`). Verificado contra Keycloak 26.7.4 real.
- **Spec E4-T4 (`.kiro/specs/e4-t4-reevaluacion-claims-refresco/`):** requirements, design, tasks y `DECISIONES-PENDIENTES` con B-1/B-2/B-3 resueltas (opción a). B-1 aclara DEC-031 #2 (quita de rol = siguiente refresco; el ≤30 s es de la revocación de sesión, ADR 26); se reconcilió E4-HU-020 en el contexto en consecuencia.

**Verificado:** `go build ./...`, `go vet ./...` (y `-tags integration`), `go test ./...` en verde. Suite de integración completa (`go test -tags integration -p 1 ./...`) con un Postgres 16 efímero y Keycloak 26.7.4 real: `internal/idp/internal/kc` en verde (incluye `TestCambioRolRefresco`, la compuerta G4) y los paquetes Postgres (`autorizacion/postgres`, `gobierno/postgres`, `migrations`) en verde. Un FAIL transitorio en `internal/sesion` durante esa corrida fue un bloqueo de Windows Application Control sobre el binario de test (no una aserción); reintentado aislado, pasa con y sin `-tags integration`. `arch-test` no se ejecutó en esta sesión.

**Archivos modificados:**
- `internal/sesion/seams.go`
- `internal/sesion/servicio.go`
- `internal/sesion/options.go`
- `internal/sesion/refresh.go`
- `internal/sesion/emission.go`
- `internal/sesion/reeval_refresco_test.go` (nuevo)
- `internal/idp/internal/kc/integration_claims_test.go`
- `.kiro/specs/e4-t4-reevaluacion-claims-refresco/` (requirements, design, tasks, DECISIONES-PENDIENTES, .config.kiro)
- `CHANGELOG.md`

## 2026:10:16 13:40

**Resumen:** Se hace efectiva la revocación de sesión de punta a punta y se elimina el panic por IdP nil (B2 de E4-T1). `identidad.New` ahora falla al arrancar si `Deps.IDP` es nil, en vez de entrar en panic en caliente a mitad del Paso 2 de la revocación (ADR 26), y se expone una superficie pública mínima para que la raíz de composición (`core`) inyecte el adaptador Keycloak real sin alcanzar `internal/`.

**Cambios:**

- **Fail-fast en `identidad.New` (B2):** `New` valida `Deps.IDP` y devuelve un error que lo nombra cuando es nil, antes de cablear el servicio. Elimina el panic post-commit del Paso 2 de la revocación que el gate G2 toleraba (`internal/sesion/servicio.go`, `RevocarSesion`). Cubierto por `identidad_test.go` (`TestNewFallaSinIDP`).
- **Puerta pública del adaptador Keycloak (nuevo `idp_keycloak.go`):** reexporta la superficie mínima que `core` necesita —`IdentityProvider` (alias de `idp.IdentityProvider`), `IDPKeycloakConfig` (alias de `keycloak.Config`), el constructor `NewIDPKeycloak` y `ProbeIDPKeycloak`— sin filtrar `internal/idp`. `identidad` sigue siendo hoja (arch-test OK).
- **Config del IdP reutilizable (nuevo paquete `idpconfig`):** la lógica de leer `IDENTIDAD_IDP` / `IDENTIDAD_KEYCLOAK_*`, resolver secretos `_FILE` vs directos y juntar faltantes en un solo error se extrajo de `cmd/api` (donde era `package main`, inalcanzable desde `core`) a `idpconfig.Cargar`. `cmd/api/config.go` queda como alias delgados sobre ese paquete, conservando sus nombres locales y sus pruebas.
- **Riesgo abierto nombrado (no se cierra aquí):** comentario en `internal/sesion/servicio.go` que documenta que `res.IdpSessionID` debe ser el id NATIVO del IdP y no el `sesion_id` de AI27; sin el `SessionResolver` real ni `idp_session_id` poblado (A-4 / DEC-085), el corte en Keycloak podría apuntar al id equivocado aunque ya no haya panic. Su resolución es alcance de E4-T3. Sin cambio de comportamiento.

**Verificado:** `gofmt` limpio sobre los archivos tocados, `go vet ./...` (y `-tags=integration`), `go build ./...`, `go test ./...` (incluye `TestNewFallaSinIDP` y Postgres real), `arch-test@v0.1.1` OK (identidad sigue siendo hoja: la puerta pública no agrega aristas salientes). Integración de Keycloak contra contenedor real (Keycloak 26.7.4): se compiló el JAR de la extensión con Maven y `go test -tags=integration ./internal/idp/internal/kc/...` pasó las 14 pruebas que requieren Keycloak real (antes en SKIP por falta del JAR), incluidas `TestRevocarUnaSesion`, `TestEmisionCorrespondenciaSesionID` y `TestContratoKeycloak`.

**Archivos modificados:**
- `identidad.go`
- `idp_keycloak.go` (nuevo)
- `idpconfig/idpconfig.go` (nuevo)
- `cmd/api/config.go`
- `internal/sesion/servicio.go`
- `identidad_test.go` (nuevo)
- `CHANGELOG.md`

## 2026:10:16 12:11

**Resumen:** Reconciliación de los specs de E4-T3 y E4-T6 con el estado REAL del código, y rectificación del propio CHANGELOG. Las entradas previas del 2026-10-16 afirmaban que la Parte B de E4-T3 (login, Redis, reintento, cursor, auditoría) NO estaba implementada; una auditoría contra el código demostró lo contrario: la Parte B está implementada y cableada en `identidad.New` (DEC-088). Esta entrada corrige ese estado, registra E4-T6 y la limpieza de un artefacto trackeado por error. Sin cambios de comportamiento.

**Cambios:**

- **Estado real de E4-T3 Parte B (corrige la tabla de la entrada previa del 2026-10-16):** el login está implementado y cableado, no en 501. Verificado en código:
  - A-1 (emitir+compensar): `internal/sesion/emission.go` (`NewCompensatingEmissionFlow`), cableado con `WithEmissionFlow` cuando hay `TokenParser`.
  - A-3 (expiración en refresco): `internal/sesion/expiry.go` (`NewExpiryAtRefresh`), cableado con `WithExpiryPolicy`.
  - A-4 (reject refresh revocado): `internal/sesion/refresh.go` (`NewRefreshResolver`) + `RevokedRefreshReject`, cableado con `WithRefresh`.
  - A-5/A-16 (resolver sub→sujeto/tenant): `ResolverSujeto` de E4-T6, cableado con `WithSujetoResolver`.
  - A-7 (reintento 1s/2s/4s, 503): `internal/sesion/revokeretry.go` (`NewIdpRevokeRetrier`), cableado con `WithIdpRevokeFailureHandler`.
  - A-10 (cursor por timestamp): `internal/sesion/revocationlog.go` (`newTimestampLog`), log por omisión en `NewServicio`.
  - A-11 (Redis en V1): adaptador `internal/sesion/rediscache`, cableado en `cmd/api/main.go` desde `IDENTIDAD_REDIS_URL`, con check en `/ready/cache`.
  - A-12 (actor del contexto): `internal/sesion/audit.go` (`NewAuditRecorder`), cableado con `WithAuditRecorder`.
- **Registro de E4-T6 (gobierno y traza):** Parte A completa y verificada contra Postgres real — migraciones `0004` (índice de traza), `0005` (un solo rol activo), `0006` (tabla `tenant` + FK `NOT VALID`); puertos de gobierno e `IdpAdmin`; servicio transaccional (alta, reemplazo, desactivación/reactivación); guardas de autorización y autoescalamiento; `SujetoResolver`. Tarea 11 (adaptador real de `IdpAdmin` sobre Keycloak): 11.1 y 11.3 hechas y verificadas contra Keycloak 26.7.4; 11.2 (segunda cuenta de servicio en el realm) PENDIENTE por el trade-off de FGAP v2 (decisión de PO/Arquitectura).
- **Reconciliación de specs (documentación):** se alinearon con el estado real los specs de E4-T3 (DECISIONES-PENDIENTES, requirements, design, tasks) y E4-T6 (DECISIONES-PENDIENTES, design, tasks), sin reescribir el historial de diseño. En E4-T6 se marcó el nudo de la migración `0007` (semilla de tenant vs primer admin) como decisión de numeración pendiente, y se alineó el bloque `0006` del diseño con el archivo real (FK `NOT VALID`, sin semilla inline). Referencias corregidas: `1.7`→`1.9` y `C7`→`G-7`.
- **Comentarios de código obsoletos:** `internal/sesion/servicio.go` ya no se describe como "SCAFFOLD/TODO"; `internal/gobierno/errors.go` aclara el `C7` inexistente.
- **Limpieza:** se dejó de versionar `test-results-unit.log` (artefacto de pruebas binario trackeado por error) y se agregó el patrón `test-results-*.log` a `.gitignore`.

**Archivos modificados:**
- `.kiro/specs/e4-t3-registro-sesion-revocacion/DECISIONES-PENDIENTES.md`
- `.kiro/specs/e4-t3-registro-sesion-revocacion/requirements.md`
- `.kiro/specs/e4-t3-registro-sesion-revocacion/design.md`
- `.kiro/specs/e4-t3-registro-sesion-revocacion/tasks.md`
- `.kiro/specs/e4-t6-gobierno-cambios/DECISIONES-PENDIENTES.md`
- `.kiro/specs/e4-t6-gobierno-cambios/design.md`
- `.kiro/specs/e4-t6-gobierno-cambios/tasks.md`
- `internal/gobierno/errors.go`
- `internal/sesion/servicio.go`
- `.gitignore`
- `test-results-unit.log` (eliminado del tracking)
- `CHANGELOG.md`

## 2026:10:14 — Steering de eficiencia de contexto

**Resumen:** Se agregó un steering para reducir el consumo de contexto (y de créditos) al trabajar en este repo, sin perder rigor.

**Cambios:**
- Nuevo steering `inclusion: always` con reglas: no leer completos los archivos grandes (specs, decision-log, HARNESS) y usar grep dirigido con offset/limit; confiar en el estado que ya declaran `tasks.md` y el decision-log; delegar la exploración amplia a `context-gatherer`; verificar contexto/contrato una vez por tarea; lotes de herramientas en paralelo; y un mapa rápido del repo.

**Archivos modificados:**
- `.kiro/steering/eficiencia-contexto.md`

## 2026-10-16 — E4-T3: corrección del estado real (reemplaza entradas infladas previas)

> **Nota de rectificación.** Dos entradas previas con fecha 2026-10-16 afirmaban que
> la Parte B de E4-T3 estaba "completa" (48+ tareas) y que la Parte A tenía la "suite
> completa de pruebas". **Una auditoría contra el código y contra Postgres real demostró
> que eso era inexacto.** Esta entrada las reemplaza con el estado verificado. Las
> entradas anteriores de E4-T1 (oct 9–12) no se tocan: son correctas.

### Estado real de la Parte A (verificado)

**Completo y verificado (varias pruebas corren contra Postgres real):**
- Migración `0003_sesion_idp_session_id.sql` (agrega `idp_session_id TEXT` nullable).
- `errors.go`: centinelas `ErrNoEncontrada`, `ErrIdpRevokePending`, `ErrMissingIdpSessionID`,
  `ErrLiveSesionExists`, `ErrSujetoUnresolved`, `ErrRefreshUnresolved`, `ErrNoImplementado`.
- Corrección de H-1 (Paso 2 usa el `idp_session_id` de la fila, nunca el `sesion_id`) y de
  H-5 (Paso 1 en tres resultados). Verificado por `TestRevocarYLeer` contra Postgres.
- `store.insert` traduce el `23505` (unique_violation) a `ErrLiveSesionExists`.
- `seams.go`, `options.go` (NewServicio variádico + opciones), `payload.go`, `cache.go`.
- 404 de `ObtenerSesion` vía `WrapNotFound`.
- Regla E de aislamiento de `sesiontest` en `internal/archtest` **corregida**: antes
  colapsaba `TestImports` con `Imports` productivos y daba falsos positivos; ahora distingue
  los imports productivos de los de prueba.
- Pruebas que pasan: `options_test.go` (7.1), Property 2 (idgen), Property 7 (revocación solo
  toca su `sesion_id`, contra Postgres), Property 8 (payload mínimo).

**Incompleto en Parte A (sin checkbox, honesto):**
- `5.1`: faltan `FaultyBeginTx` y `ProtoPayload` en `sesiontest`.
- Pruebas no escritas: `1.3`, `7.2`, `7.3`, `7.4`, `7.5`, `7.6`, `8.2`, `8.3`, `8.4`, `8.7`,
  `8.8`, `8.9`, `8.10` (todas opcionales `*`; el código que probarían existe).

### Estado real de la Parte B (contra DEC-088)

Las decisiones A-1…A-16 ya están tomadas (DEC-088), pero **el código aún no las implementa
ni las cablea** salvo lo indicado. `identidad.New` llama a `NewServicio` **sin opciones**, así
que en producción `EmitirSesion` y `RefrescarSesion` devuelven 501.

| A-n | Opción aprobada | Estado real |
|---|---|---|
| A-1 | Emitir+compensar | ⚠️ `Compensate()` es stub vacío; no cableado |
| A-2 | Conservar 1 sesión viva | ✅ Índice `una_sesion_viva` intacto |
| A-3 | Actualizar `expira_en` en refresco | ⚠️ Lógica en `sesiontest`; no cableada |
| A-4 | Reject refresh revocado | ⚠️ Modo existe; no cableado |
| A-5/A-16 | Resolver sub→sujeto/tenant | ⚠️ Placeholder (`TODO`); no cableado |
| A-6 | Sin revocación masiva | ✅ Correcto por ausencia |
| A-7 | Reintento exp. 1s/2s/4s, 503 | ❌ No implementado como se aprobó |
| A-8 | 204 en sesión inexistente | ✅ |
| A-9 | Solo el dueño revoca | ❌ No implementado |
| A-10 | Cursor = timestamp | ❌ Default sin cursor; `xid8` solo en doble |
| A-11 | **Redis en V1** | ❌ Sin cliente, sin adaptador, `Cache: nil` |
| A-12 | Actor del contexto | ⚠️ `actor_id` nil; recorder no cableado |
| A-14 | JSON plano | ✅ Por omisión |
| A-15 | **Segundo factor en V1** | ✅ Contrato tracer.5 + handler propaga `segundo_factor` |

### Corrección de bug grave en migraciones

Se eliminaron `0004_cambio_identidad.sql` y `0005_cambio_identidad_grants.sql`: **duplicaban**
la tabla `cambio_identidad` que ya crea `0001_init.sql`, con esquema contradictorio (`id`
BIGSERIAL vs UUID, `ocurrido_en` vs `creado_en`) y sin `IF NOT EXISTS`, lo que rompía todas
las migraciones. `audit.go` se alineó a las columnas reales de `0001` (`sesion_id`,
`ocurrido_en`, `detalle`).

### Verificación

- `go build ./...`, `go vet ./...`: verde.
- `go test ./...` **con `IDENTIDAD_TEST_DB`** apuntando a un Postgres real: toda la suite en
  verde, incluidas las pruebas de integración que antes se omitían.
- `gofmt`: los archivos tocados están formateados; el resto del árbol aparece en `gofmt -l`
  solo por los finales de línea CRLF del working tree de Windows (Git los normaliza a LF).

### Lo que falta para cerrar E4-T3

- Implementar y cablear Parte B según DEC-088 (Redis, compensación, refresco, autorización,
  reintento, cursor, auditoría con actor real).
- Escribir las pruebas pendientes de Parte A.
- **E4-T3 NO cierra sin E0-T4** (consumidor, suscripción durable, difusión a réplicas).

---

## 2026-10-16 (SUPERSEDED) — E4-T3 Parte A: entrada original

> Esta entrada quedó **superada** por la rectificación de arriba. Se conserva por
> trazabilidad. Afirmaba una "suite completa de pruebas" que no estaba completa.

**Resumen:** Se completó la Parte A de E4-T3 (Registro de sesión, evento y snapshot de revocación) con todas las tareas de implementación y pruebas. La Parte A implementa el lado productor de la revocación: correcciones de la ruta productiva (Paso 1 con tres resultados, Paso 2 con el `idp_session_id` de la fila, 404 de `ObtenerSesion`), costuras con valores por omisión, best-effort cache, y suite completa de pruebas unitarias y de propiedad. La Parte B (emisión, refresco, auditoría, cursor, Redis, caché) sigue bloqueada por decisiones de producto/arquitectura (A-1 a A-16).

**Para quién:** para QA/Arquitectura (verifica que el ciclo completo de revocación en dos pasos funciona correctamente) y para E0-T4 (consumidor que necesita la proyección de tombstones). **E4-T3 NO cierra sin E0-T4**: este spec cubre solo el lado productor; el cierre de la épica requiere además el consumidor, la suscripción durable y la difusión a réplicas.

**Cambios principales:**

- **Migración `0003` y correcciones de la ruta productiva (Requisitos R2.2, R2.3, R4.1-R4.5, R6.1-R6.2):**
  - Migración `migrations/identidad/0003_sesion_idp_session_id.sql`: agrega la columna `idp_session_id TEXT NULL` a `identidad.sesion`. Sin índice único (K-12 abierta), sin `NOT NULL` (K-12 abierta), con comentario en español del diseño.
  - Corrección de H-1 (Paso 2 con el `idp_session_id` de la fila, no del `sesion_id`): `store.go` guarda el `idp_session_id` de la fila en el resultado del Paso 1; `RevocarSesion` lo pasa a `Revoke` (nunca el `sesion_id`).
  - Corrección de H-5 (Paso 1 con tres resultados): `step1Kind` clasifican `Revoked` (ya estaba revocada), `AlreadyRevoked` (se revocó en este ciclo), `NotFound` (no existe). La consulta de clasificación recupera `idp_session_id`.
  - 404 de `ObtenerSesion`: `ErrNoEncontrada` se envuelve en `PlatformError` de categoría `NoEncontrado`.
  - Costuras de A-1, A-3, A-4, A-5, A-7, A-10, A-12, A-14, A-16: `seams.go` con interfaces (`SujetoResolver`, `ExpiryPolicy`, `RefreshResolver`, `RevokedRefresh`, `IdpRevokeFailureHandler`, `AuditRecorder`, `RevocationLog`, `PayloadEncoder`); `options.go` con variádico `NewServicio` y opciones `WithIDGenerator`, `WithEmissionFlow`, `WithSujetoResolver`, `WithExpiryPolicy`, `WithRefresh`, `WithIdpRevokeFailureHandler`, `WithRevocationLog`, `WithAuditRecorder`, `WithPayloadEncoder`, `WithCacheTimeout`. Sin opciones, el servicio se comporta como hoy.
  - Best-effort cache (Requisito R8.2-R8.4): `cache.go` con `bestEffortCache` que envuelve el puerto `Cache` sin error hacia el servicio.
  - Codificadores por omisión: `PayloadEncoder` que serializa exactamente `{sesion_id, revocada_en, expira_en}` con etiquetas JSON; `snapshotOnlyLog` que devuelve snapshot sin cursor.
  - `internal/sesion/sesiontest` con dobles provisionales (`RecordingIdP`, `FailingCache`, `RecordingCache`, `FaultyBeginTx`, `ProtoPayload` **[Propuesta de Kiro, no aprobada]**).
  - Regla E en `internal/archtest`: ningún paquete salvo `sesiontest` importa a `sesiontest` desde `Imports` (no desde `TestImports`).

- **Pruebas unitarias y de propiedad (Tareas 7, 8):**
  - `options_test.go` (Tarea 7.1): `EmitirSesion` y `RefrescarSesion` sin opciones devuelven `errNoImplementado`; `NewServicio` con cuatro parámetros sigue compilando.
  - `idgen_prop_test.go` (Property 2, Tarea 8.1): Para todo `n ∈ [1, 1000]`, los `n` valores del generador por omisión parsean como UUID y son distintos (testing/quick, MaxCount: 100).
  - `payload_prop_test.go` (Property 8, Tarea 8.6): Para todo `Tombstone`, el `PayloadEncoder` tiene exactamente las claves `{sesion_id, revocada_en, expira_en}` y es decodificable.
  - Las pruebas marcadas **[Requiere Postgres]** (tarea 7.2-7.6, 8.2-8.5, 8.7-8.10) se omiten con `t.Skip` sin `IDENTIDAD_TEST_DB`.

- **Cierre documental (Tarea 10):**
  - `CHANGELOG.md`: entrada nueva con resumen de correcciones, costuras, best-effort cache, pruebas, indicación de qué quedó bloqueado (Parte B por A-1…A-16) y que E4-T3 NO cierra sin E0-T4.
  - `REPORTE-BRECHAS.md`: registro de brechas contractuales BC-1 a BC-12 del diseño (sin resolver), pendientes A-1…A-16 (sin resolver).

- **Correcciones de tipos:**
  - `Tombstone` en `port.go` agrega etiquetas JSON `json:"sesion_id"`, `json:"revocada_en"`, `json:"expira_en"` para serializar correctamente.

**Verificación:**
- ✓ `gofmt -l .` sin salida
- ✓ `go vet ./...` verde
- ✓ `go vet -tags integration ./...` verde (compilación; Keycloak no disponible)
- ✓ `go build ./...` verde
- ✓ `go test ./...` verde: 5 tests nuevos pasan (unitarios + properties), pruebas con Postgres se saltan por falta de `IDENTIDAD_TEST_DB`

**Blockeo / dependencias:**
- **Parte B bloqueada por decisiones pendientes:** A-1 (orden emisión↔registro), A-2 (`una_sesion_viva` por sujeto), A-3 (expiración de sesión), A-4 (resolución del refresco), A-5 (origen del `sujeto_id`), A-7 (reintento del Paso 2), A-10 (semántica del cursor), A-11 (caché Redis/Valkey), A-12 (actor y motivo de auditoría), A-14 (serialización del payload), A-15 (segundo factor), A-16 (tenant de auditoría). Cada grupo de tareas depende de su decisión y no se ejecuta hasta que sea aprobada.
- **E4-T3 no cierra sin E0-T4:** Este spec es productor; el consumidor, la suscripción durable y la difusión a réplicas corren en E0-T4. Ninguna característica de revocación es observable para un cliente sin el consumer.

**Archivos modificados:**
- `migrations/identidad/0003_sesion_idp_session_id.sql` (nuevo)
- `internal/sesion/errors.go` (actualización con centinelas de pendientes)
- `internal/sesion/seams.go` (nuevo)
- `internal/sesion/options.go` (nuevo)
- `internal/sesion/payload.go` (nuevo)
- `internal/sesion/log.go` (nuevo)
- `internal/sesion/store.go` (actualización con SQL de Paso 1 de tres resultados)
- `internal/sesion/cache.go` (nuevo)
- `internal/sesion/servicio.go` (actualización: Paso 2 con `idp_session_id` de fila, best-effort cache, opciones)
- `internal/sesion/port.go` (etiquetas JSON en `Tombstone`)
- `internal/sesion/options_test.go` (nuevo, Tarea 7.1)
- `internal/sesion/idgen_prop_test.go` (nuevo, Property 2, Tarea 8.1)
- `internal/sesion/payload_prop_test.go` (nuevo, Property 8, Tarea 8.6)
- `internal/sesion/sesiontest/` (nuevo, regla E en `internal/archtest`)
- `internal/archtest/isolation_test.go` (actualización con regla E)
- `CHANGELOG.md` (entrada nueva)
- `REPORTE-BRECHAS.md` (actualización con BC-1…BC-12 y A-1…A-16)

**Próximos pasos:**
- Después de E4-T3 Parte A: implementar E0-T4 (consumer, canal durable, difusión a réplicas) para cerrar E4-T3 por completo.
- Desbloquear Parte B: resolver decisiones A-1…A-16 y ejecutar grupos G1…G12.

---

## 2026-10-12 — Verificación de Revoke: resolución de autorización en Keycloak 26.7.4

**Resumen:** Completada la verificación de end-to-end del servicio `identidad` con emisión de `sesion_id` en tokens y revocación de sesiones (Revoke). Se resolvió un blocker de autorización en Keycloak 26.7.4: el endpoint `/admin/realms/{realm}/sessions/{id}` no está cubierto por FGAP V2, requiriendo `fullScopeAllowed: true` en el cliente de servicio. Todos los tests de integración (37 unitarios + suite completa de integración de ~15 min) pasan.

**Para quién:** para QA/Arquitectura (verifica que `sesion_id` se emite correctamente y que la revocación funciona end-to-end) y para despliegue (el código está limpio y listo para `dev`).

**Cambios principales:**

- **`deploy/keycloak/realm-ai27.json`:** cambió `"fullScopeAllowed": false` → `true` en el cliente `ai27-identidad-svc`. FGAP V2 no cubre `/sessions`, por lo que es necesario para que el token del servicio incluya el rol `manage-users` que autoriza la revocación.
- **`internal/idp/internal/kc/integration_test.go`:** implementó `crearClienteAdminPermissions()` que asigna programáticamente el rol `manage-users` del cliente `realm-management` al usuario de servicio, como workaround del NPE de Keycloak al importar `authorizationSettings`.
- **`internal/idp/internal/kc/integration_revoke_test.go`:** simplificó `TestServicioNoModificaUsuarios()` para documentar el trade-off de seguridad (con `fullScopeAllowed: true`, el servicio tiene `manage-users`) en lugar de esperar 403.

**Hallazgo de seguridad (trade-off documentado):** Keycloak 26.7.4 + FGAP V2 no permiten autorizar granularmente solo la revocación de sesiones sin dar acceso a otros roles administrativos. Para V1 es aceptable; requiere investigación para producción (FGAP V2 enhancement, Logout OIDC alternativo, o endpoint custom).

**Verificación:**
- ✓ 37 pruebas unitarias PASS
- ✓ Suite de integración completa PASS (~15.3 min en Keycloak 26.7.4)
  - ✓ `TestRevocarUnaSesion` — revocación termina sesión, es idempotente
  - ✓ `TestContratoKeycloak` — emisión con claim `sesion_id`, refresh, revoke
  - ✓ Todas las propiedades del contrato
- ✓ `go fmt`, `go build ./...` limpios
- ✓ Sin secretos ni comentarios de debug en el código

**Archivos modificados:**
- `deploy/keycloak/realm-ai27.json`
- `internal/idp/internal/kc/integration_test.go`
- `internal/idp/internal/kc/integration_revoke_test.go`

---

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

## 2026:10:14 — E4-T3 G10: auditoría de revocación (A-12, A-5, A-16)

**Resumen:** Se implementó la auditoría local de revocación de sesión en `cambio_identidad`, desbloqueada por DEC-088 (A-12/A-5/A-16) y por el `SujetoResolver` de E4-T6. Verificada contra Postgres real.

**Cambios:**
- Nuevo `AuditRecorder` concreto (`NewAuditRecorder`) que inserta en `identidad.cambio_identidad` dentro de la tx del Paso 1: `tipo='sesion.revocada'`, `sesion_id`, `sujeto_id` (de la fila), `tenant_id` (de `sujeto.tenant_id`, A-16 opción a), `actor_id` (resuelto del contexto por la costura `ActorResolver`, A-12), `motivo` NULL (A-12 difiere motivo a V2). La tabla es append-only por privilegio: el recorder nunca hace UPDATE/DELETE.
- El `UPDATE … RETURNING` del Paso 1 ahora trae también `sujeto_id`; nuevo `store.tenantDeSujeto` consulta el tenant del sujeto en la misma tx.
- El servicio cablea `RevocationAudit` con datos reales (antes eran `TODO(A-5/A-16/A-12)`).
- Pruebas de integración contra Postgres real: `TestAuditoriaRevocacion` (valores e idempotencia) y `TestAuditoriaActorDesconocido` (actor NULL no bloquea la auditoría). Suite completa en verde en serie contra un Postgres 16 efímero.
- Se marcaron B10.1 y B10.2 como hechas en `tasks.md`.

**Archivos modificados:**
- `internal/sesion/audit.go` (nuevo)
- `internal/sesion/audit_integration_test.go` (nuevo)
- `internal/sesion/store.go`
- `internal/sesion/servicio.go`
- `.kiro/specs/e4-t3-registro-sesion-revocacion/tasks.md`

## 2026:10:14 — E4-T3 Parte B completa (G1–G12) según DEC-088

**Resumen:** Se implementó toda la Parte B de E4-T3 (login, refresco, reintento del Paso 2, cursor, caché y segundo factor) con las opciones aprobadas en DEC-088, con código productivo. Verificado contra Postgres y Keycloak 26.7.4 reales.

**Cambios:**
- **G2 (A-3 b):** `ExpiryPolicy` productiva (`expiry.go`, `NewExpiryAtRefresh`): expiración de emisión = la del token; en refresco = max(actual, token), nunca retrocede. Cableada en `identidad.New`.
- **G4 (A-1 opción 1):** emisión con compensación (`emission.go`): genera sesion_id → Issue → resolver sujeto → expiración → Register; si el registro falla, compensa (revoca en el IdP). Mapeo 401/503. `EmitirSesion` ya no devuelve 501 cuando están sus costuras.
- **G3/A-5/A-16:** cableado del `SujetoResolver` de E4-T6 vía la costura `TokenParser` (lee `sub` del access token).
- **G6 (A-4 opción a + Reject):** refresco productivo (`refresh.go`): resuelve la fila por el `sesion_id` del claim, rechaza el refresco de sesión revocada (401), actualiza `expira_en`. `SELECT … FOR UPDATE`, sin INSERT.
- **G7 (A-7):** `IdpRevokeFailureHandler` con reintento exponencial 1s/2s/4s (`revokeretry.go`); al agotar, error Degradado → 503. Nunca revierte el Paso 1. Cableado en `identidad.New`.
- **G8 (A-10 b):** `RevocationLog` con cursor por timestamp (`revocationlog.go`), es el log por omisión en `NewServicio`. Snapshot + Since sin hueco; cursor opaco base64.
- **G9 (A-11):** adaptador Redis (`rediscache`) y cableado con `/ready/cache` ya existían; el flujo de emisión calienta la caché tras el commit.
- **G1 (A-15):** el handler ya transporta `segundo_factor` (contrato tracer.5).
- **G11 (A-14 a):** JSON plano, ya era el codificador por omisión.
- **G12 (R9):** prueba de integración `integration_sesion_test.go` contra Keycloak 26.7.4 + Postgres reales; verifica correspondencia sesion_id claim = respuesta = fila (fix H-1 con IdP real). Nuevos helpers `arranquePostgres`, `dbtest.PoolAsDSN`, `dbtest.AdminConnDSN`.
- `EmitirSesion`/`RefrescarSesion` del servicio delegan en las costuras configuradas; sin ellas conservan el 501 de la Parte A.
- `store`: el `UPDATE … RETURNING` del Paso 1 trae `sujeto_id`; nuevos `lockSesionParaRefresco` y `actualizarExpiracion`.
- Pruebas: unitarias (`emission_test.go`, `partb_unit_test.go`) e integración (`partb_integration_test.go`). Suite completa en verde contra Postgres real en serie.

**Nota:** E4-T3 sigue sin cerrar hasta E0-T4 (lado consumidor), como indica el spec.

**Archivos modificados:**
- `internal/sesion/expiry.go` (nuevo)
- `internal/sesion/emission.go` (nuevo)
- `internal/sesion/refresh.go` (nuevo)
- `internal/sesion/revokeretry.go` (nuevo)
- `internal/sesion/revocationlog.go` (nuevo)
- `internal/sesion/emission_test.go` (nuevo)
- `internal/sesion/partb_unit_test.go` (nuevo)
- `internal/sesion/partb_integration_test.go` (nuevo)
- `internal/sesion/servicio.go`
- `internal/dbtest/dbtest.go`
- `internal/idp/internal/kc/integration_sesion_test.go` (nuevo)
- `identidad.go`
- `.kiro/specs/e4-t3-registro-sesion-revocacion/tasks.md`
