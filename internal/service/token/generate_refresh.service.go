package token

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// GenerateRefreshToken generates a new refresh token.
//
// The random ID is load-bearing, not decoration. Without it, two refresh tokens
// minted for the same user inside the same second carry identical claims and
// therefore sign to identical strings. Rotation would then hand back the token
// it was meant to replace, and the unique index on the stored digest would
// reject the second one as a duplicate.
func (s *tokenService) GenerateRefreshToken(userID uuid.UUID) (string, error) {
	now := time.Now()

	claims := TokenClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.RefreshTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "go-vite-react",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.RefreshTokenSecret))
}
