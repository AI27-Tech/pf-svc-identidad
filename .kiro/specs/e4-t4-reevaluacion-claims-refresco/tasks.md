# Implementation Plan: E4-T4 — Reevaluación de autorización en el refresco (ADR 16)

> Verificado contra contexto `ai27-platform-context` @ `bd8901d` y contrato `pf-pkg-contracts` fijado en `go.mod` a `v0.1.0-tracer.5`.
>
> **Estado del repo (2026-10-16):** el refresco productivo ya existe (`internal/sesion/refresh.go`, DEC-088) y la composición pura ya existe (`internal/claims/claims.go`, E4-T2). E4-T4 **no reescribe** ninguno; añade la señal de latencia y las pruebas que cierran ADR 16.

## Overview

El plan sigue el reparto Go ↔ IdP del `design.md`: la recomposición de claims en refresco la hace el artefacto del IdP (E4-T1/E4-T2) y E4-T4 la **verifica**; el código nuevo de E4-T4 es la **costura de observabilidad** y las **pruebas de cierre**. Se divide en dos partes, igual que las specs hermanas:

- **Parte A — Se puede implementar ya.** Tareas con checkbox, sin depender de una decisión de PM. Cubren la costura `LatencyObserver` con su no-op por omisión, su cableado en los flujos de emisión y refresco (opción a de B-2 como valor por omisión), y las tres propiedades de unidad.
- **Parte B — En espera de decisión.** Sin checkbox a propósito: no son ejecutables hasta que su decisión (B-1, B-2, B-3) se resuelva. Cada grupo abre con la decisión que lo desbloquea.

Reglas que gobiernan el plan:

1. Estrictamente apegado a `requirements.md` y `design.md`. Nada de "Propuestas de Kiro (K)" entra a la ruta productiva.
2. Ninguna tarea reimplementa la composición de claims ni toca el JAR/mapper del IdP (eso es E4-T1/E4-T2): E4-T4 reusa `internal/claims.Componer` y verifica el artefacto por prueba e2e.
3. No se añaden tablas ni migraciones; no se toca `internal/sesion/refresh.go` en su comportamiento, solo se inserta el punto de medición.
4. No se sube `go.mod` a un tracer nuevo.
5. **Verificación estándar** por tarea de código (`golang-best-practices.md`): `gofmt -l .` sin salida, `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...`, todo en verde.

Convenciones:

- Lenguaje: **Go**. Prosa y comentarios en español (México); identificadores en inglés salvo sustantivos de dominio (`sesion`, `rol`, `claims`, `Audiencia`, `tenant`). `Fase`, `LatencyObserver` en inglés por ser términos técnicos.
- **[Requiere Keycloak]** = `-tags integration`, necesita Docker y el JAR de E4-T1; sin ellos solo se verifica que compile (`go vet -tags integration`), no que pase.
- Las sub-tareas de prueba llevan `*` (opcionales), igual que en E4-T1/E4-T2/E4-T3. Aviso: la 3.3 es la DoD e2e de E4-T4 (compuerta G4); omitirla deja ADR 16 sin verificación extremo a extremo.

## Qué queda fuera de este plan (sin tarea)

| Tema | Motivo |
| --- | --- |
| Disparo de revocación por quita de rol | E4-T6, y solo si B-1 elige la opción b/c |
| Backend de métricas concreto | B-2; por omisión la costura es no-op + medición en adaptador Go |
| Garantía fuerte de consistencia del modelo en el refresco | B-3; tocaría el artefacto de E4-T2 |
| Cambios al JAR/mapper del IdP | E4-T1 / E4-T2 |
| Subir `go.mod` a tracer.8 | No lo necesita E4-T4; decisión aparte de PM |

## Tasks

# PARTE A — SE PUEDE IMPLEMENTAR YA

- [ ] 1. Costura de observabilidad `LatencyObserver`
  - Cierra con la verificación estándar.
  - [ ] 1.1 Definir el tipo `Fase` y la interfaz `LatencyObserver` en `internal/sesion/seams.go`
    - `Fase` con `FaseEmision` y `FaseRefresco`; `LatencyObserver.ObservarComposicion(fase Fase, d time.Duration)`.
    - Doc comment en español que cite el Requisito 3 y B-2 (la forma concreta queda tras la costura).
    - _Requirements: 3.1, 3.3_
  - [ ] 1.2 Implementar `nopLatencyObserver` (no-op) como valor por omisión
    - En `internal/sesion`. Un observador que descarta la observación, para que `NewServicio` sin opción se comporte como hoy.
    - Inicializar el campo en `NewServicio` si la opción no lo cablea (mismo patrón que `revocationLog`).
    - _Requirements: 3.1_
  - [ ] 1.3 Añadir la opción `WithLatencyObserver(o LatencyObserver) Option` en `internal/sesion/options.go`
    - Sigue el patrón de `WithRefresh`, `WithRevocationLog`.
    - _Requirements: 3.1_

- [ ] 2. Cablear la medición en emisión y refresco (opción a de B-2 por omisión)
  - Cierra con la verificación estándar.
  - [ ] 2.1 Medir el tramo `idp.Refresh` en `internal/sesion/refresh.go`
    - Envolver la llamada `s.idp.Refresh(...)` midiendo la duración y reportando `ObservarComposicion(FaseRefresco, d)`. No cambiar la secuencia ni el valor de retorno (Requisito 5.1: el flujo no compone ni copia claims).
    - La medición es best-effort: un error del observador no falla el refresco.
    - _Requirements: 3.1, 3.2, 5.1_
  - [ ] 2.2 Medir el tramo `idp.Issue` en el flujo de emisión (`internal/sesion/emission.go`)
    - Envolver `Issue` en `emissionSteps.Issue` (o el punto equivalente) y reportar `ObservarComposicion(FaseEmision, d)`. Mismo criterio best-effort.
    - _Requirements: 3.1, 3.2_

- [ ] 3. Pruebas de la Parte A
  - [ ] 3.1* Property 1 — Recomposición idéntica emisión/refresco
    - Reusar la Property 5 de E4-T2 sobre `claims.Componer`: misma entrada → misma salida; con rol `r'` distinto → resultado de `r'` sin reutilizar. Comentario `// Feature: e4-t4-..., Property 1`.
    - _Requirements: 2.4, 6.1_
  - [ ] 3.2* Property 2 — Observabilidad por fase
    - Doble de `LatencyObserver` que registra las fases recibidas. Afirmar: una `FaseEmision` por emisión, una `FaseRefresco` por refresco, ninguna cruzada. Usa el doble `internal/idp/memory`.
    - _Requirements: 3.1, 3.2, 6.2_
  - [ ] 3.3* Property 3 — El refresco no muta claims en Go
    - Con un token del IdP (doble) de claims fijos, afirmar que `refrescar` devuelve el token del IdP sin derivar claims del token anterior (el flujo Go no los lee ni compone).
    - _Requirements: 5.1, 5.2_

---

# PARTE B — EN ESPERA DE DECISIÓN

> Las tareas de esta parte **no tienen checkbox a propósito**: no son ejecutables hasta que su decisión se resuelva y se registre como DEC. Ver `DECISIONES-PENDIENTES.md`.

## Decisión pendiente B-1 — Plazo de la quita de rol (DEC-089 tentativo)

**No se ejecuta, se espera.** Si B-1 = opción a ("basta el siguiente refresco"):

- Cerrar el Requisito 4 con una nota de reconciliación hacia E4-HU-020: el "≤30 s" aplica a la **revocación de sesión** (E4-T3), no al **cambio de rol** (que se acota a la vida del token). Sin código.

Si B-1 = opción b/c ("quitar rol corta en ≤30 s"):

- Documentar en E4-T4 la dependencia hacia **E4-T6**: la operación que quita el rol dispara la revocación de las sesiones vivas del sujeto (mecanismo de dos pasos de E4-T3). E4-T4 **no** implementa ese disparo; solo deja registrada la dependencia.

## Decisión pendiente B-2 — Forma de la señal de latencia (DEC-090 tentativo)

**No se ejecuta, se espera.** Cuando se decida el backend de métricas:

- Sustituir la implementación de la costura `LatencyObserver` (hoy no-op + medición en adaptador Go) por la real (histograma con etiqueta `fase`, u opción b/c si se eligió exponer un tramo desde el JAR). El punto de llamada (tareas 2.1/2.2) no cambia.

## Decisión pendiente B-3 — Significado de "modelo vigente" (DEC-091 tentativo)

**No se ejecuta, se espera.** Si B-3 = opción a (consistencia eventual):

- La prueba e2e G4 (tarea [Requiere Keycloak] abajo) se escribe para "tras el cambio comprometido". Nada más.

Si B-3 = opción b (consistencia fuerte):

- Cambia el artefacto de composición de **E4-T2** (lectura autoritativa única en el refresco). E4-T4 pasa a depender de ese cambio; la prueba G4 se endurece para "sin ventana de inconsistencia".

## [Requiere Keycloak] DoD e2e — compuerta G4 (cierre de ADR 16)

**Ejecutable solo con Docker + Keycloak 26.7.4 + el JAR de E4-T1.** Sin infraestructura, solo compila (`go vet -tags integration`), no se ejecuta:

- Prueba `-tags integration`: alta de sujeto con rol `r` → emisión → cambio a rol `r'` (vía el adaptador admin de E4-T1/E4-T6) → refresco → aserción de que el token nuevo trae `r'` y el **mismo** `sesion_id`.
- _Requirements: 2.3, 6.3._ Condicionada a B-3 (garantía débil vs fuerte en la aserción).

## Cierre

E4-T4 cierra cuando: (1) la Parte A está en verde; (2) B-1, B-2 y B-3 están registradas como DEC y sus tareas de Parte B resueltas; (3) la DoD e2e G4 pasó contra Keycloak real. Antes de eso, E4-T4 está **parcialmente implementada** (Parte A) y su cierre formal **depende de PM/Arquitectura** (Parte B) y de infraestructura (G4).
