package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
)

// AccessLog emits exactly one structured line per request.
//
// It replaces gin.Logger(), whose plain-text output cannot be correlated with
// the rest of the application's logs. Severity follows the status code so that
// 5xx responses surface without extra filtering.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			"status", status,
			"latency_ms", float64(time.Since(start).Microseconds()) / 1000.0,
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		}
		if raw := c.Request.URL.RawQuery; raw != "" {
			attrs = append(attrs, "query", raw)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}

		log := logging.FromContext(c.Request.Context())
		switch {
		case status >= 500:
			log.Error("request completed", attrs...)
		case status >= 400:
			log.Warn("request completed", attrs...)
		default:
			log.Info("request completed", attrs...)
		}
	}
}
