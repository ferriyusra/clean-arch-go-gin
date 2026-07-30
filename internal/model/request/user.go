package request

// RegisterUserRequest is the payload for POST /api/auth/register.
//
// Password is capped at 72 bytes because bcrypt silently ignores anything past
// that — without the cap, part of a longer password would not be checked at login.
type RegisterUserRequest struct {
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Name     string `json:"name" binding:"required,min=1,max=255"`
}

// LoginRequest is the payload for POST /api/auth/login.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,max=72"`
}

// RefreshTokenRequest is the payload for POST /api/auth/refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}
