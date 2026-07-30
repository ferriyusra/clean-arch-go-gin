package user

import (
	"context"
	"time"
)

// PurgeExpiredRefreshTokens deletes refresh tokens that are past their expiry and
// reports how many were removed. Expired tokens are never read again, so without
// a periodic purge the table only grows. Called by the janitor in cmd/server.
func (s *userService) PurgeExpiredRefreshTokens(ctx context.Context) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	return s.refreshTokenRepository.DeleteExpired(ctx, time.Now())
}
