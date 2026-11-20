-- 0012_rol_cotizacion.sql — rol `cotizacion` (tracer.45, DEC-148 #1 y #20).
--
-- Rol interno y global: atiende la conversación y la cotización de las
-- solicitudes; no tiene "mis clientes" ni subordinados. El enum canónico vive
-- en el contrato (api-client-go/roles); esta tabla es el catálogo local, así
-- que sin la fila `cotizacion` la FK de asignacion_rol rechazaría el alta.
--
-- El CHECK de `rol.codigo` (0001) enumera los ocho roles anteriores: se
-- reemplaza por uno de nueve. 0001 no se edita porque su CREATE TABLE IF NOT
-- EXISTS no cambiaría una tabla ya creada. El CHECK se busca en pg_constraint
-- (CHECK de la tabla que cubre `codigo`) y no por su nombre automático, que
-- cambia si alguien lo renombró. Idempotente: si ya hay un CHECK con los nueve
-- roles lo conserva, y el INSERT usa ON CONFLICT. Sin GRANT nuevos:
-- svc_identidad ya lee `rol` (0002).

SET search_path TO identidad;

DO $$
DECLARE
  c record;
  vigente boolean := false;
  nueve text[] := ARRAY['cliente', 'jefe_operativo', 'monitorista', 'supervisor',
                        'coordinador', 'ejecutivo', 'reaccion', 'administrador', 'cotizacion'];
BEGIN
  FOR c IN
    SELECT k.conname, pg_get_constraintdef(k.oid) AS def
      FROM pg_constraint k
     WHERE k.conrelid = 'identidad.rol'::regclass
       AND k.contype = 'c'
       AND k.conkey @> ARRAY[(SELECT a.attnum FROM pg_attribute a
                               WHERE a.attrelid = 'identidad.rol'::regclass AND a.attname = 'codigo')]
  LOOP
    IF NOT vigente AND (SELECT bool_and(strpos(c.def, quote_literal(n)) > 0) FROM unnest(nueve) AS n) THEN
      vigente := true;
    ELSE
      EXECUTE format('ALTER TABLE identidad.rol DROP CONSTRAINT %I', c.conname);
    END IF;
  END LOOP;

  IF NOT vigente THEN
    EXECUTE 'ALTER TABLE identidad.rol ADD CONSTRAINT rol_codigo_check CHECK (codigo IN ('
      || (SELECT string_agg(quote_literal(n), ', ') FROM unnest(nueve) AS n) || '))';
  END IF;
END
$$;

INSERT INTO identidad.rol (codigo) VALUES ('cotizacion') ON CONFLICT (codigo) DO NOTHING;
