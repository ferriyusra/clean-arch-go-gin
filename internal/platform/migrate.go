package platform

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// entities lists every table the application owns, in dependency order.
//
// Adding a table here is the single step required to ship a schema change; the
// repositories no longer migrate themselves, so the schema is applied once at
// startup rather than implicitly four times from four constructors.
func entities() []any {
	return []any{
		&entity.UserEntity{},
		&entity.RefreshTokenEntity{},
		&entity.CounterEntity{},
		&entity.MessageEntity{},
	}
}

// Migrate applies the schema and seeds the rows the demo endpoints expect.
//
// AutoMigrate is additive only: it creates tables, columns and indexes but
// never drops or rewrites them. That is fine for development and for
// forward-only changes; a destructive change (renaming or dropping a column,
// backfilling data) needs a real migration tool such as golang-migrate or
// goose, added alongside this function.
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(entities()...); err != nil {
		return fmt.Errorf("auto-migrating schema: %w", err)
	}

	if err := seed(db); err != nil {
		return fmt.Errorf("seeding database: %w", err)
	}

	return nil
}

// seed inserts the singleton rows the counter and message endpoints read. It is
// idempotent: a non-empty table is left untouched.
func seed(db *gorm.DB) error {
	var counters int64
	if err := db.Model(&entity.CounterEntity{}).Count(&counters).Error; err != nil {
		return fmt.Errorf("counting counters: %w", err)
	}
	if counters == 0 {
		if err := db.Create(&entity.CounterEntity{ID: uuid.New(), Value: 0}).Error; err != nil {
			return fmt.Errorf("seeding counter: %w", err)
		}
	}

	var messages int64
	if err := db.Model(&entity.MessageEntity{}).Count(&messages).Error; err != nil {
		return fmt.Errorf("counting messages: %w", err)
	}
	if messages == 0 {
		msg := &entity.MessageEntity{
			ID:    uuid.New(),
			Key:   "default",
			Value: "Welcome to Clean Go Vite React!",
		}
		if err := db.Create(msg).Error; err != nil {
			return fmt.Errorf("seeding message: %w", err)
		}
	}

	return nil
}
