// Package migrations expone, como embed.FS, las migraciones SQL del esquema
// `identidad`. La raíz de composición (`core`) y `cmd/api` las aplican con el
// rol dueño (`svc_identidad`), 00-indice.md "Forma de un módulo de servicio".
//
// SCAFFOLD: sólo se exponen los archivos embebidos; este paquete no ejecuta las
// migraciones ni elige runner (eso es de quien monta el despliegue).
package migrations

import "embed"

// FS contiene las migraciones del esquema `identidad`, en orden por nombre de
// archivo (0001_init.sql, ...). Exportado para que la raíz las aplique.
//
//go:embed identidad/*.sql
var FS embed.FS
