# Requirements Document

**E4-T1 — Adaptador del IdP (Keycloak)** · Documento de requisitos

| | |
| --- | --- |
| **Estado** | En desarrollo — decisiones A-3, A-4, A-15 confirmadas el 2026-10-09 |
| **Historia técnica** | E4-T1 — Adaptador del IdP (`docs/historias-tecnicas/E4-identidad-y-acceso.md`) |
| **HU funcional** | E4-HU-017 — Autenticación y MFA (`docs/hu/E4-HU-017-autenticacion-y-mfa.md`) |
| **Épica** | E4 — Identidad y acceso |
| **Repositorio** | `repos/pf-svc-identidad` |
| **Fecha** | 2026-10-08 |

## Introduction

E4-T1 construye la **única pieza de `identidad` que conoce al proveedor de identidad**: un adaptador que implementa el puerto `internal/idp.IdentityProvider` contra Keycloak, para que el proveedor pueda sustituirse reimplementando una interfaz en vez de reescribir el servicio (ADR 15; DEC-062 ya lo puso a prueba al retirar Cognito).

`identidad` **no decide por petición**: el IdP es dueño de usuarios, credenciales, segundo factor, firma y emisión del token; `identidad` es dueño del modelo de autorización (E4-T2, ya implementado) y del ciclo de sesión observable (E4-T3). Los demás módulos verifican el token localmente contra el JWKS del IdP (E0-T3).

### Elección del IdP: Keycloak

> **Hecho de esta sesión, no registrado todavía como decisión.** El usuario confirmó el **2026-10-08** que el IdP de V1 es **Keycloak**. Las fuentes del workspace todavía dicen "propuesta ADR 32: Keycloak, o el IdP corporativo" (ADR 32 en *Propuesta*; DEC-031 y DEC-062 enmendadas el 2026-10-02; `identidad.md`; E4-T1; E0-T3; `internal/idp/internal/doc.go`; `REPORTE-BRECHAS.md` §3.1).
>
> **Pendiente de proceso (P-0):** asentar la elección en `decision-log.md` (nueva DEC o enmienda de DEC-062) y en ADR 32 §1 / Pendiente 1, y actualizar las fuentes que citan "propuesta". Este spec **no** cita la elección como si ya estuviera en el decision log.

### Estado actual del repositorio (verificado leyendo el código)

| Elemento | Estado |
| --- | --- |
| `internal/idp/port.go` | Puerto `IdentityProvider`: `Issue(ctx, Credentials{Usuario, Contrasena}) (Token, error)`, `Refresh(ctx, refreshToken) (Token, error)`, `Revoke(ctx, sesionID) error`. Excluye a propósito la administración de usuarios y grupos (`TODO(contrato)`). `Token` = `AccessToken`, `RefreshToken`, `ExpiraEn` |
| `internal/idp/memory/` | Doble en memoria; errores propios `ErrCredencialInvalida`, `ErrRefreshInvalido`; pasa `idptest` |
| `internal/idp/idptest/` | Suite de contrato: emitir válida, emitir inválida, refrescar, revocar idempotente. "El adaptador real deberá pasarla sin cambios" |
| `internal/idp/internal/doc.go` | Lugar reservado para el adaptador concreto; vacío, prohíbe SDK de AWS |
| `internal/claims.Componer` | Función pura implementada en E4-T2: `{SujetoID, TenantID, Rol, Audiencia}`, sin alcance. Su empaquetado como *mapper* del IdP quedó fuera del spec de E4-T2 ("EN ESPERA de ADR 32") |
| `internal/sesion/servicio.go` | Scaffold: `EmitirSesion`/`RefrescarSesion` llaman al IdP y devuelven `errNoImplementado`; la persistencia de `sesion` es E4-T3 |
| `internal/http/handlers.go` | Handlers de las cinco operaciones con tipos generados; responden 501 mientras sean scaffold |
| `cmd/api/main.go` | Cablea `memory.New()` como IdP; lee `IDENTIDAD_JWKS_URL`, `IDENTIDAD_AUDIENCIA`; usa `pf-pkg-platform-go/auth.NewVerifier` con refresco proactivo |
| `go.mod` | `testcontainers-go v0.44.0` presente (indirecto); ningún SDK de Keycloak ni cliente OIDC |
| Contrato `identidad.yaml` (`v0.1.0-tracer.2`) | `CredencialesEntrada {usuario, contrasena}` con `additionalProperties: false`; `SesionEmitida {sesion_id, access_token, refresh_token?, expira_en}`; `RefrescoEntrada {refresh_token}`; esquema de seguridad `bearerCognito` (desalineado, `REPORTE-BRECHAS.md` §2: no se renombra desde este servicio); descripciones que aún mencionan Cognito |

### Fuente no localizada

E4-T1 cita `.kiro/specs/ai27-microservices-architecture/requirements-identidad.md` (IDN-*, "Pendiente 4", "contradicción 1"). **Ese archivo no existe en el workspace** (búsqueda del 2026-10-08). Este spec referencia el Pendiente 4 tal como lo cita E4-T1, sin poder verificar su texto original ni los IDN-*.

## Glossary

- **IdP**: proveedor de identidad on-premise con OIDC. En este spec, **Keycloak** (elección confirmada por el usuario el 2026-10-08; ver P-0). Dueño de usuarios, credenciales, segundo factor, firma y emisión del token.
- **Puerto_IdP**: la interfaz `idp.IdentityProvider` de `internal/idp/port.go`.
- **Adaptador_Keycloak**: implementación de Puerto_IdP contra Keycloak, ubicada en `internal/idp/internal`.
- **Doble_En_Memoria**: implementación de Puerto_IdP en `internal/idp/memory`, para desarrollo standalone y pruebas.
- **Suite_Contrato_IdP**: la suite `internal/idp/idptest.Ejecutar`.
- **Realm_AI27**: el o los realms de Keycloak que sirven a AI27. Si es uno o dos (clientes e internos) está pendiente (A-1).
- **Mapper_De_Claims**: el artefacto que corre dentro de Keycloak (*protocol mapper* o extensión, ADR 32 §4) y agrega los claims de AI27 al token en emisión y refresco.
- **Claims_AI27**: los claims de dominio que el token debe llevar: identidad del `sujeto`, `tenant`, `rol`, `Audiencia` y `sesion_id` (DEC-053, DEC-054, ADR 26). Sin alcance.
- **Atributo_Tenant**: atributo del usuario en Keycloak que identifica su `tenant`.
- **Grupo_De_Rol**: grupo de Keycloak que representa uno de los ocho roles de DEC-053.
- **Roles_V1**: `cliente`, `jefe_operativo`, `monitorista`, `supervisor`, `coordinador`, `ejecutivo`, `reaccion`, `administrador` (DEC-053).
- **Audiencia**: claim de dominio (`Cliente` | `Interno`) derivado del rol, distinto del claim técnico `aud` (DEC-054).
- **sesion_id**: identificador de sesión propio de AI27, estable entre refrescos (ADR 26).
- **Raiz_De_Composicion**: `cmd/api/main.go`, único lugar que lee entorno y elige la implementación de Puerto_IdP.
- **Servicio_Sesion**: `internal/sesion`, consumidor de Puerto_IdP.
- **Keycloak_De_Prueba**: instancia efímera de Keycloak levantada por la prueba de integración (p. ej. testcontainers).
- **Host_Auth**: el nombre de host `auth.<dominio>` publicado a internet por el proxy de la DMZ (ADR 32 §2).
- **Lista_De_Bibliotecas_IdP**: rutas de módulo Go que cuentan como cliente de Keycloak u OIDC. Se fija en diseño; hoy `go.mod` no tiene ninguna.

## Requirements

### Requirement 1: Implementación del puerto y suite de contrato

**User Story:** Como equipo de desarrollo, quiero un Adaptador_Keycloak que implemente Puerto_IdP y pase la misma suite que el doble, para que el resto de `identidad` cambie de proveedor sin cambios.

#### Acceptance Criteria

1. THE Adaptador_Keycloak SHALL implementar la interfaz `idp.IdentityProvider` con las operaciones `Issue`, `Refresh` y `Revoke` y sus firmas actuales de `internal/idp/port.go`, sin agregar, quitar ni cambiar parámetros, tipos de retorno ni operaciones del puerto, de modo que `go build ./...` compile con el Adaptador_Keycloak asignado a una variable de tipo `idp.IdentityProvider`.
2. WHEN la Suite_Contrato_IdP (`idptest.Ejecutar`) se ejecuta bajo el build tag `integration` contra el Adaptador_Keycloak conectado a un Keycloak_De_Prueba, THE Suite_Contrato_IdP SHALL reportar como aprobados (ni fallidos ni omitidos) sus cuatro casos existentes: "emitir con credencial válida devuelve token", "emitir con credencial inválida no devuelve token", "refrescar con el refresh token emitido devuelve token nuevo" y "revocar es idempotente", sin modificar el nombre, el cuerpo ni las aserciones de esos casos ni la firma de `Factory`.
3. WHEN la Suite_Contrato_IdP se ejecuta con `go test ./...` (sin build tag `integration`) contra el Doble_En_Memoria después de los cambios de esta feature, THE Suite_Contrato_IdP SHALL reportar como aprobados los mismos casos que aprobaba antes de la feature, sin que ninguno pase a fallido ni a omitido.
4. WHEN `Issue` recibe una credencial válida y el segundo factor resuelto (ver A-3), THE Adaptador_Keycloak SHALL devolver `error` nulo y un `Token` con `AccessToken` no vacío, `ExpiraEn` distinto del valor cero, expresado en UTC y que difiera a lo más 1 segundo de la expiración del access token informada por Keycloak, y `RefreshToken` no vacío cuando Keycloak lo entregue.
5. WHEN `Refresh` recibe un refresh token vigente emitido por Keycloak para el Realm_AI27, THE Adaptador_Keycloak SHALL devolver `error` nulo y un `Token` nuevo obtenido de Keycloak en esa llamada, con `AccessToken` no vacío y `ExpiraEn` en UTC correspondiente a la expiración del nuevo access token, bajo la misma tolerancia de 1 segundo del criterio 4.
6. THE prueba del Adaptador_Keycloak SHALL aportar una `idptest.Factory` que, por cada invocación, devuelva un Adaptador_Keycloak configurado contra el Keycloak_De_Prueba y una `idp.Credentials` de un usuario de prueba ya existente en la configuración versionada del Realm_AI27 (Requisito 5.7), sin dar de alta usuarios por medio del adaptador (ver A-5).
7. THE configuración versionada del Realm_AI27 que usa la prueba de integración SHALL hacer que Keycloak entregue refresh token en la emisión del usuario de prueba de la Factory, de modo que el caso "refrescar" de la Suite_Contrato_IdP se ejecute y no termine omitido por `t.Skip`.

> **RESUELTA (2026-10-09, A-3):** Decisión: factor de dos pasos (OTP). `Credentials` se amplía con campo `SegundoFactor` en Tarea 1.1. El caso "emitir con credencial válida" de la suite se amplía con parámetro de factor; los cuatro casos originales se mantienen intactos. El usuario de prueba de la Factory llevará OTP configurado en Keycloak.
>
> **RESUELTA (2026-10-09, A-4):** Decisión: función `SessionResolver` inyectable en `Config`. Si es nil, asume identidad (sesionID_ai27 == kcSessionID). E4-T3 implementará el resolver concreto. El caso "revocar es idempotente" se prueba con sesiones conocidas de Keycloak.
>
> Propuesta, pendiente de validación: la tolerancia de 1 segundo de los criterios 4 y 5 no viene de una fuente. Se propone porque la expiración que informa Keycloak se expresa en segundos y se calcula en el momento en que se recibe la respuesta.

### Requirement 2: Aislamiento del proveedor

**User Story:** Como equipo de desarrollo, quiero que solo el adaptador conozca a Keycloak, para que sustituir el proveedor sea reimplementar un paquete (ADR 15; `identidad.md` (d), (l); §A.5 R2).

#### Acceptance Criteria

1. THE módulo `pf-svc-identidad` SHALL concentrar dentro del árbol `internal/idp/internal` todas las importaciones directas de bibliotecas cliente de Keycloak y de clientes OIDC, y todo código que construya peticiones hacia endpoints de Keycloak (token, refresco, administración), contando todos los archivos `.go` del módulo, incluidos los `_test.go` y los que tienen el build tag `integration`.
2. IF un archivo `.go` de un paquete fuera del árbol `internal/idp/internal` importa directamente un paquete de la Lista_De_Bibliotecas_IdP, THEN THE verificación automática de arquitectura del repositorio SHALL fallar el job de CI e indicar en su salida el paquete infractor y la ruta importada.
3. IF un archivo `.go` de cualquier paquete del módulo importa directamente un paquete del SDK de AWS para Go (versión 1 o versión 2), THEN THE verificación automática de arquitectura del repositorio SHALL fallar el job de CI e indicar en su salida el paquete infractor y la ruta importada (DEC-062).
4. THE Adaptador_Keycloak SHALL exponer hacia el resto del módulo únicamente un constructor que reciba su configuración ya resuelta y devuelva un valor que satisfaga `idp.IdentityProvider`, de modo que ninguna firma exportada del adaptador contenga tipos de la Lista_De_Bibliotecas_IdP y los únicos tipos de dominio que crucen la frontera sean los de `internal/idp` (`Credentials`, `Token` y errores del puerto).
5. THE Adaptador_Keycloak SHALL obtener su configuración exclusivamente de los parámetros del constructor, sin invocar ninguna función de lectura del entorno del proceso (`os.Getenv`, `os.LookupEnv`, `os.Environ`) ni leer archivos de configuración por su cuenta; la lectura de entorno es exclusiva de la Raiz_De_Composicion.
6. IF el constructor del Adaptador_Keycloak recibe una configuración a la que le falta un parámetro obligatorio, THEN THE Adaptador_Keycloak SHALL devolver un error que nombre el parámetro faltante sin incluir el valor de ningún parámetro, no devolver un adaptador utilizable y no hacer ninguna llamada de red a Keycloak.
7. WHEN se ejecuta el job de CI sobre el estado del repositorio que entrega E4-T1, THE verificación automática de arquitectura del repositorio SHALL aprobar los criterios 2 y 3, y SHALL conservar la verificación del grafo de ADR 17 que hoy ejecuta `arch-test@v0.1.1` (`identidad` sigue como hoja).

> Pendiente (verificado leyendo `pf-pkg-platform-go/tools/arch-test/main.go`): `arch-test@v0.1.1` solo verifica el grafo entre servicios (reglas (a)–(e): `go.mod` entre `pf-svc-*`, clientes generados de aristas declaradas, ciclos y `pf-svc-core`). **No detecta importaciones de terceros**, así que hoy no cubre los criterios 2 y 3. Falta decidir dónde vive esa verificación: (a) extender `arch-test` en `pf-pkg-platform-go`, publicar una versión nueva y subir el pin del CI; o (b) una verificación local en este repositorio, ejecutada por el CI junto a `arch-test`. Es una decisión técnica pendiente; este spec no la toma.
>
> Pendiente: la Lista_De_Bibliotecas_IdP (las rutas de módulo concretas que cuentan como "cliente de Keycloak u OIDC") depende de la biblioteca que se elija en diseño. Hoy `go.mod` no tiene ninguna. La lista se fija en diseño y se agrega al Glossary.
>
> Nota para diseño: por la regla de visibilidad `internal` de Go, `internal/idp/internal` solo se puede importar desde el árbol `internal/idp/...`, **no** desde `cmd/api` (Raiz_De_Composicion). Para que el criterio 4 se cumpla, el constructor tiene que llegar a la raíz por algún punto de paso dentro de `internal/idp` (por ejemplo, un constructor que reexporte o un subpaquete). Se define en diseño. Ese punto de paso queda sujeto al criterio 2 y no puede importar la Lista_De_Bibliotecas_IdP.
>
> Pendiente: si las pruebas de integración del Requisito 5 (inmutabilidad del Atributo_Tenant vía API de administración) necesitan un cliente de administración de Keycloak fuera de `internal/idp/internal`, hará falta una excepción explícita al criterio 1. Hoy no existe ninguna excepción.

### Requirement 3: Emisión y errores distinguibles

**User Story:** Como usuario de AI27, quiero que un login fallido no revele si mi usuario existe y que una caída del proveedor no se confunda con una credencial incorrecta, para que el acceso sea seguro y la falla se diagnostique bien (E4-HU-017 escenarios 2 y 4; `identidad.md` (i), (j)).

#### Acceptance Criteria

1. IF Keycloak rechaza la credencial recibida por `Issue` porque el usuario no existe o porque la contraseña es incorrecta, THEN THE Adaptador_Keycloak SHALL devolver en ambos casos la categoría de error de credencial inválida y un `Token` vacío (`AccessToken` y `RefreshToken` como cadena vacía y `ExpiraEn` con valor cero).
2. IF Keycloak rechaza la credencial recibida por `Issue` porque el usuario no existe o porque la contraseña es incorrecta, THEN THE Adaptador_Keycloak SHALL devolver un error cuyo texto sea idéntico en ambos casos y no incluya el `usuario` recibido ni indique cuál de las dos causas ocurrió.
3. IF durante `Issue` o `Refresh` Keycloak no responde dentro del tiempo límite por llamada configurado, no es alcanzable por la red, responde con un error interno del servidor o entrega una respuesta que el adaptador no puede interpretar, THEN THE Adaptador_Keycloak SHALL devolver la categoría de error de proveedor no disponible, distinta de las categorías de credencial inválida y de refresh inválido, y un `Token` vacío.
4. IF Keycloak rechaza en `Refresh` un refresh token vencido, revocado o desconocido, THEN THE Adaptador_Keycloak SHALL devolver la categoría de error de refresh inválido, distinta de las categorías de proveedor no disponible y de credencial inválida, y un `Token` vacío.
5. IF el `context.Context` recibido por `Issue`, `Refresh` o `Revoke` se cancela o vence antes de que Keycloak responda, THEN THE Adaptador_Keycloak SHALL terminar la llamada en curso sin esperar el tiempo límite por llamada configurado y devolver un error que permita identificar la cancelación o el vencimiento del contexto, sin clasificarlo como credencial inválida ni como refresh inválido.
6. THE Adaptador_Keycloak SHALL devolver cada error de `Issue` y `Refresh` en exactamente una categoría (credencial inválida, refresh inválido o proveedor no disponible, salvo el caso del criterio 5), identificable por el Servicio_Sesion sin inspeccionar el texto del mensaje.
7. THE Adaptador_Keycloak SHALL omitir la contraseña, el access token y el refresh token de todo log, traza, mensaje de error (incluidos los errores de la biblioteca cliente que el adaptador envuelva) y atributo de telemetría (`CredencialesEntrada`; E0-T3).
8. THE Adaptador_Keycloak SHALL recibir el tiempo límite por llamada a Keycloak como parámetro de su constructor.
9. IF el constructor del Adaptador_Keycloak recibe un tiempo límite por llamada ausente, igual a cero o negativo, THEN THE Adaptador_Keycloak SHALL rechazar la construcción con un error que nombre el parámetro.

> Nota para diseño: la suite actual solo exige `err != nil`. Distinguir categorías implica declarar errores centinela en `internal/idp` (hoy solo existen en `memory`) y extender la suite con casos nuevos sin alterar los cuatro existentes. Se valida en diseño.
>
> Pendientes de definición (no resueltos aquí): (a) **valor del tiempo límite por llamada**: sin fuente; queda como parámetro de configuración obligatorio, sin valor por omisión. (b) **Categoría de otros rechazos de Keycloak** (cuenta deshabilitada o bloqueada, cliente OIDC mal configurado o secreto inválido): no hay regla que diga si son credencial inválida, proveedor no disponible u otra categoría. (c) **Reintentos** ante proveedor no disponible en `Issue`/`Refresh`: no definidos; este requisito no los exige.

### Requirement 4: Segundo factor delegado a Keycloak

**User Story:** Como usuario de AI27, quiero que todo inicio de sesión exija segundo factor y que lo resuelva el proveedor, para que AI27 no mantenga un tramo propio de OTP (ADR 13 cerrado, DEC-031 #1; audit Q59/Q60; E4-T5).

#### Acceptance Criteria

1. THE Realm_AI27 SHALL declarar, en su configuración versionada (Requisito 5.7), el paso de segundo factor como obligatorio en el flujo de autenticación del cliente OIDC que usa el Adaptador_Keycloak para `Issue`, sin condición, grupo, atributo ni rol que lo vuelva opcional u omitible para algún usuario de los ocho Roles_V1. El canal del factor queda sujeto a A-2 y el flujo concreto a A-3.
2. IF `Issue` recibe una credencial válida y el segundo factor falta, no coincide o está vencido según Keycloak, THEN THE Adaptador_Keycloak SHALL devolver un error de una categoría distinta de proveedor no disponible (Requisito 3.3), junto con un `Token` cuyos `AccessToken`, `RefreshToken` y `ExpiraEn` estén vacíos o en valor cero. Si esa categoría también debe distinguirse de credencial inválida queda pendiente de A-3.
3. THE módulo `pf-svc-identidad` SHALL carecer de cualquier tabla, migración, evento, endpoint o componente propio que genere, envíe, almacene o valide códigos de segundo factor. Esto incluye, como mínimo, la tabla `otp_desafio`, el evento `otp.solicitado` y las rutas `/api/v1/otp/*`. Solo puede reenviar a Keycloak lo que el contrato vigente transporte.
4. WHEN la prueba de integración invoca `Issue` con la credencial válida de un usuario de prueba sin segundo factor configurado, una vez por cada uno de los ocho Roles_V1, THE Keycloak_De_Prueba SHALL rechazar la emisión del token. THE Adaptador_Keycloak SHALL devolver en cada caso el error y el `Token` vacío del criterio 2.
5. THE Adaptador_Keycloak SHALL omitir el valor del segundo factor que transporte (código, desafío o respuesta) de todo log, traza, mensaje de error y atributo de telemetría, igual que con la contraseña y los tokens (Requisito 3.7).

> Condicionado por A-2 (canal) y A-3 (forma del login): hoy el contrato no tiene dónde transportar el segundo factor.

### Requirement 5: Configuración del realm como garantía

**User Story:** Como equipo de seguridad, quiero que la configuración de Keycloak garantice tenant inmutable y roles cerrados, para que ni el usuario ni un cliente OIDC puedan alterar su organización o su rol (ADR 32 §1; E4-T1; DEC-036; DEC-053).

#### Acceptance Criteria

1. THE Realm_AI27 SHALL definir el Atributo_Tenant como no editable por el propio usuario en la consola de cuenta, en la API de cuenta y en los flujos de actualización de perfil, incluida la acción requerida de actualizar perfil durante el login.
2. THE Realm_AI27 SHALL definir el Atributo_Tenant como no modificable con las credenciales de los clientes OIDC de AI27 usados para login, token y refresco, ya sea por la API de administración o por parámetros enviados en las peticiones de autenticación, token o refresco.
3. THE Realm_AI27 SHALL contener, sumando todos los realms que lo formen, exactamente un Grupo_De_Rol por cada uno de los ocho Roles_V1 (`cliente`, `jefe_operativo`, `monitorista`, `supervisor`, `coordinador`, `ejecutivo`, `reaccion`, `administrador`), nombrado con el identificador canónico de DEC-053 y sin otro grupo que represente un rol. La distribución de los Grupos_De_Rol entre realms depende de A-1.
4. THE Realm_AI27 SHALL carecer de un grupo `auditor` en todos los realms que lo formen (DEC-036).
5. WHEN la prueba de integración intenta modificar el Atributo_Tenant o la pertenencia a un Grupo_De_Rol con la credencial del propio usuario de prueba, por la API de cuenta y por el flujo de actualización de perfil, THE Keycloak_De_Prueba SHALL rechazar cada intento, y una lectura posterior con la credencial administrativa de la prueba SHALL devolver el mismo valor de Atributo_Tenant y los mismos Grupos_De_Rol que había antes del intento.
6. WHEN la prueba de integración intenta modificar el Atributo_Tenant o la pertenencia a un Grupo_De_Rol con las credenciales del cliente OIDC de login, THE Keycloak_De_Prueba SHALL rechazar cada intento, y una lectura posterior con la credencial administrativa de la prueba SHALL devolver el mismo valor de Atributo_Tenant y los mismos Grupos_De_Rol que había antes del intento.
7. THE repositorio SHALL conservar como artefacto versionado la configuración del Realm_AI27 que usa la prueba de integración, incluida la definición del Atributo_Tenant, sus restricciones de edición y los ocho Grupos_De_Rol.
8. WHEN el Keycloak_De_Prueba arranca, THE prueba de integración SHALL importar el artefacto del criterio 7 sin pasos manuales, y IF la importación falla, THEN la prueba de integración SHALL fallar y no ejecutar los criterios 5 y 6.
9. THE Realm_AI27 SHALL impedir que el propio usuario y los clientes OIDC de AI27 usados para login, token y refresco agreguen o quiten pertenencias a un Grupo_De_Rol.
10. THE artefacto del criterio 7 SHALL omitir secretos de clientes OIDC y credenciales de cualquier entorno distinto del Keycloak_De_Prueba (ADR 32 §1; Requisito 9.4).

> Propuesta — pendiente de validación: que esa misma configuración versionada sea la fuente para el realm de producción. Las fuentes solo dicen que la composición de claims "se versiona y libera con el servicio" (E4-T2; `identidad.md` (k)); para el realm completo no hay regla.
>
> Gaps detectados (no son reglas nuevas): (1) los criterios originales solo cubrían el tenant y no la otra mitad del user story, que es que el usuario o un cliente OIDC no puedan alterar su rol. Se añaden los criterios 9 y 5–6 ampliados. (2) Si es uno o dos realms sigue pendiente (A-1); los criterios 3–4 se redactan para que valgan en ambos casos. (3) "Conservar el valor original" ahora se verifica con una lectura administrativa posterior. (4) Quién sí puede cambiar el tenant o el rol (`administrador` o la sincronización de `identidad`, A-5/A-8) no está definido y queda fuera de este requisito.

### Requirement 6: Claims del token compuestos en Keycloak

**User Story:** Como equipo de desarrollo, quiero que el token emitido por Keycloak lleve exactamente los Claims_AI27 en emisión y refresco, coherentes con `claims.Componer`, para que los consumidores deriven identidad, tenant, rol y `Audiencia` sin consultar a `identidad` (ADR 32 §4; DEC-031 #2; DEC-053; DEC-054; E0-T3).

#### Acceptance Criteria

1. WHEN Keycloak emite un access token por login para un usuario de cualquiera de los ocho Roles_V1, THE Mapper_De_Claims SHALL incluir en el token un claim no vacío por cada uno de los cinco Claims_AI27 (`sujeto`, `tenant`, `rol`, `Audiencia` y `sesion_id`), con el nombre de claim que lee `pf-pkg-platform-go/auth` para cada uno (ratificar esos nombres como contrato está pendiente de diseño; ver la segunda nota).
2. WHEN Keycloak emite un access token por refresco, THE Mapper_De_Claims SHALL recomponer `sujeto`, `tenant`, `rol` y `Audiencia` desde el modelo vigente `m'` del momento del refresco, de modo que sus valores sean iguales a `claims.Componer(m')` aunque difieran de los del token anterior, sin copiar ningún valor del token anterior (ADR 16).
3. THE Mapper_De_Claims SHALL incluir en cada access token exactamente un claim `rol`, cuyo valor sea una sola cadena, no una lista ni una cadena vacía, igual a uno de los ocho identificadores canónicos de Roles_V1 (DEC-053).
4. THE Mapper_De_Claims SHALL omitir del token todo identificador de unidad, monitorista, supervisor o cliente asignado al usuario, y todo otro dato de alcance (DEC-053 #2). Para verificarlo, la prueba de integración revisa que ningún valor del token decodificado contenga los identificadores de alcance asignados al usuario de prueba en la configuración importada.
5. THE Mapper_De_Claims SHALL escribir `Audiencia` en un claim propio, distinto del claim técnico `aud`, con valor `Cliente` cuando `rol` sea `cliente` y `Interno` cuando `rol` sea cualquiera de los otros siete Roles_V1 (DEC-054), y sin escribir ese valor dentro de `aud`.
6. WHEN la prueba de coherencia emite y luego refresca un token para cada usuario de prueba con modelo vigente `m` (al menos un usuario por cada uno de los ocho Roles_V1), THE Keycloak_De_Prueba SHALL entregar, en el token de emisión y en el de refresco, valores de `sujeto`, `tenant`, `rol` y `Audiencia` iguales campo a campo a `SujetoID`, `TenantID`, `Rol` y `Audiencia` de `claims.Componer(m)` (propiedad de coherencia; `sesion_id` queda fuera de esta comparación porque `Compuestos` no lo incluye).
7. WHEN se cambia el rol vigente de un usuario de prueba de un Rol_V1 a otro Rol_V1 distinto y se refresca su token por primera vez después del cambio, THE token refrescado SHALL contener el rol nuevo y la `Audiencia` que corresponde al rol nuevo según el criterio 5, y no SHALL contener el rol anterior (compuerta G4). Este criterio no fija qué pasa con un access token emitido antes del cambio mientras sigue vigente (A-9), y el `sesion_id` estable es responsabilidad de E4-T3/E4-T4.
8. WHEN `pf-pkg-platform-go/auth` verifica un token emitido por el Keycloak_De_Prueba contra su JWKS, con la audiencia técnica esperada configurada, THE verificador SHALL aceptar la firma, la expiración y `aud`, y devolver `Subject`, `TenantID`, `Rol`, `Audiencia` y `SesionID` no vacíos e iguales a los valores del token. `Audiencia` debe venir del claim propio presente en el token, no del respaldo que la deriva del rol cuando el claim falta.
9. IF el modelo vigente del usuario, al emitir o al refrescar, no tiene exactamente un rol de los Roles_V1 o no tiene `tenant`, THEN THE Adaptador_Keycloak SHALL devolver un error y un `Token` vacío, sin entregar un access token con Claims_AI27 incompletos (se deriva del criterio 3 y está pendiente de validación; la categoría de error está pendiente de diseño).

> Condicionado por A-4 (origen del `sesion_id`), A-6 (a qué historia pertenece el empaquetado) y A-8 (de dónde lee el mapper el modelo vigente). Si PM decide que el empaquetado es de E4-T2, este requisito se reduce a los criterios 6–8 como prueba de integración.

> Pendiente de diseño: ninguna ADR ni DEC fija los nombres de los claims. Hoy `pf-pkg-platform-go/auth` (`jwt.go`) lee `sub`, `rol`, `tenant_id`, `sesion_id` y `audiencia`; este último nombre es configurable con `ConAudienciaClaim`. Además, si `audiencia` falta, el verificador la deriva del rol sin dar error. También falta decidir si `sujeto` (`SujetoID`) es el `sub` interno de Keycloak o un identificador propio de AI27 (ligado a A-8).

### Requirement 7: Revocación en el IdP (paso 2)

**User Story:** Como equipo de desarrollo, quiero que el adaptador revoque la sesión en Keycloak de forma idempotente, para que el paso 2 de la revocación de ADR 26 pueda reintentarse sin efectos adicionales.

#### Acceptance Criteria

1. WHEN `Revoke` recibe un `sesionID` cuya sesión de Keycloak correspondiente (correspondencia según A-4) está activa, THE Adaptador_Keycloak SHALL terminar esa sesión en Keycloak y devolver éxito (`nil`), de modo que toda llamada posterior a `Refresh` con el refresh token de esa sesión devuelva la categoría de error de refresh inválido (Requisito 3.4).
2. WHEN `Revoke` recibe un `sesionID` cuya sesión de Keycloak correspondiente ya terminó o no existe, incluida la segunda y cada llamada posterior con el mismo `sesionID` tras una revocación exitosa, THE Adaptador_Keycloak SHALL devolver éxito (`nil`) sin modificar ninguna otra sesión ni dato del Realm_AI27 (idempotencia, P13).
3. IF durante `Revoke` Keycloak no responde dentro del tiempo límite configurado, responde con error de servidor o no es alcanzable por red, THEN THE Adaptador_Keycloak SHALL devolver la misma categoría de error de proveedor no disponible del Requisito 3.3, nunca éxito, para que el Servicio_Sesion reintente.
4. THE Adaptador_Keycloak SHALL usar para `Revoke` credenciales de servicio distintas de las del cliente OIDC de login, con permiso limitado a terminar sesiones del Realm_AI27 (y a consultarlas, solo si la resolución de A-4 lo requiere), sin permiso para crear, modificar ni eliminar usuarios, Grupos_De_Rol ni el Atributo_Tenant.
5. WHEN `Revoke` termina la sesión de Keycloak correspondiente a un `sesionID`, THE Adaptador_Keycloak SHALL conservar activas las demás sesiones de Keycloak del mismo usuario, de modo que `Refresh` siga aceptando sus refresh tokens vigentes.
6. IF Keycloak rechaza las credenciales de servicio del Adaptador_Keycloak o niega el permiso para terminar la sesión durante `Revoke`, THEN THE Adaptador_Keycloak SHALL devolver un error y nunca éxito, de modo que una revocación no ejecutada en Keycloak no se reporte como completada.
7. WHEN la prueba de integración intenta terminar una sesión del Realm_AI27 en el Keycloak_De_Prueba con las credenciales del cliente OIDC de login, THE Keycloak_De_Prueba SHALL rechazar la operación y conservar activa la sesión.

> Condicionado por A-4: cómo se traduce el `sesion_id` de AI27 a la sesión de Keycloak no está definido. El orden local-primero y la reintentabilidad son de E4-T3; aquí solo se exige que el paso 2 sea idempotente y con errores distinguibles.

### Requirement 8: Exposición de red

**User Story:** Como equipo de seguridad, quiero que Keycloak solo exponga a internet login, token y JWKS, y que los servicios obtengan el JWKS por la red interna, para reducir la superficie de ataque (ADR 32 §2; `identidad.md` (f)).

#### Acceptance Criteria

1. THE Host_Auth SHALL publicar a internet únicamente tres endpoints de Keycloak, todos del realm de clientes: login, token y JWKS (ADR 32 §2).
2. IF una petición que llega desde internet por el Host_Auth pide la consola de administración de Keycloak, THEN THE proxy de la DMZ SHALL rechazarla sin reenviarla a Keycloak.
3. IF una petición que llega desde internet por el Host_Auth pide la API de administración de Keycloak o cualquier otra ruta de Keycloak distinta de los tres endpoints del criterio 1, THEN THE proxy de la DMZ SHALL rechazarla sin reenviarla a Keycloak.
4. THE Adaptador_Keycloak SHALL recibir por parámetros del constructor URLs separadas de Keycloak para token, refresco y administración, todas de la red interna.
5. THE Adaptador_Keycloak SHALL hacer todas sus llamadas a Keycloak (token, refresco y administración) solo contra las URLs del criterio 4, nunca a través del Host_Auth.
6. THE Raiz_De_Composicion SHALL leer la URL de JWKS de la red interna de `IDENTIDAD_JWKS_URL` y pasarla al verificador JWT.
7. THE verificador JWT SHALL obtener el JWKS solo de la URL configurada en `IDENTIDAD_JWKS_URL`, nunca a través del Host_Auth ni de otro destino en internet.

> Pendiente: ADR 32 §2 limita el Host_Auth al realm de clientes, pero todavía no se decide si AI27 usa uno o dos realms (A-1). Si se usa uno solo, falta definir qué impide que el login interno pase por el Host_Auth. Tampoco hay regla sobre qué hace la Raiz_De_Composicion cuando `IDENTIDAD_JWKS_URL` falta o apunta a internet; esto no se define aquí.

> Los criterios 1–3 se cumplen en la configuración del proxy y se verifican en despliegue; el despliegue en k3s está fuera de alcance. Este spec solo deja la regla documentada y las URLs separadas por configuración.

### Requirement 9: Cableado por configuración

**User Story:** Como equipo de desarrollo, quiero elegir entre Adaptador_Keycloak y Doble_En_Memoria por configuración, para seguir arrancando standalone sin Keycloak (ADR 17 I5).

#### Acceptance Criteria

1. WHERE la configuración del proceso selecciona Keycloak como proveedor, THE Raiz_De_Composicion SHALL construir el Adaptador_Keycloak con su configuración ya resuelta (Requisito 2.5), inyectarlo en `identidad.Deps.IDP` y no construir el Doble_En_Memoria.
2. WHERE la configuración del proceso no selecciona proveedor (parámetro de selección ausente o vacío), THE Raiz_De_Composicion SHALL inyectar el Doble_En_Memoria en `identidad.Deps.IDP` sin intentar ninguna conexión a Keycloak y conservando sin cambios el comportamiento standalone actual: `IDENTIDAD_ADDR` con valor por defecto `:8080`, `IDENTIDAD_AUDIENCIA` con valor por defecto `ai27-internal`, `IDENTIDAD_DATABASE_URL` e `IDENTIDAD_JWKS_URL` leídas igual que hoy, y `/ready` con el check `postgres:svc_identidad` como única verificación registrada.
3. IF la configuración selecciona Keycloak y falta o está vacío uno o más parámetros obligatorios del adaptador, THEN THE Raiz_De_Composicion SHALL terminar el proceso con código de salida distinto de cero antes de abrir el puerto HTTP, con un error que nombre todos los parámetros faltantes en un solo mensaje sin mostrar el valor de ningún parámetro.
4. THE Raiz_De_Composicion SHALL obtener los secretos del adaptador (secreto del cliente OIDC, credenciales de servicio del Requisito 7.4) de variables de entorno o archivos montados, nunca de valores fijos en el código ni en archivos versionados del repositorio (ADR 32 §1).
5. WHILE el Adaptador_Keycloak está seleccionado, THE Raiz_De_Composicion SHALL exponer el resultado de una verificación de alcanzabilidad de Keycloak con nombre propio, terminada en 5 segundos o menos, de modo que si Keycloak no responde, `/ready` no cambie de 200 a 503 por esa causa (las sesiones vigentes siguen válidas, Q64).
6. IF el parámetro de selección de proveedor tiene un valor distinto de los valores aceptados, THEN THE Raiz_De_Composicion SHALL terminar el proceso con código de salida distinto de cero antes de abrir el puerto HTTP, con un error que nombre el parámetro y los valores aceptados, sin recurrir al Doble_En_Memoria.
7. WHEN la Raiz_De_Composicion termina de cablear el proveedor, THE Raiz_De_Composicion SHALL registrar en el log de arranque qué implementación de Puerto_IdP quedó inyectada (Adaptador_Keycloak o Doble_En_Memoria), sin incluir secretos, credenciales ni tokens.

> Propuesta — pendiente de validación: el criterio 5 (no bloquear `/ready`) se deriva de `identidad.md` (i), que dice que con el IdP caído "todo lo autenticado" sigue funcionando. Confirmar.
>
> **Contradicción con el código actual (verificado en `pf-pkg-platform-go/health/health.go`):** `ReadinessHandler` responde 503 si falla *cualquier* check registrado con `RegisterCheck`; el `health.Registry` no tiene checks informativos. Si se confirma el criterio 5, el check de Keycloak no puede registrarse con `RegisterCheck` tal como está. Opciones para diseño: (a) ampliar `pf-pkg-platform-go/health` con checks informativos que se reporten en el cuerpo de `/ready` sin afectar el código de estado; (b) exponer la alcanzabilidad solo por la telemetría del Requisito 10; (c) registrarlo como check bloqueante, lo que contradice el criterio 5. Si PM confirma el criterio 5, falta definir también si el arranque con Keycloak inalcanzable continúa (lo coherente con la propuesta) o falla.
>
> **Propuesta de diseño, no decisión:** un parámetro de selección `IDENTIDAD_IDP` con valores `memory` | `keycloak`, y parámetros del adaptador con prefijo `IDENTIDAD_KEYCLOAK_` (URL interna, realm, client ID, secreto del cliente OIDC, credenciales de servicio). Ninguno de esos nombres existe hoy en el código ni en las fuentes. Que el valor desconocido falle en lugar de caer al doble (criterio 6) también es propuesta. Queda pendiente decidir si el Doble_En_Memoria por defecto es aceptable fuera de desarrollo; `cmd/api` se declara binario de desarrollo ("Producción no usa este binario"), así que la selección en la raíz de composición de `core` queda fuera de este requisito.

### Requirement 10: Observabilidad del IdP

**User Story:** Como equipo de operaciones, quiero distinguir latencia y fallas del proveedor de las del servicio, para no investigar una caída de Keycloak como propia de `identidad` (`identidad.md` (j)).

#### Acceptance Criteria

1. WHEN una llamada del Adaptador_Keycloak a Keycloak termina, ya sea con respuesta exitosa, con error o por vencimiento del tiempo límite configurado, THE Adaptador_Keycloak SHALL registrar exactamente una medición de latencia para esa llamada. La medición cubre desde el inicio de la llamada hasta su terminación y lleva la etiqueta de la operación que la originó: `issue`, `refresh` o `revoke`.
2. WHEN una llamada del Adaptador_Keycloak a Keycloak termina, THE Adaptador_Keycloak SHALL registrar exactamente un resultado para esa llamada. El resultado lleva una sola categoría del conjunto cerrado {éxito, credencial inválida, refresh inválido, proveedor no disponible}, y esa categoría es la misma que el Adaptador_Keycloak devuelve al llamador según el Requirement 3 y el Requirement 7. Un `Revoke` idempotente sobre una sesión ya terminada o inexistente se registra como éxito.
3. THE Adaptador_Keycloak SHALL emitir las mediciones de latencia y los resultados de los criterios 1 y 2 por medio de la instrumentación OTLP de `pf-pkg-platform-go`. Ninguna medición, resultado, atributo ni traza asociada contiene la contraseña, el access token ni el refresh token (Requirement 3, criterio 7).
4. THE Adaptador_Keycloak SHALL marcar cada medición de latencia y cada resultado con un atributo que identifique a Keycloak como la dependencia de origen. Así, un receptor OTLP puede separar las señales de las llamadas a Keycloak de las señales propias de `identidad` filtrando solo por ese atributo y por la operación.
5. IF una llamada a Keycloak termina por una causa que no corresponde a ninguna categoría del criterio 2, por ejemplo la cancelación o el vencimiento del `context.Context` del llamador (Requirement 3, criterio 5) o la falta o invalidez del segundo factor (Requirement 4, criterio 2), THEN THE Adaptador_Keycloak SHALL registrar la latencia y un resultado con una categoría distinta de las cuatro del criterio 2. Ese resultado nunca se registra como proveedor no disponible.
6. IF la instrumentación OTLP no puede exportar las señales, THEN THE Adaptador_Keycloak SHALL devolver al llamador el mismo `Token` y la misma categoría de error que habría devuelto con la exportación disponible.

> Propuesta, pendiente de validación: el criterio 5 introduce una categoría de resultado fuera del conjunto de cuatro. Las fuentes no definen cuál es ni cómo se nombra. Tampoco definen si una cancelación iniciada por el llamador y una falla de segundo factor comparten categoría o llevan categorías separadas. Product Management o el diseño deben decidir esto. El criterio 6 se deriva del objetivo de la historia: la telemetría no debe introducir un modo de falla en la emisión, el refresco o la revocación. Falta confirmarlo.

### Requirement 11: Prueba de integración contra Keycloak

**User Story:** Como equipo de desarrollo, quiero una prueba de integración contra un Keycloak efímero, para cumplir la definición de terminado de E4-T1.

#### Acceptance Criteria

1. WHEN la prueba de integración se ejecuta con el build tag `integration` y Docker está disponible, THE prueba de integración SHALL levantar un Keycloak_De_Prueba con testcontainers-go (la versión `v0.44.0` que ya usa el módulo) a partir de una imagen de Keycloak con etiqueta de versión explícita, distinta de `latest`, e importar la configuración versionada del Realm_AI27 del repositorio (Requisito 5.7) antes de ejecutar cualquier caso, sin pasos de configuración manual.
2. WHEN el Keycloak_De_Prueba está listo y el Realm_AI27 está importado, THE prueba de integración SHALL ejecutar la Suite_Contrato_IdP contra el Adaptador_Keycloak con los cuatro casos existentes sin modificar y con los casos de categorías de error que se agreguen por el Requisito 3. También ejecutará los criterios de prueba de integración de los Requisitos 4.4, 6.6, 6.7 y 6.8, en la medida en que A-3, A-4 y A-6 los dejen dentro del alcance de E4-T1.
3. THE prueba de integración SHALL residir en archivos con la restricción de compilación `integration`, de modo que no se compile ni se ejecute sin ese build tag, igual que las pruebas de integración de Postgres de E4-T2.
4. IF la prueba de integración se ejecuta con el build tag `integration` y Docker no está disponible, THEN THE prueba de integración SHALL marcarse como omitida (no como fallida) con un mensaje que indique que la causa es Docker no disponible, igual que las pruebas de Postgres de E4-T2. La falta de Docker es la única condición de omisión.
5. WHEN se ejecutan `go build ./...`, `go vet ./...`, `go test ./...` y `arch-test` sin el build tag `integration`, en un entorno con o sin Docker, THE repositorio SHALL aprobar las cuatro verificaciones sin intentar levantar un Keycloak_De_Prueba.
6. WHEN el Keycloak_De_Prueba está listo y el Realm_AI27 está importado, THE prueba de integración SHALL verificar los Requisitos 5.3, 5.4, 5.5 y 5.6: hay exactamente un Grupo_De_Rol por cada uno de los ocho Roles_V1 con su identificador canónico, no existe un grupo `auditor`, y Keycloak rechaza la modificación del Atributo_Tenant con la credencial del usuario de prueba y con la del cliente OIDC de login, y conserva el valor original en ambos casos.
7. IF el Keycloak_De_Prueba no queda listo dentro de 120 segundos desde que se solicita el contenedor, o si la importación de la configuración del Realm_AI27 falla, THEN THE prueba de integración SHALL marcarse como fallida (no como omitida) con un mensaje que indique en qué etapa falló (arranque o importación), sin ejecutar los casos de los criterios 2 y 6.
8. WHEN la prueba de integración termina, sea aprobada, fallida o interrumpida por un pánico dentro de la prueba, THE prueba de integración SHALL detener y eliminar el contenedor del Keycloak_De_Prueba que levantó.

> Pendientes que condicionan este requisito:
> - A-7: el valor de la etiqueta de versión de la imagen queda pendiente. El criterio 1 solo exige que sea explícita y fija.
> - El límite de 120 segundos del criterio 7 es una propuesta de Kiro, pendiente de validación. Ninguna fuente fija el tiempo de arranque.
> - Los usuarios de prueba vienen de la configuración importada y no del adaptador (A-5).
> - Las fuentes no dicen si CI corre la prueba con el build tag `integration`. Este requisito no lo exige; el criterio 5 solo cubre la ejecución sin el tag.

## Ambigüedades y pendientes (no resueltos por este spec)

| # | Pendiente | Fuente | Impacto en este spec |
| --- | --- | --- | --- |
| P-0 | Asentar Keycloak como IdP en `decision-log.md` y ADR 32 (proceso) | Confirmación del usuario 2026-10-08; ADR 32 Pendiente 1 | Las fuentes siguen diciendo "propuesta" |
| A-1 | **Realms:** uno solo, o separar `cliente` e internos | E4-T1 ("requirements-identidad, Pendiente 4", archivo no localizado); ADR 32 §2 menciona "realm de clientes" en `auth.<dominio>` | **RESUELTA (2026-10-09):** un solo realm `ai27`. El artefacto `deploy/keycloak/realm-ai27.json` lo refleja |
| A-2 | **Canal del segundo factor en V1** | DEC-031 notas #1. Keycloak trae TOTP y WebAuthn nativos; SMS requiere extensión y proveedor | **RESUELTA (2026-10-09, DEC-071):** TOTP nativo de Keycloak (correo y WebAuthn descartados). Flujo de grant directo con OTP REQUIRED; el formulario de `Issue` envía `totp` |
| A-3 | **Forma del login — contradicción.** `CredencialesEntrada {usuario, contrasena}` con `additionalProperties: false` implica en Keycloak el *Direct Access Grant* (ROPC) | Contrato `identidad.yaml`; E4-HU-017 (MFA obligatorio); OAuth 2.1 desaconseja ROPC | **RESUELTA:** opción (a) — ROPC solo con TOTP; `Credentials` lleva `SegundoFactor` y el formulario de `Issue` envía `totp`. **Pendiente de proceso:** confirmar el registro (DEC) donde quedó asentada; la cita original a DEC-065/066 era errónea |
| A-4 | **Correlación `sesion_id` ↔ sesión de Keycloak** | ADR 26; E4-T3; `Revoke(ctx, sesionID)` recibe el id de AI27 | **RESUELTA:** `Config.SessionResolver` inyectable; nil = identidad (sesionID_ai27 == kcSessionID). E4-T3 implementará el resolver concreto. **Pendiente de proceso:** confirmar el registro (DEC); la cita original a DEC-065/066 era errónea |
| A-5 | **Administración de usuarios y grupos — contradicción.** E4-T1 "Qué construir" incluye "administrar usuarios y grupos"; el puerto la excluye a propósito por la brecha contractual de `/api/v1/sujetos` y `/api/v1/roles` (`internal/gobierno/port.go`, `REPORTE-BRECHAS.md` §1) | E4-T1; E4-T6 | **Propuesta:** dejarla fuera de este spec y atenderla en E4-T6 cuando `pf-pkg-contracts` publique la superficie, salvo que PM indique lo contrario. La prueba de integración da de alta usuarios de prueba por la configuración importada, no por el adaptador |
| A-6 | **¿El empaquetado del Mapper_De_Claims es de E4-T1 o de E4-T2?** | E4-T1 lo menciona; spec de E4-T2 lo dejó "EN ESPERA de ADR 32"; E4-T2 "Qué construir" lo lista | Define si el Requisito 6 es construcción o solo verificación |
| A-7 | **Versión de Keycloak a fijar** | Sin fuente | **PENDIENTE DE EJECUCIÓN:** la imagen de referencia es `26.7.0` (nunca `latest`). Falta fijar la estable exacta y correr `-tags integration` contra ella para confirmar la forma del flujo de grant directo con OTP y del *user profile*. Debe coincidir con la que opere operaciones |
| A-8 | **De dónde lee el Mapper_De_Claims el modelo vigente** (rol y tenant viven en Postgres de `identidad`, E4-T2) | `identidad.md` (d); ADR 32 §4 | Opciones a decidir en diseño: Keycloak como réplica (grupos y atributo sincronizados por `identidad`) o extensión que consulta a `identidad`. La segunda introduce una dependencia de emisión hacia `identidad` y debe medirse contra la señal de latencia de composición (E4-T4) |
| A-9 | **Quitar un rol: ¿corta en 30 s o en el siguiente refresco?** | E4-HU-020 / Q63 frente a DEC-031 #2 | No bloquea E4-T1; afecta lo que el Requisito 6.7 puede prometer |
| A-10 | **Esquema `bearerCognito` y descripciones con "Cognito"** en `identidad.yaml` | `REPORTE-BRECHAS.md` §2 | No se renombra desde este servicio; requiere versión nueva de `pf-pkg-contracts` |
| A-3 (contradicción R1 ↔ R4) | **La suite de contrato emite solo con usuario/contraseña y R4 exige segundo factor a todos** | `internal/idp/idptest`; Requisito 4; A-3 | No está definido si el usuario de prueba de la Factory queda exento del factor, si la suite se amplía o si cambia `Credentials`. Afecta R1.2, R1.6 y R4.1 |
| A-11 | **`arch-test@v0.1.1` solo verifica el grafo entre servicios** y no detecta importaciones de terceros | `pf-pkg-platform-go/tools/arch-test/main.go` | Decidir si se extiende `arch-test` en `pf-pkg-platform-go` o se agrega una verificación local. Hoy nada cubre R2.2 ni R2.3 |
| A-12 | **`pf-pkg-platform-go/health` responde 503 si falla cualquier check registrado**; no hay checks informativos | `pf-pkg-platform-go/health/health.go` | Si se confirma que Keycloak caído no bloquea `/ready`, hay que ampliar health, exponerlo solo por telemetría o aceptar el bloqueo. Afecta R9.5 |
| A-13 | **Nombres de claims no fijados por ADR/DEC.** Hoy `pf-pkg-platform-go/auth` lee `sub`, `rol`, `tenant_id`, `sesion_id` y `audiencia` (configurable) y deriva `audiencia` del rol si falta | `pf-pkg-platform-go/auth` (`jwt.go`) | **RESUELTA (2026-10-09, DEC-072):** opción (c) — el `sub` es el id opaco de Keycloak y el `SujetoID` se resuelve en servidor por `IdpSubject` (no se agrega `sujeto_id`). Claims ratificados: `sub`, `rol`, `tenant_id`, `sesion_id`, `audiencia` |
| A-14 | **Categoría de error para otros rechazos de Keycloak** (cuenta deshabilitada, cliente mal configurado) y para fallas de segundo factor / cancelación de contexto en telemetría | Sin fuente | Afecta R3, R4.2 y R10.5 |
| A-15 | **Valores propuestos por Kiro sin fuente**, pendientes de validación: tolerancia de 1 s en `ExpiraEn`, 5 s del check de alcanzabilidad, 120 s de arranque del Keycloak de prueba, nombres `IDENTIDAD_IDP`/`IDENTIDAD_KEYCLOAK_*` | Propuesta de Kiro | Afecta R1.4–1.5, R9.5, R11.7 y R9 |
| A-16 | **Permisos mínimos de la cuenta de servicio para `Revoke`** | Hallazgo de diseño (§Artefacto del realm); R7.4 | **RESUELTA (2026-10-09, DEC-073):** privilegio mínimo — FGAP v2 como primera opción, Logout OIDC como alternativa; `manage-users` descartado. La cuenta `ai27-identidad-svc` hoy no lleva `manage-users` (R7.4/R7.7 pasan por construcción). **Pendiente de ejecución:** finalizar el permiso acotado de terminación de sesión contra la versión fijada (A-7), que hace pasar R7.1/R7.5 |

> **Notas de proceso (ronda PM 2026-10-09, antes en `decisiones-pendientes.md`, ya retirado):**
> - **DEC-074 (`t.Skip` con caducidad):** la excepción a R11.4 se aceptó solo mientras no existiera el artefacto del realm. Con el realm escrito, los `t.Skip` por decisión se removieron; la única omisión viva de la suite de integración es "Docker no disponible".
> - **Registro de DEC por confirmar:** este spec citaba A-1/A-3/A-4/A-7/A-15 como "decididas por DEC-065/066", pero DEC-065 es el retiro de `tracer.2` y DEC-066 el esquema `roster` en `operacion`. Falta confirmar con Angel en qué registro quedaron asentadas esas cinco, o abrir las DEC que falten. A-2/A-13/A-16 y el proceso sí están asentados (DEC-071/072/073/074).

## Trazabilidad

| Requisito | Fuente |
| --- | --- |
| R1 Puerto y suite | E4-T1 (definición de terminado); `internal/idp/idptest`; ADR 15 |
| R2 Aislamiento | E4-T1 criterio 1; `identidad.md` (d), (l); §A.5 R2; DEC-062 (sin AWS) |
| R3 Emisión y errores | E4-HU-017 criterios y escenarios 2 y 4; Q64; `identidad.md` (i), (j); E0-T3 |
| R4 Segundo factor | E4-HU-017 escenarios 1 y 3; ADR 13 / DEC-031 #1; Q59/Q60; E4-T5 |
| R5 Realm como garantía | E4-T1 "Qué construir" y criterios 2–3; ADR 32 §1; DEC-053; DEC-036 |
| R6 Claims | E4-T2; ADR 16 / DEC-031 #2; DEC-053; DEC-054; ADR 32 §4; E0-T3; `internal/claims` |
| R7 Revocación paso 2 | ADR 26; `identidad.md` (h); E4-T3; P13 |
| R8 Exposición | ADR 32 §2; `identidad.md` (f) |
| R9 Cableado | ADR 17 I5; `cmd/api/main.go`; ADR 32 §1 (secretos) |
| R10 Observabilidad | `identidad.md` (j) |
| R11 Prueba de integración | E4-T1 definición de terminado; spec E4-T2 (build tag `integration`, testcontainers) |

## Dependencias

- **Depende de:** E0-T1, E0-T2, E0-T3 (verificación local de token contra JWKS). E4-T2 ya implementado (`claims.Componer`, modelo de autorización).
- **Bloquea:** E4-T2 (empaquetado del mapper, según A-6), E4-T3 (emisión y revocación reales), E4-T4 (reevaluación en refresco contra el IdP real).
- **Bloqueado por decisión:** nada. A-1, A-2 (DEC-071), A-3, A-4, A-13 (DEC-072) y A-16 (DEC-073) están resueltas. Lo que queda es ejecución, no decisión: fijar la versión de Keycloak (A-7) y correr la suite de integración contra ella (incluye finalizar el permiso mínimo de la cuenta de servicio para R7.1/R7.5).

## Fuera de alcance

- Persistencia de `sesion`, generación del `sesion_id` en Postgres, caché Redis, outbox y evento `sesion.revocada` (E4-T3).
- Revocación masiva y revocación administrativa (E4-T3).
- Reevaluación de rol en refresco con `sesion_id` estable extremo a extremo (E4-T4).
- Administración de usuarios, grupos y roles, traza `cambio_identidad` y eventos `sujeto.*`/`rol.*` (E4-T6; ver A-5).
- Modelo y eventos de alcance (DEC-053, DEC-045 notas).
- Despliegue de Keycloak y `core` en k3s, alta disponibilidad, configuración del proxy de la DMZ y certificados (ADR 32 §2–3; operaciones).
- Cambios a `pf-pkg-contracts` (renombrar `bearerCognito`, ampliar `CredencialesEntrada`); si A-3 los exige, se solicitan al repo de contratos.
- Break-glass administrativo (Q65) y textos de mensajes de error al usuario (E4-HU-017, pendientes).
