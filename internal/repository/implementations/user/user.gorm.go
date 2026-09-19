package user

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// GORMUserRepository is a GORM implementation of UserRepository
type GORMUserRepository struct {
	db *gorm.DB
}

// UserModel represents the users table schema
type UserModel = entity.UserEntity

// NewGORMUserRepository creates a new GORM user repository.
//
// The schema is applied once at startup by platform.Migrate, not here: a
// constructor that migrates makes the schema a side effect of wiring and runs
// DDL four times on every boot.
func NewGORMUserRepository(db *gorm.DB) *GORMUserRepository {
	return &GORMUserRepository{
		db: db,
	}
}
