package refresh_token

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"gorm.io/gorm"
)

// FindByToken finds a refresh token by its token string
func (r *GORMRefreshTokenRepository) FindByToken(ctx context.Context, tokenString string) (*entity.RefreshTokenEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var token entity.RefreshTokenEntity
	if err := r.db.WithContext(ctx).Where("token = ?", tokenString).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("finding refresh token: %w", err)
	}
	return &token, nil
}
