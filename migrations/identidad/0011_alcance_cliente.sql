-- 0011_alcance_cliente.sql — alcance de monitoreo por cliente: "mis clientes"
-- y los clientes efectivos que publica `alcance.cambiado` (tracer.41, DEC-149
-- #18 y #20). Reemplaza a `alcance_unidad` como alcance; esa tabla se queda
-- mientras `fijarUnidadesSujeto` siga deprecada en el contrato.
--
-- alcance_cliente: los clientes de cada Monitorista (DEC-149 #18). `cliente_id`
-- es id de `catalogos`: sin FK entre módulos, identidad no valida que exista ni
-- que esté activo (lo valida el `bff`, DEC-149 #21). Un cliente puede estar en
-- varios Monitoristas, así que la PK es el par. Mismo ciclo que
-- `alcance_unidad`: fijar reemplaza el conjunto (DELETE + INSERT en una tx) y
-- cambiar de rol lo retira (DEC-140 #3), con el retiro en `cambio_identidad`.
--
-- alcance_efectivo: el último conjunto de clientes efectivos publicado por
-- sujeto (sus clientes más los de sus subordinados por la jerarquía) y cuándo
-- cambió. Es lo que sirve `listarAlcances`: el snapshot trae las filas con al
-- menos un cliente y la continuación por cursor trae también las que quedaron
-- vacías, para que el consumidor las quite. Una fila por sujeto que tuvo alcance
-- alguna vez; se actualiza en la misma tx que escribe `alcance.cambiado`, con
-- el mismo `cambiado_en`. Sin DELETE: el sujeto tampoco se borra.
--
-- GRANT: alcance_cliente igual que alcance_unidad (SELECT, INSERT, DELETE, sin
-- UPDATE). alcance_efectivo SELECT, INSERT, UPDATE, sin DELETE; el UPDATE
-- también habilita el LOCK TABLE con que el servicio serializa las escrituras
-- de alcance y da un cursor sin ventana. Los casos están en
-- grants_integration_test.go.

SET search_path TO identidad;

CREATE TABLE IF NOT EXISTS identidad.alcance_cliente (
  monitorista_id UUID        NOT NULL REFERENCES identidad.sujeto (id),
  cliente_id     UUID        NOT NULL,
  asignado_en    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (monitorista_id, cliente_id)
);

CREATE TABLE IF NOT EXISTS identidad.alcance_efectivo (
  sujeto_id              UUID        PRIMARY KEY REFERENCES identidad.sujeto (id),
  clientes_efectivos_ids UUID[]      NOT NULL,
  cambiado_en            TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS alcance_efectivo_cambiado_en_idx
  ON identidad.alcance_efectivo (cambiado_en);

GRANT SELECT, INSERT, DELETE ON identidad.alcance_cliente  TO svc_identidad;
GRANT SELECT, INSERT, UPDATE ON identidad.alcance_efectivo TO svc_identidad;
