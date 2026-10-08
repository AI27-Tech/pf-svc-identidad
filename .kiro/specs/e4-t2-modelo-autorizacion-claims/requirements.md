# Requirements Document

_Modelo de autorización y composición de claims (E4-T2)_

## Introduction

Esta feature cubre la historia técnica **E4-T2 — Modelo de autorización: jerarquía de tenant, alcance y `Audiencia`** del servicio `pf-svc-identidad`. El servicio `identidad` es dueño del **modelo de autorización** de AI27: el catálogo de los ocho roles de DEC-053, la jerarquía de tenant, el alcance efectivo en dos dimensiones (DEC-045 #8) y la derivación de la `Audiencia` de dominio (DEC-054). `identidad` **no decide autorización por petición**: expresa el modelo y **compone claims**. El IdP (on-premise, OIDC; propuesta Keycloak, ADR 32 en estado *Propuesta*) es dueño de usuarios, credenciales, segundo factor, firma y emisión del token.

El alcance de esta feature es el modelo de autorización persistido y la **función pura de composición de claims** que ya existe hoy en el repositorio (`internal/claims`, `internal/autorizacion`). Compone exactamente identidad, tenant, rol y `Audiencia`; **el alcance no viaja en el token** (DEC-053 #2) y se resuelve en servidor. La integración concreta de la composición con el IdP real (empaquetado como *mapper* o extensión del IdP, ADR 32 §4) queda **EN ESPERA de ADR 32** y está fuera del alcance de implementación de esta feature.

Esta feature no inventa reglas de negocio no decididas. Donde existen pendientes de decisión —el contrato de lectura de alcance y sus eventos (DEC-053), y la mecánica del vínculo Monitorista↔`unidades` (DEC-045, notas)— se marcan explícitamente como fuera de alcance, sin resolverlos.

### Fuentes

- `docs/arquitectura/services/identidad.md` (a)–(l)
- `docs/historias-tecnicas/E4-identidad-y-acceso.md` (E4-T2)
- `docs/adr/0032-despliegue-v1-on-premise.md` (ADR 32)
- `docs/adr/0026-revocacion-local-de-sesiones.md` (ADR 26)
- `.kiro/steering/decision-log.md`: DEC-045, DEC-053, DEC-054, DEC-022

## Glossary

- **Modelo_De_Autorizacion**: componente de `identidad` dueño del catálogo de roles, la jerarquía de tenant, el alcance efectivo y la derivación de `Audiencia`. No decide por petición.
- **Composicion_De_Claims**: función pura que, dado el modelo vigente de un sujeto, produce el conjunto de claims de dominio a inyectar en el token. Idéntica en emisión y refresco.
- **sujeto**: entidad de AI27 que correlaciona el identificador externo del IdP con un sujeto interno, dentro de un tenant.
- **rol**: uno de los ocho roles canónicos de DEC-053: `cliente`, `jefe_operativo`, `monitorista`, `supervisor`, `coordinador`, `ejecutivo`, `reaccion`, `administrador`.
- **asignacion_rol**: relación que otorga un `rol` a un `sujeto` dentro de un tenant, con su dimensión de alcance.
- **alcance_tenant**: jerarquía de tenant (padre/hijo), plana en V1 (DEC-022 #2).
- **alcance**: ámbito de visibilidad efectivo de un sujeto, en dos dimensiones (DEC-045 #8): monitoreo por `unidad` y conversación por `cliente`.
- **unidad**: paquete pre-armado de monitoreo (DEC-044); dimensión de alcance del monitoreo.
- **cliente**: organización atendida; dimensión de alcance de la conversación y rol externo.
- **tenant**: espacio de pertenencia de un `sujeto` en la jerarquía de tenant.
- **Audiencia**: claim de dominio (`Cliente` | `Interno`), derivado del rol (DEC-054), separado del claim técnico `aud`.
- **sesion_id**: identificador propio de sesión de AI27, estable durante el refresco (ADR 26).
- **IdP**: proveedor de identidad on-premise con OIDC, dueño de usuarios, credenciales, segundo factor, firma y emisión del token (propuesta Keycloak, ADR 32).
- **IdP_En_Memoria**: doble en memoria del IdP, usado en desarrollo standalone y pruebas mientras ADR 32 no cierra el proveedor real.

## Requirements

### Requisito 1: Modelo del sujeto y su correlación con el IdP

**Historia de Usuario:** Como equipo de desarrollo de `identidad`, quiero un modelo de `sujeto` que correlacione el identificador externo del IdP con el sujeto interno de AI27 y su tenant, para que los demás módulos tengan una identidad de dominio estable independiente del proveedor.

#### Criterios de Aceptación

1. THE Modelo_De_Autorizacion SHALL persistir cada `sujeto` con, como mínimo, su identificador externo del IdP, su identificador interno de AI27 y su `tenant`.
2. IF se intenta registrar un `sujeto` cuyo identificador externo del IdP ya existe, THEN THE Modelo_De_Autorizacion SHALL rechazar el registro por violación de unicidad del identificador externo y no modificar el `sujeto` ya existente.
3. IF se intenta registrar un `sujeto` cuyo par `(tenant_id, sujeto_id)` ya existe, THEN THE Modelo_De_Autorizacion SHALL rechazar el registro por violación de unicidad del par `(tenant_id, sujeto_id)` y no modificar el `sujeto` ya existente.
4. IF una consulta `SELECT` sobre el esquema `identidad` usa el rol de base de datos de otro servicio, THEN THE Modelo_De_Autorizacion SHALL impedir la lectura con el error `permission denied for schema` (Property P1).

### Requisito 2: Catálogo de los ocho roles de DEC-053

**Historia de Usuario:** Como equipo de desarrollo de `identidad`, quiero un catálogo cerrado de los ocho roles de DEC-053, para que las asignaciones de rol solo puedan referirse a roles canónicos del modelo V1.

#### Criterios de Aceptación

1. THE Modelo_De_Autorizacion SHALL reconocer como catálogo de `rol` exactamente los ocho roles `cliente`, `jefe_operativo`, `monitorista`, `supervisor`, `coordinador`, `ejecutivo`, `reaccion` y `administrador`, sin ningún rol adicional y sin entradas duplicadas del mismo identificador de rol.
2. IF una `asignacion_rol` referencia un identificador de rol fuera de los ocho roles de DEC-053, THEN THE Modelo_De_Autorizacion SHALL rechazar la asignación, no persistir la `asignacion_rol` e indicar al solicitante el rechazo por rol fuera del catálogo.
3. THE Modelo_De_Autorizacion SHALL excluir cualquier rol `auditor` del catálogo de `rol` (DEC-036).

### Requisito 3: Asignación de rol con alcance

**Historia de Usuario:** Como equipo de desarrollo de `identidad`, quiero asignar un `rol` a un `sujeto` dentro de un tenant con su dimensión de alcance, para que el modelo exprese qué puede ver cada sujeto sin duplicar asignaciones.

#### Criterios de Aceptación

1. THE Modelo_De_Autorizacion SHALL persistir cada `asignacion_rol` con su `sujeto`, su `rol` y su dimensión de alcance dentro de un tenant.
2. IF se intenta registrar una `asignacion_rol` cuya terna `(sujeto_id, rol_id, alcance)` ya existe, THEN THE Modelo_De_Autorizacion SHALL rechazar la asignación por violación de unicidad de la terna `(sujeto_id, rol_id, alcance)` y no duplicar la asignación existente.
3. THE Modelo_De_Autorizacion SHALL derivar la dimensión de alcance de monitoreo por `unidad` para los roles `monitorista`, `supervisor` y `coordinador` (DEC-045 #8).
4. THE Modelo_De_Autorizacion SHALL derivar la dimensión de alcance de conversación por `cliente` para el rol `ejecutivo` (DEC-045 #8).
5. THE Modelo_De_Autorizacion SHALL derivar el alcance interno global para el rol `administrador` (DEC-045 #6).
6. THE Modelo_De_Autorizacion SHALL tratar la derivación de dimensión de alcance de los roles `jefe_operativo` y `reaccion` como fuera del alcance de esta feature, sin inferir una dimensión para ellos (DEC-045, notas).

### Requisito 4: Jerarquía de tenant plana en V1

**Historia de Usuario:** Como equipo de desarrollo de `identidad`, quiero una tabla `alcance_tenant` para la jerarquía de tenant que exista desde V1 aunque sea plana, para no migrar el modelo cuando V2 introduzca jerarquía real.

#### Criterios de Aceptación

1. THE Modelo_De_Autorizacion SHALL mantener la estructura `alcance_tenant` capaz de expresar que un `tenant` tiene a lo sumo un `tenant` padre.
2. WHILE V1 esté vigente, THE Modelo_De_Autorizacion SHALL operar con la jerarquía de tenant plana, con cero relaciones padre/hijo pobladas (DEC-022 #2).
3. IF se intenta poblar una relación padre/hijo de tenant en V1, THEN THE Modelo_De_Autorizacion SHALL rechazar la operación por jerarquía plana en V1 (DEC-022 #2).

### Requisito 5: Derivación de la `Audiencia` desde el rol

**Historia de Usuario:** Como equipo de seguridad, quiero que la `Audiencia` de dominio se derive del rol y nunca de la petición, para que el portal y el acceso a datos de un sujeto no puedan falsificarse desde el cliente.

#### Criterios de Aceptación

1. IF el `rol` del `sujeto` es `cliente`, THEN THE Composicion_De_Claims SHALL derivar la `Audiencia` con el valor exacto `Cliente`.
2. IF el `rol` del `sujeto` es `jefe_operativo`, `monitorista`, `supervisor`, `coordinador`, `ejecutivo`, `reaccion` o `administrador`, THEN THE Composicion_De_Claims SHALL derivar la `Audiencia` con el valor exacto `Interno`.
3. THE Composicion_De_Claims SHALL derivar la `Audiencia` únicamente a partir del `rol` del `sujeto` (DEC-054, P10).
4. IF una petición incluye un valor de `Audiencia`, THEN THE Composicion_De_Claims SHALL ignorar el valor de la petición y usar la `Audiencia` derivada del `rol`.
5. THE Modelo_De_Autorizacion SHALL mantener la `Audiencia` de dominio separada del claim técnico `aud`.
6. THE Modelo_De_Autorizacion SHALL excluir el claim `aud` de toda decisión de autorización.

### Requisito 6: Composición de claims sin alcance en el token

**Historia de Usuario:** Como equipo de desarrollo de `identidad`, quiero que la composición de claims produzca exactamente identidad, tenant, rol y `Audiencia`, para que el token no congele un formato de alcance antes de que sus reglas estén definidas (DEC-053).

#### Criterios de Aceptación

1. THE Composicion_De_Claims SHALL producir exactamente el claim de identidad del `sujeto`, el `tenant`, un único `rol` y la `Audiencia`, y ningún otro claim de dominio.
2. THE Composicion_De_Claims SHALL excluir del resultado toda referencia a `unidad`, a monitoristas subordinados, a supervisores subordinados y a `cliente` asignado (DEC-053 #2).
3. IF el modelo vigente de un `sujeto` no tiene exactamente un `rol`, THEN THE Composicion_De_Claims SHALL rechazar la composición en vez de emitir un resultado con cero o múltiples roles.

### Requisito 7: Composición idéntica en emisión y refresco

**Historia de Usuario:** Como equipo de seguridad, quiero que la composición de claims en el refresco recomponga desde el modelo vigente igual que en la emisión, para que un cambio de rol tenga efecto en el siguiente refresco y la exposición quede acotada por la vida del token (P11).

#### Criterios de Aceptación

1. WHEN la Composicion_De_Claims recibe el mismo modelo vigente de un `sujeto` en emisión y en refresco, THE Composicion_De_Claims SHALL producir el mismo conjunto y los mismos valores de los claims identidad, `tenant`, `rol` y `Audiencia`.
2. WHEN el `rol` de un `sujeto` cambia en el modelo y se vuelve a componer, THE Composicion_De_Claims SHALL producir el resultado correspondiente al `rol` vigente, sin reutilizar el resultado previo.
3. THE Composicion_De_Claims SHALL recomponer el resultado a partir del modelo vigente en cada ejecución, sin copiar claims de una composición anterior.
4. WHILE un token ya emitido no se refresque, THE Modelo_De_Autorizacion SHALL dejar sin alterar los claims de ese token aunque el `rol` del `sujeto` cambie en el modelo (P11).

### Requisito 8: Propagación de cambios de alcance fuera del token

**Historia de Usuario:** Como equipo de desarrollo de los módulos consumidores, quiero que un cambio de alcance se aplique sin esperar al refresco del token, para que una reasignación de `unidad` a media ruta surta efecto de inmediato (DEC-053).

#### Criterios de Aceptación

1. THE Modelo_De_Autorizacion SHALL resolver el alcance efectivo de un `sujeto` a partir del modelo vigente en servidor, y no a partir de claims contenidos en el token (DEC-053 #2).
2. WHEN cambia el alcance de un `sujeto`, THE Modelo_De_Autorizacion SHALL aplicar el cambio sin reemitir ni refrescar los tokens vigentes del `sujeto` (DEC-053 #3).
3. WHEN cambia el alcance de un `sujeto`, THE Composicion_De_Claims SHALL mantener invariable el conjunto de claims del token (identidad, `tenant`, `rol`, `Audiencia`).
4. IF se resuelve el alcance efectivo mientras existe un token vigente del `sujeto`, THEN THE Modelo_De_Autorizacion SHALL resolverlo contra el modelo en servidor y no tratar el token vigente como fuente de alcance.

> **Nota de alcance (pendiente de decisión, fuera de implementación en esta feature):** el contrato de la lectura de alcance y de los eventos de cambio de alcance queda **pendiente** (DEC-053), porque depende de la mecánica del vínculo Monitorista↔`unidades`, aún **sin definir** (DEC-045, notas). Esta feature deja marcado el lugar de resolución de alcance en servidor; no define tablas, rutas ni eventos de alcance, ni infiere la mecánica del vínculo. El Requisito 8 se verifica sobre la estructura y la separación del token, no sobre un contrato de eventos todavía inexistente.

### Requisito 9: Aislamiento de esquema por rol de base de datos

**Historia de Usuario:** Como equipo de seguridad, quiero que cada servicio solo pueda leer su propio esquema, para que `identidad` no quede expuesto a otros servicios por una ruta de base de datos (Property P1).

#### Criterios de Aceptación

1. THE Modelo_De_Autorizacion SHALL ser dueño del esquema `identidad` y de su rol de base de datos propio.
2. IF un rol de base de datos distinto del rol de `identidad` intenta leer (`SELECT`) cualquier objeto del esquema `identidad`, THEN THE Modelo_De_Autorizacion SHALL impedir la lectura con el error `permission denied for schema` (Property P1).
3. THE Modelo_De_Autorizacion SHALL operar sin otorgar `GRANT` de lectura, uso ni escritura sobre su esquema a ningún rol de otro servicio.
4. WHERE la lectura se hace con el rol propio de `identidad`, THE Modelo_De_Autorizacion SHALL permitir leer los objetos de su esquema `identidad`.

### Requisito 10: Operación standalone con dobles del IdP y auth

**Historia de Usuario:** Como equipo de desarrollo, quiero poder construir y probar el modelo de autorización y la composición de claims en standalone sin el IdP real, para avanzar E4-T2 mientras ADR 32 no cierra el proveedor concreto.

#### Criterios de Aceptación

1. WHERE se ejecuta en modo standalone, THE Modelo_De_Autorizacion SHALL operar contra el IdP_En_Memoria sin importar ningún SDK ni API del proveedor real y sin abrir ninguna conexión de red hacia el IdP real.
2. WHEN la Composicion_De_Claims recibe dos veces el mismo modelo vigente de un `sujeto`, THE Composicion_De_Claims SHALL producir resultados idénticos sin leer ni escribir estado del IdP real ni de ningún recurso externo.
3. WHERE se ejecuta en modo standalone, THE Composicion_De_Claims SHALL ejecutarse hasta producir su resultado sin requerir disponibilidad del IdP real ni credenciales del proveedor concreto.

> **Nota de alcance (EN ESPERA de ADR 32):** el adaptador concreto del IdP y el empaquetado de la composición de claims como *mapper* o extensión del IdP (ADR 32 §4) quedan fuera del alcance de implementación de esta feature. La integración con el IdP real depende de la selección de proveedor en ADR 32 (en *Propuesta*).

## Propiedades de correctitud candidatas

Estas propiedades se derivan de los criterios de aceptación y orientan la fase de diseño. No sustituyen a los requisitos.

1. **Derivación determinista de `Audiencia` (invariante):** para todo `sujeto`, `Audiencia = f(rol)`, donde `cliente → Cliente` y los otros siete roles `→ Interno`. La misma entrada produce siempre la misma `Audiencia` (Requisito 5).
2. **Ausencia de alcance en el token (invariante):** para todo resultado de composición, el conjunto de claims no contiene `unidad`, subordinados ni `cliente` asignado; contiene exactamente identidad, tenant, un rol y `Audiencia` (Requisito 6).
3. **Unicidad de claves del modelo (invariante):** `sujeto` es único por identificador externo del IdP y por `(tenant_id, sujeto_id)`; `asignacion_rol` es única por `(sujeto_id, rol_id, alcance)` (Requisitos 1 y 3).
4. **Aislamiento de esquema por rol de BD (invariante, Property P1):** una lectura del esquema `identidad` con el rol de otro servicio falla con `permission denied for schema` (Requisito 9).
5. **Recomposición idéntica entre emisión y refresco (metamórfica):** para el mismo modelo vigente, componer en emisión y componer en refresco producen resultados iguales; al cambiar el rol en el modelo, el resultado refleja el rol vigente, sin reutilizar el previo (Requisito 7).

## Dependencias y límites

- **Depende de E4-T1** (adaptador del IdP). La parte real del adaptador está EN ESPERA de ADR 32; esta feature usa el IdP_En_Memoria para standalone.
- **Depende de E0-T5** (esquema y rol de base de datos de `identidad`).
- **Fuera de alcance (EN ESPERA de ADR 32):** adaptador concreto del IdP; empaquetado de la composición de claims como *mapper* o extensión del IdP.
- **Fuera de alcance (pendiente de decisión):** contrato de lectura de alcance y sus eventos (DEC-053); mecánica del vínculo Monitorista↔`unidades` y la jerarquía Supervisor→Monitoristas / Coordinador→Supervisores (DEC-045, notas). No se infieren tablas, columnas, rutas ni eventos para estos pendientes.
