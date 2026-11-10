-- 0008_sujeto_datos_locales.sql — copia local del nombre y el identificador de
-- acceso del sujeto (tracer.40, E8-T19, B-2/B-15).
--
-- Hasta ahora el nombre y el identificador de acceso vivían solo en el IdP
-- (`CrearUsuario`). Listar y buscar usuarios (`listarSujetos`) sin llamar al IdP
-- por cada fila exige una copia local. El IdP sigue siendo dueño de la
-- credencial; editar el nombre (`editarSujeto`) cambia los dos lados (local
-- manda, DEC-085). El identificador no se edita en V1 (DEC-139 #5).
--
-- Las dos columnas admiten NULL: un sujeto dado de alta antes de tracer.40 no
-- tiene esos datos y solo aparece en el listado sin filtro `q`.
--
-- 0007 queda reservado para la semilla del tenant interno (ver 0006).
--
-- GRANT: `sujeto` ya tiene SELECT, INSERT, UPDATE por tabla (0002), que cubre
-- las columnas nuevas. El caso de escritura de estas columnas está en
-- grants_integration_test.go.

SET search_path TO identidad;

ALTER TABLE identidad.sujeto
  ADD COLUMN IF NOT EXISTS nombre                TEXT,
  ADD COLUMN IF NOT EXISTS identificador_usuario TEXT;
