package refresh_token

import (
	"context"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// Create stores a new refresh token
func (r *GORMRefreshTokenRepository) Create(ctx context.Context, token entity.RefreshTokenEntity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := dbtx.Conn(ctx, r.db).Create(&token).Error; err != nil {
		return fmt.Errorf("creating refresh token: %w", err)
	}
	return nil
}
