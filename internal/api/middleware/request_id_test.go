package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRequestIDGeneratesAndEchoesID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var seen string
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) {
		seen = GetRequestIDFromContext(c)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("expected a request ID in the context")
	}
	if _, err := uuid.Parse(seen); err != nil {
		t.Errorf("expected a generated UUID, got %q", seen)
	}
	if got := w.Header().Get(RequestIDHeader); got != seen {
		t.Errorf("expected response header %q to be %q, got %q", RequestIDHeader, seen, got)
	}
}

func TestRequestIDReusesInboundID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const inbound = "trace-from-upstream-proxy"

	var seen string
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) {
		seen = GetRequestIDFromContext(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, inbound)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if seen != inbound {
		t.Errorf("expected the inbound ID %q to be reused, got %q", inbound, seen)
	}
	if got := w.Header().Get(RequestIDHeader); got != inbound {
		t.Errorf("expected the inbound ID echoed back, got %q", got)
	}
}

func TestGetRequestIDFromContextWithoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if got := GetRequestIDFromContext(c); got != "" {
		t.Errorf("expected an empty ID when the middleware did not run, got %q", got)
	}
}
