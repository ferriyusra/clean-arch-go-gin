package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserEntity represents a user in the system
type UserEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	Email     string    `gorm:"uniqueIndex"`
	Password  []byte
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UserEntity) TableName() string {
	return "user_entities"
}
