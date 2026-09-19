package refresh_token

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// ListActiveByUserID returns a page of the user's unexpired refresh tokens,
// newest first.
//
// The expiry filter is not an optimisation: an expired row is not a session,
// and the janitor only sweeps on its interval, so between sweeps the table
// still holds rows that no longer authenticate anything. Reporting those as
// live sessions would show a user devices they were signed out of hours ago.
//
// The ordering is by created_at with the primary key as a tiebreak, so two
// tokens minted in the same clock tick still page deterministically instead of
// swapping places between requests and duplicating or skipping a row.
func (r *GORMRefreshTokenRepository) ListActiveByUserID(
	ctx context.Context,
	userID uuid.UUID,
	now time.Time,
	limit, offset int,
) ([]entity.RefreshTokenEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var tokens []entity.RefreshTokenEntity
	if err := dbtx.Conn(ctx, r.db).
		Where("user_id = ? AND expires_at > ?", userID, now).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Offset(offset).
		Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("listing active refresh tokens for user %s: %w", userID, err)
	}

	return tokens, nil
}
