# DECISIONES PENDIENTES — E4-T3
## Registro de sesión, evento y snapshot de revocación

**Preparado para:** Product Manager, Tech Lead  
**Spec:** E4-T3 (identidad, lado productor)  
**Fecha:** 2026-10-12  
**Estado:** 12 decisiones bloqueantes | Parte A lista para ejecutar

---

## Resumen ejecutivo

**E4-T3** es el trabajo que hace de AI27 la autoridad durable de las sesiones de usuario. Hoy identidad revoca sesiones localmente, pero no genera su propio identificador (lo genera el IdP), no lo registra en la base de datos, y no publica el evento de revocación.

Con E4-T3:
- AI27 genera y controla el sesion_id propio (antes del IdP)
- Se registra en Postgres cuál es el idp_session_id de cada sesión (para revocar bien en el IdP)
- La revocación se publica como evento para que otros módulos se entren en tiempo real
- Las lecturas de "¿está revocada?" van directo a Postgres (fallback seguro)

**Lo urgente:** El spec está 100% completo (diseño, requisitos y plan). **34 tareas no dependen de ninguna decisión y se pueden ejecutar ahora** (Parte A). Las 68 restantes (Parte B) están bloqueadas por **12 decisiones arquitectónicas** que solo el PM puede tomar.

**4 decisiones son críticas para V1:**
- **A-1 (DEC-085):** ¿Qué hacer si el IdP emite el token pero falla el registro en Postgres? Dos opciones opuestas en seguridad/UX.
- **A-3:** ¿Qué fecha de expiración se registra? ¿La del primer token o se actualiza en cada refresco? Una mala elección deja hueco de seguridad.
- **A-2:** ¿Puede un sujeto tener varias sesiones vivas? El índice de Postgres lo prohíbe hoy.
- **A-15:** ¿Se transporta el segundo factor (OTP) en la emisión? Sin esto, las pruebas contra Keycloak se atascan.

---

## 12 DECISIONES BLOQUEANTES

### A-1 — Orden de emisión y registro (DEC-085)

**Problema:** Cuando AI27 pide al IdP que emita un token y luego intenta registrar la sesión en Postgres, hay un punto de falla: si el IdP emite pero Postgres rechaza, ¿qué hace AI27? El usuario tiene token válido pero la sesión no está en la BD. Dos opciones: devolver error (seguro, mala UX) o tragarse el error (UX ok, peligroso: token sin sesión).

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| 1 | Emisión primero, compensar si falla | IdP emite → Si registro falla, revocar en IdP y error al usuario. **Seguro.** UX: "intente más tarde". Requiere reversión en IdP |
| 2 | Registro primero | AI27 valida, genera sesión → Luego pide token. IdP rechaza pero Postgres ya tiene sesión viva sin token. Complejo |
| 3 | Sin decisión | Quedarse con hoy: sin reintento. **Deja hueco conocido** |

**Recomendación:** Opción 1. Es estándar en arquitecturas distribuidas: intenta flujo feliz, si falla paso 2, deshaz paso 1. Costo: revocación extra si falla Postgres (rara).

**Bloqueador de:**
- B1.1 — Cablear EmissionFlow
- B1.2 — Prueba de compensación
- B1.3 — Handler del error (504/500)

**Plazo:** **Antes del viernes 2026-10-18.** Es el bloqueador más crítico; retrasa 6+ tareas de emisión.

---

### A-3 — Expiración de la sesión en Postgres

**Problema:** Cada token tiene expira_en. Al registrar la sesión, ¿qué fecha se guarda? ¿La del primer token o se actualiza en cada refresco? Una mala elección deja hueco: si registras la fecha inicial, un token refrescado 10 veces sobrevive 50 min en total, pero Postgres dice expiró a los 5. El snapshot de revocaciones expira toda revocación cuya sesión haya expirado, así que podría mostrar "revocada" cuando un token del atacante sigue vivo.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Primera emisión, nunca actualizar | Simple. **Hueco de seguridad:** refresh de 50 min en Postgres muestra "expirada" a los 5 |
| b | Actualizar en cada refresco | Seguro. Costo: más escrituras en Postgres (1k writes/min con 5k sesiones). Necesita A-4 decidida |
| c | Duración máxima de sesión (DEC-078) | Futuro; requiere ampliar IdP. No resuelve V1 |

**Recomendación:** Opción b (actualizar en cada refresco). El hueco de seguridad de (a) es inaceptable. El costo de escritura es manejable.

**Bloqueador de:**
- B2.1 — Cablear ExpiryPolicy.AtRefresh()
- B2.2 — Prueba de expiración en refresco
- B2.3 — Métrica de carga Postgres

**Plazo:** **Antes del 2026-10-18.** Afecta emisión y refresh.

---

### A-2 — Sesiones múltiples por sujeto

**Problema:** Hoy el índice una_sesion_viva permite máximo una sesión viva por sujeto. Esto es seguro pero restrictivo: si un usuario abre dos navegadores, una de las sesiones falla. Las historias de usuario y DEC-053 lo permiten. Cambiarlo cuesta migración de índice.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Conservar 1 sesión viva por sujeto | Seguro. UX: usuario solo puede estar en un lugar. Contradice HU. Compatible con A-1 opción 2 |
| b | Permitir N sesiones vivas por sujeto | Alerta de seguridad en logout (requiere A-9). Índice se borra. Requiere migración |
| c | Por ahora (a), decidir (b) en E4-T9 | Aplaza decisión. Ruta crítica: V1 sin multi-sesión |

**Recomendación:** Opción c (por ahora conservar 1). E4-T3 cierra igual con (a). Multi-sesión es un refinamiento; no es bloqueador de V1.

**Bloqueador de:**
- B5.1 — Pruebas de logout con múltiples sesiones
- B5.2 — Propiedad de aislamiento: "revocación solo toca su sesion_id"

**Plazo:** **Antes del 2026-10-20.** Afecta pruebas de E4-T3; decisión de arquitectura para E4-T9.

---

### A-15 — Segundo factor en la ruta de emisión

**Problema:** El contrato actual transporta usuario y contrasena en la emisión, pero no segundo_factor (OTP). El puerto interno del IdP ya soporta Credentials.SegundoFactor, pero el handler HTTP no lo puede llenar porque el contrato no lo tiene. Sin esto, las pruebas contra Keycloak (que exige OTP) se atascan.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Ampliar contrato para transportar OTP | Se agrega campo en CredencialesEntrada. Handler HTTP lo llena. Pruebas con Keycloak andan. **Costo:** cambio en contrato (scope V2 potencial) |
| b | Sin segundo factor en V1 | Pruebas contra Keycloak usan un workaround (p. ej. usuaria pre-registrada sin OTP). Handler HTTP no toca nada. Más lento pero funciona |
| c | OTP por header o contexto | No estándar. Peligroso: expone credencial en logs |

**Recomendación:** Opción b por ahora. Ampliar contrato (a) es scope para V2. Las pruebas de sesiontest.HappyPathEmission usan la opción (b) hoy.

**Bloqueador de:**
- B1.3 — Pruebas de emisión contra Keycloak real
- R9.1 — Correspondencia sesion_id ↔ idp_session_id en prueba

**Plazo:** **Antes del 2026-10-20.** Afecta solo pruebas; no bloquea código.

---

### A-4 — Refresh de sesión revocada

**Problema:** ¿Qué pasa si un usuario intenta refrescar un token de una sesión que AI27 ya revocó? Dos opciones: rechazar (seguro) o dejar que el IdP decida (delega la decisión). Sin decidir, no se cablea el refresco.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| Reject | Identidad rechaza el refresco de sesión revocada | Seguro. UX: usuario debe re-autenticarse |
| DelegateToIdP | Identidad deja que el IdP resuelva | IdP puede tener la sesión aún activa; emite token nuevo. Menos robusto |
| Undecided | Sin cablear refresco en V1 | Bloquea código del refresco. No se ejecuta |

**Recomendación:** Reject. Es más seguro y coherente con "revocación local es la verdad". A-3 (actualizar expiración) la facilita.

**Bloqueador de:**
- B3.1 — Cablear RefreshResolver
- B3.2 — Lógica de rechazo en refresco
- B3.3 — Pruebas de refresco revocado

**Plazo:** **Antes del 2026-10-20.** Afecta refresco; menos crítico que A-1 y A-3.

---

### A-5 y A-16 — Sujeto e identidad del tenant

**Problema:** Cuando se emite una sesión, ¿cómo se obtiene el sujeto_id? El token trae un sub del IdP, pero no es el sujeto_id de AI27 (son sistemas separados, DEC-072). Tampoco se conoce el tenant (A-16) a menos que se resuelva explícitamente. Sin eso, no se puede registrar la fila.

**Opciones:**

| # | Opción | Descripción | Impacto |
|---|--------|-------------|--------|
| a | Resolver sub → sujeto_id en identidad | Identidad consulta sujetos (E4-T6), obtiene sujeto_id y 	enant_id | Normal; requiere puerto SujetoResolver |
| b | El IdP transporta sujeto_id en claim | Cambia mappers de Keycloak; requiere E4-T1 | Más rápido; retrasa a E4-T1 |
| c | Sin resolver; usar sub como sujeto_id | Rompe modelo: sub de Keycloak ≠ sujeto.id de AI27. **No válido** |

**Recomendación:** Opción a (resolver en identidad). Necesita E4-T6; por ahora se deja como costura sin implementar productiva.

**Bloqueador de:**
- B2.1 — Cablear SujetoResolver
- B6.1 — Auditoría: registrar sujeto_id y 	enant_id

**Plazo:** **Antes del 2026-10-20.** Afecta emisión y auditoría.

---

### A-7 — Reintento y respuesta HTTP del Paso 2

**Problema:** Si el Paso 2 falla (IdP no responde, red caída), ¿qué retorna al usuario? ¿Se reintenta automáticamente? El diseño marca que la revocación es "reintentable" pero no define la estrategia (exponencial, lineal, máximo intentos).

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| Handler implementa reintento local | IdpRevokeFailureHandler embebido en identidad reintenta N veces | Contiene la lógica de reintento en un lugar |
| Reintento externo (retry en API Gateway o sidekick) | Identidad retorna 503 sesion_revocada_parcialmente. Externo reintenta | Desacoplado; requiere nuevo estado HTTP |
| Sin reintento; solo logging | Identidad retorna el error (hoy 501). Operador revoca manual | Incompleto; hueco operacional |

**Recomendación:** Handler implementa reintento local con exponencial (1s, 2s, 4s, máx 3 intentos). Es predecible y encapsulado.

**Bloqueador de:**
- B4.1 — Cablear IdpRevokeFailureHandler
- B4.2 — Lógica de reintento exponencial
- B4.3 — Estado HTTP 503 (no 501) si sigue fallando

**Plazo:** **Antes del 2026-10-20.** Afecta revocación; menos crítico que emisión.

---

### A-10 — Semántica del cursor en snapshot

**Problema:** El snapshot devuelve un cursor. ¿Qué significa? ¿Es una marca de tiempo, una secuencia, un índice de fila? Si es marca de tiempo, hay huecos con transacciones concurrentes (el SQL 
ow() es inicio de tx, no commit). Sin definir, no se puede implementar correctamente.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Cursor = snapshot de Postgres (xid) | Preciso; requiere SQL avanzado y decodificación. Costo: lógica compleja |
| b | Cursor = timestamp (momento del snapshot) | Simple; fácil. Riesgo: huecos pequeños con Tx concurrentes |
| c | Sin cursor; snapshot siempre completo | Nulo; consumidor no puede continuar sin ventana. No satisface ADR 26 |

**Recomendación:** Opción b por ahora (timestamp). Es simple y los huecos son micrométricos (< 1ms típico). A-10 es costura; se puede mejorar luego si volumen de revocaciones es alto.

**Bloqueador de:**
- B7.1 — Cablear RevocationLog.Since(cursor)
- B7.2 — Prueba de continuación sin ventana
- B7.3 — Métrica de hueco temporal

**Plazo:** **Antes del 2026-10-22.** Afecta snapshot; menos crítico.

---

### A-12 — Actor y motivo de la auditoría local

**Problema:** La auditoría de revocación debe registrar quién revocó y por qué. Pero el puerto actual de RevocarSesion no recibe ninguno de esos datos. ¿De dónde sacará identidad el actor (usuario que revocó) y el motivo?

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Del contexto HTTP (claims del token del revocador) | Identidad extrae del ClaimsFromContext. Requiere que revocador sea usuario autenticado |
| b | Header personalizado (p. ej. X-Revocation-Reason) | Externo llena el header; identidad lo lee. Flexible; requiere coordinación |
| c | Sin actor/motivo en V1; auditoría vacía | Incompleto; requisito de seguridad no se cumple |

**Recomendación:** Opción a (del contexto). Es estándar para "quién lo hizo"; el motivo se deja para V2 (opcional por ahora).

**Bloqueador de:**
- B6.1 — Cablear AuditRecorder
- B6.2 — Extraer actor del contexto
- B6.3 — Prueba de auditoría con actor

**Plazo:** **Antes del 2026-10-22.** Afecta auditoría; requisito de seguridad.

---

### A-14 — Serialización del payload del evento

**Problema:** El evento sesion.revocada lleva {sesion_id, revocada_en, expira_en}. ¿En qué formato se serializa? ¿JSON plano (map[string]any), protobuf (protojson), otro? El diseño propone dos opciones; sin elegir, se usa la de hoy (JSON plano).

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | JSON plano (map[string]any) | Comportamiento de hoy. Simple. Sin schema fuerte |
| b | Protobuf (protojson con UseProtoNames: true) | Schema fuerte; versioning nativo. Costo: google.golang.org/protobuf en dependencias |
| c | Sin cambio; usar opción (a) | Compatible con hoy. Sin ventajas |

**Recomendación:** Opción a por ahora (JSON plano). Es lo que funciona hoy. La migración a protobuf (b) es futura.

**Bloqueador de:**
- B8.1 — Cablear PayloadEncoder
- B8.2 — Contract test del payload

**Plazo:** **Antes del 2026-10-22.** Afecta serialización; no bloquea ruta crítica.

---

### A-11 — Tecnología de caché de sesión

**Problema:** El registro caliente sesion:{sesion_id} debe guardarse en caché (Redis/Valkey según identidad.md). Pero go.mod no tiene cliente. ¿Se agrega Redis, se espera a E4-T10, o se ignora la caché en V1?

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Agregar cliente Redis a go.mod; cablear en V1 | Completo desde V1. Costo: dependencia nueva; infra Redis. Mejora perf lectura |
| b | Sin caché en V1; identidad.New pasa cache: nil | Va todo a Postgres (más lento). Funcional. Migrará a caché cuando esté infra |
| c | Dejar costura sin cablear; usar la opción (b) | Igual a (b); código está listo para caché luego |

**Recomendación:** Opción c (no agregar a V1). E4-T10 o V2 lo cablean. Por ahora cache: nil funciona; Postgres es suficiente para el volumen inicial.

**Bloqueador de:**
- B9.1 — Cablear WithCacheTimeout
- B9.2 — Adaptador de Redis (futuro)
- B9.3 — Métrica de hit/miss caché

**Plazo:** **Antes del 2026-10-24.** Afecta caché; no es crítico para V1.

---

### A-9 — Autorización: quién puede revocar qué

**Problema:** ¿Quién puede revocar una sesión? ¿Solo el usuario de esa sesión (logout)? ¿Un admin de ese tenant? ¿Cualquiera autenticado? Sin decidir, no hay guardrail.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Solo el usuario de la sesión (logout propio) | Seguro. Revocación en DELETE /sesiones/{id}. Usuario solo toca su propia sesión |
| b | User + admin del tenant + operador | Flexibilidad; requiere RBAC en identidad. Más complejo |
| c | Sin guardrail; cualquiera autenticado puede revocar | **NO válido** |

**Recomendación:** Opción a (solo el usuario). Es seguro y coherente con E4-T3. RBAC es futuro (E4-T8).

**Bloqueador de:**
- B10.1 — Validación en handler: "el usuario del token = el usuario de la sesión"
- B10.2 — Prueba de autorización

**Plazo:** **Antes del 2026-10-24.** Afecta seguridad; requisito de E4-T3.

---

### A-6 — Revocación masiva de sesiones

**Problema:** El "Qué construir" de E4-T3 menciona revocación administrativa masiva. ¿Existe esa operación? ¿Quién la invoca? ¿Cómo se transporta (nuevo endpoint, evento admin)? Sin decisión, no hay criterio.

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | Nueva operación POST /api/v1/sesiones/revocar-por-sujeto | Requiere: contrato nuevo, handler nuevo, operación en identidad.yaml. Costo: scope |
| b | Evento admin externo | Otro módulo (Operación, Seguridad) publica evento sujeto.revocado; identidad lo consume | Desacoplado; requiere orquestación |
| c | Sin revocación masiva en V1 | E0-T4 la puede hacer si necesita (múltiples revocaciones individuales) | Simple; incompleto |

**Recomendación:** Opción c (no en V1). La revocación masiva es un refinamiento. E0-T4 puede hacer N revocaciones individuales si necesita. E4-T8 puede especializarla.

**Bloqueador de:**
- B11.1 — Operación en contrato
- B11.2 — Handler RevocarPorSujeto
- B11.3 — Prueba de idempotencia

**Plazo:** **Antes del 2026-10-24.** No es crítico; aplaza a E4-T8.

---

### A-8 — Respuesta HTTP de sesión inexistente

**Problema:** ¿Qué retorna DELETE /sesiones/{id} si la sesión no existe? ¿204 (comportamiento actual, "ya revocada")? ¿404 (no encontrada)? La elección afecta cómo el cliente interpreta "la revocación sucedió" vs. "no hay nada que revocar".

**Opciones:**

| Opción | Descripción | Impacto |
|--------|-------------|--------|
| a | 204 No Content (comportamiento actual) | Compatible con hoy. Semántica: "no hay más para hacer" |
| b | 404 Not Found | Semántica explícita: "esa sesión no existe". Cliente distingue "revocada" de "nunca existió" |
| c | 200 OK con cuerpo indicando "no encontrada" | Híbrido; no es HTTP estándar |

**Recomendación:** Opción a (seguir con 204). Es idempotente e intuitivo: revocar algo que no existe es un no-op. A-8 es una brecha conocida del contrato; no bloquea E4-T3.

**Bloqueador de:**
- B12.1 — Mantener comportamiento actual (204)
- B12.2 — Documentar que no es 404

**Plazo:** **Antes del 2026-10-24.** No es crítico; es una clarificación.

---

## PRÓXIMOS PASOS

### ✅ Ahora: Parte A (sin decisiones pendientes)

**34 tareas están listas para ejecutar:**

1. Migración  003 y errores de dominio
2. Costuras y opciones con valores por omisión
3. Corrección de H-1 y del Paso 1 (ruta productiva)
4. estEffortCache
5. internal/sesion/sesiontest y regla E
6. Pruebas unitarias y de ejemplo (7 tareas)
7. Pruebas de propiedad (8 tareas)
8. Cierre documental y verificación final

**Estas tareas:**
- Corrigen lo roto hoy (H-1: Paso 2 recibe idp_session_id de la fila, no sesion_id)
- Abren las costuras para que cada decisión pendiente se enchufe sin cambiar el resto
- Permiten pruebas ejecutables sin Keycloak
- Alcanzan ~70% de cobertura de requisitos

**Tiempo estimado:** 8–10 días (1–2 desarrolladores).

### ⏳ Cuando se resuelvan los pendientes: Parte B

Cada grupo de tareas de la Parte B se desbloquea cuando su decisión se resuelve:

- **A-1 decisiva → B1:** emisión con compensación (2–3 tareas)
- **A-3 decisiva → B2:** expiración en refresco (1–2 tareas)
- **A-4 decisiva → B3:** rechazo de refresh revocado (2 tareas)
- **A-5/A-16 decisivas → B6:** auditoría con sujeto/tenant (2–3 tareas)
- **A-7 decisiva → B4:** reintento del Paso 2 (1–2 tareas)
- **A-10 decisiva → B7:** cursor en snapshot (2–3 tareas)
- **A-12 decisiva → B8:** actor en auditoría (1–2 tareas)
- **A-14 decisiva → B9:** serialización del payload (1 tarea)
- **A-15 (diseño) → R9:** pruebas de emisión con Keycloak (3–4 tareas)
- **A-2/A-9/A-11/A-6/A-8:** recomendaciones, poco código (1–2 tareas cada una)

**Total Parte B:** 20–25 tareas, 5–7 días una vez decididas.

### 🎯 E4-T3 NO cierra sin E0-T4

Recordar: el consumidor (suscripción durable, broadcast a réplicas, 
ot-ready, fallback a 15s, corte a 30s) es **E0-T4**. E4-T3 solo es el lado productor. Ambas tareas de estas dos historias técnicas corren en paralelo.

---

## MATRIZ DE DECISIONES

| # | Decisión | Bloques | Urgencia | Recomendación |
|---|----------|---------|----------|---------------|
| A-1 | Orden emisión↔registro | B1 (6 tareas) | **CRÍTICA** | Opción 1 (compensar) |
| A-3 | Expiración sesión | B2 (2 tareas) + A-4 | **CRÍTICA** | Opción b (actualizar) |
| A-2 | Multi-sesión | B5 (2 tareas) | Media | Opción c (posponer) |
| A-15 | Segundo factor | R9 (4 tareas) | Media | Opción b (workaround) |
| A-4 | Refresh revocado | B3 (2 tareas) | Media | Reject |
| A-5/A-16 | Sujeto/tenant | B6 (3 tareas) | Media | Opción a (resolver) |
| A-7 | Reintento Paso 2 | B4 (2 tareas) | Baja | Handler local |
| A-10 | Cursor snapshot | B7 (3 tareas) | Baja | Opción b (timestamp) |
| A-12 | Actor auditoría | B8 (2 tareas) | Baja | Del contexto |
| A-14 | Serialización | B9 (1 tarea) | Baja | Opción a (JSON) |
| A-11 | Caché Redis | B10 (2 tareas) | Baja | Posponer a V2 |
| A-9 | Autorización revoca | B11 (2 tareas) | Media | Solo usuario |
| A-6 | Revocación masiva | B12 (3 tareas) | Baja | Posponer a E4-T8 |
| A-8 | 204 vs 404 | Brecha conocida | Baja | Mantener 204 |

