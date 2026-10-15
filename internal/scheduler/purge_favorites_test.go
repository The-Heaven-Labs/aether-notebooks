package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
)

// seedFavorite inserts one favorite row for the seeded purge-test user.
func seedFavorite(t *testing.T, db *database.DB, f purgeTestOrg, resourceType, resourceID string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO favorites (user_id, org_id, resource_type, resource_id)
		VALUES ($1, $2, $3, $4)`, f.userID, f.orgID, resourceType, resourceID)
	require.NoError(t, err)
}

// countFavoritesFor counts favorite rows for one resource (across users).
func countFavoritesFor(t *testing.T, db *database.DB, resourceType, resourceID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM favorites WHERE resource_type = $1 AND resource_id = $2::uuid`,
		resourceType, resourceID).Scan(&n))
	return n
}

// TestPurgeTrashRemovesFavoritesOfPurgedResources pins the per-type cleanup:
// favorites for past-retention resources go with the purge, while within-
// retention and live resources keep theirs (soft delete keeps them by design).
func TestPurgeTrashRemovesFavoritesOfPurgedResources(t *testing.T) {
	db := setupPurgeTestDB(t)
	ctx := context.Background()
	f := seedPurgeTestOrg(t, db)

	pastRetention := time.Now().Add(-8 * 24 * time.Hour)
	withinRetention := time.Now().Add(-24 * time.Hour)

	purgedNB, _ := seedPurgeNotebook(t, db, f, &pastRetention)
	trashedNB, _ := seedPurgeNotebook(t, db, f, &withinRetention)
	liveNB, _ := seedPurgeNotebook(t, db, f, nil)

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
		{"notebook", purgedNB}, {"notebook", trashedNB}, {"notebook", liveNB},
		{"connector", purgedConnector}, {"connector", liveConnector},
		{"dashboard", purgedDashboard}, {"dashboard", liveDashboard},
		{"folder", purgedFolder}, {"folder", liveFolder},
	} {
		seedFavorite(t, db, f, r.resourceType, r.id)
	}

	New(db, nil).purgeTrash(ctx)

	for _, r := range []struct {
		resourceType string
		id           string
		want         int
	}{
		{"notebook", purgedNB, 0}, {"notebook", trashedNB, 1}, {"notebook", liveNB, 1},
		{"connector", purgedConnector, 0}, {"connector", liveConnector, 1},
		{"dashboard", purgedDashboard, 0}, {"dashboard", liveDashboard, 1},
		{"folder", purgedFolder, 0}, {"folder", liveFolder, 1},
	} {
		require.Equal(t, r.want, countFavoritesFor(t, db, r.resourceType, r.id),
			"favorites for %s %s", r.resourceType, r.id)
	}
}

// TestPurgeTrashRemovesFavoritesOfPurgedFolderSubtrees pins the recursive
// cleanup: purging a past-retention folder removes favorites for every folder
// the cascade deletes AND for the resources that lived inside the subtree.
// The resources themselves survive with folder_id NULL (ON DELETE SET NULL),
// but the folder that held them is gone, so their favorites are cleaned in
// the same purge.
func TestPurgeTrashRemovesFavoritesOfPurgedFolderSubtrees(t *testing.T) {
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
	seedNotebook := func(title string, folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO notebooks (id, org_id, folder_id, title, created_by)
			VALUES ($1, $2, $3, $4, $5)`, id, f.orgID, folderID, title, f.userID)
		require.NoError(t, err)
		return id
	}
	seedConnector := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO connectors (id, org_id, folder_id, name, type, config_encrypted)
			VALUES ($1, $2, $3, 'Subtree Conn', 'postgres', $4)`, id, f.orgID, folderID, []byte("{}"))
		require.NoError(t, err)
		return id
	}
	seedDashboard := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO dashboards (id, org_id, folder_id, title, created_by)
			VALUES ($1, $2, $3, 'Subtree Dashboard', $4)`, id, f.orgID, folderID, f.userID)
		require.NoError(t, err)
		return id
	}
	seedAgent := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO agents (id, org_id, folder_id, name, created_by)
			VALUES ($1, $2, $3, 'Subtree Agent', $4)`, id, f.orgID, folderID, f.userID)
		require.NoError(t, err)
		return id
	}
	seedModelConfig := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO model_configs (id, org_id, folder_id, name, provider, base_url, model, api_key_encrypted, created_by)
			VALUES ($1, $2, $3, 'Subtree Model', 'openai', 'https://example.com', 'gpt-4', $4, $5)`,
			id, f.orgID, folderID, []byte("enc"), f.userID)
		require.NoError(t, err)
		return id
	}
	seedSkill := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO skills (id, org_id, folder_id, name, created_by)
			VALUES ($1, $2, $3, 'Subtree Skill', $4)`, id, f.orgID, folderID, f.userID)
		require.NoError(t, err)
		return id
	}
	seedMCPServer := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO mcp_servers (id, org_id, folder_id, name, type, command, created_by)
			VALUES ($1, $2, $3, 'Subtree MCP', 'stdio', 'echo', $4)`, id, f.orgID, folderID, f.userID)
		require.NoError(t, err)
		return id
	}
	seedTool := func(folderID *string) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO tools (id, org_id, folder_id, name, type, created_by)
			VALUES ($1, $2, $3, 'Subtree Tool', 'webhook', $4)`, id, f.orgID, folderID, f.userID)
		require.NoError(t, err)
		return id
	}

	parentID := seedFolder("Purge Parent", nil, &pastRetention)
	childID := seedFolder("Live Child", &parentID, nil)
	grandchildID := seedFolder("Live Grandchild", &childID, nil)
	unrelatedID := seedFolder("Live Unrelated", nil, nil)

	// Resources inside the subtree across every folder-aware type, plus
	// controls outside it.
	insideNotebook := seedNotebook("Inside Notebook", &childID)
	insideConnector := seedConnector(&childID)
	insideDashboard := seedDashboard(&grandchildID)
	insideAgent := seedAgent(&parentID)
	insideModelConfig := seedModelConfig(&childID)
	insideSkill := seedSkill(&grandchildID)
	insideMCPServer := seedMCPServer(&childID)
	insideTool := seedTool(&grandchildID)
	outsideNotebook := seedNotebook("Outside Notebook", &unrelatedID)

	for _, r := range []struct{ resourceType, id string }{
		{"folder", parentID}, {"folder", childID}, {"folder", grandchildID},
		{"notebook", insideNotebook}, {"connector", insideConnector},
		{"dashboard", insideDashboard}, {"agent", insideAgent},
		{"model_config", insideModelConfig}, {"skill", insideSkill},
		{"mcp_server", insideMCPServer}, {"tool", insideTool},
		{"notebook", outsideNotebook}, {"folder", unrelatedID},
	} {
		seedFavorite(t, db, f, r.resourceType, r.id)
	}

	New(db, nil).purgeTrash(ctx)

	for _, r := range []struct {
		resourceType string
		id           string
		want         int
	}{
		{"folder", parentID, 0}, {"folder", childID, 0}, {"folder", grandchildID, 0},
		{"notebook", insideNotebook, 0}, {"connector", insideConnector, 0},
		{"dashboard", insideDashboard, 0}, {"agent", insideAgent, 0},
		{"model_config", insideModelConfig, 0}, {"skill", insideSkill, 0},
		{"mcp_server", insideMCPServer, 0}, {"tool", insideTool, 0},
		{"notebook", outsideNotebook, 1}, {"folder", unrelatedID, 1},
	} {
		require.Equal(t, r.want, countFavoritesFor(t, db, r.resourceType, r.id),
			"favorites for %s %s", r.resourceType, r.id)
	}

	// The subtree resources survive (folder_id falls back to NULL); only
	// their favorites were cleaned.
	var folderID *string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT folder_id FROM notebooks WHERE id = $1`, insideNotebook).Scan(&folderID))
	require.Nil(t, folderID, "subtree resources survive the folder purge with folder_id NULL")
	require.Equal(t, 1, countPurgeRows(t, db, `SELECT COUNT(*) FROM notebooks WHERE id = $1`, insideNotebook))
}
