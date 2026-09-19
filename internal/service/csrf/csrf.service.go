package csrf

import "time"

// clockSkew is how far in the future a token may claim to have been issued
// before it is rejected, so that instances whose clocks differ slightly do not
// refuse each other tokens.
const clockSkew = time.Minute

// CSRFService handles CSRF token generation and validation
type CSRFService interface {
	GenerateToken() (string, error)
	ValidateToken(token string) bool
}

// csrfService implements CSRFService.
//
// Tokens are stateless: nothing is stored server side, and validity is decided
// by recomputing an HMAC. That keeps the service horizontally scalable, but it
// also means a token cannot be revoked once issued, which is why it carries an
// issue time and stops being accepted after ttl.
type csrfService struct {
	secret []byte
	ttl    time.Duration
	// now is swappable so tests can move time instead of sleeping.
	now func() time.Time
}

// NewCSRFService creates a new CSRF service with HMAC-based token validation.
func NewCSRFService(secret string, ttl time.Duration) CSRFService {
	return &csrfService{
		secret: []byte(secret),
		ttl:    ttl,
		now:    time.Now,
	}
}
