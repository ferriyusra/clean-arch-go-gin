package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
)

// Recovery turns a panic into the standard APIResponse envelope.
//
// gin's built-in recovery writes an empty body (or an HTML stack trace in debug
// mode), which breaks clients that always parse the envelope.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				logging.FromContext(c.Request.Context()).Error("panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
				)

				abortWithError(c, http.StatusInternalServerError, apperr.ErrInternal.Message)
			}
		}()

		c.Next()
	}
}
