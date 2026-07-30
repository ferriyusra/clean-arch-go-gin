package platform

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/ferriyusra/boilerplate-golang-gin/internal/model/entity"
)

// migrationModels lists every entity whose table is managed by AutoMigrate.
// Add new entities here when you add them to internal/model/entity.
var migrationModels = []any{
	&entity.UserEntity{},
	&entity.RefreshTokenEntity{},
}

// Migrate applies the schema for all registered entities.
//
// AutoMigrate is convenient but it is not a versioned migration tool: it only
// adds columns and indexes, and never drops or alters existing ones. For real
// deployments set DATABASE_AUTO_MIGRATE=false and manage the schema with a
// dedicated migration tool (golang-migrate, atlas, goose) instead.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(migrationModels...); err != nil {
		return fmt.Errorf("running auto-migration: %w", err)
	}
	return nil
}
