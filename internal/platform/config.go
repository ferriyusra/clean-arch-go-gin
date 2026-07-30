package platform

import (
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Config holds all application configuration
type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Auth      AuthConfig
	RateLimit RateLimitConfig
}

// AuthConfig holds authentication and security configuration
type AuthConfig struct {
	JWTAccessSecret    string
	JWTRefreshSecret   string
	JWTIssuer          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	DevMode            bool
	AllowedOrigins     []string
}

// RateLimitConfig holds rate limiting configuration for auth endpoints
type RateLimitConfig struct {
	LoginAttempts int
	LoginWindow   time.Duration
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port            int
	Host            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	// TrustedProxies lists the proxy CIDRs allowed to set X-Forwarded-For. Empty
	// means trust none, so the client IP is always the direct peer address —
	// otherwise any caller could spoof its IP and bypass the rate limiter.
	TrustedProxies []string
	// LogLevel is one of debug, info, warn, error. Empty picks a default from DEV_MODE.
	LogLevel string
}

// DatabaseConfig holds database connection configuration.
//
// Gorm is normally nil and filled in by platform.InitializeDatabase; tests may
// pre-populate it to inject an in-memory database.
type DatabaseConfig struct {
	Gorm            *gorm.DB
	Type            string
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	AutoMigrate     bool
}

// NewConfig loads configuration from environment variables
func NewConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            getEnvInt("SERVER_PORT", 8080),
			Host:            getEnv("SERVER_HOST", ""),
			ReadTimeout:     getEnvDuration("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getEnvDuration("SERVER_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:     getEnvDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
			TrustedProxies:  parseCSVEnv("TRUSTED_PROXIES", ""),
			LogLevel:        getEnv("LOG_LEVEL", ""),
		},
		Database: DatabaseConfig{
			DSN:             getEnv("DATABASE_DSN", "dev.db"),
			Type:            getEnv("DATABASE_TYPE", "sqlite"),
			MaxOpenConns:    getEnvInt("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DATABASE_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getEnvDuration("DATABASE_CONN_MAX_LIFETIME", 5*time.Minute),
			AutoMigrate:     getEnvBool("DATABASE_AUTO_MIGRATE", true),
		},
		Auth: AuthConfig{
			JWTAccessSecret:    os.Getenv("JWT_ACCESS_SECRET"),
			JWTRefreshSecret:   os.Getenv("JWT_REFRESH_SECRET"),
			JWTIssuer:          getEnv("JWT_ISSUER", "boilerplate-golang-gin"),
			AccessTokenExpiry:  getEnvDuration("JWT_ACCESS_EXPIRY", 15*time.Minute),
			RefreshTokenExpiry: getEnvDuration("JWT_REFRESH_EXPIRY", 7*24*time.Hour),
			DevMode:            getEnvBool("DEV_MODE", false),
			AllowedOrigins:     parseCSVEnv("ALLOWED_ORIGINS", "http://localhost:5173"),
		},
		RateLimit: RateLimitConfig{
			LoginAttempts: getEnvInt("RATE_LIMIT_LOGIN_ATTEMPTS", 10),
			LoginWindow:   getEnvDuration("RATE_LIMIT_LOGIN_WINDOW", time.Minute),
		},
	}
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func parseCSVEnv(key string, defaultValue string) []string {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	if defaultValue != "" {
		return []string{defaultValue}
	}
	return nil
}
