package message

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// GetMessage returns a stored message from GORM
func (r *GORMMessageRepository) GetMessage(ctx context.Context, key string) (*string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var message MessageModel
	if err := r.db.WithContext(ctx).Where("key = ?", key).First(&message).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting message: %w", err)
	}
	resp := message.Value

	return &resp, nil
}
