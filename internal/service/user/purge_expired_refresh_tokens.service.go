package user

import (
	"context"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/apperr"
)

// PurgeExpiredRefreshTokens deletes refresh tokens that are past their expiry
// and reports how many went.
//
// Nothing ever reads an expired row again: Refresh rejects it and issues
// nothing. Without a sweep the table grows for the life of the service, and
// every lookup pays for rows that can never match.
func (s *userService) PurgeExpiredRefreshTokens(ctx context.Context) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	removed, err := s.refreshTokenRepository.DeleteExpired(ctx, s.now())
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("purging expired refresh tokens: %w", err))
	}
	return removed, nil
}
