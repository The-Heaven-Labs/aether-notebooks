package api_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPendingGrantTablesExist pins the V127 schema: both staging tables exist
// so materialization and the handlers can rely on them.
func TestPendingGrantTablesExist(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"pending_acl_entries", "pending_warehouse_table_grants"} {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
			table).Scan(&exists))
		require.True(t, exists, "%s must exist", table)
	}
}
