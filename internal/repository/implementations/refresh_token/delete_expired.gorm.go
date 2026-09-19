package refresh_token

import (
	"context"
	"fmt"
	"time"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// DeleteExpired removes every token that expired before the given instant.
func (r *GORMRefreshTokenRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	result := dbtx.Conn(ctx, r.db).
		Where("expires_at < ?", before).
		Delete(&entity.RefreshTokenEntity{})
	if result.Error != nil {
		return 0, fmt.Errorf("deleting expired refresh tokens: %w", result.Error)
	}

	return result.RowsAffected, nil
}
