package database

import (
	"context"
	"fmt"
)

// dbKey is the private context key for storing the database connection
type dbKey struct{}

// FromContext retrieves the *Database instance from the given context.Context.
// Returns an error if no database connection is found in the context.
func FromContext(ctx context.Context) (*Database, error) {
	db, ok := ctx.Value(dbKey{}).(*Database)
	if !ok {
		return nil, fmt.Errorf("database connection not found in context")
	}
	return db, nil
}

// ContextWith returns a new context.Context that contains the given *Database instance.
func ContextWith(ctx context.Context, db *Database) context.Context {
	return context.WithValue(ctx, dbKey{}, db)
}
