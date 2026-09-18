package platform_test

import (
	"strings"
	"testing"

	"github.com/ferriyusra/clean-arch-go-gin/internal/platform"
	"github.com/ferriyusra/clean-arch-go-gin/internal/testutil"
)

// productionEnv sets a minimal valid production configuration, which each test
// then breaks in exactly one way.
func productionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DEV_MODE", "false")
	t.Setenv("JWT_ACCESS_SECRET", strings.Repeat("a", 32))
	t.Setenv("JWT_REFRESH_SECRET", strings.Repeat("b", 32))
	t.Setenv("CSRF_SECRET", strings.Repeat("c", 32))
}

func TestValidateAcceptsAValidProductionConfig(t *testing.T) {
	productionEnv(t)

	testutil.NoError(t, platform.NewConfig().Validate())
}

func TestValidateAcceptsDevModeWithNoSecrets(t *testing.T) {
	// Development must stay frictionless: no secrets required.
	t.Setenv("DEV_MODE", "true")
	t.Setenv("JWT_ACCESS_SECRET", "")
	t.Setenv("JWT_REFRESH_SECRET", "")
	t.Setenv("CSRF_SECRET", "")

	testutil.NoError(t, platform.NewConfig().Validate())
}

func TestValidateRejectsMisconfiguration(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
		want  string
	}{
		{
			name:  "missing secrets in production",
			setup: func(t *testing.T) { t.Setenv("JWT_ACCESS_SECRET", "") },
			want:  "JWT_ACCESS_SECRET must be set",
		},
		{
			// A one-character secret used to pass the old presence-only check.
			name:  "a secret that is too short",
			setup: func(t *testing.T) { t.Setenv("CSRF_SECRET", "x") },
			want:  "CSRF_SECRET must be at least 32 characters",
		},
		{
			// Identical secrets would make a refresh token usable as an access
			// token, collapsing the whole two-token design.
			name: "identical access and refresh secrets",
			setup: func(t *testing.T) {
				same := strings.Repeat("s", 32)
				t.Setenv("JWT_ACCESS_SECRET", same)
				t.Setenv("JWT_REFRESH_SECRET", same)
			},
			want: "must differ",
		},
		{
			// Previously a typo here silently fell through to sqlite, writing
			// production data to a local file.
			name:  "an unsupported database type",
			setup: func(t *testing.T) { t.Setenv("DATABASE_TYPE", "postgresql") },
			want:  "DATABASE_TYPE",
		},
		{
			name:  "a port that is not a number",
			setup: func(t *testing.T) { t.Setenv("SERVER_PORT", "not-a-number") },
			want:  "is not an integer",
		},
		{
			name:  "a duration that cannot be parsed",
			setup: func(t *testing.T) { t.Setenv("SERVER_READ_TIMEOUT", "15 seconds") },
			want:  "is not a duration",
		},
		{
			name:  "a port outside the valid range",
			setup: func(t *testing.T) { t.Setenv("SERVER_PORT", "70000") },
			want:  "outside 1-65535",
		},
		{
			// Credentials plus a wildcard origin is the classic CORS mistake.
			name:  "a wildcard origin while credentials are allowed",
			setup: func(t *testing.T) { t.Setenv("ALLOWED_ORIGINS", "*") },
			want:  "must not be",
		},
		{
			name:  "a refresh TTL shorter than the access TTL",
			setup: func(t *testing.T) { t.Setenv("JWT_REFRESH_TTL", "1m") },
			want:  "must be longer than",
		},
		{
			name:  "an unsupported log level",
			setup: func(t *testing.T) { t.Setenv("LOG_LEVEL", "verbose") },
			want:  "LOG_LEVEL",
		},
		{
			name:  "more idle connections than open ones",
			setup: func(t *testing.T) { t.Setenv("DATABASE_MAX_IDLE_CONNS", "100") },
			want:  "must not exceed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			productionEnv(t)
			tt.setup(t)

			err := platform.NewConfig().Validate()

			testutil.Error(t, err, tt.name)
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected the error to mention %q, got: %v", tt.want, err)
			}
		})
	}
}

// TestValidateReportsEveryProblemAtOnce saves a restart-per-mistake loop when
// bringing a new environment up.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	productionEnv(t)
	t.Setenv("JWT_ACCESS_SECRET", "")
	t.Setenv("DATABASE_TYPE", "mysql")
	t.Setenv("LOG_FORMAT", "xml")

	err := platform.NewConfig().Validate()
	testutil.Error(t, err, "several problems at once")

	for _, want := range []string{"JWT_ACCESS_SECRET", "DATABASE_TYPE", "LOG_FORMAT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got: %v", want, err)
		}
	}
}
