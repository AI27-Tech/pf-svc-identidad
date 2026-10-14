---
inclusion: always
---

# AI27 identidad — Trabajar con eficiencia de contexto

El objetivo de este archivo es gastar el **menor contexto posible** (y, con eso, menos créditos) sin perder rigor. En este repo los documentos de spec y el `decision-log` son grandes y se truncan al leerlos completos; releerlos entero es el gasto más común y más evitable.

La regla general: **leer lo mínimo necesario, una sola vez, con la herramienta más barata.** Preferir una búsqueda dirigida a una lectura completa, y confiar en lo que un documento ya afirma antes que reconfirmarlo leyendo más.

---

## 1. No leer archivos grandes completos

Estos archivos superan el límite de lectura y se truncan; leerlos entero quema contexto para traer texto que no se usa:

- `.kiro/specs/*/tasks.md`, `requirements.md`, `design.md`
- `.kiro/specs/*/DECISIONES-*.md`, `CUESTIONES-ABIERTAS.md`
- `repos/ai27-platform-context/.kiro/steering/decision-log.md`
- `repos/ai27-platform-context/HARNESS.md`
- `CHANGELOG.md`, `REPORTE-BRECHAS.md`

En su lugar:

- **Buscar con `grep_search`** el símbolo, la tarea (`B10.1`, `R4.2`), la decisión (`DEC-088`, `A-5`) o el término exacto. El resultado trae las líneas con contexto; casi siempre es suficiente.
- Si hace falta leer, usar `read_file` con `offset`/`limit` acotado a la sección que el grep señaló, no el archivo entero.
- Para código, preferir `read_code` con un `selector` (p. ej. `servicio.EmitirSesion`) antes que leer el archivo completo.

---

## 2. Confiar en lo que el documento ya afirma

Antes de leer para "confirmar", comprobar si la respuesta ya está escrita:

- **`tasks.md` lleva su propio estado en el encabezado y en los checkboxes.** `[x]` = hecho, `[ ]` = pendiente, `[-]`/`[~]` = en progreso/parcial. Las notas de cierre ("Cierre de Parte A", "Parte A implementada y verificada") son el estado real; no hay que releer el código para "verificar" lo que la nota ya declara, salvo que se vaya a modificar ese código.
- **`DECISIONES-TOMADAS.md` y el `decision-log` son la verdad de una decisión.** Si DEC-088 dice que A-5 se resolvió con la opción a, esa es la opción; no reabrir el análisis de alternativas.
- Si una afirmación del documento se va a usar tal cual, citarla; no reconstruirla leyendo las fuentes que la originaron.

Excepción: cuando se va a **modificar** un archivo, sí leer la parte exacta que se toca (nunca editar a ciegas).

---

## 3. Delegar la exploración amplia a un sub-agente

Cuando una pregunta exige recorrer varios archivos (entender un flujo, mapear dependencias, auditar un spec entero), **dispatchar `context-gatherer`** en vez de hacer muchas lecturas en el hilo principal. El sub-agente trabaja en su propio contexto y devuelve una respuesta compacta; el hilo principal queda libre para implementar.

- Una pregunta amplia → un `context-gatherer` con intent claro.
- Varias preguntas independientes → varios en paralelo.
- No re-explorar lo que un sub-agente ya devolvió: tratar su salida como lecturas hechas.

---

## 4. Verificar contexto/contrato una vez por tarea, no por mensaje

`contexto-plataforma.md` obliga a verificar que el contexto y el contrato estén al día. Hacerlo **una vez al inicio de la tarea** y reutilizar el resultado durante toda la tarea. No repetir `git fetch`/`git status` en cada paso.

---

## 5. No regenerar lo pesado por costumbre

- No reconstruir `HARNESS.md` ni correr `build-harness.sh` salvo que se hayan tocado docs y se pida el bundle.
- No volcar archivos largos completos en la respuesta al usuario: resumir y enlazar por ruta + línea.
- No repetir en prosa lo que ya quedó en un artefacto o en un archivo; referenciarlo.

---

## 6. Lotes de herramientas en paralelo

Cuando varias lecturas o búsquedas son **independientes**, lanzarlas en el mismo turno (en paralelo) en vez de una por mensaje. Menos turnos = menos reenvío de contexto. Encadenar solo cuando una llamada depende del resultado de otra.

---

## 7. Mapa rápido del repo (para no buscar lo ya sabido)

Dónde vive lo que más se consulta, para ir directo sin explorar:

| Necesito | Ruta |
| --- | --- |
| Servicio de sesión (emitir/refrescar/revocar) | `internal/sesion/servicio.go` |
| Costuras y puertos de sesión | `internal/sesion/seams.go`, `options.go` |
| Tipos del dominio de sesión | `internal/sesion/port.go` |
| SQL de la tabla `sesion` | `internal/sesion/store.go` |
| Gobierno (alta/baja, rol, resolver de sujeto) | `internal/gobierno/` (`servicio.go`, `resolver.go`) |
| Adaptador y puerto del IdP | `internal/idp/` (puerto en `port.go`; Keycloak en `internal/kc/`) |
| Migraciones | `migrations/identidad/000N_*.sql` (van en orden; nuevas = siguiente número) |
| Composición raíz | `identidad.go`, `cmd/api/main.go` |
| Specs | `.kiro/specs/e4-t3-*`, `.kiro/specs/e4-t6-*` |

Si algo no está en este mapa, buscarlo con `file_search`/`grep_search` antes de listar directorios a ciegas.
