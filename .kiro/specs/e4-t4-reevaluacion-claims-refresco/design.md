# Design Document

**E4-T4 — Reevaluación de autorización en el refresco (ADR 16)** · Diseño

| | |
| --- | --- |
| **Estado** | Borrador — depende de B-1, B-2, B-3 (`DECISIONES-PENDIENTES.md`) |
| **Repositorio** | `repos/pf-svc-identidad` |
| **Fuentes verificadas** | Contexto @ `bd8901d`; contrato `go.mod` → `v0.1.0-tracer.5` |
| **Fecha** | 2026-10-16 |

## Overview

E4-T4 garantiza que la composición de claims corra en el refresco con el modelo vigente, mida su latencia por separado de la emisión, y no introduzca una segunda ruta de composición en el proceso Go. El diseño parte del **estado real verificado del repositorio**, no de un folio en blanco: el refresco productivo ya existe (`internal/sesion/refresh.go`, DEC-088) y la composición pura ya existe (`internal/claims/claims.go`, E4-T2). E4-T4 **no reescribe** ninguno de los dos; añade una costura de observabilidad y las pruebas que cierran ADR 16.

## El reparto Go ↔ IdP (lo más importante de este diseño)

La composición de claims tiene **un solo dueño en producción: el artefacto del IdP** (mapper o extensión Java, ADR 32 §4), que corre dentro de Keycloak en las rutas de emisión y de refresco. El proceso Go **no compone claims en el token de producción**: recibe de `idp.Refresh` un token que **ya** los trae recompuestos.

```
                 EMISIÓN                                   REFRESCO
   cliente ──POST /sesiones──> identidad(Go)    cliente ──POST /sesiones/refresh──> identidad(Go)
                                   │                                         │
                                   │ idp.Issue                               │ idp.Refresh
                                   ▼                                         ▼
                          ┌─────────────────┐                      ┌─────────────────┐
                          │   IdP (Keycloak)│                      │   IdP (Keycloak)│
                          │  ARTEFACTO de   │◄── modelo vigente ──►│  ARTEFACTO de   │
                          │  composición    │   (grupo de rol)     │  composición    │
                          │  (mapper/ext.)  │                      │  (mapper/ext.)  │
                          └─────────────────┘                      └─────────────────┘
                                   │ token con claims                        │ token con claims
                                   ▼ recompuestos                            ▼ recompuestos (rol NUEVO)
                          identidad registra fila             identidad resuelve fila, actualiza expira_en,
                          (E4-T3)                             conserva sesion_id (E4-T3). NO toca claims.
```

Por eso E4-T4 se reparte así:

| Pieza | Dónde vive | Responsable | Qué hace E4-T4 |
| --- | --- | --- | --- |
| Recomposición de claims en refresco | Artefacto del IdP (JAR/mapper) | E4-T1 (empaqueta) / E4-T2 (define) | **Verifica** que corre en refresco (prueba e2e G4). No reimplementa |
| Función pura `Componer` | `internal/claims` | E4-T2 | **Reusa** su Property 5 (idéntica emisión/refresco). No la cambia |
| Flujo Go de refresco | `internal/sesion/refresh.go` | E4-T3 (DEC-088) | **Conserva** su contrato: no copiar ni componer claims (Requisito 5). Añade el punto de medición |
| Señal de latencia emisión/refresco | `internal/sesion` (costura nueva) | **E4-T4** | **Implementa** la costura de observabilidad |

Consecuencia honesta de alcance: **la mayor parte de la garantía "se recompone en refresco" ya está satisfecha por E4-T1/E4-T2** (el artefacto corre en ambas rutas y la función es la misma). Lo que E4-T4 **aporta de código nuevo** es la señal de latencia y las pruebas de cierre de ADR 16. Lo que E4-T4 **aporta de verificación** es la prueba e2e G4 contra Keycloak.

## Components and Interfaces

### 1. `internal/claims` — sin cambios

La función pura `Componer(ModeloVigente) Compuestos` ya es idéntica en emisión y refresco (DEC-031). E4-T4 **no la toca**; reusa su Property 5 como prueba del Requisito 2.4 y 6.1.

### 2. `internal/sesion/refresh.go` — sin cambios de comportamiento

El flujo `refrescar` ya: refresca en el IdP, resuelve el `sesion_id` del token (A-4 a), rechaza sesión revocada (A-4 Reject), actualiza `expira_en` (A-3 b) y conserva el `sesion_id`. **No compone ni copia claims** — exactamente lo que el Requisito 5 exige. E4-T4 añade solo el punto de instrumentación de latencia (abajo), sin cambiar la secuencia.

### 3. `internal/sesion` — costura de observabilidad (código nuevo de E4-T4)

Una costura pequeña, aceptada por el consumidor (patrón de `seams.go`), que mide la latencia de la llamada de composición del IdP distinguiendo la fase:

```go
// LatencyObserver registra la latencia de la composición de claims del IdP,
// distinguiendo emisión de refresco (Requisito 3). La forma concreta (backend
// de métricas) queda tras esta costura (B-2); el valor por omisión es un no-op.
type Fase int

const (
    FaseEmision Fase = iota
    FaseRefresco
)

// LatencyObserver observa la latencia de composición por fase.
type LatencyObserver interface {
    ObservarComposicion(fase Fase, d time.Duration)
}
```

- **Valor por omisión:** `nopLatencyObserver` (no-op), para que `NewServicio` sin opción se comporte como hoy. Opción `WithLatencyObserver(o LatencyObserver)` la cablea.
- **Punto de medición (opción a de B-2, por omisión):** envolver la llamada `idp.Issue` en el flujo de emisión y `idp.Refresh` en `refrescar`, midiendo el tramo y reportando con `FaseEmision` / `FaseRefresco`. Mide el `Issue`/`Refresh` del IdP (que incluye la composición); B-2 puede refinarlo a un tramo expuesto por la extensión.
- **Por qué una costura y no una métrica directa:** el backend de métricas de V1 no está decidido (B-2). La costura permite implementar y probar la separación emisión/refresco sin acoplar a un backend; cuando B-2 resuelva, se implementa la costura real.

### 4. Artefacto del IdP — fuera del proceso Go, verificado por prueba e2e

La recomposición en refresco corre en el JAR/mapper de E4-T1/E4-T2. E4-T4 no lo modifica; lo **ejercita** con la prueba e2e G4: cambiar rol → refrescar → verificar rol nuevo + mismo `sesion_id`.

## Data Models

E4-T4 **no añade tablas ni migraciones.** No toca `identidad.sesion` (E4-T3) ni `asignacion_rol` (E4-T2). El rol vigente lo leen el artefacto del IdP y el modelo de E4-T2; E4-T4 no introduce estado durable nuevo.

## Decisiones pendientes que el diseño respeta

- **B-1 (quita de rol ≤30 s):** el diseño implementa la reevaluación en refresco (Requisito 2). **No** implementa disparo de revocación por quita de rol: eso es E4-T6 y solo aplica si B-1 elige la opción b/c. El diseño deja el gancho documentado, no cableado.
- **B-2 (forma de la señal):** el diseño implementa la opción a (medición en el adaptador Go, por fase) **detrás de la costura `LatencyObserver`**, con no-op por omisión. Cambiar a b/c sustituye la implementación de la costura, no el punto de llamada.
- **B-3 ("modelo vigente"):** el diseño asume **consistencia eventual** (opción a de B-3): el refresco trae lo que el IdP tenga tras converger. La prueba e2e se escribe para "tras el cambio comprometido". La garantía fuerte (b) cambiaría E4-T2, no este diseño.

## Error Handling

Sin errores nuevos. El flujo de refresco ya mapea los del IdP (`mapearErrorRefresco`: refresh inválido → 401, IdP no disponible → 503) y el Reject de sesión revocada → 401. La costura de observabilidad **nunca** falla la operación: un error al reportar métrica es best-effort y no propaga (mismo criterio que la caché best-effort de E4-T3).

## Testing Strategy

Propiedades y pruebas, con la convención del repo (`testing/quick`, `MaxCount: 100`, semilla fija registrada; comentario `// Feature: e4-t4-reevaluacion-claims-refresco, Property N`):

- **Property 1 — Recomposición idéntica emisión/refresco (reusa E4-T2 Property 5).** Para todo `ModeloVigente`, `Componer` da el mismo resultado; con un rol `r'` distinto, da el de `r'` sin reutilizar el anterior. Cierra Requisito 2.4 y 6.1. _(No requiere infraestructura.)_
- **Property 2 — Observabilidad por fase.** Un doble de `LatencyObserver` recibe exactamente una observación con `FaseEmision` por emisión y una con `FaseRefresco` por refresco, y ninguna cruzada. Cierra Requisito 3 y 6.2. _(No requiere infraestructura.)_
- **Property 3 — El refresco no muta claims en Go.** Dado un token del IdP (doble) con claims fijos, el `Emitida` que devuelve `refrescar` no deriva de claims del token anterior: el flujo Go no los lee ni los compone. Cierra Requisito 5. _(No requiere infraestructura; usa el doble `internal/idp/memory`.)_
- **[Requiere Keycloak] DoD e2e (compuerta G4).** Contra Keycloak 26.7.4 + el JAR de E4-T1: alta de sujeto con rol `r`, emisión, cambio a rol `r'` (vía el adaptador admin de E4-T6/E4-T1), refresco, y aserción de que el token nuevo trae `r'` y el mismo `sesion_id`. Cierra Requisito 6.3. _Sin Docker/JAR, la prueba `-tags integration` solo compila (`go vet -tags integration`), no se ejecuta — se dice explícitamente, no se da por verificada._

### Verificación estándar Go (por tarea de código)

`gofmt -l .` sin salida · `go vet ./...` · `go vet -tags integration ./...` · `go build ./...` · `go test ./...`, todo en verde, en la raíz de `repos/pf-svc-identidad`.

## Scope and Deferrals

- **Fuera de alcance:** disparo de revocación por quita de rol (E4-T6, condicionado a B-1); backend de métricas concreto (B-2); garantía fuerte de consistencia del modelo (B-3, tocaría E4-T2); cualquier cambio al JAR/mapper (E4-T1/E4-T2).
- **No se sube `go.mod`:** el tracer fijado (v0.1.0-tracer.5) ya trae `/sesiones/refresh`; subir a tracer.8 no es necesario para E4-T4 y arrastraría cambios de otras historias. Si PM lo pide, se evalúa aparte.
- **Dependencia de infraestructura para el cierre:** el Requisito 6.3 (G4) no se puede marcar verificado sin Keycloak corriendo; el resto de E4-T4 se verifica en unidad sin infraestructura.
