package database

import (
	"context"
	"database/sql"
	"fmt"
)

// dbKey is the private context key for storing the database connection
type dbKey struct{}

// FromContext retrieves the *sql.DB instance from the given context.Context.
// Returns an error if no database connection is found in the context.
func FromContext(ctx context.Context) (*sql.DB, error) {
	db, ok := ctx.Value(dbKey{}).(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("database connection not found in context")
	}
	return db, nil
}

// ContextWith returns a new context.Context that contains the given *sql.DB instance.
func ContextWith(ctx context.Context, db *sql.DB) context.Context {
	return context.WithValue(ctx, dbKey{}, db)
}
