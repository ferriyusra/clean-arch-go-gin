package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newRateLimitedRouter(limiter *RateLimiter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Trust no proxies so ClientIP comes from RemoteAddr and cannot be spoofed
	// through X-Forwarded-For.
	_ = r.SetTrustedProxies(nil)
	r.POST("/login", limiter.Middleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func doRequest(r *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimiterAllowsUpToLimit(t *testing.T) {
	r := newRateLimitedRouter(NewRateLimiter(3, time.Minute))

	for i := 1; i <= 3; i++ {
		if w := doRequest(r, "10.0.0.1:1234"); w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}

	w := doRequest(r, "10.0.0.1:1234")
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after exceeding the limit, got %d", w.Code)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("expected a Retry-After header on a 429")
	} else if seconds, err := strconv.Atoi(retryAfter); err != nil || seconds <= 0 {
		t.Errorf("expected a positive Retry-After, got %q", retryAfter)
	}
}

func TestRateLimiterIsolatesClients(t *testing.T) {
	r := newRateLimitedRouter(NewRateLimiter(1, time.Minute))

	if w := doRequest(r, "10.0.0.1:1111"); w.Code != http.StatusOK {
		t.Fatalf("first client: expected 200, got %d", w.Code)
	}
	if w := doRequest(r, "10.0.0.1:2222"); w.Code != http.StatusTooManyRequests {
		t.Errorf("same IP on a new port shares the budget: expected 429, got %d", w.Code)
	}
	// A different IP has its own window.
	if w := doRequest(r, "10.0.0.2:1111"); w.Code != http.StatusOK {
		t.Errorf("second client: expected 200, got %d", w.Code)
	}
}

func TestRateLimiterWindowResets(t *testing.T) {
	r := newRateLimitedRouter(NewRateLimiter(1, 50*time.Millisecond))

	if w := doRequest(r, "10.0.0.1:1234"); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w := doRequest(r, "10.0.0.1:1234"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	time.Sleep(70 * time.Millisecond)

	if w := doRequest(r, "10.0.0.1:1234"); w.Code != http.StatusOK {
		t.Errorf("expected 200 after the window elapsed, got %d", w.Code)
	}
}

func TestRateLimiterDisabledWhenLimitNotPositive(t *testing.T) {
	r := newRateLimitedRouter(NewRateLimiter(0, time.Minute))

	for i := 0; i < 20; i++ {
		if w := doRequest(r, "10.0.0.1:1234"); w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 with limiting disabled, got %d", i, w.Code)
		}
	}
}

// TestRateLimiterSweepsFinishedWindows checks the map does not keep an entry per
// IP forever, which would be an unbounded memory leak under scanning traffic.
func TestRateLimiterSweepsFinishedWindows(t *testing.T) {
	limiter := NewRateLimiter(1, 10*time.Millisecond)
	now := time.Now()

	for i := 0; i < 100; i++ {
		limiter.allow("10.0.0."+strconv.Itoa(i), now)
	}

	limiter.mu.Lock()
	before := len(limiter.windows)
	limiter.mu.Unlock()
	if before != 100 {
		t.Fatalf("expected 100 tracked windows, got %d", before)
	}

	// Advance past both the windows and the sweep interval.
	limiter.allow("10.1.0.1", now.Add(time.Second))

	limiter.mu.Lock()
	after := len(limiter.windows)
	limiter.mu.Unlock()
	if after != 1 {
		t.Errorf("expected finished windows to be swept, got %d entries", after)
	}
}

// TestRateLimiterConcurrentAccess exercises the limiter under -race: exactly
// `limit` callers must be admitted no matter how the goroutines interleave.
func TestRateLimiterConcurrentAccess(t *testing.T) {
	const (
		limit    = 10
		requests = 200
	)

	limiter := NewRateLimiter(limit, time.Minute)
	now := time.Now()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := limiter.allow("10.0.0.1", now); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != limit {
		t.Errorf("expected exactly %d allowed requests, got %d", limit, allowed)
	}
}
