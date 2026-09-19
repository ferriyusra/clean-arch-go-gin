package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/ferriyusra/clean-arch-go-gin/internal/di"
	"github.com/ferriyusra/clean-arch-go-gin/internal/logging"
	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// run holds the real body of main so that every deferred cleanup still runs on
// the error path; os.Exit in main would skip them.
func run() error {
	// Load environment variables from .env file if it exists
	_ = godotenv.Load()

	cfg := platform.NewConfig()

	// The build stamps the version into this binary, which is more reliable
	// than a value someone remembers to set in the environment, so it wins
	// unless the environment was explicit.
	if os.Getenv("OTEL_SERVICE_VERSION") == "" {
		cfg.Tracing.ServiceVersion = version
	}

	logger := logging.New(logging.Options{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	slog.SetDefault(logger)

	container, err := di.NewContainer(cfg, logger)
	if err != nil {
		return fmt.Errorf("initializing application: %w", err)
	}
	defer func() {
		if closeErr := container.Close(); closeErr != nil {
			logger.Error("closing database", "error", closeErr)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      container.Router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// NotifyContext cancels on SIGINT/SIGTERM and restores the default signal
	// behaviour on stop, so a second Ctrl-C still kills a wedged process.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Background maintenance lives as long as the shutdown context.
	container.StartJanitor(ctx)

	// The metrics/pprof listener is separate from the public server, so it has
	// its own start and is drained by container.Close.
	container.StartAdmin()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting",
			"addr", addr,
			"version", version,
			"dev_mode", cfg.Auth.DevMode,
			"database", cfg.Database.Type,
			"tracing", cfg.Tracing.Enabled,
			"metrics", cfg.Observability.MetricsEnabled,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		stop()
		logger.Info("shutdown signal received", "timeout", cfg.Server.ShutdownTimeout.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}
