// Package tracing initialises an OpenTelemetry tracer that exports spans via
// OTLP/HTTP to a local collector (default: http://localhost:4318).
//
// Usage:
//
//	shutdown, err := tracing.Init(ctx, "gateway", "http://otel-collector:4318")
//	if err != nil { log.Fatal(err) }
//	defer shutdown(ctx)
//
//	// In HTTP handlers:
//	ctx, span := tracing.Start(r.Context(), "handleChatCompletions")
//	defer span.End()
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// Init creates a TracerProvider that exports to the given OTLP HTTP endpoint.
// Returns a shutdown function that must be called on exit to flush pending spans.
func Init(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"),
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// Start begins a new span under the global tracer for serviceName.
func Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer("").Start(ctx, spanName, opts...)
}

// Middleware wraps an http.Handler, extracting incoming trace context from
// request headers and starting a server span for every request.
// The span name is the request path.
func Middleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(serviceName)
	prop := otel.GetTextMapPropagator()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
