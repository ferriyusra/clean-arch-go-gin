package user

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// Update updates a user in GORM
func (r *GORMUserRepository) Update(ctx context.Context, id uuid.UUID, user entity.UserEntity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := dbtx.Conn(ctx, r.db).
		Model(&UserModel{}).Where("id = ?", id).
		Updates(&user).
		Error; err != nil {
		return err
	}

	return nil
}
