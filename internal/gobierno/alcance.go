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

// Alcance es la lectura de obtenerAlcanceSujeto. Las tres listas van
// ordenadas y nunca son nil (el contrato las exige aunque estén vacías).
type Alcance struct {
	UnidadesIDs          []string
	SubordinadosIDs      []string
	UnidadesEfectivasIDs []string
}

// AlcanceRetirado es lo que el cambio de rol quitó al sujeto, para la traza.
type AlcanceRetirado struct {
	UnidadesIDs     []string
	SubordinadosIDs []string
	// SuperiorID es el superior del que salió; vacío si no tenía.
	SuperiorID string
}

func (r AlcanceRetirado) vacio() bool {
	return len(r.UnidadesIDs) == 0 && len(r.SubordinadosIDs) == 0 && r.SuperiorID == ""
}

// CandidatoSubordinado es lo que el servicio necesita saber de cada
// subordinado pedido: su rol activo ("" si no tiene) y su superior actual (""
// si no tiene).
type CandidatoSubordinado struct {
	ID         string
	Rol        autorizacion.Rol
	SuperiorID string
}

// AlcanceRepoTx es la persistencia del alcance. Las escrituras corren en la tx
// del caso de uso. La cumple gobierno/postgres.AlcanceRepo.
type AlcanceRepoTx interface {
	// RolActivoEnTx lee el rol activo del sujeto y bloquea su asignación (FOR
	// UPDATE) hasta el commit, para que un cambio de rol concurrente espere y
	// retire lo que esta tx escriba. ok=false si no tiene rol activo.
	RolActivoEnTx(ctx context.Context, tx pgx.Tx, sujetoID string) (rol autorizacion.Rol, ok bool, err error)
	// ReemplazarUnidades deja exactamente `unidades` como las del Monitorista.
	ReemplazarUnidades(ctx context.Context, tx pgx.Tx, monitoristaID string, unidades []string) error
	// CandidatosEnTx devuelve los sujetos existentes de `ids` con su rol activo
	// y su superior. Bloquea sus asignaciones en modo compartido (FOR SHARE).
	CandidatosEnTx(ctx context.Context, tx pgx.Tx, ids []string) ([]CandidatoSubordinado, error)
	// ReemplazarSubordinados deja exactamente `subordinados` bajo el superior.
	// Si otro superior tomó uno a la vez, devuelve ErrSubordinadoConSuperior
	// (unicidad de la tabla, DEC-145 #2).
	ReemplazarSubordinados(ctx context.Context, tx pgx.Tx, superiorID string, subordinados []string) error
	// Leer devuelve el alcance del sujeto, con las unidades efectivas por la
	// jerarquía de forma transitiva (DEC-140 #2).
	Leer(ctx context.Context, sujetoID string) (Alcance, error)
	// Retirar borra las unidades del sujeto y sus filas como superior y como
	// subordinado, y devuelve lo que borró.
	Retirar(ctx context.Context, tx pgx.Tx, sujetoID string) (AlcanceRetirado, error)
}

// FijacionUnidades es la entrada de fijarUnidadesSujeto.
type FijacionUnidades struct {
	SujetoID    string
	UnidadesIDs []string
	ActorID     string
	ActorRol    autorizacion.Rol
}

// FijacionSubordinados es la entrada de fijarSubordinadosSujeto.
type FijacionSubordinados struct {
	SujetoID        string
	SubordinadosIDs []string
	ActorID         string
	ActorRol        autorizacion.Rol
}

// LecturaAlcance es la entrada de obtenerAlcanceSujeto.
type LecturaAlcance struct {
	SujetoID string
	ActorRol autorizacion.Rol
}

// rolSubordinado dice qué rol deben tener los subordinados de cada superior
// (DEC-140 #2). Un rol fuera del mapa no tiene subordinados.
var rolSubordinado = map[autorizacion.Rol]autorizacion.Rol{
	autorizacion.RolSupervisor:  autorizacion.RolMonitorista,
	autorizacion.RolCoordinador: autorizacion.RolSupervisor,
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

// FijarSubordinadosSujeto reemplaza los subordinados directos de un Supervisor
// (Monitoristas) o de un Coordinador (Supervisores). Solo administrador
// (DEC-140 #2). La jerarquía es exclusiva (DEC-145 #2): un subordinado de otro
// superior es 409; si ya es de este, no es error.
func (s *servicio) FijarSubordinadosSujeto(ctx context.Context, in FijacionSubordinados) error {
	if err := guardarAdministrador(in.ActorRol == autorizacion.RolAdministrador); err != nil {
		return fmt.Errorf("fijar subordinados: %w", err)
	}
	ids, err := normalizarIDs(in.SubordinadosIDs, "sujetos_ids")
	if err != nil {
		return err
	}
	if _, ok, err := s.sujetos.PorID(ctx, in.SujetoID); err != nil {
		return fmt.Errorf("fijar subordinados: %w", err)
	} else if !ok {
		return fmt.Errorf("fijar subordinados: %w", autorizacion.ErrSujetoNoEncontrado)
	}
	err = s.enTx(ctx, func(tx pgx.Tx) error {
		rol, ok, e := s.alcance.RolActivoEnTx(ctx, tx, in.SujetoID)
		if e != nil {
			return e
		}
		esperado, tieneSubordinados := rolSubordinado[rol]
		if !ok || !tieneSubordinados {
			return ErrRolSinAlcance
		}
		candidatos, e := s.alcance.CandidatosEnTx(ctx, tx, ids)
		if e != nil {
			return e
		}
		if e := validarSubordinados(in.SujetoID, esperado, ids, candidatos); e != nil {
			return e
		}
		return s.alcance.ReemplazarSubordinados(ctx, tx, in.SujetoID, ids)
	})
	if err != nil {
		return fmt.Errorf("fijar subordinados: %w", err)
	}
	return nil
}

// validarSubordinados aplica, en este orden, existencia (400), rol esperado
// (409 subordinado_rol_invalido) y exclusividad (409 subordinado_con_superior).
func validarSubordinados(superiorID string, esperado autorizacion.Rol, ids []string, candidatos []CandidatoSubordinado) error {
	porID := make(map[string]CandidatoSubordinado, len(candidatos))
	for _, c := range candidatos {
		porID[c.ID] = c
	}
	for _, id := range ids {
		if _, ok := porID[id]; !ok {
			return platformerrors.NewValidacion("el subordinado %s no existe", id)
		}
	}
	for _, id := range ids {
		if porID[id].Rol != esperado {
			return ErrSubordinadoRolInvalido
		}
	}
	for _, id := range ids {
		if sup := porID[id].SuperiorID; sup != "" && sup != superiorID {
			return ErrSubordinadoConSuperior
		}
	}
	return nil
}

// ObtenerAlcanceSujeto lee el alcance de un sujeto. Lee el administrador; el
// jefe_operativo solo el de un Monitorista (DEC-142 #2); otro rol, 403.
func (s *servicio) ObtenerAlcanceSujeto(ctx context.Context, in LecturaAlcance) (Alcance, error) {
	if in.ActorRol != autorizacion.RolAdministrador && in.ActorRol != autorizacion.RolJefeOperativo {
		return Alcance{}, fmt.Errorf("obtener alcance: %w", ErrNoAutorizado)
	}
	det, err := s.detalle(ctx, in.SujetoID)
	if err != nil {
		return Alcance{}, fmt.Errorf("obtener alcance: %w", err)
	}
	if in.ActorRol == autorizacion.RolJefeOperativo && det.RolActivo != autorizacion.RolMonitorista {
		return Alcance{}, fmt.Errorf("obtener alcance: %w", ErrNoAutorizado)
	}
	a, err := s.alcance.Leer(ctx, in.SujetoID)
	if err != nil {
		return Alcance{}, fmt.Errorf("obtener alcance: %w", err)
	}
	return a, nil
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

// retirarAlcance quita, en la tx del cambio de rol, todo el alcance del sujeto
// y lo deja en el detalle de la traza `rol.revocado` (DEC-140 #3). El contrato
// no documenta un `tipo` de traza propio para el retiro.
func (s *servicio) retirarAlcance(ctx context.Context, tx pgx.Tx, sujetoID string) (map[string]any, error) {
	r, err := s.alcance.Retirar(ctx, tx, sujetoID)
	if err != nil {
		return nil, fmt.Errorf("retirar alcance: %w", err)
	}
	if r.vacio() {
		return nil, nil
	}
	retirado := map[string]any{"unidades_ids": r.UnidadesIDs, "subordinados_ids": r.SubordinadosIDs}
	if r.SuperiorID != "" {
		retirado["superior_id"] = r.SuperiorID
	}
	return map[string]any{"alcance_retirado": retirado}, nil
}
