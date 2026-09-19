package user

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/ferriyusra/clean-arch-go-gin/internal/service/token"
)

// StoreRefreshToken records an issued refresh token so that it can later be
// verified and revoked.
//
// Only the digest is written. The caller passes the real token because that is
// what it has; turning it into something safe to persist is this method's job,
// and keeping that in one place is what makes "we never store raw tokens" a
// property of the system rather than a habit.
func (s *userService) StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenStr string, expiresAt time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	record := entity.RefreshTokenEntity{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: token.Hash(tokenStr),
		ExpiresAt: expiresAt,
	}

	if err := s.refreshTokenRepository.Create(ctx, record); err != nil {
		return apperr.Internal(fmt.Errorf("storing refresh token: %w", err))
	}
	return nil
}
