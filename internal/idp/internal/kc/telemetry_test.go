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

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AI27-Tech/pf-svc-identidad/internal/idp"
)

// servidorPorResultado monta un Keycloak falso que responde según el resultado
// pedido, para ejercitar cada categoría de telemetría de forma determinista.
type resultadoPedido int

const (
	pideExito resultadoPedido = iota
	pideCredInvalida
	pideProveedorNoDisponible
	pideRevokeExito
)

func (resultadoPedido) Generate(r *rand.Rand, _ int) reflect.Value {
	return reflect.ValueOf(resultadoPedido(r.Intn(4)))
}

// adaptadorConReader arma un adaptador cuyo MeterProvider es un ManualReader,
// apuntado a un servidor que responde según cada resultado pedido.
func adaptadorConReader(t *testing.T) (*adapter, *metric.ManualReader, *tracetest.SpanRecorder, func()) {
	t.Helper()

	mux := http.NewServeMux()
	// /token sirve a Issue, Refresh y al token de servicio.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(cuerpo), "grant_type=client_credentials"):
			fmt.Fprint(w, `{"access_token":"svc","expires_in":300}`)
		case strings.Contains(string(cuerpo), "CRED_MALA"):
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
		case strings.Contains(string(cuerpo), "CAIDA"):
			w.WriteHeader(http.StatusInternalServerError)
		default:
			fmt.Fprint(w, `{"access_token":"at","refresh_token":"rt","expires_in":300}`)
		}
	})
	mux.HandleFunc("/sessions/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))

	cfg := configValida()
	cfg.TokenURL = srv.URL + "/token"
	cfg.RefreshURL = srv.URL + "/token"
	cfg.AdminSessionsURL = srv.URL + "/sessions"
	cfg.HTTPClient = srv.Client()
	cfg.Timeout = 5 * time.Second
	cfg.MeterProvider = mp

	a, err := New(cfg)
	if err != nil {
		srv.Close()
		t.Fatalf("New: %v", err)
	}
	// Inyectar el tracer de prueba (New toma el provider global para trazas).
	a.tel.tracer = tp.Tracer(nombreTracer)

	return a, reader, rec, srv.Close
}

// contarMediciones recorre el histograma y devuelve el total de registros por
// valor del atributo `resultado`.
func contarMediciones(t *testing.T, reader *metric.ManualReader) map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	conteo := map[string]uint64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != nombreInstrumento {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("instrumento %s no es histograma float64", m.Name)
			}
			for _, dp := range hist.DataPoints {
				res, existe := dp.Attributes.Value("resultado")
				if !existe {
					t.Error("medición sin atributo resultado")
					continue
				}
				conteo[res.AsString()] += dp.Count
			}
		}
	}
	return conteo
}

// TestPropiedadUnaMedicionPorLlamada verifica que cada llamada registre
// exactamente una medición con la categoría devuelta. Un Revoke idempotente se
// registra como exito; la cancelación como otro, nunca como
// proveedor_no_disponible.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 9: Una medición por llamada, con la categoría devuelta
func TestPropiedadUnaMedicionPorLlamada(t *testing.T) {
	propiedad := func(ops []resultadoPedido) bool {
		a, reader, _, cerrar := adaptadorConReader(t)
		defer cerrar()

		esperado := map[string]uint64{}
		for _, op := range ops {
			switch op {
			case pideExito:
				a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "p"}, "")
				esperado[string(resultadoExito)]++
			case pideCredInvalida:
				a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "CRED_MALA"}, "")
				esperado[string(resultadoCredInvalida)]++
			case pideProveedorNoDisponible:
				a.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: "CAIDA"}, "")
				esperado[string(resultadoProveedorNoDis)]++
			case pideRevokeExito:
				a.Revoke(context.Background(), "ses")
				esperado[string(resultadoExito)]++
			}
		}

		conteo := contarMediciones(t, reader)
		if len(conteo) != len(esperado) {
			return false
		}
		for res, n := range esperado {
			if conteo[res] != n {
				return false
			}
		}
		return true
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// TestCancelacionEsOtro verifica que la cancelación del contexto del llamador
// se registre como `otro`, nunca como `proveedor_no_disponible` (R10.5).
func TestCancelacionEsOtro(t *testing.T) {
	a, reader, _, cerrar := adaptadorConReader(t)
	defer cerrar()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Issue(ctx, idp.Credentials{Usuario: "u", Contrasena: "p"}, "")

	conteo := contarMediciones(t, reader)
	if conteo[string(resultadoOtro)] != 1 {
		t.Errorf("esperaba 1 medición 'otro', obtuvo %d", conteo[string(resultadoOtro)])
	}
	if conteo[string(resultadoProveedorNoDis)] != 0 {
		t.Error("la cancelación se registró como proveedor_no_disponible")
	}
}

// readerQueFalla es un Reader cuyo ForceFlush/Collect fallan, para simular una
// exportación rota. El SDK no propaga ese error al Record, así que el resultado
// de la operación no debe cambiar.
type lectorQueFalla struct{ metric.Reader }

func (lectorQueFalla) ForceFlush(context.Context) error { return errors.New("exportación rota") }

// TestPropiedadExportacionFallidaNoAlteraResultado compara un adaptador con un
// exportador que siempre falla contra uno funcional en el mismo escenario: el
// mismo Token y el mismo centinela según errors.Is.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 11: La exportación de telemetría no altera el resultado
func TestPropiedadExportacionFallidaNoAlteraResultado(t *testing.T) {
	nuevo := func(falla bool) (*adapter, func()) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cuerpo, _ := io.ReadAll(r.Body)
			if strings.Contains(string(cuerpo), "CRED_MALA") {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"at","refresh_token":"rt","expires_in":300}`)
		}))

		var reader metric.Reader = metric.NewManualReader()
		if falla {
			reader = lectorQueFalla{Reader: metric.NewManualReader()}
		}
		mp := metric.NewMeterProvider(metric.WithReader(reader))

		cfg := configValida()
		cfg.TokenURL = srv.URL + "/token"
		cfg.RefreshURL = srv.URL + "/token"
		cfg.HTTPClient = srv.Client()
		cfg.Timeout = 5 * time.Second
		cfg.MeterProvider = mp
		a, err := New(cfg)
		if err != nil {
			srv.Close()
			t.Fatalf("New: %v", err)
		}
		return a, srv.Close
	}

	propiedad := func(credMala bool) bool {
		aOK, cerrarOK := nuevo(false)
		defer cerrarOK()
		aFalla, cerrarFalla := nuevo(true)
		defer cerrarFalla()

		pwd := "p"
		if credMala {
			pwd = "CRED_MALA"
		}

		tokOK, errOK := aOK.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: pwd}, "")
		tokFalla, errFalla := aFalla.Issue(context.Background(), idp.Credentials{Usuario: "u", Contrasena: pwd}, "")

		mismoError := errors.Is(errFalla, idp.ErrCredencialInvalida) == errors.Is(errOK, idp.ErrCredencialInvalida) &&
			(errOK == nil) == (errFalla == nil)
		mismoToken := tokOK.AccessToken == tokFalla.AccessToken
		return mismoError && mismoToken
	}

	if err := quick.Check(propiedad, &quick.Config{MaxCount: 50}); err != nil {
		t.Error(err)
	}
}

// TestSinSecretosEnTelemetria amplía la Property 3 a métricas y spans: ningún
// atributo del histograma ni del span contiene usuario, contraseña ni tokens.
//
// Feature: e4-t1-adaptador-idp-keycloak, Property 3: Ningún secreto llega a errores ni a telemetría
func TestSinSecretosEnTelemetria(t *testing.T) {
	a, reader, rec, cerrar := adaptadorConReader(t)
	defer cerrar()

	const secreto = "SECRETO<xyz>SECRETO"
	a.Issue(context.Background(), idp.Credentials{Usuario: secreto, Contrasena: secreto}, secreto)
	a.Refresh(context.Background(), secreto)
	a.Revoke(context.Background(), secreto)

	// Atributos del histograma: sólo dependencia, operacion, resultado, causa.
	permitidos := map[string]bool{"dependencia": true, "operacion": true, "resultado": true, "causa": true}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != nombreInstrumento {
				continue
			}
			hist := m.Data.(metricdata.Histogram[float64])
			for _, dp := range hist.DataPoints {
				for _, kv := range dp.Attributes.ToSlice() {
					if !permitidos[string(kv.Key)] {
						t.Errorf("atributo de métrica no permitido: %s", kv.Key)
					}
					if strings.Contains(kv.Value.AsString(), "SECRETO<") {
						t.Errorf("la métrica filtró un secreto en %s", kv.Key)
					}
				}
			}
		}
	}

	// Atributos y nombre de los spans.
	for _, sp := range rec.Ended() {
		if strings.Contains(sp.Name(), "SECRETO<") {
			t.Errorf("el nombre del span filtró un secreto: %s", sp.Name())
		}
		for _, kv := range sp.Attributes() {
			if !permitidos[string(kv.Key)] {
				t.Errorf("atributo de span no permitido: %s", kv.Key)
			}
			if strings.Contains(kv.Value.AsString(), "SECRETO<") {
				t.Errorf("el span filtró un secreto en %s", kv.Key)
			}
		}
	}
}
