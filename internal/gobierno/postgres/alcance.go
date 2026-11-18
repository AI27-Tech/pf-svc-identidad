package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/gobierno"
)

// AlcanceRepo lee y escribe el alcance de monitoreo (0010, 0011):
// `alcance_cliente`, `alcance_unidad`, `jerarquia` y `alcance_efectivo`. SQL
// plano; las reglas viven en el servicio.
type AlcanceRepo struct {
	db querier
}

// NewAlcanceRepo crea el repositorio de alcance sobre un querier (conexión en
// pruebas, pool del módulo en producción).
func NewAlcanceRepo(db querier) *AlcanceRepo {
	return &AlcanceRepo{db: db}
}

var _ gobierno.AlcanceRepoTx = (*AlcanceRepo)(nil)

// RolActivoEnTx lee el rol activo y bloquea su fila de asignación hasta el
// commit. Un ReemplazarRol concurrente espera en su UPDATE y, al seguir, su
// DELETE del alcance ya ve lo que esta tx escribió (READ COMMITTED).
func (r *AlcanceRepo) RolActivoEnTx(ctx context.Context, tx pgx.Tx, sujetoID string) (autorizacion.Rol, bool, error) {
	var rol string
	err := tx.QueryRow(ctx, `
		SELECT rol_codigo FROM identidad.asignacion_rol
		 WHERE sujeto_id = $1 AND revocado_en IS NULL
		   FOR UPDATE`, sujetoID).Scan(&rol)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("leer rol activo: %w", err)
	}
	return autorizacion.Rol(rol), true, nil
}

// CandidatosEnTx bloquea en modo compartido las asignaciones activas de `ids`
// (un cambio de rol concurrente de un subordinado espera) y luego lee, en otra
// sentencia para ver el estado ya comprometido, su rol y su superior.
func (r *AlcanceRepo) CandidatosEnTx(ctx context.Context, tx pgx.Tx, ids []string) ([]gobierno.CandidatoSubordinado, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `
		SELECT 1 FROM identidad.asignacion_rol
		 WHERE sujeto_id = ANY($1::uuid[]) AND revocado_en IS NULL
		   FOR SHARE`, ids); err != nil {
		return nil, fmt.Errorf("bloquear subordinados: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT s.id::text, coalesce(a.rol_codigo, ''), coalesce(j.superior_id::text, '')
		  FROM identidad.sujeto s
		  LEFT JOIN identidad.asignacion_rol a ON a.sujeto_id = s.id AND a.revocado_en IS NULL
		  LEFT JOIN identidad.jerarquia j ON j.subordinado_id = s.id
		 WHERE s.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("leer subordinados: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (gobierno.CandidatoSubordinado, error) {
		var c gobierno.CandidatoSubordinado
		var rol string
		err := row.Scan(&c.ID, &rol, &c.SuperiorID)
		c.Rol = autorizacion.Rol(rol)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("leer subordinados: %w", err)
	}
	return out, nil
}

// ReemplazarUnidades borra las unidades del Monitorista e inserta el conjunto
// nuevo. Con el mismo conjunto el estado final es el mismo.
func (r *AlcanceRepo) ReemplazarUnidades(ctx context.Context, tx pgx.Tx, monitoristaID string, unidades []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM identidad.alcance_unidad WHERE monitorista_id = $1`, monitoristaID); err != nil {
		return fmt.Errorf("reemplazar unidades: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO identidad.alcance_unidad (monitorista_id, unidad_id)
		SELECT $1, u FROM unnest($2::uuid[]) AS u`, monitoristaID, unidades); err != nil {
		return fmt.Errorf("reemplazar unidades: %w", err)
	}
	return nil
}

// ReemplazarClientes borra los clientes del Monitorista e inserta el conjunto
// nuevo. Con el mismo conjunto el estado final es el mismo.
func (r *AlcanceRepo) ReemplazarClientes(ctx context.Context, tx pgx.Tx, monitoristaID string, clientes []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM identidad.alcance_cliente WHERE monitorista_id = $1`, monitoristaID); err != nil {
		return fmt.Errorf("reemplazar clientes: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO identidad.alcance_cliente (monitorista_id, cliente_id)
		SELECT $1, c FROM unnest($2::uuid[]) AS c`, monitoristaID, clientes); err != nil {
		return fmt.Errorf("reemplazar clientes: %w", err)
	}
	return nil
}

// BloquearEnTx toma `alcance_efectivo` en SHARE ROW EXCLUSIVE, que choca
// consigo mismo y con el SHARE de AlcancesEnTx: las escrituras de alcance van
// de una en una y una lectura espera a que comprometa la que está en curso. La
// hora se toma ya con el candado, así que es posterior a toda lectura previa y
// el cursor por hora no pierde cambios (cierra la ventana H-6 que el log de
// revocaciones acepta).
func (r *AlcanceRepo) BloquearEnTx(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	return bloquear(ctx, tx, "SHARE ROW EXCLUSIVE")
}

func bloquear(ctx context.Context, tx pgx.Tx, modo string) (time.Time, error) {
	if _, err := tx.Exec(ctx, `LOCK TABLE identidad.alcance_efectivo IN `+modo+` MODE`); err != nil {
		return time.Time{}, fmt.Errorf("bloquear alcance: %w", err)
	}
	var ahora time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&ahora); err != nil {
		return time.Time{}, fmt.Errorf("hora del alcance: %w", err)
	}
	return ahora.UTC(), nil
}

// GrafoEnTx lee todos los clientes por Monitorista y toda la jerarquía.
//
// shortcut: carga el grafo completo en cada cambio de alcance; basta para los
// cientos de filas de V1, pasar a leer solo la cadena del sujeto si crece a
// miles.
func (r *AlcanceRepo) GrafoEnTx(ctx context.Context, tx pgx.Tx) (gobierno.GrafoAlcance, error) {
	g := gobierno.GrafoAlcance{Clientes: map[string][]string{}, Superior: map[string]string{}}
	rows, err := tx.Query(ctx, `SELECT monitorista_id::text, cliente_id::text FROM identidad.alcance_cliente`)
	if err != nil {
		return g, fmt.Errorf("leer clientes: %w", err)
	}
	var m, c string
	if _, err := pgx.ForEachRow(rows, []any{&m, &c}, func() error {
		g.Clientes[m] = append(g.Clientes[m], c)
		return nil
	}); err != nil {
		return g, fmt.Errorf("leer clientes: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT subordinado_id::text, superior_id::text FROM identidad.jerarquia`)
	if err != nil {
		return g, fmt.Errorf("leer jerarquía: %w", err)
	}
	var sub, sup string
	if _, err := pgx.ForEachRow(rows, []any{&sub, &sup}, func() error {
		g.Superior[sub] = sup
		return nil
	}); err != nil {
		return g, fmt.Errorf("leer jerarquía: %w", err)
	}
	return g, nil
}

// GuardarEfectivos deja cada cambio como el conjunto vigente de su sujeto y
// llena su IdpSubject desde `sujeto` (DEC-149 #22).
func (r *AlcanceRepo) GuardarEfectivos(ctx context.Context, tx pgx.Tx, cambios []gobierno.AlcanceEfectivo) error {
	for i, c := range cambios {
		if err := tx.QueryRow(ctx, `
			WITH guardado AS (
			  INSERT INTO identidad.alcance_efectivo (sujeto_id, clientes_efectivos_ids, cambiado_en)
			  VALUES ($1, $2::uuid[], $3)
			  ON CONFLICT (sujeto_id) DO UPDATE
			     SET clientes_efectivos_ids = EXCLUDED.clientes_efectivos_ids, cambiado_en = EXCLUDED.cambiado_en
			  RETURNING sujeto_id)
			SELECT s.idp_subject FROM guardado g JOIN identidad.sujeto s ON s.id = g.sujeto_id`,
			c.SujetoID, c.ClientesEfectivosIDs, c.CambiadoEn).Scan(&cambios[i].IdpSubject); err != nil {
			return fmt.Errorf("guardar clientes efectivos: %w", err)
		}
	}
	return nil
}

// AlcancesEnTx toma `alcance_efectivo` en SHARE (espera a la escritura en
// curso; ver BloquearEnTx) y lee, con la hora de la lectura: sin `desde`, los
// sujetos con al menos un cliente; con `desde`, todos los que cambiaron
// después, aunque hayan quedado vacíos.
func (r *AlcanceRepo) AlcancesEnTx(ctx context.Context, tx pgx.Tx, desde *time.Time) ([]gobierno.AlcanceEfectivo, time.Time, error) {
	ahora, err := bloquear(ctx, tx, "SHARE")
	if err != nil {
		return nil, time.Time{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT ae.sujeto_id::text, s.idp_subject, ae.clientes_efectivos_ids::text[], ae.cambiado_en
		  FROM identidad.alcance_efectivo ae
		  JOIN identidad.sujeto s ON s.id = ae.sujeto_id
		 WHERE ae.cambiado_en <= $1
		   AND ($2::timestamptz IS NULL AND cardinality(ae.clientes_efectivos_ids) > 0
		        OR ae.cambiado_en > $2::timestamptz)
		 ORDER BY ae.cambiado_en, ae.sujeto_id`, ahora, desde)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("leer alcances: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (gobierno.AlcanceEfectivo, error) {
		var a gobierno.AlcanceEfectivo
		err := row.Scan(&a.SujetoID, &a.IdpSubject, &a.ClientesEfectivosIDs, &a.CambiadoEn)
		a.CambiadoEn = a.CambiadoEn.UTC()
		if a.ClientesEfectivosIDs == nil {
			a.ClientesEfectivosIDs = []string{}
		}
		return a, err
	})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("leer alcances: %w", err)
	}
	return out, ahora, nil
}

// ReemplazarSubordinados borra los subordinados del superior e inserta el
// conjunto nuevo. Si otro superior insertó a uno de ellos en paralelo, la
// unicidad de `subordinado_id` lo rechaza y se traduce al centinela.
func (r *AlcanceRepo) ReemplazarSubordinados(ctx context.Context, tx pgx.Tx, superiorID string, subordinados []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM identidad.jerarquia WHERE superior_id = $1`, superiorID); err != nil {
		return fmt.Errorf("reemplazar subordinados: %w", err)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO identidad.jerarquia (superior_id, subordinado_id)
		SELECT $1, s FROM unnest($2::uuid[]) AS s`, superiorID, subordinados)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "jerarquia_subordinado_unico" {
		return fmt.Errorf("reemplazar subordinados: %w: %w", gobierno.ErrSubordinadoConSuperior, err)
	}
	if err != nil {
		return fmt.Errorf("reemplazar subordinados: %w", err)
	}
	return nil
}

// Retirar borra todo el alcance del sujeto: sus clientes, sus unidades, sus
// subordinados y su lugar bajo un superior. Devuelve lo borrado, con listas no
// nil.
func (r *AlcanceRepo) Retirar(ctx context.Context, tx pgx.Tx, sujetoID string) (gobierno.AlcanceRetirado, error) {
	var out gobierno.AlcanceRetirado
	var err error
	if out.ClientesIDs, err = idsDe(tx.Query(ctx,
		`DELETE FROM identidad.alcance_cliente WHERE monitorista_id = $1 RETURNING cliente_id::text`, sujetoID)); err != nil {
		return out, fmt.Errorf("retirar clientes: %w", err)
	}
	if out.UnidadesIDs, err = idsDe(tx.Query(ctx,
		`DELETE FROM identidad.alcance_unidad WHERE monitorista_id = $1 RETURNING unidad_id::text`, sujetoID)); err != nil {
		return out, fmt.Errorf("retirar unidades: %w", err)
	}
	if out.SubordinadosIDs, err = idsDe(tx.Query(ctx,
		`DELETE FROM identidad.jerarquia WHERE superior_id = $1 RETURNING subordinado_id::text`, sujetoID)); err != nil {
		return out, fmt.Errorf("retirar subordinados: %w", err)
	}
	superiores, err := idsDe(tx.Query(ctx,
		`DELETE FROM identidad.jerarquia WHERE subordinado_id = $1 RETURNING superior_id::text`, sujetoID))
	if err != nil {
		return out, fmt.Errorf("retirar superior: %w", err)
	}
	if len(superiores) > 0 {
		out.SuperiorID = superiores[0] // a lo sumo uno: UNIQUE(subordinado_id)
	}
	return out, nil
}

// Leer devuelve los clientes y las unidades directos, los subordinados
// directos, los clientes efectivos guardados (los mismos que publicó
// `alcance.cambiado`; sin fila, ninguno) y la unión transitiva de unidades por
// la jerarquía (Coordinador → Supervisores → Monitoristas). UNION en el CTE
// descarta repetidos y corta cualquier ciclo.
func (r *AlcanceRepo) Leer(ctx context.Context, sujetoID string) (gobierno.Alcance, error) {
	var out gobierno.Alcance
	var err error
	if out.ClientesIDs, err = idsDe(r.db.Query(ctx,
		`SELECT cliente_id::text FROM identidad.alcance_cliente WHERE monitorista_id = $1 ORDER BY 1`, sujetoID)); err != nil {
		return out, fmt.Errorf("leer clientes: %w", err)
	}
	if out.ClientesEfectivosIDs, err = idsDe(r.db.Query(ctx,
		`SELECT c::text FROM identidad.alcance_efectivo, unnest(clientes_efectivos_ids) AS c WHERE sujeto_id = $1 ORDER BY 1`, sujetoID)); err != nil {
		return out, fmt.Errorf("leer clientes efectivos: %w", err)
	}
	if out.UnidadesIDs, err = idsDe(r.db.Query(ctx,
		`SELECT unidad_id::text FROM identidad.alcance_unidad WHERE monitorista_id = $1 ORDER BY 1`, sujetoID)); err != nil {
		return out, fmt.Errorf("leer unidades: %w", err)
	}
	if out.SubordinadosIDs, err = idsDe(r.db.Query(ctx,
		`SELECT subordinado_id::text FROM identidad.jerarquia WHERE superior_id = $1 ORDER BY 1`, sujetoID)); err != nil {
		return out, fmt.Errorf("leer subordinados: %w", err)
	}
	if out.UnidadesEfectivasIDs, err = idsDe(r.db.Query(ctx, `
		WITH RECURSIVE equipo(id) AS (
		  SELECT $1::uuid
		  UNION
		  SELECT j.subordinado_id FROM identidad.jerarquia j JOIN equipo e ON j.superior_id = e.id
		)
		SELECT DISTINCT u.unidad_id::text
		  FROM identidad.alcance_unidad u JOIN equipo e ON u.monitorista_id = e.id
		 ORDER BY 1`, sujetoID)); err != nil {
		return out, fmt.Errorf("leer unidades efectivas: %w", err)
	}
	return out, nil
}

// idsDe recoge una columna de texto en una lista no nil.
func idsDe(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}
