package refresh_token

import (
	"context"
	"fmt"
)

// DeleteByTokenHash deletes a specific refresh token by its SHA-256 hash
func (r *GORMRefreshTokenRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&RefreshTokenModel{}).Error; err != nil {
		return fmt.Errorf("deleting refresh token: %w", err)
	}
	return nil
}
