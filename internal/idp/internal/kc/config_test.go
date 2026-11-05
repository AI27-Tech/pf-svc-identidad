package kc

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"
)

// camposObligatorios son los campos que New/Validate exigen (sin RefreshURL,
// que toma por omisión TokenURL).
var camposObligatorios = []string{
	"TokenURL", "AdminSessionsURL", "ClientID", "ClientSecret",
	"ServiceClientID", "ServiceClientSecret",
}

// configParcial es un escenario que parte de una configuración válida y vacía
// un subconjunto de campos y/o pone un Timeout arbitrario.
type configParcial struct {
	vaciar        map[string]bool
	timeout       time.Duration
	timeoutValido bool
}

// Generate implementa quick.Generator.
func (configParcial) Generate(r *rand.Rand, _ int) reflect.Value {
	c := configParcial{vaciar: map[string]bool{}}
	for _, campo := range camposObligatorios {
		if r.Intn(2) == 0 {
			c.vaciar[campo] = true
		}
	}
	c.timeoutValido = r.Intn(2) == 0
	if c.timeoutValido {
		c.timeout = time.Duration(1+r.Intn(60)) * time.Second
	} else {
		c.timeout = time.Duration(-r.Intn(10)) * time.Second // <= 0
	}
	return reflect.ValueOf(c)
}

// configValida arma una Config completa y válida.
func configValida() Config {
	return Config{
		TokenURL:            "https://kc.test/token",
		AdminSessionsURL:    "https://kc.test/sessions",
		AdminUsersURL:       "https://kc.test/users",
		AdminGroupsURL:      "https://kc.test/groups",
		ClientID:            "login",
		ClientSecret:        "s1",
		ServiceClientID:     "svc",
		ServiceClientSecret: "s2",
		AdminClientID:       "admin-svc",
		AdminClientSecret:   "s3",
		Timeout:             5 * time.Second,
	}
}

// TestPropiedadValidacionConfig verifica que New falle si y sólo si el
// subconjunto vaciado no está vacío o Timeout <= 0; que el error nombre
// exactamente esos campos sin valores; y que no se haga ninguna petición de red.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 5: Validación de configuración
func TestPropiedadValidacionConfig(t *testing.T) {
	var peticiones int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&peticiones, 1)
	}))
	defer srv.Close()

	propiedad := func(cp configParcial) bool {
		cfg := configValida()
		cfg.HTTPClient = srv.Client()

		var vaciados []string
		for campo := range cp.vaciar {
			switch campo {
			case "TokenURL":
				cfg.TokenURL = ""
			case "AdminSessionsURL":
				cfg.AdminSessionsURL = ""
			case "ClientID":
				cfg.ClientID = ""
			case "ClientSecret":
				cfg.ClientSecret = ""
			case "ServiceClientID":
				cfg.ServiceClientID = ""
			case "ServiceClientSecret":
				cfg.ServiceClientSecret = ""
			}
			vaciados = append(vaciados, campo)
		}
		cfg.Timeout = cp.timeout

		_, err := New(cfg)
		debeFallar := len(vaciados) > 0 || !cp.timeoutValido

		if (err != nil) != debeFallar {
			return false
		}
		if err == nil {
			return true
		}
		// El error nombra cada campo vaciado y no incluye valores.
		msg := err.Error()
		for _, campo := range vaciados {
			if !strings.Contains(msg, campo) {
				return false
			}
		}
		if !cp.timeoutValido && !strings.Contains(msg, "Timeout") {
			return false
		}
		// No debe revelar valores reales de la config válida.
		for _, valor := range []string{"login", "svc", "s1", "s2"} {
			if strings.Contains(msg, valor) {
				return false
			}
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}

	if n := atomic.LoadInt64(&peticiones); n != 0 {
		t.Errorf("New hizo %d peticiones de red; esperaba 0", n)
	}
}

// TestConfigValidateURLs verifica que Validate rechace URLs no parseables o con
// esquema distinto de http/https, nombrando el campo.
func TestConfigValidateURLs(t *testing.T) {
	casos := []struct {
		nombre string
		ajuste func(*Config)
		campo  string
	}{
		{"TokenURL esquema invalido", func(c *Config) { c.TokenURL = "ftp://x/y" }, "TokenURL"},
		{"AdminSessionsURL esquema invalido", func(c *Config) { c.AdminSessionsURL = "gopher://x" }, "AdminSessionsURL"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cfg := configValida()
			c.ajuste(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate aceptó una URL con esquema inválido")
			}
			if !strings.Contains(err.Error(), c.campo) {
				t.Errorf("el error no nombra el campo %s: %v", c.campo, err)
			}
		})
	}
}
