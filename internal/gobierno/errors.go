package gobierno

import (
	stderrors "errors"
)

// Errores centinela del dominio de gobierno. Reusan, donde aplica, los del
// paquete autorizacion (ErrSujetoDuplicado, ErrAsignacionDuplicada,
// ErrRolFueraDeCatalogo) y el ErrSujetoUnresolved de internal/sesion.
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

	// ErrSujetoDesactivado: se intentó asignar o reemplazar el rol de un sujeto
	// desactivado. No se asigna rol a un sujeto inactivo (C2/G-5).
	ErrSujetoDesactivado = stderrors.New("el sujeto está desactivado")

	// ErrUltimoAdministrador: un administrador intentó quitarse el rol o
	// desactivarse siendo el último administrador activo. Se rechaza para no
	// dejar el sistema sin administración (C7/G-7).
	ErrUltimoAdministrador = stderrors.New("no se puede dejar el sistema sin administrador activo")

	// ErrAutoescalamiento: un sujeto que no es administrador intentó modificar
	// su propio rol. Se rechaza antes de tocar el IdP (Requisito 9.2,
	// E4-HU-018 Escenario 9).
	ErrAutoescalamiento = stderrors.New("un sujeto no puede modificar su propio rol")

	// ErrNoAutorizado: el invocante no tiene el rol administrador para ejecutar
	// o consultar una operación de gobierno (Requisito 9.1, 10.3).
	ErrNoAutorizado = stderrors.New("operación de gobierno restringida a administrador")

	// ErrIdpSyncPending: el cambio se comprometió en local (estado + traza +
	// outbox), pero la sincronización con el IdP falló tras agotar los
	// reintentos. El cambio local NO se revierte (C1, DEC-085): local manda.
	ErrIdpSyncPending = stderrors.New("sincronización con el IdP pendiente")
)
