package platform

import (
	"context"
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitializeDatabase opens the configured database and applies pool settings.
func InitializeDatabase(cfg *Config) (*gorm.DB, error) {
	// Query logging is verbose and can leak data into logs, so it is only
	// enabled in dev mode; production logs slow queries and errors only.
	logLevel := logger.Warn
	if cfg.Auth.DevMode {
		logLevel = logger.Info
	}

	var dialector gorm.Dialector
	dbConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	}
	if cfg.Database.Type == "postgres" {
		dialector = postgres.Open(cfg.Database.DSN)
	} else {
		dialector = sqlite.Open(cfg.Database.DSN)
	}
	db, err := gorm.Open(dialector, dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}
	sqlDB, err := db.DB() // Get the underlying generic *sql.DB
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Set connection pool settings from your config
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	return db, nil
}

// PingDatabase verifies the database is reachable. Used by the health endpoint.
func PingDatabase(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}
	return nil
}

// CloseDatabase releases the connection pool. Call it during shutdown so
// in-flight connections are returned instead of being dropped by process exit.
func CloseDatabase(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("getting underlying sql.DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("closing database: %w", err)
	}
	return nil
}
