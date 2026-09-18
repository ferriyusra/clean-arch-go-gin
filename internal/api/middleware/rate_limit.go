package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/response"
)

// RateLimitConfig configures the per-client token bucket.
type RateLimitConfig struct {
	RPS   float64       // sustained requests per second per client
	Burst int           // requests allowed in a burst
	TTL   time.Duration // how long an idle client's bucket is kept
}

// ipRateLimiter keeps one token bucket per client IP.
//
// This is deliberately in-process: it needs no infrastructure and protects a
// single instance. Behind more than one replica, move this to a shared store.
type ipRateLimiter struct {
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

func newIPRateLimiter(cfg RateLimitConfig) *ipRateLimiter {
	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Minute
	}
	return &ipRateLimiter{
		buckets: make(map[string]*bucket),
		rps:     rate.Limit(cfg.RPS),
		burst:   cfg.Burst,
		ttl:     cfg.TTL,
		nowFunc: time.Now,
	}
}

func (l *ipRateLimiter) allow(key string) bool {
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
func (l *ipRateLimiter) gcLocked(now time.Time) {
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

// RateLimit throttles each client IP to a sustained rate with a burst
// allowance, responding 429 with the standard envelope once exceeded.
func RateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	limiter := newIPRateLimiter(cfg)

	return func(c *gin.Context) {
		if !limiter.allow(c.ClientIP()) {
			c.AbortWithStatusJSON(
				http.StatusTooManyRequests,
				response.Err(apperr.ErrRateLimit.Message),
			)
			return
		}
		c.Next()
	}
}
