-- 0004_cambio_identidad_indice.sql — índice de la traza de gobierno (E4-T6).
--
-- La traza `cambio_identidad` responde la pregunta de un incidente: "quién
-- otorgó este acceso y cuándo". Esa consulta es por sujeto afectado y en orden
-- de tiempo descendente (lo más reciente primero), así que necesita un índice
-- por `(sujeto_id, ocurrido_en)` (E4-T6, Qué construir; identidad.md (e)).
--
-- No edita `0001`–`0003`. Un índice hereda los privilegios de la tabla, así que
-- no requiere `GRANT` nuevos (H-13 de E4-T3).

SET search_path TO identidad;

CREATE INDEX IF NOT EXISTS cambio_identidad_sujeto_ts_idx
  ON identidad.cambio_identidad (sujeto_id, ocurrido_en DESC);
