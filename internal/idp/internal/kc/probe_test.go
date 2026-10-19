package kc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// probeConfig arma una Config válida apuntando TokenURL al servidor dado, con
// la forma de ruta que el probe espera para derivar el discovery endpoint.
func probeConfig(baseURL string) Config {
	cfg := configValida()
	cfg.TokenURL = baseURL + "/realms/ai27/protocol/openid-connect/token"
	cfg.AdminSessionsURL = baseURL + "/admin/realms/ai27/sessions"
	cfg.Timeout = 5 * time.Second
	return cfg
}

// TestProbeAlcanzabilidad cubre: servidor caído → error saneado en ≤ 5 s;
// respuesta 500 → "respondió 500" sin cuerpo; respuesta 200 → nil.
func TestProbeAlcanzabilidad(t *testing.T) {
	t.Run("200 responde nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
				t.Errorf("el probe pidió una ruta inesperada: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cfg := probeConfig(srv.URL)
		cfg.HTTPClient = srv.Client()
		probe, err := NewProbe(cfg)
		if err != nil {
			t.Fatalf("NewProbe: %v", err)
		}
		if err := probe(context.Background()); err != nil {
			t.Errorf("esperaba nil con 200, obtuvo %v", err)
		}
	})

	t.Run("500 responde codigo sin cuerpo", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("detalle interno que no debe filtrarse"))
		}))
		defer srv.Close()

		cfg := probeConfig(srv.URL)
		cfg.HTTPClient = srv.Client()
		probe, _ := NewProbe(cfg)
		err := probe(context.Background())
		if err == nil {
			t.Fatal("esperaba error con 500")
		}
		msg := err.Error()
		if !strings.Contains(msg, "500") {
			t.Errorf("el error no menciona el código 500: %q", msg)
		}
		if strings.Contains(msg, "detalle interno") {
			t.Errorf("el error filtró el cuerpo del servidor: %q", msg)
		}
	})

	t.Run("servidor caido responde error saneado en menos de 5s", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		cfg := probeConfig(srv.URL)
		cfg.HTTPClient = srv.Client()
		srv.Close() // caído antes de llamar

		probe, _ := NewProbe(cfg)
		inicio := time.Now()
		err := probe(context.Background())
		transcurrido := time.Since(inicio)

		if err == nil {
			t.Fatal("esperaba error con el servidor caído")
		}
		if !strings.Contains(err.Error(), "no alcanzable") {
			t.Errorf("esperaba un error saneado 'no alcanzable', obtuvo %q", err.Error())
		}
		if transcurrido > 5*time.Second {
			t.Errorf("el probe tardó %v; debía responder en ≤ 5 s", transcurrido)
		}
	})
}
