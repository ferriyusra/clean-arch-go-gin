package interfaces

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"
)

// RefreshTokenRepository defines the interface for refresh token data access
type RefreshTokenRepository interface {
	Create(ctx context.Context, token entity.RefreshTokenEntity) error
	FindByToken(ctx context.Context, tokenString string) (*entity.RefreshTokenEntity, error)
	DeleteByUserID(ctx context.Context, userID uuid.UUID) error
	DeleteByToken(ctx context.Context, tokenString string) error
}
