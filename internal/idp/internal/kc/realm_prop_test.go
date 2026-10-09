package kc

import (
	"sort"
	"testing"

	"github.com/AI27-Tech/pf-pkg-contracts/packages/api-client-go/roles"
	"github.com/AI27-Tech/pf-svc-identidad/internal/autorizacion"
)

// Feature: e4-t1-adaptador-idp-keycloak, Property 7: Coherencia del artefacto del realm con el modelo
//
// El dominio de esta propiedad es finito (el artefacto versionado), así que se
// recorre por completo en vez de generar casos aleatorios.
func TestPropiedadCoherenciaRealm(t *testing.T) {
	r := cargarRealm(t)

	// 1. El conjunto de nombres de grupo es igual al de los ocho Rol del
	//    contrato, sin auditor.
	nombresGrupo := make([]string, 0, len(r.Groups))
	for _, g := range r.Groups {
		nombresGrupo = append(nombresGrupo, g.Name)
	}
	esperados := make([]string, 0, len(roles.Todos))
	for _, rol := range roles.Todos {
		esperados = append(esperados, string(rol))
	}
	sort.Strings(nombresGrupo)
	sort.Strings(esperados)
	if len(nombresGrupo) != len(esperados) {
		t.Fatalf("el realm tiene %d grupos; se esperaban %d (los ocho roles de DEC-053)", len(nombresGrupo), len(esperados))
	}
	for i := range esperados {
		if nombresGrupo[i] != esperados[i] {
			t.Fatalf("conjunto de grupos %v != roles del contrato %v", nombresGrupo, esperados)
		}
	}
	for _, nombre := range nombresGrupo {
		if nombre == "auditor" {
			t.Error("el realm no debe tener un grupo auditor (R5.3)")
		}
	}

	// 2. Cada grupo tiene exactamente un valor en rol (igual a su nombre) y uno
	//    en audiencia (igual a AudienciaDeRol(rol)).
	for _, g := range r.Groups {
		rolAttr := g.Attributes["rol"]
		if len(rolAttr) != 1 {
			t.Errorf("grupo %q: atributo rol = %v; se esperaba exactamente un valor", g.Name, rolAttr)
			continue
		}
		if rolAttr[0] != g.Name {
			t.Errorf("grupo %q: rol = %q; debe ser igual al nombre del grupo", g.Name, rolAttr[0])
		}
		audAttr := g.Attributes["audiencia"]
		if len(audAttr) != 1 {
			t.Errorf("grupo %q: atributo audiencia = %v; se esperaba exactamente un valor", g.Name, audAttr)
			continue
		}
		esperada := string(autorizacion.AudienciaDeRol(roles.Rol(g.Name)))
		if audAttr[0] != esperada {
			t.Errorf("grupo %q: audiencia = %q; se esperaba %q (AudienciaDeRol)", g.Name, audAttr[0], esperada)
		}
	}

	// 3. Cada mapper del scope ai27-claims que proyecta un claim de dominio
	//    usa un claim.name permitido y multivalued=false. El audience mapper
	//    (ai27-internal) no lleva claim.name y queda fuera de esta regla.
	claimsPermitidos := map[string]bool{
		"tenant_id": true,
		"rol":       true,
		"audiencia": true,
		"sesion_id": true,
	}
	var scope *realmScope
	for i := range r.ClientScopes {
		if r.ClientScopes[i].Name == "ai27-claims" {
			scope = &r.ClientScopes[i]
			break
		}
	}
	if scope == nil {
		t.Fatal("no existe el client scope ai27-claims")
	}
	for _, m := range scope.ProtocolMappers {
		nombreClaim, tieneClaim := m.Config["claim.name"]
		if !tieneClaim {
			// Mapper sin claim.name (p. ej. el audience mapper). No aplica.
			continue
		}
		if !claimsPermitidos[nombreClaim] {
			t.Errorf("mapper %q proyecta claim.name %q, fuera del conjunto permitido", m.Name, nombreClaim)
		}
		if m.Config["multivalued"] != "false" {
			t.Errorf("mapper %q tiene multivalued=%q; se esperaba false", m.Name, m.Config["multivalued"])
		}
	}

	// 4. Todo campo secret del artefacto es un marcador ${...}.
	for _, c := range r.Clients {
		if c.Secret != "" && !esMarcador(c.Secret) {
			t.Errorf("cliente %q tiene secret literal %q; debe ser marcador ${...} (R5.10)", c.ClientID, c.Secret)
		}
	}
}
