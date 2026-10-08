# Implementación de persistencia: Decisiones A y B

## Decisión A: `UNIQUE(tenant_id, id)` en sujeto

**Resultado:** No se agrega constraint en BD.

**Criterio:** El criterio 1.3 se considera cubierto por:
- `id PK` (unicidad global garantizada)
- `idp_subject UNIQUE` (unicidad del identificador del IdP)
- `alcance_tenant.PRIMARY KEY (sujeto_id, tenant_id)` (implica unicidad del par)

**Nota:** La migración `0001_init.sql` ya existe y no se modifica por esta decisión.

---

## Decisión B: Validación en código de duplicados `(sujeto_id, rol_codigo)`

**Resultado:** No se agrega constraint `UNIQUE (sujeto_id, rol_codigo) WHERE revocado_en IS NULL` en BD.

**Validación:** En código, antes de insertar en `asignacion_rol`:

1. El repositorio `AsignacionRolRepo.Asignar` consulta primero `ActivasDe(ctx, sujetoID)`.
2. Itera sobre las asignaciones activas y verifica que no existe otra asignación del **mismo rol** al **mismo sujeto**.
3. Si existe, retorna `ErrAsignacionDuplicada` sin insertar.
4. De lo contrario, procede a insertar.

**Flujo de Asignar:**

```
1. Asignar(ctx, AsignacionRol{SujetoID, Rol, ...})
   ↓
2. ActivasDe(ctx, SujetoID)  // SELECT WHERE revocado_en IS NULL
   ↓
3. for cada asignación activa:
   - if asignacion.Rol == Rol.solicitado
     → retorna ErrAsignacionDuplicada
   ↓
4. INSERT INTO asignacion_rol(...)
   ↓
5. Mapear errores de FK/CHECK a ErrRolFueraDeCatalogo
```

**Razón del enfoque:** La decisión B se tomó porque en T2 no se ha decidido aún cómo expresar el **alcance** de cada asignación (el identificador del recurso: unidad, cliente). Una vez se defina ese vínculo (DEC-045), el constraint natural será `UNIQUE (sujeto_id, rol_codigo, alcance_id) WHERE revocado_en IS NULL`. Hasta entonces, la validación en código es más flexible.

---

## Mapeo de errores de constraint

### `ErrSujetoDuplicado`

- **Trigger:** Código `23505` (unique_violation) en PostgreSQL, sobre `idp_subject`.
- **Mapeo:** `fmt.Errorf("crear sujeto: %w", ErrSujetoDuplicado)`.

### `ErrRolFueraDeCatalogo`

- **Triggers:**
  - Código `23503` (foreign_key_violation) — violación de la FK `asignacion_rol.rol_codigo` → `rol.codigo`.
  - Código `23514` (check_violation) — violación del CHECK `rol IN (...)`.
- **Mapeo:** `fmt.Errorf("asignar rol: %w", ErrRolFueraDeCatalogo)`.

### `ErrAsignacionDuplicada`

- **Trigger:** En código (no en BD); detectado por `ActivasDe` antes de insertar.
- **Mapeo:** `fmt.Errorf("asignar rol: %w", ErrAsignacionDuplicada)`.

### `ErrRolNoUnico`

- **Trigger:** `CargadorModeloVigente.ModeloVigenteDe` detecta que el sujeto tiene 0 o >1 asignaciones activas.
- **Mapeo:** `fmt.Errorf("cargar modelo vigente: %w", ErrRolNoUnico)`.

---

## Estructura de archivos

```
internal/autorizacion/postgres/
  ├── sujeto.go              // SujetoRepo: Crear, PorIdpSubject
  ├── sujeto_test.go         // Unit tests (skipped sin testcontainers)
  ├── asignacion_rol.go      // AsignacionRolRepo: Asignar, ActivasDe
  ├── asignacion_rol_test.go // Referencia a integration_test.go
  ├── cargador.go            // CargadorModeloVigente: ModeloVigenteDe
  ├── cargador_test.go       // Unit tests (skipped sin testcontainers)
  ├── integration_test.go    // Tests de integración con testcontainers (build tag: integration)
  └── NOTAS.md               // Este archivo
```

---

## Tests de integración (build tag: integration)

Los tests de integración requieren **testcontainers** para levantar Postgres dinámicamente. Se encuentran en `integration_test.go` bajo el build tag `// +build integration` y se ejecutan con:

```bash
go test -tags integration ./internal/autorizacion/postgres/...
```

### Tareas implementadas

**Tarea 7.1 — TestAsignacionRolRepoDuplicadoEnCodigoRetornaError:**
- **Feature:** e4-t2-modelo-autorizacion-claims, Property 4: Unicidad de claves del modelo
- **Comportamiento:** Crear un sujeto y asignarle dos veces el mismo rol activamente. Segunda asignación debe rechazar con `ErrAsignacionDuplicada` sin modificar BD.
- **Validación:** Decisión B confirma que la validación ocurre en código, no en constraint BD. El test verifica que `AsignacionRolRepo.Asignar()` rechaza en tiempo de ejecución.

**Tarea 7.2 — TestAsignacionRolRepoRolFueraDeCatalogo:**
- **Feature:** e4-t2-modelo-autorizacion-claims, Property 7: Rechazo de rol fuera del catálogo
- **Comportamiento:** Intentar asignar un rol inválido (string fuera del enum de 8 roles válidos, p. ej. "auditor", "") debe rechazar con `ErrRolFueraDeCatalogo`.
- **Validación:** El catálogo está protegido por `CHECK (rol_codigo IN (...))` y `FK` en `asignacion_rol`. Test verifica que se mapea correctamente el error.

**Tarea 7.3 — TestPropertyAislamientoEsquemaP1:**
- **Feature:** e4-t2-modelo-autorizacion-claims, Property 6: Aislamiento de esquema por rol de BD (P1)
- **Comportamiento:**
  - Control positivo: con conexión superuser, `SELECT` en esquema `identidad` debe funcionar.
  - Control negativo: crear rol sin acceso a esquema; verificar que los permisos están revocados.
- **Validación:** Verifica P1 (aislamiento por rol DB) desde la infraestructura de test. La conexión de prueba es superuser (testuser), que puede crear roles y verificar permisos. En un entorno real con svc_identidad, se validaría con una conexión como ese rol.

### Ejecución

- **Con testcontainers disponible (Docker corriendo):** Los tres tests se ejecutan y verifican el comportamiento.
- **Sin testcontainers (p. ej. Windows sin Docker):** Los tests se skippean elegantemente con mensaje `Docker no disponible o no corriendo (testcontainers)`.
- **Sin build tag integration:** Los tests no se compilan (ignorados por el build tag).

### Dependencia

Testcontainers ya está en `go.mod`:
```
github.com/testcontainers/testcontainers-go v0.44.0 // indirect
```

---

## Verificación

- `gofmt`: ✓ Todos los archivos formateados.
- `go vet`: ✓ Sin errores.
- `go build ./...`: ✓ Compila.
- `go build -tags integration ./...`: ✓ Compila con build tag.
- `go test ./...`: ✓ Todos los tests pasan (12 skipped de integración en MVP).
- `go test -tags integration ./...`: ✓ 3 tests de integración skippean elegantemente si Docker no disponible.

---

## Notas para infraestructura de CI/CD

Para ejecutar los tests de integración en CI:

1. Verificar que Docker está disponible en el runner (p. ej. ubuntu-latest en GitHub Actions).
2. Correr: `go test -tags integration -v ./internal/autorizacion/postgres/...`
3. Los tests se ejecutarán completos si Docker está disponible; de lo contrario, se skippean.

---

## Por implementar en T2+ o futura

- **En CI/CD:** Verificar que los tests de integración se ejecutan en al menos un runner con Docker.
- **Adaptador real del IdP:** Todavía en espera de ADR 32 (`internal/idp/real`).
- **DEC-045:** Vínculo Monitorista↔unidades; `TODO(DEC-045)` en todo el código.
- **Eventos:** Outbox y publicación de `sujeto.*` / `rol.*` (E4-T6).
