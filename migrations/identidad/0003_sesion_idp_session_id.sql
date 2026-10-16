-- 0003_sesion_idp_session_id.sql — Agregar idp_session_id a la tabla sesion.
--
-- E4-T3 Requisito 2: la tabla `sesion` registra el identificador de sesión del
-- IdP (Keycloak) para que el Paso 2 de revocación (DEC-083 #2, Paso 2) revoque
-- la sesión correcta del IdP, no una genérica. El campo es nullable (una sesión
-- que se emitió sin EmissionFlow configurado no lo tiene); la auditoría de
-- constraints específicas de emisión la cubre Requisito 2.4 (corresponde al
-- claim y a la fila) y las pruebas E4-T3 R9.1–R9.2.
--
-- Por qué separada de 0001: la tabla `sesion` ya está en el tag v0.1.1 (rama
-- `release/v0.1.1`, migración 0001_init.sql) y en producción. Una columna nueva
-- se agrega en migración nueva (0003); 0001 no se reescribe (patrón del Lead,
-- E0-T5, y ADR 17 I2).
--
-- Por qué no hay secuencia de GRANT nuevo: H-13 (design.md) observa que una
-- columna nueva hereda los GRANT de su tabla. sesion ya tiene SELECT, INSERT, UPDATE
-- (0002_grants.sql). No se agrega GRANT especial. H-13 también indica que de haber
-- tabla nueva con la columna, sí habría GRANT nuevo; ese no es este caso.

SET search_path TO identidad;

ALTER TABLE identidad.sesion
  ADD COLUMN idp_session_id TEXT;

-- idp_session_id es nullable: una sesión sin EmissionFlow configurado no lo tiene.
-- H-2 (design.md) observa que memory.Issue devuelve un UUID; en producción puede
-- llegar otro formato (token/session_state de Keycloak en el grant; H-3 sin oráculo
-- todavía). Postgres acepta TEXT para ambos casos.
--
-- No se añade UNIQUE, FOREIGN KEY ni DEFAULT: el campo y su contenido son
-- responsabilidad de EmissionFlow (A-1) cuando se cablee, y de las pruebas de E4-T3
-- R9 (contract test con Keycloak real) cuando el productor esté completo.
