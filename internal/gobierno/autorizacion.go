package gobierno

import (
	"context"
	"fmt"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// guardarAdministrador rechaza la operación si el invocante no es administrador
// (Requisito 9.1): solo el administrador ejecuta cambios de gobierno. Se llama
// ANTES de tocar el IdP o escribir en cualquier almacén.
func guardarAdministrador(esAdmin bool) error {
	if !esAdmin {
		return ErrNoAutorizado
	}
	return nil
}

// guardarAutoescalamientoRol rechaza que un sujeto que no es administrador
// modifique su propio rol (Requisito 9.2, E4-HU-018 Escenario 9). Un
// administrador sí puede, sujeto a la guarda del último administrador.
func guardarAutoescalamientoRol(actorID, objetivoID string, actorEsAdmin bool) error {
	if actorID == objetivoID && !actorEsAdmin {
		return ErrAutoescalamiento
	}
	return nil
}

// guardarUltimoAdministrador impide que un administrador se quite el rol o se
// desactive si es el ÚNICO administrador activo (Requisito 9.3, G-7). Se
// consulta justo antes del cambio; el conteo debe resistir dos solicitudes
// concurrentes (el constraint y la transacción del cambio lo acotan, pero una
// verificación previa evita el caso común).
func guardarUltimoAdministrador(ctx context.Context, repo AsignacionRepoTx, objetivoID string) error {
	// ¿El objetivo es administrador activo?
	activas, err := repo.ActivasDe(ctx, objetivoID)
	if err != nil {
		return fmt.Errorf("verificar último administrador: %w", err)
	}
	esAdmin := false
	for _, a := range activas {
		if a.Rol == autorizacion.RolAdministrador {
			esAdmin = true
			break
		}
	}
	if !esAdmin {
		return nil // el objetivo no es administrador: no aplica la guarda
	}
	n, err := repo.ContarActivosConRol(ctx, autorizacion.RolAdministrador)
	if err != nil {
		return fmt.Errorf("verificar último administrador: %w", err)
	}
	if n <= 1 {
		return ErrUltimoAdministrador
	}
	return nil
}
