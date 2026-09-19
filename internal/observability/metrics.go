// Package observability exposes the process's operational signals: Prometheus
// metrics and the stdlib pprof profiles.
//
// The package is deliberately free-standing: it imports nothing from the rest
// of the application, takes a plain Config rather than the platform one, and
// hands back an *http.Server the caller owns. That keeps it trivially testable
// and keeps the wiring decision (ports, which signals are on) with the caller.
//
// The signals are served on their own listener, never on the public router.
// /debug/pprof lets an unauthenticated caller dump the heap, stall the process
// for a thirty-second CPU profile, or read a full goroutine dump, and /metrics
// leaks operational detail (route names, traffic shape, build info). Neither
// belongs on a port the internet can reach.
package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RouteUnmatched labels a request that matched no registered route.
//
// Those requests still have to be counted -- a flood of 404s is exactly the
// kind of thing metrics exist to show -- but they must not be labelled with the
// path they asked for, because an attacker would then be choosing our label
// values and could mint an unbounded number of time series.
const RouteUnmatched = "unmatched"

// MethodOther labels a request whose HTTP method is not one of the standard
// verbs.
//
// This closes the same hole RouteUnmatched closes, on the other label. net/http
// only checks that a method is a valid HTTP *token*, not that it is a verb
// anyone has heard of, so `curl -X SLURP` reaches the router, falls through to
// the no-route handler and would otherwise mint a brand new time series. The
// token grammar allows arbitrary length, so an unauthenticated caller could
// mint them without limit and take the Prometheus server down. promhttp
// sanitises its own method label the same way.
const MethodOther = "other"

// standardMethods is the whitelist. Anything outside it becomes MethodOther.
var standardMethods = map[string]struct{}{
	http.MethodGet: {}, http.MethodHead: {}, http.MethodPost: {},
	http.MethodPut: {}, http.MethodPatch: {}, http.MethodDelete: {},
	http.MethodConnect: {}, http.MethodOptions: {}, http.MethodTrace: {},
}

// sanitizeMethod bounds the method label to a fixed set of values.
func sanitizeMethod(method string) string {
	if _, ok := standardMethods[method]; ok {
		return method
	}
	return MethodOther
}

// Metrics owns a Prometheus registry and the instruments recorded by the gin
// middleware.
//
// The registry is explicit rather than the promauto/global default so that a
// test can build an isolated one and assert on exactly what this middleware
// recorded, with no leakage between tests.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics builds a Metrics with its own registry, pre-loaded with the Go
// runtime and process collectors so the endpoint also reports goroutines, heap
// and GC rather than only this service's own counters.
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		// Process metrics (RSS, open file descriptors, CPU seconds) are only
		// available on some platforms. The collector reports nothing rather
		// than erroring elsewhere, so registering it unconditionally is safe.
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return NewMetricsWithRegistry(registry)
}

// NewMetricsWithRegistry registers the HTTP instruments on an existing
// registry. Use it when the caller wants to control what else is collected --
// a test, typically, which wants the HTTP series and nothing else.
//
// It panics if the instruments are already registered on reg, which is the
// same contract as prometheus.MustRegister: a duplicate registration is a
// programming error, not a runtime condition.
func NewMetricsWithRegistry(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		registry: reg,
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests handled, by method, route pattern and status code.",
			},
			[]string{"method", "route", "status"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request latency in seconds, by method and route pattern.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
	}

	reg.MustRegister(m.requests, m.duration)
	return m
}

// Registry returns the registry these instruments are registered on, so the
// caller can add collectors of its own or scrape it directly in a test.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// Handler returns the /metrics handler for this registry.
//
// ContinueOnError rather than the default HTTPErrorOnError: one collector that
// is unhappy (the process collector on an unsupported platform, say) should
// degrade the scrape, not turn the whole endpoint into a 500 and blind the
// operator to everything else.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
}

// Middleware records a count and a latency observation for every request.
//
// The route label is c.FullPath(), the *pattern* the router matched
// ("/api/users/:id"), never c.Request.URL.Path. Labelling with the raw path
// would give every user id its own time series; a handful of routes would
// become millions of series and take the Prometheus server with them.
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		// FullPath is only populated once the router has matched, so it is read
		// after c.Next rather than before.
		route := c.FullPath()
		if route == "" {
			route = RouteUnmatched
		}

		method := sanitizeMethod(c.Request.Method)
		elapsed := time.Since(start).Seconds()

		m.requests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
		m.duration.WithLabelValues(method, route).Observe(elapsed)
	}
}
