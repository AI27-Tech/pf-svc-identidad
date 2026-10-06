// Package claims compone los claims de dominio de AI27 como FUNCIÓN PURA del
// modelo vigente: (tenant, rol) → (tenant, rol, Audiencia). Corre en la emisión
// Y en el refresco (ADR 16 cerrado por DEC-031): un refresco recompone los
// claims desde el modelo vigente, no sólo renueva el token. Es requisito
// verificable de P11 (compuerta G4), no opción.
//
// El ALCANCE (unidad/cliente) NO entra en el resultado (DEC-053): no viaja en
// el token; se resuelve en servidor y se propaga por evento. Un cambio de
// alcance no espera al refresco.
//
// Nota de empaquetado: en producción esta composición es un ARTEFACTO que corre
// en el IdP (mapper o extensión, ADR 32 §4), no en nuestro proceso, y se
// versiona con este módulo (identidad.md (k)). Aquí vive como función pura para
// poder probarla y versionarla; la envoltura que la expone al IdP elegido queda
// EN ESPERA de ADR 32.
package claims

import (
	"github.com/AI27-Tech/pf-pkg-platform-go/auth"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// ModeloVigente es la entrada de la composición: lo que el modelo de
// autorización afirma HOY sobre el sujeto. No incluye alcance (DEC-053).
type ModeloVigente struct {
	// SujetoID es la identidad del sujeto (irá al claim `sub`).
	SujetoID string
	// TenantID es el tenant del sujeto. Atributo inmutable, no editable por el
	// usuario ni por el cliente OIDC (P10): aquí se asume ya validado.
	TenantID string
	// Rol es el único rol del sujeto (DEC-053: el token lleva un solo rol).
	Rol autorizacion.Rol
}

// Compuestos es el resultado de la composición: EXACTAMENTE identidad, tenant,
// rol y Audiencia. Sin alcance (DEC-053). Es lo que la composición inyecta en
// el token, tanto en emisión como en refresco.
type Compuestos struct {
	SujetoID  string
	TenantID  string
	Rol       autorizacion.Rol
	Audiencia auth.Audiencia
}

// Componer es la función pura de composición de claims. Deriva la Audiencia del
// rol (DEC-054), nunca la acepta como parámetro (P10). Idéntica en emisión y
// refresco (DEC-031): la misma entrada produce la misma salida.
func Componer(m ModeloVigente) Compuestos {
	return Compuestos{
		SujetoID:  m.SujetoID,
		TenantID:  m.TenantID,
		Rol:       m.Rol,
		Audiencia: autorizacion.AudienciaDeRol(m.Rol),
	}
}
