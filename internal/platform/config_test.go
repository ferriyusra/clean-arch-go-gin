package platform

import (
	"testing"
	"time"
)

func TestNewConfig_DefaultValues(t *testing.T) {
	// Clear environment variables
	clearEnv(t)

	// Act
	cfg := NewConfig()

	// Assert
	if cfg == nil {
		t.Fatal("expected config to be created")
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Observability.AdminHost != "127.0.0.1" {
		t.Errorf("expected default admin host 127.0.0.1, got %q", cfg.Observability.AdminHost)
	}

	if cfg.Observability.AdminPort != 9090 {
		t.Errorf("expected default admin port 9090, got %d", cfg.Observability.AdminPort)
	}

	if cfg.Observability.MetricsEnabled || cfg.Observability.PprofEnabled {
		t.Error("metrics and pprof must both default to off")
	}

}

func TestNewConfig_EnvironmentOverrides(t *testing.T) {
	// Clear and set environment variables
	clearEnv(t)
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("ADMIN_PORT", "9999")

	// Act
	cfg := NewConfig()

	// Assert
	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}

	if cfg.Observability.AdminPort != 9999 {
		t.Errorf("expected admin port 9999, got %d", cfg.Observability.AdminPort)
	}
}

func TestNewConfig_DatabaseConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_DSN", "postgres://user:pass@localhost/db")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "50")
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "10")

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
	t.Setenv("TEST_ENV_VAR", "test_value")

	result := getEnv("TEST_ENV_VAR", "default")

	if result != "test_value" {
		t.Errorf("expected 'test_value', got %q", result)
	}
}

func TestGetEnv_WithDefault(t *testing.T) {
	t.Setenv("NONEXISTENT_VAR", "")

	result := getEnv("NONEXISTENT_VAR", "default_value")

	if result != "default_value" {
		t.Errorf("expected 'default_value', got %q", result)
	}
}

func TestGetEnvInt_WithValue(t *testing.T) {
	t.Setenv("TEST_INT", "42")

	result := getEnvInt("TEST_INT", 0)

	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestGetEnvInt_WithDefault(t *testing.T) {
	t.Setenv("NONEXISTENT_INT", "")

	result := getEnvInt("NONEXISTENT_INT", 99)

	if result != 99 {
		t.Errorf("expected 99, got %d", result)
	}
}

func TestGetEnvInt_InvalidValue(t *testing.T) {
	t.Setenv("INVALID_INT", "not_a_number")

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
		t.Setenv("TEST_BOOL", tt.value)
		result := getEnvBool("TEST_BOOL", false)
		if !result {
			t.Errorf("expected true for value %q", tt.value)
		}
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
		t.Setenv("TEST_BOOL", tt.value)
		result := getEnvBool("TEST_BOOL", true)
		if result {
			t.Errorf("expected false for value %q", tt.value)
		}
	}
}

func TestGetEnvBool_EmptyString(t *testing.T) {
	t.Setenv("TEST_BOOL", "")

	result := getEnvBool("TEST_BOOL", true)

	// Empty string returns default
	if !result {
		t.Error("expected default true for empty string")
	}
}

func TestGetEnvBool_WithDefault(t *testing.T) {
	t.Setenv("NONEXISTENT_BOOL", "")

	result := getEnvBool("NONEXISTENT_BOOL", true)

	if !result {
		t.Error("expected default true")
	}
}

func TestGetEnvDuration_WithValue(t *testing.T) {
	t.Setenv("TEST_DURATION", "30s")

	result := getEnvDuration("TEST_DURATION", 0)

	if result != 30*time.Second {
		t.Errorf("expected 30s, got %v", result)
	}
}

func TestGetEnvDuration_WithDefault(t *testing.T) {
	t.Setenv("NONEXISTENT_DURATION", "")

	result := getEnvDuration("NONEXISTENT_DURATION", 5*time.Minute)

	if result != 5*time.Minute {
		t.Errorf("expected 5m, got %v", result)
	}
}

func TestGetEnvDuration_InvalidValue(t *testing.T) {
	t.Setenv("INVALID_DURATION", "not_a_duration")

	result := getEnvDuration("INVALID_DURATION", 10*time.Second)

	if result != 10*time.Second {
		t.Errorf("expected default 10s, got %v", result)
	}
}

func TestNewConfig_AllServerSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("SERVER_HOST", "0.0.0.0")
	t.Setenv("SERVER_PORT", "3000")
	t.Setenv("SERVER_READ_TIMEOUT", "20s")
	t.Setenv("SERVER_WRITE_TIMEOUT", "25s")
	t.Setenv("SERVER_IDLE_TIMEOUT", "90s")

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

// Helper function to clear all relevant environment variables
// clearEnv blanks every variable NewConfig reads, so a test observes the
// defaults rather than whatever the developer happens to have exported.
//
// t.Setenv is used rather than os.Unsetenv so the original values are restored
// when the test ends, keeping tests independent of their order.
func clearEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"SERVER_PORT", "SERVER_HOST", "SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_IDLE_TIMEOUT",
		"DATABASE_DSN", "DATABASE_MAX_OPEN_CONNS", "DATABASE_MAX_IDLE_CONNS", "DATABASE_CONN_MAX_LIFETIME",
		"METRICS_ENABLED", "PPROF_ENABLED", "ADMIN_HOST", "ADMIN_PORT",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}
}
