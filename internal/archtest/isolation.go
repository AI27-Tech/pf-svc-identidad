// Package archtest implementa pruebas de aislamiento y arquitectura del módulo
// idp, verificando que NINGÚN código fuera de internal/idp/internal conoce
// detalles de implementación del IdP (en particular, bibliotecas de terceros
// como Keycloak SDK, AWS SDK, etc.) ni enpoints/protocolos internos del
// proveedor.
package archtest

var (
	// ListaDeBibliotecasIdP son los prefijos de importación prohibidos fuera
	// de internal/idp/internal. Estos son los módulos de terceros que implementan
	// adaptadores específicos del IdP y deben estar completamente aislados.
	ListaDeBibliotecasIdP = []string{
		// Keycloak
		"github.com/Nerzal/gocloak",
		// Otros posibles adaptadores aquí, a medida que se agreguen
	}

	// PrefijosAWSV1 son los prefijos de AWS SDK v1 (deprecated).
	PrefijosAWSV1 = []string{
		"github.com/aws/aws-sdk-go",
	}

	// PrefijosAWSV2 son los prefijos de AWS SDK v2.
	PrefijosAWSV2 = []string{
		"github.com/aws/aws-sdk-go-v2",
	}

	// PrefijoDImplementacionIdP es el prefijo permitido donde viven los
	// adaptadores concretos del IdP.
	PrefijoDImplementacionIdP = "github.com/AI27-Tech/pf-svc-identidad/internal/idp/internal"
)

// violations retorna un slice de mensajes de violación detectadas. El argumento
// pkg es el nombre de un paquete (p. ej. "github.com/AI27-Tech/pf-svc-identidad/cmd/api"),
// e imports es la lista de imports de ese paquete.
//
// La regla es:
//   - Cualquier import de ListaDeBibliotecasIdP fuera de PrefijoDImplementacionIdP es violación.
//   - Cualquier import de PrefijosAWSV1 o PrefijosAWSV2 en cualquier paquete es violación.
//
// Retorna un slice vacío si no hay violaciones.
func violations(pkg string, imports []string) []string {
	var viols []string
	isImplementation := hasPrefix(pkg, PrefijoDImplementacionIdP)

	for _, imp := range imports {
		// Regla A: bibliotecas de IdP fuera de la implementación
		if !isImplementation && hasAnyPrefix(imp, ListaDeBibliotecasIdP) {
			viols = append(viols, pkg+" importa "+imp+" (prohibido fuera de internal/idp/internal)")
		}

		// Regla B: AWS en cualquier lado
		if hasAnyPrefix(imp, append(PrefijosAWSV1, PrefijosAWSV2...)) {
			viols = append(viols, pkg+" importa "+imp+" (prohibido en cualquier paquete en V1)")
		}
	}

	return viols
}

// hasPrefix es un ayudante que verifica si s comienza con prefix.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// hasAnyPrefix verifica si s comienza con cualquiera de los prefijos en list.
func hasAnyPrefix(s string, list []string) bool {
	for _, prefix := range list {
		if hasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
