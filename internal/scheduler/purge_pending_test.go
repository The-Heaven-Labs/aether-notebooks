package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
)

// seedPendingACLForResource inserts one staged row; the test only checks that
// purge deletes the right rows.
func seedPendingACLForResource(t *testing.T, db *database.DB, orgID, resourceType, resourceID string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions)
		VALUES ($1, $2, $3, 'future@example.com', ARRAY['view'])`, orgID, resourceType, resourceID)
	require.NoError(t, err)
}

func countPendingFor(t *testing.T, db *database.DB, resourceType, resourceID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pending_acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid`,
		resourceType, resourceID).Scan(&n))
	return n
}

func TestPurgeTrashRemovesPendingACLsOfPurgedResources(t *testing.T) {
	db := setupPurgeTestDB(t)
	ctx := context.Background()
	f := seedPurgeTestOrg(t, db)

	pastRetention := time.Now().Add(-8 * 24 * time.Hour)
	withinRetention := time.Now().Add(-24 * time.Hour)

	purgedNB, purgedSession := seedPurgeNotebook(t, db, f, &pastRetention)
	trashedNB, trashedSession := seedPurgeNotebook(t, db, f, &withinRetention)
	liveNB, liveSession := seedPurgeNotebook(t, db, f, nil)

	for _, r := range []struct{ resourceType, id string }{
		{"notebook", purgedNB}, {"notebook", trashedNB}, {"notebook", liveNB},
		{"agent_session", purgedSession}, {"agent_session", trashedSession}, {"agent_session", liveSession},
	} {
		seedPendingACLForResource(t, db, f.orgID, r.resourceType, r.id)
	}

	seedConnector := func(deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO connectors (id, org_id, name, type, config_encrypted, deleted_at)
			VALUES ($1, $2, 'Purge Conn', 'postgres', $3, $4)`, id, f.orgID, []byte("{}"), deletedAt)
		require.NoError(t, err)
		return id
	}
	seedDashboard := func(deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO dashboards (id, org_id, title, created_by, deleted_at)
			VALUES ($1, $2, 'Purge Dashboard', $3, $4)`, id, f.orgID, f.userID, deletedAt)
		require.NoError(t, err)
		return id
	}
	seedFolder := func(deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO folders (id, org_id, name, created_by, deleted_at)
			VALUES ($1, $2, 'Purge Folder', $3, $4)`, id, f.orgID, f.userID, deletedAt)
		require.NoError(t, err)
		return id
	}

	purgedConnector, liveConnector := seedConnector(&pastRetention), seedConnector(nil)
	purgedDashboard, liveDashboard := seedDashboard(&pastRetention), seedDashboard(nil)
	purgedFolder, liveFolder := seedFolder(&pastRetention), seedFolder(nil)
	for _, r := range []struct{ resourceType, id string }{
		{"connector", purgedConnector}, {"connector", liveConnector},
		{"dashboard", purgedDashboard}, {"dashboard", liveDashboard},
		{"folder", purgedFolder}, {"folder", liveFolder},
	} {
		seedPendingACLForResource(t, db, f.orgID, r.resourceType, r.id)
	}

	New(db, nil).purgeTrash(ctx)

	for _, r := range []struct {
		resourceType string
		id           string
		want         int
	}{
		{"notebook", purgedNB, 0}, {"notebook", trashedNB, 1}, {"notebook", liveNB, 1},
		{"agent_session", purgedSession, 0}, {"agent_session", trashedSession, 1}, {"agent_session", liveSession, 1},
		{"connector", purgedConnector, 0}, {"connector", liveConnector, 1},
		{"dashboard", purgedDashboard, 0}, {"dashboard", liveDashboard, 1},
		{"folder", purgedFolder, 0}, {"folder", liveFolder, 1},
	} {
		require.Equal(t, r.want, countPendingFor(t, db, r.resourceType, r.id),
			"pending rows for %s %s", r.resourceType, r.id)
	}
}

// TestPurgeTrashRemovesPendingACLsOfCascadePurgedChildFolders covers the case
// where a past-retention folder is purged while its subtree is still live:
// the parent_id ON DELETE CASCADE removes the children, so their pending rows
// must be consumed too (a plain RETURNING id from the purge predicate would
// miss them).
func TestPurgeTrashRemovesPendingACLsOfCascadePurgedChildFolders(t *testing.T) {
	db := setupPurgeTestDB(t)
	ctx := context.Background()
	f := seedPurgeTestOrg(t, db)

	pastRetention := time.Now().Add(-8 * 24 * time.Hour)

	seedFolder := func(name string, parentID *string, deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO folders (id, org_id, parent_id, name, created_by, deleted_at)
			VALUES ($1, $2, $3, $4, $5, $6)`, id, f.orgID, parentID, name, f.userID, deletedAt)
		require.NoError(t, err)
		return id
	}

	parentID := seedFolder("Purge Parent", nil, &pastRetention)
	childID := seedFolder("Live Child", &parentID, nil)
	grandchildID := seedFolder("Live Grandchild", &childID, nil)
	liveID := seedFolder("Live Unrelated", nil, nil)

	for _, id := range []string{parentID, childID, grandchildID, liveID} {
		seedPendingACLForResource(t, db, f.orgID, "folder", id)
	}

	New(db, nil).purgeTrash(ctx)

	for _, id := range []string{parentID, childID, grandchildID} {
		require.Zero(t, countPurgeRows(t, db, `SELECT COUNT(*) FROM folders WHERE id = $1`, id),
			"folder %s is removed by the parent purge cascade", id)
		require.Zero(t, countPendingFor(t, db, "folder", id),
			"pending rows for cascade-purged folder %s must be consumed", id)
	}

	require.Equal(t, 1, countPurgeRows(t, db, `SELECT COUNT(*) FROM folders WHERE id = $1`, liveID),
		"unrelated live folder must survive the purge")
	require.Equal(t, 1, countPendingFor(t, db, "folder", liveID),
		"unrelated live folder must keep its pending rows")
}
