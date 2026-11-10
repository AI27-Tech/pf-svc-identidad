-- 0009_tenant_activa.sql — estado activo del tenant (tracer.40, E8-T19, DEC-144).
--
-- Desactivar la organización cliente marca su tenant inactivo. Es terminal: no
-- hay reactivación en V1 (DEC-139 #4). Sus sujetos pierden el acceso en el
-- siguiente refresco: la emisión y el refresco rechazan a un sujeto de tenant
-- inactivo. Todo tenant existente queda activo (DEFAULT true).
--
-- GRANT: el servicio solo actualiza la columna `activa`, nunca el nombre, el
-- tipo ni el id (privilegio por columna). Sigue sin DELETE. El caso está en
-- grants_integration_test.go.

SET search_path TO identidad;

ALTER TABLE identidad.tenant
  ADD COLUMN IF NOT EXISTS activa BOOLEAN NOT NULL DEFAULT true;

GRANT UPDATE (activa) ON identidad.tenant TO svc_identidad;
