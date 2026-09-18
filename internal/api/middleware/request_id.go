package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/tracing"
)

const (
	// RequestIDHeader is read from the incoming request, so a gateway or an
	// upstream service can propagate its own id, and echoed on the response.
	RequestIDHeader = "X-Request-ID"
	// RequestIDCtxKey is where the id is stored on the gin context.
	RequestIDCtxKey = "request_id"
	// TraceIDCtxKey holds the W3C trace id when the request is part of a trace.
	TraceIDCtxKey = "trace_id"

	// maxRequestIDLength bounds a client-supplied id. It is echoed back and
	// written to every log line for the request, so it must not be unbounded.
	maxRequestIDLength = 128
)

// RequestID assigns every request a correlation id and attaches a logger
// pre-tagged with it to the request context.
//
// When tracing is enabled this middleware must run *after* otelgin, so that the
// span already exists and the request id can be the trace id. Sharing one
// identifier means a log line, an error response and a span in the tracing
// backend can all be found from whichever one the reporter happens to have.
//
// Because the logger travels in context.Context, every service and repository
// method downstream reaches it through the ctx it already receives.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		traceID := tracing.TraceIDFromContext(ctx)

		id := resolveRequestID(c, traceID)

		c.Set(RequestIDCtxKey, id)
		c.Header(RequestIDHeader, id)

		attrs := []any{
			"request_id", id,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		}

		// trace_id and span_id keep their OpenTelemetry spelling: log
		// correlation in every backend expects those exact keys. The camelCase
		// rule applies to the JSON API, not to log records.
		if traceID != "" {
			c.Set(TraceIDCtxKey, traceID)
			attrs = append(attrs, "trace_id", traceID)

			if spanID := tracing.SpanIDFromContext(ctx); spanID != "" {
				attrs = append(attrs, "span_id", spanID)
			}
		}

		reqLogger := base.With(attrs...)
		c.Request = c.Request.WithContext(logging.Into(ctx, reqLogger))

		c.Next()
	}
}

// resolveRequestID prefers a valid client-supplied id, then the trace id, then
// a fresh UUID.
func resolveRequestID(c *gin.Context, traceID string) string {
	if id := sanitizeRequestID(c.GetHeader(RequestIDHeader)); id != "" {
		return id
	}
	if traceID != "" {
		return traceID
	}
	return uuid.NewString()
}

// sanitizeRequestID rejects anything that should not end up in a log record or
// a response header. An unbounded or exotic client-supplied value is a way to
// pollute logs, so a rejected id is replaced rather than cleaned.
func sanitizeRequestID(id string) string {
	if id == "" || len(id) > maxRequestIDLength {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return ""
		}
	}
	return id
}

// GetRequestID returns the correlation id assigned to this request.
func GetRequestID(c *gin.Context) string {
	return stringFromContext(c, RequestIDCtxKey)
}

// GetTraceID returns the W3C trace id, or "" when the request is not traced.
func GetTraceID(c *gin.Context) string {
	return stringFromContext(c, TraceIDCtxKey)
}

func stringFromContext(c *gin.Context, key string) string {
	value, ok := c.Get(key)
	if !ok {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return str
}
