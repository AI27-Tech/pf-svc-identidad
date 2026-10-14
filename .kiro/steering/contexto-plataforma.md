---
inclusion: always
---

# AI27 — Fuentes de verdad: contexto y contrato

Antes de usar una decisión, HU, ADR, operación de API o evento en un análisis, spec, diseño o código, hay **dos fuentes de verdad** que verificar al día:

1. **El contexto** vive en `repos/ai27-platform-context` (decisiones, ADR, HU, historias técnicas, arquitectura, glosario).
2. **El contrato** vive en `repos/pf-pkg-contracts` (OpenAPI, eventos, enums generados). Es la fuente única desde la que se generan los tipos de Go y TypeScript.

Hacer la verificación **al inicio de cada tarea** que dependa de estas fuentes, no en cada lectura. Es solo lectura de red: no modifica nada.

---

## 1. Contexto — `repos/ai27-platform-context`

### Regla

Leer de ahí, no de copias. Las copias de `.kiro/steering/` que pueda haber en otros repos o workspaces **pueden estar desactualizadas**. Se comprobó que a una copia le faltaban DEC-067 a DEC-086 (entre ellas DEC-072, DEC-073, DEC-083, DEC-084, DEC-085). Si una copia y el repo de contexto difieren, **prevalece el repo de contexto**.

### Cómo verificar que está al día

Desde `repos/ai27-platform-context`:

```powershell
git fetch
git status -sb
git log --oneline HEAD..origin/main
```

- **Sin salida en el `log`:** está al día. Citar el commit consultado (`git rev-parse --short HEAD`).
- **Hay commits por traer y el árbol está limpio:** `git pull --ff-only`, e indicar al usuario qué cambió si afecta el trabajo.
- **El árbol tiene cambios locales o la rama divergió:** **no hacer pull.** Avisar al usuario y leer con cuidado lo que haya.
- **Sin red:** decirlo, y advertir que el contexto leído puede estar desactualizado.

### Qué leer y dónde

| Necesito | Archivo (relativo a `repos/ai27-platform-context`) |
| --- | --- |
| Panorama rápido, glosario, manifiesto de docs | `HARNESS.md` (bundle generado; **buscar con grep**, no leer completo) |
| Una decisión (DEC-n) | `.kiro/steering/decision-log.md`; las reemplazadas están en `decision-log-historico.md` |
| Lenguaje ubicuo | `CONTEXT.md` |
| Arquitectura y servicios | `docs/arquitectura/` (incluye `services/`) |
| ADR | `docs/adr/` |
| Historias de usuario | `docs/hu/` |
| Historias técnicas | `docs/historias-tecnicas/` |
| Reglas de negocio y estructura del producto | `.kiro/steering/` del repo de contexto |

Si `HARNESS.md` y los archivos fuente difieren, es porque el bundle quedó sin regenerar: **leer el archivo fuente** y avisar.

---

## 2. Contrato — `repos/pf-pkg-contracts`

### Regla

El contrato es la fuente única de las operaciones de API, los eventos y los enums. **No se escribe a mano** un tipo de wire, un evento o un enum que debería venir del contrato (regla 6 del scaffold): si falta, es una **brecha contractual** que se reporta, no se rellena.

`pf-svc-identidad` consume el contrato de dos formas, y hay que mirar las dos:

- **Dependencia por versión exacta en `go.mod`:** `github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go v0.1.0-tracer.N`, **sin `replace`** (regla 1 y 7 del scaffold). Esta es la versión que **compila y se libera**.
- **Copia local vía `go.work`:** el `go.work` de `repos/` une `./pf-pkg-contracts/packages/api-client-go`, así que en desarrollo el código lee el **checkout local** del repo de contratos, no el tag de `go.mod`. Si el checkout local no está en el tag que fija `go.mod`, lo que ves en desarrollo **no es** lo que se libera.

### Cómo verificar que está al día

Desde `repos/pf-pkg-contracts`:

```powershell
git fetch --tags
git status -sb
git tag | Sort-Object -Descending | Select-Object -First 5   # último tracer publicado
git rev-parse --short HEAD                                    # dónde está el checkout
```

Luego, **comparar tres cosas**:

1. **Último tracer publicado** (el tag más alto, p. ej. `v0.1.0-tracer.6`).
2. **Lo que fija `go.mod` de identidad** (`Select-String -Path go.mod -Pattern pf-pkg-contracts` en `repos/pf-svc-identidad`).
3. **Dónde está el checkout local** del repo de contratos (`git status -sb`: rama o tag, árbol limpio o no).

Interpretación:

- **Los tres coinciden y el árbol está limpio:** al día. Citar el tag usado.
- **`go.mod` apunta a un tracer menor que el último publicado:** hay una versión de contrato más nueva que el servicio aún no adoptó. **Avisar al usuario**; no subir la versión de `go.mod` sin que lo pida (puede traer cambios de otras historias). Decir qué cambió entre ambos tracers:
  ```powershell
  git diff --stat v0.1.0-tracer.<viejo> v0.1.0-tracer.<nuevo>
  ```
- **El checkout local está en una rama feature o en un tag distinto del de `go.mod`:** lo que lee `go.work` en desarrollo **no corresponde** a lo que se libera. Avisar, y para leer el contrato que realmente aplica usar el tag de `go.mod`:
  ```powershell
  git show v0.1.0-tracer.<N>:contracts/openapi/identidad.yaml
  ```
- **Sin red:** decirlo, y advertir que el contrato leído puede estar desactualizado.

### Qué leer y dónde

| Necesito | Ruta (relativa a `repos/pf-pkg-contracts`) |
| --- | --- |
| Operaciones de API de un servicio | `contracts/openapi/<servicio>.yaml` (p. ej. `identidad.yaml`) |
| Tipos y formatos comunes | `contracts/openapi/_comunes.yaml` |
| Catálogo de eventos (productor, consumidores, agregado, clave de orden) | `contracts/events/INVENTARIO.md` |
| Definición serializada de un evento | `contracts/events/registro.tipos.yaml` y los `.proto` bajo `contracts/events/` |
| Enum de roles | el que genera `tools/gen-roles` en `packages/api-client-go/roles/` |
| Enum de estados del viaje | `packages/api-client-go/.../estados` |
| Qué cambió entre versiones | `CHANGELOG.md` y `VERSION` |

Para una lectura puntual, preferir **buscar con grep** sobre el YAML del tag de `go.mod` antes que leer el archivo completo.

### Brecha contractual (importante para identidad)

Al 2026-10-16, el contrato de `identidad` (hasta `tracer.6`) solo tiene las cinco operaciones de **sesión** y el único evento `sesion.revocada`. **No** existen `/api/v1/sujetos`, `/api/v1/roles` ni los eventos `sujeto.*`/`rol.*` que E4-T6 necesita. Eso es una brecha abierta: la superficie de gobierno **no se construye con wire** hasta que el contrato la publique. Verificarlo contra el último tag antes de dar por cerrada la brecha, porque puede resolverse en un tracer nuevo.

---

## 3. Decisiones nuevas

- **No** escribir ni hacer commit en `repos/ai27-platform-context` ni en `repos/pf-pkg-contracts` sin que el usuario lo pida: son repos compartidos con flujo de promoción propio.
- Cuando el usuario comunique una decisión, **redactar la entrada DEC propuesta** (estado, owner, fecha, decisión, impacto) y ofrecerla para registro. Mientras no esté registrada, en los documentos se marca como "decidida, pendiente de registro como DEC".
- Antes de proponer un número de DEC, revisar el último del repo de contexto **y** los que haya en el workspace local, para no repetir números.

## 4. Qué señalar siempre

- Si un DEC está en estado `PENDIENTE` en el repo de contexto y se trabaja como si estuviera decidido.
- Si el mismo número de DEC significa cosas distintas en dos lugares.
- Si una fuente local contradice al repo de contexto.
- Si el `go.mod` del servicio apunta a un tracer **anterior** al último publicado.
- Si el checkout local del repo de contratos **no** está en el tag que fija `go.mod` (lo de desarrollo no es lo que se libera).
- Si se necesita una operación o evento que **no existe** en el contrato (brecha contractual): reportarla, no inventarla.
