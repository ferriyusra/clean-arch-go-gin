// Package tracing configures OpenTelemetry tracing for the application.
//
// It deliberately exposes a very small surface: Init at startup, a shutdown
// function to flush on exit, and two accessors for the current ids. Everything
// else is instrumentation that hangs off context.Context, which every service
// and repository method already carries.
package tracing

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Supported exporters.
const (
	// ExporterOTLP sends spans over OTLP/HTTP to a collector (Jaeger, Tempo,
	// the OpenTelemetry Collector, or a vendor endpoint).
	ExporterOTLP = "otlp"
	// ExporterConsole prints spans to stdout. It exists so tracing can be seen
	// working without standing up a collector first.
	ExporterConsole = "console"
)

// Config describes how traces are produced and where they go.
type Config struct {
	Enabled        bool
	ServiceName    string
	ServiceVersion string
	Environment    string
	Exporter       string
	Endpoint       string
	Insecure       bool
	// SampleRatio is the fraction of traces kept for a request that arrives
	// with no sampling decision of its own, between 0 and 1.
	SampleRatio float64
}

// ShutdownFunc flushes buffered spans and releases the exporter. Spans are
// batched, so skipping this on exit loses whatever has not been sent yet.
type ShutdownFunc func(context.Context) error

// Init installs the global tracer provider and text-map propagators.
//
// The propagators are installed even when tracing is disabled: an incoming
// W3C traceparent then still reaches the request context, so this service can
// report the caller trace id in its logs and error responses without producing
// spans of its own.
func Init(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	noop := func(context.Context) error { return nil }
	if !cfg.Enabled {
		return noop, nil
	}

	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return noop, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(newResource(cfg)),
		// ParentBased keeps a distributed trace intact: once an upstream
		// service has decided to sample a trace, every downstream span for it
		// is kept, and a ratio is only applied to traces that start here.
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		)),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}

func newExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch strings.ToLower(cfg.Exporter) {
	case ExporterConsole, "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case ExporterOTLP, "":
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return otlptracehttp.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unsupported trace exporter %q (want %q or %q)",
			cfg.Exporter, ExporterOTLP, ExporterConsole)
	}
}

// newResource describes *this* service on every span it produces.
func newResource(cfg Config) *resource.Resource {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	}
	if cfg.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", cfg.Environment))
	}

	custom := resource.NewWithAttributes(semconv.SchemaURL, attrs...)

	// Merge fails when the SDK default resource was built against a different
	// semconv schema than the one imported here. That is a versioning detail,
	// not a reason to refuse to start, so fall back to our own attributes.
	merged, err := resource.Merge(resource.Default(), custom)
	if err != nil {
		return custom
	}
	return merged
}

// TraceIDFromContext returns the current trace id, or "" when the request is
// not part of a trace. Safe to call whether or not tracing is enabled.
func TraceIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.TraceID().IsValid() {
		return ""
	}
	return spanCtx.TraceID().String()
}

// SpanIDFromContext returns the current span id, or "" when there is none.
func SpanIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.SpanID().IsValid() {
		return ""
	}
	return spanCtx.SpanID().String()
}

// RecordError marks the active span as failed and attaches err to it.
//
// Without this a trace shows a slow or odd-looking request but gives no reason,
// which is most of the value of having the trace at all. It is a no-op when the
// request is not traced.
func RecordError(ctx context.Context, err error, description string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, description)
}

// SetAttributes adds attributes to the active span, if there is one.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attrs...)
}
