package observability_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ferriyusra/clean-arch-go-gin/internal/observability"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// sample is one gathered time series, flattened so the assertions below read
// as plain Go rather than as protobuf accessors.
type sample struct {
	labels map[string]string
	// value is the counter's value, or a histogram's observation count.
	value float64
}

func (s sample) has(want map[string]string) bool {
	for key, value := range want {
		if s.labels[key] != value {
			return false
		}
	}
	return true
}

// newIsolatedMetrics gives each test its own registry, so one test's requests
// can never be counted by another.
func newIsolatedMetrics(t *testing.T) *observability.Metrics {
	t.Helper()
	return observability.NewMetricsWithRegistry(prometheus.NewRegistry())
}

// gather scrapes the registry and returns every series of the named metric.
func gather(t *testing.T, metrics *observability.Metrics, name string) []sample {
	t.Helper()

	families, err := metrics.Registry().Gather()
	testutil.NoError(t, err)

	var found []sample
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}

			value := metric.GetCounter().GetValue()
			if metric.GetHistogram() != nil {
				value = float64(metric.GetHistogram().GetSampleCount())
			}
			found = append(found, sample{labels: labels, value: value})
		}
	}
	return found
}

// find returns the single series carrying every label in want.
func find(t *testing.T, metrics *observability.Metrics, name string, want map[string]string) sample {
	t.Helper()

	for _, s := range gather(t, metrics, name) {
		if s.has(want) {
			return s
		}
	}
	t.Fatalf("no %s series matching %v", name, want)
	return sample{}
}

func metricNames(t *testing.T, metrics *observability.Metrics) map[string]bool {
	t.Helper()

	families, err := metrics.Registry().Gather()
	testutil.NoError(t, err)

	names := make(map[string]bool, len(families))
	for _, family := range families {
		names[family.GetName()] = true
	}
	return names
}

// TestMiddlewareLabelsWithTheRoutePatternNotTheRawPath is the whole reason the
// middleware reads c.FullPath(): labelling with the raw path would give every
// id in every URL its own time series, so a handful of routes would become
// millions of series and take the Prometheus server down with them.
func TestMiddlewareLabelsWithTheRoutePatternNotTheRawPath(t *testing.T) {
	tests := []struct {
		name      string
		register  string
		requests  []string
		wantRoute string
	}{
		{
			name:      "a static route keeps its own pattern",
			register:  "/api/counter",
			requests:  []string{"/api/counter"},
			wantRoute: "/api/counter",
		},
		{
			name:     "every id collapses onto one parameterised pattern",
			register: "/api/users/:id",
			requests: []string{
				"/api/users/0f8fad5b-d9cb-469f-a165-70867728950e",
				"/api/users/7c9e6679-7425-40de-944b-e07fc1f90ae7",
				"/api/users/3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			},
			wantRoute: "/api/users/:id",
		},
		{
			// An unmatched route must not be labelled with what the caller
			// asked for, or an attacker picks our label values.
			name:      "an unmatched route lands under the literal fallback",
			register:  "/api/counter",
			requests:  []string{"/not-a-route", "/wp-login.php"},
			wantRoute: observability.RouteUnmatched,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := newIsolatedMetrics(t)

			engine := testutil.NewEngine(t)
			engine.Use(metrics.Middleware())
			engine.GET(tt.register, func(c *gin.Context) { c.Status(http.StatusOK) })
			// gin only runs the middleware chain for an unmatched route when a
			// NoRoute handler exists, which the real router installs.
			engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })

			for _, target := range tt.requests {
				testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, target, nil))
			}

			series := gather(t, metrics, "http_requests_total")
			testutil.Equal(t, len(series), 1, "the requests collapse onto one time series")
			testutil.Equal(t, series[0].labels["route"], tt.wantRoute, "route label")
			testutil.Equal(t, series[0].value, float64(len(tt.requests)), "every request counted")
		})
	}
}

func TestMiddlewareRecordsMethodAndStatus(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		handler    gin.HandlerFunc
		wantStatus string
	}{
		{
			name:       "a successful GET",
			method:     http.MethodGet,
			handler:    func(c *gin.Context) { c.Status(http.StatusOK) },
			wantStatus: "200",
		},
		{
			name:       "a failing POST",
			method:     http.MethodPost,
			handler:    func(c *gin.Context) { c.Status(http.StatusInternalServerError) },
			wantStatus: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := newIsolatedMetrics(t)

			engine := testutil.NewEngine(t)
			engine.Use(metrics.Middleware())
			engine.Handle(tt.method, "/api/thing", tt.handler)

			testutil.Do(engine, testutil.JSONRequest(t, tt.method, "/api/thing", nil))

			got := find(t, metrics, "http_requests_total", map[string]string{
				"method": tt.method,
				"route":  "/api/thing",
				"status": tt.wantStatus,
			})
			testutil.Equal(t, got.value, 1.0, "request counted once")
		})
	}
}

// TestMiddlewareRecordsLatency pins the histogram's name and labels: the
// dashboards and alerts are written against those, so a rename is a breaking
// change even though nothing fails to compile.
func TestMiddlewareRecordsLatency(t *testing.T) {
	metrics := newIsolatedMetrics(t)

	engine := testutil.NewEngine(t)
	engine.Use(metrics.Middleware())
	engine.GET("/api/counter", func(c *gin.Context) { c.Status(http.StatusOK) })

	testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/api/counter", nil))
	testutil.Do(engine, testutil.JSONRequest(t, http.MethodGet, "/api/counter", nil))

	got := find(t, metrics, "http_request_duration_seconds", map[string]string{
		"method": http.MethodGet,
		"route":  "/api/counter",
	})
	testutil.Equal(t, got.value, 2.0, "both requests observed")

	// The histogram must not carry a status label: splitting latency by status
	// code multiplies the bucket count for no analytical gain.
	if _, ok := got.labels["status"]; ok {
		t.Error("http_request_duration_seconds must not be labelled by status")
	}
}

// TestNewMetricsIncludesRuntimeCollectors makes sure the endpoint is useful on
// its own: goroutine counts and GC behaviour explain most incidents that the
// request counters only hint at.
func TestNewMetricsIncludesRuntimeCollectors(t *testing.T) {
	names := metricNames(t, observability.NewMetrics())

	for _, want := range []string{"go_goroutines", "go_memstats_alloc_bytes", "go_gc_duration_seconds"} {
		testutil.True(t, names[want], "runtime collector exports "+want)
	}
}
