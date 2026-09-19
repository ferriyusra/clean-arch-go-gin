package testutil

import (
	"context"

	"github.com/ferriyusra/clean-arch-go-gin/internal/repository/interfaces"
)

// PassthroughTx is a TxManager that runs the unit of work directly, with no
// transaction at all.
//
// Service tests mock their repositories, so there is no database to roll back
// and a real transaction would only add noise to the expectations. What those
// tests check is that the right calls happen in the right order; that the
// rollback itself works is checked against a real database in the transaction
// manager tests, which is the only place it can be checked honestly.
type PassthroughTx struct{}

// WithinTx satisfies interfaces.TxManager.
func (PassthroughTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

var _ interfaces.TxManager = PassthroughTx{}
