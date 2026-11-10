-- 0010_alcance_monitoreo.sql — alcance de monitoreo: "mis unidades" y la
-- jerarquía (tracer.40, E8-T19, DEC-140, DEC-145). Cierra el TODO(DEC-045) de
-- 0001.
--
-- alcance_unidad: las unidades de cada Monitorista (DEC-140 #1). `unidad_id` es
-- id de `operacion`: sin FK entre módulos, identidad no valida que exista (lo
-- valida el `bff`). Una unidad puede estar en varios Monitoristas (DEC-145 #1),
-- así que la PK es el par.
--
-- jerarquia: Supervisor → Monitoristas y Coordinador → Supervisores (DEC-140
-- #2). Es exclusiva: cada subordinado tiene a lo sumo un superior (DEC-145 #2),
-- y la unicidad de `subordinado_id` lo garantiza aunque dos administradores
-- asignen a la vez; el servicio traduce la violación a 409
-- `subordinado_con_superior`.
--
-- Las dos tablas son el estado vigente, no un historial: fijar reemplaza el
-- conjunto (DELETE + INSERT en una tx) y cambiar de rol lo retira (DEC-140 #3).
-- El retiro queda en `cambio_identidad`, que sigue siendo append-only.
--
-- GRANT: SELECT, INSERT y DELETE, sin UPDATE. Es la única excepción a "nunca
-- borra" del esquema, porque el contrato modela un conjunto que se reemplaza.
-- El caso está en grants_integration_test.go.

SET search_path TO identidad;

CREATE TABLE IF NOT EXISTS identidad.alcance_unidad (
  monitorista_id UUID        NOT NULL REFERENCES identidad.sujeto (id),
  unidad_id      UUID        NOT NULL,
  asignado_en    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (monitorista_id, unidad_id)
);

CREATE TABLE IF NOT EXISTS identidad.jerarquia (
  superior_id    UUID        NOT NULL REFERENCES identidad.sujeto (id),
  subordinado_id UUID        NOT NULL REFERENCES identidad.sujeto (id),
  asignado_en    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (superior_id, subordinado_id),
  CONSTRAINT jerarquia_subordinado_unico UNIQUE (subordinado_id),
  CONSTRAINT jerarquia_sin_autorreferencia CHECK (superior_id <> subordinado_id)
);

GRANT SELECT, INSERT, DELETE ON identidad.alcance_unidad TO svc_identidad;
GRANT SELECT, INSERT, DELETE ON identidad.jerarquia      TO svc_identidad;
