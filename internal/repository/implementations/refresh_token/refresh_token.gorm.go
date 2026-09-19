package refresh_token

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// GORMRefreshTokenRepository is a GORM implementation of RefreshTokenRepository
type GORMRefreshTokenRepository struct {
	db *gorm.DB
}

// RefreshTokenModel represents the refresh_token_entities table schema
type RefreshTokenModel = entity.RefreshTokenEntity

// NewGORMRefreshTokenRepository creates a new GORM refresh token repository.
//
// Schema migration lives in platform.Migrate.
func NewGORMRefreshTokenRepository(db *gorm.DB) *GORMRefreshTokenRepository {
	return &GORMRefreshTokenRepository{db: db}
}
