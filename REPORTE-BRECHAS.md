# Reporte de brechas contractuales y de implementación — `identidad`

> Estado verificado: 2026-10-07.
>
> Este reporte distingue las brechas que siguen abiertas de los elementos que ya
> están resueltos. No convierte propuestas ni pendientes de decisión en reglas
> de producto.
>
> **Versiones verificadas:**
> - `github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go` `v0.1.0-tracer.3`
> - `github.com/AI27-Tech/pf-pkg-platform-go` `v0.1.1`
>
> **Validación ejecutada:** `go mod verify`, `go build ./...`, `go vet ./...`,
> `go test ./...`, `go build ./cmd/api` y
> `go run github.com/AI27-Tech/pf-pkg-platform-go/tools/arch-test@v0.1.1 .`.
> Todas pasan en un checkout aislado del `go.work`.

## 1. Brechas contractuales abiertas

Estas brechas no se resuelven desde `pf-svc-identidad`: requieren ampliar y
publicar `pf-pkg-contracts` antes de cablear las superficies correspondientes.

| Superficie o capacidad | Fuente | Estado actual | Impacto en este servicio |
|---|---|---|---|
| `POST/DELETE /api/v1/sujetos/...` para alta y baja | `identidad.md` (f), superficie administrativa | **FALTA** en `api-client-go/identidad` | `internal/gobierno/port.go` permanece vacío; no se montan rutas ni casos de uso. |
| `/api/v1/roles/...` para asignación y revocación de roles con alcance | `identidad.md` (f), DEC-045 #8 | **FALTA** en el contrato | No se implementan rutas ni comandos de gobierno. |
| Eventos `sujeto.creado`, `sujeto.desactivado`, `rol.asignado` y `rol.revocado` | `identidad.md` (g); proyección de `mensajeria` | **FALTA** en `.proto`, `INVENTARIO.md` y `registro.tipos.yaml` | No se publican esos eventos ni se agregan filas de outbox para ellos. |
| Lectura y cambios del alcance por `unidad`/`cliente` | DEC-045 #8 y DEC-053 | **PENDIENTE**: el alcance no viaja en el token y la mecánica Monitorista↔`unidad` sigue sin decisión | `internal/autorizacion/port.go` conserva `TODO(DEC-045)`; no se modelan tablas, rutas ni eventos nuevos de alcance. |

### Efecto delimitado

El módulo sí contiene la forma inicial de `sujeto`, `rol`, `asignacion_rol`,
`alcance_tenant`, `sesion`, `cambio_identidad` y `outbox` en la migración, pero
la ausencia de contratos para administración y eventos impide exponer esos
casos de uso. No se deben crear tipos de wire, rutas ni eventos manualmente
para llenar esta brecha.

## 2. Brechas de nomenclatura abiertas

| Elemento | Estado actual | Acción |
|---|---|---|
| Esquema de seguridad `bearerCognito` | **DESALINEADO**: el contrato conserva ese identificador y el generado expone `identidad.BearerCognitoScopes`, aunque V1 no usa Cognito | Usarlo tal cual mientras exista en `pf-pkg-contracts`; corregirlo en el contrato cuando se defina una versión neutral. No renombrarlo desde este servicio. |

## 3. Brechas de implementación abiertas

### 3.1 Adaptador real del IdP — EN ESPERA DE ADR 32

El puerto `internal/idp/IdentityProvider`, el doble en memoria y la suite de
contrato existen. El adaptador concreto para el IdP on-premise y su empaquetado
no se implementan hasta que ADR 32 seleccione el producto y la integración.

No se importa ningún SDK de proveedor desde este módulo. `cmd/api` usa el doble
en memoria para desarrollo standalone.

### 3.2 Composición de claims del IdP — EN ESPERA DE ADR 32

La composición de claims de AI27 existe como función pura y se prueba en emisión
y refresco. Falta el artefacto de integración con el IdP real, que depende de la
decisión de ADR 32.

### 3.3 Broker y despliegue on-premise — EN ESPERA DE ADR 32

El servicio conserva puertos agnósticos para publicación y usa el bus en memoria
en standalone. Falta seleccionar y empaquetar el broker real y el despliegue
correspondiente cuando ADR 32 cierre esas decisiones.

## 4. Elementos resueltos que ya no son brechas

### 4.1 Metadata reproducible de Go

`go.mod` fija versiones exactas y `go.sum` está versionado. La dependencia de
platform Go quedó actualizada a `v0.1.1` y las sumas fueron regeneradas.

### 4.2 Acceso del CI a módulos privados

El workflow valida la presencia de `GH_PAT`, configura Git para resolver los
módulos privados `AI27-Tech/*` y declara `GOPRIVATE`.

El secreto debe existir en la configuración del repositorio de GitHub; su
configuración no forma parte de este código.

### 4.3 Validación de arquitectura

`pf-pkg-platform-go v0.1.1` corrige la detección del paquete compartido
`api-client-go/roles`. El workflow ejecuta `arch-test@v0.1.1` y la validación
actual devuelve:

```text
arch-test: OK
```

La antigua brecha de `arch-test` ya no bloquea este repositorio.

## 5. Estado de cierre

El scaffold compila y pasa sus validaciones técnicas actuales. Las brechas
restantes son de contrato, de decisión de producto/arquitectura o de integración
con componentes todavía no seleccionados; no deben ocultarse implementando
superficies o tipos fuera de `pf-pkg-contracts`.
