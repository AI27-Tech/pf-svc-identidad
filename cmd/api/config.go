package main

import (
	"github.com/AI27-Tech/pf-svc-identidad/idpconfig"
)

// seleccionIDP es el proveedor de identidad elegido por el entorno. Alias del
// tipo público de idpconfig para conservar los nombres locales de este cmd.
type seleccionIDP = idpconfig.Seleccion

const (
	idpMemory   = idpconfig.Memory
	idpKeycloak = idpconfig.Keycloak
)

// configIDP es el resultado de resolver el proveedor desde el entorno. Alias del
// tipo público de idpconfig: la lógica vive una sola vez en ese paquete, y tanto
// este cmd standalone como `core` la comparten (antes estaba atrapada aquí como
// package main, inalcanzable desde core).
type configIDP = idpconfig.Config

// cargarConfig resuelve el proveedor de identidad desde el entorno. Delega en
// idpconfig.Cargar; se conserva este nombre para las pruebas de este cmd.
func cargarConfig(lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (configIDP, error) {
	return idpconfig.Cargar(lookup, readFile)
}
