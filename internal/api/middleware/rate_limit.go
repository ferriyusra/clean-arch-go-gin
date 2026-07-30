package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/response"
)

// RateLimiter is a fixed-window request counter keyed by client IP.
//
// State lives in process memory, which is enough for a single instance and for
// blunting credential-stuffing against the auth endpoints. Behind more than one
// replica each instance counts separately, so move the counter to Redis (or a
// gateway-level limiter) before relying on it as a hard guarantee.
type RateLimiter struct {
	mu        sync.Mutex
	windows   map[string]*rateWindow
	limit     int
	window    time.Duration
	nextSweep time.Time
}

type rateWindow struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a limiter allowing `limit` requests per `window` per IP.
// A non-positive limit disables limiting entirely.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		windows: make(map[string]*rateWindow),
		limit:   limit,
		window:  window,
	}
}

// allow reports whether the key may proceed, and when its window resets.
func (l *RateLimiter) allow(key string, now time.Time) (bool, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	w, exists := l.windows[key]
	if !exists || now.After(w.resetAt) {
		w = &rateWindow{resetAt: now.Add(l.window)}
		l.windows[key] = w
	}

	w.count++
	return w.count <= l.limit, w.resetAt
}

// sweep drops finished windows so the map cannot grow without bound as new IPs
// arrive. Callers must hold the lock.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Before(l.nextSweep) {
		return
	}
	for key, w := range l.windows {
		if now.After(w.resetAt) {
			delete(l.windows, key)
		}
	}
	l.nextSweep = now.Add(l.window)
}

// Middleware rejects requests from a client IP that exceeded the window with a
// 429 and a Retry-After header.
func (l *RateLimiter) Middleware() gin.HandlerFunc {
	if l.limit <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		now := time.Now()
		allowed, resetAt := l.allow(c.ClientIP(), now)
		if !allowed {
			retryAfter := int(time.Until(resetAt).Seconds()) + 1
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, response.Err("Too many requests, please try again later"))
			return
		}
		c.Next()
	}
}
