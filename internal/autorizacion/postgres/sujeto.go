package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

// Crear persiste un nuevo sujeto DENTRO de la transacción `tx` (atomicidad del
// alta de gobierno: Crear + AsignarInicial + traza + outbox en un solo commit,
// Requisito 6 de E4-T6). Si s.ID viene vacío, deja que la base genere el id
// (DEFAULT gen_random_uuid()): así lo usa el alta, que no fija el id. Si viene
// con valor, lo respeta (como las pruebas de E4-T2). Retorna ErrSujetoDuplicado
// si el idp_subject ya existe (violación del UNIQUE).
func (r *SujetoRepo) Crear(ctx context.Context, tx pgx.Tx, s autorizacion.Sujeto) (autorizacion.Sujeto, error) {
	// Dos variantes del INSERT: con id explícito o dejando el DEFAULT de la BD.
	// No se interpola el id vacío como UUID (Postgres lo rechaza con 22P02).
	conID := `
		INSERT INTO identidad.sujeto (id, idp_subject, tenant_id, activo)
		VALUES ($1, $2, $3, $4)
		RETURNING id, idp_subject, tenant_id, activo
	`
	sinID := `
		INSERT INTO identidad.sujeto (idp_subject, tenant_id, activo)
		VALUES ($1, $2, $3)
		RETURNING id, idp_subject, tenant_id, activo
	`

	var creado autorizacion.Sujeto
	var err error
	if s.ID == "" {
		err = tx.QueryRow(ctx, sinID, s.IdpSubject, s.TenantID, s.Activo).
			Scan(&creado.ID, &creado.IdpSubject, &creado.TenantID, &creado.Activo)
	} else {
		err = tx.QueryRow(ctx, conID, s.ID, s.IdpSubject, s.TenantID, s.Activo).
			Scan(&creado.ID, &creado.IdpSubject, &creado.TenantID, &creado.Activo)
	}

	if err == nil {
		return creado, nil
	}

	// Mapear error de violación de UNIQUE en idp_subject a ErrSujetoDuplicado.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" { // unique_violation
			return autorizacion.Sujeto{}, fmt.Errorf("crear sujeto: %w", autorizacion.ErrSujetoDuplicado)
		}
	}

	return autorizacion.Sujeto{}, fmt.Errorf("crear sujeto: %w", err)
}

// PorIdpSubject consulta un sujeto por su identificador del IdP. Retorna
// (sujeto, true, nil) si existe, (cero, false, nil) si no, o un error en
// caso de fallo de BD.
func (r *SujetoRepo) PorIdpSubject(ctx context.Context, idpSubject string) (autorizacion.Sujeto, bool, error) {
	const query = `
		SELECT id, idp_subject, tenant_id, activo
		FROM identidad.sujeto
		WHERE idp_subject = $1
	`

	var s autorizacion.Sujeto
	err := r.db.QueryRow(ctx, query, idpSubject).
		Scan(&s.ID, &s.IdpSubject, &s.TenantID, &s.Activo)

	if err == pgx.ErrNoRows {
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
	const query = `
		SELECT id, idp_subject, tenant_id, activo
		FROM identidad.sujeto
		WHERE id = $1
	`
	var s autorizacion.Sujeto
	err := r.db.QueryRow(ctx, query, id).
		Scan(&s.ID, &s.IdpSubject, &s.TenantID, &s.Activo)
	if err == pgx.ErrNoRows {
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

var _ autorizacion.SujetoRepo = (*SujetoRepo)(nil)
