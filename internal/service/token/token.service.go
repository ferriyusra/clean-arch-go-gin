package token

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// DefaultIssuer is used as the JWT `iss` claim when TokenConfig.Issuer is empty.
const DefaultIssuer = "boilerplate-golang-gin"

// TokenConfig holds JWT configuration
type TokenConfig struct {
	AccessTokenSecret  string
	AccessTokenExpiry  time.Duration
	RefreshTokenSecret string
	RefreshTokenExpiry time.Duration
	Issuer             string
}

// TokenClaims represents JWT claims
type TokenClaims struct {
	UserID uuid.UUID `json:"sub"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	jwt.RegisteredClaims
}

// TokenService handles token generation and validation
type TokenService interface {
	GenerateAccessToken(userID uuid.UUID, email, name string) (string, error)
	GenerateRefreshToken(userID uuid.UUID) (string, error)
	ValidateAccessToken(tokenString string) (*TokenClaims, error)
	ValidateRefreshToken(tokenString string) (*TokenClaims, error)
	RefreshTokenExpiry() time.Duration
}

// tokenService implements TokenService
type tokenService struct {
	config TokenConfig
}

// NewTokenService creates a new token service
func NewTokenService(config TokenConfig) TokenService {
	if config.Issuer == "" {
		config.Issuer = DefaultIssuer
	}
	return &tokenService{
		config: config,
	}
}

// RefreshTokenExpiry returns the configured refresh token TTL
func (s *tokenService) RefreshTokenExpiry() time.Duration {
	return s.config.RefreshTokenExpiry
}
