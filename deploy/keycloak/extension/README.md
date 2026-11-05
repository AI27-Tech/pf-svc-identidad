# Extensión de Keycloak de AI27 — `ai27-keycloak-ext`

Authenticator que transporta el `sesion_id` de AI27 hacia la sesión de Keycloak
(E4-T1, DEC-083). Es la pieza Java de la Decisión C: `identidad` **genera** el
`sesion_id` y Keycloak sólo lo **transporta**.

## Qué hace

En el flujo `ai27-direct-grant`, después de validar usuario, contraseña y OTP, el
authenticator `ai27-sesion-id-note`:

1. Lee el parámetro de formulario `sesion_id` del grant directo.
2. Falla cerrada si falta o no es un UUID válido (rechaza con credencial
   inválida; no emite un token sin `sesion_id`).
3. Guarda el valor como nota de sesión de usuario `SESION_ID`.

El note-mapper del realm (`oidc-usersessionmodel-note-mapper`, en
`../realm-ai27.json`) emite esa nota como el claim `sesion_id`. La nota sobrevive
a los refrescos, así que el `sesion_id` se mantiene estable (ADR 26 §1).

## Correspondencia con el realm

Estos tres nombres deben coincidir EXACTAMENTE con `../realm-ai27.json`:

| Elemento | Valor | Dónde en el realm |
| --- | --- | --- |
| Id del provider | `ai27-sesion-id-note` | `authenticationFlows[ai27-direct-grant].authenticationExecutions[].authenticator` |
| Nota de sesión | `SESION_ID` | mapper `sesion_id` → `config.user.session.note` |
| Parámetro / claim | `sesion_id` | mapper `sesion_id` → `config.claim.name` (y el form param que envía el adaptador Go) |

## Compilar

Requiere JDK 17+ y Maven. Las dependencias de Keycloak son `provided`
(no se empaquetan). Versión fijada: Keycloak **26.7.4** (DEC-083 #5), que debe
coincidir con `imagenKeycloak` en
`internal/idp/internal/kc/integration_test.go`.

```sh
mvn -f deploy/keycloak/extension/pom.xml clean package
```

El artefacto queda en `target/ai27-keycloak-ext.jar`.

## Cómo lo consume la suite de integración

La suite Go **no** compila el JAR (ratificado 2026-10-12, review punto 5): lo
consume prebuilt. El arnés (`arranqueKeycloak`) lo monta en
`/opt/keycloak/providers/ai27-keycloak-ext.jar`. La ruta se toma de
`IDENTIDAD_KC_EXT_JAR` o del destino por defecto de Maven; si el JAR no existe,
la suite se omite con `t.Skip`.

```sh
mvn -f deploy/keycloak/extension/pom.xml clean package
go test -tags integration ./internal/idp/internal/kc/...
```

## Despliegue

En producción, el JAR se coloca en `/opt/keycloak/providers/` y se corre
`kc.sh build` (ADR 32 §4). Con `start-dev --import-realm`, Keycloak lo construye
al arrancar.
