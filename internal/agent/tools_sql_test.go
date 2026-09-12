package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

func createSQLTestPGConnector(t *testing.T, db *database.DB, orgID, userID string) (string, []byte) {
	t.Helper()
	connID := uuid.New().String()
	cfg := models.ConnectorConfig{Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether"}
	cfgJSON, _ := json.Marshal(cfg)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	configEncrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, 'Test SQL PG', 'postgres', $3, $4, $5, $5)
	`, connID, orgID, configEncrypted, userID, time.Now())
	require.NoError(t, err)
	return connID, masterKey
}

func runExecuteSQL(t *testing.T, handler ToolHandler, ctx *ToolContext, args map[string]any) *executor.ResultSet {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	result, err := handler(raw, ctx)
	require.NoError(t, err)
	rs, ok := result.(*executor.ResultSet)
	require.True(t, ok, "expected *executor.ResultSet, got %T", result)
	return rs
}

func TestClampSQLRowLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults", 0, defaultSQLRowLimit},
		{"negative defaults", -7, defaultSQLRowLimit},
		{"positive kept", 7, 7},
		{"default kept", defaultSQLRowLimit, defaultSQLRowLimit},
		{"max kept", maxSQLRowLimit, maxSQLRowLimit},
		{"above max clamps", 1_000_000, maxSQLRowLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clampSQLRowLimit(tc.in))
		})
	}
}

func TestExecuteSQLHandlerThreadsLimit(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createSQLTestPGConnector(t, db, orgID, userID)

	handler := makeExecuteSQLHandler(db.Pool)
	ctx := &ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "admin",
		DB:        db.Pool,
		MasterKey: masterKey,
	}

	t.Run("explicit limit", func(t *testing.T) {
		rs := runExecuteSQL(t, handler, ctx, map[string]any{
			"connector_id": connID,
			"query":        "SELECT generate_series(1, 10) AS n",
			"limit":        3,
		})
		require.Len(t, rs.Rows, 3)
	})

	t.Run("absent limit defaults", func(t *testing.T) {
		rs := runExecuteSQL(t, handler, ctx, map[string]any{
			"connector_id": connID,
			"query":        "SELECT generate_series(1, 1500) AS n",
		})
		require.Len(t, rs.Rows, 1000)
	})

	t.Run("oversized limit clamps", func(t *testing.T) {
		rs := runExecuteSQL(t, handler, ctx, map[string]any{
			"connector_id": connID,
			"query":        "SELECT generate_series(1, 10001) AS n",
			"limit":        1000000,
		})
		require.Len(t, rs.Rows, 10000)
	})
}
