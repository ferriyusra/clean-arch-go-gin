package interfaces

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
	"github.com/google/uuid"
)

// UserRepository defines the interface for user data access
type UserRepository interface {
	Create(ctx context.Context, user entity.UserEntity) (*uuid.UUID, error)
	FindByID(ctx context.Context, id uuid.UUID) (*entity.UserEntity, error)
	FindByEmail(ctx context.Context, email string) (*entity.UserEntity, error)
	Update(ctx context.Context, id uuid.UUID, user entity.UserEntity) error
	Delete(ctx context.Context, id uuid.UUID) error
}
