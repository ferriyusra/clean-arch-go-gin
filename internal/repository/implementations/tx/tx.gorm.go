package tx

import (
	"context"

	"gorm.io/gorm"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/dbtx"
)

// GORMTxManager is a GORM implementation of TxManager.
type GORMTxManager struct {
	db *gorm.DB
}

// NewGORMTxManager creates a new GORM transaction manager.
func NewGORMTxManager(db *gorm.DB) *GORMTxManager {
	return &GORMTxManager{db: db}
}

// WithinTx runs fn inside a transaction.
//
// A nested call reuses the transaction already on the context instead of
// opening a second one. Without that, a service that composes two transactional
// operations would deadlock on databases that do not allow concurrent writes,
// sqlite among them.
func (m *GORMTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := dbtx.From(ctx); ok {
		return fn(ctx)
	}

	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(dbtx.Into(ctx, tx))
	})
}
