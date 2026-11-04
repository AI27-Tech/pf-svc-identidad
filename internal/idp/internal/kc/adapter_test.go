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
	"testing"
	"testing/quick"
	"time"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// nuevoAdaptador arma un adaptador apuntado a srv con un Timeout holgado.
func nuevoAdaptador(t *testing.T, srv *httptest.Server) *adapter {
	t.Helper()
	cfg := configValida()
	cfg.TokenURL = srv.URL + "/token"
	cfg.RefreshURL = srv.URL + "/token"
	cfg.AdminSessionsURL = srv.URL + "/sessions"
	cfg.AdminUsersURL = srv.URL + "/users"
	cfg.AdminGroupsURL = srv.URL + "/groups"
	cfg.HTTPClient = srv.Client()
	cfg.Timeout = 5 * time.Second
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// TestPropiedadCredencialRechazada verifica que, ante invalid_grant, el error
// sea siempre ErrCredencialInvalida, con texto constante, sin revelar el
// usuario ni el error_description, para usuarios arbitrarios (vacíos, Unicode).
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 2: Credencial rechazada no revela causa ni usuario
func TestPropiedadCredencialRechazada(t *testing.T) {
	// El servidor responde invalid_grant con un error_description centinela fijo
	// (sin estado compartido con el test, para evitar carreras entre goroutines).
	const descServidor = "DSC<descripcion-del-servidor>DSC"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"invalid_grant","error_description":%q}`, descServidor)
	}))
	defer srv.Close()
	a := nuevoAdaptador(t, srv)

	propiedad := func(semillaUsuario string) bool {
		// Se envuelve el usuario en un centinela único e improbable, de modo que
		// cualquier aparición en el mensaje sea una filtración real y no una
		// coincidencia de subcadena con el texto constante del error.
		usuario := "USR<" + semillaUsuario + ">USR"

		_, err := a.Issue(context.Background(), idp.Credentials{Usuario: usuario, Contrasena: "x"}, "")
		if !errors.Is(err, idp.ErrCredencialInvalida) {
			return false
		}
		msg := err.Error()
		// Ni el usuario ni la descripción del servidor deben aparecer.
		if strings.Contains(msg, "USR<") || strings.Contains(msg, "DSC<") {
			return false
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// TestPropiedadSinSecretosEnError verifica que la contraseña y los tokens que
// el servidor repite en el cuerpo y en error_description nunca aparezcan en el
// texto del error (Property 3, alcance de esta tarea: el texto de error).
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 3: Ningún secreto llega a errores ni a telemetría
func TestPropiedadSinSecretosEnError(t *testing.T) {
	// El servidor lee el cuerpo de la propia petición (donde viajan la
	// contraseña y el refresh token) y lo refleja en error_description y en el
	// cuerpo de error. Así el reflejo es por-petición, sin estado compartido.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"invalid_grant","error_description":%q,"leak":%q}`, string(cuerpo), string(cuerpo))
	}))
	defer srv.Close()
	a := nuevoAdaptador(t, srv)

	propiedad := func(semillaPwd, semillaRefresh string) bool {
		// Centinelas únicos: cualquier aparición es filtración real.
		password := "PWD<" + semillaPwd + ">PWD"
		refresh := "REF<" + semillaRefresh + ">REF"

		_, errIssue := a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: password}, "")
		_, errRefresh := a.Refresh(context.Background(), refresh)
		for _, err := range []error{errIssue, errRefresh} {
			if err == nil {
				continue
			}
			msg := err.Error()
			if strings.Contains(msg, "PWD<") || strings.Contains(msg, "REF<") {
				return false
			}
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// escenarioToken genera una respuesta de token exitosa con expires_in acotado.
type escenarioToken struct {
	expiresIn  int64
	conRefresh bool
}

func (escenarioToken) Generate(r *rand.Rand, _ int) reflect.Value {
	return reflect.ValueOf(escenarioToken{
		expiresIn:  int64(1 + r.Intn(1e7)),
		conRefresh: r.Intn(2) == 0,
	})
}

// TestPropiedadMapeoToken verifica el mapeo de la respuesta de token: ExpiraEn
// se calcula desde un now fijo en UTC, y un refresh_token ausente produce
// RefreshToken vacío sin error.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 4: Mapeo de la respuesta de token
func TestPropiedadMapeoToken(t *testing.T) {
	var esc escenarioToken
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if esc.conRefresh {
			fmt.Fprintf(w, `{"access_token":"at","refresh_token":"rt","expires_in":%d}`, esc.expiresIn)
		} else {
			fmt.Fprintf(w, `{"access_token":"at","expires_in":%d}`, esc.expiresIn)
		}
	}))
	defer srv.Close()
	a := nuevoAdaptador(t, srv)

	// now fijo e inyectado.
	fijo := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return fijo }

	propiedad := func(e escenarioToken) bool {
		esc = e
		tok, err := a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
		if err != nil {
			return false
		}
		if tok.AccessToken != "at" {
			return false
		}
		if e.conRefresh && tok.RefreshToken != "rt" {
			return false
		}
		if !e.conRefresh && tok.RefreshToken != "" {
			return false
		}
		if tok.ExpiraEn.Location() != time.UTC {
			return false
		}
		esperado := fijo.UTC().Add(time.Duration(e.expiresIn) * time.Second)
		return tok.ExpiraEn.Equal(esperado)
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// TestAdaptadorCancelacion verifica que la cancelación del contexto del
// llamador corte la llamada antes del Timeout propio y devuelva context.Canceled.
func TestAdaptadorCancelacion(t *testing.T) {
	libera := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-libera // bloquea hasta que el test lo libere
	}))
	defer srv.Close()
	defer close(libera)

	a := nuevoAdaptador(t, srv)
	a.config.Timeout = 10 * time.Second // Timeout holgado: la cancelación debe ganar

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	inicio := time.Now()
	_, err := a.Issue(ctx, idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
	transcurrido := time.Since(inicio)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("esperaba context.Canceled, obtuvo %v", err)
	}
	if transcurrido >= 10*time.Second {
		t.Errorf("el retorno tardó %v; debía ocurrir antes del Timeout", transcurrido)
	}
}

// TestAdaptadorTimeoutPropio verifica que el vencimiento del Timeout propio
// produzca ErrProveedorNoDisponible.
func TestAdaptadorTimeoutPropio(t *testing.T) {
	libera := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-libera
	}))
	defer srv.Close()
	defer close(libera)

	a := nuevoAdaptador(t, srv)
	a.config.Timeout = 100 * time.Millisecond

	_, err := a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
	if !errors.Is(err, idp.ErrProveedorNoDisponible) {
		t.Errorf("esperaba ErrProveedorNoDisponible, obtuvo %v", err)
	}
}

// TestAdaptadorRespuestasNoExito cubre 5xx, cuerpo no JSON y conexión rechazada.
func TestAdaptadorRespuestasNoExito(t *testing.T) {
	t.Run("5xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		a := nuevoAdaptador(t, srv)
		_, err := a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
		if !errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Errorf("5xx: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
		}
	})

	t.Run("200 cuerpo no JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "no-es-json")
		}))
		defer srv.Close()
		a := nuevoAdaptador(t, srv)
		_, err := a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
		if !errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Errorf("cuerpo no JSON: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
		}
	})

	t.Run("conexion rechazada", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		a := nuevoAdaptador(t, srv)
		srv.Close() // cerrar antes de llamar: la conexión será rechazada
		_, err := a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
		if !errors.Is(err, idp.ErrProveedorNoDisponible) {
			t.Errorf("conexión rechazada: esperaba ErrProveedorNoDisponible, obtuvo %v", err)
		}
	})
}
