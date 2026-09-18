package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets conservative defaults for a JSON API.
//
// The CSP is deliberately restrictive: this server returns JSON, never HTML, so
// there is nothing legitimate for a browser to load from it.
func SecurityHeaders(devMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-site")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// HSTS only makes sense over TLS, which dev mode does not use.
		if !devMode {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}
