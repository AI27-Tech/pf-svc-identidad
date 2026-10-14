// audit.go implementa el AuditRecorder concreto que escribe la auditoría de
// revocación en `identidad.cambio_identidad` (A-12, A-16; Requisito 5.3).
//
// Decisiones de DEC-088 que esta implementación aplica:
//   - A-12 (actor): el actor sale del contexto HTTP (`auth.ClaimsFromContext`),
//     cuyo `sub` es el id OPACO de Keycloak (H-14, DEC-072). Se resuelve a
//     `sujeto.id` con la costura `actorResolver`, igual que el ResolverSujeto de
//     E4-T6. Si no hay claims en el contexto o el actor no se puede resolver, el
//     `actor_id` queda nulo: la auditoría se escribe igual (no bloquea la
//     revocación), pero el actor es desconocido.
//   - A-12 (motivo): se DIFIERE a V2. En V1 el `motivo` queda nulo (el DELETE de
//     sesión no tiene cuerpo donde transportarlo; brecha de contrato BC-8).
//   - A-16 (tenant): el `tenant_id` sale de la fila del sujeto revocado
//     (`sujeto.tenant_id`, opción a), no del token del actor.
//   - A-5 (sujeto): el `sujeto_id` sale de la fila `sesion` revocada.
package sesion

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// ActorResolver resuelve el actor de una operación a su `sujeto.id` de AI27 a
// partir del contexto de la petición. Es una costura: el `sub` del token es el
// id opaco de Keycloak (DEC-072, H-14) y traducirlo a `sujeto.id` es gobierno de
// usuarios (E4-T6). Devuelve ("", false, nil) cuando el contexto no trae un
// actor identificable; un error solo ante fallo real al resolver.
type ActorResolver func(ctx context.Context) (sujetoID string, ok bool, err error)

// cambioIdentidadRecorder implementa AuditRecorder escribiendo en
// `identidad.cambio_identidad`. La tabla es append-only por privilegio
// (0002_grants.sql: svc_identidad solo tiene SELECT, INSERT), así que este
// recorder nunca hace UPDATE ni DELETE (ADR 26 §7).
type cambioIdentidadRecorder struct {
	actorResolver ActorResolver
}

// NewAuditRecorder construye el AuditRecorder productivo. `actorResolver` puede
// ser nil: en ese caso el actor siempre queda desconocido (actor_id nulo).
func NewAuditRecorder(actorResolver ActorResolver) AuditRecorder {
	return &cambioIdentidadRecorder{actorResolver: actorResolver}
}

// tipoRevocacion es el nombre canónico del hecho de revocación en la traza.
// Coincide con el tipo del evento `sesion.revocada` (registro.tipos.yaml).
const tipoRevocacion = "sesion.revocada"

// RecordRevocation inserta una fila en `cambio_identidad` dentro de la tx del
// Paso 1 (misma transacción que la marca de revocación y el outbox, K-6). El
// payload del evento es mínimo; el detalle de la revocación vive AQUÍ (ADR 26 §7).
func (r *cambioIdentidadRecorder) RecordRevocation(ctx context.Context, tx pgx.Tx, a RevocationAudit) error {
	// El actor sale del contexto (A-12). Si no se puede resolver, queda nulo:
	// la auditoría registra la revocación aunque el actor sea desconocido.
	actorID := a.ActorID
	if actorID == nil && r.actorResolver != nil {
		if sujetoID, ok, err := r.actorResolver(ctx); err == nil && ok && sujetoID != "" {
			actorID = &sujetoID
		}
	}

	// Columnas nulables (sujeto_id, tenant_id, actor_id, motivo son UUID/TEXT
	// que admiten NULL). Se pasan punteros para que un valor vacío sea NULL y no
	// una cadena vacía que rompería el tipo UUID.
	sujetoID := nilSiVacio(a.SujetoID)

	_, err := tx.Exec(ctx,
		`INSERT INTO identidad.cambio_identidad (tipo, sujeto_id, tenant_id, actor_id, sesion_id, motivo)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		tipoRevocacion, sujetoID, a.TenantID, actorID, a.SesionID, a.Motivo,
	)
	if err != nil {
		return err
	}
	return nil
}

// nilSiVacio devuelve nil para una cadena vacía, y el puntero al valor en otro
// caso. Evita bindear "" a una columna UUID (Postgres: 22P02).
func nilSiVacio(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
