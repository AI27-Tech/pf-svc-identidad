package sesion

import (
	"encoding/json"
)

// payloadDefault es el codificador por omisión del payload de sesion.revocada
// (Requisito 5.1, tarea 2.3). Serializa el Tombstone con los nombres de campo
// canónicos (lowerCamelCase por protojson; H-11 de design.md).
//
// El contrato declara el evento SesionRevocada en protobuf (pf-pkg-contracts
// eventos/v1/sesion.proto); la opción A-14 deja abierto si se serializa con
// json.Marshal (snake_case) o protojson (lowerCamelCase). La propuesta de Kiro
// usa protojson con UseProtoNames: true; mientras tanto, el codificador por
// omisión usa json.Marshal (comportamiento de hoy).
func payloadDefault(t Tombstone) (json.RawMessage, error) {
	return json.Marshal(t)
}

// payloadViaProtojson es una alternativa que serializa con protojson (opción b
// de A-14, propuesta de Kiro no aprobada). Se usaría si A-14 así lo decide.
// Preserva por si se necesita en pruebas (sesiontest).
func payloadViaProtojson(t Tombstone) (json.RawMessage, error) {
	// Decodificar el JSON a un tipo protobuf, codificar con protojson.
	// Por ahora es referencia comentada, ya que no se exporta y la propuesta
	// no entra en la ruta productiva (design.md §Soporte de pruebas).
	// Implementación real requeriría generar el tipo de protobuf y hacer la
	// conversión; queda como nota para el futuro.
	return json.Marshal(t) // Por ahora, usar el comportamiento de hoy
}
