package user

import "errors"

// Sentinel errors returned by the user service.
//
// Handlers match on these to choose an HTTP status and a safe client-facing
// message. Anything else coming out of the service is an internal failure whose
// text must never reach the client — it may carry database or driver detail.
var (
	ErrEmailAlreadyRegistered = errors.New("email is already registered")
	ErrInvalidCredentials     = errors.New("invalid email or password")
	ErrInvalidRefreshToken    = errors.New("invalid refresh token")
	ErrRefreshTokenRevoked    = errors.New("refresh token has been revoked")
	ErrRefreshTokenExpired    = errors.New("refresh token has expired")
	ErrUserNotFound           = errors.New("user not found")
	ErrInvalidUserID          = errors.New("invalid user id")
)
