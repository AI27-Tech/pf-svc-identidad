> **CERRADO (2026-10-16).** Las seis cuestiones de abajo ya tienen respuesta. Las resoluciones están en `DECISIONES-TOMADAS.md`. Este documento se conserva como el planteamiento original que se presentó.

# Cuestiones abiertas — E4-T6

**Gobierno y traza de cambios administrativos** · `pf-svc-identidad`
**Fecha:** 2026-10-16
**Verificado contra:** contexto `ai27-platform-context` @ `913380e` (al día con `origin/main`) y contrato `pf-pkg-contracts` tag `v0.1.0-tracer.6` (último publicado). Ninguna de las cuestiones de abajo tiene respuesta en las decisiones, HU, ADR ni en el contrato a esa fecha.

Las decisiones principales de E4-T6 (G-1 a G-11) ya están tomadas. Al incorporarlas al spec quedaron **seis cuestiones** que ninguna de esas decisiones alcanza a cubrir: aparecen en el borde entre dos decisiones, o porque una decisión delegó el dato a otra parte. Cada una se presenta con qué es y las opciones de resolución.

---

## Cuestión 1 — Orden y reintento en el reemplazo de rol

**Qué es.** Cambiar el rol de un usuario (por ejemplo, de monitorista a supervisor) implica revocar el rol anterior y asignar el nuevo. Cada uno de esos dos pasos se escribe en dos sistemas: la base de datos local (Postgres) y el proveedor de identidad (Keycloak). Para un retiro de acceso se decidió "base local primero"; para un otorgamiento, "proveedor de identidad primero". Un reemplazo es las dos cosas a la vez, así que no está definido qué orden sigue. Tampoco está definido cuántas veces se reintenta un paso si falla.

**Opciones de resolución.**

| Opción | Descripción |
|--------|-------------|
| A | En el proveedor de identidad, quitar el grupo del rol anterior **antes** de agregar el del nuevo, para no dejar al usuario en dos grupos a la vez. Escritura local y proveedor en el orden del retiro (local primero) |
| B | Tratar el reemplazo como un otorgamiento (proveedor primero), asumiendo que el rol nuevo pesa más que el viejo |
| C | Escribir todo en local primero (una sola transacción: revoca + asigna + traza + eventos) y después sincronizar el proveedor |

**Reintento.** En paralelo, decidir la política del paso que toca el proveedor de identidad:

| Opción | Descripción |
|--------|-------------|
| R1 | Reintento con espera creciente (1 s, 2 s, 4 s; máximo 3 intentos) y, si se agota, responder error al cliente |
| R2 | Sin reintento: un fallo del proveedor devuelve error de inmediato |
| R3 | Local primero y un proceso aparte que reconcilia el proveedor después |

---

## Cuestión 2 — Quitar un rol sin asignar otro

**Qué es.** Está definido qué pasa al **cambiar** un rol (reemplazo) y al **desactivar** un usuario. No está definido el caso de **solo quitar** el rol sin dar otro, que deja a un usuario activo pero sin ningún rol. Además, dos fuentes se contradicen: una pide que quitar un rol corte el acceso del usuario en un máximo de 30 segundos; otra dice que el cambio surte efecto hasta el siguiente refresco de la sesión.

**Opciones de resolución.**

| Opción | Descripción |
|--------|-------------|
| A | Se permite dejar a un usuario sin rol, y su sesión viva se corta de inmediato (máximo 30 segundos). Trata "quitar el rol" como un retiro de acceso |
| B | Se permite dejar a un usuario sin rol, pero el corte espera al siguiente refresco de la sesión |
| C | No se permite quitar un rol sin asignar otro: la única forma de cambiar es el reemplazo, y para retirar el acceso por completo se desactiva al usuario |

---

## Cuestión 3 — Tenant de los usuarios internos: valor y ubicación

**Qué es.** Se decidió que los siete roles internos pertenecen a un único tenant de AI27. Falta el identificador (UUID) de ese tenant, y dónde se guarda: hoy el servicio de identidad no tiene una tabla de tenants, solo una columna `tenant_id` en el usuario.

**Opciones de resolución.**

| Opción | Descripción |
|--------|-------------|
| A | Crear una tabla de tenants y sembrar en ella el tenant interno con un UUID fijo, igual que se siembran los ocho roles |
| B | No crear tabla: el UUID del tenant interno vive como constante de configuración del servicio y se escribe directamente en la columna `tenant_id` del usuario |
| C | Dejar el tenant interno como un valor nulo o especial sin tabla, y tratar a los internos como "sin organización" |

**Dato pendiente en todas las opciones.** El valor concreto del UUID del tenant interno.

---

## Cuestión 4 — Enlace del primer administrador con el proveedor de identidad

**Qué es.** El primer administrador se crea con una migración de despliegue, porque no puede crearse con el endpoint de alta (ese endpoint exige que ya exista un administrador). El problema: el identificador del usuario en Keycloak (`sub`) es opaco y no lo controlamos de antemano, así que la migración no sabe con qué `sub` enlazar la fila que crea en la base de datos.

**Opciones de resolución.**

| Opción | Descripción |
|--------|-------------|
| A | El realm de Keycloak se importa con el primer administrador ya creado y con un `sub` fijo conocido; la migración usa ese mismo valor |
| B | La migración crea la fila sin `sub`; el enlace se completa la primera vez que ese administrador inicia sesión |
| C | Un paso de arranque (script de despliegue) crea el usuario en Keycloak, lee el `sub` que devuelve y luego inserta la fila local ya enlazada |

**Dato relacionado.** Toda fila de la traza exige un actor no nulo. El alta del primer administrador necesita un identificador de "actor de sistema" fijo; falta definir cuál es.

---

## Cuestión 5 — Reactivación de un usuario desactivado

**Qué es.** Entre los campos editables de un usuario está el estado (activo/inactivo), lo que implica que un usuario desactivado se puede volver a activar. No está definido qué hecho publica esa reactivación (hoy solo existe el hecho de "usuario desactivado", no uno de "reactivado") ni qué queda registrado en la traza.

**Opciones de resolución.**

| Opción | Descripción |
|--------|-------------|
| A | La reactivación publica un hecho nuevo ("usuario reactivado") y deja su propia fila en la traza |
| B | La reactivación reutiliza el hecho de alta ("usuario creado") y deja fila en la traza |
| C | No se permite reactivar en V1: un usuario desactivado se queda así, y para readmitirlo se da de alta uno nuevo |

---

## Cuestión 6 — Consumidores del hecho de alta/baja de usuario

**Qué es.** Los hechos de gobierno (usuario creado, usuario desactivado, rol asignado, rol revocado) los consumen otros servicios. Dos fuentes discrepan sobre quién consume los hechos de usuario: una lista mensajería y reportes; otra añade catálogos cuando el usuario está referenciado por un custodio. No se elige en silencio.

**Opciones de resolución.**

| Opción | Descripción |
|--------|-------------|
| A | Los consumidores son mensajería, reportes y catálogos (los tres) |
| B | Los consumidores son solo mensajería y reportes; catálogos no consume estos hechos |
| C | Catálogos consume solo el hecho de baja (usuario desactivado), no el de alta |

---

## Resumen

| # | Cuestión | Bloquea el diseño |
|---|----------|-------------------|
| 1 | Orden y reintento en el reemplazo de rol | Sí |
| 2 | Quitar un rol sin asignar otro | Sí |
| 3 | Tenant de los internos: valor y ubicación | Sí (la migración) |
| 4 | Enlace del primer administrador | Sí (la migración) |
| 5 | Reactivación de un usuario | Parcial |
| 6 | Consumidores del hecho de alta/baja | No (afecta el contrato, no la lógica local) |
