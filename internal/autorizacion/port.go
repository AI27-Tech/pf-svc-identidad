// Package autorizacion es dueño del MODELO DE AUTORIZACIÓN de AI27: la
// jerarquía de tenant, los ocho roles internos (DEC-045, identificadores de
// DEC-053) y las dos dimensiones de alcance (DEC-045 #8). El alcance efectivo
// se resuelve EN SERVIDOR (DEC-053), nunca en el token: el JWT lleva sólo
// identidad, tenant, un rol y Audiencia.
//
// Este paquete NO decide por petición (identidad.md (a)): expresa el modelo y
// la derivación de Audiencia. Las decisiones de autorización por petición son
// de cada módulo consumidor, sobre su propia proyección de alcance alimentada
// por los eventos de `identidad`.
//
// SCAFFOLD: aquí va la FORMA del modelo. La lógica real (resolución de alcance,
// jerarquía de subordinados) no se implementa todavía, y la mecánica del
// vínculo Monitorista↔unidades está SIN DEFINIR (DEC-045, Notas) — ver los
// TODO(DEC-045) más abajo: no inventar tabla ni columna para ella.
package autorizacion

import (
	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/roles"
	"github.com/AI27-Tech/pf-pkg-platform-go/auth"
)

// Rol reexporta el enum de rol del contrato (api-client-go/roles). El enum es un
// dato de dominio que viaja en el TOKEN, por eso vive en el contrato y no se
// redefine aquí (regla 6 de 00-indice.md). Son los ocho roles de DEC-045 con
// los identificadores de DEC-053.
type Rol = roles.Rol

// Los ocho roles, reexportados para uso interno sin reimportar el paquete del
// contrato en cada archivo. Fuente única: api-client-go/roles.
const (
	RolCliente       = roles.RolCliente
	RolJefeOperativo = roles.RolJefeOperativo
	RolMonitorista   = roles.RolMonitorista
	RolSupervisor    = roles.RolSupervisor
	RolCoordinador   = roles.RolCoordinador
	RolEjecutivo     = roles.RolEjecutivo
	RolReaccion      = roles.RolReaccion
	RolAdministrador = roles.RolAdministrador
)

// DimensionAlcance distingue las DOS dimensiones de alcance de DEC-045 #8. No
// es una sola: el modelo debe poder expresar ambas en la asignación de rol.
type DimensionAlcance string

const (
	// AlcanceUnidad acota el MONITOREO por `unidad` (Monitorista, Supervisor,
	// Coordinador): las unidades que el sujeto tiene asignadas, no clientes.
	AlcanceUnidad DimensionAlcance = "unidad"
	// AlcanceCliente acota la CONVERSACIÓN por `cliente` (Ejecutivo).
	AlcanceCliente DimensionAlcance = "cliente"
	// AlcanceGlobal es el alcance interno global del administrador (DEC-045 #6).
	AlcanceGlobal DimensionAlcance = "global"
	// AlcanceTenant es el alcance propio del cliente, acotado a su tenant.
	AlcanceTenant DimensionAlcance = "tenant"
)

// DimensionDeRol devuelve la dimensión de alcance que gobierna a un rol, según
// DEC-045 #8. Es modelo, no decisión por petición. `reaccion` es un caso
// especial: su visibilidad es tipo monitorista pero sólo del viaje/unidad del
// caso y sólo al recibir la alerta (DEC-045 #7, DEC-028); no es alcance
// permanente, por eso NO se le asigna una dimensión durable aquí.
func DimensionDeRol(r Rol) (DimensionAlcance, bool) {
	switch r {
	case RolCliente:
		return AlcanceTenant, true
	case RolMonitorista, RolSupervisor, RolCoordinador:
		return AlcanceUnidad, true
	case RolEjecutivo:
		return AlcanceCliente, true
	case RolAdministrador:
		return AlcanceGlobal, true
	case RolJefeOperativo:
		// Arma el roster de unidades y acepta/asigna viajes; rol distinto del
		// que monitorea (DEC-045 #2). Su alcance operativo aún no está
		// modelado como dimensión durable.
		return "", false
	case RolReaccion:
		// Visibilidad por caso, no permanente (DEC-045 #7). Ver TODO(DEC-045).
		return "", false
	}
	return "", false
}

// AudienciaDeRol deriva la Audiencia de dominio (DEC-054) a partir del rol:
// `cliente` → Cliente; los otros siete (incluido `reaccion`) → Interno. La
// Audiencia es un claim personalizado DERIVADO, nunca aceptado como parámetro
// (P10). Se delega en el enum del contrato (roles.EsInterno) para no duplicar
// el mapeo.
func AudienciaDeRol(r Rol) auth.Audiencia {
	if r.EsInterno() {
		return auth.AudienciaInterno
	}
	return auth.AudienciaCliente
}

// AsignacionRol es la FORMA de una asignación de rol a un sujeto dentro de un
// tenant, con su dimensión de alcance. Es el molde; su persistencia vive en
// internal/ y en la tabla `asignacion_rol`. El patrón de "asignación con ciclo
// de vida (activa/revocada) y actor" es lo único que se reutiliza del repo de
// referencia (mp-backend-auth), traducido a Go y recortado al dominio de AI27.
type AsignacionRol struct {
	SujetoID  string
	TenantID  string
	Rol       Rol
	Dimension DimensionAlcance

	// TODO(DEC-045): el IDENTIFICADOR del recurso al que se acota el alcance
	// (p. ej. a qué `unidad` o a qué `cliente`) NO se modela todavía. La
	// mecánica del vínculo Monitorista↔`unidades` está SIN DEFINIR (DEC-045,
	// Notas): si se fija al crear la unidad, en la asignación o como roster de
	// "mis unidades", y quién lo hace. No se inventa aquí tabla ni columna para
	// ese vínculo. Lo mismo aplica a la relación Supervisor→Monitoristas y
	// Coordinador→Supervisores, y a la visibilidad por caso de `reaccion`.
}

// ResolverAlcance es el punto donde, en servidor, se resolverá el alcance
// efectivo de un sujeto (DEC-053) para que cada módulo mantenga su proyección.
// SCAFFOLD: la firma marca el lugar; la lógica no existe todavía porque depende
// del vínculo pendiente de DEC-045.
//
// TODO(DEC-045): implementar cuando la mecánica del vínculo Monitorista↔unidades
// (y la jerarquía Supervisor→Monitoristas / Coordinador→Supervisores) esté
// definida. No anidar poderes: el alcance de Coordinador es la UNIÓN de sus
// supervisores, no facultades extra (DEC-045 #4).
type ResolverAlcance interface {
	// UnidadesDe devuelve las unidades visibles para un sujeto con rol de
	// monitoreo. Vacío + false mientras el vínculo de DEC-045 no esté definido.
	UnidadesDe(sujetoID string) (unidades []string, resuelto bool)
	// ClientesDe devuelve los clientes visibles para un Ejecutivo (DEC-045 #5).
	ClientesDe(sujetoID string) (clientes []string, resuelto bool)
}

// AlcancePendiente es el marcador zero value del resolvedor de alcance mientras
// el vínculo Monitorista↔unidades y las relaciones jerárquicas de DEC-045 sigan
// pendientes. No inventa una relación de alcance ni persiste información.
//
// TODO(DEC-045): reemplazar este marcador cuando se defina la mecánica del
// vínculo de alcance y su resolución en servidor.
type AlcancePendiente struct{}

// UnidadesDe devuelve un slice vacío y false porque el vínculo de DEC-045 aún
// no está definido. El sujetoID se conserva sólo para satisfacer el puerto.
// TODO(DEC-045): resolver las unidades visibles cuando se defina el vínculo.
func (AlcancePendiente) UnidadesDe(sujetoID string) ([]string, bool) {
	return []string{}, false
}

// ClientesDe devuelve un slice vacío y false porque la resolución de alcance
// de DEC-045 aún no está definida. El sujetoID se conserva sólo para
// satisfacer el puerto.
// TODO(DEC-045): resolver los clientes visibles cuando se defina el vínculo.
func (AlcancePendiente) ClientesDe(sujetoID string) ([]string, bool) {
	return []string{}, false
}

var _ ResolverAlcance = AlcancePendiente{}
