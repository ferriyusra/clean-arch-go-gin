package observability

import (
	"net"
	"net/http"
	"strconv"
	"time"
)

// Default paths served by the admin listener.
const (
	MetricsPath = "/metrics"
	HealthPath  = "/healthz"
)

// Defaults applied when a Config field is left at its zero value.
const (
	// DefaultHost binds to loopback rather than to every interface. An admin
	// listener that answers on 0.0.0.0 is reachable from anywhere the host is,
	// which for pprof means an unauthenticated heap dump; reaching it from
	// elsewhere should take a deliberate act (a port-forward, a sidecar, an
	// explicit Host override), not a forgotten default.
	DefaultHost = "127.0.0.1"
	// DefaultPort is the conventional Prometheus "other" port.
	DefaultPort = 9090

	defaultReadTimeout  = 15 * time.Second
	defaultWriteTimeout = 15 * time.Second
	defaultIdleTimeout  = 60 * time.Second

	// pprofMinWriteTimeout keeps a CPU profile from being truncated. A
	// /debug/pprof/profile request runs for 30s by default, so a 15s write
	// timeout would cut the connection and hand back a corrupt file with no
	// explanation.
	pprofMinWriteTimeout = 35 * time.Second
)

// Config describes the admin listener. It is a plain struct with no dependency
// on the application's own configuration type, so this package stays
// standalone and the caller decides where the values come from.
type Config struct {
	// MetricsEnabled serves MetricsPath.
	MetricsEnabled bool
	// PprofEnabled serves PprofPrefix. Off by default, and it should stay off
	// unless someone is actively profiling.
	PprofEnabled bool
	// Host to bind. Empty means DefaultHost (loopback).
	Host string
	// Port to bind. Zero means DefaultPort.
	Port int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// Addr returns the host:port the admin server binds, with defaults applied.
func (c Config) Addr() string {
	host := c.Host
	if host == "" {
		host = DefaultHost
	}
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Enabled reports whether anything would be served.
func (c Config) Enabled() bool {
	return c.MetricsEnabled || c.PprofEnabled
}

// NewAdminMux builds the admin listener's routes.
//
// Only the enabled signals are registered: a disabled endpoint is absent from
// the mux entirely and 404s, rather than being present behind a runtime flag
// that someone can flip by accident.
//
// metrics may be nil when Config.MetricsEnabled is false.
func NewAdminMux(cfg Config, metrics *Metrics) *http.ServeMux {
	mux := http.NewServeMux()

	// A liveness path for the admin listener itself, so a probe can tell
	// "the process is up" from "the scrape target is misconfigured".
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	if cfg.MetricsEnabled && metrics != nil {
		mux.Handle(MetricsPath, metrics.Handler())
	}

	if cfg.PprofEnabled {
		RegisterPprof(mux)
	}

	return mux
}

// NewAdminServer returns the admin *http.Server, or nil when neither metrics
// nor pprof is enabled -- there is no reason to open a socket nobody will use.
//
// The caller owns the returned server: start it with ListenAndServe in a
// goroutine and stop it with the Shutdown(ctx) that *http.Server already
// provides. Treat http.ErrServerClosed from ListenAndServe as a clean stop.
//
// metrics may be nil when cfg.MetricsEnabled is false.
func NewAdminServer(cfg Config, metrics *Metrics) *http.Server {
	if !cfg.Enabled() {
		return nil
	}

	read := cfg.ReadTimeout
	if read <= 0 {
		read = defaultReadTimeout
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	write := cfg.WriteTimeout
	if write <= 0 {
		write = defaultWriteTimeout
	}
	if cfg.PprofEnabled && write < pprofMinWriteTimeout {
		write = pprofMinWriteTimeout
	}

	return &http.Server{
		Addr:         cfg.Addr(),
		Handler:      NewAdminMux(cfg, metrics),
		ReadTimeout:  read,
		WriteTimeout: write,
		IdleTimeout:  idle,
	}
}
