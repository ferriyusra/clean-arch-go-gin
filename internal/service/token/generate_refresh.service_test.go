package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestGenerateRefreshToken(t *testing.T) {
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
	if claims.ExpiresAt == nil || claims.ExpiresAt.Before(now) {
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

	if claims.Issuer != DefaultIssuer {
		t.Errorf("expected issuer %s, got %s", DefaultIssuer, claims.Issuer)
	}
}
