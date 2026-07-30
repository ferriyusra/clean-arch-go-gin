package user

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
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
