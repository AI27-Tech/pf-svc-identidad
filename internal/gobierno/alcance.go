package gobierno

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// Alcance de monitoreo (tracer.40, E8-T19, DEC-140, DEC-145): "mis unidades"
// de cada Monitorista y la jerarquía Supervisor → Monitoristas y Coordinador →
// Supervisores. Es estado vigente: fijar reemplaza el conjunto y cambiar de rol
// lo retira (DEC-140 #3). El contrato no publica hechos de alcance, así que
// fijar no escribe outbox ni traza; el retiro sí queda en la traza.

// AlcanceRepoTx es la persistencia del alcance. Las escrituras corren en la tx
// del caso de uso. La cumple gobierno/postgres.AlcanceRepo.
type AlcanceRepoTx interface {
	// RolActivoEnTx lee el rol activo del sujeto y bloquea su asignación (FOR
	// UPDATE) hasta el commit, para que un cambio de rol concurrente espere y
	// retire lo que esta tx escriba. ok=false si no tiene rol activo.
	RolActivoEnTx(ctx context.Context, tx pgx.Tx, sujetoID string) (rol autorizacion.Rol, ok bool, err error)
	// ReemplazarUnidades deja exactamente `unidades` como las del Monitorista.
	ReemplazarUnidades(ctx context.Context, tx pgx.Tx, monitoristaID string, unidades []string) error
}

// FijacionUnidades es la entrada de fijarUnidadesSujeto.
type FijacionUnidades struct {
	SujetoID    string
	UnidadesIDs []string
	ActorID     string
	ActorRol    autorizacion.Rol
}

// FijarUnidadesSujeto reemplaza "mis unidades" de un Monitorista (DEC-140 #1).
// Escriben administrador y jefe_operativo (DEC-142 #2). Repetir la misma
// llamada deja el mismo estado. Una unidad puede estar en varios Monitoristas
// (DEC-145 #1); identidad no valida que exista en `operacion`.
func (s *servicio) FijarUnidadesSujeto(ctx context.Context, in FijacionUnidades) error {
	// Autoescalamiento antes que el rol del actor, como en ReemplazarRol: quien
	// toca su propio alcance recibe su código propio.
	if in.ActorID == in.SujetoID {
		return fmt.Errorf("fijar unidades: %w", ErrAutoescalamientoAlcance)
	}
	if in.ActorRol != autorizacion.RolAdministrador && in.ActorRol != autorizacion.RolJefeOperativo {
		return fmt.Errorf("fijar unidades: %w", ErrNoAutorizado)
	}
	unidades, err := normalizarIDs(in.UnidadesIDs, "unidades_ids")
	if err != nil {
		return err
	}
	if err := s.guardarSujetoActivo(ctx, in.SujetoID); err != nil {
		return fmt.Errorf("fijar unidades: %w", err)
	}
	err = s.enTx(ctx, func(tx pgx.Tx) error {
		rol, ok, e := s.alcance.RolActivoEnTx(ctx, tx, in.SujetoID)
		if e != nil {
			return e
		}
		if !ok || rol != autorizacion.RolMonitorista {
			return ErrRolSinAlcance
		}
		return s.alcance.ReemplazarUnidades(ctx, tx, in.SujetoID, unidades)
	})
	if err != nil {
		return fmt.Errorf("fijar unidades: %w", err)
	}
	return nil
}

// guardarSujetoActivo exige que el sujeto exista (404) y esté activo (409).
func (s *servicio) guardarSujetoActivo(ctx context.Context, sujetoID string) error {
	suj, ok, err := s.sujetos.PorID(ctx, sujetoID)
	if err != nil {
		return err
	}
	if !ok {
		return autorizacion.ErrSujetoNoEncontrado
	}
	if !suj.Activo {
		return ErrSujetoDesactivado
	}
	return nil
}

// normalizarIDs valida que cada id sea UUID (400) y quita repetidos en
// silencio, porque el contrato no los prohíbe. Devuelve los ids canónicos,
// ordenados.
func normalizarIDs(ids []string, campo string) ([]string, error) {
	vistos := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, platformerrors.NewValidacion("%s: id inválido %q", campo, raw)
		}
		if k := id.String(); !vistos[k] {
			vistos[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}
