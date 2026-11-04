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

> **Parte A implementada y verificada (2026-10-16).** Las tareas 1 y 2 están hechas; las tres pruebas de la tarea 3 pasan. Verificación estándar en verde: `go vet ./...`, `go vet -tags integration ./...`, `go build ./...`, `go test ./...` (exit 0 en los cuatro). Archivos tocados: `internal/sesion/seams.go` (costura `LatencyObserver`, `Fase`, no-op), `internal/sesion/servicio.go` (campo + default), `internal/sesion/options.go` (`WithLatencyObserver`), `internal/sesion/refresh.go` (medición del refresco), `internal/sesion/emission.go` (medición de la emisión), `internal/sesion/reeval_refresco_test.go` (las tres propiedades). Nota: `gofmt -l` reporta todos los archivos del paquete por el CRLF del working tree en Windows (`core.autocrlf=true`), no por formato de código; `vet`/`build`/`test` pasan.

- [x] 1. Costura de observabilidad `LatencyObserver`
  - Cierra con la verificación estándar.
  - [x] 1.1 Definir el tipo `Fase` y la interfaz `LatencyObserver` en `internal/sesion/seams.go`
    - `Fase` con `FaseEmision` y `FaseRefresco`; `LatencyObserver.ObservarComposicion(fase Fase, d time.Duration)`.
    - Doc comment en español que cite el Requisito 3 y B-2 (la forma concreta queda tras la costura).
    - _Requirements: 3.1, 3.3_
  - [x] 1.2 Implementar `nopLatencyObserver` (no-op) como valor por omisión
    - En `internal/sesion`. Un observador que descarta la observación, para que `NewServicio` sin opción se comporte como hoy.
    - Inicializar el campo en `NewServicio` si la opción no lo cablea (mismo patrón que `revocationLog`).
    - _Requirements: 3.1_
  - [x] 1.3 Añadir la opción `WithLatencyObserver(o LatencyObserver) Option` en `internal/sesion/options.go`
    - Sigue el patrón de `WithRefresh`, `WithRevocationLog`.
    - _Requirements: 3.1_

- [x] 2. Cablear la medición en emisión y refresco (opción a de B-2 por omisión)
  - Cierra con la verificación estándar.
  - [x] 2.1 Medir el tramo `idp.Refresh` en `internal/sesion/refresh.go`
    - Envolver la llamada `s.idp.Refresh(...)` midiendo la duración y reportando `ObservarComposicion(FaseRefresco, d)`. No cambiar la secuencia ni el valor de retorno (Requisito 5.1: el flujo no compone ni copia claims).
    - La medición es best-effort: un error del observador no falla el refresco.
    - _Requirements: 3.1, 3.2, 5.1_
  - [x] 2.2 Medir el tramo `idp.Issue` en el flujo de emisión (`internal/sesion/emission.go`)
    - Envolver `Issue` en `emissionSteps.Issue` (o el punto equivalente) y reportar `ObservarComposicion(FaseEmision, d)`. Mismo criterio best-effort.
    - _Requirements: 3.1, 3.2_

- [x] 3. Pruebas de la Parte A
  - [x] 3.1* Property 1 — Recomposición idéntica emisión/refresco
    - Reusa `claims.Componer`: determinismo (misma entrada → misma salida) y metamórfica (con rol `r'` distinto → resultado de `r'` sin reutilizar). `TestPropertyRecomposicionIdenticaEmisionRefresco`.
    - _Requirements: 2.4, 6.1_
  - [x] 3.2* Property 2 — Observabilidad por fase
    - Doble `observadorGrabador`. Afirma una `FaseEmision` por `emissionSteps.Issue` y una `FaseRefresco` por `servicio.refrescar` (cortado por error del IdP antes de Postgres), ninguna cruzada. `TestPropertyObservabilidadPorFase`.
    - _Requirements: 3.1, 3.2, 6.2_
  - [x] 3.3* Property 3 — El refresco deriva el sesion_id del token del IdP
    - Afirma que el `RefreshResolver` productivo deriva el `sesion_id` del token del IdP y que los tokens no se transforman en Go (parte de Requisito 5 verificable en unidad). `TestPropertyRefrescoDerivaSesionIDDelToken`. _La prueba del flujo completo de `refrescar` hasta el return es [Requiere Postgres] (integración de E4-T3)._
    - _Requirements: 5.1, 5.2_

---

# PARTE B — EN ESPERA DE DECISIÓN

> Las tareas de esta parte **no tienen checkbox a propósito**: no son ejecutables hasta que su decisión se resuelva y se registre como DEC. Ver `DECISIONES-PENDIENTES.md`.

- [x] 4. Decisión B-1 — Plazo de la quita de rol: RESUELTA opción a (DEC-091, registrada en contexto)
  - Decidida 2026-10-16: basta el siguiente refresco; el "≤30 s" es de la revocación de sesión (ADR 26), no del cambio de rol (DEC-031 #2). **Registrada como DEC-091** en `ai27-platform-context` (`main`, commit `06bed9a`). Ver `DECISIONES-PENDIENTES.md`.
  - [x] 4.1 Reconciliación hacia E4-HU-020 **aplicada y subida** (contexto `main`, commit `3797f80`): el criterio y el escenario 3 ya dicen "siguiente refresco", el "conflicto abierto" se movió a decisiones cerradas. Sin disparo de revocación por quita de rol, **sin dependencia a E4-T6**, sin código adicional.
    - _Requirements: 4.1_
  - [x] 4.2 (No aplica — era la rama opción b/c, descartada por DEC-091.)
    - _Requirements: 4.2_

- [x] 5. Decisión B-2 — Forma de la señal de latencia: RESUELTA opción a (DEC-092, registrada en contexto)
  - Decidida 2026-10-16: medición en el adaptador Go por fase, tras la costura `LatencyObserver`. **Registrada como DEC-092** (contexto `main`, commit `06bed9a`). Ya implementado en la Parte A (tareas 1 y 2).
  - [ ] 5.1 (Diferida — refinar la costura si V1 fija un backend de métricas concreto; los puntos de llamada 2.1/2.2 no cambian.)
    - _Requirements: 3.3_

- [x] 6. Decisión B-3 — "Modelo vigente" en el refresco: RESUELTA opción a (DEC-093, registrada en contexto)
  - Decidida 2026-10-16: consistencia eventual; el refresco trae el rol tras converger. No cambia E4-T2. **Registrada como DEC-093** (contexto `main`, commit `06bed9a`).
  - [x] 6.1 La prueba e2e G4 (tarea 7) se escribió para "tras el cambio comprometido".
    - _Requirements: 2.3_

- [x] 7. [Requiere Keycloak] DoD e2e — compuerta G4 (cierre de ADR 16): ESCRITA Y EJECUTADA EN VERDE
  - Corrida contra Keycloak 26.7.4 real (testcontainers, con el JAR de E4-T1) el 2026-10-16: `go test -tags integration -p 1 ./...` con un Postgres 16 efímero; el paquete `internal/idp/internal/kc` pasó (`ok`, ~1095 s). B-3 resuelta con garantía eventual (DEC-093): la aserción es "tras el cambio comprometido".
  - [x] 7.1 Prueba `-tags integration` escrita: se **extendió** `TestCambioRolRefresco` (`internal/idp/internal/kc/integration_claims_test.go`, de E4-T1) para que, además de verificar el rol nuevo (`monitorista` → `supervisor`) tras el refresco, afirme que el **`sesion_id` se conserva** entre emisión y refresco. Antes esa prueba solo cubría el rol; la estabilidad del `sesion_id` la cubrían por separado las pruebas R9 de sesión. Ahora las dos aserciones de G4 viven juntas en un caso.
    - _Requirements: 2.3, 6.3_
  - [x] 7.2 Ejecutada G4 contra Keycloak real: el paquete `kc` cerró en verde en la corrida de integración completa. (El único FAIL de esa corrida fue `internal/sesion` por un bloqueo transitorio de Windows Application Control sobre el binario de test —no una aserción—; reintentado aislado, pasa con y sin `-tags integration`.)
    - _Requirements: 6.3_

## Cierre

**E4-T4 verificada de punta a punta (2026-10-16).** (1) La Parte A está en verde; (2) B-1/B-2/B-3 están registradas como DEC-091/092/093 en el contexto (`main`) y E4-HU-020 reconciliada; (3) la DoD e2e G4 pasó contra Keycloak 26.7.4 real. Queda **pendiente de commit** la rama `feature/e4-t4-reeval-claims-refresco` (Parte A, spec, prueba G4 extendida). La única tarea abierta es 5.1, diferida a propósito (refinar la señal de latencia cuando V1 fije un backend de métricas).

## Notes

- Las sub-tareas marcadas con `*` son pruebas y pueden omitirse para un MVP más rápido; las tareas de implementación central (1 y 2) nunca se marcan opcionales. La 3.3 es parte de la verificación del Requisito 5.
- **E4-T4 no cierra solo con la Parte A.** La recomposición de claims corre en el artefacto del IdP (ADR 32 §4): la garantía de punta a punta ("cambiar rol → refrescar → rol nuevo") solo la cierra la tarea 7 (DoD e2e G4) contra Keycloak real. La Parte A verifica la señal de latencia por fase y la recomposición pura (`claims.Componer`), no el flujo contra el IdP.
- Las tareas de la **Parte B (4–7) no entran al grafo de dependencias**: no son ejecutables hasta que su decisión (B-1/B-2/B-3) se resuelva y se registre como DEC. Al desbloquearse, se agregan como waves nuevas.
- Las propiedades usan `testing/quick` (la librería que ya usa el repo), `MaxCount ≥ 100` y semilla fija registrada en el mensaje de fallo, con el comentario `// Feature: e4-t4-reevaluacion-claims-refresco, Property N`.

## Task Dependency Graph

Solo contiene las sub-tareas ejecutables de la Parte A (hojas con notación decimal). Las tareas padre no se incluyen; las de la Parte B (4–7) no están en el grafo. `1.1` define el tipo y la interfaz que el resto consume; `1.2` inicializa el default que `2.1`/`2.2` usan al medir.

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "3.1"] },
    { "id": 1, "tasks": ["1.2", "1.3"] },
    { "id": 2, "tasks": ["2.1", "2.2"] },
    { "id": 3, "tasks": ["3.2", "3.3"] }
  ]
}
```
