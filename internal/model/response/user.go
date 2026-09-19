package response

import "github.com/google/uuid"

// GetUser represents a user in the system
type GetUser struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}

// LoginResponse is returned by UserService.Login.
//
// The tokens are marked json:"-" on purpose: they reach the client as HttpOnly
// cookies set by the handler and must never appear in a response body, but the
// handler still needs them from the service.
type LoginResponse struct {
	User         GetUser `json:"user"`
	AccessToken  string  `json:"-"`
	RefreshToken string  `json:"-"`
}

// RegisterResponse is returned by UserService.Register. See LoginResponse for
// why the tokens are excluded from the JSON body.
type RegisterResponse struct {
	User         GetUser `json:"user"`
	AccessToken  string  `json:"-"`
	RefreshToken string  `json:"-"`
}

// RefreshResponse carries a freshly minted token pair, destined for cookies
// rather than the response body.
//
// Refresh returns a new *refresh* token as well, not just an access token:
// every refresh rotates the pair so that a stolen refresh token has a usable
// life measured in one request rather than in days.
type RefreshResponse struct {
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
}

// ChangePasswordResponse carries the token pair minted for the client that
// changed the password.
//
// Changing a password revokes every session for the account, including the one
// that made the request. Re-issuing a pair for the acting client is what keeps
// that client signed in while every other device is signed out, which is the
// behaviour a user expects and the reason they changed the password.
//
// The tokens are json:"-" for the same reason as everywhere else: they reach
// the client as HttpOnly cookies, never in a body.
type ChangePasswordResponse struct {
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
}

type CSRFTokenResponse struct {
	Token string `json:"token"`
}
