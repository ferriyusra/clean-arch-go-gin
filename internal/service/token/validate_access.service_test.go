package token

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateAccessToken(t *testing.T) {
	t.Parallel()

	testUserID := uuid.New()
	config := TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	tests := []struct {
		name          string
		tokenFunc     func(TokenService) string
		expectedError bool
		errorMsg      string
		validateEmail string
	}{
		{
			name: "should validate access token successfully",
			tokenFunc: func(svc TokenService) string {
				token, _ := svc.GenerateAccessToken(testUserID, "test@example.com", "Test User")
				return token
			},
			expectedError: false,
			validateEmail: "test@example.com",
		},
		{
			name: "should return error for empty token",
			tokenFunc: func(svc TokenService) string {
				return ""
			},
			expectedError: true,
			errorMsg:      "token is empty",
		},
		{
			name: "should return error for invalid token format",
			tokenFunc: func(svc TokenService) string {
				return "invalid.token.format"
			},
			expectedError: true,
		},
		{
			name: "should return error for tampered token",
			tokenFunc: func(svc TokenService) string {
				token, _ := svc.GenerateAccessToken(testUserID, "test@example.com", "Test User")
				return token + "tampered"
			},
			expectedError: true,
		},
		{
			name: "should return error for wrong secret",
			tokenFunc: func(svc TokenService) string {
				wrongConfig := TokenConfig{
					AccessTokenSecret:  "wrong-secret",
					AccessTokenExpiry:  15 * time.Minute,
					RefreshTokenSecret: "test-refresh-secret",
					RefreshTokenExpiry: 7 * 24 * time.Hour,
				}
				wrongSvc := NewTokenService(wrongConfig)
				token, _ := wrongSvc.GenerateAccessToken(testUserID, "test@example.com", "Test User")
				return token
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := NewTokenService(config)
			tokenString := tt.tokenFunc(service)

			claims, err := service.ValidateAccessToken(tokenString)

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if claims == nil {
					t.Errorf("expected non-nil claims")
				}
				if claims != nil {
					if claims.UserID != testUserID {
						t.Errorf("expected UserID %v, got %v", testUserID, claims.UserID)
					}
					if claims.Email != tt.validateEmail {
						t.Errorf("expected Email %s, got %s", tt.validateEmail, claims.Email)
					}
				}
			}
		})
	}
}

func TestValidateAccessTokenExpired(t *testing.T) {
	t.Parallel()

	testUserID := uuid.New()
	config := TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  -1 * time.Second,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	service := NewTokenService(config)
	tokenString, _ := service.GenerateAccessToken(testUserID, "test@example.com", "Test User")

	time.Sleep(100 * time.Millisecond)

	claims, err := service.ValidateAccessToken(tokenString)

	if err == nil {
		t.Errorf("expected error for expired token, got nil")
	}
	if claims != nil {
		t.Errorf("expected nil claims for invalid token")
	}
}

func TestValidateAccessTokenClaimsIntegrity(t *testing.T) {
	t.Parallel()

	testUserID := uuid.New()
	testEmail := "test@example.com"
	testName := "Test User"

	config := TokenConfig{
		AccessTokenSecret:  "test-access-secret",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenSecret: "test-refresh-secret",
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	service := NewTokenService(config)
	tokenString, _ := service.GenerateAccessToken(testUserID, testEmail, testName)

	claims, err := service.ValidateAccessToken(tokenString)

	if err != nil {
		t.Fatalf("failed to validate token: %v", err)
	}

	if claims.UserID != testUserID {
		t.Errorf("UserID mismatch: expected %v, got %v", testUserID, claims.UserID)
	}
	if claims.Email != testEmail {
		t.Errorf("Email mismatch: expected %s, got %s", testEmail, claims.Email)
	}
	if claims.Name != testName {
		t.Errorf("Name mismatch: expected %s, got %s", testName, claims.Name)
	}
	if claims.Issuer != "go-vite-react" {
		t.Errorf("Issuer mismatch: expected go-vite-react, got %s", claims.Issuer)
	}
}

// BenchmarkValidateAccessToken measures the check every authenticated request
// pays before any handler runs: parse, verify the HMAC, decode the claims.
func BenchmarkValidateAccessToken(b *testing.B) {
	service := NewTokenService(benchTokenConfig())

	tokenString, err := service.GenerateAccessToken(uuid.New(), "bench@example.com", "Bench User")
	if err != nil {
		b.Fatalf("generating access token: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		claims, err := service.ValidateAccessToken(tokenString)
		if err != nil {
			b.Fatalf("validating access token: %v", err)
		}
		benchClaims = claims
	}
}

// FuzzValidateAccessToken throws arbitrary strings at the access-token
// validator. It must never panic, and it must never hand back claims for
// anything this test did not sign with the access secret.
//
// The seed corpus includes a token signed with the *refresh* secret. Rejecting
// that is the property the two-secret design exists to guarantee: if it were
// accepted, a stolen refresh token would grant immediate API access.
//
// The assertion compares the claims rather than the token string, so it stays
// honest without also asserting that JWT's base64 encoding is canonical — a
// non-canonical re-encoding of our own token is not a forgery.
func FuzzValidateAccessToken(f *testing.F) {
	const (
		email = "fuzz@example.com"
		name  = "Fuzz User"
	)

	// A fixed id, not uuid.New(): go test re-runs this function in every fuzz
	// worker process, and a random id would leave each worker expecting
	// different claims than the seed corpus was signed with.
	userID := uuid.MustParse("6f1b4a52-0000-4000-8000-00000000f0f0")

	service := NewTokenService(TokenConfig{
		AccessTokenSecret:  "fuzz-access-secret",
		AccessTokenExpiry:  time.Hour,
		RefreshTokenSecret: "fuzz-refresh-secret",
		RefreshTokenExpiry: time.Hour,
	})

	signed, err := service.GenerateAccessToken(userID, email, name)
	if err != nil {
		f.Fatalf("generating an access token: %v", err)
	}

	refreshSigned, err := service.GenerateRefreshToken(userID)
	if err != nil {
		f.Fatalf("generating a refresh token: %v", err)
	}

	for _, seed := range []string{
		signed,
		signed + "x",           // mangled signature
		signed[:len(signed)-1], // truncated signature
		refreshSigned,          // signed with the other secret
		"",
		"a.b.c",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, tokenString string) {
		claims, err := service.ValidateAccessToken(tokenString)
		if err != nil {
			if claims != nil {
				t.Fatalf("claims returned alongside an error for %q", tokenString)
			}
			return
		}

		if claims == nil {
			t.Fatalf("validation succeeded but returned no claims for %q", tokenString)
		}
		if claims.UserID != userID || claims.Email != email || claims.Name != name {
			t.Fatalf("accepted a token this test never signed: %q gave %+v", tokenString, claims)
		}
	})
}
