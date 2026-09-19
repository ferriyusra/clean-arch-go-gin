package refresh_token

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// DeleteByUserID deletes all refresh tokens for a given user
func (r *GORMRefreshTokenRepository) DeleteByUserID(ctx context.Context, userID uuid.UUID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := dbtx.Conn(ctx, r.db).Where("user_id = ?", userID).Delete(&RefreshTokenModel{}).Error; err != nil {
		return fmt.Errorf("deleting refresh tokens for user %s: %w", userID, err)
	}
	return nil
}
