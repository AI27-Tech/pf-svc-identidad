package main

import (
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

// variablesKeycloak son todas las variables que puede tener una config de
// Keycloak, con un valor "centinela" único por cada una para detectar fugas.
var variablesKeycloak = map[string]string{
	"IDENTIDAD_KEYCLOAK_TOKEN_URL":             "https://kc/realms/ai27/protocol/openid-connect/token",
	"IDENTIDAD_KEYCLOAK_ADMIN_SESSIONS_URL":    "https://kc/admin/realms/ai27/sessions",
	"IDENTIDAD_KEYCLOAK_ADMIN_USERS_URL":       "https://kc/admin/realms/ai27/users",
	"IDENTIDAD_KEYCLOAK_ADMIN_GROUPS_URL":      "https://kc/admin/realms/ai27/groups",
	"IDENTIDAD_KEYCLOAK_CLIENT_ID":             "VAL_CLIENT_ID",
	"IDENTIDAD_KEYCLOAK_CLIENT_SECRET":         "VAL_CLIENT_SECRET",
	"IDENTIDAD_KEYCLOAK_SERVICE_CLIENT_ID":     "VAL_SVC_ID",
	"IDENTIDAD_KEYCLOAK_SERVICE_CLIENT_SECRET": "VAL_SVC_SECRET",
	"IDENTIDAD_KEYCLOAK_ADMIN_CLIENT_ID":       "VAL_ADMIN_SVC_ID",
	"IDENTIDAD_KEYCLOAK_ADMIN_CLIENT_SECRET":   "VAL_ADMIN_SVC_SECRET",
	"IDENTIDAD_KEYCLOAK_TIMEOUT":               "5s",
}

// entornoGenerado cubre los tres casos del diseño:
//
//	(i)   IDENTIDAD_IDP ausente/vacío/"memory" → memory
//	(ii)  "keycloak" con un subconjunto de variables vaciadas
//	(iii) un valor arbitrario no aceptado
type entornoGenerado struct {
	caso   int             // 0=(i), 1=(ii), 2=(iii)
	vaciar map[string]bool // sólo para (ii)
}

func (entornoGenerado) Generate(r *rand.Rand, _ int) reflect.Value {
	e := entornoGenerado{caso: r.Intn(3), vaciar: map[string]bool{}}
	if e.caso == 1 {
		for v := range variablesKeycloak {
			if r.Intn(2) == 0 {
				e.vaciar[v] = true
			}
		}
	}
	return reflect.ValueOf(e)
}

// construirEnv arma el mapa de entorno para un escenario generado.
func (e entornoGenerado) construirEnv() (env map[string]string, debeFallar bool, esMemory bool) {
	env = map[string]string{}
	switch e.caso {
	case 0: // (i)
		switch rand.Intn(3) {
		case 0: // ausente: no se pone IDENTIDAD_IDP
		case 1:
			env["IDENTIDAD_IDP"] = ""
		case 2:
			env["IDENTIDAD_IDP"] = "memory"
		}
		return env, false, true
	case 1: // (ii)
		env["IDENTIDAD_IDP"] = "keycloak"
		for k, v := range variablesKeycloak {
			if !e.vaciar[k] {
				env[k] = v
			}
		}
		return env, len(e.vaciar) > 0, false
	default: // (iii)
		env["IDENTIDAD_IDP"] = "valor-arbitrario-no-aceptado"
		return env, true, false
	}
}

// TestPropiedadCargaConfig verifica que cargarConfig acepte/rechace según el
// caso y que NINGÚN valor del entorno aparezca en el texto del error.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 10: Carga de configuración en la raíz
func TestPropiedadCargaConfig(t *testing.T) {
	readFile := func(string) ([]byte, error) { return nil, errors.New("sin archivos") }

	propiedad := func(e entornoGenerado) bool {
		env, debeFallar, esMemory := e.construirEnv()

		cfg, err := cargarConfig(lookupDe(env), readFile)

		if (err != nil) != debeFallar {
			return false
		}
		if err != nil {
			// Ningún valor del entorno debe aparecer en el error.
			msg := err.Error()
			for _, v := range env {
				if v != "" && strings.Contains(msg, v) {
					return false
				}
			}
			return true
		}
		if esMemory && cfg.Seleccion != idpMemory {
			return false
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 200}); err != nil {
		t.Error(err)
	}
}
