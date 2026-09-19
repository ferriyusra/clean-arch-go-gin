package entity

import (
	"time"

	"github.com/google/uuid"
)

// RefreshTokenEntity is the server-side record of an issued refresh token.
//
// Only a digest of the token is stored, never the token itself: a leaked
// database dump then yields nothing that can be replayed against the API.
//
// There is deliberately no gorm.DeletedAt here. A soft-deleted row would still
// occupy the unique index on TokenHash, and a revoked credential that lingers
// in the table is one that can be restored; revocation has to be a real delete.
type RefreshTokenEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	UserID    uuid.UUID `gorm:"index"`
	TokenHash string    `gorm:"uniqueIndex;size:64"`
	ExpiresAt time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (RefreshTokenEntity) TableName() string {
	return "refresh_token_entities"
}
