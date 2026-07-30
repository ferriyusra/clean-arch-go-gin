package response

import "github.com/google/uuid"

// GetUser represents a user in the system
type GetUser struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}

type LoginResponse struct {
	User         GetUser `json:"user"`
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken"`
}

type RegisterResponse struct {
	User GetUser `json:"user"`
}

type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}
