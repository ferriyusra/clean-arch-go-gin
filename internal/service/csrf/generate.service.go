package csrf

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

// nonceLength is the random part of a token, in bytes.
const nonceLength = 32

// GenerateToken produces a token of the form nonce.issuedAt.signature.
//
// The issue time is inside the signed material, not merely appended, so it
// cannot be edited to extend the life of a captured token.
func (s *csrfService) GenerateToken() (string, error) {
	nonce := make([]byte, nonceLength)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating CSRF nonce: %w", err)
	}

	issuedAt := make([]byte, 8)
	// The uint64 is only the wire encoding. ValidateToken converts it straight
	// back to int64, and that round trip preserves every bit, so no value can
	// overflow on the way.
	binary.BigEndian.PutUint64(issuedAt, uint64(s.now().Unix())) //nolint:gosec // G115: lossless round trip, see above.

	return hex.EncodeToString(nonce) + "." +
		hex.EncodeToString(issuedAt) + "." +
		hex.EncodeToString(s.sign(nonce, issuedAt)), nil
}

func (s *csrfService) sign(nonce, issuedAt []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(nonce)
	mac.Write(issuedAt)
	return mac.Sum(nil)
}
