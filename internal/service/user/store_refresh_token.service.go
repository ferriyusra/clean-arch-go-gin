package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// StoreRefreshToken persists a refresh token so that it can later be revoked.
func (s *userService) StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenStr string, expiresAt time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	token := entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		Token:     tokenStr,
		ExpiresAt: expiresAt,
	}

	if err := s.refreshTokenRepository.Create(ctx, token); err != nil {
		return apperr.Internal(fmt.Errorf("storing refresh token: %w", err))
	}
	return nil
}
