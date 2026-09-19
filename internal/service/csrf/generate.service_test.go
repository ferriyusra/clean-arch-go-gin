package csrf

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)
	token, err := service.GenerateToken()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Errorf("expected non-empty token, got empty string")
	}

	// Verify token format: nonce.issuedAt.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected token format nonce.issuedAt.signature, got %q", token)
	}

	// nonce = 32 bytes = 64 hex chars
	if len(parts[0]) != 64 {
		t.Errorf("expected nonce length 64, got %d", len(parts[0]))
	}

	// issued at = int64 seconds = 8 bytes = 16 hex chars
	if len(parts[1]) != 16 {
		t.Errorf("expected issued-at length 16, got %d", len(parts[1]))
	}

	// signature = SHA256 = 32 bytes = 64 hex chars
	if len(parts[2]) != 64 {
		t.Errorf("expected signature length 64, got %d", len(parts[2]))
	}
}

func TestGenerateTokenUniqueness(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)

	token1, _ := service.GenerateToken()
	token2, _ := service.GenerateToken()

	if token1 == token2 {
		t.Errorf("expected different tokens, but got same token: %s", token1)
	}
}

func TestGenerateTokenRandomness(t *testing.T) {
	service := NewCSRFService("test-secret", testCSRFTTL)
	tokens := make(map[string]bool)

	for i := 0; i < 100; i++ {
		token, err := service.GenerateToken()
		if err != nil {
			t.Fatalf("token generation failed on iteration %d: %v", i, err)
		}
		if tokens[token] {
			t.Errorf("generated duplicate token on iteration %d: %s", i, token)
		}
		tokens[token] = true
	}

	if len(tokens) != 100 {
		t.Errorf("expected 100 unique tokens, got %d", len(tokens))
	}
}
