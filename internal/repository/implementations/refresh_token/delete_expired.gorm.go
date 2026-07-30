package refresh_token

import (
	"context"
	"fmt"
	"time"
)

// DeleteExpired removes every refresh token that expired before the given time
// and reports how many rows were deleted. Without this the table grows forever,
// since expired tokens are never read again.
func (r *GORMRefreshTokenRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	result := r.db.WithContext(ctx).Where("expires_at < ?", before).Delete(&RefreshTokenModel{})
	if result.Error != nil {
		return 0, fmt.Errorf("deleting expired refresh tokens: %w", result.Error)
	}
	return result.RowsAffected, nil
}
