// Package gobierno albergará la superficie de GOBIERNO de identidad: alta/baja
// de sujetos y asignación/revocación de rol con alcance, más sus eventos
// (`sujeto.creado`, `sujeto.desactivado`, `rol.asignado`, `rol.revocado`), que
// `mensajeria` proyecta para su pool (identidad.md (f), (g); DEC-045 #8).
//
// BRECHA CONTRACTUAL (regla 6 de 00-indice.md): nada de esta superficie existe
// en la versión fijada de pf-pkg-contracts (verificado en api-client-go
// v0.1.0-tracer.3):
//
//   - FALTA  /api/v1/sujetos/...        (alta, baja)
//   - FALTA  /api/v1/roles/...          (asignación y revocación de rol con alcance)
//   - FALTA  eventos sujeto.creado · sujeto.desactivado · rol.asignado · rol.revocado
//     (no hay .proto ni entrada en INVENTARIO.md / registro.tipos.yaml)
//
// Por eso este paquete NO cablea rutas ni escribe filas de `outbox` para esos
// eventos: la brecha se REPORTA (ver el reporte de brechas del scaffold), no se
// rellena a mano. El puerto queda vacío a propósito.
//
// Además, el ALCANCE (unidad/cliente) no viaja en el token (DEC-053): la lectura
// de alcance y los eventos de cambio de alcance también faltan en el contrato, y
// su forma depende de la mecánica del vínculo Monitorista↔unidades aún pendiente
// (DEC-045, Notas) → TODO(DEC-045).
package gobierno

// Gobierno es el puerto de la superficie de gobierno. Está VACÍO a propósito:
// no se le añaden métodos mientras exista la brecha contractual de /sujetos y
// /roles. Añadir métodos aquí implicaría escribir a mano tipos de wire o
// eventos, lo que la regla 6 prohíbe.
//
// TODO(contrato): poblar cuando pf-pkg-contracts publique /api/v1/sujetos,
// /api/v1/roles y los eventos sujeto.*/rol.* (con su forma de alcance, que a su
// vez espera la mecánica de DEC-045). Hasta entonces, no hay implementación.
type Gobierno interface{}
