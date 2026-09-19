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
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Auth     AuthConfig
	Log      LogConfig
	Security SecurityConfig
	Tracing  TracingConfig
}

// AuthConfig holds authentication and security configuration
type AuthConfig struct {
	JWTAccessSecret  string
	JWTRefreshSecret string
	CSRFSecret       string
	DevMode          bool
	AllowedOrigins   []string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	CSRFTokenTTL     time.Duration
	// RefreshTokenPurgeInterval is how often expired refresh-token rows are
	// swept. Zero disables the sweep.
	RefreshTokenPurgeInterval time.Duration
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port            int
	Host            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	RequestTimeout  time.Duration
}

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	Gorm            *gorm.DB
	Type            string
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	LogLevel        string
	// AutoMigrate runs pending migrations at startup. Switch it off in
	// production and run them as their own step, so that several replicas
	// starting at once do not race each other.
	AutoMigrate bool
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string
	Port     int
	DB       int
	Password string
}

// LogConfig holds structured logging configuration
type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // json | text
}

// TracingConfig holds OpenTelemetry tracing configuration.
//
// Variable names follow the OpenTelemetry environment conventions so that an
// operator who has configured another OTel service already knows them.
type TracingConfig struct {
	Enabled        bool
	ServiceName    string
	ServiceVersion string
	Environment    string
	Exporter       string
	Endpoint       string
	Insecure       bool
	SampleRatio    float64
}

// SecurityConfig holds request-level hardening configuration
type SecurityConfig struct {
	RateLimitEnabled bool
	RateLimitRPS     float64
	RateLimitBurst   int
	// Auth* applies to the credential endpoints, which need a far tighter
	// budget than the rest of the API: those are the ones worth guessing at.
	AuthRateLimitRPS   float64
	AuthRateLimitBurst int
	MaxRequestBody     int64
	TrustedProxies     []string
}

// NewConfig loads configuration from environment variables.
//
// Parsing never fails here: unreadable values fall back to a default so the
// process can still start in development. Call Validate to reject a
// misconfigured production deployment before serving traffic.
func NewConfig() *Config {
	devMode := getEnvBool("DEV_MODE", false)

	return &Config{
		Server: ServerConfig{
			Port:            getEnvInt("SERVER_PORT", 8080),
			Host:            getEnv("SERVER_HOST", ""),
			ReadTimeout:     getEnvDuration("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getEnvDuration("SERVER_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:     getEnvDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
			RequestTimeout:  getEnvDuration("SERVER_REQUEST_TIMEOUT", 10*time.Second),
		},
		Database: DatabaseConfig{
			DSN:             getEnv("DATABASE_DSN", "dev.db"),
			Type:            getEnv("DATABASE_TYPE", "sqlite"),
			MaxOpenConns:    getEnvInt("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DATABASE_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getEnvDuration("DATABASE_CONN_MAX_LIFETIME", 5*time.Minute),
			LogLevel:        getEnv("DATABASE_LOG_LEVEL", defaultDatabaseLogLevel(devMode)),
			AutoMigrate:     getEnvBool("DATABASE_AUTO_MIGRATE", true),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvInt("REDIS_PORT", 6379),
			DB:       getEnvInt("REDIS_DB", 0),
			Password: getEnv("REDIS_PASSWORD", ""),
		},
		Auth: AuthConfig{
			JWTAccessSecret:           os.Getenv("JWT_ACCESS_SECRET"),
			JWTRefreshSecret:          os.Getenv("JWT_REFRESH_SECRET"),
			CSRFSecret:                os.Getenv("CSRF_SECRET"),
			DevMode:                   devMode,
			AllowedOrigins:            parseCSVEnv("ALLOWED_ORIGINS", "http://localhost:5173"),
			AccessTokenTTL:            getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTokenTTL:           getEnvDuration("JWT_REFRESH_TTL", 7*24*time.Hour),
			CSRFTokenTTL:              getEnvDuration("CSRF_TOKEN_TTL", 12*time.Hour),
			RefreshTokenPurgeInterval: getEnvDuration("REFRESH_TOKEN_PURGE_INTERVAL", time.Hour),
		},
		Log: LogConfig{
			Level:  strings.ToLower(getEnv("LOG_LEVEL", defaultLogLevel(devMode))),
			Format: strings.ToLower(getEnv("LOG_FORMAT", defaultLogFormat(devMode))),
		},
		Security: SecurityConfig{
			RateLimitEnabled:   getEnvBool("RATE_LIMIT_ENABLED", true),
			RateLimitRPS:       getEnvFloat("RATE_LIMIT_RPS", 20),
			RateLimitBurst:     getEnvInt("RATE_LIMIT_BURST", 40),
			AuthRateLimitRPS:   getEnvFloat("AUTH_RATE_LIMIT_RPS", 0.2),
			AuthRateLimitBurst: getEnvInt("AUTH_RATE_LIMIT_BURST", 5),
			MaxRequestBody:     int64(getEnvInt("MAX_REQUEST_BODY_BYTES", 1<<20)),
			TrustedProxies:     parseCSVEnv("TRUSTED_PROXIES", ""),
		},
		Tracing: TracingConfig{
			Enabled:        getEnvBool("OTEL_ENABLED", false),
			ServiceName:    getEnv("OTEL_SERVICE_NAME", "clean-arch-go-gin"),
			ServiceVersion: getEnv("OTEL_SERVICE_VERSION", "dev"),
			Environment:    getEnv("OTEL_ENVIRONMENT", defaultEnvironment(devMode)),
			Exporter:       strings.ToLower(getEnv("OTEL_TRACES_EXPORTER", "otlp")),
			Endpoint:       getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"),
			Insecure:       getEnvBool("OTEL_EXPORTER_OTLP_INSECURE", true),
			SampleRatio:    getEnvFloat("OTEL_TRACES_SAMPLER_ARG", 1.0),
		},
	}
}

func defaultLogLevel(devMode bool) string {
	if devMode {
		return "debug"
	}
	return "info"
}

func defaultLogFormat(devMode bool) string {
	if devMode {
		return "text"
	}
	return "json"
}

func defaultEnvironment(devMode bool) string {
	if devMode {
		return "development"
	}
	return "production"
}

func defaultDatabaseLogLevel(devMode bool) string {
	if devMode {
		return "info"
	}
	return "warn"
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

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
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
