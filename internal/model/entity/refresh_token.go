package entity

import (
	"time"

	"github.com/google/uuid"
)

// RefreshTokenEntity represents a refresh token stored in the database.
//
// Only the SHA-256 hash of the token is persisted — never the raw token — so a
// database leak cannot be replayed against the auth endpoints. Rows are hard
// deleted on revocation so a revoked token can never be resurrected.
type RefreshTokenEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	UserID    uuid.UUID `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RefreshTokenEntity) TableName() string {
	return "refresh_tokens"
}
