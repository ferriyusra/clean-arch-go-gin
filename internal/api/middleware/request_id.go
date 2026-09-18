package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
)

const (
	// RequestIDHeader is both read from the incoming request (so a gateway or
	// upstream service can propagate its own id) and echoed on the response.
	RequestIDHeader = "X-Request-ID"
	// RequestIDCtxKey is where the id is stored on the gin context.
	RequestIDCtxKey = "request_id"
)

// RequestID assigns every request a correlation id and attaches a logger
// pre-tagged with it to the request context.
//
// Because the logger travels in context.Context, every service and repository
// method downstream can reach it through the ctx it already receives.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}

		c.Set(RequestIDCtxKey, id)
		c.Header(RequestIDHeader, id)

		reqLogger := base.With(
			"request_id", id,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		)
		c.Request = c.Request.WithContext(logging.Into(c.Request.Context(), reqLogger))

		c.Next()
	}
}

// GetRequestID returns the correlation id assigned to this request.
func GetRequestID(c *gin.Context) string {
	if v, ok := c.Get(RequestIDCtxKey); ok {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}
