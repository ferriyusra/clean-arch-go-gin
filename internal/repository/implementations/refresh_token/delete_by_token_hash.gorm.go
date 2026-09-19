package refresh_token

import (
	"context"
	"fmt"
)

// DeleteByToken deletes a specific refresh token
func (r *GORMRefreshTokenRepository) DeleteByToken(ctx context.Context, tokenString string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := r.db.WithContext(ctx).Where("token = ?", tokenString).Delete(&RefreshTokenModel{}).Error; err != nil {
		return fmt.Errorf("deleting refresh token: %w", err)
	}
	return nil
}
