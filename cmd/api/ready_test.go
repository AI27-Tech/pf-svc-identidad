package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AI27-Tech/pf-pkg-platform-go/health"
)

// montarMux replica el cableado de health de run(): /ready con el check de
// postgres y, si hay probe de IdP, /ready/idp en un registro aparte. Permite
// probar que la caída de Keycloak no contamina /ready.
func montarMux(postgresOK bool, probeIDP func(context.Context) error) *http.ServeMux {
	reg := health.NewRegistry()
	reg.RegisterCheck("postgres:svc_identidad", func(context.Context) error {
		if postgresOK {
			return nil
		}
		return errors.New("postgres caído")
	})

	mux := http.NewServeMux()
	health.RegisterRoutes(mux, reg)

	if probeIDP != nil {
		regIDP := health.NewRegistry()
		regIDP.RegisterCheck("idp:keycloak", health.CheckFunc(probeIDP))
		mux.Handle("/ready/idp", health.ReadinessHandler(regIDP))
		regIDP.SetReady(true)
	}

	reg.SetReady(true)
	return mux
}

// TestReadyIdpAisladoDeReady verifica que, con el probe del IdP fallando,
// /ready responde 200 y /ready/idp responde 503 con un cuerpo saneado.
func TestReadyIdpAisladoDeReady(t *testing.T) {
	probeFalla := func(context.Context) error {
		return errors.New("idp:keycloak no alcanzable")
	}
	mux := montarMux(true, probeFalla)

	// /ready debe seguir en 200: el IdP no es su check.
	t.Run("/ready sigue 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("esperaba 200 en /ready, obtuvo %d", w.Code)
		}
	})

	// /ready/idp debe responder 503 con el error saneado.
	t.Run("/ready/idp responde 503 saneado", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready/idp", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("esperaba 503 en /ready/idp, obtuvo %d", w.Code)
		}
		cuerpo := w.Body.String()
		if !strings.Contains(cuerpo, "no alcanzable") {
			t.Errorf("el cuerpo no trae el error saneado: %q", cuerpo)
		}
	})
}

// TestReadyIdpSanoResponde200 verifica que, con el probe en verde, /ready/idp
// responde 200.
func TestReadyIdpSanoResponde200(t *testing.T) {
	probeOK := func(context.Context) error { return nil }
	mux := montarMux(true, probeOK)

	req := httptest.NewRequest(http.MethodGet, "/ready/idp", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("esperaba 200 en /ready/idp, obtuvo %d", w.Code)
	}
}
