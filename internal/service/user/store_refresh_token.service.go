package user

import (
	"context"
	"fmt"
	"time"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"
)

// StoreRefreshToken persists a refresh token in the database
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
		return fmt.Errorf("storing refresh token: %w", err)
	}
	return nil
}
