package user

import (
	"context"

	"github.com/google/uuid"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// Delete deletes a user in GORM
func (r *GORMUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := dbtx.Conn(ctx, r.db).Delete(&UserModel{}, id).Error; err != nil {
		return err
	}

	return nil
}
