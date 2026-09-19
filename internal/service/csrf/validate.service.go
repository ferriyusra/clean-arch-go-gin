package csrf

import (
	"crypto/hmac"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"
)

// ValidateToken reports whether a token was issued by this service and is still
// within its lifetime.
func (s *csrfService) ValidateToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}

	nonce, err := hex.DecodeString(parts[0])
	if err != nil || len(nonce) != nonceLength {
		return false
	}

	issuedAt, err := hex.DecodeString(parts[1])
	if err != nil || len(issuedAt) != 8 {
		return false
	}

	signature, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}

	// The signature is checked before the timestamp is read. Until the MAC
	// verifies, the timestamp is attacker-controlled input and must not be used
	// for anything, not even a comparison.
	if !hmac.Equal(signature, s.sign(nonce, issuedAt)) {
		return false
	}

	age := s.now().Sub(time.Unix(int64(binary.BigEndian.Uint64(issuedAt)), 0))

	// A token from the future beyond the allowed skew is as suspect as an
	// expired one.
	return age >= -clockSkew && age <= s.ttl
}
