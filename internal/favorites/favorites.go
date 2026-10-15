// Package favorites holds the shared cleanup helper for the favorites table.
//
// favorites.resource_id is polymorphic and deliberately carries no foreign key
// (the same convention as acl_entries), so rows are removed explicitly by
// every hard-delete path that deletes the resource. Soft-delete (trash) paths
// leave them in place by design: the favorite stays until the resource is
// purged, and the API's visibility filtering hides it while trashed.
package favorites

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// deleteChunkSize bounds one DELETE statement's id array. Purging a large
// folder subtree can collect thousands of ids; chunking keeps every statement
// small and predictable.
const deleteChunkSize = 1000

// ExecQuerier is the write surface shared by *pgxpool.Pool and pgx.Tx.
type ExecQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// DeleteFor removes favorite rows for hard-deleted resources. resource_id
// carries no FK, so every hard-delete path that deletes the resource's ACL
// rows must call this (favorites are user data about resources that no longer
// exist). Idempotent; chunks large id sets (≤deleteChunkSize per statement).
func DeleteFor(ctx context.Context, q ExecQuerier, resourceType string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	for start := 0; start < len(ids); start += deleteChunkSize {
		end := min(start+deleteChunkSize, len(ids))
		if _, err := q.Exec(ctx,
			`DELETE FROM favorites WHERE resource_type = $1 AND resource_id = ANY($2::uuid[])`,
			resourceType, ids[start:end]); err != nil {
			return fmt.Errorf("delete favorites for %s: %w", resourceType, err)
		}
	}
	return nil
}
