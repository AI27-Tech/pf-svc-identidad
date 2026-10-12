package kc

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// nombreInstrumento es el histograma único que cubre R10.1 y R10.2: el conteo
// por `resultado` da "exactamente un resultado por llamada" y su valor da la
// latencia. El nombre sigue el patrón `ai27_*` de observability; es propuesta,
// no contrato.
const nombreInstrumento = "ai27_idp_duracion_segundos"

// nombreMeter y nombreTracer identifican la instrumentación de este adaptador.
const (
	nombreMeter  = "pf-svc-identidad/internal/idp/internal/kc"
	nombreTracer = "pf-svc-identidad/internal/idp/internal/kc"
)

// telemetría agrupa los instrumentos OTel del adaptador. Si el histograma no se
// pudo crear, `duracion` es nil y `record` no hace nada (R10.6: la telemetría
// nunca altera el resultado de la operación).
type telemetría struct {
	duracion metric.Float64Histogram
	tracer   trace.Tracer
}

// nuevaTelemetria construye los instrumentos a partir del MeterProvider de la
// configuración. Si mp es nil, usa el proveedor global (otel.GetMeterProvider,
// H6). Un error al crear el histograma se absorbe: la telemetría degrada a
// no-op, nunca propaga el error a Issue/Refresh/Revoke (R10.6).
func nuevaTelemetria(mp metric.MeterProvider) *telemetría {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(nombreMeter)

	hist, err := meter.Float64Histogram(
		nombreInstrumento,
		metric.WithUnit("s"),
		metric.WithDescription("Latencia de las llamadas del adaptador de identidad al IdP, por operación y resultado"),
	)
	if err != nil {
		// Degradar a no-op: la observabilidad no puede tumbar la autenticación.
		hist = nil
	}

	return &telemetría{
		duracion: hist,
		tracer:   otel.GetTracerProvider().Tracer(nombreTracer),
	}
}

// iniciarSpan abre el span cliente `idp.keycloak.<operacion>`. Devuelve el
// contexto con el span y una función para cerrarlo con los atributos finales.
// El span lleva sólo `dependencia`, `operacion`, `resultado` y, cuando aplica,
// `causa`: nunca usuario, sesion_id, tokens ni contraseña (R10.3).
func (t *telemetría) iniciarSpan(ctx context.Context, op string) (context.Context, trace.Span) {
	if t == nil || t.tracer == nil {
		return ctx, nil
	}
	return t.tracer.Start(ctx, "idp.keycloak."+operacionCanonica(op),
		trace.WithSpanKind(trace.SpanKindClient),
	)
}

// record registra la medición de una llamada: una sola vez por operación, con
// la categoría devuelta (R10.1, R10.2). El atributo `causa` sólo aparece cuando
// el resultado es `proveedor_no_disponible` (R10.4). Si el span no es nil, se
// le ponen los mismos atributos y se cierra.
func (t *telemetría) record(ctx context.Context, span trace.Span, op string, cat categoria, causa causaTransporte, inicio time.Time) {
	if t == nil {
		return
	}

	atributos := []attribute.KeyValue{
		attribute.String("dependencia", "keycloak"),
		attribute.String("operacion", operacionCanonica(op)),
		attribute.String("resultado", string(resultadoDeCategoria(cat))),
	}
	// El atributo `causa` sólo acompaña a proveedor_no_disponible, de una lista
	// cerrada (timeout, dns, conn_refused, http_5xx, cuerpo_invalido).
	if cat == proveedorNoDisponible && causa != "" {
		atributos = append(atributos, attribute.String("causa", string(causa)))
	}

	if t.duracion != nil {
		segundos := time.Since(inicio).Seconds()
		t.duracion.Record(ctx, segundos, metric.WithAttributes(atributos...))
	}

	if span != nil {
		span.SetAttributes(atributos...)
		span.End()
	}
}

// resultado es el valor del atributo `resultado` del histograma y del span.
type resultado string

const (
	resultadoExito          resultado = "exito"
	resultadoCredInvalida   resultado = "credencial_invalida"
	resultadoRefreshInv     resultado = "refresh_invalido"
	resultadoProveedorNoDis resultado = "proveedor_no_disponible"
	// resultadoOtro es provisional (R10.5): hoy cubre la cancelación del
	// contexto del llamador y, si A-14 lo decide, la falla de segundo factor.
	// Su nombre y si se divide quedan pendientes de A-14.
	resultadoOtro resultado = "otro"
)

// resultadoDeCategoria traduce la categoría interna de clasificación al valor
// del atributo `resultado`. La cancelación cae en `otro`, nunca en
// `proveedor_no_disponible` (R10.5).
func resultadoDeCategoria(cat categoria) resultado {
	switch cat {
	case exito:
		return resultadoExito
	case credencialInvalida:
		return resultadoCredInvalida
	case refreshInvalido:
		return resultadoRefreshInv
	case proveedorNoDisponible:
		return resultadoProveedorNoDis
	case cancelado:
		return resultadoOtro
	default:
		return resultadoOtro
	}
}

// operacionCanonica normaliza el nombre de operación al valor en minúsculas que
// usan el atributo `operacion` y el nombre del span (issue, refresh, revoke).
func operacionCanonica(op string) string {
	switch op {
	case "Issue":
		return "issue"
	case "Refresh":
		return "refresh"
	case "Revoke":
		return "revoke"
	default:
		return op
	}
}
