// servicio.go implementa gobierno.Gobierno y gobierno.Consulta (E4-T6). Sigue
// el patrón transaccional de internal/sesion/servicio.go: una sola transacción
// de Postgres compromete estado + traza + outbox (atomicidad, Requisito 6), y
// la sincronización con el IdP ocurre DESPUÉS del commit, de forma reintentable
// (local manda, DEC-085 / C1). Un fallo del IdP no revierte el commit local.
package gobierno

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/roles"
	"github.com/AI27-Tech/pf-pkg-platform-go/outbox"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// BeginTx abre una transacción de Postgres. Interfaz en el consumidor, igual
// que la de internal/sesion; la cumple *pgxpool.Pool. El caso de uso controla
// el commit (patrón Unit of Work).
type BeginTx interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// SujetoRepoTx es la parte de autorizacion.SujetoRepo que el gobierno usa,
// incluyendo las operaciones transaccionales que E4-T6 añadió.
type SujetoRepoTx interface {
	Crear(ctx context.Context, tx pgx.Tx, s autorizacion.Sujeto) (autorizacion.Sujeto, error)
	PorID(ctx context.Context, id string) (autorizacion.Sujeto, bool, error)
	PorIdpSubject(ctx context.Context, idpSubject string) (autorizacion.Sujeto, bool, error)
	FijarActivo(ctx context.Context, tx pgx.Tx, id string, activo bool) error
}

// AsignacionRepoTx es la parte de autorizacion.AsignacionRolRepo que el
// gobierno usa.
type AsignacionRepoTx interface {
	ActivasDe(ctx context.Context, sujetoID string) ([]autorizacion.AsignacionRol, error)
	AsignarInicial(ctx context.Context, tx pgx.Tx, a autorizacion.AsignacionRol) error
	Reemplazar(ctx context.Context, tx pgx.Tx, in autorizacion.Reemplazo) error
	// ContarActivosConRol cuenta los sujetos con una asignación activa de un
	// rol dado. Lo usa la guarda del "último administrador" (G-7).
	ContarActivosConRol(ctx context.Context, rol autorizacion.Rol) (int, error)
}

// TrazaRepoTx escribe la traza dentro de la transacción del cambio.
type TrazaRepoTx interface {
	Registrar(ctx context.Context, tx pgx.Tx, c CambioAdministrativo) error
	Historial(ctx context.Context, sujetoID, cursor string) (PaginaHistorial, error)
}

// RevocadorSesion revoca la sesión viva de un sujeto al desactivarlo (G-6). El
// gobierno delega en el servicio de sesión; el corte local de la sesión y su
// evento ocurren en ese servicio (reusa su Paso 1 de dos pasos).
//
// TODO(K-6): el diseño prefería que la revocación de la sesión comparta la
// MISMA transacción que la desactivación del sujeto. Eso exige exponer el Paso
// 1 del store de sesión, hoy privado. En esta versión se delega al servicio de
// sesión (dos commits: sujeto, luego sesión); refinar a una sola tx cuando el
// store de sesión exponga su Paso 1.
type RevocadorSesion interface {
	// SesionVivaDe devuelve el sesion_id de la sesión viva del sujeto, si la hay.
	SesionVivaDe(ctx context.Context, sujetoID string) (string, bool, error)
	// Revocar revoca una sesión por su id (dos pasos de ADR 26).
	Revocar(ctx context.Context, sesionID string) error
}

// nombres canónicos de los Hechos_Gobierno (G-2, C5). El tipo de la traza usa
// el mismo nombre (K-1).
const (
	hechoSujetoCreado      = "sujeto.creado"
	hechoSujetoDesactivado = "sujeto.desactivado"
	hechoSujetoReactivado  = "sujeto.reactivado"
	hechoRolAsignado       = "rol.asignado"
	hechoRolRevocado       = "rol.revocado"
)

// maxIntentosIdp es el máximo de intentos de la sincronización con el IdP tras
// el commit local (C1; patrón de DEC-088 A-7).
const maxIntentosIdp = 3

// servicio implementa Gobierno y Consulta. No se exporta: la raíz del módulo lo
// obtiene por NewServicio.
type servicio struct {
	tx         BeginTx
	publisher  *outbox.Publisher
	idpAdmin   idp.IdpAdmin
	sujetos    SujetoRepoTx
	asignacion AsignacionRepoTx
	traza      TrazaRepoTx
	revocador  RevocadorSesion

	// reintentoEspera permite inyectar la espera entre intentos en pruebas; por
	// omisión es una espera creciente real.
	reintentoEspera func(intento int)
}

var (
	_ Gobierno = (*servicio)(nil)
	_ Consulta = (*servicio)(nil)
)

// NewServicio construye el servicio de gobierno con sus colaboradores. El
// idpAdmin puede ser el doble en memoria (pruebas y desarrollo) o el adaptador
// real de Keycloak (tarea 11); ambos cumplen el mismo puerto.
func NewServicio(
	tx BeginTx,
	pub *outbox.Publisher,
	idpAdmin idp.IdpAdmin,
	sujetos SujetoRepoTx,
	asignacion AsignacionRepoTx,
	traza TrazaRepoTx,
	revocador RevocadorSesion,
) *servicio {
	return &servicio{
		tx:              tx,
		publisher:       pub,
		idpAdmin:        idpAdmin,
		sujetos:         sujetos,
		asignacion:      asignacion,
		traza:           traza,
		revocador:       revocador,
		reintentoEspera: esperaCreciente,
	}
}

// AltaSujeto: Otorgamiento (IdP primero, G-3). Crea el usuario en el IdP, y en
// una sola transacción local crea el sujeto, su asignación inicial, dos filas
// de traza (sujeto.creado + rol.asignado) y dos hechos en outbox (G-8).
func (s *servicio) AltaSujeto(ctx context.Context, in AltaSujeto) (autorizacion.Sujeto, error) {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return autorizacion.Sujeto{}, fmt.Errorf("alta: %w", err)
	}
	if !roles.EsRol(string(in.Rol)) {
		return autorizacion.Sujeto{}, fmt.Errorf("alta: %w", autorizacion.ErrRolFueraDeCatalogo)
	}
	// Paso 1 (Otorgamiento): crear el usuario en el IdP y obtener su sub opaco.
	sub, err := s.idpAdmin.CrearUsuario(ctx, idp.NuevoUsuario{
		Usuario:  in.IdentificadorUsuario,
		Nombre:   in.Nombre,
		TenantID: in.TenantID,
		Rol:      string(in.Rol),
	})
	if err != nil {
		return autorizacion.Sujeto{}, fmt.Errorf("alta: crear usuario en IdP: %w", err)
	}

	// Paso 2: estado local + traza + outbox en una sola transacción.
	var creado autorizacion.Sujeto
	err = s.enTx(ctx, func(tx pgx.Tx) error {
		var e error
		creado, e = s.sujetos.Crear(ctx, tx, autorizacion.Sujeto{
			ID:         "", // lo genera la BD (gen_random_uuid)
			IdpSubject: sub,
			TenantID:   in.TenantID,
			Activo:     true,
		})
		if e != nil {
			return fmt.Errorf("crear sujeto: %w", e)
		}
		if e := s.asignacion.AsignarInicial(ctx, tx, autorizacion.AsignacionRol{
			SujetoID:    creado.ID,
			Rol:         in.Rol,
			TenantID:    in.TenantID,
			AsignadoPor: in.ActorID,
		}); e != nil {
			return traducirAsignacion(e)
		}
		if e := s.registrar(ctx, tx, hechoSujetoCreado, creado.ID, in.TenantID, in.ActorID); e != nil {
			return e
		}
		if e := s.registrar(ctx, tx, hechoRolAsignado, creado.ID, in.TenantID, in.ActorID); e != nil {
			return e
		}
		if e := s.publicar(ctx, tx, hechoSujetoCreado, creado.ID); e != nil {
			return e
		}
		return s.publicar(ctx, tx, hechoRolAsignado, creado.ID)
	})
	if err != nil {
		return autorizacion.Sujeto{}, fmt.Errorf("alta: %w", err)
	}
	return creado, nil
}

// DesactivarSujeto: Retiro de acceso (local primero, G-3). Marca inactivo,
// escribe traza y outbox en una tx; luego revoca la sesión viva (≤30 s, G-6) y
// deshabilita en el IdP, reintentable.
func (s *servicio) DesactivarSujeto(ctx context.Context, in CambioEstado) error {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return fmt.Errorf("desactivar: %w", err)
	}
	// Un administrador puede desactivarse, salvo que sea el último activo (G-7).
	if err := guardarUltimoAdministrador(ctx, s.asignacion, in.SujetoID); err != nil {
		return fmt.Errorf("desactivar: %w", err)
	}
	suj, ok, err := s.sujetos.PorID(ctx, in.SujetoID)
	if err != nil {
		return fmt.Errorf("desactivar: %w", err)
	}
	if !ok {
		return fmt.Errorf("desactivar: %w", autorizacion.ErrSujetoNoEncontrado)
	}

	err = s.enTx(ctx, func(tx pgx.Tx) error {
		if e := s.sujetos.FijarActivo(ctx, tx, in.SujetoID, false); e != nil {
			return e
		}
		if e := s.registrar(ctx, tx, hechoSujetoDesactivado, in.SujetoID, suj.TenantID, in.ActorID); e != nil {
			return e
		}
		return s.publicar(ctx, tx, hechoSujetoDesactivado, in.SujetoID)
	})
	if err != nil {
		return fmt.Errorf("desactivar: %w", err)
	}

	// Cortar el acceso vivo (G-6). Reusa el servicio de sesión (dos pasos).
	if s.revocador != nil {
		if sesionID, viva, e := s.revocador.SesionVivaDe(ctx, in.SujetoID); e == nil && viva {
			_ = s.revocador.Revocar(ctx, sesionID) // reintento/idempotencia los maneja sesión
		}
	}

	// Deshabilitar en el IdP, reintentable; local ya mandó.
	return s.sincronizarIdp(ctx, func() error {
		return s.idpAdmin.DeshabilitarUsuario(ctx, suj.IdpSubject)
	})
}

// ReactivarSujeto: Otorgamiento (IdP primero). Habilita en el IdP, luego marca
// activo, traza y outbox (sujeto.reactivado, C5).
func (s *servicio) ReactivarSujeto(ctx context.Context, in CambioEstado) error {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return fmt.Errorf("reactivar: %w", err)
	}
	suj, ok, err := s.sujetos.PorID(ctx, in.SujetoID)
	if err != nil {
		return fmt.Errorf("reactivar: %w", err)
	}
	if !ok {
		return fmt.Errorf("reactivar: %w", autorizacion.ErrSujetoNoEncontrado)
	}
	if err := s.idpAdmin.HabilitarUsuario(ctx, suj.IdpSubject); err != nil {
		return fmt.Errorf("reactivar: habilitar en IdP: %w", err)
	}
	err = s.enTx(ctx, func(tx pgx.Tx) error {
		if e := s.sujetos.FijarActivo(ctx, tx, in.SujetoID, true); e != nil {
			return e
		}
		if e := s.registrar(ctx, tx, hechoSujetoReactivado, in.SujetoID, suj.TenantID, in.ActorID); e != nil {
			return e
		}
		return s.publicar(ctx, tx, hechoSujetoReactivado, in.SujetoID)
	})
	if err != nil {
		return fmt.Errorf("reactivar: %w", err)
	}
	return nil
}

// ReemplazarRol: todo local en una tx (C1), luego sincroniza el IdP moviendo de
// grupo (quita el anterior antes de agregar el nuevo, 7.6).
func (s *servicio) ReemplazarRol(ctx context.Context, in ReemplazoRol) error {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return fmt.Errorf("reemplazar: %w", err)
	}
	if err := guardarAutoescalamientoRol(in.ActorID, in.SujetoID, in.ActorEsAdmin); err != nil {
		return fmt.Errorf("reemplazar: %w", err)
	}
	// Si el objetivo es administrador y cambia su rol a otro, no puede ser el
	// último administrador activo (G-7).
	if in.RolNuevo != autorizacion.RolAdministrador {
		if err := guardarUltimoAdministrador(ctx, s.asignacion, in.SujetoID); err != nil {
			return fmt.Errorf("reemplazar: %w", err)
		}
	}
	if !roles.EsRol(string(in.RolNuevo)) {
		return fmt.Errorf("reemplazar: %w", autorizacion.ErrRolFueraDeCatalogo)
	}
	suj, ok, err := s.sujetos.PorID(ctx, in.SujetoID)
	if err != nil {
		return fmt.Errorf("reemplazar: %w", err)
	}
	if !ok {
		return fmt.Errorf("reemplazar: %w", autorizacion.ErrSujetoNoEncontrado)
	}
	if !suj.Activo {
		return fmt.Errorf("reemplazar: %w", ErrSujetoDesactivado)
	}

	activas, err := s.asignacion.ActivasDe(ctx, in.SujetoID)
	if err != nil {
		return fmt.Errorf("reemplazar: %w", err)
	}
	if len(activas) == 0 {
		return fmt.Errorf("reemplazar: %w", ErrSujetoSinRol)
	}
	rolAnterior := activas[0].Rol
	if rolAnterior == in.RolNuevo {
		return fmt.Errorf("reemplazar: %w", autorizacion.ErrAsignacionDuplicada)
	}

	err = s.enTx(ctx, func(tx pgx.Tx) error {
		if e := s.asignacion.Reemplazar(ctx, tx, autorizacion.Reemplazo{
			SujetoID: in.SujetoID,
			RolNuevo: in.RolNuevo,
			ActorID:  in.ActorID,
		}); e != nil {
			return e
		}
		if e := s.registrar(ctx, tx, hechoRolRevocado, in.SujetoID, suj.TenantID, in.ActorID); e != nil {
			return e
		}
		if e := s.registrar(ctx, tx, hechoRolAsignado, in.SujetoID, suj.TenantID, in.ActorID); e != nil {
			return e
		}
		if e := s.publicar(ctx, tx, hechoRolRevocado, in.SujetoID); e != nil {
			return e
		}
		return s.publicar(ctx, tx, hechoRolAsignado, in.SujetoID)
	})
	if err != nil {
		return fmt.Errorf("reemplazar: %w", err)
	}

	// Sincronizar el IdP: quitar el grupo anterior ANTES de agregar el nuevo (7.6).
	return s.sincronizarIdp(ctx, func() error {
		return s.idpAdmin.MoverDeGrupo(ctx, suj.IdpSubject, string(rolAnterior), string(in.RolNuevo))
	})
}

// AsignarRolInicial asigna un rol a un sujeto sin rol activo.
func (s *servicio) AsignarRolInicial(ctx context.Context, in AsignacionInicial) error {
	if err := guardarAdministrador(in.ActorEsAdmin); err != nil {
		return fmt.Errorf("asignar inicial: %w", err)
	}
	if err := guardarAutoescalamientoRol(in.ActorID, in.SujetoID, in.ActorEsAdmin); err != nil {
		return fmt.Errorf("asignar inicial: %w", err)
	}
	if !roles.EsRol(string(in.Rol)) {
		return fmt.Errorf("asignar inicial: %w", autorizacion.ErrRolFueraDeCatalogo)
	}
	suj, ok, err := s.sujetos.PorID(ctx, in.SujetoID)
	if err != nil {
		return fmt.Errorf("asignar inicial: %w", err)
	}
	if !ok {
		return fmt.Errorf("asignar inicial: %w", autorizacion.ErrSujetoNoEncontrado)
	}
	if !suj.Activo {
		return fmt.Errorf("asignar inicial: %w", ErrSujetoDesactivado)
	}

	err = s.enTx(ctx, func(tx pgx.Tx) error {
		if e := s.asignacion.AsignarInicial(ctx, tx, autorizacion.AsignacionRol{
			SujetoID:    in.SujetoID,
			Rol:         in.Rol,
			TenantID:    suj.TenantID,
			AsignadoPor: in.ActorID,
		}); e != nil {
			return traducirAsignacion(e)
		}
		if e := s.registrar(ctx, tx, hechoRolAsignado, in.SujetoID, suj.TenantID, in.ActorID); e != nil {
			return e
		}
		return s.publicar(ctx, tx, hechoRolAsignado, in.SujetoID)
	})
	if err != nil {
		return fmt.Errorf("asignar inicial: %w", err)
	}
	return nil
}

// SujetoPorID devuelve el detalle de un sujeto y su rol activo.
func (s *servicio) SujetoPorID(ctx context.Context, sujetoID string) (SujetoDetalle, error) {
	suj, ok, err := s.sujetos.PorID(ctx, sujetoID)
	if err != nil {
		return SujetoDetalle{}, fmt.Errorf("consultar sujeto: %w", err)
	}
	if !ok {
		return SujetoDetalle{}, autorizacion.ErrSujetoNoEncontrado
	}
	det := SujetoDetalle{Sujeto: suj}
	activas, err := s.asignacion.ActivasDe(ctx, sujetoID)
	if err != nil {
		return SujetoDetalle{}, fmt.Errorf("consultar sujeto: %w", err)
	}
	if len(activas) > 0 {
		det.RolActivo = activas[0].Rol
		det.TieneRol = true
	}
	return det, nil
}

// HistorialDe devuelve los cambios administrativos de un sujeto.
func (s *servicio) HistorialDe(ctx context.Context, sujetoID, cursor string) (PaginaHistorial, error) {
	return s.traza.Historial(ctx, sujetoID, cursor)
}

// enTx abre una transacción, ejecuta fn y hace commit; si fn falla, revierte.
// Mismo patrón que revocarLocalConEvento de sesión.
func (s *servicio) enTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op tras commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// registrar escribe una fila de traza con el nombre canónico del hecho (K-1).
func (s *servicio) registrar(ctx context.Context, tx pgx.Tx, tipo, sujetoID, tenantID, actorID string) error {
	return s.traza.Registrar(ctx, tx, CambioAdministrativo{
		Tipo:     tipo,
		SujetoID: sujetoID,
		TenantID: tenantID,
		ActorID:  actorID,
	})
}

// publicar escribe un hecho en el outbox dentro de la tx. El payload mínimo
// (solo el agregado) es provisional: la forma serializada definitiva espera el
// contrato (G-2). Hasta entonces no se serializa un payload de wire.
func (s *servicio) publicar(ctx context.Context, tx pgx.Tx, tipo, agregadoID string) error {
	ev := outbox.Event{
		Tipo:         tipo,
		Version:      1,
		AgregadoID:   agregadoID,
		Sensibilidad: outbox.SensibilidadAmplia,
		Payload:      []byte(`{}`), // provisional: la forma del contrato espera G-2
	}
	return s.publisher.Publish(ctx, tx, ev)
}

// sincronizarIdp ejecuta la operación del IdP con reintento de espera creciente
// (máx 3). Si se agotan, devuelve ErrIdpSyncPending SIN revertir el commit
// local (C1): local manda.
func (s *servicio) sincronizarIdp(ctx context.Context, op func() error) error {
	var ultimo error
	for intento := 0; intento < maxIntentosIdp; intento++ {
		if ultimo = op(); ultimo == nil {
			return nil
		}
		if intento < maxIntentosIdp-1 && s.reintentoEspera != nil {
			s.reintentoEspera(intento)
		}
	}
	return fmt.Errorf("%w: %w", ErrIdpSyncPending, ultimo)
}

// esperaCreciente duerme 1s, 2s, 4s… según el intento (patrón DEC-088 A-7).
func esperaCreciente(intento int) {
	time.Sleep(time.Duration(1<<intento) * time.Second)
}

// traducirAsignacion convierte el error de "ya existe asignación activa" del
// repo (ErrAsignacionActivaExiste) al centinela de dominio ErrSujetoYaTieneRol.
func traducirAsignacion(err error) error {
	if errors.Is(err, autorizacion.ErrAsignacionActivaExiste) {
		return ErrSujetoYaTieneRol
	}
	return err
}
