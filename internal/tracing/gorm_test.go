package tracing_test

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
	"github.com/ferriyusra/clean-arch-go-gin/internal/tracing"
)

// secretEmail is what the assertion about query variables looks for. If it ever
// turns up in a span attribute, the instrumentation is leaking user data into
// the tracing backend.
const secretEmail = "do-not-leak@example.com"

// tracedServer wires the real middleware chain and a real database, both
// instrumented, against an in-memory span exporter.
func tracedServer(t *testing.T) (*gin.Engine, *tracetest.InMemoryExporter) {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(t.Context())
	})

	db := testutil.NewDB(t)
	testutil.NoError(t, db.Use(tracing.NewGormPlugin()))
	testutil.MustCreate(t, db, &entity.UserEntity{
		ID:       uuid.New(),
		Email:    secretEmail,
		Password: []byte("hashed"),
		Name:     "Traced User",
	})

	r := testutil.NewEngine(t)
	r.Use(otelgin.Middleware("test-service", otelgin.WithTracerProvider(provider)))
	r.Use(middleware.RequestID(slog.New(slog.DiscardHandler)))
	r.GET("/users", func(c *gin.Context) {
		var found entity.UserEntity
		// WithContext is what links the database span to the HTTP span. Every
		// repository in this project does exactly this.
		err := db.WithContext(c.Request.Context()).
			Where("email = ?", secretEmail).First(&found).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": found.Email})
	})

	return r, exporter
}

func TestDatabaseSpansAreChildrenOfTheRequestSpan(t *testing.T) {
	engine, exporter := tracedServer(t)

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/users", nil))
	testutil.Equal(t, rec.Code, http.StatusOK, "status")

	spans := exporter.GetSpans()
	testutil.True(t, len(spans) >= 2, "an HTTP span and at least one database span")

	var httpSpan, dbSpan *tracetest.SpanStub
	for i := range spans {
		switch {
		case strings.HasPrefix(spans[i].Name, "gorm."):
			dbSpan = &spans[i]
		case spans[i].Parent.SpanID().IsValid() == false:
			httpSpan = &spans[i]
		}
	}

	if httpSpan == nil || dbSpan == nil {
		t.Fatalf("expected an HTTP root span and a gorm span, got %d spans", len(spans))
	}

	// One trace covering the request and its queries is the whole point; two
	// unrelated traces would make the database time impossible to attribute.
	testutil.Equal(t, dbSpan.SpanContext.TraceID(), httpSpan.SpanContext.TraceID(),
		"database span shares the request trace")
	testutil.Equal(t, dbSpan.Parent.SpanID(), httpSpan.SpanContext.SpanID(),
		"database span is a child of the request span")
}

// TestDatabaseSpansNeverCarryQueryVariables is a security regression test. The
// bound parameters of this project queries include email addresses, refresh
// tokens and bcrypt hashes; the SQL text may be recorded, the values may not.
func TestDatabaseSpansNeverCarryQueryVariables(t *testing.T) {
	engine, exporter := tracedServer(t)

	testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/users", nil))

	var checkedStatement bool
	for _, span := range exporter.GetSpans() {
		for _, attr := range span.Attributes {
			value := attr.Value.Emit()

			if strings.Contains(value, secretEmail) {
				t.Errorf("span %q attribute %q leaked a query variable: %s",
					span.Name, attr.Key, value)
			}

			if attr.Key == "db.statement" {
				checkedStatement = true
				testutil.True(t, strings.Contains(value, "?"),
					"the statement keeps its placeholders")
			}
		}
	}

	testutil.True(t, checkedStatement, "a db.statement attribute was recorded")
}

func TestRequestIDAdoptsTheTraceID(t *testing.T) {
	engine, _ := tracedServer(t)

	rec := testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/users", nil))

	// One identifier for the log line, the response header and the trace means
	// whichever one a reporter has, the other two can be found.
	requestID := rec.Header().Get(middleware.RequestIDHeader)
	testutil.Equal(t, len(requestID), 32, "request id is the 32-char trace id")
}
