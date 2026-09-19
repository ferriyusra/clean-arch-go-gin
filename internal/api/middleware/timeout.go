package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout bounds how long downstream work may run by replacing the request
// context with a deadline-bearing one.
//
// Services already select on ctx.Done(), so they observe the deadline and
// return early; the handler then maps context.DeadlineExceeded to 504.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
