-- Migración inicial del esquema `identidad` (ADR 17 I1, identidad.md (e)).
--
-- SCAFFOLD: crea el esquema, el rol dueño y las tablas con su forma. No incluye
-- datos semilla ni lógica. Los nombres de dominio van en español
-- (naming-conventions.md): sesion, sujeto, alcance_tenant, asignacion_rol,
-- cambio_identidad; los identificadores técnicos, en inglés.
--
-- El vínculo Monitorista↔`unidades` NO se modela aquí: su mecánica está sin
-- definir (DEC-045, Notas). No se crea tabla ni columna para él (TODO(DEC-045)).

-- -----------------------------------------------------------------------------
-- Esquema y rol dueño. `svc_identidad` tiene USAGE sólo sobre este esquema
-- (identidad.md (e), §A.5 R1). La contraseña y los GRANT finos los fija el
-- despliegue; aquí se declara el rol y su USAGE mínimo.
-- -----------------------------------------------------------------------------
CREATE SCHEMA IF NOT EXISTS identidad;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'svc_identidad') THEN
    CREATE ROLE svc_identidad NOLOGIN;
  END IF;
END
$$;

GRANT USAGE ON SCHEMA identidad TO svc_identidad;

SET search_path TO identidad;

-- -----------------------------------------------------------------------------
-- sujeto — identidad de un sujeto dentro de un tenant. `identidad` NO es dueño
-- de credenciales ni contraseñas (eso es del IdP): aquí sólo vive la referencia
-- de identidad y su tenant inmutable (P10).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sujeto (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  -- identificador del sujeto en el IdP (subject OIDC). No es la credencial.
  idp_subject   TEXT        NOT NULL UNIQUE,
  tenant_id     UUID        NOT NULL,
  activo        BOOLEAN     NOT NULL DEFAULT true,
  creado_en     TIMESTAMPTZ NOT NULL DEFAULT now(),
  actualizado_en TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- -----------------------------------------------------------------------------
-- rol — catálogo de los ocho roles de DEC-045 (identificadores de DEC-053). El
-- enum canónico vive en el contrato (api-client-go/roles); esta tabla es el
-- catálogo local. No hay rol `auditor` ni listas de 4/5 roles (DEC-036/022).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS rol (
  codigo        TEXT        PRIMARY KEY
    CHECK (codigo IN (
      'cliente', 'jefe_operativo', 'monitorista', 'supervisor',
      'coordinador', 'ejecutivo', 'reaccion', 'administrador'
    ))
);

-- -----------------------------------------------------------------------------
-- asignacion_rol — asignación de un rol a un sujeto dentro de un tenant, con
-- ciclo de vida (activa/revocada) y actor. Es el molde reutilizado del repo de
-- referencia, recortado al dominio de AI27.
--
-- El IDENTIFICADOR del recurso de alcance (a qué `unidad` o `cliente`) NO se
-- modela todavía: la mecánica del vínculo está sin definir (DEC-045, Notas).
-- TODO(DEC-045): no se añade columna de recurso de alcance aquí hasta que esté.
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS asignacion_rol (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  sujeto_id     UUID        NOT NULL REFERENCES sujeto(id),
  rol_codigo    TEXT        NOT NULL REFERENCES rol(codigo),
  asignado_por  UUID        NOT NULL,
  asignado_en   TIMESTAMPTZ NOT NULL DEFAULT now(),
  revocado_por  UUID,
  revocado_en   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS asignacion_rol_sujeto_idx
  ON asignacion_rol (sujeto_id) WHERE revocado_en IS NULL;

-- -----------------------------------------------------------------------------
-- alcance_tenant — jerarquía de tenant. Expresa la pertenencia de un sujeto a
-- su tenant y, para `cliente`, el acotamiento a su propio tenant (P10). El
-- alcance efectivo se resuelve EN SERVIDOR (DEC-053), nunca en el token.
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS alcance_tenant (
  sujeto_id     UUID        NOT NULL REFERENCES sujeto(id),
  tenant_id     UUID        NOT NULL,
  creado_en     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (sujeto_id, tenant_id)
);

-- -----------------------------------------------------------------------------
-- sesion — FUENTE DE VERDAD del ciclo de vida de sesión (ADR 26). El `sesion_id`
-- es propio de AI27 y estable durante todos los refresh. Redis es sólo caché y
-- nunca fuente de verdad (identidad.md (e)).
--
-- Índice parcial `una_sesion_viva`: a lo sumo una sesión viva por sujeto
-- (revocada_en IS NULL). Revocar marca la fila; nunca se borra (tombstone).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sesion (
  sesion_id     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  sujeto_id     UUID        NOT NULL REFERENCES sujeto(id),
  emitida_en    TIMESTAMPTZ NOT NULL DEFAULT now(),
  expira_en     TIMESTAMPTZ NOT NULL,
  revocada_en   TIMESTAMPTZ
);

-- A lo sumo una sesión viva por sujeto (ADR 26). El parcial sólo cuenta las no
-- revocadas; las revocadas (tombstones) permanecen sin violar la unicidad.
CREATE UNIQUE INDEX IF NOT EXISTS una_sesion_viva
  ON sesion (sujeto_id) WHERE revocada_en IS NULL;

-- Para el snapshot de tombstones no expirados (ADR 26 §4).
CREATE INDEX IF NOT EXISTS sesion_revocadas_idx
  ON sesion (revocada_en) WHERE revocada_en IS NOT NULL;

-- -----------------------------------------------------------------------------
-- cambio_identidad — auditoría local append-only (ADR 26 §7). El payload del
-- evento `sesion.revocada` es mínimo; el motivo, sujeto, tenant, actor y claims
-- quedan AQUÍ, no en el evento.
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cambio_identidad (
  id            BIGSERIAL   PRIMARY KEY,
  tipo          TEXT        NOT NULL, -- p.ej. 'sesion.revocada', 'rol.asignado'
  sujeto_id     UUID,
  tenant_id     UUID,
  actor_id      UUID,
  sesion_id     UUID,
  motivo        TEXT,
  detalle       JSONB       NOT NULL DEFAULT '{}'::jsonb,
  ocurrido_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- -----------------------------------------------------------------------------
-- outbox — publicación transaccional de eventos (ADR 29). El DDL es el de
-- referencia de pf-pkg-platform-go/outbox (OutboxDDL), creado en ESTE esquema
-- porque platform-go no administra esquemas. El Relay del módulo la drena.
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS outbox (
  id            BIGSERIAL   PRIMARY KEY,
  evento_id     UUID        NOT NULL UNIQUE,
  tipo          TEXT        NOT NULL,
  version       INT         NOT NULL DEFAULT 1,
  agregado_id   TEXT        NOT NULL,
  sensibilidad  TEXT        NOT NULL DEFAULT 'amplia',
  ocurrido_en   TIMESTAMPTZ NOT NULL,
  payload       JSONB       NOT NULL,
  trace_context JSONB       NOT NULL DEFAULT '{}'::jsonb,
  publicado     BOOLEAN     NOT NULL DEFAULT false,
  publicado_en  TIMESTAMPTZ,
  creado_en     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outbox_pendientes_idx
  ON outbox (creado_en) WHERE publicado = false;

-- Privilegios del rol dueño sobre lo creado. El despliegue puede afinar más.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identidad TO svc_identidad;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA identidad TO svc_identidad;
