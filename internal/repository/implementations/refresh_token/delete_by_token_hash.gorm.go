package refresh_token

import (
	"context"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// DeleteByTokenHash removes a single stored refresh token.
//
// Deleting a row that is not there is not an error: rotation and logout can
// race, and both must be able to succeed.
func (r *GORMRefreshTokenRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := r.db.WithContext(ctx).
		Where("token_hash = ?", tokenHash).
		Delete(&entity.RefreshTokenEntity{}).Error; err != nil {
		return fmt.Errorf("deleting refresh token: %w", err)
	}
	return nil
}
