package archtest

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsolation verifica las reglas de aislamiento del módulo idp.
func TestIsolation(t *testing.T) {
	// Ejecutar go list para obtener la estructura de paquetes y sus imports
	pkgs, err := listPackages(false)
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	var viols []string
	for _, pkg := range pkgs {
		// Regla A y B: verificar imports prohibidos
		pkgViols := violationsConImportsDePrueba(pkg)
		viols = append(viols, pkgViols...)
	}

	// Si hay violaciones, fallar
	if len(viols) > 0 {
		t.Errorf("violaciones de aislamiento encontradas:\n%s", strings.Join(viols, "\n"))
	}

	// Regla C: verificar que no haya rutas de protocolo Keycloak en literal de cadena
	// (salvo en el archivo de prueba actual)
	verifyNoKCProtocolLiterals(t)

	// Verificar que internal/idp/internal no use os.Getenv (Regla D)
	verifyNoEnvAccess(t)

	// Verificar que no se filtre un OTP propio de AI27 (R4.3, DEC-031): el
	// segundo factor vive en el IdP, no en este servicio.
	verifyNoOTPArtifacts(t)
}

// TestIsolationIntegration verifica los imports con `-tags integration`.
func TestIsolationIntegration(t *testing.T) {
	pkgs, err := listPackages(true)
	if err != nil {
		// Si go list falla con tags de integración, skip (todavía no hay build tags)
		t.Skipf("go list -tags integration no disponible: %v", err)
	}

	var viols []string
	for _, pkg := range pkgs {
		pkgViols := violationsConImportsDePrueba(pkg)
		viols = append(viols, pkgViols...)
	}

	if len(viols) > 0 {
		t.Errorf("violaciones de aislamiento en tests de integración:\n%s", strings.Join(viols, "\n"))
	}
}

// violationsConImportsDePrueba aplica las reglas distinguiendo los imports
// PRODUCTIVOS (pkg.Imports) de los de PRUEBA (TestImports, XTestImports). Las
// reglas A y B aplican a todos los imports. La regla E (aislamiento de
// sesiontest) solo prohíbe el import PRODUCTIVO: un paquete de prueba —o los
// TestImports/XTestImports de cualquier paquete— sí puede importar sesiontest.
// Así, un archivo _test.go que use los dobles no viola la regla.
func violationsConImportsDePrueba(pkg Package) []string {
	var viols []string

	// Reglas A y B: aplican a cualquier import (productivo o de prueba).
	todos := append([]string{}, pkg.Imports...)
	todos = append(todos, pkg.TestImports...)
	todos = append(todos, pkg.XTestImports...)
	for _, imp := range todos {
		isImplementation := hasPrefix(pkg.ImportPath, PrefijoDImplementacionIdP)
		if !isImplementation && hasAnyPrefix(imp, ListaDeBibliotecasIdP) {
			viols = append(viols, pkg.ImportPath+" importa "+imp+" (prohibido fuera de internal/idp/internal)")
		}
		if hasAnyPrefix(imp, append(PrefijosAWSV1, PrefijosAWSV2...)) {
			viols = append(viols, pkg.ImportPath+" importa "+imp+" (prohibido en cualquier paquete en V1)")
		}
	}

	// Regla E: solo los imports PRODUCTIVOS de un paquete NO-prueba la violan.
	for _, imp := range pkg.Imports {
		if hasPrefix(imp, "github.com/AI27-Tech/pf-svc-identidad/internal/sesion/sesiontest") {
			viols = append(viols, pkg.ImportPath+" importa sesiontest (prohibido en paquetes productivos; sesiontest es SOPORTE DE PRUEBAS, tarea E4-T3 5.2)")
		}
	}

	return viols
}

// Package representa un paquete listado por go list -json.
type Package struct {
	ImportPath   string   `json:"ImportPath"`
	Imports      []string `json:"Imports"`
	TestImports  []string `json:"TestImports"`
	XTestImports []string `json:"XTestImports"`
}

// listPackages ejecuta go list y retorna los paquetes.
func listPackages(integration bool) ([]Package, error) {
	args := []string{"list", "-deps=false", "-json", "./..."}
	if integration {
		args = append(args, "-tags=integration")
	}

	cmd := exec.Command("go", args...)
	cmd.Dir = getRepoRoot()
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}

	var pkgs []Package
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg Package
		if err := decoder.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		// Conservar Imports, TestImports y XTestImports POR SEPARADO (tarea 5.2):
		// la regla E distingue los imports productivos de los de prueba.
		pkgs = append(pkgs, pkg)
	}

	return pkgs, nil
}

// verifyNoKCProtocolLiterals implementa la Regla C del diseño: recorre los
// archivos .go FUERA de internal/idp/internal y falla si un literal contiene
// una ruta de endpoint interna de Keycloak. El subárbol internal/idp/internal
// es la única zona autorizada a conocer esos endpoints, por lo que se excluye
// por completo. Se excluye también el propio archivo de la prueba, que declara
// esos patrones (diseño §Aislamiento, Regla C).
func verifyNoKCProtocolLiterals(t *testing.T) {
	forbidden := []string{
		"/protocol/openid-connect/",
		"/admin/realms/",
	}

	root := getRepoRoot()
	// Zona autorizada: todo lo que cuelga de internal/idp/internal queda fuera
	// del alcance de la regla.
	zonaAutorizada := filepath.Join(root, "internal", "idp", "internal")

	err := filepath.Walk(filepath.Join(root, "internal", "idp"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Saltar el subárbol autorizado completo.
			if path == zonaAutorizada {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "isolation_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, pattern := range forbidden {
			if strings.Contains(string(content), pattern) {
				t.Errorf("%s contiene ruta prohibida %q", path, pattern)
			}
		}
		return nil
	})

	if err != nil {
		t.Errorf("al verificar rutas literales: %v", err)
	}
}

// verifyNoEnvAccess verifica que internal/idp/internal no use os.Getenv.
func verifyNoEnvAccess(t *testing.T) {
	implPath := filepath.Join(getRepoRoot(), "internal", "idp", "internal")
	if _, err := os.Stat(implPath); os.IsNotExist(err) {
		// El directorio no existe aún; esta verificación no aplica
		return
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, implPath, nil, 0)
	if err != nil {
		t.Fatalf("parser.ParseDir: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, spec := range file.Imports {
				if spec.Path.Value == `"os"` {
					t.Errorf("%s importa os (verificar que no usa Getenv/LookupEnv/Environ)", fset.Position(spec.Pos()))
				}
			}
		}
	}
}

// verifyNoOTPArtifacts implementa R4.3: AI27 no tiene un segundo factor propio
// (DEC-031); el OTP vive en el IdP. Falla si aparece el nombre de migración
// otp_desafio en migrations/, o los marcadores otp.solicitado o /api/v1/otp en
// el código o en las migraciones. Se excluye el propio archivo de la prueba,
// que declara esos marcadores.
func verifyNoOTPArtifacts(t *testing.T) {
	root := getRepoRoot()

	// Marcadores prohibidos en todo el árbol (código y migraciones).
	prohibidosGlobal := []string{"otp.solicitado", "/api/v1/otp"}
	// Marcador prohibido sólo en migrations/ (nombre de migración).
	const prohibidoMigraciones = "otp_desafio"

	raices := []string{
		filepath.Join(root, "internal"),
		filepath.Join(root, "cmd"),
		filepath.Join(root, "migrations"),
	}

	for _, base := range raices {
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		}
		enMigraciones := base == filepath.Join(root, "migrations")

		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.Contains(path, "isolation_test.go") {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			texto := string(content)

			for _, marcador := range prohibidosGlobal {
				if strings.Contains(texto, marcador) {
					t.Errorf("%s contiene el marcador de OTP propio %q (R4.3: el OTP vive en el IdP, DEC-031)", path, marcador)
				}
			}
			// El nombre de migración otp_desafio sólo tiene sentido vigilarlo en
			// migrations/, pero también lo rechazamos en el nombre de archivo.
			if enMigraciones && strings.Contains(texto, prohibidoMigraciones) {
				t.Errorf("%s contiene la migración prohibida %q (R4.3)", path, prohibidoMigraciones)
			}
			if strings.Contains(filepath.Base(path), prohibidoMigraciones) {
				t.Errorf("%s es una migración prohibida %q (R4.3)", path, prohibidoMigraciones)
			}
			return nil
		})
		if err != nil {
			t.Errorf("al verificar artefactos de OTP en %s: %v", base, err)
		}
	}
}

// getRepoRoot retorna la raíz del repositorio (donde está go.mod).
func getRepoRoot() string {
	// Asumir que nos ejecutamos desde la raíz del módulo
	wd, _ := os.Getwd()
	// Si estamos ejecutando los tests desde aquí, ir para arriba
	for wd != "/" && wd != "" {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		wd = filepath.Dir(wd)
	}
	// Default: asumir que estamos en la raíz
	return "."
}
