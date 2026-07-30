package refresh_token

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
)

// FindByTokenHash finds a refresh token by its SHA-256 hash.
// Returns (nil, nil) when no matching token exists.
func (r *GORMRefreshTokenRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*entity.RefreshTokenEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var token entity.RefreshTokenEntity
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("finding refresh token: %w", err)
	}
	return &token, nil
}
