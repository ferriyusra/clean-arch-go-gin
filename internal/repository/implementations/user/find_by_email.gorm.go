package user

import (
	"context"
	"errors"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"gorm.io/gorm"
)

// FindByEmail finds a user by email in GORM
func (r *GORMUserRepository) FindByEmail(ctx context.Context, email string) (*entity.UserEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var user entity.UserEntity
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}
