package counter

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// IncrementCounter increments the counter in GORM and returns the new value
func (r *GORMCounterRepository) IncrementCounter(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	var counter CounterModel

	// Use a transaction to atomically read and update the counter
	if err := dbtx.Conn(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		// Read current value with row-level lock
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&counter).Error; err != nil {
			return err
		}

		// Increment and update
		counter.Value++
		return tx.Save(&counter).Error
	}); err != nil {
		return 0, err
	}

	return counter.Value, nil
}
