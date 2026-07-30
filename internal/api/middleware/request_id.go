package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// RequestIDCtxKey is the gin context key holding the current request ID.
	RequestIDCtxKey = "request_id"
	// RequestIDHeader is the header the ID is read from and echoed back on.
	RequestIDHeader = "X-Request-ID"
)

// RequestID assigns every request a correlation ID, reusing an inbound
// X-Request-ID when the caller (or an upstream proxy) already set one. The ID is
// echoed in the response header so a client-reported failure can be traced back
// to its log lines.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}

		c.Set(RequestIDCtxKey, id)
		c.Header(RequestIDHeader, id)
		c.Next()
	}
}

// GetRequestIDFromContext returns the current request ID, or "" if unset.
func GetRequestIDFromContext(c *gin.Context) string {
	id, exists := c.Get(RequestIDCtxKey)
	if !exists {
		return ""
	}
	if s, ok := id.(string); ok {
		return s
	}
	return ""
}
