package interfaces

import "context"

// TxManager runs a unit of work inside a database transaction.
//
// It lives beside the repository contracts because a service needs to depend on
// it, and a service must not know that the store is GORM, or SQL at all.
type TxManager interface {
	// WithinTx runs fn inside a transaction, passing it a context that carries
	// the transaction. Every repository call made with that context joins it.
	//
	// The transaction commits when fn returns nil and rolls back otherwise, so
	// a service signals failure by returning an error rather than by calling
	// anything. Calls nest: a WithinTx inside a WithinTx reuses the outer
	// transaction rather than opening a second one.
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
