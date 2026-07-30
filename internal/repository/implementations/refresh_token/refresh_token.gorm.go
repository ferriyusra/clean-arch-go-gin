package refresh_token

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
)

// GORMRefreshTokenRepository is a GORM implementation of RefreshTokenRepository
type GORMRefreshTokenRepository struct {
	db *gorm.DB
}

// RefreshTokenModel represents the refresh_tokens table schema
type RefreshTokenModel = entity.RefreshTokenEntity

// NewGORMRefreshTokenRepository creates a new GORM refresh token repository.
//
// Schema migration is not performed here — see platform.Migrate.
func NewGORMRefreshTokenRepository(db *gorm.DB) *GORMRefreshTokenRepository {
	return &GORMRefreshTokenRepository{db: db}
}
