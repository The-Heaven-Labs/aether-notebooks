// Package database provides PostgreSQL connection pool management and schema migrations.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB manages the PostgreSQL connection pool and schema migrations.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect establishes a PostgreSQL connection pool using the provided URL.
// If schema is non-empty, SET search_path is executed after connecting to
// ensure tables are found in the correct schema (avoids PgBouncer filtering
// of search_path as a startup parameter).
func Connect(ctx context.Context, dsn string, schema string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	if schema != "" {
		if _, err := pool.Exec(ctx, "SET search_path TO "+schema+", public"); err != nil {
			pool.Close()
			return nil, fmt.Errorf("set search_path: %w", err)
		}
	}

	return &DB{Pool: pool}, nil
}

// Close closes the database connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}
