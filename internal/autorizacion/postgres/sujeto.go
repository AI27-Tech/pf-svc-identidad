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
	db *pgx.Conn
}

// NewSujetoRepo crea un repositorio de sujetos sobre una conexión Postgres.
func NewSujetoRepo(db *pgx.Conn) *SujetoRepo {
	return &SujetoRepo{db: db}
}

// Crear persiste un nuevo sujeto. Retorna ErrSujetoDuplicado si el
// idp_subject ya existe (violación del UNIQUE).
func (r *SujetoRepo) Crear(ctx context.Context, s autorizacion.Sujeto) (autorizacion.Sujeto, error) {
	const query = `
		INSERT INTO identidad.sujeto (id, idp_subject, tenant_id, activo)
		VALUES ($1, $2, $3, $4)
		RETURNING id, idp_subject, tenant_id, activo
	`

	var creado autorizacion.Sujeto
	err := r.db.QueryRow(ctx, query, s.ID, s.IdpSubject, s.TenantID, s.Activo).
		Scan(&creado.ID, &creado.IdpSubject, &creado.TenantID, &creado.Activo)

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

var _ autorizacion.SujetoRepo = (*SujetoRepo)(nil)
