package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ferriyusra/clean-arch-go-gin/internal/tracing"
)

// Supported values for DATABASE_TYPE.
const (
	DatabaseTypeSQLite   = "sqlite"
	DatabaseTypePostgres = "postgres"
)

// pingAttempts bounds the startup retry loop. A database that is still booting
// (very common under docker compose) should not crash the application.
const (
	pingAttempts = 5
	pingBackoff  = 500 * time.Millisecond
)

// InitializeDatabase opens the connection pool, verifies it with a ping and
// applies the pool limits from config.
func InitializeDatabase(cfg *Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.Database.Type {
	case DatabaseTypePostgres:
		dialector = postgres.Open(cfg.Database.DSN)
	case DatabaseTypeSQLite, "":
		dialector = sqlite.Open(cfg.Database.DSN)
	default:
		// Falling through to sqlite on a typo would silently write production
		// data to a local file, so refuse instead.
		return nil, fmt.Errorf("unsupported DATABASE_TYPE %q (want %q or %q)",
			cfg.Database.Type, DatabaseTypeSQLite, DatabaseTypePostgres)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel(cfg.Database.LogLevel)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	sqlDB, err := db.DB() // Get the underlying generic *sql.DB
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Set connection pool settings from config
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	if cfg.Tracing.Enabled {
		if err := db.Use(tracing.NewGormPlugin()); err != nil {
			_ = CloseDatabase(db)
			return nil, fmt.Errorf("installing database tracing: %w", err)
		}
	}

	if err := pingWithRetry(db, pingAttempts, pingBackoff); err != nil {
		_ = CloseDatabase(db)
		return nil, err
	}

	return db, nil
}

// pingWithRetry waits for the database to accept connections, backing off
// linearly between attempts.
func pingWithRetry(db *gorm.DB, attempts int, backoff time.Duration) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		lastErr = sqlDB.PingContext(ctx)
		cancel()

		if lastErr == nil {
			return nil
		}
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * backoff)
		}
	}

	return fmt.Errorf("database unreachable after %d attempts: %w", attempts, lastErr)
}

// CloseDatabase releases the connection pool. Call it on shutdown so in-flight
// connections are returned rather than dropped.
func CloseDatabase(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	return sqlDB.Close()
}

// PingCheck adapts the pool to the health service's dependency-probe shape.
func PingCheck(db *gorm.DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if db == nil {
			return fmt.Errorf("database is not initialized")
		}
		sqlDB, err := db.DB()
		if err != nil {
			return fmt.Errorf("failed to get underlying sql.DB: %w", err)
		}
		return sqlDB.PingContext(ctx)
	}
}

// gormLogLevel maps DATABASE_LOG_LEVEL onto GORM's levels. GORM's own default
// of Info prints every statement, which is unusable in production.
func gormLogLevel(level string) logger.LogLevel {
	switch level {
	case "silent", "none", "off":
		return logger.Silent
	case "info", "debug":
		return logger.Info
	case "error":
		return logger.Error
	default:
		return logger.Warn
	}
}
