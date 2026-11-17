package gobierno

import (
	"reflect"
	"testing"
)

// Pruebas sin Postgres de los clientes efectivos y de qué sujetos publican
// `alcance.cambiado` (DEC-149 #18 y #20, DEC-140 #2 y #3). Cada caso describe
// el grafo antes y después de una escritura y espera la lista literal de
// cambios que publicaría el servicio.

// grafo arma un GrafoAlcance desde clientes por Monitorista y pares
// subordinado → superior.
func grafo(clientes map[string][]string, superior map[string]string) GrafoAlcance {
	if clientes == nil {
		clientes = map[string][]string{}
	}
	if superior == nil {
		superior = map[string]string{}
	}
	return GrafoAlcance{Clientes: clientes, Superior: superior}
}

// cambio es un AlcanceEfectivo sin hora, que la pone quien publica.
func cambio(sujeto string, clientes ...string) AlcanceEfectivo {
	if clientes == nil {
		clientes = []string{}
	}
	return AlcanceEfectivo{SujetoID: sujeto, ClientesEfectivosIDs: clientes}
}

func TestEfectivosUnionTransitivaSinRepetidos(t *testing.T) {
	g := grafo(
		map[string][]string{"m1": {"b", "a"}, "m2": {"a", "c"}, "m3": {"d"}},
		map[string]string{"m1": "s1", "m2": "s1", "s1": "c1", "m3": "s2"},
	)
	casos := map[string][]string{
		"m1": {"a", "b"},
		"s1": {"a", "b", "c"},
		"c1": {"a", "b", "c"},
		"s2": {"d"},
		"x":  {},
	}
	for id, quiere := range casos {
		if got := g.efectivos(id); !reflect.DeepEqual(got, quiere) {
			t.Errorf("efectivos(%s) = %v; quiere %v", id, got, quiere)
		}
	}
}

func TestEfectivosToleraCiclo(t *testing.T) {
	// La jerarquía no admite ciclos por rol, pero el recorrido no debe colgarse.
	g := grafo(map[string][]string{"a": {"x"}}, map[string]string{"a": "b", "b": "a"})
	if got := g.efectivos("b"); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("efectivos(b) = %v; quiere [x]", got)
	}
}

func TestCambiosDeAlcance(t *testing.T) {
	cadena := map[string]string{"m": "s", "s": "c"}
	casos := []struct {
		nombre        string
		antes, despue GrafoAlcance
		quiere        []AlcanceEfectivo
	}{
		{
			nombre: "fijar clientes publica al Monitorista y a su cadena",
			antes:  grafo(nil, cadena),
			despue: grafo(map[string][]string{"m": {"b", "a"}}, cadena),
			quiere: []AlcanceEfectivo{cambio("c", "a", "b"), cambio("m", "a", "b"), cambio("s", "a", "b")},
		},
		{
			nombre: "PUT repetido no publica",
			antes:  grafo(map[string][]string{"m": {"a", "b"}}, cadena),
			despue: grafo(map[string][]string{"m": {"b", "a"}}, cadena),
			quiere: nil,
		},
		{
			nombre: "un cliente que otro subordinado ya cubre solo cambia al Monitorista",
			antes:  grafo(map[string][]string{"m2": {"a"}}, map[string]string{"m": "s", "m2": "s"}),
			despue: grafo(map[string][]string{"m": {"a"}, "m2": {"a"}}, map[string]string{"m": "s", "m2": "s"}),
			quiere: []AlcanceEfectivo{cambio("m", "a")},
		},
		{
			nombre: "fijar subordinados publica al superior y a su cadena, no al subordinado",
			antes:  grafo(map[string][]string{"m": {"a"}}, map[string]string{"s": "c"}),
			despue: grafo(map[string][]string{"m": {"a"}}, cadena),
			quiere: []AlcanceEfectivo{cambio("c", "a"), cambio("s", "a")},
		},
		{
			nombre: "cambio de rol del Monitorista: vacío para él y recalcula a sus superiores",
			antes:  grafo(map[string][]string{"m": {"a", "b"}, "m2": {"b"}}, map[string]string{"m": "s", "m2": "s", "s": "c"}),
			despue: grafo(map[string][]string{"m2": {"b"}}, map[string]string{"m2": "s", "s": "c"}),
			quiere: []AlcanceEfectivo{cambio("c", "b"), cambio("m"), cambio("s", "b")},
		},
		{
			nombre: "cambio de rol del Supervisor: vacío para él y para su Coordinador; sus Monitoristas no cambian",
			antes:  grafo(map[string][]string{"m": {"a"}}, cadena),
			despue: grafo(map[string][]string{"m": {"a"}}, nil),
			quiere: []AlcanceEfectivo{cambio("c"), cambio("s")},
		},
		{
			nombre: "cambio de rol sin alcance no publica",
			antes:  grafo(map[string][]string{"m": {"a"}}, cadena),
			despue: grafo(map[string][]string{"m": {"a"}}, cadena),
			quiere: nil,
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := cambiosDeAlcance(c.antes, c.despue); !reflect.DeepEqual(got, c.quiere) {
				t.Errorf("cambios = %v; quiere %v", got, c.quiere)
			}
		})
	}
}

// TestMoverSubordinadoDeSuperior: la jerarquía es exclusiva (DEC-145 #2), así
// que mover a un Monitorista son dos escrituras: quitarlo de su Supervisor y
// asignarlo al otro. Cada una publica solo la cadena que cambia.
func TestMoverSubordinadoDeSuperior(t *testing.T) {
	clientes := map[string][]string{"m": {"a"}}
	inicio := grafo(clientes, map[string]string{"m": "s1", "s1": "c1", "s2": "c2"})
	sinSuperior := grafo(clientes, map[string]string{"s1": "c1", "s2": "c2"})
	final := grafo(clientes, map[string]string{"m": "s2", "s1": "c1", "s2": "c2"})

	if got, quiere := cambiosDeAlcance(inicio, sinSuperior), []AlcanceEfectivo{cambio("c1"), cambio("s1")}; !reflect.DeepEqual(got, quiere) {
		t.Errorf("quitar de s1: %v; quiere %v", got, quiere)
	}
	if got, quiere := cambiosDeAlcance(sinSuperior, final), []AlcanceEfectivo{cambio("c2", "a"), cambio("s2", "a")}; !reflect.DeepEqual(got, quiere) {
		t.Errorf("asignar a s2: %v; quiere %v", got, quiere)
	}
}
