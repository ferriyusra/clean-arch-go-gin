package csrf

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestValidateTokenWithGeneratedToken(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	token, err := service.GenerateToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	if !service.ValidateToken(token) {
		t.Errorf("expected generated token to be valid")
	}
}

func TestValidateTokenConsistency(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	token, _ := service.GenerateToken()

	r1 := service.ValidateToken(token)
	r2 := service.ValidateToken(token)
	r3 := service.ValidateToken(token)

	if !r1 || !r2 || !r3 {
		t.Errorf("validation should be consistently true for a valid token")
	}
}

func TestValidateTokenRejectsEmpty(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	if service.ValidateToken("") {
		t.Errorf("expected empty token to be invalid")
	}
}

func TestValidateTokenRejectsTampered(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	token, _ := service.GenerateToken()

	if service.ValidateToken(token + "x") {
		t.Errorf("expected tampered token to be invalid")
	}
}

func TestValidateTokenRejectsWrongSecret(t *testing.T) {
	service1 := NewCSRFService("secret-1", testCSRFTTL)
	service2 := NewCSRFService("secret-2", testCSRFTTL)

	token, _ := service1.GenerateToken()

	if service2.ValidateToken(token) {
		t.Errorf("expected token from different secret to be invalid")
	}
}

func TestValidateTokenRejectsInvalidFormat(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	invalidTokens := []string{
		"no-dot-separator",
		".",
		".only-signature",
		"only-nonce.",
		"not-hex.not-hex",
		"abc123",
		"token with spaces",
		"!@#$%^&*()",
	}

	for _, token := range invalidTokens {
		if service.ValidateToken(token) {
			t.Errorf("expected token %q to be invalid", token)
		}
	}
}

func TestValidateTokenRejectsWrongSignature(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	token, _ := service.GenerateToken()

	// Replace signature with a different valid hex string
	parts := splitToken(token)
	if len(parts) == 2 {
		fakeToken := parts[0] + "." + "00000000000000000000000000000000" + "00000000000000000000000000000000"
		if service.ValidateToken(fakeToken) {
			t.Errorf("expected token with wrong signature to be invalid")
		}
	}
}

func splitToken(token string) []string {
	for i, ch := range token {
		if ch == '.' {
			return []string{token[:i], token[i+1:]}
		}
	}
	return []string{token}
}

// atTime builds a service whose clock is fixed, so expiry can be tested by
// moving the clock rather than by sleeping.
func atTime(secret string, ttl time.Duration, now time.Time) *csrfService {
	return &csrfService{
		secret: []byte(secret),
		ttl:    ttl,
		now:    func() time.Time { return now },
	}
}

func TestValidateTokenRejectsAnExpiredToken(t *testing.T) {
	issued := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	const ttl = time.Hour

	token, err := atTime("test-secret", ttl, issued).GenerateToken()
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}

	tests := []struct {
		name  string
		at    time.Time
		valid bool
	}{
		{name: "immediately", at: issued, valid: true},
		{name: "just inside the ttl", at: issued.Add(ttl - time.Second), valid: true},
		{name: "just past the ttl", at: issued.Add(ttl + time.Second)},
		{name: "long past the ttl", at: issued.Add(30 * 24 * time.Hour)},
		// A token stamped in the future is as suspect as an expired one, but a
		// small skew has to be tolerated or instances reject each other.
		{name: "within the allowed clock skew", at: issued.Add(-30 * time.Second), valid: true},
		{name: "beyond the allowed clock skew", at: issued.Add(-10 * time.Minute)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atTime("test-secret", ttl, tt.at).ValidateToken(token); got != tt.valid {
				t.Errorf("ValidateToken at %v = %v, want %v", tt.at.Sub(issued), got, tt.valid)
			}
		})
	}
}

// TestValidateTokenRejectsATamperedTimestamp is the reason the issue time is
// inside the signed material: an attacker who could edit it would be able to
// extend the life of a captured token indefinitely.
func TestValidateTokenRejectsATamperedTimestamp(t *testing.T) {
	issued := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	service := atTime("test-secret", time.Hour, issued)

	token, err := service.GenerateToken()
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected three parts, got %d", len(parts))
	}

	// Restamp the token as if it had been issued much later.
	future := make([]byte, 8)
	binary.BigEndian.PutUint64(future, uint64(issued.Add(24*time.Hour).Unix()))
	forged := parts[0] + "." + hex.EncodeToString(future) + "." + parts[2]

	later := atTime("test-secret", time.Hour, issued.Add(12*time.Hour))
	if later.ValidateToken(forged) {
		t.Errorf("a token with an edited issue time must not validate")
	}
}
