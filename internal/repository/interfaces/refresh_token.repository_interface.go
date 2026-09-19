package interfaces

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// RefreshTokenRepository defines the interface for refresh token data access.
//
// Every method that identifies a token takes a *hash*, not the token. Hashing
// belongs to the service layer, and naming the parameter for what it is keeps a
// raw token from being passed in here by accident.
type RefreshTokenRepository interface {
	Create(ctx context.Context, token entity.RefreshTokenEntity) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*entity.RefreshTokenEntity, error)
	// ListActiveByUserID returns the user's unexpired tokens, newest first,
	// windowed by limit and offset.
	//
	// "Active" is relative to the instant the caller passes rather than to the
	// database's clock, so the service keeps a single, swappable notion of now
	// and a row that expired a second ago is not reported as a live session.
	ListActiveByUserID(ctx context.Context, userID uuid.UUID, now time.Time, limit, offset int) ([]entity.RefreshTokenEntity, error)
	// CountActiveByUserID counts the same rows ListActiveByUserID would return
	// without its window, which is what a pagination total means.
	CountActiveByUserID(ctx context.Context, userID uuid.UUID, now time.Time) (int64, error)
	DeleteByUserID(ctx context.Context, userID uuid.UUID) error
	DeleteByTokenHash(ctx context.Context, tokenHash string) error
	// DeleteExpired removes every token that expired before the given instant
	// and reports how many rows went. Nothing reads an expired row, so without
	// this the table only grows.
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
