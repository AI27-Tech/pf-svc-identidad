# Reporte de brechas contractuales y de arquitectura — scaffold de `identidad`

> Entregable de la "Definición de listo" de `03-identidad.md`. Lista lo que el
> scaffold necesitaba y **no** está en la versión fijada del contrato, más una
> brecha del tooling de arquitectura detectada al correr `arch-test`. Ninguna se
> rellenó a mano (regla 6 de `00-indice.md`): se reportan.
>
> **Versiones fijadas:** `pf-pkg-contracts/packages/api-client-go` `v0.1.0-tracer.3`,
> `pf-pkg-platform-go` `v0.1.0`. Verificado el 2026-10-06.

## 1. Lo que SÍ estaba (y se cableó)

Para contraste con el estado que `03-identidad.md` describía sobre `8f29008`
(release retirada `tracer.1`). En `tracer.3` ya está publicado:

- **Enum `Rol` con los ocho roles de DEC-053** — `api-client-go/roles`:
  `cliente, jefe_operativo, monitorista, supervisor, coordinador, ejecutivo,
  reaccion, administrador`. El desalineamiento de cinco roles de `8f29008`
  quedó corregido.
- **Las cinco operaciones de sesiones** — `api-client-go/identidad`:
  `emitirSesion`, `refrescarSesion`, `revocarSesion`, `obtenerSesion`,
  `listarSesionesRevocadas`, con sus tipos (`SesionEmitida`, `EstadoSesion`,
  `Revocacion`, `PaginaRevocaciones`, `CredencialesEntrada`, `RefrescoEntrada`,
  `Error`).
- **Evento `sesion.revocada`** — `eventos/ai27/eventos/v1` (`SesionRevocada`:
  `sesion_id`, `revocada_en`, `expira_en`), registrado en
  `registro.tipos.yaml` e `INVENTARIO.md` (agregado `sesion_id`, amplia).
- **Tipos Go generados** — `api-client-go` está publicado (ya no es el problema
  de `8f29008`, donde el tag retirado sólo traía `estados/`).

## 2. Brechas contractuales (FALTA) — reportadas, no cableadas

| Superficie | Fuente de la necesidad | Estado |
|---|---|---|
| `POST/DELETE /api/v1/sujetos/...` (alta, baja) | identidad.md (f), superficie admin | **FALTA** en `api-client-go/identidad` |
| `/api/v1/roles/...` (asignación/revocación de rol con alcance) | identidad.md (f); DEC-045 #8 | **FALTA** |
| Eventos `sujeto.creado` · `sujeto.desactivado` · `rol.asignado` · `rol.revocado` | identidad.md (g); `mensajeria` proyecta su pool desde ellos | **FALTA**: no hay `.proto` ni entrada en `INVENTARIO.md` / `registro.tipos.yaml` |
| Lectura de alcance (unidad/cliente) y eventos de cambio de alcance | DEC-045 #8; DEC-053 | **FALTA**: el alcance no viaja en el token; su forma depende de la mecánica de vínculo aún pendiente (DEC-045, Notas) → `TODO(DEC-045)` |

**Efecto en el scaffold:** `internal/gobierno/port.go` queda con su puerto
**vacío** y `TODO(contrato)`; no se montan rutas ni filas de `outbox` para esos
eventos. El vínculo Monitorista↔`unidades` **no** se modela (ni tabla ni
columna): `internal/autorizacion/port.go` y `migrations/.../0001_init.sql` lo
dejan como `TODO(DEC-045)`.

## 3. Brecha de nombre (DESALINEADO) — reportada, usada tal cual

| Elemento | Estado | Acción |
|---|---|---|
| Esquema de seguridad `bearerCognito` | `_comunes.yaml` lo llama `bearerCognito`; el generado expone `identidad.BearerCognitoScopes` | Es un identificador de contrato: se usa tal cual mientras exista. Se **reporta** como brecha de nombre (debería ser neutral respecto del proveedor; V1 no usa Cognito, DEC-062). No se renombra desde este repo |

## 4. Brecha del tooling de arquitectura (`arch-test`) — BLOQUEANTE del CI

Al correr `go run github.com/AI27-Tech/pf-pkg-platform-go/tools/arch-test@v0.1.0`
sobre `identidad`, falla con:

```
arch-test: violaciones de arquitectura:
  - identidad importa el cliente de "roles", que no es una arista declarada de "identidad"
```

**Diagnóstico (causa raíz):** `identidad` importa `api-client-go/roles` para el
enum de los ocho roles. Eso es **correcto y prescrito**: el propio código
generado (`roles.gen.go`) dice que el enum "viaja en el TOKEN, no en la
superficie HTTP. Los servicios importan este paquete", igual que `estados` y
`eventos`. El problema es del tool:

- `reglas.yaml` declara `shared_packages: [estados, eventos]` (paquetes
  importables por cualquiera, que **no** son aristas), pero **omite `roles`**.
- Además, `tools/arch-test/verificar.go` (función `importsDeClientes`) trata
  **cualquier** subpaquete bajo `api-client-go/` como una arista y **nunca
  consulta `shared_packages`**: el campo se parsea en `reglas.go`
  (`Reglas.SharedPackages`) pero no se usa. Es decir, hoy un servicio que importe
  `estados` o `eventos` también sería marcado, pese a estar declarados como
  compartidos.

**Qué NO se hizo (regla 6/10, "stop and report"):** no se modificó `reglas.yaml`
ni el tool de `pf-pkg-platform-go` desde este repo —es otro repo, con su propia
autoridad y es materia de `01-cimientos`— ni se evitó el import correcto de
`roles` reescribiendo el enum a mano (eso violaría la regla de "contrato
primero").

**Arreglo propuesto para `01-cimientos` / `pf-pkg-platform-go` (a decidir ahí):**
una de las dos, idealmente ambas:
1. Agregar `roles` a `shared_packages` en `reglas.yaml` (es un dato de dominio
   del token, no una arista), y
2. Hacer que `verificar.go` **honre `shared_packages`**: excluir esos
   subpaquetes al derivar aristas de `importsDeClientes` (hoy el campo es letra
   muerta; `estados`/`eventos` sólo "pasan" porque ningún servicio del scaffold
   los ha importado todavía).

Hasta que eso se resuelva, el paso `arch-test` del CI de `identidad` fallará por
esta causa, **no** por una arista real indebida: `identidad` sigue siendo hoja
(`identidad: []`), sin aristas salientes hacia ningún `pf-svc-*`.

## 5. En espera de ADR 32 (no es brecha de contrato)

No bloquean el scaffold, pero sí la implementación real:

- Adaptador concreto del IdP (`internal/idp/internal/`) y el artefacto de
  composición de claims (mapper/extensión del IdP) con su empaquetado.
- No se eligió producto ni se importó SDK/API de administración de ninguno
  (DEC-062: sin AWS ni Cognito).
