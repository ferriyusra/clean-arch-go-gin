package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestGenerateRefreshToken(t *testing.T) {
	t.Parallel()

	testUserID := uuid.New()

	tests := []struct {
		name          string
		config        TokenConfig
		userID        uuid.UUID
		expectedError bool
	}{
		{
			name: "should generate refresh token successfully",
			config: TokenConfig{
				AccessTokenSecret:  "test-access-secret",
				AccessTokenExpiry:  15 * time.Minute,
				RefreshTokenSecret: "test-refresh-secret",
				RefreshTokenExpiry: 7 * 24 * time.Hour,
			},
			userID:        testUserID,
			expectedError: false,
		},
		{
			name: "should generate refresh token with different user",
			config: TokenConfig{
				AccessTokenSecret:  "another-access",
				AccessTokenExpiry:  1 * time.Hour,
				RefreshTokenSecret: "another-refresh",
				RefreshTokenExpiry: 30 * 24 * time.Hour,
			},
			userID:        uuid.New(),
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewTokenService(tt.config)
			tokenString, err := service.GenerateRefreshToken(tt.userID)

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
					return []byte(tt.config.RefreshTokenSecret), nil
				})

				if parseErr != nil {
					t.Errorf("failed to parse generated token: %v", parseErr)
				}
				if claims.UserID != tt.userID {
					t.Errorf("expected UserID %v, got %v", tt.userID, claims.UserID)
				}
				// Refresh token should not have email or name
				if claims.Email != "" {
					t.Errorf("refresh token should not contain email, got %s", claims.Email)
				}
				if claims.Name != "" {
					t.Errorf("refresh token should not contain name, got %s", claims.Name)
				}
			}
		})
	}
}

func TestGenerateRefreshTokenWithEmptySecret(t *testing.T) {
	t.Parallel()

	service := NewTokenService(TokenConfig{
		AccessTokenSecret:  "test-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})

	tokenString, err := service.GenerateRefreshToken(uuid.New())

	if err != nil {
		t.Errorf("unexpected error with empty secret: %v", err)
	}
	if tokenString == "" {
		t.Errorf("expected non-empty token")
	}
}

func TestGenerateRefreshTokenClaimsExpiry(t *testing.T) {
	t.Parallel()

	expiry := 7 * 24 * time.Hour
	service := NewTokenService(TokenConfig{
		AccessTokenSecret:  "access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "refresh-secret",
		RefreshTokenExpiry: expiry,
	})

	tokenString, err := service.GenerateRefreshToken(uuid.New())
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims := &TokenClaims{}
	jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("refresh-secret"), nil
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

func TestGenerateRefreshTokenIssuer(t *testing.T) {
	t.Parallel()

	service := NewTokenService(TokenConfig{
		AccessTokenSecret:  "access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})

	tokenString, err := service.GenerateRefreshToken(uuid.New())
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	claims := &TokenClaims{}
	jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte("refresh-secret"), nil
	})

	if claims.Issuer != "go-vite-react" {
		t.Errorf("expected issuer 'go-vite-react', got %s", claims.Issuer)
	}
}

// TestGenerateRefreshTokenIsUniquePerCall is a regression test for a bug that
// silently disabled rotation: without a random JWT ID, two tokens minted for
// the same user inside the same second are byte-identical, so "rotating" a
// refresh token handed back the very token it was replacing.
func TestGenerateRefreshTokenIsUniquePerCall(t *testing.T) {
	t.Parallel()

	service := NewTokenService(TokenConfig{
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	})

	userID := uuid.New()
	seen := make(map[string]bool, 100)

	// Deliberately in a tight loop so every call lands in the same second,
	// which is exactly the condition the bug needed.
	for i := 0; i < 100; i++ {
		tokenStr, err := service.GenerateRefreshToken(userID)
		if err != nil {
			t.Fatalf("generating token %d: %v", i, err)
		}
		if seen[tokenStr] {
			t.Fatalf("duplicate refresh token on iteration %d", i)
		}
		seen[tokenStr] = true
	}
}

// BenchmarkGenerateRefreshToken measures minting one refresh token. It costs a
// little more than an access token because every call draws a random JWT ID,
// which is what makes rotation actually rotate.
func BenchmarkGenerateRefreshToken(b *testing.B) {
	service := NewTokenService(benchTokenConfig())
	userID := uuid.New()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		token, err := service.GenerateRefreshToken(userID)
		if err != nil {
			b.Fatalf("generating refresh token: %v", err)
		}
		benchToken = token
	}
}
