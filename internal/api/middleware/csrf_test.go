package middleware_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/csrf"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

func TestCSRFMiddleware(t *testing.T) {
	csrfService := csrf.NewCSRFService("test-csrf-secret")

	validToken, err := csrfService.GenerateToken()
	testutil.NoError(t, err)

	foreignToken, err := csrf.NewCSRFService("another-csrf-secret").GenerateToken()
	testutil.NoError(t, err)

	tests := []struct {
		name       string
		method     string
		header     string
		wantStatus int
		wantMsg    string
	}{
		{
			// Safe methods carry no CSRF risk, so requiring a token on them
			// would only break clients.
			name:       "lets GET through without a token",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "accepts a valid token on POST",
			method:     http.MethodPost,
			header:     validToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "rejects POST with no token",
			method:     http.MethodPost,
			wantStatus: http.StatusForbidden,
			wantMsg:    "Missing CSRF token",
		},
		{
			name:       "rejects POST with a garbage token",
			method:     http.MethodPost,
			header:     "garbage",
			wantStatus: http.StatusForbidden,
			wantMsg:    "Invalid CSRF token",
		},
		{
			name:       "rejects a token signed with another secret",
			method:     http.MethodPost,
			header:     foreignToken,
			wantStatus: http.StatusForbidden,
			wantMsg:    "Invalid CSRF token",
		},
		{
			name:       "protects PUT",
			method:     http.MethodPut,
			wantStatus: http.StatusForbidden,
			wantMsg:    "Missing CSRF token",
		},
		{
			name:       "protects PATCH",
			method:     http.MethodPatch,
			wantStatus: http.StatusForbidden,
			wantMsg:    "Missing CSRF token",
		},
		{
			name:       "protects DELETE",
			method:     http.MethodDelete,
			wantStatus: http.StatusForbidden,
			wantMsg:    "Missing CSRF token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testutil.NewEngine(t)
			r.Handle(tt.method, "/resource", middleware.CSRFMiddleware(csrfService), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := testutil.JSONRequest(t, tt.method, "/resource", nil)
			if tt.header != "" {
				req.Header.Set("X-CSRF-Token", tt.header)
			}

			rec := testutil.Do(r, req)

			testutil.Equal(t, rec.Code, tt.wantStatus, "status")
			if tt.wantMsg != "" {
				testutil.Equal(t, testutil.Envelope(t, rec).Message, tt.wantMsg, "message")
			}
		})
	}
}
