package user

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
)

// GORMUserRepository is a GORM implementation of UserRepository
type GORMUserRepository struct {
	db *gorm.DB
}

// UserModel represents the users table schema
type UserModel = entity.UserEntity

// NewGORMUserRepository creates a new GORM user repository.
//
// Schema migration is not performed here — see platform.Migrate.
func NewGORMUserRepository(db *gorm.DB) *GORMUserRepository {
	return &GORMUserRepository{
		db: db,
	}
}
