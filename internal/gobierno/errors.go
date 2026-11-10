package gobierno

import (
	stderrors "errors"

	platformerrors "github.com/AI27-Tech/pf-pkg-platform-go/errors"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// Errores centinela del dominio de gobierno. Reusan, donde aplica, los del
// paquete autorizacion (ErrSujetoDuplicado, ErrAsignacionDuplicada,
// ErrRolFueraDeCatalogo) y el ErrSujetoUnresolved de internal/sesion. Classify
// los traduce a la taxonomía de la plataforma con su `codigo` de máquina.
var (
	// ErrSujetoYaTieneRol: se intentó una asignación inicial sobre un sujeto que
	// ya tiene un rol activo. Para cambiar el rol se usa ReemplazarRol (C2/G-5).
	// El repositorio traduce a este centinela la violación del índice
	// asignacion_rol_un_activo_idx (23505) en una asignación inicial.
	ErrSujetoYaTieneRol = stderrors.New("el sujeto ya tiene un rol activo")

	// ErrSujetoSinRol: se intentó un reemplazo sobre un sujeto que no tiene rol
	// activo. Un reemplazo exige un rol anterior que revocar; si no lo hay, se
	// usa AsignarRolInicial.
	ErrSujetoSinRol = stderrors.New("el sujeto no tiene un rol activo que reemplazar")

	// ErrRolIgual: el rol pedido es el mismo que el activo del sujeto.
	ErrRolIgual = stderrors.New("el rol nuevo es igual al rol activo")

	// ErrSujetoDesactivado: se intentó asignar o reemplazar el rol de un sujeto
	// desactivado. No se asigna rol a un sujeto inactivo (C2/G-5).
	ErrSujetoDesactivado = stderrors.New("el sujeto está desactivado")

	// ErrUltimoAdministrador: un administrador intentó quitarse el rol o
	// desactivarse siendo el último administrador activo. Se rechaza para no
	// dejar el sistema sin administración (G-7; no existe una cuestión "C7",
	// las decisiones tomadas van de C1 a C6).
	ErrUltimoAdministrador = stderrors.New("no se puede dejar el sistema sin administrador activo")

	// ErrCruceAudiencia: el rol pedido es del otro lado de la cuenta (cliente o
	// interno). Ninguna cuenta cruza entre `cliente` y un rol interno, en
	// ninguna dirección; un rol tampoco puede quedar en un tenant del otro tipo
	// (DEC-141).
	ErrCruceAudiencia = stderrors.New("el rol no corresponde al lado de la cuenta (cliente o interno)")

	// ErrAutoescalamiento: un sujeto que no es administrador intentó modificar
	// su propio rol. Se rechaza antes de tocar el IdP (Requisito 9.2,
	// E4-HU-018 Escenario 9).
	ErrAutoescalamiento = stderrors.New("un sujeto no puede modificar su propio rol")

	// ErrNoAutorizado: el invocante no tiene el rol administrador para ejecutar
	// o consultar una operación de gobierno (Requisito 9.1, 10.3).
	ErrNoAutorizado = stderrors.New("operación de gobierno restringida a administrador")

	// ErrRevocadorNoCableado: el módulo no cableó el revocador de sesiones, así
	// que no puede cortar la sesión viva. Es un fallo de composición: se
	// responde 503 en vez de desactivar sin cortar (G-6).
	ErrRevocadorNoCableado = stderrors.New("gobierno: el revocador de sesiones no está cableado")

	// ErrTenantInterno: se intentó crear con el id del tenant interno de AI27 o
	// desactivarlo. El tenant interno no se pisa ni se desactiva (DEC-144).
	ErrTenantInterno = stderrors.New("el tenant interno no se crea ni se desactiva por esta vía")

	// ErrTenantNoEncontrado: el tenant pedido no existe.
	ErrTenantNoEncontrado = stderrors.New("tenant no encontrado")

	// ErrIdpSyncPending: el cambio se comprometió en local (estado + traza +
	// outbox), pero la sincronización con el IdP falló tras agotar los
	// reintentos. El cambio local NO se revierte (C1, DEC-085): local manda.
	ErrIdpSyncPending = stderrors.New("sincronización con el IdP pendiente")
)

// clase asocia un centinela con su categoría de plataforma y su `codigo` de
// máquina. Un `codigo` vacío deja el genérico de la categoría (CodigoDe).
type clase struct {
	centinela error
	categoria platformerrors.Category
	codigo    string
}

// clases es la única tabla centinela → (status, codigo) del gobierno. La usan
// el handler HTTP y la fachada en proceso, así que los dos responden igual.
var clases = []clase{
	{ErrNoAutorizado, platformerrors.CategoryProhibido, ""},
	{ErrAutoescalamiento, platformerrors.CategoryProhibido, "autoescalamiento"},
	{ErrCruceAudiencia, platformerrors.CategoryConflicto, "rol_cruza_audiencia"},
	{ErrUltimoAdministrador, platformerrors.CategoryConflicto, "ultimo_administrador"},
	{ErrSujetoDesactivado, platformerrors.CategoryConflicto, "sujeto_desactivado"},
	{ErrRolIgual, platformerrors.CategoryConflicto, "rol_igual"},
	{ErrSujetoYaTieneRol, platformerrors.CategoryConflicto, ""},
	{autorizacion.ErrAsignacionDuplicada, platformerrors.CategoryConflicto, ""},
	{autorizacion.ErrSujetoNoEncontrado, platformerrors.CategoryNoEncontrado, ""},
	{autorizacion.ErrRolFueraDeCatalogo, platformerrors.CategoryValidacion, ""},
	{ErrRevocadorNoCableado, platformerrors.CategoryDegradado, ""},
	{ErrTenantInterno, platformerrors.CategoryConflicto, ""},
	{ErrTenantNoEncontrado, platformerrors.CategoryNoEncontrado, ""},
	// El cambio local sí ocurrió; el 409 avisa que reintentar sincroniza el IdP.
	{ErrIdpSyncPending, platformerrors.CategoryConflicto, "idp_sync_pending"},
}

// Classify traduce un error del gobierno a un *PlatformError con su categoría y
// su `codigo`, conservando la cadena (errors.Is sigue viendo el centinela). Un
// error que ya es de plataforma, o que no es de gobierno, se devuelve igual. El
// mensaje público es el del centinela, no la cadena envuelta.
func Classify(err error) error {
	if err == nil {
		return nil
	}
	for _, c := range clases {
		if stderrors.Is(err, c.centinela) {
			return &platformerrors.PlatformError{
				Category: c.categoria,
				Message:  c.centinela.Error(),
				Err:      err,
				Codigo:   c.codigo,
			}
		}
	}
	return err
}
