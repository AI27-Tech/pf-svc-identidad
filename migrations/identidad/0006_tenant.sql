-- 0006_tenant.sql — tabla de tenants del esquema `identidad` (E4-T6).
--
-- Hasta ahora `sujeto.tenant_id` era un UUID sin tabla destino. El gobierno de
-- usuarios necesita que el tenant exista como entidad: el tenant de cada
-- cliente lo determina su organización (E4-HU-018) y los siete roles internos
-- comparten un tenant AI27 único (C3/G-9, DEC-092 propuesta). El tenant vive en
-- `identidad`, no en `catalogos`: catálogos es dueño del expediente comercial
-- del cliente, no de su tenant (identidad.md; refinación 2026-09-08).
--
-- Esta migración crea SOLO la ESTRUCTURA (tabla, FK, grants). La SEMILLA del
-- tenant interno con su UUID real es dato de Arquitectura y vive en una
-- migración aparte (`0007_tenant_seed.sql`), que no se aplica hasta tenerlo.
--
-- La FK se agrega como NOT VALID y luego se valida: así no falla si ya hay
-- filas de `sujeto` con un `tenant_id` que todavía no está en `tenant`. En un
-- despliegue nuevo la tabla está vacía y la validación es inmediata; en uno con
-- datos, la semilla debe poblar los tenants referenciados antes de validar.
-- Cada objeto nuevo declara sus GRANT y suma su caso en grants_integration_test.go.

SET search_path TO identidad;

-- -----------------------------------------------------------------------------
-- tenant — espacio de pertenencia de un `sujeto`. `tipo` distingue el tenant
-- interno de AI27 (uno) de los tenants de cliente (uno por organización).
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant (
  id          UUID        PRIMARY KEY,
  nombre      TEXT        NOT NULL,
  tipo        TEXT        NOT NULL CHECK (tipo IN ('interno', 'cliente')),
  creado_en   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Integridad referencial de `sujeto.tenant_id` hacia la tabla nueva. NOT VALID
-- evita que la migración falle con datos previos; la validación va aparte.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'sujeto_tenant_fk'
  ) THEN
    ALTER TABLE sujeto
      ADD CONSTRAINT sujeto_tenant_fk
      FOREIGN KEY (tenant_id) REFERENCES tenant (id) NOT VALID;
  END IF;
END
$$;

-- svc_identidad lee tenants (al resolver el sujeto y al validar el alta) e
-- inserta (al dar de alta un tenant de cliente). No borra: un tenant referido
-- por sujetos y auditoría no se elimina. SELECT, INSERT.
GRANT SELECT, INSERT ON identidad.tenant TO svc_identidad;
