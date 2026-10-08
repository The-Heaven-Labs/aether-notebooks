package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

type connectorHealthCall struct {
	orgID       string
	connectorID string
	ok          bool
	errMsg      string
}

type connectorHealthCapture struct {
	calls []connectorHealthCall
}

func (c *connectorHealthCapture) record(_ context.Context, orgID, connectorID string, ok bool, errMsg string) {
	c.calls = append(c.calls, connectorHealthCall{orgID: orgID, connectorID: connectorID, ok: ok, errMsg: errMsg})
}

func (c *connectorHealthCapture) last(t *testing.T) connectorHealthCall {
	t.Helper()
	require.NotEmpty(t, c.calls, "expected a recorded connector activity")
	return c.calls[len(c.calls)-1]
}

func TestExecuteSQLRecordsConnectorHealthOnSuccess(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createSQLTestPGConnector(t, db, orgID, userID)

	capture := &connectorHealthCapture{}
	ctx := &ToolContext{
		Context:                 context.Background(),
		UserID:                  userID,
		OrgID:                   orgID,
		OrgRole:                 "admin",
		DB:                      db.Pool,
		MasterKey:               masterKey,
		CheckPermissionFunc:     allowAllPermissions,
		RecordConnectorActivity: capture.record,
	}

	handler := makeExecuteSQLHandler(db.Pool)
	runExecuteSQL(t, handler, ctx, map[string]any{"connector_id": connID, "query": "SELECT 1"})

	last := capture.last(t)
	require.Equal(t, orgID, last.orgID)
	require.Equal(t, connID, last.connectorID)
	require.True(t, last.ok)
	require.Empty(t, last.errMsg)
}

func TestOpenAgentExecutorRecordsConnectorHealthOnConnectFailure(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())

	capture := &connectorHealthCapture{}
	pool := executor.NewConnPool(executor.PoolConfig{
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			return nil, errors.New("dial tcp 10.0.0.5:9000: connect: connection refused")
		},
	})
	ctx := &ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "admin",
		DB:        db.Pool,
		MasterKey: masterKey,
		ResolveTarget: func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return &executor.ExecutionTarget{
				Endpoint: "warehouse.invalid:9000",
				CHUser:   "aether_test_wh_u_fail",
				Config: models.ConnectorConfig{
					Host: "warehouse.invalid", Port: 9000, User: "aether_test_wh_u_fail",
					Password: "derived", Database: "analytics",
				},
			}, nil
		},
		ConnPool:                pool,
		CheckPermissionFunc:     allowAllPermissions,
		RecordConnectorActivity: capture.record,
	}

	handler := makeExecuteSQLHandler(db.Pool)
	raw, err := json.Marshal(map[string]any{"connector_id": connID, "query": "SELECT 1"})
	require.NoError(t, err)
	_, err = handler(raw, ctx)
	require.ErrorContains(t, err, "connect to warehouse")

	last := capture.last(t)
	require.Equal(t, orgID, last.orgID)
	require.Equal(t, connID, last.connectorID)
	require.False(t, last.ok)
	require.Contains(t, last.errMsg, "connection refused")
}

func TestRunCellRecordsConnectorHealthOnSuccess(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createSQLTestPGConnector(t, db, orgID, userID)
	nbID := createTestNotebook(t, db, orgID, userID)

	cellID := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, "limit", created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', $3, 'SELECT 1 AS x', 0, 1000, NOW(), NOW())
	`, cellID, nbID, connID)
	require.NoError(t, err)

	capture := &connectorHealthCapture{}
	ctx := &ToolContext{
		Context:                 context.Background(),
		UserID:                  userID,
		OrgID:                   orgID,
		OrgRole:                 "admin",
		NotebookID:              nbID,
		DB:                      db.Pool,
		MasterKey:               masterKey,
		CheckPermissionFunc:     allowAllPermissions,
		RecordConnectorActivity: capture.record,
	}

	reg := NewToolRegistry()
	RegisterNotebookTools(reg, db.Pool)
	runCellDef, ok := reg.Get("run_cell")
	require.True(t, ok)

	args, _ := json.Marshal(map[string]any{"cell_id": cellID})
	_, err = runCellDef.Handler(args, ctx)
	require.NoError(t, err)

	last := capture.last(t)
	require.Equal(t, orgID, last.orgID)
	require.Equal(t, connID, last.connectorID)
	require.True(t, last.ok)
	require.Empty(t, last.errMsg)
}
