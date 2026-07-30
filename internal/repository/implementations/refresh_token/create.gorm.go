package refresh_token

import (
	"context"
	"fmt"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
)

// Create stores a new refresh token
func (r *GORMRefreshTokenRepository) Create(ctx context.Context, token entity.RefreshTokenEntity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := r.db.WithContext(ctx).Create(&token).Error; err != nil {
		return fmt.Errorf("creating refresh token: %w", err)
	}
	return nil
}
