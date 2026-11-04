package gobierno

import (
	"context"
	"fmt"

	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
	"github.com/AI27-Tech/pf-svc-identidad/internal/sesion"
)

// ResolverSujeto implementa sesion.SujetoResolver (Requisito 11, DEC-072). Es
// el habilitador del login de E4-T3: resuelve el `sub` OPACO del IdP al
// `SujetoID` de AI27 consultando el `sujeto` por su `idp_subject`. No hay claim
// `sujeto_id` nuevo ni alineación de identificadores (DEC-072 opción c), ni
// cambios en pf-pkg-platform-go.
//
// Vive en el paquete gobierno porque la correlación `IdpSubject → SujetoID` es
// gobierno de usuarios: DEC-072 la dejó explícitamente "con el alta/
// sincronización de usuarios de E4-T6".
type ResolverSujeto struct {
	sujetos SujetoRepoLectura
	// subDeToken obtiene el `sub` opaco a partir del token emitido. idp.Token no
	// lo expone directamente (solo trae access/refresh/expira/idp_session_id):
	// el `sub` viaja dentro del AccessToken como claim. Esta costura se cierra al
	// cablear el login de E4-T3 (ver nota de diseño del design.md): puede
	// exponerse en idp.Token o leerse verificando el AccessToken con
	// pf-pkg-platform-go/auth. Mientras tanto, el resolver no asume una forma.
	subDeToken func(tok idp.Token) (string, error)
}

// SujetoRepoLectura es la parte de lectura del repo de sujetos que el resolver
// necesita. Interfaz en el consumidor.
type SujetoRepoLectura interface {
	PorIdpSubject(ctx context.Context, idpSubject string) (autorizacion.Sujeto, bool, error)
}

// NewResolverSujeto construye el resolver. `subDeToken` es la costura que
// obtiene el `sub` del token; si es nil, ResolveSujeto devuelve un error claro
// en vez de resolver contra un `sub` vacío.
func NewResolverSujeto(sujetos SujetoRepoLectura, subDeToken func(idp.Token) (string, error)) *ResolverSujeto {
	return &ResolverSujeto{sujetos: sujetos, subDeToken: subDeToken}
}

var _ sesion.SujetoResolver = (*ResolverSujeto)(nil)

// ResolveSujeto devuelve el SujetoID y el TenantID del sujeto cuyo idp_subject
// coincide con el `sub` del token. Devuelve sesion.ErrSujetoUnresolved si no
// existe tal sujeto o si está desactivado (G-6): un sujeto inactivo no resuelve,
// así no puede volver a iniciar sesión.
func (r *ResolverSujeto) ResolveSujeto(ctx context.Context, tok idp.Token) (sesion.SujetoRef, error) {
	if r.subDeToken == nil {
		return sesion.SujetoRef{}, fmt.Errorf("resolver sujeto: costura subDeToken no configurada")
	}
	sub, err := r.subDeToken(tok)
	if err != nil {
		return sesion.SujetoRef{}, fmt.Errorf("resolver sujeto: obtener sub del token: %w", err)
	}
	s, ok, err := r.sujetos.PorIdpSubject(ctx, sub)
	if err != nil {
		return sesion.SujetoRef{}, fmt.Errorf("resolver sujeto: %w", err)
	}
	if !ok || !s.Activo {
		return sesion.SujetoRef{}, sesion.ErrSujetoUnresolved
	}
	return sesion.SujetoRef{SujetoID: s.ID, TenantID: s.TenantID}, nil
}
