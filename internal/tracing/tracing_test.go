package tracing_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
	"github.com/ferriyusra/clean-arch-go-gin/internal/tracing"
)

func TestInitDisabledIsANoOp(t *testing.T) {
	shutdown, err := tracing.Init(context.Background(), tracing.Config{Enabled: false})
	testutil.NoError(t, err)
	testutil.NoError(t, shutdown(context.Background()))

	// No span means no ids, which is what keeps them out of response bodies
	// when tracing is switched off.
	testutil.Equal(t, tracing.TraceIDFromContext(context.Background()), "", "trace id")
	testutil.Equal(t, tracing.SpanIDFromContext(context.Background()), "", "span id")
}

func TestInitRejectsAnUnknownExporter(t *testing.T) {
	_, err := tracing.Init(context.Background(), tracing.Config{
		Enabled:  true,
		Exporter: "carrier-pigeon",
	})

	testutil.Error(t, err, "an unsupported exporter")
}

// TestInitInstallsW3CPropagationEvenWhenDisabled is the reason the propagator
// is set before the enabled check: a caller trace id must still reach the logs
// and the error response of a service that produces no spans itself.
func TestInitInstallsW3CPropagationEvenWhenDisabled(t *testing.T) {
	_, err := tracing.Init(context.Background(), tracing.Config{Enabled: false})
	testutil.NoError(t, err)

	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")

	ctx := otel.GetTextMapPropagator().Extract(
		req.Context(),
		propagation.HeaderCarrier(req.Header),
	)

	testutil.Equal(t, tracing.TraceIDFromContext(ctx), traceID, "extracted trace id")
	testutil.Equal(t, tracing.SpanIDFromContext(ctx), spanID, "extracted span id")
}

func TestTraceIDFromContextWithARecordedSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, span := provider.Tracer("test").Start(context.Background(), "work")

	traceID := tracing.TraceIDFromContext(ctx)
	testutil.Equal(t, len(traceID), 32, "trace id length")
	testutil.Equal(t, len(tracing.SpanIDFromContext(ctx)), 16, "span id length")

	span.End()

	spans := exporter.GetSpans()
	testutil.Equal(t, len(spans), 1, "exported span count")
	testutil.Equal(t, spans[0].Name, "work", "span name")
}

func TestRecordErrorMarksTheSpanFailed(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, span := provider.Tracer("test").Start(context.Background(), "work")
	tracing.RecordError(ctx, errors.New("boom"), "it broke")
	span.End()

	spans := exporter.GetSpans()
	testutil.Equal(t, len(spans), 1, "exported span count")
	testutil.Equal(t, spans[0].Status.Description, "it broke", "status description")
	testutil.True(t, len(spans[0].Events) > 0, "the error is recorded as an event")
}

// RecordError must tolerate an untraced context, because handler.Fail calls it
// on every 5xx whether or not tracing is enabled.
func TestRecordErrorOnAnUntracedContextIsSafe(t *testing.T) {
	tracing.RecordError(context.Background(), errors.New("boom"), "it broke")
	tracing.SetAttributes(context.Background())
}
