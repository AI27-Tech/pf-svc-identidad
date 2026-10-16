# Design Document

**E4-T6 — Gobierno y traza de cambios administrativos (`identidad`)** · Documento de diseño

| | |
| --- | --- |
| **Estado** | Borrador — cubre lo decidido (G-1 a G-11, C1–C6). El wire (endpoints, eventos serializados) espera el contrato (G-2); las semillas esperan tres datos de tercero |
| **Requisitos** | `requirements.md` de este spec (R1–R13) |
| **Repositorio / rama** | `repos/pf-svc-identidad` · `feature/e4-t3-event-snapshot` |
| **Contexto consultado** | `ai27-platform-context` @ `bd8901d`; contrato `pf-pkg-contracts` tag `v0.1.0-tracer.6` |
| **Fecha** | 2026-10-16 |

## Overview

E4-T6 añade a `identidad` la **superficie de gobierno**: alta y desactivación de sujetos, asignación y revocación de rol (siempre como reemplazo), y la traza inmutable de esos cambios. Cada cambio se compromete en una sola transacción de Postgres (estado + traza + outbox) y luego se sincroniza con el IdP de forma reintentable. Además entrega el `SujetoResolver` productivo (`IdpSubject → SujetoID`) que desbloquea el login de E4-T3.

El diseño sigue el patrón que ya usa `internal/sesion`: un servicio no exportado que reúne colaboradores inyectados detrás de puertos, con la frontera transaccional explícita y el publicador de outbox en la misma `tx`. No se inventa estructura: se reusa `BeginTx`, `outbox.Publisher`, `autorizacion.SujetoRepo` y `autorizacion.AsignacionRolRepo`, extendiéndolos con lo que falta.

Dos afirmaciones gobiernan el diseño:

1. **Local manda.** La fuente de verdad es Postgres; el IdP se sincroniza después y de forma idempotente (DEC-085, C1). Un fallo del IdP no revierte el commit local.
2. **El `sub` del IdP es opaco** (DEC-072). El `SujetoID` de AI27 se resuelve en servidor por `idp_subject`; nunca se asume `sub == SujetoID`.

### Qué está construido hoy (base del diseño)

| Pieza | Estado |
| --- | --- |
| `internal/gobierno/port.go` | `type Gobierno interface{}` vacío. Este diseño lo puebla |
| `autorizacion.SujetoRepo` | `Crear`, `PorIdpSubject`. Faltan `Desactivar`, `Reactivar`, `PorID` |
| `autorizacion.AsignacionRolRepo` | `Asignar` (con validación en código, no constraint), `ActivasDe`. Faltan `Revocar` y el reemplazo atómico |
| `autorizacion.ModeloVigente`, `CargadorModeloVigente` | Existen; el resolver de sujeto los puede reusar |
| `internal/sesion` (`BeginTx`, `outbox.Publisher`, patrón de dos pasos) | Existe; el gobierno copia el patrón transaccional de `revocarLocalConEvento` |
| `internal/idp.IdentityProvider` | `Issue`, `Refresh`, `Revoke`. Falta administración de usuarios y grupos |
| Migraciones | `0001` (tablas), `0002` (grants + semilla de roles). `0003` (de T3) añadió `idp_session_id`. Las de T6 son `0004`+ |

## Architecture

```
                    API_Gobierno (handlers HTTP)         [DIFERIDO: espera contrato G-2]
                              │
                              ▼
   ┌──────────────────────────────────────────────────┐
   │  internal/gobierno — Servicio_Gobierno            │
   │                                                    │
   │  Alta · Desactivar · Reactivar · Reemplazar rol    │
   │  Consultar · ResolverSujeto                        │
   └───┬───────────────┬───────────────┬───────────────┘
       │               │               │
       ▼               ▼               ▼
  BeginTx (tx)   IdpAdmin (puerto)  outbox.Publisher
       │               │               │ (misma tx)
       ▼               ▼               ▼
  SujetoRepo      Keycloak /       identidad.outbox
  AsignacionRol   IdP_En_Memoria
  TrazaRepo
  TenantRepo
       │
       ▼
  Postgres (schema identidad)
```

- **La capa HTTP (API_Gobierno) no se construye todavía:** no hay operaciones en el contrato (G-2). El diseño define el servicio y sus puertos; los handlers se cablean cuando el contrato publique `/sujetos` y `/roles`, siguiendo el patrón de `internal/http/handlers.go`.
- **El `Servicio_Gobierno` vive en `internal/gobierno`**, junto a su puerto, igual que `internal/sesion` vive junto al suyo.
- **El orden IdP ↔ local lo decide el tipo de operación** (C1/G-3), encapsulado en el servicio, no en los repos.

## Components and Interfaces

### `internal/gobierno` — el servicio

Se puebla el puerto hoy vacío. "Accept interfaces, return structs": el servicio acepta los puertos que consume y se construye con `NewServicio`, como `internal/sesion`.

```go
// Gobierno es la superficie de gobierno de identidad: alta/baja de sujetos,
// reemplazo de rol y la correlación IdpSubject → SujetoID. Local manda; el IdP
// se sincroniza después y de forma idempotente (DEC-085).
type Gobierno interface {
    // AltaSujeto crea el usuario en el IdP y el sujeto local con su rol inicial
    // (Otorgamiento: IdP primero). Publica sujeto.creado y rol.asignado.
    AltaSujeto(ctx context.Context, in AltaSujeto) (Sujeto, error)

    // DesactivarSujeto marca el sujeto inactivo (Retiro: local primero),
    // revoca su sesión viva y publica sujeto.desactivado.
    DesactivarSujeto(ctx context.Context, in CambioEstado) error

    // ReactivarSujeto marca el sujeto activo de nuevo (Otorgamiento) y publica
    // sujeto.reactivado (C5).
    ReactivarSujeto(ctx context.Context, in CambioEstado) error

    // ReemplazarRol revoca el rol activo y asigna el nuevo en una sola
    // transacción local (C1); dos filas de traza, dos hechos. Es la ÚNICA vía
    // para cambiar el rol: no existe revocar suelto (C2).
    ReemplazarRol(ctx context.Context, in ReemplazoRol) error

    // AsignarRolInicial asigna un rol a un sujeto SIN rol activo (parte del
    // alta, o un sujeto que nunca tuvo rol). Rechaza si ya tiene uno (usar
    // ReemplazarRol).
    AsignarRolInicial(ctx context.Context, in AsignacionInicial) error
}

// Consulta es la lectura de gobierno (Requisito 10). Puerto separado por
// lectura/escritura; el acceso lo restringe la capa HTTP a administrador.
type Consulta interface {
    SujetoPorID(ctx context.Context, sujetoID string) (SujetoDetalle, error)
    HistorialDe(ctx context.Context, sujetoID string, cursor string) (PaginaHistorial, error)
}
```

El **actor** y la **autorización** no son parámetros del dominio: llegan del contexto HTTP (claims verificados) y el handler los valida antes de invocar al servicio (Requisito 9). El servicio recibe el `ActorID` ya resuelto dentro de cada `in`.

### Puerto del IdP para administración

`idp.IdentityProvider` crece con las operaciones que E4-T1 difirió (A-5 de su spec). Interfaz pequeña, en el consumidor:

```go
// IdpAdmin administra usuarios y grupos en el IdP. Vive junto al puerto del IdP
// (internal/idp). Lo implementan el adaptador de Keycloak y el doble en memoria;
// ambos cumplen la suite de contrato idptest (Requisito 12.1).
//
// Usa una cuenta de servicio SEPARADA de la que revoca sesiones (C4/G-4,
// DEC-073): la de revocación no tiene manage-users y no se amplía.
type IdpAdmin interface {
    // CrearUsuario da de alta al usuario con su atributo tenant_id (inmutable
    // por el usuario, E4-T1) y lo mete en el grupo del rol. Devuelve el sub
    // opaco que el IdP asigna (DEC-072). Idempotente por idp_subject.
    CrearUsuario(ctx context.Context, u NuevoUsuario) (idpSubject string, err error)

    // DeshabilitarUsuario deja al usuario sin poder autenticarse (G-6).
    DeshabilitarUsuario(ctx context.Context, idpSubject string) error

    // HabilitarUsuario revierte lo anterior (reactivación, C5).
    HabilitarUsuario(ctx context.Context, idpSubject string) error

    // MoverDeGrupo quita al usuario del grupo del rol anterior y lo pone en el
    // nuevo, en ese orden, para no exponerlo en dos grupos a la vez (DEC-084,
    // Requisito 7.6). rolAnterior vacío = solo agregar (asignación inicial).
    MoverDeGrupo(ctx context.Context, idpSubject, rolAnterior, rolNuevo string) error
}
```

> **Nota de aislamiento (identidad.md (d), (l)).** Ningún paquete fuera de `internal/idp` importa el SDK del proveedor. `IdpAdmin` es parte de ese límite.

### Resolver de sujeto — `SujetoResolver` productivo

El `SujetoResolver` que `internal/sesion/seams.go` declara se implementa aquí (Requisito 11; DEC-072 opción c):

```go
// resolverSujeto implementa sesion.SujetoResolver resolviendo el sub opaco del
// token al sujeto de AI27 por idp_subject (DEC-072). No hay claim sujeto_id ni
// alineación de ids. Vive en gobierno porque la correlación es gobierno de
// usuarios (DEC-072 "vive con el alta/sincronización de E4-T6").
type resolverSujeto struct {
    sujetos autorizacion.SujetoRepo
}

func (r resolverSujeto) ResolveSujeto(ctx context.Context, tok idp.Token) (sesion.SujetoRef, error) {
    sub := subDelToken(tok) // ver nota de diseño abajo
    s, ok, err := r.sujetos.PorIdpSubject(ctx, sub)
    if err != nil {
        return sesion.SujetoRef{}, fmt.Errorf("resolver sujeto: %w", err)
    }
    if !ok {
        return sesion.SujetoRef{}, ErrSujetoUnresolved // centinela de E4-T3
    }
    if !s.Activo {
        return sesion.SujetoRef{}, ErrSujetoUnresolved // sujeto desactivado no resuelve (G-6)
    }
    return sesion.SujetoRef{SujetoID: s.ID, TenantID: s.TenantID}, nil
}
```

> **Nota de diseño abierta — `subDelToken`.** `idp.Token` hoy **no expone el `sub`** (solo `AccessToken`, `RefreshToken`, `ExpiraEn`, `IdpSessionID`). El `sub` vive dentro del `AccessToken` como claim, que cada servicio verifica con `pf-pkg-platform-go/auth`. Opciones para obtenerlo, a decidir en la implementación (ninguna reabre una decisión de producto):
> - (i) Que `idp.Token` exponga el `sub` que el adaptador ya lee del grant.
> - (ii) Que el resolver verifique el `AccessToken` con `auth` y lea `Claims.Subject`.
> La opción (ii) no toca el puerto del IdP pero duplica verificación; la (i) es un cambio aditivo de un campo. Se resuelve al cablear el login de E4-T3, que es quien consume el resolver.

### Reutilización de los repos existentes

`SujetoRepo` y `AsignacionRolRepo` crecen con lo que falta, sin romper lo que hay:

```go
// Añadidos a autorizacion.SujetoRepo
type SujetoRepo interface {
    Crear(ctx context.Context, s Sujeto) (Sujeto, error)       // existe
    PorIdpSubject(ctx context.Context, idpSubject string) (Sujeto, bool, error) // existe
    PorID(ctx context.Context, id string) (Sujeto, bool, error)                 // nuevo
    FijarActivo(ctx context.Context, tx pgx.Tx, id string, activo bool) error   // nuevo (desactivar/reactivar)
}

// Añadidos a autorizacion.AsignacionRolRepo
type AsignacionRolRepo interface {
    ActivasDe(ctx context.Context, sujetoID string) ([]AsignacionRol, error)    // existe
    // Reemplazar revoca la activa (si hay) y crea la nueva en la MISMA tx (C1).
    // Es la vía única de cambio de rol (C2). Devuelve ErrAsignacionDuplicada si
    // el rol nuevo es el mismo que ya tiene activo.
    Reemplazar(ctx context.Context, tx pgx.Tx, in Reemplazo) error              // nuevo
    // AsignarInicial crea la asignación cuando el sujeto no tiene rol activo.
    AsignarInicial(ctx context.Context, tx pgx.Tx, a AsignacionRol) error       // nuevo
}
```

> **Cambio respecto de hoy.** `Asignar` valida duplicados **en código** leyendo `ActivasDe` ("Decisión B" del comentario actual). Con C2/G-5 (un solo rol activo por constraint), esa validación pasa a la base de datos (ver Data Models) y los métodos nuevos reciben la `tx` para componer el reemplazo atómicamente, igual que `revocarLocalConEvento` recibe la suya.

### Traza y tenants — repos nuevos

```go
// TrazaRepo escribe en cambio_identidad. Solo inserción (0002: sin UPDATE/DELETE).
type TrazaRepo interface {
    Registrar(ctx context.Context, tx pgx.Tx, c CambioAdministrativo) error
    Historial(ctx context.Context, sujetoID, cursor string) (PaginaHistorial, error)
}

// TenantRepo lee/siembra tenants (C3). Nuevo: hoy no hay tabla de tenants.
type TenantRepo interface {
    Existe(ctx context.Context, tenantID string) (bool, error)
}
```

## Data Models

Migraciones nuevas, cada una idempotente, sin tocar `0001`–`0003`, cada objeto con sus `GRANT` y su caso en `grants_integration_test.go` (nota de `0002`).

### `0004` — índice de la traza (Requisito 1.7)

```sql
-- Índice por (sujeto afectado, tiempo) para "quién otorgó este acceso y cuándo".
CREATE INDEX IF NOT EXISTS cambio_identidad_sujeto_ts_idx
  ON identidad.cambio_identidad (sujeto_id, ocurrido_en DESC);
```
Sin `GRANT` nuevos (un índice hereda los privilegios de la tabla).

### `0005` — un solo rol activo por sujeto (Requisito 4.5, C2/G-5)

```sql
-- Un sujeto tiene A LO SUMO un rol activo. Más estricto que único por
-- (sujeto_id, rol_codigo): refleja que el token lleva un solo rol (DEC-053).
-- Reemplaza la validación en código de AsignacionRolRepo.Asignar.
CREATE UNIQUE INDEX IF NOT EXISTS asignacion_rol_un_activo_idx
  ON identidad.asignacion_rol (sujeto_id) WHERE revocado_en IS NULL;
```
La violación (`23505`) la mapea el repo a `ErrAsignacionDuplicada` o a un error de "ya tiene rol" según el flujo.

### `0006` — tabla de tenants y FK (Requisito 13, C3)

> **Nota de implementación (2026-10-16).** El archivo real `0006_tenant.sql`
> contiene **solo ESTRUCTURA** (tabla + FK + grants), **sin la semilla**. La FK se
> agrega como **`NOT VALID`** dentro de un bloque `DO $$ … $$` idempotente (no
> plana como se muestra abajo), para no fallar con filas de `sujeto` previas; su
> validación va aparte. La **semilla** del tenant interno (el `INSERT` de abajo)
> **NO vive en `0006`**: va en una migración de semilla separada, pendiente del
> UUID real de Arquitectura. El bloque de abajo refleja el archivo real.

```sql
CREATE TABLE IF NOT EXISTS identidad.tenant (
  id          UUID        PRIMARY KEY,
  nombre      TEXT        NOT NULL,
  tipo        TEXT        NOT NULL CHECK (tipo IN ('interno', 'cliente')),
  creado_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- sujeto.tenant_id referencia la tabla (integridad referencial, C3). NOT VALID
-- para no fallar con filas previas; la validación va aparte. En el archivo real
-- esto vive dentro de un bloque DO $$ … $$ idempotente.
ALTER TABLE identidad.sujeto
  ADD CONSTRAINT sujeto_tenant_fk FOREIGN KEY (tenant_id)
  REFERENCES identidad.tenant (id) NOT VALID;

GRANT SELECT, INSERT ON identidad.tenant TO svc_identidad;
```
El tenant de cada cliente también vive aquí (`tipo = 'cliente'`), coherente con que "el cliente determina su tenant" (E4-HU-018) y con que el tenant vive en `identidad`, no en `catalogos` (Requisito 13.5).

> **Dato de tercero.** `<<UUID_TENANT_AI27>>` es un placeholder. La migración de
> **semilla del tenant** —no `0006`, que es solo estructura— **no se da por lista**
> hasta tener el UUID de Arquitectura. La FK es segura solo si el tenant de cada
> sujeto ya existe en la tabla; la semilla del tenant interno va antes que la del
> primer admin.
>
> ```sql
> -- Semilla del tenant interno (migración de semilla aparte, NO 0006).
> INSERT INTO identidad.tenant (id, nombre, tipo)
> VALUES ('<<UUID_TENANT_AI27>>', 'AI27 interno', 'interno')
> ON CONFLICT (id) DO NOTHING;
> ```

> ## ⚠️ PENDIENTE DE DECISIÓN — numeración de las migraciones de semilla
>
> Hay un desacuerdo de numeración sin resolver, y **la elección no es de Kiro**:
> - El comentario de `0006_tenant.sql` (real) dice que la semilla del tenant va en
>   **`0007_tenant_seed.sql`**.
> - Este `design.md` (abajo) llama **`0007`** a la semilla del **primer administrador**.
> - **Ninguno de los dos `0007` existe todavía como archivo.**
>
> Hay que decidir cómo se reparten las semillas en archivos: ¿`0007` = tenant y
> `0008` = primer admin?, ¿un solo `0007` con ambas?, ¿otro orden? Quien lo decida
> debe dejar `0006_tenant.sql`, este `design.md` y `tasks.md` (B6/B7/B8) apuntando
> al **mismo** esquema de numeración. Hasta entonces, las referencias a `0007` en
> estos documentos están **sin reconciliar**.

### `0007` — primer administrador (Requisito 13, C4)

```sql
-- Primer administrador por semilla. Usa el sub FIJO del admin que trae el
-- realm importado de Keycloak (C4) y el identificador de actor de sistema para
-- la traza (Requisito 1.9). Ambos son datos de tercero: placeholders.
INSERT INTO identidad.sujeto (id, idp_subject, tenant_id, activo)
VALUES ('<<UUID_ADMIN_BOOTSTRAP>>', '<<SUB_ADMIN_REALM>>', '<<UUID_TENANT_AI27>>', true)
ON CONFLICT (idp_subject) DO NOTHING;

INSERT INTO identidad.asignacion_rol (sujeto_id, rol_codigo, asignado_por)
VALUES ('<<UUID_ADMIN_BOOTSTRAP>>', 'administrador', '<<ACTOR_SISTEMA>>')
ON CONFLICT DO NOTHING;

INSERT INTO identidad.cambio_identidad (tipo, sujeto_id, tenant_id, actor_id, detalle)
VALUES ('sujeto.creado', '<<UUID_ADMIN_BOOTSTRAP>>', '<<UUID_TENANT_AI27>>', '<<ACTOR_SISTEMA>>', '{"bootstrap":true}'::jsonb);
```

> **Datos de tercero (bloquean solo esta migración):** `<<SUB_ADMIN_REALM>>` (Arquitectura/Operaciones, al construir el realm), `<<ACTOR_SISTEMA>>` (identificador fijo de sistema), `<<UUID_ADMIN_BOOTSTRAP>>`. Hasta tenerlos, `0007` queda redactada pero no se aplica.

### Formas de dominio

| Tipo | Campos | Nota |
| --- | --- | --- |
| `CambioAdministrativo` | `Tipo`, `SujetoID`, `TenantID`, `ActorID`, `Detalle`, `OcurridoEn` | Mapea a `cambio_identidad`. `Tipo` usa el nombre canónico del hecho (K-1) |
| `Reemplazo` | `SujetoID`, `RolAnterior`, `RolNuevo`, `ActorID` | Entrada del reemplazo atómico |
| `AltaSujeto` | `Nombre`, `IdentificadorUsuario`, `Rol`, `OrganizacionCliente` (cond.), `ActorID` | Campos de E4-HU-018; la forma de wire la fija el contrato (G-2) |

## Flujo transaccional (el corazón del diseño)

Todos los cambios publicables siguen el patrón de `revocarLocalConEvento` (una `tx`, estado + traza + outbox juntos), más la sincronización del IdP según el tipo.

### Reemplazo de rol (C1 — el caso que motivó la cuestión)

```
1. Validar actor (handler): es administrador. Si no, 403 sin tocar nada.
2. Cargar rol activo del sujeto. Si el nuevo == actual → ErrAsignacionDuplicada.
3. tx := Begin
     - AsignacionRol.Reemplazar(tx): revoca la activa + crea la nueva
     - Traza.Registrar(tx): fila rol.revocado  (una)
     - Traza.Registrar(tx): fila rol.asignado  (otra)
     - outbox.Publish(tx): rol.revocado
     - outbox.Publish(tx): rol.asignado
   tx.Commit            ← aquí "ocurrió" (local manda)
4. IdpAdmin.MoverDeGrupo(sub, rolAnterior, rolNuevo)   ← reintentable, máx 3
     - quita grupo anterior ANTES de agregar el nuevo (no dos grupos, 7.6)
     - si falla tras 3 intentos: el cambio local NO se revierte (C1);
       se devuelve al cliente un error de "sincronización pendiente"
```

El token del sujeto conserva el rol anterior hasta el siguiente refresco (Requisito 4.10; DEC-031 #2): el rol efectivo se resuelve en servidor y el token lleva uno solo, así que la ventana en el IdP no abre un hueco de autorización.

### Alta (Otorgamiento: IdP primero)

```
1. Validar actor administrador. Validar rol en catálogo. Si cliente, exigir organización.
2. sub := IdpAdmin.CrearUsuario(usuario, tenant_id, grupo del rol)   ← IdP primero
3. tx := Begin
     - SujetoRepo.Crear(sub, tenant_id, activo)
     - AsignacionRol.AsignarInicial(tx)
     - Traza: sujeto.creado + rol.asignado  (dos filas, G-8)
     - outbox: sujeto.creado + rol.asignado
   tx.Commit
```
Si el paso 3 falla tras crear el usuario en el IdP, el usuario queda creado pero sin sujeto local: es el caso de compensación. Por simetría con DEC-085 (que descartó compensar sobre dos sistemas), **se prefiere** que el alta registre primero un sujeto provisional y complete con el `sub`; esta variante se evalúa en la implementación y se nota aquí como punto a cerrar, sin reabrir G-3.

### Desactivación (Retiro: local primero)

```
1. Validar actor administrador.
2. tx := Begin
     - SujetoRepo.FijarActivo(tx, id, false)
     - Traza: sujeto.desactivado
     - outbox: sujeto.desactivado
     - revocar la sesión viva del sujeto (si hay) EN LA MISMA tx (K-6):
       reusa el Paso 1 de internal/sesion para marcar revocada_en + outbox
   tx.Commit            ← acceso cortado en local
3. IdpAdmin.DeshabilitarUsuario(sub)      ← reintentable
4. (Paso 2 de la sesión) IdpAdmin/Revoke de la sesión en el IdP     ← reintentable
```
Corta el acceso HTTP/SSE en ≤30 s desde el commit (Requisito 3.4; una sesión viva por sujeto, A-2, así que no hace falta revocación masiva).

## Correctness Properties

Derivadas de los requisitos; orientan las pruebas (PBT con `testing/quick`, igual que los specs hermanos).

1. **Atomicidad del cambio (P17).** Para todo cambio publicable, estado + traza + outbox se comprometen juntos: si la `tx` falla, no queda ninguno de los tres (Requisito 6). Prueba con `FaultyBeginTx` / fallo inyectado.
2. **Un solo rol activo (invariante).** Para toda secuencia de reemplazos, el sujeto tiene exactamente una `asignacion_rol` activa; el constraint `0005` rechaza un segundo activo (Requisito 4.5).
3. **Reemplazo = dos filas, dos hechos.** Todo reemplazo exitoso deja exactamente una fila `rol.revocado` y una `rol.asignado` en la traza, y dos eventos en outbox (Requisitos 4.3, 1.1).
4. **Traza inmutable.** `UPDATE`/`DELETE` sobre `cambio_identidad` con `svc_identidad` falla por privilegio (Requisito 1.6). Prueba de grants.
5. **Resolver fiel y fail-safe.** `ResolveSujeto` devuelve el `(SujetoID, TenantID)` del `sujeto` cuyo `idp_subject` es el `sub`, o `ErrSujetoUnresolved` si no existe o está inactivo (Requisito 11.3–11.4).
6. **Local manda tras fallo del IdP.** Si la sincronización del IdP falla tras agotar reintentos, el estado local del cambio permanece comprometido (Requisito 7.7, C1).
7. **No autoescalamiento.** Un no administrador que intenta cambiar su propio rol es rechazado antes de tocar el IdP y sin escribir en ningún almacén (Requisito 9.2). Un administrador que es el último activo no puede quitarse el rol ni desactivarse (Requisito 9.3).

## Decisiones de diseño abiertas (no reabren producto)

- **`subDelToken`** (cómo el resolver obtiene el `sub`): opción (i) exponerlo en `idp.Token`, u (ii) verificar el `AccessToken` con `auth`. Se cierra al cablear el login de E4-T3.
- **Compensación del alta** si el paso local falla tras crear el usuario en el IdP: compensar en el IdP o preferir sujeto provisional (simetría con DEC-085). A cerrar en implementación; no cambia G-3.
- **Datos de tercero en las migraciones `0006`/`0007`:** UUID del tenant interno, `sub` fijo del admin, identificador de actor de sistema. Las migraciones quedan redactadas con placeholders; no se aplican hasta tenerlos.

## Lo que este diseño NO cubre

- **Capa HTTP** (`/api/v1/sujetos`, `/api/v1/roles`) y **serialización de eventos**: esperan el contrato (G-2). El diseño deja el servicio y los puertos listos para cablearlos.
- **Alcance por `unidad`/`cliente`** y su edición: vínculo de DEC-045 sin definir.
- **Login/emisión** de E4-T3 Parte B: este spec solo entrega el resolver.
- **Consumo** de los hechos por `mensajeria`/`reportes`/`catalogos`: otros servicios (C6 a confirmar).

## Verificación

Por `golang-best-practices.md`, cada tarea de código cierra con `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...` y `go test ./...`. Las pruebas con Postgres se omiten con `t.Skip` sin `IDENTIDAD_TEST_DB` (comportamiento declarado como no verificado, no como verde). Las de Keycloak (`-tags integration`) necesitan Docker y el JAR de E4-T1; sin ellos solo se comprueba que compilan.
