// expiry.go implementa la ExpiryPolicy productiva (A-3, opción b aprobada en
// DEC-088: actualizar expira_en en cada refresco).
//
// Por qué la opción b: si la fila guardara solo la expiración de la primera
// emisión y nunca se actualizara (opción a), una sesión refrescada varias veces
// seguiría viva en el IdP mucho después de que `expira_en` dijera que expiró.
// El snapshot de tombstones deja de listar una revocación cuando su `expira_en`
// pasó, así que ese desfase abriría una ventana en la que un token refrescado
// del atacante sigue vigente pero el snapshot ya no lo reporta. Actualizar
// `expira_en` en cada refresco cierra esa ventana.
package sesion

import (
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// expiryAtRefresh implementa ExpiryPolicy con la opción b de A-3.
//   - AtIssue: la expiración registrada es la del token recién emitido.
//   - AtRefresh: la mayor entre la expiración actual de la fila y la del token
//     refrescado. Nunca retrocede la expiración (un refresco solo puede
//     extenderla o mantenerla), lo que evita que un token viejo acorte la
//     vigencia registrada de la sesión.
type expiryAtRefresh struct{}

// NewExpiryAtRefresh devuelve la política de expiración productiva (A-3, b).
func NewExpiryAtRefresh() ExpiryPolicy {
	return expiryAtRefresh{}
}

// AtIssue registra la expiración del token emitido.
func (expiryAtRefresh) AtIssue(tok idp.Token) (time.Time, error) {
	return tok.ExpiraEn, nil
}

// AtRefresh devuelve max(current, tok.ExpiraEn): el refresco extiende la
// expiración, nunca la acorta.
func (expiryAtRefresh) AtRefresh(current time.Time, tok idp.Token) (time.Time, error) {
	if tok.ExpiraEn.After(current) {
		return tok.ExpiraEn, nil
	}
	return current, nil
}
