package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/api/middleware"
	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// captureLogger returns a logger writing JSON into buf, so tests can assert on
// what was actually logged rather than on the logger being called.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestRequestIDGeneratesAndEchoesAnID(t *testing.T) {
	var buf bytes.Buffer

	r := testutil.NewEngine(t)
	r.Use(middleware.RequestID(captureLogger(&buf)))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"request_id": middleware.GetRequestID(c)})
	})

	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/ping", nil))

	testutil.Equal(t, rec.Code, http.StatusOK, "status")

	header := rec.Header().Get(middleware.RequestIDHeader)
	testutil.True(t, header != "", "response carries a request id header")

	var body struct {
		RequestID string `json:"request_id"`
	}
	decodeJSON(t, rec, &body)
	testutil.Equal(t, body.RequestID, header, "context id matches the echoed header")
}

func TestRequestIDPropagatesAnIncomingID(t *testing.T) {
	r := testutil.NewEngine(t)
	r.Use(middleware.RequestID(captureLogger(&bytes.Buffer{})))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"request_id": middleware.GetRequestID(c)})
	})

	req := testutil.JSONRequest(t, http.MethodGet, "/ping", nil)
	req.Header.Set(middleware.RequestIDHeader, "upstream-id-123")

	rec := testutil.Do(r, req)

	// An id supplied by a gateway must survive, or traces break at the edge.
	testutil.Equal(t, rec.Header().Get(middleware.RequestIDHeader), "upstream-id-123", "echoed id")
}

func TestRequestIDPutsACorrelatedLoggerInContext(t *testing.T) {
	var buf bytes.Buffer

	r := testutil.NewEngine(t)
	r.Use(middleware.RequestID(captureLogger(&buf)))
	r.GET("/ping", func(c *gin.Context) {
		// A service or repository would reach the logger exactly this way,
		// through the ctx it already receives.
		logging.FromContext(c.Request.Context()).Info("work happened")
		c.JSON(http.StatusOK, gin.H{})
	})

	req := testutil.JSONRequest(t, http.MethodGet, "/ping", nil)
	req.Header.Set(middleware.RequestIDHeader, "corr-1")
	testutil.Do(r, req)

	logged := buf.String()
	testutil.True(t, strings.Contains(logged, "work happened"), "the log line was emitted")
	testutil.True(t, strings.Contains(logged, "corr-1"), "the log line carries the request id")
}

func TestAccessLogRecordsTheOutcome(t *testing.T) {
	var buf bytes.Buffer

	r := testutil.NewEngine(t)
	r.Use(middleware.RequestID(captureLogger(&buf)))
	r.Use(middleware.AccessLog())
	r.GET("/teapot", func(c *gin.Context) {
		c.JSON(http.StatusTeapot, gin.H{})
	})

	testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/teapot", nil))

	logged := buf.String()
	testutil.True(t, strings.Contains(logged, "request completed"), "access log line emitted")
	testutil.True(t, strings.Contains(logged, "418"), "status recorded")
	testutil.True(t, strings.Contains(logged, "/teapot"), "path recorded")
}

func TestRecoveryReturnsTheStandardEnvelope(t *testing.T) {
	var buf bytes.Buffer

	r := testutil.NewEngine(t)
	r.Use(middleware.RequestID(captureLogger(&buf)))
	r.Use(middleware.Recovery())
	r.GET("/boom", func(*gin.Context) {
		panic("something went very wrong")
	})

	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/boom", nil))

	testutil.Equal(t, rec.Code, http.StatusInternalServerError, "status")

	envelope := testutil.Envelope(t, rec)
	testutil.Equal(t, envelope.Success, false, "success flag")
	testutil.Equal(t, envelope.Message, "Internal server error", "message")

	// The panic value belongs in the log, never in the response body.
	testutil.True(t, strings.Contains(buf.String(), "something went very wrong"),
		"panic value is logged")
	testutil.True(t, !strings.Contains(rec.Body.String(), "something went very wrong"),
		"panic value is not returned to the client")
}

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name     string
		devMode  bool
		wantHSTS bool
	}{
		{name: "production sets HSTS", devMode: false, wantHSTS: true},
		{name: "development omits HSTS because there is no TLS", devMode: true, wantHSTS: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testutil.NewEngine(t)
			r.Use(middleware.SecurityHeaders(tt.devMode))
			r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

			rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/ping", nil))

			testutil.Equal(t, rec.Header().Get("X-Content-Type-Options"), "nosniff", "nosniff")
			testutil.Equal(t, rec.Header().Get("X-Frame-Options"), "DENY", "frame options")
			testutil.Equal(t, rec.Header().Get("Referrer-Policy"), "no-referrer", "referrer policy")

			hasHSTS := rec.Header().Get("Strict-Transport-Security") != ""
			testutil.Equal(t, hasHSTS, tt.wantHSTS, "HSTS present")
		})
	}
}

func TestRateLimitRejectsOnceTheBurstIsSpent(t *testing.T) {
	r := testutil.NewEngine(t)
	r.Use(middleware.RateLimit(middleware.NewIPRateLimiter(middleware.RateLimitConfig{RPS: 0.0001, Burst: 2})))
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

	// The bucket starts full, so exactly Burst requests succeed.
	for i := 0; i < 2; i++ {
		rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/ping", nil))
		testutil.Equal(t, rec.Code, http.StatusOK, "request within the burst")
	}

	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/ping", nil))
	testutil.Equal(t, rec.Code, http.StatusTooManyRequests, "request beyond the burst")
	testutil.Equal(t, testutil.Envelope(t, rec).Message, "Too many requests", "message")
}

func TestTimeoutCancelsTheRequestContext(t *testing.T) {
	r := testutil.NewEngine(t)
	r.Use(middleware.Timeout(20 * time.Millisecond))
	r.GET("/slow", func(c *gin.Context) {
		// Services observe the deadline through the ctx they already take.
		<-c.Request.Context().Done()
		c.JSON(http.StatusGatewayTimeout, gin.H{"err": c.Request.Context().Err().Error()})
	})

	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/slow", nil))

	testutil.Equal(t, rec.Code, http.StatusGatewayTimeout, "status")
	testutil.True(t, strings.Contains(rec.Body.String(), "deadline exceeded"), "deadline reported")
}

func TestBodyLimitRejectsAnOversizedBody(t *testing.T) {
	r := testutil.NewEngine(t)
	r.Use(middleware.BodyLimit(16))
	r.POST("/upload", func(c *gin.Context) {
		var payload map[string]string
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"err": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	big := strings.Repeat("x", 128)
	rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodPost, "/upload", map[string]string{"k": big}))

	testutil.Equal(t, rec.Code, http.StatusRequestEntityTooLarge, "status")
}

// TestRequestIDRejectsUntrustworthyClientValues covers the hardening around a
// client-supplied id: it is echoed on the response and written to every log
// line for the request, so an unbounded or exotic value is a way to pollute the
// logs of whoever is reading them.
func TestRequestIDRejectsUntrustworthyClientValues(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		accepted bool
	}{
		{name: "plain identifier", incoming: "abc-123_x.y", accepted: true},
		{name: "a uuid", incoming: "3c33ab85-4e6d-4bbc-a2a4-7a03016869e6", accepted: true},
		{name: "spaces", incoming: "not a valid id"},
		{name: "path separators", incoming: "../../etc/passwd"},
		{name: "percent-encoded newline", incoming: "id%0Ainjected"},
		{name: "quotes that would break a log parser", incoming: "id\"}{"},
		{name: "over the length cap", incoming: strings.Repeat("a", 129)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testutil.NewEngine(t)
			r.Use(middleware.RequestID(captureLogger(&bytes.Buffer{})))
			r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

			req := testutil.JSONRequest(t, http.MethodGet, "/ping", nil)
			req.Header.Set(middleware.RequestIDHeader, tt.incoming)

			rec := testutil.Do(r, req)
			got := rec.Header().Get(middleware.RequestIDHeader)

			if tt.accepted {
				testutil.Equal(t, got, tt.incoming, "the id is propagated")
				return
			}

			// A rejected id is replaced, not cleaned: a sanitized value would
			// still be attacker-shaped and would no longer match what the
			// client believes it sent.
			testutil.True(t, got != tt.incoming, "the id is not echoed back")
			testutil.Equal(t, len(got), 36, "a fresh uuid is issued instead")
		})
	}
}

// stubLimiter records what it was asked about and answers from a script, so a
// test can check the middleware rather than the bucket arithmetic.
type stubLimiter struct {
	allow bool
	keys  []string
}

func (s *stubLimiter) Allow(key string) bool {
	s.keys = append(s.keys, key)
	return s.allow
}

// TestRateLimitIsPluggable pins the seam that lets a shared limiter replace the
// in-process one once there is more than one replica.
func TestRateLimitIsPluggable(t *testing.T) {
	tests := []struct {
		name       string
		allow      bool
		wantStatus int
	}{
		{name: "a limiter that allows lets the request through", allow: true, wantStatus: http.StatusOK},
		{name: "a limiter that refuses returns 429", wantStatus: http.StatusTooManyRequests},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := &stubLimiter{allow: tt.allow}

			r := testutil.NewEngine(t)
			r.Use(middleware.RateLimit(limiter))
			r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

			rec := testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/ping", nil))

			testutil.Equal(t, rec.Code, tt.wantStatus, "status")
			testutil.Equal(t, len(limiter.keys), 1, "the limiter was consulted once")
			testutil.True(t, limiter.keys[0] != "", "it was given a client key")
		})
	}
}

// Separate limiters must not share a budget, which is the whole point of giving
// the auth endpoints their own.
func TestRateLimitBudgetsAreIndependent(t *testing.T) {
	global := middleware.NewIPRateLimiter(middleware.RateLimitConfig{RPS: 0.0001, Burst: 1})
	auth := middleware.NewIPRateLimiter(middleware.RateLimitConfig{RPS: 0.0001, Burst: 1})

	r := testutil.NewEngine(t)
	r.GET("/public", middleware.RateLimit(global), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })
	r.POST("/login", middleware.RateLimit(auth), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

	// Spend the public budget entirely.
	testutil.Equal(t, testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/public", nil)).Code,
		http.StatusOK, "first public request")
	testutil.Equal(t, testutil.Do(r, testutil.JSONRequest(t, http.MethodGet, "/public", nil)).Code,
		http.StatusTooManyRequests, "second public request")

	// The auth endpoint still has its own.
	testutil.Equal(t, testutil.Do(r, testutil.JSONRequest(t, http.MethodPost, "/login", nil)).Code,
		http.StatusOK, "first login attempt")
	testutil.Equal(t, testutil.Do(r, testutil.JSONRequest(t, http.MethodPost, "/login", nil)).Code,
		http.StatusTooManyRequests, "second login attempt")
}
