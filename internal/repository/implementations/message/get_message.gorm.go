package message

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// GetMessage returns a stored message from GORM
func (r *GORMMessageRepository) GetMessage(ctx context.Context, key string) (*string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var message MessageModel
	if err := dbtx.Conn(ctx, r.db).Where("key = ?", key).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting message: %w", err)
	}
	resp := message.Value

	return &resp, nil
}
