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
