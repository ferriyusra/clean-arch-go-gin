package apperr

// Sentinel errors shared across layers. Handlers and tests compare against
// these with errors.Is; the message on each is what the client sees.
//
// Anything not listed here is treated as internal: the cause is logged and the
// client gets ErrInternal's message instead.
var (
	// Generic
	ErrInternal   = New(CodeInternal, "Internal server error")
	ErrTimeout    = New(CodeTimeout, "Request timed out")
	ErrCanceled   = New(CodeTimeout, "Request was canceled")
	ErrValidation = New(CodeInvalidInput, "Validation failed")
	ErrBadRequest = New(CodeInvalidInput, "Invalid request body")
	ErrRateLimit  = New(CodeForbidden, "Too many requests")

	// Auth / user
	ErrInvalidCredentials = New(CodeUnauthorized, "Invalid email or password")
	ErrUserAlreadyExists  = New(CodeConflict, "Email is already registered")
	ErrUserNotFound       = New(CodeNotFound, "User not found")
	ErrInvalidUserID      = New(CodeInvalidInput, "Invalid user id")
	ErrUnauthorized       = New(CodeUnauthorized, "Unauthorized")

	// Tokens
	ErrMissingAccessToken  = New(CodeUnauthorized, "Missing authentication token")
	ErrInvalidAccessToken  = New(CodeUnauthorized, "Invalid or expired token")
	ErrMissingRefreshToken = New(CodeUnauthorized, "Missing refresh token")
	ErrInvalidRefreshToken = New(CodeUnauthorized, "Invalid refresh token")
	ErrRefreshTokenRevoked = New(CodeUnauthorized, "Refresh token has been revoked")
	ErrRefreshTokenExpired = New(CodeUnauthorized, "Refresh token has expired")

	// CSRF
	ErrMissingCSRFToken = New(CodeForbidden, "Missing CSRF token")
	ErrInvalidCSRFToken = New(CodeForbidden, "Invalid CSRF token")

	// Health
	ErrUnhealthy = New(CodeUnavailable, "Service is not ready")
)
