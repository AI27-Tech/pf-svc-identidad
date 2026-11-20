-- 0012_rol_cotizacion.sql — rol `cotizacion` (tracer.45, DEC-148 #1 y #20).
--
-- Rol interno y global: atiende la conversación y la cotización de las
-- solicitudes; no tiene "mis clientes" ni subordinados. El enum canónico vive
-- en el contrato (api-client-go/roles); esta tabla es el catálogo local, así
-- que sin la fila `cotizacion` la FK de asignacion_rol rechazaría el alta.
--
-- El CHECK de `rol.codigo` (0001) enumera los ocho roles anteriores: se
-- reemplaza por uno de nueve. 0001 no se edita porque su CREATE TABLE IF NOT
-- EXISTS no cambiaría una tabla ya creada. Idempotente: DROP IF EXISTS + ADD, e
-- INSERT con ON CONFLICT. Sin GRANT nuevos: svc_identidad ya lee `rol` (0002).

SET search_path TO identidad;

ALTER TABLE identidad.rol DROP CONSTRAINT IF EXISTS rol_codigo_check;
ALTER TABLE identidad.rol ADD CONSTRAINT rol_codigo_check CHECK (codigo IN (
  'cliente', 'jefe_operativo', 'monitorista', 'supervisor',
  'coordinador', 'ejecutivo', 'reaccion', 'administrador', 'cotizacion'
));

INSERT INTO identidad.rol (codigo) VALUES ('cotizacion') ON CONFLICT (codigo) DO NOTHING;
