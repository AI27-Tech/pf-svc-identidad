# E4-T1 — Diseño de la extensión Java de Keycloak para `sesion_id` (Decisión C / DEC-083)

> Estado: **RATIFICADO por Arquitectura.** 2026-10-12. Diseño original 2026-10-09.
> Autor: Dev A (Angel), con copiloto. Fuente de la decisión: DEC-083 (DECIDIDA),
> ADR 26, ADR 32 §4, DEC-072, DEC-073, DEC-074.
>
> Arquitectura ratificó el mecanismo (§3) con una corrección de la premisa de §1
> y resolvió las cuatro preguntas de §10. Las resoluciones están incorporadas en
> el cuerpo de este documento y marcadas con **[Ratificado 2026-10-12]**.
>
> **Construido 2026-10-12:** el cambio de puerto de §6, el JAR de la extensión
> (`deploy/keycloak/extension/`), el executor en el realm, el permiso FGAP v2
> (§7) y el cableado del JAR en el arnés de integración. **Falta verificar en
> vivo:** compilar el JAR con Maven y correr la suite `-tags integration` contra
> Keycloak 26.7.4 para confirmar (a) que el note-mapper emite desde la nota del
> authenticator y (b) que el permiso FGAP v2 realmente termina sesiones. Esto
> corre en el job de CI `integration-keycloak`; no se pudo ejecutar en el entorno
> de desarrollo (sin Docker ni Maven).

## 1. Qué problema resuelve

Keycloak 26.7 no sabe meter en el token el `sesion_id` propio de AI27. El realm
ya declara el mapper `oidc-usersessionmodel-note-mapper` que emite el claim
`sesion_id` desde una **nota de sesión** llamada `SESION_ID`, pero **nadie
escribe esa nota**, así que el claim sale vacío. En rojo hoy, a propósito:

> **[Corrección 2026-10-12]** La revisión de Arquitectura reportó que el realm
> seguía mapeando `sesion_id` con `oidc-sessionid-mapper` (el `sid` nativo de
> Keycloak, que ADR 26 §1 rechaza). La rama ya tenía el swap al
> `oidc-usersessionmodel-note-mapper`; lo que faltaba —y es el objeto de este
> documento— es el **authenticator que escribe la nota**. El mapper correcto y
> la ausencia de la nota conviven: el claim sale vacío hasta que el JAR escriba
> `SESION_ID`.

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
                          4. ai27-sesion-id-note (REQUIRED, NUESTRO) ◄── el JAR
                               lee form param `sesion_id`
                               authSession → userSession note "SESION_ID"
                          │
                          ▼
                        token endpoint
                          note-mapper SESION_ID → claim `sesion_id`
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
       `context.getAuthenticationSession().setUserSessionNote("SESION_ID", valor)`.
    4. `context.success()`.
  - `requiresUser() = false`, `configuredFor(...) = true`, `setRequiredActions(...)` no-op.

- **`SesionIdNoteAuthenticatorFactory implements AuthenticatorFactory`**
  - `getId() = "ai27-sesion-id-note"` (el id que el realm referencia en el flujo).
  - `create(KeycloakSession) → new SesionIdNoteAuthenticator()` (singleton sin estado).
  - `getRequirementChoices() = { REQUIRED, DISABLED }`.
  - `isUserSetupAllowed() = false`, `isConfigurable() = false`.
  - Metadatos de nombre y ayuda en español.

> **[Ratificado 2026-10-12] Nombres (review punto 4).** El parámetro de
> formulario y el claim son `sesion_id` (español: es sustantivo de dominio). El
> id del proveedor del authenticator es `ai27-sesion-id-note` y la nota interna
> `SESION_ID` (convención del workspace: naming-conventions §4; es una
> constante). El id NATIVO de sesión de Keycloak va en inglés —
> `idp_session_id` / `IdpSessionID`— por no ser sustantivo de dominio, en línea
> con `IdpSubject` (DEC-072).

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

**[Ratificado 2026-10-12] Proceso del JAR (review punto 5).** El JAR se compila
**aparte** (paso de CI con Maven+JDK) y la suite Go consume el **prebuilt**: ruta
configurable, con `t.Skip` claro cuando el JAR no existe. La suite Go no toma
dependencia de JDK. El montaje en el arnés (`arranqueKeycloak`) sigue como en el
§5.2.

## 6. Cambios en el adaptador Go (fuera del JAR, pero parte de C)

Para que `identidad` mande el `sesion_id`, hay dos cambios de interfaz. **Tocan
el puerto, así que son los de más cuidado y conviene ratificarlos aparte.**

### 6.1 Pasar el `sesion_id` a `Issue` — **[Ratificado 2026-10-12: opción (b)] APLICADO**

Hoy `idp.Credentials` no tiene dónde llevar el `sesion_id`, y `Issue` no lo
manda. DEC-083 #3: "`Issue` recibe el `sesion_id` del llamador y lo transporta".

Arquitectura eligió **(b)**: la firma es `Issue(ctx, cred, sesionID string)`. El
razonamiento (review punto 2): el doc de `Credentials` dice "lo que el sujeto
presenta"; el `sesion_id` no lo presenta el sujeto, lo genera `identidad`, así
que no pertenece a `Credentials`. (`SegundoFactor` sí es credencial y se queda
donde está.) El ripple es acotado: puerto, adapter, memory, idptest, servicio;
no cambia comportamiento.

El cierre es **simétrico**: `Issue` devuelve el `sid` de Keycloak para que E4-T3
llene `idp_session_id`. El adaptador ya decodificaba `session_state`; ahora lo
expone como `idp.Token.IdpSessionID`.

Estado en la rama: aplicado. `issueForm` agrega `&sesion_id=...` cuando el valor
no está vacío; vacío ⇒ no se agrega y, contra el realm con el authenticator en
REQUIRED (falla cerrada), la emisión falla con credencial inválida.

### 6.2 `Revoke` recibe `idp_session_id` — **[Ratificado 2026-10-12] APLICADO**

DEC-083 #2/#3: `Revoke` recibe el id de sesión del IdP (`idp_session_id`), no el
`sesion_id` de AI27. `revoke.go` ya usaba el argumento como id de Keycloak
directo cuando `SessionResolver == nil`, que es justo este caso (verificado por
Arquitectura). Lo que cambió es el **nombre** del argumento a `idpSessionID` y su
documentación (R7.1, R7.5). La correlación `sesion_id ↔ idp_session_id` vive en
la tabla `sesion` de **E4-T3**, no aquí (reparto de DEC-083 #3).

`TestRevocarUnaSesion` ya no lee el claim `sesion_id` del token para revocar;
revoca por `tok.IdpSessionID` (el `session_state` que ahora expone el adaptador),
que es el id nativo del IdP. Con `SessionResolver` nil, `Revoke` lo usa directo.

## 7. El permiso de revocación (FGAP v2) — **modelado 2026-10-12, pendiente de verificación en vivo**

Independiente del JAR. La cuenta `ai27-identidad-svc` necesita el permiso mínimo
para terminar sesiones (DEC-073: FGAP v2 primero, Logout OIDC como alternativa;
**nunca** el rol amplio de gestión de usuarios). Se expresa en el **realm JSON**,
no en Java.

Lo modelado en `realm-ai27.json`:

- `adminPermissionsEnabled: true` a nivel de realm (habilita FGAP v2).
- Un cliente `admin-permissions` con `authorizationServicesEnabled` y un
  `authorizationSettings` que declara:
  - el recurso `Users` con el alcance `manage-members` (el que gobierna
    terminar sesiones de miembros, **distinto** del rol amplio de gestión);
  - una política de cliente que apunta sólo a `ai27-identidad-svc`;
  - un permiso de alcance que concede `manage-members` sobre `Users` aplicando
    esa política.

`TestRealmPermisoTerminarSesion` (prueba pura) fija esta intención: FGAP v2
habilitado, el permiso `manage-members` ligado a la cuenta de servicio, y que la
cadena `manage-users` **no aparezca** en el artefacto.

> **Riesgo a validar (A-7).** La forma exacta del bloque `authorizationSettings`
> en un import de un solo archivo sólo se confirma corriendo la suite
> `-tags integration` contra Keycloak 26.7.4. Si el import no materializa el
> permiso tal cual, `TestRevocarUnaSesion` lo delata con un mensaje claro (un
> `Revoke` que recibe 403) y se ajusta el bloque, o se cae a la alternativa de
> **Logout OIDC** (DEC-073) sin tocar el adaptador Go. Este bloque se escribió a
> partir del modelo documentado de FGAP v2, no de un export real; trátese como
> propuesta verificable, no como configuración confirmada.

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

## 10. Ratificación de Arquitectura — **resuelta 2026-10-12**

1. **Mecanismo del §3 (authenticator + nota + note-mapper).** Ratificado, con la
   corrección de la premisa de §1 (el swap del mapper ya estaba; faltaba el
   authenticator que escribe la nota).
2. **Nombres.** Parámetro de formulario y claim `sesion_id`; id del provider
   `ai27-sesion-id-note`; nota interna `SESION_ID`; id nativo del IdP
   `idp_session_id` / `IdpSessionID` (ver §4.1 ratificado).
3. **Interfaz del §6.1.** Opción (b): `Issue(ctx, cred, sesionID string)` y
   `Token.IdpSessionID`. Aplicada en la rama.
4. **Proceso del §5.2.** JAR compilado aparte; la suite Go consume el prebuilt
   con `t.Skip`.

**Pendientes que NO bloquean E4-T1** (review): DEC-084 (dos grupos de rol) y
DEC-085 (fallo al registrar la sesión). DEC-085 toca el orden `Issue` ↔ registro
en la tabla `sesion` de E4-T3 y debe cerrarse antes de cablear `Issue` en
producción. El Gate G2 es independiente (usa un emisor de prueba del lado del
verificador).

Trabajo restante de C: las dos clases Java + el registro SPI (nota `SESION_ID`,
provider `ai27-sesion-id-note`), el permiso FGAP v2 en el realm, montar el JAR
prebuilt en el arnés, y dejar en verde `TestPropiedadCoherenciaClaims`,
`TestRevocarUnaSesion` y el caso `revocar es idempotente` de `idptest`. Los
cambios de puerto del §6 (firma de `Issue`, `Token.IdpSessionID`, renombre en
`Revoke`) y el realm (note-mapper `SESION_ID` + executor `ai27-sesion-id-note`
REQUIRED) ya están aplicados.
