package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// abortWithError stops the chain and writes the standard envelope, tagged with
// the identifiers that lead back to this request in the logs and the trace.
//
// Middleware rejections (missing token, bad CSRF, rate limit, panic) are the
// responses a user is most likely to report, so they are exactly the ones that
// need to be traceable.
func abortWithError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, response.Err(message).
		WithCorrelation(GetRequestID(c), GetTraceID(c)))
}
