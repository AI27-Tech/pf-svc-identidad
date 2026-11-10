package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// SujetoRepo implementa autorizacion.SujetoRepo sobre Postgres. Mapea
// violaciones de unicidad de idp_subject a ErrSujetoDuplicado.
type SujetoRepo struct {
	db Querier
}

// NewSujetoRepo crea un repositorio de sujetos sobre un Querier (una conexión
// *pgx.Conn en pruebas, o el *pgxpool.Pool del módulo en producción).
func NewSujetoRepo(db Querier) *SujetoRepo {
	return &SujetoRepo{db: db}
}

// columnasSujeto es la proyección común de un sujeto. El nombre y el
// identificador son NULL en un sujeto previo a tracer.40; se leen como "".
const columnasSujeto = `id, idp_subject, tenant_id, activo, COALESCE(nombre, ''), COALESCE(identificador_usuario, '')`

func escanearSujeto(row pgx.Row) (autorizacion.Sujeto, error) {
	var s autorizacion.Sujeto
	err := row.Scan(&s.ID, &s.IdpSubject, &s.TenantID, &s.Activo, &s.Nombre, &s.IdentificadorUsuario)
	return s, err
}

// Crear persiste un nuevo sujeto DENTRO de la transacción `tx` (atomicidad del
// alta de gobierno: Crear + AsignarInicial + traza + outbox en un solo commit,
// Requisito 6 de E4-T6). Si s.ID viene vacío, deja que la base genere el id
// (DEFAULT gen_random_uuid()): así lo usa el alta, que no fija el id. Si viene
// con valor, lo respeta (como las pruebas de E4-T2). Un nombre o identificador
// vacío se guarda como NULL. Retorna ErrSujetoDuplicado si el idp_subject ya
// existe (violación del UNIQUE).
func (r *SujetoRepo) Crear(ctx context.Context, tx pgx.Tx, s autorizacion.Sujeto) (autorizacion.Sujeto, error) {
	// NULLIF deja que la base genere el id cuando viene vacío, sin interpolar
	// "" como UUID (Postgres lo rechaza con 22P02).
	const query = `
		INSERT INTO identidad.sujeto (id, idp_subject, tenant_id, activo, nombre, identificador_usuario)
		VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''))
		RETURNING ` + columnasSujeto
	creado, err := escanearSujeto(tx.QueryRow(ctx, query,
		s.ID, s.IdpSubject, s.TenantID, s.Activo, s.Nombre, s.IdentificadorUsuario))
	if err == nil {
		return creado, nil
	}

	// Mapear error de violación de UNIQUE en idp_subject a ErrSujetoDuplicado.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return autorizacion.Sujeto{}, fmt.Errorf("crear sujeto: %w", autorizacion.ErrSujetoDuplicado)
	}
	return autorizacion.Sujeto{}, fmt.Errorf("crear sujeto: %w", err)
}

// PorIdpSubject consulta un sujeto por su identificador del IdP. Retorna
// (sujeto, true, nil) si existe, (cero, false, nil) si no, o un error en
// caso de fallo de BD.
func (r *SujetoRepo) PorIdpSubject(ctx context.Context, idpSubject string) (autorizacion.Sujeto, bool, error) {
	s, err := escanearSujeto(r.db.QueryRow(ctx,
		`SELECT `+columnasSujeto+` FROM identidad.sujeto WHERE idp_subject = $1`, idpSubject))
	if errors.Is(err, pgx.ErrNoRows) {
		return autorizacion.Sujeto{}, false, nil
	}
	if err != nil {
		return autorizacion.Sujeto{}, false, fmt.Errorf("consultar sujeto por idp_subject: %w", err)
	}
	return s, true, nil
}

// PorID consulta un sujeto por su identificador interno de AI27. Retorna
// (sujeto, true, nil) si existe, (cero, false, nil) si no, o un error en caso
// de fallo de BD. Lo usan el gobierno (consulta, cambio de estado) y el
// resolver de sujeto.
func (r *SujetoRepo) PorID(ctx context.Context, id string) (autorizacion.Sujeto, bool, error) {
	s, err := escanearSujeto(r.db.QueryRow(ctx,
		`SELECT `+columnasSujeto+` FROM identidad.sujeto WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return autorizacion.Sujeto{}, false, nil
	}
	if err != nil {
		return autorizacion.Sujeto{}, false, fmt.Errorf("consultar sujeto por id: %w", err)
	}
	return s, true, nil
}

// FijarActivo cambia el estado activo/inactivo de un sujeto DENTRO de la
// transacción del cambio (desactivación o reactivación de E4-T6). Recibe la
// `tx` para componer con la traza y el outbox en un solo commit (atomicidad,
// Requisito 6). Actualiza `actualizado_en`. No borra la fila: la identidad es
// referida por sesiones y auditoría (P10).
func (r *SujetoRepo) FijarActivo(ctx context.Context, tx pgx.Tx, id string, activo bool) error {
	const query = `
		UPDATE identidad.sujeto
		SET activo = $2, actualizado_en = now()
		WHERE id = $1
	`
	ct, err := tx.Exec(ctx, query, id, activo)
	if err != nil {
		return fmt.Errorf("fijar activo del sujeto: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("fijar activo del sujeto: %w", autorizacion.ErrSujetoNoEncontrado)
	}
	return nil
}

// FijarNombre cambia el nombre local del sujeto DENTRO de la transacción de
// editarSujeto (B-8). El identificador de acceso no se edita (DEC-139 #5).
func (r *SujetoRepo) FijarNombre(ctx context.Context, tx pgx.Tx, id, nombre string) error {
	ct, err := tx.Exec(ctx,
		`UPDATE identidad.sujeto SET nombre = $2, actualizado_en = now() WHERE id = $1`, id, nombre)
	if err != nil {
		return fmt.Errorf("fijar nombre del sujeto: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("fijar nombre del sujeto: %w", autorizacion.ErrSujetoNoEncontrado)
	}
	return nil
}

var _ autorizacion.SujetoRepo = (*SujetoRepo)(nil)

// TamanoPaginaSujetos es el máximo de sujetos por página de Listar.
const TamanoPaginaSujetos = 50

// Listar devuelve una página de sujetos con su rol activo, ordenada por id y
// paginada por un cursor opaco (el id de la última fila, en base64, como el del
// historial). cursorSiguiente es nil cuando no hay más páginas. Un cursor
// ilegible es un error de validación (400).
func (r *SujetoRepo) Listar(ctx context.Context, f autorizacion.FiltroSujetos, cursor string) (items []autorizacion.SujetoListado, cursorSiguiente *string, err error) {
	var desde *string
	if cursor != "" {
		id, err := decodificarCursorSujeto(cursor)
		if err != nil {
			return nil, nil, platformerrors.NewValidacion("cursor inválido")
		}
		desde = &id
	}
	// El índice de un solo rol activo (0005) garantiza a lo sumo una fila de
	// asignación por sujeto, así que el LEFT JOIN no duplica sujetos.
	const query = `
		SELECT s.id, s.idp_subject, s.tenant_id, s.activo,
		       COALESCE(s.nombre, ''), COALESCE(s.identificador_usuario, ''),
		       COALESCE(a.rol_codigo, '')
		  FROM identidad.sujeto s
		  LEFT JOIN identidad.asignacion_rol a
		    ON a.sujeto_id = s.id AND a.revocado_en IS NULL
		 WHERE ($1::text IS NULL OR s.nombre ILIKE $1 OR s.identificador_usuario ILIKE $1)
		   AND ($2::text IS NULL OR a.rol_codigo = $2)
		   AND ($3::uuid IS NULL OR s.tenant_id = $3)
		   AND ($4::boolean IS NULL OR s.activo = $4)
		   AND ($5::uuid IS NULL OR s.id > $5)
		 ORDER BY s.id
		 LIMIT $6`
	rows, err := r.db.Query(ctx, query,
		patronBusqueda(f.Q), nilSiVacio(string(f.Rol)), nilSiVacio(f.TenantID), f.Activo, desde, TamanoPaginaSujetos+1)
	if err != nil {
		return nil, nil, fmt.Errorf("listar sujetos: %w", err)
	}
	items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (autorizacion.SujetoListado, error) {
		var it autorizacion.SujetoListado
		var rol string
		err := row.Scan(&it.Sujeto.ID, &it.Sujeto.IdpSubject, &it.Sujeto.TenantID, &it.Sujeto.Activo,
			&it.Sujeto.Nombre, &it.Sujeto.IdentificadorUsuario, &rol)
		it.Rol = autorizacion.Rol(rol)
		return it, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("listar sujetos: %w", err)
	}
	if len(items) > TamanoPaginaSujetos {
		items = items[:TamanoPaginaSujetos]
		cur := base64.RawURLEncoding.EncodeToString([]byte(items[len(items)-1].Sujeto.ID))
		cursorSiguiente = &cur
	}
	return items, cursorSiguiente, nil
}

// patronBusqueda arma el patrón ILIKE de `q`, escapando los comodines para que
// `%` o `_` en el texto se busquen literales. nil si `q` viene vacío.
func patronBusqueda(q string) *string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	p := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	return &p
}

func decodificarCursorSujeto(cursor string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	id, err := uuid.Parse(string(raw))
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// nilSiVacio devuelve nil para una cadena vacía (NULL en el filtro SQL).
func nilSiVacio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
