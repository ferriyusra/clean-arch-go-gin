package interfaces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
)

// RefreshTokenRepository defines the interface for refresh token data access.
//
// Tokens are addressed by their SHA-256 hash — callers hash the raw token before
// calling in, so plaintext tokens never reach the persistence layer.
type RefreshTokenRepository interface {
	Create(ctx context.Context, token entity.RefreshTokenEntity) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*entity.RefreshTokenEntity, error)
	DeleteByUserID(ctx context.Context, userID uuid.UUID) error
	DeleteByTokenHash(ctx context.Context, tokenHash string) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
