// payload_contract_test.go es la prueba de contrato de los Hechos_Gobierno
// (Tarea 14 / B3, Requisito 8.5). Reconcilia dos cosas contra el contrato
// publicado (tracer.11), sin Postgres ni red:
//
//  1. El nombre canónico de cada hecho que emite el servicio coincide con el
//     registro contractual de tipos (registro.Tipos) y su productor es
//     `identidad`.
//  2. El payload que serializa codificarHecho decodifica sin error contra el
//     mensaje protobuf generado del contrato, con sus campos intactos.
//
// Si el contrato cambia el nombre o la forma de un hecho, esta prueba falla y
// delata la divergencia antes de que llegue a un consumidor.
package gobierno

import (
	"testing"
	"time"

	eventosv1 "github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/eventos/ai27/eventos/v1"
	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/registro"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TestHechosGobiernoEnRegistro verifica que los cinco nombres canónicos que el
// servicio emite están en el registro contractual y los produce `identidad`.
func TestHechosGobiernoEnRegistro(t *testing.T) {
	hechos := []string{
		hechoSujetoCreado,
		hechoSujetoDesactivado,
		hechoSujetoReactivado,
		hechoRolAsignado,
		hechoRolRevocado,
		hechoAlcanceCambiado,
	}
	for _, tipo := range hechos {
		if !registro.EsTipoRegistrado(tipo) {
			t.Errorf("el hecho %q no está en el registro contractual (tracer.11)", tipo)
			continue
		}
		prod, ok := registro.ProductorDe(tipo)
		if !ok || prod != "identidad" {
			t.Errorf("productor de %q = %q (ok=%v); se esperaba \"identidad\"", tipo, prod, ok)
		}
	}
}

// TestPayloadDecodificaContraProto verifica que el payload de cada hecho
// decodifica contra su mensaje protobuf del contrato y preserva los campos.
func TestPayloadDecodificaContraProto(t *testing.T) {
	const (
		sujetoID = "11111111-1111-1111-1111-111111111111"
		tenantID = "22222222-2222-2222-2222-222222222222"
		rol      = "administrador"
	)
	ocurrido := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	d := datosHecho{sujetoID: sujetoID, tenantID: tenantID, rol: rol, ocurridoEn: ocurrido}

	casos := []struct {
		nombre string
		tipo   string
		msg    proto.Message
		verify func(t *testing.T, m proto.Message)
	}{
		{
			nombre: "sujeto.creado",
			tipo:   hechoSujetoCreado,
			msg:    &eventosv1.SujetoCreado{},
			verify: func(t *testing.T, m proto.Message) {
				e := m.(*eventosv1.SujetoCreado)
				if e.GetSujetoId() != sujetoID || e.GetTenantId() != tenantID || e.GetRol() != rol {
					t.Errorf("SujetoCreado = %+v; campos no coinciden", e)
				}
				if e.GetCreadoEn() == nil {
					t.Error("SujetoCreado.creado_en vacío")
				}
			},
		},
		{
			nombre: "sujeto.desactivado",
			tipo:   hechoSujetoDesactivado,
			msg:    &eventosv1.SujetoDesactivado{},
			verify: func(t *testing.T, m proto.Message) {
				e := m.(*eventosv1.SujetoDesactivado)
				if e.GetSujetoId() != sujetoID || e.GetTenantId() != tenantID {
					t.Errorf("SujetoDesactivado = %+v; campos no coinciden", e)
				}
				if e.GetDesactivadoEn() == nil {
					t.Error("SujetoDesactivado.desactivado_en vacío")
				}
			},
		},
		{
			nombre: "sujeto.reactivado",
			tipo:   hechoSujetoReactivado,
			msg:    &eventosv1.SujetoReactivado{},
			verify: func(t *testing.T, m proto.Message) {
				e := m.(*eventosv1.SujetoReactivado)
				if e.GetSujetoId() != sujetoID || e.GetTenantId() != tenantID {
					t.Errorf("SujetoReactivado = %+v; campos no coinciden", e)
				}
				if e.GetReactivadoEn() == nil {
					t.Error("SujetoReactivado.reactivado_en vacío")
				}
			},
		},
		{
			nombre: "rol.asignado",
			tipo:   hechoRolAsignado,
			msg:    &eventosv1.RolAsignado{},
			verify: func(t *testing.T, m proto.Message) {
				e := m.(*eventosv1.RolAsignado)
				if e.GetSujetoId() != sujetoID || e.GetTenantId() != tenantID || e.GetRol() != rol {
					t.Errorf("RolAsignado = %+v; campos no coinciden", e)
				}
				if e.GetAsignadoEn() == nil {
					t.Error("RolAsignado.asignado_en vacío")
				}
			},
		},
		{
			nombre: "rol.revocado",
			tipo:   hechoRolRevocado,
			msg:    &eventosv1.RolRevocado{},
			verify: func(t *testing.T, m proto.Message) {
				e := m.(*eventosv1.RolRevocado)
				if e.GetSujetoId() != sujetoID || e.GetTenantId() != tenantID || e.GetRol() != rol {
					t.Errorf("RolRevocado = %+v; campos no coinciden", e)
				}
				if e.GetRevocadoEn() == nil {
					t.Error("RolRevocado.revocado_en vacío")
				}
			},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			payload, err := codificarHecho(c.tipo, d)
			if err != nil {
				t.Fatalf("codificarHecho(%q) devolvió error: %v", c.tipo, err)
			}
			// protojson acepta los nombres de campo del proto (snake_case), que
			// es justo la forma que usa el payload del outbox.
			if err := (protojson.UnmarshalOptions{}).Unmarshal(payload, c.msg); err != nil {
				t.Fatalf("protojson.Unmarshal del payload de %q contra el proto: %v", c.tipo, err)
			}
			c.verify(t, c.msg)
		})
	}
}

// TestPayloadAlcanceDecodificaContraProto verifica que alcance.cambiado
// (tracer.41) decodifica contra AlcanceCambiado del contrato, también con el
// conjunto vacío del retiro por cambio de rol.
func TestPayloadAlcanceDecodificaContraProto(t *testing.T) {
	const sujetoID = "11111111-1111-1111-1111-111111111111"
	cambiado := time.Date(2026, 11, 17, 12, 0, 0, 123000000, time.UTC)
	casos := map[string][]string{
		"con clientes": {"33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444"},
		"vacío":        nil,
	}
	for nombre, clientes := range casos {
		t.Run(nombre, func(t *testing.T) {
			payload, err := codificarAlcance(AlcanceEfectivo{SujetoID: sujetoID, ClientesEfectivosIDs: clientes, CambiadoEn: cambiado})
			if err != nil {
				t.Fatalf("codificarAlcance: %v", err)
			}
			var e eventosv1.AlcanceCambiado
			if err := (protojson.UnmarshalOptions{}).Unmarshal(payload, &e); err != nil {
				t.Fatalf("protojson.Unmarshal contra AlcanceCambiado: %v (payload %s)", err, payload)
			}
			if e.GetSujetoId() != sujetoID || len(e.GetClientesEfectivosIds()) != len(clientes) || !e.GetCambiadoEn().AsTime().Equal(cambiado) {
				t.Errorf("AlcanceCambiado = %+v; no coincide con la entrada", &e)
			}
			for i, c := range clientes {
				if e.GetClientesEfectivosIds()[i] != c {
					t.Errorf("clientes_efectivos_ids[%d] = %q; quiere %q", i, e.GetClientesEfectivosIds()[i], c)
				}
			}
		})
	}
}
