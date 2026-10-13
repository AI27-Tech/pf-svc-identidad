package sesion

import (
	stderrors "errors"

	"github.com/AI27-Tech/pf-pkg-platform-go/errors"
)

// Errores centinela del dominio de sesión.
//
// H-4 (design.md): hoy todos los errores de sesión sin categoría salen como 501
// no_implementado. ErrNoEncontrada se envuelve en un PlatformError de categoría
// NoEncontrado para que ObtenerSesion devuelva 404 (Requisito 6.2). Los demás
// siguen sin categoría (Paso 2 fallido, Paso 1 fallido, missing config) hasta
// que A-7 fije el reintento.

var (
	// ErrNoEncontrada se devuelve cuando ObtenerSesion consulta una fila que
	// no existe. Se envuelve en un PlatformError de categoría NoEncontrado
	// en ObtenerSesion (Requisito 6.2).
	ErrNoEncontrada = stderrors.New("sesión no encontrada")

	// ErrIdpRevokePending: el Paso 1 se comprometió, pero el Paso 2 (revocación
	// en el IdP) falló o está pendiente. El Paso 1 no se revierte (Requisito 4.5).
	// Devuelto sin categoría hasta que A-7 fije el manejo de errores del Paso 2.
	ErrIdpRevokePending = stderrors.New("revocación del IdP pendiente")

	// ErrMissingIdpSessionID: la fila de sesión no tiene idp_session_id (no se
	// pudo registrar, o se registró sin EmissionFlow configurado). El Paso 2 no
	// puede ejecutarse; se invoca el handler de fallos (A-7).
	ErrMissingIdpSessionID = stderrors.New("idp_session_id ausente en la fila")

	// ErrLiveSesionExists: el INSERT de una sesión violó el índice
	// una_sesion_viva (23505): ya hay una sesión viva para ese sujeto (A-2,
	// opción conservada por DEC-088). store.insert traduce el 23505 de Postgres
	// a este centinela para que el llamador (el flujo de emisión) lo distinga de
	// otros errores de base de datos.
	ErrLiveSesionExists = stderrors.New("ya existe una sesión viva para el sujeto")

	// ErrSujetoUnresolved: el SujetoResolver no pudo determinar el sujeto_id de
	// AI27 a partir del token del IdP (A-5). Corresponde a un sub que no mapea a
	// ningún sujeto conocido, o a un fallo del resolver. El flujo de emisión no
	// puede registrar la fila sin el sujeto_id.
	ErrSujetoUnresolved = stderrors.New("no se pudo resolver el sujeto desde el token")

	// ErrRefreshUnresolved: el RefreshResolver no pudo determinar el sesion_id
	// desde el token refrescado (A-4). Corresponde a un fallo del resolver o un
	// token inesperado del IdP.
	ErrRefreshUnresolved = stderrors.New("no se pudo resolver sesion_id en refresco")

	// ErrNoImplementado: la operación no está cableada porque depende de un
	// pendiente (A-1, A-4, etc.). EmitirSesion y RefrescarSesion lo devuelven
	// cuando falta su costura correspondiente.
	ErrNoImplementado = stderrors.New("no implementado")
)

// WrapNotFound envuelve ErrNoEncontrada en un PlatformError de categoría
// NoEncontrado, para que el handler HTTP lo traduzca a 404 (Requisito 6.2).
func WrapNotFound(err error) error {
	if err == nil {
		return nil
	}
	if stderrors.Is(err, ErrNoEncontrada) {
		return &errors.PlatformError{
			Category: errors.CategoryNoEncontrado,
			Message:  "sesión no encontrada",
			Err:      err,
		}
	}
	return err
}
