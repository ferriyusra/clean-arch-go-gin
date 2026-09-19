package token

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewTokenService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config TokenConfig
	}{
		{
			name: "should create service successfully",
			config: TokenConfig{
				AccessTokenSecret:  "test-access-secret",
				AccessTokenExpiry:  15 * time.Minute,
				RefreshTokenSecret: "test-refresh-secret",
				RefreshTokenExpiry: 7 * 24 * time.Hour,
			},
		},
		{
			name: "should create service with different expiry times",
			config: TokenConfig{
				AccessTokenSecret:  "another-secret",
				AccessTokenExpiry:  1 * time.Hour,
				RefreshTokenSecret: "another-refresh-secret",
				RefreshTokenExpiry: 30 * 24 * time.Hour,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewTokenService(tt.config)

			if service == nil {
				t.Errorf("expected non-nil service, got nil")
			}

			_, ok := service.(*tokenService)
			if !ok {
				t.Errorf("expected *tokenService, got %T", service)
			}
		})
	}
}

func TestTokenClaimsStructure(t *testing.T) {
	t.Parallel()

	testUserID := uuid.New()
	claims := TokenClaims{
		UserID: testUserID,
		Email:  "test@example.com",
		Name:   "Test User",
	}

	if claims.UserID != testUserID {
		t.Errorf("expected UserID %v, got %v", testUserID, claims.UserID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("expected Email test@example.com, got %s", claims.Email)
	}
	if claims.Name != "Test User" {
		t.Errorf("expected Name Test User, got %s", claims.Name)
	}
}

// benchTokenConfig mirrors the production TTLs so the benchmarks below measure
// the work a real request does, not a toy configuration.
func benchTokenConfig() TokenConfig {
	return TokenConfig{
		AccessTokenSecret:  "bench-access-secret-at-least-32-characters",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "bench-refresh-secret-at-least-32-characters",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}
}

// Sinks for the benchmark results, so the compiler cannot delete the work.
var (
	benchToken  string
	benchClaims *TokenClaims
)
