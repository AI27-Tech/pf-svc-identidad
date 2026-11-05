// payload.go serializa los Hechos_Gobierno al outbox con la forma del contrato
// (tracer.11, G-2 / Requisito 8.1, 8.2). El contrato declara cada hecho en
// protobuf (pf-pkg-contracts eventos/v1/sujeto.proto: SujetoCreado,
// SujetoDesactivado, SujetoReactivado, RolAsignado, RolRevocado); el payload
// del outbox es el JSON canónico de esos mensajes, con los nombres de campo del
// contrato en snake_case (sujeto_id, tenant_id, rol, <verbo>_en). Se usa
// json.Marshal de un struct con tags, igual que el payload de sesion.revocada
// (internal/sesion/payload.go), para no depender de proto.Marshal en el outbox.
package gobierno

import (
	"encoding/json"
	"fmt"
	"time"
)

// payloadSujetoCreado es el payload de sujeto.creado (SujetoCreado del
// contrato): agregado + tenant + rol inicial + marca de tiempo.
type payloadSujetoCreado struct {
	SujetoID string `json:"sujeto_id"`
	TenantID string `json:"tenant_id"`
	Rol      string `json:"rol"`
	CreadoEn string `json:"creado_en"`
}

// payloadSujetoDesactivado es el payload de sujeto.desactivado.
type payloadSujetoDesactivado struct {
	SujetoID      string `json:"sujeto_id"`
	TenantID      string `json:"tenant_id"`
	DesactivadoEn string `json:"desactivado_en"`
}

// payloadSujetoReactivado es el payload de sujeto.reactivado (hecho propio, C5).
type payloadSujetoReactivado struct {
	SujetoID     string `json:"sujeto_id"`
	TenantID     string `json:"tenant_id"`
	ReactivadoEn string `json:"reactivado_en"`
}

// payloadRolAsignado es el payload de rol.asignado.
type payloadRolAsignado struct {
	SujetoID   string `json:"sujeto_id"`
	TenantID   string `json:"tenant_id"`
	Rol        string `json:"rol"`
	AsignadoEn string `json:"asignado_en"`
}

// payloadRolRevocado es el payload de rol.revocado.
type payloadRolRevocado struct {
	SujetoID   string `json:"sujeto_id"`
	TenantID   string `json:"tenant_id"`
	Rol        string `json:"rol"`
	RevocadoEn string `json:"revocado_en"`
}

// datosHecho reúne lo que un Hecho_Gobierno necesita para su payload. El rol es
// opcional: solo lo llevan sujeto.creado, rol.asignado y rol.revocado.
type datosHecho struct {
	sujetoID   string
	tenantID   string
	rol        string
	ocurridoEn time.Time
}

// codificarHecho serializa el payload del hecho `tipo` con la forma del
// contrato. Devuelve error si el tipo no es un Hecho_Gobierno conocido, para no
// publicar un payload mudo (regla: no inventar una variante local).
func codificarHecho(tipo string, d datosHecho) (json.RawMessage, error) {
	ts := d.ocurridoEn.UTC().Format(time.RFC3339Nano)
	switch tipo {
	case hechoSujetoCreado:
		return json.Marshal(payloadSujetoCreado{SujetoID: d.sujetoID, TenantID: d.tenantID, Rol: d.rol, CreadoEn: ts})
	case hechoSujetoDesactivado:
		return json.Marshal(payloadSujetoDesactivado{SujetoID: d.sujetoID, TenantID: d.tenantID, DesactivadoEn: ts})
	case hechoSujetoReactivado:
		return json.Marshal(payloadSujetoReactivado{SujetoID: d.sujetoID, TenantID: d.tenantID, ReactivadoEn: ts})
	case hechoRolAsignado:
		return json.Marshal(payloadRolAsignado{SujetoID: d.sujetoID, TenantID: d.tenantID, Rol: d.rol, AsignadoEn: ts})
	case hechoRolRevocado:
		return json.Marshal(payloadRolRevocado{SujetoID: d.sujetoID, TenantID: d.tenantID, Rol: d.rol, RevocadoEn: ts})
	default:
		return nil, fmt.Errorf("hecho de gobierno desconocido: %q", tipo)
	}
}
