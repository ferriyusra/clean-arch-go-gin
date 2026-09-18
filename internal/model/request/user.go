package request

// RegisterUserRequest is the payload for POST /api/auth/register.
//
// Validation is declarative so that every rule is visible in one place and the
// handler stays free of hand-rolled checks. The password max of 72 is not
// arbitrary: bcrypt refuses inputs longer than 72 bytes, so without it a long
// password would surface as a 500 from the hashing step.
type RegisterUserRequest struct {
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Name     string `json:"name"     binding:"required,min=1,max=100"`
}

// LoginRequest is the payload for POST /api/auth/login.
//
// Login deliberately validates only presence: rejecting a malformed password
// here would tell an attacker which stored passwords cannot exist.
type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,max=72"`
}
