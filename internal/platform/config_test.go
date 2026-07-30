package platform

import (
	"os"
	"testing"
	"time"
)

func TestNewConfig_DefaultValues(t *testing.T) {
	// Clear environment variables
	clearEnv()

	// Act
	cfg := NewConfig()

	// Assert
	if cfg == nil {
		t.Fatal("expected config to be created")
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Database.Type != "sqlite" {
		t.Errorf("expected default database type 'sqlite', got %q", cfg.Database.Type)
	}

	if cfg.Auth.JWTIssuer != "boilerplate-golang-gin" {
		t.Errorf("expected default issuer 'boilerplate-golang-gin', got %q", cfg.Auth.JWTIssuer)
	}

	if cfg.Auth.AccessTokenExpiry != 15*time.Minute {
		t.Errorf("expected default access token expiry 15m, got %v", cfg.Auth.AccessTokenExpiry)
	}

	if cfg.Auth.RefreshTokenExpiry != 7*24*time.Hour {
		t.Errorf("expected default refresh token expiry 168h, got %v", cfg.Auth.RefreshTokenExpiry)
	}

	if cfg.Auth.DevMode {
		t.Error("expected DevMode to default to false")
	}

	if len(cfg.Auth.AllowedOrigins) != 1 || cfg.Auth.AllowedOrigins[0] != "http://localhost:5173" {
		t.Errorf("expected default allowed origins [http://localhost:5173], got %v", cfg.Auth.AllowedOrigins)
	}

	if cfg.RateLimit.LoginAttempts != 10 {
		t.Errorf("expected default login attempts 10, got %d", cfg.RateLimit.LoginAttempts)
	}

	if cfg.RateLimit.LoginWindow != time.Minute {
		t.Errorf("expected default login window 1m, got %v", cfg.RateLimit.LoginWindow)
	}
}

func TestNewConfig_EnvironmentOverrides(t *testing.T) {
	// Clear and set environment variables
	clearEnv()
	os.Setenv("SERVER_PORT", "9000")
	os.Setenv("DATABASE_TYPE", "postgres")
	os.Setenv("ALLOWED_ORIGINS", "https://a.example.com, https://b.example.com")
	defer clearEnv()

	// Act
	cfg := NewConfig()

	// Assert
	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}

	if cfg.Database.Type != "postgres" {
		t.Errorf("expected database type 'postgres', got %q", cfg.Database.Type)
	}

	want := []string{"https://a.example.com", "https://b.example.com"}
	if len(cfg.Auth.AllowedOrigins) != len(want) {
		t.Fatalf("expected %d allowed origins, got %v", len(want), cfg.Auth.AllowedOrigins)
	}
	for i, origin := range want {
		if cfg.Auth.AllowedOrigins[i] != origin {
			t.Errorf("allowed origin %d: expected %q, got %q", i, origin, cfg.Auth.AllowedOrigins[i])
		}
	}
}

func TestNewConfig_DatabaseConfig(t *testing.T) {
	clearEnv()
	os.Setenv("DATABASE_DSN", "postgres://user:pass@localhost/db")
	os.Setenv("DATABASE_MAX_OPEN_CONNS", "50")
	os.Setenv("DATABASE_MAX_IDLE_CONNS", "10")
	defer clearEnv()

	// Act
	cfg := NewConfig()

	// Assert
	if cfg.Database.DSN != "postgres://user:pass@localhost/db" {
		t.Errorf("expected DSN, got %q", cfg.Database.DSN)
	}

	if cfg.Database.MaxOpenConns != 50 {
		t.Errorf("expected max open conns 50, got %d", cfg.Database.MaxOpenConns)
	}

	if cfg.Database.MaxIdleConns != 10 {
		t.Errorf("expected max idle conns 10, got %d", cfg.Database.MaxIdleConns)
	}
}

func TestGetEnv_WithValue(t *testing.T) {
	os.Setenv("TEST_ENV_VAR", "test_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	result := getEnv("TEST_ENV_VAR", "default")

	if result != "test_value" {
		t.Errorf("expected 'test_value', got %q", result)
	}
}

func TestGetEnv_WithDefault(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR")

	result := getEnv("NONEXISTENT_VAR", "default_value")

	if result != "default_value" {
		t.Errorf("expected 'default_value', got %q", result)
	}
}

func TestGetEnvInt_WithValue(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	defer os.Unsetenv("TEST_INT")

	result := getEnvInt("TEST_INT", 0)

	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestGetEnvInt_WithDefault(t *testing.T) {
	os.Unsetenv("NONEXISTENT_INT")

	result := getEnvInt("NONEXISTENT_INT", 99)

	if result != 99 {
		t.Errorf("expected 99, got %d", result)
	}
}

func TestGetEnvInt_InvalidValue(t *testing.T) {
	os.Setenv("INVALID_INT", "not_a_number")
	defer os.Unsetenv("INVALID_INT")

	result := getEnvInt("INVALID_INT", 77)

	if result != 77 {
		t.Errorf("expected default 77, got %d", result)
	}
}

func TestGetEnvBool_True(t *testing.T) {
	tests := []struct {
		value string
	}{
		{"true"},
		{"1"},
		{"yes"},
	}

	for _, tt := range tests {
		os.Setenv("TEST_BOOL", tt.value)
		result := getEnvBool("TEST_BOOL", false)
		if !result {
			t.Errorf("expected true for value %q", tt.value)
		}
		os.Unsetenv("TEST_BOOL")
	}
}

func TestGetEnvBool_False(t *testing.T) {
	tests := []struct {
		value string
	}{
		{"false"},
		{"0"},
		{"no"},
	}

	for _, tt := range tests {
		os.Setenv("TEST_BOOL", tt.value)
		result := getEnvBool("TEST_BOOL", true)
		if result {
			t.Errorf("expected false for value %q", tt.value)
		}
		os.Unsetenv("TEST_BOOL")
	}
}

func TestGetEnvBool_EmptyString(t *testing.T) {
	os.Setenv("TEST_BOOL", "")
	defer os.Unsetenv("TEST_BOOL")

	result := getEnvBool("TEST_BOOL", true)

	// Empty string returns default
	if !result {
		t.Error("expected default true for empty string")
	}
}

func TestGetEnvBool_WithDefault(t *testing.T) {
	os.Unsetenv("NONEXISTENT_BOOL")

	result := getEnvBool("NONEXISTENT_BOOL", true)

	if !result {
		t.Error("expected default true")
	}
}

func TestGetEnvDuration_WithValue(t *testing.T) {
	os.Setenv("TEST_DURATION", "30s")
	defer os.Unsetenv("TEST_DURATION")

	result := getEnvDuration("TEST_DURATION", 0)

	if result != 30*time.Second {
		t.Errorf("expected 30s, got %v", result)
	}
}

func TestGetEnvDuration_WithDefault(t *testing.T) {
	os.Unsetenv("NONEXISTENT_DURATION")

	result := getEnvDuration("NONEXISTENT_DURATION", 5*time.Minute)

	if result != 5*time.Minute {
		t.Errorf("expected 5m, got %v", result)
	}
}

func TestGetEnvDuration_InvalidValue(t *testing.T) {
	os.Setenv("INVALID_DURATION", "not_a_duration")
	defer os.Unsetenv("INVALID_DURATION")

	result := getEnvDuration("INVALID_DURATION", 10*time.Second)

	if result != 10*time.Second {
		t.Errorf("expected default 10s, got %v", result)
	}
}

func TestNewConfig_AllServerSettings(t *testing.T) {
	clearEnv()
	os.Setenv("SERVER_HOST", "0.0.0.0")
	os.Setenv("SERVER_PORT", "3000")
	os.Setenv("SERVER_READ_TIMEOUT", "20s")
	os.Setenv("SERVER_WRITE_TIMEOUT", "25s")
	os.Setenv("SERVER_IDLE_TIMEOUT", "90s")
	defer clearEnv()

	cfg := NewConfig()

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected host '0.0.0.0', got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 20*time.Second {
		t.Errorf("expected read timeout 20s, got %v", cfg.Server.ReadTimeout)
	}
	if cfg.Server.WriteTimeout != 25*time.Second {
		t.Errorf("expected write timeout 25s, got %v", cfg.Server.WriteTimeout)
	}
	if cfg.Server.IdleTimeout != 90*time.Second {
		t.Errorf("expected idle timeout 90s, got %v", cfg.Server.IdleTimeout)
	}
}

func TestNewConfig_AuthOverrides(t *testing.T) {
	clearEnv()
	os.Setenv("JWT_ACCESS_SECRET", "access-secret")
	os.Setenv("JWT_REFRESH_SECRET", "refresh-secret")
	os.Setenv("JWT_ISSUER", "my-service")
	os.Setenv("JWT_ACCESS_EXPIRY", "5m")
	os.Setenv("JWT_REFRESH_EXPIRY", "24h")
	os.Setenv("DEV_MODE", "true")
	os.Setenv("RATE_LIMIT_LOGIN_ATTEMPTS", "3")
	os.Setenv("RATE_LIMIT_LOGIN_WINDOW", "30s")
	defer clearEnv()

	cfg := NewConfig()

	if cfg.Auth.JWTAccessSecret != "access-secret" {
		t.Errorf("expected access secret 'access-secret', got %q", cfg.Auth.JWTAccessSecret)
	}
	if cfg.Auth.JWTRefreshSecret != "refresh-secret" {
		t.Errorf("expected refresh secret 'refresh-secret', got %q", cfg.Auth.JWTRefreshSecret)
	}
	if cfg.Auth.JWTIssuer != "my-service" {
		t.Errorf("expected issuer 'my-service', got %q", cfg.Auth.JWTIssuer)
	}
	if cfg.Auth.AccessTokenExpiry != 5*time.Minute {
		t.Errorf("expected access expiry 5m, got %v", cfg.Auth.AccessTokenExpiry)
	}
	if cfg.Auth.RefreshTokenExpiry != 24*time.Hour {
		t.Errorf("expected refresh expiry 24h, got %v", cfg.Auth.RefreshTokenExpiry)
	}
	if !cfg.Auth.DevMode {
		t.Error("expected DevMode true")
	}
	if cfg.RateLimit.LoginAttempts != 3 {
		t.Errorf("expected login attempts 3, got %d", cfg.RateLimit.LoginAttempts)
	}
	if cfg.RateLimit.LoginWindow != 30*time.Second {
		t.Errorf("expected login window 30s, got %v", cfg.RateLimit.LoginWindow)
	}
}

// Helper function to clear all relevant environment variables
func clearEnv() {
	vars := []string{
		"SERVER_PORT", "SERVER_HOST", "SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_IDLE_TIMEOUT",
		"DATABASE_DSN", "DATABASE_MAX_OPEN_CONNS", "DATABASE_MAX_IDLE_CONNS", "DATABASE_CONN_MAX_LIFETIME",
		"DATABASE_TYPE",
		"JWT_ACCESS_SECRET", "JWT_REFRESH_SECRET", "JWT_ISSUER", "JWT_ACCESS_EXPIRY", "JWT_REFRESH_EXPIRY",
		"DEV_MODE", "ALLOWED_ORIGINS",
		"RATE_LIMIT_LOGIN_ATTEMPTS", "RATE_LIMIT_LOGIN_WINDOW",
	}
	for _, v := range vars {
		os.Unsetenv(v)
	}
}
