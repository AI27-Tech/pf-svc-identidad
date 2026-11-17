package gobierno

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// Alcance de monitoreo (tracer.40 y tracer.41, DEC-140, DEC-145, DEC-149):
// "mis clientes" de cada Monitorista (DEC-149 #18), la jerarquía Supervisor →
// Monitoristas y Coordinador → Supervisores, y "mis unidades" (deprecada, ya no
// define el alcance). Es estado vigente: fijar reemplaza el conjunto y cambiar
// de rol lo retira (DEC-140 #3), con el retiro en la traza.
//
// Los clientes efectivos de un sujeto son los suyos más los de sus subordinados
// por la jerarquía. Cada escritura que puede moverlos (fijar clientes, fijar
// subordinados, retirar) los compara antes y después en su tx y publica
// `alcance.cambiado` para cada sujeto cuyo conjunto cambió (DEC-149 #20).

// Alcance es la lectura de obtenerAlcanceSujeto. Las listas van ordenadas y
// nunca son nil (el contrato las exige aunque estén vacías).
type Alcance struct {
	ClientesIDs          []string
	ClientesEfectivosIDs []string
	SubordinadosIDs      []string
	// Deprecadas por DEC-149 #18; siguen en el contrato hasta otra ola.
	UnidadesIDs          []string
	UnidadesEfectivasIDs []string
}

// AlcanceRetirado es lo que el cambio de rol quitó al sujeto, para la traza.
type AlcanceRetirado struct {
	ClientesIDs     []string
	UnidadesIDs     []string
	SubordinadosIDs []string
	// SuperiorID es el superior del que salió; vacío si no tenía.
	SuperiorID string
}

func (r AlcanceRetirado) vacio() bool {
	return len(r.ClientesIDs) == 0 && len(r.UnidadesIDs) == 0 && len(r.SubordinadosIDs) == 0 && r.SuperiorID == ""
}

// AlcanceEfectivo son los clientes efectivos de un sujeto y cuándo cambiaron.
// Es el payload de `alcance.cambiado` y el item de listarAlcances.
type AlcanceEfectivo struct {
	SujetoID             string
	ClientesEfectivosIDs []string
	CambiadoEn           time.Time
}

// PaginaAlcances es la respuesta de listarAlcances: los alcances y el cursor
// para pedir lo que cambie después.
type PaginaAlcances struct {
	Items           []AlcanceEfectivo
	CursorSiguiente *string
}

// GrafoAlcance es lo que decide los clientes efectivos: los clientes de cada
// Monitorista y el superior de cada subordinado.
type GrafoAlcance struct {
	Clientes map[string][]string // monitorista_id → cliente_id
	Superior map[string]string   // subordinado_id → superior_id
}

// efectivos devuelve la unión, ordenada y sin repetidos, de los clientes de
// `id` y los de sus subordinados de forma transitiva (DEC-140 #2). Nunca nil.
func (g GrafoAlcance) efectivos(id string) []string {
	subordinados := make(map[string][]string, len(g.Superior))
	for sub, sup := range g.Superior {
		subordinados[sup] = append(subordinados[sup], sub)
	}
	vistos := map[string]bool{id: true}
	clientes := map[string]bool{}
	pendientes := []string{id}
	for len(pendientes) > 0 {
		x := pendientes[0]
		pendientes = pendientes[1:]
		for _, c := range g.Clientes[x] {
			clientes[c] = true
		}
		for _, sub := range subordinados[x] {
			if !vistos[sub] {
				vistos[sub] = true
				pendientes = append(pendientes, sub)
			}
		}
	}
	out := make([]string, 0, len(clientes))
	for c := range clientes {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// sujetos devuelve todos los sujetos que aparecen en el grafo.
func (g GrafoAlcance) sujetos() map[string]bool {
	out := make(map[string]bool, len(g.Clientes)+2*len(g.Superior))
	for id := range g.Clientes {
		out[id] = true
	}
	for sub, sup := range g.Superior {
		out[sub], out[sup] = true, true
	}
	return out
}

// cambiosDeAlcance devuelve, ordenados por sujeto, los sujetos cuyos clientes
// efectivos difieren entre `antes` y `despues`, con su conjunto nuevo (vacío si
// se quedó sin clientes). CambiadoEn lo pone quien publica.
func cambiosDeAlcance(antes, despues GrafoAlcance) []AlcanceEfectivo {
	candidatos := antes.sujetos()
	for id := range despues.sujetos() {
		candidatos[id] = true
	}
	ids := make([]string, 0, len(candidatos))
	for id := range candidatos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []AlcanceEfectivo
	for _, id := range ids {
		nuevo := despues.efectivos(id)
		if !slices.Equal(antes.efectivos(id), nuevo) {
			out = append(out, AlcanceEfectivo{SujetoID: id, ClientesEfectivosIDs: nuevo})
		}
	}
	return out
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
	// ReemplazarClientes deja exactamente `clientes` como los del Monitorista.
	ReemplazarClientes(ctx context.Context, tx pgx.Tx, monitoristaID string, clientes []string) error
	// BloquearEnTx serializa las escrituras de alcance hasta el commit y
	// devuelve la hora, tomada ya con el candado, que marca el cambio.
	BloquearEnTx(ctx context.Context, tx pgx.Tx) (time.Time, error)
	// GrafoEnTx lee los clientes de cada Monitorista y la jerarquía.
	GrafoEnTx(ctx context.Context, tx pgx.Tx) (GrafoAlcance, error)
	// GuardarEfectivos deja `cambios` como el último conjunto publicado de cada
	// sujeto, el que sirve listarAlcances.
	GuardarEfectivos(ctx context.Context, tx pgx.Tx, cambios []AlcanceEfectivo) error
	// AlcancesEnTx lee los alcances guardados que cambiaron después de `desde`,
	// o los que tienen al menos un cliente si `desde` es nil, y la hora de la
	// lectura. Espera a que terminen las escrituras de alcance en curso.
	AlcancesEnTx(ctx context.Context, tx pgx.Tx, desde *time.Time) ([]AlcanceEfectivo, time.Time, error)
	// CandidatosEnTx devuelve los sujetos existentes de `ids` con su rol activo
	// y su superior. Bloquea sus asignaciones en modo compartido (FOR SHARE).
	CandidatosEnTx(ctx context.Context, tx pgx.Tx, ids []string) ([]CandidatoSubordinado, error)
	// ReemplazarSubordinados deja exactamente `subordinados` bajo el superior.
	// Si otro superior tomó uno a la vez, devuelve ErrSubordinadoConSuperior
	// (unicidad de la tabla, DEC-145 #2).
	ReemplazarSubordinados(ctx context.Context, tx pgx.Tx, superiorID string, subordinados []string) error
	// Leer devuelve el alcance del sujeto, con las unidades efectivas por la
	// jerarquía de forma transitiva (DEC-140 #2) y los clientes efectivos
	// guardados (los mismos que publicó `alcance.cambiado`).
	Leer(ctx context.Context, sujetoID string) (Alcance, error)
	// Retirar borra los clientes y las unidades del sujeto y sus filas como
	// superior y como subordinado, y devuelve lo que borró.
	Retirar(ctx context.Context, tx pgx.Tx, sujetoID string) (AlcanceRetirado, error)
}

// FijacionUnidades es la entrada de fijarUnidadesSujeto.
type FijacionUnidades struct {
	SujetoID    string
	UnidadesIDs []string
	ActorID     string
	ActorRol    autorizacion.Rol
}

// FijacionClientes es la entrada de fijarClientesSujeto.
type FijacionClientes struct {
	SujetoID    string
	ClientesIDs []string
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

// FijarClientesSujeto reemplaza "mis clientes" de un Monitorista (DEC-149
// #18) con las reglas de "mis unidades": escriben administrador y
// jefe_operativo, un cliente puede estar en varios Monitoristas e identidad no
// valida que exista en `catalogos` (lo valida el `bff`). Publica
// `alcance.cambiado` para el Monitorista y sus superiores si su conjunto
// efectivo cambia; repetir la misma llamada no publica nada.
func (s *servicio) FijarClientesSujeto(ctx context.Context, in FijacionClientes) error {
	if in.ActorID == in.SujetoID {
		return fmt.Errorf("fijar clientes: %w", ErrAutoescalamientoAlcance)
	}
	if in.ActorRol != autorizacion.RolAdministrador && in.ActorRol != autorizacion.RolJefeOperativo {
		return fmt.Errorf("fijar clientes: %w", ErrNoAutorizado)
	}
	clientes, err := normalizarIDs(in.ClientesIDs, "clientes_ids")
	if err != nil {
		return err
	}
	if err := s.guardarSujetoActivo(ctx, in.SujetoID); err != nil {
		return fmt.Errorf("fijar clientes: %w", err)
	}
	err = s.enTx(ctx, func(tx pgx.Tx) error {
		rol, ok, e := s.alcance.RolActivoEnTx(ctx, tx, in.SujetoID)
		if e != nil {
			return e
		}
		if !ok || rol != autorizacion.RolMonitorista {
			return ErrRolSinAlcance
		}
		return s.cambiarAlcance(ctx, tx, func() error {
			return s.alcance.ReemplazarClientes(ctx, tx, in.SujetoID, clientes)
		})
	})
	if err != nil {
		return fmt.Errorf("fijar clientes: %w", err)
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
		return s.cambiarAlcance(ctx, tx, func() error {
			return s.alcance.ReemplazarSubordinados(ctx, tx, in.SujetoID, ids)
		})
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
// no documenta un `tipo` de traza propio para el retiro. Publica
// `alcance.cambiado` vacío para el sujeto y recalculado para los superiores
// que tenía, si cambian.
func (s *servicio) retirarAlcance(ctx context.Context, tx pgx.Tx, sujetoID string) (map[string]any, error) {
	var r AlcanceRetirado
	err := s.cambiarAlcance(ctx, tx, func() error {
		var e error
		r, e = s.alcance.Retirar(ctx, tx, sujetoID)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("retirar alcance: %w", err)
	}
	if r.vacio() {
		return nil, nil
	}
	retirado := map[string]any{"clientes_ids": r.ClientesIDs, "unidades_ids": r.UnidadesIDs, "subordinados_ids": r.SubordinadosIDs}
	if r.SuperiorID != "" {
		retirado["superior_id"] = r.SuperiorID
	}
	return map[string]any{"alcance_retirado": retirado}, nil
}

// cambiarAlcance corre `mutar` en `tx` con las escrituras de alcance
// serializadas, compara los clientes efectivos antes y después, y por cada
// sujeto que cambió guarda el conjunto nuevo y publica `alcance.cambiado` en
// el outbox de la misma tx (DEC-149 #20). El candado evita que dos cambios
// concurrentes bajo el mismo superior le publiquen un conjunto rancio.
func (s *servicio) cambiarAlcance(ctx context.Context, tx pgx.Tx, mutar func() error) error {
	cambiadoEn, err := s.alcance.BloquearEnTx(ctx, tx)
	if err != nil {
		return err
	}
	antes, err := s.alcance.GrafoEnTx(ctx, tx)
	if err != nil {
		return err
	}
	if err := mutar(); err != nil {
		return err
	}
	despues, err := s.alcance.GrafoEnTx(ctx, tx)
	if err != nil {
		return err
	}
	cambios := cambiosDeAlcance(antes, despues)
	if len(cambios) == 0 {
		return nil
	}
	for i := range cambios {
		cambios[i].CambiadoEn = cambiadoEn
	}
	if err := s.alcance.GuardarEfectivos(ctx, tx, cambios); err != nil {
		return err
	}
	for _, c := range cambios {
		if err := s.publicarAlcance(ctx, tx, c); err != nil {
			return err
		}
	}
	return nil
}

// ListarAlcances es listarAlcances: sin cursor, el snapshot de los sujetos con
// al menos un cliente efectivo; con cursor, los que cambiaron después,
// incluidos los que quedaron vacíos para que el consumidor los quite. Siempre
// devuelve el cursor para seguir (DEC-149 #20, patrón de ADR 26). La
// autorización es la de listarSesionesRevocadas: token válido, sin rol.
func (s *servicio) ListarAlcances(ctx context.Context, cursor string) (PaginaAlcances, error) {
	var desde *time.Time
	if cursor != "" {
		t, err := decodificarCursorTiempo(cursor)
		if err != nil {
			return PaginaAlcances{}, platformerrors.NewValidacion("cursor de alcances inválido")
		}
		desde = &t
	}
	var items []AlcanceEfectivo
	var ahora time.Time
	err := s.enTx(ctx, func(tx pgx.Tx) error {
		var e error
		items, ahora, e = s.alcance.AlcancesEnTx(ctx, tx, desde)
		return e
	})
	if err != nil {
		return PaginaAlcances{}, fmt.Errorf("listar alcances: %w", err)
	}
	if items == nil {
		items = []AlcanceEfectivo{}
	}
	sig := codificarCursorTiempo(ahora)
	return PaginaAlcances{Items: items, CursorSiguiente: &sig}, nil
}

// codificarCursorTiempo y decodificarCursorTiempo envuelven un instante en un
// valor opaco base64, con la misma forma que el cursor de revocaciones
// (internal/sesion/revocationlog.go).
func codificarCursorTiempo(t time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano)))
}

func decodificarCursorTiempo(cursor string) (time.Time, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, string(raw))
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
