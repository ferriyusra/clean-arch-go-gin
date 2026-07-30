package token

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashToken returns the hex-encoded SHA-256 digest of a token.
//
// Refresh tokens are persisted as this digest rather than in plaintext, so a
// leaked database cannot be replayed against the auth endpoints. SHA-256 is the
// right primitive here (rather than bcrypt) because tokens are already
// high-entropy secrets, and lookups must be a single indexed query.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
