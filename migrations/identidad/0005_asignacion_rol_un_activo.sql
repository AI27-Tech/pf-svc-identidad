-- 0005_asignacion_rol_un_activo.sql — un solo rol activo por sujeto (E4-T6).
--
-- El token lleva EXACTAMENTE un `rol` (DEC-053) y la composición de claims
-- rechaza cero o varios (`ErrRolNoUnico`). Por eso el modelo debe garantizar a
-- lo sumo UNA asignación de rol activa por sujeto (C2/G-5, DEC-089 propuesta).
--
-- Este índice único parcial es MÁS ESTRICTO que único por `(sujeto_id,
-- rol_codigo)`: no basta con que no se repita el mismo rol, sino que no puede
-- haber dos roles activos distintos. Cambiar de rol es un REEMPLAZO (revocar el
-- anterior y asignar el nuevo en la misma transacción), nunca dos activos a la
-- vez.
--
-- Reemplaza la validación en código de `AsignacionRolRepo.Asignar` (la antigua
-- "Decisión B" que leía `ActivasDe` antes de insertar): a partir de aquí la
-- unicidad la garantiza la base de datos. Una violación emerge como `23505` y
-- el repo la traduce a un error de dominio.
--
-- No edita `0001`–`0004`. Un índice no requiere `GRANT` nuevos.

SET search_path TO identidad;

CREATE UNIQUE INDEX IF NOT EXISTS asignacion_rol_un_activo_idx
  ON identidad.asignacion_rol (sujeto_id) WHERE revocado_en IS NULL;
