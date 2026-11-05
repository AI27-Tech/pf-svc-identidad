// Package postgres implementa, sobre Postgres, los repositorios del gobierno de
// identidad (E4-T6): la traza inmutable `cambio_identidad` y la tabla `tenant`.
// Sigue el estilo de internal/sesion/store.go: SQL plano, sin lógica de
// decisión, traduciendo entre el modelo de dominio y el relacional.
package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
)

// TrazaRepo escribe y lee la traza de gobierno `cambio_identidad`. La tabla es
// de solo inserción por privilegio (migración 0002: GRANT SELECT, INSERT), así
// que este repo nunca hace UPDATE ni DELETE: la traza es inmutable (ADR 26 §7).
type TrazaRepo struct {
	db querier
}

// NewTrazaRepo crea un repositorio de traza sobre un querier (conexión en
// pruebas, pool del módulo en producción).
func NewTrazaRepo(db querier) *TrazaRepo {
	return &TrazaRepo{db: db}
}

// Registrar inserta una fila de traza DENTRO de la transacción del cambio
// administrativo (atomicidad con el estado y el outbox, Requisito 6). El `tipo`
// es el nombre canónico del hecho (sujeto.creado, rol.asignado, …). El actor no
// puede ser vacío (Requisito 1.9).
func (r *TrazaRepo) Registrar(ctx context.Context, tx pgx.Tx, c gobierno.CambioAdministrativo) error {
	if c.ActorID == "" {
		return fmt.Errorf("registrar traza: %w", errActorVacio)
	}
	detalle := c.Detalle
	if detalle == nil {
		detalle = map[string]any{}
	}
	const query = `
		INSERT INTO identidad.cambio_identidad (tipo, sujeto_id, tenant_id, actor_id, detalle)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := tx.Exec(ctx, query, c.Tipo, c.SujetoID, c.TenantID, c.ActorID, detalle)
	if err != nil {
		return fmt.Errorf("registrar traza: %w", err)
	}
	return nil
}

// tamanoPagina es el número máximo de cambios por página del historial.
const tamanoPagina = 50

// Historial devuelve los cambios de un sujeto ordenados por tiempo descendente,
// paginados por cursor (apoyado en el índice cambio_identidad_sujeto_ts_idx,
// migración 0004). El cursor codifica el id de la última fila devuelta; nil
// cuando no hay más páginas, al estilo de PaginaRevocadas de sesión.
func (r *TrazaRepo) Historial(ctx context.Context, sujetoID, cursor string) (gobierno.PaginaHistorial, error) {
	// El cursor es el `id` (BIGSERIAL) de la última fila de la página anterior.
	// La primera página usa un id "infinito" (sin filtro superior).
	desdeID := int64(0)
	tieneCursor := false
	if cursor != "" {
		id, err := decodificarCursor(cursor)
		if err != nil {
			return gobierno.PaginaHistorial{}, fmt.Errorf("historial: cursor inválido: %w", err)
		}
		desdeID = id
		tieneCursor = true
	}

	// Se pide un elemento de más para saber si hay página siguiente.
	const query = `
		SELECT id, tipo, sujeto_id, tenant_id, actor_id, ocurrido_en
		FROM identidad.cambio_identidad
		WHERE sujeto_id = $1
		  AND ($2 = false OR id < $3)
		ORDER BY id DESC
		LIMIT $4
	`
	rows, err := r.db.Query(ctx, query, sujetoID, tieneCursor, desdeID, tamanoPagina+1)
	if err != nil {
		return gobierno.PaginaHistorial{}, fmt.Errorf("historial: consultar: %w", err)
	}
	defer rows.Close()

	type fila struct {
		id     int64
		cambio gobierno.CambioAdministrativo
	}
	var filas []fila
	for rows.Next() {
		var id int64
		var c gobierno.CambioAdministrativo
		var tenantID, actorID *string
		var ocurridoEn time.Time
		if err := rows.Scan(&id, &c.Tipo, &c.SujetoID, &tenantID, &actorID, &ocurridoEn); err != nil {
			return gobierno.PaginaHistorial{}, fmt.Errorf("historial: escanear: %w", err)
		}
		if tenantID != nil {
			c.TenantID = *tenantID
		}
		if actorID != nil {
			c.ActorID = *actorID
		}
		c.OcurridoEn = ocurridoEn.UTC().Format(time.RFC3339)
		filas = append(filas, fila{id: id, cambio: c})
	}
	if err := rows.Err(); err != nil {
		return gobierno.PaginaHistorial{}, fmt.Errorf("historial: iterar: %w", err)
	}

	var pagina gobierno.PaginaHistorial
	hayMas := len(filas) > tamanoPagina
	if hayMas {
		filas = filas[:tamanoPagina]
	}
	for _, f := range filas {
		pagina.Cambios = append(pagina.Cambios, f.cambio)
	}
	if hayMas && len(filas) > 0 {
		cur := codificarCursor(filas[len(filas)-1].id)
		pagina.CursorSiguiente = &cur
	}
	return pagina, nil
}

var errActorVacio = errors.New("la traza de gobierno exige un actor no vacío")

// codificarCursor y decodificarCursor envuelven el id de fila en un valor opaco
// base64, como el Cursor de _comunes.yaml. No expone el id crudo al cliente.
func codificarCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodificarCursor(cursor string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(raw), 10, 64)
}
