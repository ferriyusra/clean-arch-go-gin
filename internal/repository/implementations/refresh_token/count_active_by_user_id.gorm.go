package refresh_token

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// CountActiveByUserID counts the user's unexpired refresh tokens.
//
// The predicate is deliberately identical to ListActiveByUserID's. A count that
// filters differently from the listing produces a pagination total that does
// not match the rows, which shows up as an empty last page.
func (r *GORMRefreshTokenRepository) CountActiveByUserID(
	ctx context.Context,
	userID uuid.UUID,
	now time.Time,
) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	var total int64
	if err := dbtx.Conn(ctx, r.db).
		Model(&entity.RefreshTokenEntity{}).
		Where("user_id = ? AND expires_at > ?", userID, now).
		Count(&total).Error; err != nil {
		return 0, fmt.Errorf("counting active refresh tokens for user %s: %w", userID, err)
	}

	return total, nil
}
