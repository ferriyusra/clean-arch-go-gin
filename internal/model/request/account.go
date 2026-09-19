package request

// ChangePasswordRequest is the payload for PATCH /api/v1/auth/password.
//
// NewPassword carries the same rules Register's password does, and for the same
// reason: bcrypt refuses inputs longer than 72 bytes, so without the max a long
// password surfaces as a 500 from the hashing step rather than as a field-level
// validation error the client can show.
//
// CurrentPassword validates only presence and the bcrypt ceiling, the way Login
// does. Applying the min here would tell a caller that the stored password
// cannot be shorter than eight characters, and would reject a legitimate
// attempt to replace a password that predates that rule.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required,max=72"`
	NewPassword     string `json:"newPassword"     binding:"required,min=8,max=72"`
}
