package observability_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gin-gonic/gin"

	"github.com/ferriyusra/clean-arch-go-gin/internal/observability"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// get runs a request against the admin mux and returns the recorder.
func get(t *testing.T, mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestAdminMuxServesOnlyWhatIsEnabled is the security-relevant case: pprof
// must be absent, not merely undocumented, when it is switched off.
func TestAdminMuxServesOnlyWhatIsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		cfg        observability.Config
		target     string
		wantStatus int
	}{
		{
			name:       "metrics are served when enabled",
			cfg:        observability.Config{MetricsEnabled: true},
			target:     observability.MetricsPath,
			wantStatus: http.StatusOK,
		},
		{
			name:       "metrics are absent when disabled",
			cfg:        observability.Config{PprofEnabled: true},
			target:     observability.MetricsPath,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "pprof is absent when disabled",
			cfg:        observability.Config{MetricsEnabled: true},
			target:     observability.PprofPrefix,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "a named pprof profile is absent when pprof is disabled",
			cfg:        observability.Config{MetricsEnabled: true},
			target:     observability.PprofPrefix + "heap",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "cmdline is absent when pprof is disabled",
			cfg:        observability.Config{MetricsEnabled: true},
			target:     observability.PprofPrefix + "cmdline",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "pprof index is served when enabled",
			cfg:        observability.Config{PprofEnabled: true},
			target:     observability.PprofPrefix,
			wantStatus: http.StatusOK,
		},
		{
			name:       "a named pprof profile is served through the index handler",
			cfg:        observability.Config{PprofEnabled: true},
			target:     observability.PprofPrefix + "goroutine?debug=1",
			wantStatus: http.StatusOK,
		},
		{
			name:       "healthz is always served",
			cfg:        observability.Config{MetricsEnabled: true},
			target:     observability.HealthPath,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := observability.NewAdminMux(tt.cfg, newIsolatedMetrics(t))

			rec := get(t, mux, tt.target)

			testutil.Equal(t, rec.Code, tt.wantStatus, tt.target)
		})
	}
}

// TestAdminMuxMetricsBodyIsPrometheusText guards the scrape contract: a 200
// with the wrong body is worse than a 404, because the scraper reports success.
func TestAdminMuxMetricsBodyIsPrometheusText(t *testing.T) {
	metrics := newIsolatedMetrics(t)

	// A real request is recorded first. Scraping an untouched registry exports
	// no http_* series at all — a CounterVec with no children exports nothing —
	// so without this the assertions below would pass against a handler that
	// returned an empty body with the right content type.
	r := gin.New()
	r.Use(metrics.Middleware())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	mux := observability.NewAdminMux(observability.Config{MetricsEnabled: true}, metrics)

	rec := get(t, mux, observability.MetricsPath)

	testutil.Equal(t, rec.Code, http.StatusOK, "metrics status")
	testutil.True(t, strings.Contains(rec.Header().Get("Content-Type"), "text/plain"),
		"metrics are exported as Prometheus text")

	// The body has to carry the exposition format, not merely claim the type.
	body := rec.Body.String()
	for _, want := range []string{
		"# HELP http_requests_total",
		"# TYPE http_requests_total counter",
		`http_requests_total{method="GET",route="/ping",status="200"} 1`,
		"# TYPE http_request_duration_seconds histogram",
	} {
		testutil.True(t, strings.Contains(body, want), "the scrape contains "+want)
	}
}

// TestAdminMuxIsIndependentOfDefaultServeMux pins the reason the handlers are
// registered by hand.
//
// Importing net/http/pprof at all registers every profile on
// http.DefaultServeMux -- that side effect cannot be opted out of, as the first
// assertion below records. The defence is therefore not to avoid the
// registration but never to serve DefaultServeMux: the admin mux is built from
// http.NewServeMux and routes only what Config enables, so switching pprof off
// really does make it unreachable rather than merely undocumented.
func TestAdminMuxIsIndependentOfDefaultServeMux(t *testing.T) {
	_, pattern := http.DefaultServeMux.Handler(
		httptest.NewRequest(http.MethodGet, "http://example.test"+observability.PprofPrefix, nil))
	testutil.True(t, pattern != "",
		"importing net/http/pprof registers on DefaultServeMux, which is why we never serve it")

	mux := observability.NewAdminMux(observability.Config{MetricsEnabled: true}, newIsolatedMetrics(t))

	for _, target := range []string{
		observability.PprofPrefix,
		observability.PprofPrefix + "heap",
		observability.PprofPrefix + "profile",
	} {
		testutil.Equal(t, get(t, mux, target).Code, http.StatusNotFound,
			"pprof stays unreachable on the admin mux: "+target)
	}
}

func TestNewAdminServer(t *testing.T) {
	tests := []struct {
		name      string
		cfg       observability.Config
		wantNil   bool
		wantAddr  string
		wantWrite time.Duration
	}{
		{
			name:    "nil when nothing is enabled, so no socket is opened",
			cfg:     observability.Config{},
			wantNil: true,
		},
		{
			// Loopback by default: an admin listener on every interface is an
			// unauthenticated profiler for anyone who can reach the host.
			name:      "defaults bind loopback on 9090",
			cfg:       observability.Config{MetricsEnabled: true},
			wantAddr:  "127.0.0.1:9090",
			wantWrite: 15 * time.Second,
		},
		{
			name:      "host and port are honoured",
			cfg:       observability.Config{MetricsEnabled: true, Host: "0.0.0.0", Port: 9123},
			wantAddr:  "0.0.0.0:9123",
			wantWrite: 15 * time.Second,
		},
		{
			name:      "an IPv6 host is bracketed",
			cfg:       observability.Config{MetricsEnabled: true, Host: "::1", Port: 9090},
			wantAddr:  "[::1]:9090",
			wantWrite: 15 * time.Second,
		},
		{
			// The other half of the floor rule: metrics alone must NOT be widened,
			// or the floor would just be the default and the rule untested.
			name: "metrics alone keeps the ordinary write timeout",
			cfg: observability.Config{
				MetricsEnabled: true,
				WriteTimeout:   15 * time.Second,
			},
			wantAddr:  "127.0.0.1:9090",
			wantWrite: 15 * time.Second,
		},
		{
			// A 30s CPU profile would be truncated by the ordinary 15s write
			// timeout, producing a corrupt file with no explanation.
			name: "the write timeout is widened for pprof",
			cfg: observability.Config{
				PprofEnabled: true,
				WriteTimeout: 15 * time.Second,
			},
			wantAddr:  "127.0.0.1:9090",
			wantWrite: 35 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := observability.NewAdminServer(tt.cfg, newIsolatedMetrics(t))

			if tt.wantNil {
				if server != nil {
					t.Fatalf("expected no admin server, got one on %q", server.Addr)
				}
				return
			}

			if server == nil {
				t.Fatal("expected an admin server, got nil")
			}
			testutil.Equal(t, server.Addr, tt.wantAddr, "listen address")
			testutil.True(t, server.ReadTimeout > 0, "a read timeout is set")
			testutil.True(t, server.IdleTimeout > 0, "an idle timeout is set")
			// Exact, not a lower bound: `>= time.Second` was satisfied by any
			// value at all and asserted nothing. The pprof floor only means
			// something if the non-pprof cases are pinned to the ordinary value.
			testutil.Equal(t, server.WriteTimeout, tt.wantWrite, "write timeout")
			testutil.Equal(t, server.Handler != nil, true, "a handler is set, so DefaultServeMux is never served")
		})
	}
}

// TestNewAdminServerToleratesNilMetrics: the caller should not have to build a
// registry it has switched off.
func TestNewAdminServerToleratesNilMetrics(t *testing.T) {
	server := observability.NewAdminServer(observability.Config{PprofEnabled: true}, nil)

	if server == nil {
		t.Fatal("expected an admin server, got nil")
	}

	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, observability.HealthPath, nil))
	testutil.Equal(t, rec.Code, http.StatusOK, "healthz still served")
}

// TestNewMetricsWithRegistryIsIsolated: two independent registries must not
// share state, which is what lets each test assert on exactly its own traffic.
func TestNewMetricsWithRegistryIsIsolated(t *testing.T) {
	first := prometheus.NewRegistry()
	second := prometheus.NewRegistry()

	firstMetrics := observability.NewMetricsWithRegistry(first)
	observability.NewMetricsWithRegistry(second)

	// Record through the FIRST one only. Comparing two untouched registries
	// would prove nothing: two empty registries are trivially "isolated", and
	// that assertion would still pass if both shared one global CounterVec.
	r := gin.New()
	r.Use(firstMetrics.Middleware())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	firstFamilies, err := first.Gather()
	testutil.NoError(t, err)
	secondFamilies, err := second.Gather()
	testutil.NoError(t, err)

	testutil.True(t, len(firstFamilies) > 0, "the registry that recorded has series")
	testutil.Equal(t, len(secondFamilies), 0, "the other registry saw nothing")
}
