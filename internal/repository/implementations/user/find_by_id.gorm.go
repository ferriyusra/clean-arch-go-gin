package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// FindByID finds a user by ID in GORM
func (r *GORMUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*entity.UserEntity, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var user entity.UserEntity
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("finding user by id: %w", err)
	}

	return &user, nil
}
