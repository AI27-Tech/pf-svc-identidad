-- NOTA PARA E4 (Angel): esta migración la agregó el Lead como parte de E0-T5.
-- Toda tabla nueva de identidad declara sus GRANT explícitos en su propia fase
-- de grants y se agrega a grants_integration_test.go.
--
-- 0002_grants.sql — privilegio mínimo por tabla del esquema identidad (E0-T5).
-- Fase de grants, separada de la de estructura (0001, que está en el tag
-- v0.1.1 y NO se reescribe). Patrón de referencia:
-- pf-svc-catalogos/migrations/catalogos/0002_grants.sql.
--
-- Por qué existe: 0001 otorga SELECT, INSERT, UPDATE y DELETE sobre TODAS las
-- tablas y USAGE/SELECT sobre todas las secuencias a svc_identidad. Medido sobre
-- Postgres: svc_identidad podía DELETE en sesion (contradice el tombstone de
-- retención larga, ADR 26), en cambio_identidad (append-only, ADR 26 §7) y en el
-- catálogo rol. El invariante I1 (ADR 17) solo es real con privilegio mínimo: el
-- rol lee lo suyo, escribe solo lo que su flujo documentado necesita, y nunca
-- borra. Ningún GRANT hacia este esquema fuera de svc_identidad y ningún GRANT
-- de svc_identidad sobre otro esquema (§A.5 R1).
--
-- La aplica el MISMO rol que aplicó 0001. Es idempotente: reaplicarla deja el
-- mismo estado.

-- --- svc_identidad pasa a LOGIN (decisión del Lead E0-T5) ----------------------
-- 0001 creó el rol NOLOGIN; el pool del servicio se conecta con él, así que debe
-- poder iniciar sesión. No se reescribe 0001 (está en v0.1.1): el cambio va aquí.
ALTER ROLE svc_identidad LOGIN;

-- --- Partir de cero: retirar lo que 0001 otorgó de forma genérica --------------
REVOKE ALL ON ALL TABLES    IN SCHEMA identidad FROM svc_identidad;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA identidad FROM svc_identidad;
-- 0001 no deja un ALTER DEFAULT PRIVILEGES genérico (otorga sobre ALL TABLES ya
-- creadas, no un default), así que una tabla futura no hereda privilegios por
-- ese camino. Se declara el REVOKE del default de todas formas, por el patrón
-- del Lead y para blindar contra un default que se agregara más adelante.
ALTER DEFAULT PRIVILEGES IN SCHEMA identidad
  REVOKE ALL ON TABLES FROM svc_identidad;

-- --- Semilla de los ocho roles (DEC-045, identificadores de DEC-053) -----------
-- Los ocho roles canónicos se siembran AQUÍ (como admin), no desde el servicio:
-- svc_identidad solo tiene SELECT sobre `rol`. El catálogo local refleja el enum
-- del contrato (api-client-go/roles). Idempotente por ON CONFLICT.
INSERT INTO identidad.rol (codigo) VALUES
  ('cliente'),
  ('jefe_operativo'),
  ('monitorista'),
  ('supervisor'),
  ('coordinador'),
  ('ejecutivo'),
  ('reaccion'),
  ('administrador')
ON CONFLICT (codigo) DO NOTHING;

-- --- rol: catálogo de solo lectura para el servicio ----------------------------
-- El catálogo lo administra la migración (semilla de arriba); el servicio solo
-- lo lee para validar. Sin INSERT, UPDATE ni DELETE.
GRANT SELECT ON identidad.rol TO svc_identidad;

-- --- sujeto: identidad del sujeto, ciclo de vida activa/baja por marca ---------
-- Se da de alta (INSERT) y se actualiza (activo, actualizado_en). No se borra:
-- la identidad es referida por sesiones y auditoría (P10). SELECT, INSERT, UPDATE.
GRANT SELECT, INSERT, UPDATE ON identidad.sujeto TO svc_identidad;

-- --- asignacion_rol: asignación de rol con ciclo de vida activa/revocada --------
-- Revocar un rol marca revocado_en (UPDATE), no borra la fila. SELECT, INSERT,
-- UPDATE; sin DELETE (la historia de qué rol tuvo un sujeto se conserva).
GRANT SELECT, INSERT, UPDATE ON identidad.asignacion_rol TO svc_identidad;

-- --- sesion: fuente de verdad del ciclo de sesión (ADR 26) ---------------------
-- Revocar marca revocada_en (UPDATE); la fila es un tombstone de retención larga
-- (ADR 26 §4). NUNCA se borra: no hay purga en V1. SELECT, INSERT, UPDATE.
GRANT SELECT, INSERT, UPDATE ON identidad.sesion TO svc_identidad;

-- --- cambio_identidad: auditoría local append-only (ADR 26 §7) -----------------
-- Alta del cambio en la misma tx que su efecto. Nunca se edita ni borra.
GRANT SELECT, INSERT ON identidad.cambio_identidad TO svc_identidad;
GRANT USAGE ON SEQUENCE identidad.cambio_identidad_id_seq TO svc_identidad;

-- --- alcance_tenant: pertenencia de sujeto a tenant ----------------------------
-- Se da de alta (INSERT) y se lee (SELECT). SIN UPDATE ni DELETE.
-- NOTA (pendiente, DEC-022 #2): el versionado del alcance queda pendiente para
-- cuando exista jerarquía de tenant real; cuando se defina, esta tabla recibirá
-- el privilegio de versionado que su flujo necesite (y su caso en la prueba).
GRANT SELECT, INSERT ON identidad.alcance_tenant TO svc_identidad;

-- --- outbox: publicación transaccional de eventos (ADR 29) ---------------------
-- El publicador inserta en la misma tx que el efecto; el relay lee los
-- pendientes y marca publicado/publicado_en. No borra. Igual que catalogos.
GRANT SELECT, INSERT, UPDATE ON identidad.outbox TO svc_identidad;
GRANT USAGE ON SEQUENCE identidad.outbox_id_seq TO svc_identidad;
