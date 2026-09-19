package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestGenerateAccessToken(t *testing.T) {
	t.Parallel()

	testUserID := uuid.New()

	tests := []struct {
		name          string
		config        TokenConfig
		userID        uuid.UUID
		email         string
		userName      string
		expectedError bool
	}{
		{
			name: "should generate access token successfully",
			config: TokenConfig{
				AccessTokenSecret:  "test-access-secret-key",
				AccessTokenExpiry:  15 * time.Minute,
				RefreshTokenSecret: "test-refresh-secret-key",
				RefreshTokenExpiry: 7 * 24 * time.Hour,
			},
			userID:        testUserID,
			email:         "test@example.com",
			userName:      "Test User",
			expectedError: false,
		},
		{
			name: "should generate token with different user data",
			config: TokenConfig{
				AccessTokenSecret:  "another-secret",
				AccessTokenExpiry:  1 * time.Hour,
				RefreshTokenSecret: "another-refresh",
				RefreshTokenExpiry: 30 * 24 * time.Hour,
			},
			userID:        uuid.New(),
			email:         "another@example.com",
			userName:      "Another User",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewTokenService(tt.config)
			tokenString, err := service.GenerateAccessToken(tt.userID, tt.email, tt.userName)

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if tokenString == "" {
					t.Errorf("expected non-empty token, got empty string")
				}

				// Verify token can be parsed
				claims := &TokenClaims{}
				_, parseErr := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
					return []byte(tt.config.AccessTokenSecret), nil
				})

				if parseErr != nil {
					t.Errorf("failed to parse generated token: %v", parseErr)
				}
				if claims.UserID != tt.userID {
					t.Errorf("expected UserID %v, got %v", tt.userID, claims.UserID)
				}
				if claims.Email != tt.email {
					t.Errorf("expected Email %s, got %s", tt.email, claims.Email)
				}
				if claims.Name != tt.userName {
					t.Errorf("expected Name %s, got %s", tt.userName, claims.Name)
				}
			}
		})
	}
}

func TestGenerateAccessTokenWithEmptySecret(t *testing.T) {
	t.Parallel()

	service := NewTokenService(TokenConfig{
		AccessTokenSecret:  "",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})

	tokenString, err := service.GenerateAccessToken(uuid.New(), "test@example.com", "Test User")

	if err != nil {
		t.Errorf("unexpected error with empty secret: %v", err)
	}
	if tokenString == "" {
		t.Errorf("expected non-empty token")
	}
}

func TestGenerateAccessTokenClaimsExpiry(t *testing.T) {
	t.Parallel()

	expiry := 15 * time.Minute
	service := NewTokenService(TokenConfig{
		AccessTokenSecret:  "test-secret",
		AccessTokenExpiry:  expiry,
		RefreshTokenSecret: "refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})

	tokenString, err := service.GenerateAccessToken(uuid.New(), "test@example.com", "Test User")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims := &TokenClaims{}
	jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-secret"), nil
	})

	now := time.Now()
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(now) {
		t.Errorf("token already expired")
	}
	if claims.IssuedAt != nil {
		diff := now.Sub(claims.IssuedAt.Time)
		if diff < -1*time.Second || diff > 1*time.Second {
			t.Errorf("IssuedAt mismatch")
		}
	}
}

// BenchmarkGenerateAccessToken measures minting one access token. Register,
// login and every refresh mint one, so its allocation profile sits on the hot
// path of the auth endpoints.
func BenchmarkGenerateAccessToken(b *testing.B) {
	service := NewTokenService(benchTokenConfig())
	userID := uuid.New()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		token, err := service.GenerateAccessToken(userID, "bench@example.com", "Bench User")
		if err != nil {
			b.Fatalf("generating access token: %v", err)
		}
		benchToken = token
	}
}
