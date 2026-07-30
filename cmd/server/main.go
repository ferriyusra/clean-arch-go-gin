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
	"time"

	"github.com/joho/godotenv"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/di"
	"github.com/ferriyusra/boilerplate-golang-gin/internal/platform"
)

// tokenPurgeInterval is how often expired refresh tokens are swept from the
// database. Expired rows are harmless but unbounded, so an hourly pass is plenty.
const tokenPurgeInterval = time.Hour

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run wires up and serves the application, returning any startup or shutdown
// error. Keeping it separate from main means every deferred cleanup still runs —
// os.Exit in main would skip them.
func run() error {
	// Load environment variables from .env file if it exists
	_ = godotenv.Load()

	cfg := platform.NewConfig()

	container, err := di.NewContainer(cfg)
	if err != nil {
		return fmt.Errorf("initializing application: %w", err)
	}
	logger := container.Logger

	defer func() {
		if err := platform.CloseDatabase(container.DB); err != nil {
			logger.Error("closing database", slog.Any("error", err))
		}
	}()

	// Signal context: cancelled on SIGINT/SIGTERM, which both stops the janitor
	// and triggers graceful shutdown below.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	purgeDone := startTokenJanitor(ctx, container, logger)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      container.Router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("starting server",
			slog.String("addr", addr),
			slog.Bool("dev_mode", cfg.Auth.DevMode),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// Wait for either a signal or the server falling over on its own.
	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	<-purgeDone

	logger.Info("shutdown complete")
	return nil
}

// startTokenJanitor periodically deletes expired refresh tokens until ctx is
// cancelled. The returned channel closes once the loop has stopped.
func startTokenJanitor(ctx context.Context, container *di.Container, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(tokenPurgeInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleted, err := container.Services.User.PurgeExpiredRefreshTokens(ctx)
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						logger.Error("purging expired refresh tokens", slog.Any("error", err))
					}
					continue
				}
				if deleted > 0 {
					logger.Info("purged expired refresh tokens", slog.Int64("count", deleted))
				}
			}
		}
	}()

	return done
}
