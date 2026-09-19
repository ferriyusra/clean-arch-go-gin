package refresh_token

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// FindByTokenHash finds a stored refresh token by its digest.
//
// A token that is simply not there is (nil, nil), not an error: the service
// treats a missing row as a revoked or already-rotated token, which is a normal
// outcome rather than a failure.
func (r *GORMRefreshTokenRepository) FindByTokenHash(ctx context.Context, tokenHash string) (*entity.RefreshTokenEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var token entity.RefreshTokenEntity
	if err := dbtx.Conn(ctx, r.db).Where("token_hash = ?", tokenHash).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("finding refresh token: %w", err)
	}

	return &token, nil
}
