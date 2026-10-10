# E4-T1 — Diseño de la extensión Java de Keycloak para `sesion_id` (Decisión C / DEC-083)

> Estado: **PROPUESTA, pendiente de ratificación de Arquitectura.** 2026-10-09.
> Autor: Dev A (Angel), con copiloto. Fuente de la decisión: DEC-083 (DECIDIDA),
> ADR 26, ADR 32 §4, DEC-072, DEC-073, DEC-074.
>
> Este documento NO construye nada todavía. Describe qué se va a construir, por
> qué, y cómo se verifica, para que Arquitectura (dueña de ADR 26 / ADR 32 §4 y
> DEC-072) lo ratifique antes de escribir el JAR. Nota 3 de DEC-083.

## 1. Qué problema resuelve

Keycloak 26.7 no sabe meter en el token el `sesion_id` propio de AI27. El realm
ya declara el mapper `oidc-usersessionmodel-note-mapper` que emite el claim
`sesion_id` desde una **nota de sesión** llamada `SESSION_ID`, pero **nadie
escribe esa nota**, así que el claim sale vacío. En rojo hoy, a propósito:

- `TestPropiedadCoherenciaClaims` — `sesion_id` vacío en emisión y refresco.
- `TestRevocarUnaSesion` — sin `sesion_id` no hay por dónde revocar.
- `idptest.go` (`revocar es idempotente`) — la cuenta de servicio no puede revocar.

DEC-083 decide el mecanismo: `identidad` **genera** el `sesion_id`; Keycloak solo
lo **transporta**. La pieza que lo transporta dentro de Keycloak es una extensión
Java empaquetada como JAR (la "extensión" de ADR 32 §4, no un script mapper, que
viene deshabilitado en KC 26).

## 2. Por qué un authenticator y no otra cosa

Repaso de lo descartado por DEC-083, para que la elección quede trazable:

| Mecanismo | Por qué no |
|---|---|
| Leer el `sid` nativo de Keycloak (C.1) | ADR 26 §1 rechaza el id del IdP como clave canónica; además tocaría `pf-pkg-platform-go` (DEC-072 #2) |
| Copiar `sid` → `sesion_id` con script mapper (C.3) | El script mapper viene deshabilitado por defecto en KC 26; exige `preview`/`scripts` |
| Post-procesar el token en el servicio (C.2 variante) | El `sesion_id` debe sobrevivir al refresco; una nota de sesión lo logra sin que el servicio reescriba cada token |

Un **authenticator** corre dentro del flujo `direct_grant`, tiene acceso a los
parámetros del request y a la sesión de usuario que se está creando, y puede
escribir una nota en esa sesión. Es el único punto donde el valor que manda
`identidad` queda guardado en un lugar que el note-mapper ya sabe leer y que
persiste a través de los refrescos.

## 3. Arquitectura de la solución (extremo a extremo)

```
identidad (Go)                     Keycloak (realm ai27)
──────────────                     ─────────────────────
Issue(cred, sesionID)
  │  genera sesionID (UUID)
  │  POST /token
  │    grant_type=password
  │    username, password, totp
  │    sesion_id=<UUID>  ◄───────┐ parámetro adicional del form
  ▼                              │
                        flujo ai27-direct-grant
                          1. validate-username   (REQUIRED, nativo)
                          2. validate-password   (REQUIRED, nativo)
                          3. validate-otp        (REQUIRED, nativo)
                          4. ai27-session-id-note (REQUIRED, NUESTRO) ◄── el JAR
                               lee form param `sesion_id`
                               authSession → userSession note "SESSION_ID"
                          │
                          ▼
                        token endpoint
                          note-mapper SESSION_ID → claim `sesion_id`
                          (ya existe en el realm)
  │                              │
  ◄──────────────────────────────┘ access_token con claim sesion_id = <UUID>
```

En el refresco no se vuelve a pasar el parámetro: la nota ya vive en la sesión de
usuario de Keycloak, así que el note-mapper la vuelve a emitir con el mismo valor.
Eso satisface R6.1 (claim presente) y la estabilidad del `sesion_id` que exige
ADR 26 §1.

## 4. El artefacto Java

### 4.1 Pieza principal: Authenticator

Dos clases, patrón estándar de SPI de Keycloak:

- **`SesionIdNoteAuthenticator implements Authenticator`**
  - `authenticate(AuthenticationFlowContext context)`:
    1. Lee el parámetro de formulario `sesion_id` del request
       (`context.getHttpRequest().getDecodedFormParameters().getFirst("sesion_id")`).
    2. **Falla cerrada** si falta o no es un UUID válido: `context.failure(...)`
       con `AuthenticationFlowError.INVALID_CREDENTIALS` (sugerencia de DEC-083
       nota 2; evita emitir un token sin `sesion_id`).
    3. Si es válido, lo guarda como nota de la sesión de autenticación que se
       promoverá a sesión de usuario:
       `context.getAuthenticationSession().setUserSessionNote("SESSION_ID", valor)`.
    4. `context.success()`.
  - `requiresUser() = false`, `configuredFor(...) = true`, `setRequiredActions(...)` no-op.

- **`SesionIdNoteAuthenticatorFactory implements AuthenticatorFactory`**
  - `getId() = "ai27-session-id-note"` (el id que el realm referencia en el flujo).
  - `create(KeycloakSession) → new SesionIdNoteAuthenticator()` (singleton sin estado).
  - `getRequirementChoices() = { REQUIRED, DISABLED }`.
  - `isUserSetupAllowed() = false`, `isConfigurable() = false`.
  - Metadatos de nombre y ayuda en español.

> **Decisión abierta a confirmar con Arquitectura (nombre del parámetro):**
> DEC-083 nota 2 sugiere `sesion_id`. Lo adopto por consistencia con el claim.
> Si Arquitectura prefiere otro nombre, es un cambio de una constante.

### 4.2 Registro SPI

Archivo de servicio en el JAR:
`META-INF/services/org.keycloak.authentication.AuthenticatorFactory`
con una línea: el nombre completo de la clase factory.

### 4.3 Dependencias y versión

- Solo dependencias `provided` de Keycloak: `keycloak-server-spi`,
  `keycloak-server-spi-private`, `keycloak-services`, en **26.7.4 exacta**
  (DEC-083 #5). Nada se empaqueta dentro del JAR salvo nuestras dos clases + el
  archivo de servicio.
- Build con Maven. El JAR resultante no lleva dependencias transitivas.

### 4.4 Fallback del rol (mismo JAR, solo si hace falta)

DEC-083 #4 nota 1(a): si en 26.7.4 el `oidc-usermodel-attribute-mapper` **no**
resolviera el atributo `rol` heredado del grupo, se agrega a este mismo JAR un
**protocol mapper** que derive `rol` del grupo de rol del usuario.

**La integración del 2026-10-09 ya confirmó que SÍ resuelve** (`TestCambioRolRefresco`
pasó). Por tanto este fallback **no es necesario hoy** y queda documentado como
contingencia, no como trabajo a construir.

## 5. Dónde vive el código y cómo se despliega

### 5.1 Ubicación en el repo

Propuesta (a confirmar con la convención de repos):

```
pf-svc-identidad/
  deploy/keycloak/
    realm-ai27.json              (ya existe)
    extension/                    (NUEVO — módulo Maven del JAR)
      pom.xml
      src/main/java/tech/ai27/keycloak/
        SesionIdNoteAuthenticator.java
        SesionIdNoteAuthenticatorFactory.java
      src/main/resources/META-INF/services/
        org.keycloak.authentication.AuthenticatorFactory
```

El JAR compilado se versiona como artefacto de despliegue (ADR 32 §4: "se
versiona y despliega un JAR de extensión de Keycloak con el servicio"). En
producción se coloca en `/opt/keycloak/providers/` y se corre `kc.sh build`.

### 5.2 En el contenedor de pruebas (testcontainers)

`arranqueKeycloak` (en `integration_test.go`) debe:

1. Montar el JAR en `/opt/keycloak/providers/ai27-keycloak-ext.jar`
   (otro `testcontainers.ContainerFile`, como ya hace con el realm y los usuarios).
2. El `start-dev --import-realm` detecta providers nuevos y hace el build en el
   arranque, así que no hace falta un paso extra de `kc.sh build`.

**Pregunta de proceso (para Arquitectura/DevEx):** ¿el JAR se compila como parte
de la suite Go (requiere Maven+JDK en el runner de CI), o se compila aparte y la
prueba consume un JAR versionado? Recomiendo lo segundo: la prueba Go consume un
JAR ya construido (ruta configurable, con `t.Skip` claro si no existe), y un paso
de Cize separado compila el JAR. Mantiene la suite Go sin dependencia de JDK.

## 6. Cambios en el adaptador Go (fuera del JAR, pero parte de C)

Para que `identidad` mande el `sesion_id`, hay dos cambios de interfaz. **Tocan
el puerto, así que son los de más cuidado y conviene ratificarlos aparte.**

### 6.1 Pasar el `sesion_id` a `Issue`

Hoy `idp.Credentials` no tiene dónde llevar el `sesion_id`, y `Issue` no lo
manda. DEC-083 #3: "`Issue` recibe el `sesion_id` del llamador y lo transporta".

Dos opciones, a decidir con el dueño del puerto:

- **(a)** Agregar un campo a `Credentials` (p. ej. `SesionID string`).
  `issueForm` lo agrega como `&sesion_id=...` si no está vacío.
  Simple; pero mezcla "credencial" con "identidad de sesión".
- **(b)** Cambiar la firma a `Issue(ctx, cred, sesionID string)`.
  Más limpio semánticamente; cambia la interfaz y el doble `memory` y la suite
  `idptest`.

Recomiendo **(a)** por impacto mínimo en la interfaz, con un comentario que
explique que el `sesion_id` lo genera el llamador (E4-T3), no el adaptador.

### 6.2 `Revoke` recibe `idp_session_id`

DEC-083 #2/#3: `Revoke` recibe el id de sesión del IdP (`idp_session_id`), no el
`sesion_id` de AI27. Hoy `revoke.go` usa el argumento como id de Keycloak directo
cuando `SessionResolver == nil`, que es justo este caso. **El código actual ya
hace lo correcto**; lo que cambia es el contrato/nombre (R7.1, R7.5) y la
documentación del argumento. La correlación `sesion_id ↔ idp_session_id` vive en
la tabla `sesion` de **E4-T3**, no aquí (reparto de DEC-083 #3).

> Implicación para las pruebas: `TestRevocarUnaSesion` hoy lee `sesion_id` del
> token y lo pasa a `Revoke`. Con DEC-083, lo correcto es revocar por el `sid`
> del IdP. Como en V1 `SessionResolver` es nil y la prueba fija el valor, el
> ajuste es acotado; se detalla al implementar, no en este diseño.

## 7. El permiso de revocación (FGAP v2) — parte de C, no es el JAR

Independiente del JAR. La cuenta `ai27-identidad-svc` necesita el permiso mínimo
para terminar sesiones (DEC-073: FGAP v2 primero, Logout OIDC como alternativa;
**nunca** `manage-users`). Esto se expresa en el **realm JSON**, no en Java:
una política de permisos de administración acotada a la operación de terminar
sesión. Se diseña y verifica junto con el JAR porque `TestRevocarUnaSesion` las
necesita a ambas, pero es un artefacto distinto (configuración del realm).

## 8. Cómo se verifica

Sin inventar pruebas nuevas: las que ya están en rojo son el criterio de salida.

| Prueba | Qué confirma | Verde cuando |
|---|---|---|
| `TestPropiedadCoherenciaClaims` | el token trae `sesion_id` no vacío en emisión y refresco | el authenticator escribe la nota y el note-mapper la emite |
| `TestRevocarUnaSesion` | revocar por id de sesión termina la sesión y es idempotente | authenticator + permiso FGAP v2 |
| `idptest.go` (`revocar es idempotente`) | `Revoke` no falla con "proveedor no disponible" | permiso FGAP v2 |
| `TestServicioNoModificaUsuarios` (ya verde) | el permiso NO incluye `manage-users` | sigue verde: el FGAP v2 es acotado |

Prueba adicional sugerida (DEC-083 nota 2, falla cerrada): un `Issue` **sin** el
parámetro `sesion_id` debe fallar con credencial inválida, no emitir un token sin
el claim. Se agrega al implementar.

## 9. Riesgos y supuestos a validar al construir

1. **El note-mapper emite desde una nota escrita por un authenticator** (DEC-083
   nota 1b). Es el supuesto central; si no se cumple, el authenticator tendría
   que escribir la nota en otro punto del ciclo (p. ej. como `userSessionNote` en
   un `postAuthenticate`). Se valida con `TestPropiedadCoherenciaClaims`.
2. **`start-dev --import-realm` construye el provider al arrancar.** Si no, el
   arnés hace `kc.sh build` antes de `start`. Riesgo de arranque, no de diseño.
3. **El flujo `direct_grant` admite un authenticator REQUIRED extra** sin romper
   el ROPC. Estándar en Keycloak; se verifica con cualquier `Issue` exitoso.
4. **DEC-084 (dos grupos de rol) y DEC-085 (fallo al registrar la sesión) siguen
   PENDIENTES** y no los resuelve este JAR. DEC-085 toca el orden de `Issue` vs
   el registro en la tabla `sesion` (E4-T3); conviene cerrarla antes de cablear
   `Issue` en producción, aunque no bloquea la prueba de integración de E4-T1.

## 10. Qué pido ratificar a Arquitectura

1. El mecanismo del §3 (authenticator + nota + mapper existente) como la forma de
   cumplir DEC-083 #1 — dueña de ADR 26 / ADR 32 §4.
2. El nombre del parámetro de formulario (`sesion_id`) y el id del provider
   (`ai27-session-id-note`).
3. La opción de interfaz del §6.1 (campo en `Credentials` vs. firma de `Issue`) —
   dueño del puerto.
4. El reparto de proceso del §5.2 (quién compila el JAR y cómo lo consume la
   suite Go).

Con eso ratificado, el trabajo restante de C es acotado: dos clases Java + el
registro SPI, el permiso FGAP v2 en el realm, el parámetro en `issueForm`, montar
el JAR en el arnés, y dejar las 3 pruebas en verde.
