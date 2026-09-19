package csrf

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestValidateTokenWithGeneratedToken(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	service := NewCSRFService("test-secret", testCSRFTTL)

	if service.ValidateToken("") {
		t.Errorf("expected empty token to be invalid")
	}
}

func TestValidateTokenRejectsTampered(t *testing.T) {
	t.Parallel()

	service := NewCSRFService("test-secret", testCSRFTTL)

	token, _ := service.GenerateToken()

	if service.ValidateToken(token + "x") {
		t.Errorf("expected tampered token to be invalid")
	}
}

func TestValidateTokenRejectsWrongSecret(t *testing.T) {
	t.Parallel()

	service1 := NewCSRFService("secret-1", testCSRFTTL)
	service2 := NewCSRFService("secret-2", testCSRFTTL)

	token, _ := service1.GenerateToken()

	if service2.ValidateToken(token) {
		t.Errorf("expected token from different secret to be invalid")
	}
}

func TestValidateTokenRejectsInvalidFormat(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
			t.Parallel()

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
	t.Parallel()

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

// BenchmarkValidateToken measures the check every state-changing request pays:
// three hex decodes plus a recomputed HMAC-SHA256.
func BenchmarkValidateToken(b *testing.B) {
	service := NewCSRFService("bench-csrf-secret", testCSRFTTL)

	token, err := service.GenerateToken()
	if err != nil {
		b.Fatalf("generating CSRF token: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchValid = service.ValidateToken(token)
	}
}

// flipHexDigit changes one hex digit, flipping four bits of the decoded byte.
func flipHexDigit(s string) string {
	if s == "" {
		return s
	}
	flipped := []byte(s)
	if flipped[0] == '0' {
		flipped[0] = '1'
	} else {
		flipped[0] = '0'
	}
	return string(flipped)
}

// fixedNonce is a deterministic stand-in for the random nonce GenerateToken
// draws. Determinism is load-bearing for the fuzz target below: go test re-runs
// the fuzz function in every worker process, so a crypto/rand nonce would give
// each worker a different "the one valid token" than the seed corpus was minted
// with, and a perfectly legitimate seed would look like a forgery.
func fixedNonce() []byte {
	nonce := make([]byte, nonceLength)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	return nonce
}

// mintToken builds a token the way GenerateToken does, but from a caller-chosen
// nonce and issue time, using the service's real signing key.
func mintToken(s *csrfService, nonce []byte, at time.Time) string {
	issuedAt := make([]byte, 8)
	binary.BigEndian.PutUint64(issuedAt, uint64(at.Unix()))

	return hex.EncodeToString(nonce) + "." +
		hex.EncodeToString(issuedAt) + "." +
		hex.EncodeToString(s.sign(nonce, issuedAt))
}

// FuzzValidate throws arbitrary strings at the CSRF validator.
//
// Two properties are asserted. It must never panic — the validator indexes into
// attacker-controlled, variable-length input. And it must never accept anything
// this test did not mint with the real secret, which is the guarantee the whole
// stateless design rests on.
//
// The match is case-insensitive because hex decoding is: an uppercased copy of a
// valid token decodes to the same bytes, so it is the same token rather than a
// forgery.
func FuzzValidate(f *testing.F) {
	const secret = "fuzz-csrf-secret"

	// A fixed clock keeps validity deterministic, so the valid seed is always
	// exactly as old as this test says it is.
	issued := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	service := atTime(secret, time.Hour, issued)

	valid := mintToken(service, fixedNonce(), issued)

	parts := strings.Split(valid, ".")
	if len(parts) != 3 {
		f.Fatalf("expected three parts, got %d", len(parts))
	}

	// Minted with the right secret, but two days before the clock the validator
	// runs on, so it is well past its one-hour ttl.
	expired := mintToken(service, fixedNonce(), issued.Add(-48*time.Hour))

	restamped := make([]byte, 8)
	binary.BigEndian.PutUint64(restamped, uint64(issued.Add(24*time.Hour).Unix()))

	for _, seed := range []string{
		valid,
		expired,
		parts[0] + "." + parts[1] + "." + flipHexDigit(parts[2]),        // flipped byte in the MAC
		parts[0] + "." + hex.EncodeToString(restamped) + "." + parts[2], // edited issue time
		"not-hex.not-hex.not-hex",
		"",
		parts[0] + "." + parts[1], // too few segments
		valid + "." + parts[2],    // too many segments
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, token string) {
		if service.ValidateToken(token) && !strings.EqualFold(token, valid) {
			t.Fatalf("accepted a token this test never minted: %q", token)
		}
	})
}
