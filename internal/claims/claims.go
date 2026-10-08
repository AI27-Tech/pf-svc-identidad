// Package claims compone los claims de dominio de AI27 como FUNCIÓN PURA del
// Package claims compone los claims de dominio de AI27 como función pura del
// modelo vigente: (tenant, rol) → (tenant, rol, Audiencia). Corre en la emisión
// y en el refresco (ADR 16 cerrado por DEC-031): un refresco recompone los
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

// ModeloVigente conserva la API de claims como alias del modelo canónico de
// autorizacion. El alias evita duplicar el tipo y mantiene a los llamadores
// existentes mientras autorizacion puede usarlo sin importar este paquete.
type ModeloVigente = autorizacion.ModeloVigente

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
