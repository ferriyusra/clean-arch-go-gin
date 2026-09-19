package counter

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// GetCounter returns the current counter value from GORM
func (r *GORMCounterRepository) GetCounter(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	var counter CounterModel
	if err := dbtx.Conn(ctx, r.db).First(&counter).Error; err != nil {
		return 0, err
	}

	return counter.Value, nil
}
