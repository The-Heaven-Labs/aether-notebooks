package favorites_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/favorites"
)

func setupFavoritesTestDB(t *testing.T) *database.DB {
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

func insertFavorite(t *testing.T, db *database.DB, orgID, userID, resourceType, resourceID string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO favorites (user_id, org_id, resource_type, resource_id)
		VALUES ($1, $2, $3, $4)`, userID, orgID, resourceType, resourceID)
	require.NoError(t, err)
}

func countFavorites(t *testing.T, db *database.DB, resourceType, resourceID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM favorites WHERE resource_type = $1 AND resource_id = $2::uuid`,
		resourceType, resourceID).Scan(&n))
	return n
}

// TestDeleteFor pins the helper contract: empty id sets are a no-op, only the
// named rows for the named type are removed, id sets over the chunk boundary
// execute across multiple statements, calls are idempotent, and invalid ids
// wrap an error instead of panicking.
func TestDeleteFor(t *testing.T) {
	db := setupFavoritesTestDB(t)
	ctx := context.Background()

	orgID, userID := uuid.NewString(), uuid.NewString()
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, 'Favorites Test Org', $2)`,
		orgID, "fav-"+orgID[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, orgID)
	})
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, 'Favorites User', 'hash')`,
		userID, "fav-"+userID[:8]+"@example.com")
	require.NoError(t, err)

	t.Run("empty id set is a no-op", func(t *testing.T) {
		require.NoError(t, favorites.DeleteFor(ctx, db.Pool, "notebook", nil))
		require.NoError(t, favorites.DeleteFor(ctx, db.Pool, "notebook", []string{}))
	})

	notebookA, notebookB, connectorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	insertFavorite(t, db, orgID, userID, "notebook", notebookA)
	insertFavorite(t, db, orgID, userID, "notebook", notebookB)
	insertFavorite(t, db, orgID, userID, "connector", connectorID)

	t.Run("removes only the named type and ids", func(t *testing.T) {
		require.NoError(t, favorites.DeleteFor(ctx, db.Pool, "notebook", []string{notebookA}))
		require.Zero(t, countFavorites(t, db, "notebook", notebookA))
		require.Equal(t, 1, countFavorites(t, db, "notebook", notebookB))
		require.Equal(t, 1, countFavorites(t, db, "connector", connectorID))

		// Idempotent: the row is already gone.
		require.NoError(t, favorites.DeleteFor(ctx, db.Pool, "notebook", []string{notebookA}))
	})

	t.Run("id sets over the chunk boundary execute", func(t *testing.T) {
		// 1001 ids crosses deleteChunkSize, so the helper issues two
		// statements; only the last id names a row.
		ids := make([]string, 0, 1001)
		for i := 0; i < 1000; i++ {
			ids = append(ids, uuid.NewString())
		}
		ids = append(ids, notebookB)
		require.NoError(t, favorites.DeleteFor(ctx, db.Pool, "notebook", ids))
		require.Zero(t, countFavorites(t, db, "notebook", notebookB))
		require.Equal(t, 1, countFavorites(t, db, "connector", connectorID))
	})

	t.Run("invalid ids wrap an error", func(t *testing.T) {
		err := favorites.DeleteFor(ctx, db.Pool, "notebook", []string{"not-a-uuid"})
		require.ErrorContains(t, err, "delete favorites for notebook")
	})
}
