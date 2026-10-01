package scheduler

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
)

func setupPurgeTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background()))
	t.Cleanup(func() { db.Close() })
	return db
}

type purgeTestOrg struct {
	orgID   string
	userID  string
	agentID string
}

func seedPurgeTestOrg(t *testing.T, db *database.DB) purgeTestOrg {
	t.Helper()
	ctx := context.Background()
	f := purgeTestOrg{orgID: uuid.NewString(), userID: uuid.NewString(), agentID: uuid.NewString()}
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, 'Purge Test Org', $2)`,
		f.orgID, "purge-"+f.orgID[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, f.orgID)
	})
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, 'Purge User', 'hash')`,
		f.userID, "purge-"+f.userID[:8]+"@example.com")
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO agents (id, org_id, name, created_by) VALUES ($1, $2, 'Purge Agent', $3)`,
		f.agentID, f.orgID, f.userID)
	require.NoError(t, err)
	return f
}

// seedPurgeNotebook creates a notebook, an agent session on it, and the
// session's owner ACL row. deletedAt is the notebook's soft-delete timestamp
// (nil keeps the notebook live).
func seedPurgeNotebook(t *testing.T, db *database.DB, f purgeTestOrg, deletedAt *time.Time) (notebookID, sessionID string) {
	t.Helper()
	ctx := context.Background()
	notebookID = uuid.NewString()
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, title, created_by, deleted_at) VALUES ($1, $2, 'Purge NB', $3, $4)`,
		notebookID, f.orgID, f.userID, deletedAt)
	require.NoError(t, err)

	sessionID = uuid.NewString()
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id) VALUES ($1, $2, $3, $4)`,
		sessionID, f.agentID, notebookID, f.userID)
	require.NoError(t, err)

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2, 'user', $3, ARRAY['view','edit','share','delete','admin'])`,
		f.orgID, sessionID, f.userID)
	require.NoError(t, err)
	return notebookID, sessionID
}

func countPurgeRows(t *testing.T, db *database.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.Pool.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

func TestPurgeTrashRemovesSessionACLsOfPurgedNotebooks(t *testing.T) {
	db := setupPurgeTestDB(t)
	ctx := context.Background()
	f := seedPurgeTestOrg(t, db)

	pastRetention := time.Now().Add(-8 * 24 * time.Hour)
	withinRetention := time.Now().Add(-24 * time.Hour)

	purgedNB, purgedSession := seedPurgeNotebook(t, db, f, &pastRetention)
	trashedNB, trashedSession := seedPurgeNotebook(t, db, f, &withinRetention)
	liveNB, liveSession := seedPurgeNotebook(t, db, f, nil)

	New(db, nil).purgeTrash(ctx)

	require.Zero(t, countPurgeRows(t, db, `SELECT COUNT(*) FROM notebooks WHERE id = $1`, purgedNB), "past-retention notebook must be purged")
	require.Zero(t, countPurgeRows(t, db, `SELECT COUNT(*) FROM agent_sessions WHERE id = $1`, purgedSession), "purged notebook's sessions cascade away")
	require.Zero(t, countPurgeRows(t, db,
		`SELECT COUNT(*) FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1`, purgedSession),
		"purged notebook's session ACL rows must not leak")

	require.Equal(t, 1, countPurgeRows(t, db, `SELECT COUNT(*) FROM notebooks WHERE id = $1`, trashedNB))
	require.Equal(t, 1, countPurgeRows(t, db, `SELECT COUNT(*) FROM agent_sessions WHERE id = $1`, trashedSession))
	require.Equal(t, 1, countPurgeRows(t, db,
		`SELECT COUNT(*) FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1`, trashedSession),
		"within-retention trash must keep its session ACL rows")

	require.Equal(t, 1, countPurgeRows(t, db, `SELECT COUNT(*) FROM notebooks WHERE id = $1`, liveNB))
	require.Equal(t, 1, countPurgeRows(t, db, `SELECT COUNT(*) FROM agent_sessions WHERE id = $1`, liveSession))
	require.Equal(t, 1, countPurgeRows(t, db,
		`SELECT COUNT(*) FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1`, liveSession),
		"live notebooks must keep their session ACL rows")
}
