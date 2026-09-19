package platform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// minSecretLength is the shortest secret accepted in production. HMAC-SHA256
// gains nothing from a key longer than its 32-byte block, and anything shorter
// weakens it.
const minSecretLength = 32

// Validate reports every configuration problem at once, so a misconfigured
// deployment is fixed in one pass instead of one restart per mistake.
//
// NewConfig is deliberately lenient (an unparseable value silently falls back
// to its default) so that development is frictionless; Validate is where that
// leniency stops.
func (c *Config) Validate() error {
	var problems []string

	problems = append(problems, c.validateParsing()...)
	problems = append(problems, c.validateServer()...)
	problems = append(problems, c.validateDatabase()...)
	problems = append(problems, c.validateAuth()...)
	problems = append(problems, c.validateLog()...)
	problems = append(problems, c.validateTracing()...)
	problems = append(problems, c.validateTokenLifetimes()...)

	if len(problems) == 0 {
		return nil
	}

	return errors.New("invalid configuration:\n  - " + strings.Join(problems, "\n  - "))
}

// validateParsing re-reads the raw environment so that a value NewConfig had to
// discard is reported rather than silently replaced by a default.
func (c *Config) validateParsing() []string {
	var problems []string

	intVars := []string{
		"SERVER_PORT", "DATABASE_MAX_OPEN_CONNS", "DATABASE_MAX_IDLE_CONNS",
		"REDIS_PORT", "REDIS_DB", "RATE_LIMIT_BURST", "MAX_REQUEST_BODY_BYTES",
	}
	for _, key := range intVars {
		if raw := os.Getenv(key); raw != "" {
			if _, err := strconv.Atoi(raw); err != nil {
				problems = append(problems, fmt.Sprintf("%s=%q is not an integer", key, raw))
			}
		}
	}

	durationVars := []string{
		"SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_IDLE_TIMEOUT",
		"SERVER_SHUTDOWN_TIMEOUT", "SERVER_REQUEST_TIMEOUT",
		"DATABASE_CONN_MAX_LIFETIME", "JWT_ACCESS_TTL", "JWT_REFRESH_TTL",
		"CSRF_TOKEN_TTL", "REFRESH_TOKEN_PURGE_INTERVAL",
	}
	for _, key := range durationVars {
		if raw := os.Getenv(key); raw != "" {
			if _, err := time.ParseDuration(raw); err != nil {
				problems = append(problems, fmt.Sprintf("%s=%q is not a duration (e.g. 15s, 5m, 24h)", key, raw))
			}
		}
	}

	if raw := os.Getenv("RATE_LIMIT_RPS"); raw != "" {
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			problems = append(problems, fmt.Sprintf("RATE_LIMIT_RPS=%q is not a number", raw))
		}
	}

	return problems
}

func (c *Config) validateServer() []string {
	var problems []string

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		problems = append(problems, fmt.Sprintf("SERVER_PORT=%d is outside 1-65535", c.Server.Port))
	}
	if c.Server.ShutdownTimeout <= 0 {
		problems = append(problems, "SERVER_SHUTDOWN_TIMEOUT must be positive")
	}

	return problems
}

func (c *Config) validateDatabase() []string {
	var problems []string

	switch c.Database.Type {
	case DatabaseTypeSQLite, DatabaseTypePostgres:
	default:
		problems = append(problems, fmt.Sprintf(
			"DATABASE_TYPE=%q is not supported (want %q or %q)",
			c.Database.Type, DatabaseTypeSQLite, DatabaseTypePostgres))
	}

	if c.Database.DSN == "" {
		problems = append(problems, "DATABASE_DSN must not be empty")
	}
	if c.Database.MaxOpenConns < 1 {
		problems = append(problems, "DATABASE_MAX_OPEN_CONNS must be at least 1")
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		problems = append(problems, "DATABASE_MAX_IDLE_CONNS must not exceed DATABASE_MAX_OPEN_CONNS")
	}

	return problems
}

func (c *Config) validateAuth() []string {
	var problems []string

	if c.Auth.AccessTokenTTL <= 0 {
		problems = append(problems, "JWT_ACCESS_TTL must be positive")
	}
	if c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL {
		problems = append(problems, "JWT_REFRESH_TTL must be longer than JWT_ACCESS_TTL")
	}

	// Development substitutes throwaway secrets, so the rules below only apply
	// to a real deployment.
	if c.Auth.DevMode {
		return problems
	}

	secrets := map[string]string{
		"JWT_ACCESS_SECRET":  c.Auth.JWTAccessSecret,
		"JWT_REFRESH_SECRET": c.Auth.JWTRefreshSecret,
		"CSRF_SECRET":        c.Auth.CSRFSecret,
	}
	for name, value := range secrets {
		switch {
		case value == "":
			problems = append(problems, name+" must be set when DEV_MODE is false")
		case len(value) < minSecretLength:
			problems = append(problems, fmt.Sprintf(
				"%s must be at least %d characters (got %d)", name, minSecretLength, len(value)))
		}
	}

	if c.Auth.JWTAccessSecret != "" && c.Auth.JWTAccessSecret == c.Auth.JWTRefreshSecret {
		problems = append(problems, "JWT_ACCESS_SECRET and JWT_REFRESH_SECRET must differ, "+
			"otherwise a refresh token is accepted as an access token")
	}

	for _, origin := range c.Auth.AllowedOrigins {
		if origin == "*" {
			problems = append(problems, "ALLOWED_ORIGINS must not be \"*\" because credentials are allowed")
		}
	}

	return problems
}

func (c *Config) validateLog() []string {
	var problems []string

	switch c.Log.Level {
	case "debug", "info", "warn", "warning", "error":
	default:
		problems = append(problems, fmt.Sprintf(
			"LOG_LEVEL=%q is not supported (want debug, info, warn or error)", c.Log.Level))
	}

	switch c.Log.Format {
	case "json", "text":
	default:
		problems = append(problems, fmt.Sprintf(
			"LOG_FORMAT=%q is not supported (want json or text)", c.Log.Format))
	}

	return problems
}

func (c *Config) validateTracing() []string {
	var problems []string

	// Tracing is opt-in, so an unused block should not block startup.
	if !c.Tracing.Enabled {
		return problems
	}

	switch c.Tracing.Exporter {
	case ExporterOTLP, ExporterConsole, "stdout":
	default:
		problems = append(problems, fmt.Sprintf(
			"OTEL_TRACES_EXPORTER=%q is not supported (want %q or %q)",
			c.Tracing.Exporter, ExporterOTLP, ExporterConsole))
	}

	if c.Tracing.Exporter == ExporterOTLP && c.Tracing.Endpoint == "" {
		problems = append(problems, "OTEL_EXPORTER_OTLP_ENDPOINT must be set when the otlp exporter is used")
	}

	if c.Tracing.SampleRatio < 0 || c.Tracing.SampleRatio > 1 {
		problems = append(problems, fmt.Sprintf(
			"OTEL_TRACES_SAMPLER_ARG=%v is outside 0-1", c.Tracing.SampleRatio))
	}

	if c.Tracing.ServiceName == "" {
		problems = append(problems, "OTEL_SERVICE_NAME must not be empty when tracing is enabled")
	}

	// Sending spans unencrypted off the machine leaks request metadata, and the
	// insecure default is only there to make a local collector painless.
	if !c.Auth.DevMode && c.Tracing.Insecure && c.Tracing.Exporter == ExporterOTLP &&
		!isLoopbackEndpoint(c.Tracing.Endpoint) {
		problems = append(problems, fmt.Sprintf(
			"OTEL_EXPORTER_OTLP_INSECURE must be false outside DEV_MODE for a remote collector (%s)",
			c.Tracing.Endpoint))
	}

	return problems
}

func isLoopbackEndpoint(endpoint string) bool {
	return strings.Contains(endpoint, "localhost") ||
		strings.Contains(endpoint, "127.0.0.1") ||
		strings.Contains(endpoint, "[::1]")
}

func (c *Config) validateTokenLifetimes() []string {
	var problems []string

	if c.Auth.CSRFTokenTTL <= 0 {
		problems = append(problems, "CSRF_TOKEN_TTL must be positive")
	}
	if c.Auth.RefreshTokenPurgeInterval < 0 {
		problems = append(problems, "REFRESH_TOKEN_PURGE_INTERVAL must not be negative (0 disables the sweep)")
	}

	return problems
}
