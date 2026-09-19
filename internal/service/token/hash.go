package token

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns the digest under which a refresh token is stored.
//
// SHA-256 rather than bcrypt on purpose: a refresh token is 256 bits of
// generated randomness, not a human-chosen password, so there is no dictionary
// to attack and nothing for a slow hash to buy. It does have to be fast,
// because every refresh looks a token up by its digest.
//
// This is a package-level function rather than a TokenService method so that
// hashing cannot be mocked away: a test that stubbed it could pass while the
// real code stored raw tokens.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
