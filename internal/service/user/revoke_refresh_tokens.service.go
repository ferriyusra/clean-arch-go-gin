package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
)

// RevokeRefreshTokens deletes all refresh tokens for a user, ending every
// active session for that account.
func (s *userService) RevokeRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := s.refreshTokenRepository.DeleteByUserID(ctx, userID); err != nil {
		return apperr.Internal(fmt.Errorf("revoking refresh tokens: %w", err))
	}
	return nil
}
