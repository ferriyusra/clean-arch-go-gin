package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserEntity represents a user in the system.
//
// Password holds a bcrypt digest, never a plaintext password.
type UserEntity struct {
	ID        uuid.UUID `gorm:"primaryKey"`
	Email     string    `gorm:"uniqueIndex;size:255;not null"`
	Password  []byte    `gorm:"not null"`
	Name      string    `gorm:"size:255;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (UserEntity) TableName() string {
	return "users"
}
