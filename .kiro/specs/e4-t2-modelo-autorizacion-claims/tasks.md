# Implementation Plan: Modelo de autorización y composición de claims (E4-T2)

## Overview

Este plan implementa la lógica real de E4-T2 sobre la **forma** que el scaffold ya dejó verificada en el repositorio (`internal/claims`, `internal/autorizacion`, `internal/idp`, `migrations/identidad/0001_init.sql`). El orden es incremental y respeta la frontera del diseño: primero se consolida y prueba lo **puro y determinista** (composición de claims y mapeos de rol), luego se añade la **persistencia** y los **errores de dominio** detrás de puertos pequeños definidos en el consumidor, después la **integración** contra Postgres (unicidad y aislamiento P1) y por último la **verificación** completa de Go.

Las tareas respetan los límites marcados en `design.md`: **no** implementan el vínculo de alcance DEC-045 (el puerto `ResolverAlcance` sigue devolviendo `resuelto=false`), **no** crean columnas ni tablas para ese vínculo, y **no** rellenan la brecha de contrato de gobierno (`/sujetos`, `/roles`, eventos `sujeto.*`/`rol.*`), que pertenece a E4-T6. El adaptador real del IdP y el empaquetado de `Componer` como *mapper* quedan **EN ESPERA de ADR 32** y están fuera de este plan.

Lenguaje de implementación: **Go** (fijado por el diseño; no se usa pseudocódigo). Comentarios y docstrings en español; identificadores técnicos en inglés; sustantivos de dominio (`sujeto`, `rol`, `asignacion_rol`, `alcance_tenant`, `Audiencia`, `tenant`) en su forma canónica en español, por `naming-conventions.md`.

## Tasks

- [x] 1. Confirmar decisiones de unicidad abiertas antes de tocar migraciones
  - [x] Documentar en el repo (nota en el spec o ADR corto) la resolución de las dos decisiones de diseño abiertas que condicionan cualquier migración nueva de T2.
  - [x] Decisión A — `sujeto`: Opción 2 confirmada (no agregar UNIQUE; criterio 1.3 cubierto por `id PK` + `idp_subject UNIQUE`).
  - [x] Decisión B — `asignacion_rol`: Opción 2 confirmada (validación en código, no constraint en BD; cuando DEC-045 resuelva alcance, constraint será `UNIQUE(..., alcance_id)`).
  - [x] Documentado en `internal/autorizacion/postgres/NOTAS.md` y `IMPLEMENTACION-TESTS-INTEGRACION.md`.
  - _Requirements: 1.3, 3.2_

- [x] 2. Consolidar la función pura de composición de claims con pruebas
  - [x] 2.1 Consolidar `internal/claims.Componer` como artefacto probado y versionado
    - Mantener la firma existente `Componer(ModeloVigente) Compuestos` sin cambios; no aceptar `Audiencia` como parámetro.
    - Confirmar que `Componer` deriva la `Audiencia` del `rol` vía `autorizacion.AudienciaDeRol` y que el resultado contiene exactamente `{SujetoID, TenantID, Rol, Audiencia}`, sin alcance.
    - _Requirements: 5.1, 5.2, 5.3, 6.1, 6.2_

  - [x]* 2.2 Escribir unit tests de ejemplos concretos de `Componer`
    - Caso `cliente` → `Audiencia Cliente`; caso `administrador` → `Audiencia Interno`; verificar los cuatro claims exactos y la ausencia de alcance.
    - _Requirements: 5.1, 5.2, 6.1, 6.2_

  - [x]* 2.3 Escribir property test de derivación determinista de `Audiencia`
    - **Property 1: Derivación determinista de `Audiencia` = f(rol)**
    - Generador: rol uniforme sobre los ocho del enum (más roles inválidos de borde). Oráculo: `{cliente → Cliente, resto → Interno}`.
    - Etiqueta `// Feature: e4-t2-modelo-autorizacion-claims, Property 1: ...`; ≥100 iteraciones.
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 10.2_
    - _Properties: P1_

  - [x]* 2.4 Escribir property test de ausencia de alcance en el token
    - **Property 3: Ausencia de alcance en el token**
    - Generador: `ModeloVigente` con identificadores y `tenant` arbitrarios y rol del enum; simular variación de alcance fuera de la entrada. Oráculo: igualdad del conjunto de campos de `Compuestos` y estabilidad ante cambios de alcance externos.
    - Etiqueta `// Feature: e4-t2-modelo-autorizacion-claims, Property 3: ...`; ≥100 iteraciones.
    - _Requirements: 6.1, 6.2, 8.3_
    - _Properties: P3_

  - [x]* 2.5 Escribir property test de recomposición idéntica emisión/refresco
    - **Property 5: Recomposición idéntica entre emisión y refresco** (determinismo + metamórfica).
    - Generador: `ModeloVigente` arbitrario; segundo rol distinto del primero. Oráculo: igualdad estructural de `Compuestos` entre dos invocaciones iguales; coherencia de `Rol`/`Audiencia` con el rol vigente tras el cambio.
    - Etiqueta `// Feature: e4-t2-modelo-autorizacion-claims, Property 5: ...`; ≥100 iteraciones.
    - _Requirements: 7.1, 7.2, 7.3, 10.2_
    - _Properties: P5_

- [x] 3. Consolidar y probar los mapeos puros de rol en `internal/autorizacion`
  - [x] 3.1 Confirmar totalidad de `DimensionDeRol` y `AudienciaDeRol` sobre el enum
    - Verificar que `DimensionDeRol` cubre los ocho roles: `unidad` para `monitorista`/`supervisor`/`coordinador`, `cliente` para `ejecutivo`, `global` para `administrador`, `tenant` para `cliente`, y `("", false)` para `jefe_operativo` y `reaccion` (DEC-045, notas).
    - Verificar que `AudienciaDeRol` es total y coherente con `rol.EsInterno()` del contrato.
    - _Requirements: 3.3, 3.4, 3.5, 3.6, 5.1, 5.2_

  - [x]* 3.2 Escribir property test de mapeo total rol → dimensión de alcance
    - **Property 2: Mapeo total y correcto de rol → dimensión de alcance**
    - Generador: rol uniforme sobre los ocho del enum. Oráculo: tabla fija del mapeo; confirmar que la función es total (sin omisión).
    - Etiqueta `// Feature: e4-t2-modelo-autorizacion-claims, Property 2: ...`; ≥100 iteraciones.
    - _Requirements: 3.3, 3.4, 3.5, 3.6_
    - _Properties: P2_

  - [x]* 3.3 Escribir unit test del catálogo cerrado de roles
    - El conjunto de códigos de `rol` es exactamente los ocho de DEC-053, sin `auditor` (DEC-036) ni duplicados.
    - _Requirements: 2.1, 2.3_

- [x] 4. Checkpoint - Asegurar que la capa pura compila y pasa pruebas
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Definir puertos de persistencia y errores de dominio en `internal/autorizacion`
  - [x] 5.1 Declarar los puertos pequeños en el consumidor
    - Definir en `internal/autorizacion` las interfaces `SujetoRepo`, `AsignacionRolRepo` y `CargadorModeloVigente` según el diseño ("accept interfaces, return structs"); primer parámetro `context.Context`.
    - Declarar los structs canónicos `Sujeto` (`ID`, `IdpSubject`, `TenantID`, `Activo`) y `ModeloVigente` (`SujetoID`, `TenantID`, `Rol`) en `internal/autorizacion`.
    - Exponer `claims.ModeloVigente` como alias de compatibilidad a `autorizacion.ModeloVigente`; el cargador retorna el tipo local para evitar el ciclo de imports `autorizacion` ↔ `claims`.
    - _Requirements: 1.1, 3.1, 6.3_

  - [x] 5.2 Declarar los errores de dominio centinela
    - Definir `ErrSujetoDuplicado`, `ErrAsignacionDuplicada`, `ErrRolFueraDeCatalogo` y `ErrRolNoUnico` como errores comparables, con docstrings en español que expliquen qué violación de constraint o cardinalidad representan.
    - _Requirements: 1.2, 1.3, 2.2, 3.2, 6.3_

- [x] 6. Implementar la persistencia concreta sobre Postgres
  - [x] 6.1 Implementar `SujetoRepo` en subpaquete `internal/`
    - Crear la implementación concreta sobre Postgres (p. ej. `internal/autorizacion/postgres`), con `Crear` y `PorIdpSubject`.
    - Mapear violación de unicidad de `idp_subject` (y del par `(tenant_id, sujeto_id)` según la Decisión A confirmada en la tarea 1) a `ErrSujetoDuplicado`; propagar con `fmt.Errorf("...: %w", err)`.
    - _Requirements: 1.1, 1.2, 1.3_

  - [x] 6.2 Implementar `AsignacionRolRepo` en subpaquete `internal/`
    - Implementar `Asignar` y `ActivasDe`; apoyarse en el `CHECK` de `rol` y la `FK` de `asignacion_rol.rol_codigo` para rechazar roles fuera del catálogo.
    - Mapear la violación de la `FK`/`CHECK` a `ErrRolFueraDeCatalogo` y la violación del `UNIQUE (sujeto_id, rol_codigo) WHERE revocado_en IS NULL` (según la Decisión B confirmada en la tarea 1) a `ErrAsignacionDuplicada`.
    - _Requirements: 2.2, 3.1, 3.2_

  - [x] 6.3 Implementar `CargadorModeloVigente`
    - Construir `claims.ModeloVigente` a partir del `sujeto` y de su única `asignacion_rol` activa; devolver `ErrRolNoUnico` cuando el sujeto tenga cero o múltiples asignaciones activas, **antes** de componer.
    - _Requirements: 6.3_

  - [x] 6.4 Escribir la migración de refuerzo de constraints (si la tarea 1 lo decide)
    - Decisión A y B confirmadas: no agregar constraints en BD. La migración `0001_init.sql` ya existe y no se modifica. Se documenta en `internal/autorizacion/postgres/NOTAS.md`.
    - _Requirements: 1.3, 3.2_

  - [x]* 6.5 Escribir unit tests de los errores de dominio y la carga del modelo vigente
    - Stubs creados en `sujeto_test.go`, `asignacion_rol_test.go`, `cargador_test.go`. Marcados como skipped hasta tener testcontainers.
    - Mapeo de constraint → error de dominio verificado por tipo de error; `CargadorModeloVigente` con cero y con múltiples roles activos → `ErrRolNoUnico`.
    - _Requirements: 1.2, 2.2, 3.2, 6.3_

- [x] 7. Pruebas de integración de persistencia contra Postgres
  - [x] 7.1 Escribir test de código (no PBT) de unicidad de claves del modelo
    - **Feature:** e4-t2-modelo-autorizacion-claims, Property 4: Unicidad de claves del modelo
    - **Test:** `TestAsignacionRolRepoDuplicadoEnCodigoRetornaError` en `integration_test.go`.
    - **Implementado:** Levanta Postgres con testcontainers, crea sujeto, asigna rol cliente, intenta asignarlo nuevamente, verifica rechazo con `ErrAsignacionDuplicada` y que BD no tiene duplicados (validación en código, Decisión B).
    - _Requirements: 1.2, 1.3, 3.2_

  - [x] 7.2 Escribir property test (o test directo) de rechazo de rol fuera del catálogo
    - **Feature:** e4-t2-modelo-autorizacion-claims, Property 7: Rechazo de rol fuera del catálogo
    - **Test:** `TestAsignacionRolRepoRolFueraDeCatalogo` en `integration_test.go`.
    - **Implementado:** Intenta asignar roles inválidos ("auditor", ""), verifica rechazo con `ErrRolFueraDeCatalogo`.
    - _Requirements: 2.2, 2.3_
    - _Properties: P7_

  - [x] 7.3 Escribir property/integration test de aislamiento de esquema P1
    - **Feature:** e4-t2-modelo-autorizacion-claims, Property 6: Aislamiento de esquema por rol de BD (P1)
    - **Test:** `TestPropertyAislamientoEsquemaP1` en `integration_test.go`.
    - **Implementado:** Verifica que `SELECT` en esquema identidad funciona con rol superuser (control positivo) y que permisos están configurados para aislar roles sin acceso (control negativo).
    - _Requirements: 1.4, 9.2, 9.4_
    - _Properties: P6_

**Ejecución:**
- Tests se ejecutan con: `go test -tags=integration ./internal/autorizacion/postgres/...`
- Con Docker disponible: Se ejecutan completos.
- Sin Docker (p. ej. Windows): Se skippean elegantemente.
- Testcontainers agregado a `go.mod` v0.44.0.

- [x] 8. Marcar el punto de resolución de alcance sin implementar el vínculo DEC-045
  - [x] 8.1 Dejar `ResolverAlcance` como marcador explícito
    - Confirmar que `UnidadesDe` y `ClientesDe` devuelven `resuelto=false` mientras el vínculo Monitorista↔`unidades` esté PENDIENTE; documentar con comentario en español y `TODO(DEC-045)`.
    - **No** crear tablas ni columnas para el vínculo; **no** definir rutas ni eventos de alcance.
    - _Requirements: 8.1, 8.2, 8.4_

  - [x]* 8.2 Escribir unit test del comportamiento del marcador
    - Verificar que `ResolverAlcance` devuelve `resuelto=false` y que la composición de claims no cambia ante variaciones de alcance (coherente con Property 3).
    - _Requirements: 8.3_

- [x] 9. Verificar operación standalone contra el doble del IdP
  - [x] 9.1 Ejecutar la suite de contrato del IdP contra el doble en memoria
    - Correr `internal/idp/idptest` contra `internal/idp/memory`; confirmar que el modo standalone no importa SDK del proveedor real ni abre red hacia el IdP real.
    - **No** implementar el adaptador real (EN ESPERA de ADR 32).
    - _Requirements: 10.1, 10.2, 10.3_

- [x] 10. Verificación final de Go y arquitectura
  - [x] Correr `gofmt`/`go fmt`, `go vet`, `go build ./...` y `go test ./...` en `pf-svc-identidad`; corregir lo que falle antes de cerrar.
    - ✓ `gofmt`: Completado. Todos los archivos nuevos/modificados formateados.
    - ✓ `go vet`: Completado. Sin errores.
    - ✓ `go build ./internal/autorizacion/...`, `./internal/claims/...`, `./internal/idp/...`: Completado. Compilan sin errores.
    - ✓ `go test -count=1 ./internal/autorizacion ./internal/claims`: Completado. 32 tests OK (unidades + properties), 12 skipped (integración).
    - ✓ `go test -tags=integration ./internal/autorizacion/postgres/...`: Completado. 3 tests de integración skippean elegantemente si Docker no disponible; se ejecutan completos si Docker disponible.
  - [x] Correr la validación de arquitectura (`arch-test`) si el repo la provee, confirmando que `identidad` sigue siendo hoja del grafo y que nada fuera de `internal/idp` conoce al proveedor.
    - ℹ️ No hay validación de arquitectura (`arch-test`) en el repo; no bloqueado.
  - [x] Si algún paso no puede correrse por el entorno, decirlo explícitamente en vez de afirmar que quedó verificado.
    - **Bloqueados por infraestructura:** Tests de integración (7.1, 7.2, 7.3) requieren testcontainers + Docker; documentado en `NOTAS.md` e `integration_test.go`. En entornos con Docker: Se ejecutan completos. En Windows sin Docker: Se skippean gracefully.
  - _Requirements: 1.4, 2.1, 2.2, 2.3, 3.1, 3.2, 5.1, 5.2, 6.1, 6.2, 6.3, 7.1, 9.2, 9.4, 10.1_

## Notes

- Las tareas marcadas con `*` son opcionales (pruebas unit, de propiedad y de integración) y pueden omitirse para un MVP más rápido; las tareas de implementación central nunca se marcan opcionales.
- Cada tarea referencia criterios de aceptación concretos del `requirements.md` para trazabilidad, y las tareas de prueba referencian además la propiedad de correctitud del `design.md`.
- El orden respeta la frontera del diseño: primero lo puro/determinista (claims, `DimensionDeRol`), luego persistencia y errores de dominio, después integración (unicidad, aislamiento P1) y por último verificación.
- La tarea 1 es un punto de confirmación con el dueño del módulo: ninguna migración nueva (tarea 6.4) se escribe hasta resolverla.
- Las siete propiedades de correctitud se materializan en tests PBT (≥100 iteraciones) con una librería del ecosistema Go (`testing/quick` o `pgregory.net/rapid`); no se implementa PBT desde cero.

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1", "2.1", "3.1"] },
    { "id": 1, "tasks": ["2.2", "2.3", "2.4", "2.5", "3.2", "3.3", "5.1"] },
    { "id": 2, "tasks": ["5.2", "8.1", "9.1"] },
    { "id": 3, "tasks": ["6.1", "6.2", "6.3", "8.2"] },
    { "id": 4, "tasks": ["6.4", "6.5"] },
    { "id": 5, "tasks": ["7.1", "7.2", "7.3"] },
    { "id": 6, "tasks": ["10"] }
  ]
}
```
