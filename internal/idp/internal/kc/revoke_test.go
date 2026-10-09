package kc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// servidorAdmin simula la API de administración de sesiones de Keycloak, con un
// conjunto de sesiones existentes y un endpoint de token de servicio.
type servidorAdmin struct {
	mu       sync.Mutex
	sesiones map[string]bool
	borradas []string // rutas DELETE recibidas
	srv      *httptest.Server
}

func nuevoServidorAdmin(sesiones ...string) *servidorAdmin {
	sa := &servidorAdmin{sesiones: map[string]bool{}}
	for _, s := range sesiones {
		sa.sesiones[s] = true
	}
	mux := http.NewServeMux()
	// Token de servicio (client_credentials).
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"access_token":"svc-token","expires_in":300,"token_type":"Bearer"}`)
	})
	// Administración de sesiones: DELETE /sessions/{id}.
	mux.HandleFunc("/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/sessions/")
		sa.mu.Lock()
		sa.borradas = append(sa.borradas, r.URL.Path)
		existe := sa.sesiones[id]
		delete(sa.sesiones, id)
		sa.mu.Unlock()
		if existe {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	sa.srv = httptest.NewServer(mux)
	return sa
}

func (sa *servidorAdmin) adaptador(t *testing.T) *adapter {
	t.Helper()
	cfg := configValida()
	cfg.TokenURL = sa.srv.URL + "/token"
	cfg.AdminSessionsURL = sa.srv.URL + "/sessions"
	cfg.HTTPClient = sa.srv.Client()
	cfg.Timeout = 5 * time.Second
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func (sa *servidorAdmin) cerrar() { sa.srv.Close() }

func (sa *servidorAdmin) numSesiones() int {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	return len(sa.sesiones)
}

// escenarioRevoke genera un id de sesión y un número de repeticiones.
type escenarioRevoke struct {
	sesionID string
	reps     int
}

func (escenarioRevoke) Generate(r *rand.Rand, _ int) reflect.Value {
	return reflect.ValueOf(escenarioRevoke{
		sesionID: fmt.Sprintf("ses-%d", r.Intn(5)),
		reps:     1 + r.Intn(4),
	})
}

// TestPropiedadRevokeIdempotente verifica que, para un sesionID arbitrario y
// n >= 1 repeticiones, todas devuelvan nil, que el conjunto final sea el inicial
// menos a lo sumo esa sesión, y que sólo haya DELETE a esa ruta.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 6: Revoke idempotente y acotado
func TestPropiedadRevokeIdempotente(t *testing.T) {
	propiedad := func(e escenarioRevoke) bool {
		sa := nuevoServidorAdmin("ses-0", "ses-1", "ses-2")
		defer sa.cerrar()
		a := sa.adaptador(t)

		inicial := sa.numSesiones()
		for i := 0; i < e.reps; i++ {
			if err := a.Revoke(context.Background(), e.sesionID); err != nil {
				return false
			}
		}
		// El conjunto final es el inicial menos a lo sumo una sesión.
		final := sa.numSesiones()
		if final != inicial && final != inicial-1 {
			return false
		}
		// Sólo DELETE a la ruta de esa sesión.
		rutaEsperada := "/sessions/" + e.sesionID
		for _, ruta := range sa.borradas {
			if ruta != rutaEsperada {
				return false
			}
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// grabadorRoundTrip registra los hosts y rutas de cada petición saliente.
type grabadorRoundTrip struct {
	mu      sync.Mutex
	destino []string
}

func (g *grabadorRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	g.mu.Lock()
	g.destino = append(g.destino, req.URL.Host+req.URL.Path)
	g.mu.Unlock()
	// Respuesta mínima según el endpoint.
	cuerpo := `{"access_token":"t","expires_in":300}`
	status := http.StatusOK
	if req.Method == http.MethodDelete {
		cuerpo = ""
		status = http.StatusNoContent
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(cuerpo)),
		Header:     make(http.Header),
	}, nil
}

// TestPropiedadSoloURLsConfiguradas verifica que, ante secuencias arbitrarias de
// Issue/Refresh/Revoke, el adaptador sólo contacte los hosts/rutas de TokenURL,
// RefreshURL y AdminSessionsURL (incluido el token de servicio).
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 8: El adaptador sólo llama a las URLs configuradas
func TestPropiedadSoloURLsConfiguradas(t *testing.T) {
	propiedad := func(ops []byte) bool {
		g := &grabadorRoundTrip{}
		cfg := configValida()
		cfg.TokenURL = "https://kc.interno/realms/ai27/token"
		cfg.RefreshURL = "https://kc.interno/realms/ai27/token"
		cfg.AdminSessionsURL = "https://kc.interno/admin/sessions"
		cfg.HTTPClient = &http.Client{Transport: g}
		cfg.Timeout = 5 * time.Second
		a, err := New(cfg)
		if err != nil {
			return false
		}

		for _, op := range ops {
			switch op % 3 {
			case 0:
				a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"})
			case 1:
				a.Refresh(context.Background(), "rt")
			case 2:
				a.Revoke(context.Background(), "ses")
			}
		}

		permitidos := map[string]bool{
			"kc.interno/realms/ai27/token":  true,
			"kc.interno/admin/sessions/ses": true,
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, d := range g.destino {
			if !permitidos[d] {
				return false
			}
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// TestRevokeReintentoYNoExito cubre: reintento único ante 401, 403 como
// no-éxito, error del resolvedor, reutilización de la caché del token de
// servicio y su renovación tras invalidación.
func TestRevokeReintentoYNoExito(t *testing.T) {
	t.Run("reintento unico ante 401 luego exito", func(t *testing.T) {
		var intentosDelete, intentosToken int
		var mu sync.Mutex
		mux := http.NewServeMux()
		mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			intentosToken++
			mu.Unlock()
			fmt.Fprint(w, `{"access_token":"svc","expires_in":300}`)
		})
		mux.HandleFunc("/sessions/", func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			intentosDelete++
			n := intentosDelete
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized) // primer intento: 401
			} else {
				w.WriteHeader(http.StatusNoContent) // reintento: éxito
			}
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		cfg := configValida()
		cfg.TokenURL = srv.URL + "/token"
		cfg.AdminSessionsURL = srv.URL + "/sessions"
		cfg.HTTPClient = srv.Client()
		cfg.Timeout = 5 * time.Second
		a, _ := New(cfg)

		if err := a.Revoke(context.Background(), "ses"); err != nil {
			t.Errorf("esperaba nil tras el reintento, obtuvo %v", err)
		}
		if intentosDelete != 2 {
			t.Errorf("esperaba 2 DELETE (original + reintento), hubo %d", intentosDelete)
		}
		// La caché se invalidó tras el 401: se pidió un segundo token de servicio.
		if intentosToken != 2 {
			t.Errorf("esperaba 2 emisiones de token de servicio, hubo %d", intentosToken)
		}
	})

	t.Run("403 es no-exito", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"access_token":"svc","expires_in":300}`)
		})
		mux.HandleFunc("/sessions/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		cfg := configValida()
		cfg.TokenURL = srv.URL + "/token"
		cfg.AdminSessionsURL = srv.URL + "/sessions"
		cfg.HTTPClient = srv.Client()
		cfg.Timeout = 5 * time.Second
		a, _ := New(cfg)

		err := a.Revoke(context.Background(), "ses")
		if !errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Errorf("403: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
		}
	})

	t.Run("error del resolvedor se propaga", func(t *testing.T) {
		sa := nuevoServidorAdmin("ses-0")
		defer sa.cerrar()
		cfg := configValida()
		cfg.TokenURL = sa.srv.URL + "/token"
		cfg.AdminSessionsURL = sa.srv.URL + "/sessions"
		cfg.HTTPClient = sa.srv.Client()
		cfg.Timeout = 5 * time.Second
		errResolver := errors.New("no se pudo resolver")
		cfg.SessionResolver = func(string) (string, error) { return "", errResolver }
		a, _ := New(cfg)

		err := a.Revoke(context.Background(), "ses-0")
		if !errors.Is(err, errResolver) {
			t.Errorf("esperaba el error del resolvedor envuelto, obtuvo %v", err)
		}
	})

	t.Run("reutiliza la cache del token de servicio", func(t *testing.T) {
		var intentosToken int
		var mu sync.Mutex
		mux := http.NewServeMux()
		mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			intentosToken++
			mu.Unlock()
			fmt.Fprint(w, `{"access_token":"svc","expires_in":300}`)
		})
		mux.HandleFunc("/sessions/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		cfg := configValida()
		cfg.TokenURL = srv.URL + "/token"
		cfg.AdminSessionsURL = srv.URL + "/sessions"
		cfg.HTTPClient = srv.Client()
		cfg.Timeout = 5 * time.Second
		a, _ := New(cfg)

		for i := 0; i < 3; i++ {
			if err := a.Revoke(context.Background(), fmt.Sprintf("ses-%d", i)); err != nil {
				t.Fatalf("Revoke %d: %v", i, err)
			}
		}
		// El token de servicio se emite una vez y se reutiliza (caché vigente).
		if intentosToken != 1 {
			t.Errorf("esperaba 1 emisión de token (caché), hubo %d", intentosToken)
		}
	})
}
