package message

import (
	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/model/entity"
)

// GORMMessageRepository is a GORM implementation of MessageRepository
type GORMMessageRepository struct {
	db *gorm.DB
}

// MessageModel represents the message table schema
type MessageModel = entity.MessageEntity

// NewGORMMessageRepository creates a new GORM message repository.
//
// Schema migration and seeding of the default message live in platform.Migrate.
func NewGORMMessageRepository(db *gorm.DB) *GORMMessageRepository {
	return &GORMMessageRepository{
		db: db,
	}
}
