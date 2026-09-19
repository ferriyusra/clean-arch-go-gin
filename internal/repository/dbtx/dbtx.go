// Package dbtx carries a database transaction through context.Context.
//
// Repositories stay unaware of whether they are inside a transaction: they ask
// for a connection and get either the pooled handle or the ambient transaction.
// That keeps the transaction boundary where it belongs, in the service that
// knows which writes have to succeed or fail together, without threading a
// *gorm.DB through every repository signature.
package dbtx

import (
	"context"

	"gorm.io/gorm"
)

// contextKey is unexported so nothing outside this package can put a value
// under it, which is what makes From trustworthy.
type contextKey struct{}

// Into returns a context carrying tx.
func Into(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, contextKey{}, tx)
}

// From returns the ambient transaction, if the caller is inside one.
func From(ctx context.Context) (*gorm.DB, bool) {
	tx, ok := ctx.Value(contextKey{}).(*gorm.DB)
	return tx, ok && tx != nil
}

// Conn returns the connection a repository should use: the ambient transaction
// when there is one, otherwise the pooled handle it was constructed with.
//
// Every repository method calls this instead of touching its own *gorm.DB, so
// a write silently joins whatever transaction its caller opened.
func Conn(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx, ok := From(ctx); ok {
		return tx.WithContext(ctx)
	}
	return fallback.WithContext(ctx)
}
