package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
)

// RateLimitConfig configures the per-client token bucket.
type RateLimitConfig struct {
	RPS   float64       // sustained requests per second per client
	Burst int           // requests allowed in a burst
	TTL   time.Duration // how long an idle client's bucket is kept
}

// Limiter decides whether a request from key is allowed through.
//
// It exists so the in-process limiter below can be swapped for a shared one
// (Redis, or a gateway) without touching the middleware or the routes. That is
// the seam to reach for the moment there is more than one replica.
type Limiter interface {
	Allow(key string) bool
}

// IPRateLimiter keeps one token bucket per client IP.
//
// This is deliberately in-process: it needs no infrastructure and protects a
// single instance. Behind more than one replica, move this to a shared store.
type IPRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     rate.Limit
	burst   int
	ttl     time.Duration
	lastGC  time.Time
	nowFunc func() time.Time
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

// NewIPRateLimiter builds an in-process limiter. Construct one per policy: the
// public API and the auth endpoints want very different limits.
func NewIPRateLimiter(cfg RateLimitConfig) *IPRateLimiter {
	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Minute
	}
	return &IPRateLimiter{
		buckets: make(map[string]*bucket),
		rps:     rate.Limit(cfg.RPS),
		burst:   cfg.Burst,
		ttl:     cfg.TTL,
		nowFunc: time.Now,
	}
}

// Allow reports whether the client has budget left, and spends it if so.
func (l *IPRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()
	l.gcLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now

	return b.limiter.Allow()
}

// gcLocked drops buckets for clients that have gone quiet, so the map cannot
// grow without bound. Callers must hold l.mu.
func (l *IPRateLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.ttl {
		return
	}
	l.lastGC = now
	for key, b := range l.buckets {
		if now.Sub(b.seen) > l.ttl {
			delete(l.buckets, key)
		}
	}
}

// RateLimit throttles each client IP, responding 429 with the standard envelope
// once the budget is spent.
//
// Keying on ClientIP is only as trustworthy as the proxy configuration: with no
// trusted proxies set, it is the direct peer and cannot be spoofed. It also
// means an attacker with many addresses is not stopped by this alone, which is
// why the auth endpoints are slow by policy rather than merely rate limited.
func RateLimit(limiter Limiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow(c.ClientIP()) {
			abortWithError(c, http.StatusTooManyRequests, apperr.ErrRateLimit.Message)
			return
		}
		c.Next()
	}
}

var _ Limiter = (*IPRateLimiter)(nil)
