package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

// identityProbeConn is a clickhouse.Conn whose Query always fails with a marker
// error. Combined with a pool whose Open captures the config, it proves which
// credential a pooled execution dialed with — without needing a real
// ClickHouse server for the managed-connector cases.
type identityProbeConn struct {
	clickhouse.Conn
}

func (c *identityProbeConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	return nil, errors.New("identity probe query")
}

// identityResultConn serves `rows` one-column rows so success paths (output
// persistence, audit, row limits) can be exercised on the pooled route.
type identityResultConn struct {
	clickhouse.Conn
	rows int
}

func (c *identityResultConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	return newIdentityProbeRows(c.rows), nil
}

// identityBlockingConn blocks Query until the execution context ends, so
// timeout enforcement can be exercised on the pooled route.
type identityBlockingConn struct {
	clickhouse.Conn
}

func (c *identityBlockingConn) Query(ctx context.Context, _ string, _ ...any) (driver.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type identityProbeRows struct {
	remaining int
	idx       int
}

func newIdentityProbeRows(n int) *identityProbeRows {
	return &identityProbeRows{remaining: n}
}

func (r *identityProbeRows) Next() bool {
	if r.remaining <= 0 {
		return false
	}
	r.remaining--
	r.idx++
	return true
}

func (r *identityProbeRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("identity probe rows: expected 1 destination, got %d", len(dest))
	}
	out, ok := dest[0].(*string)
	if !ok {
		return fmt.Errorf("identity probe rows: unexpected destination %T", dest[0])
	}
	*out = fmt.Sprintf("row_%d", r.idx)
	return nil
}

func (r *identityProbeRows) ScanStruct(any) error {
	return errors.New("identity probe rows: ScanStruct unsupported")
}

func (r *identityProbeRows) ColumnTypes() []driver.ColumnType {
	return []driver.ColumnType{identityProbeColumnType{}}
}

func (r *identityProbeRows) Totals(...any) error { return nil }

func (r *identityProbeRows) Columns() []string { return []string{"identity_probe"} }

func (r *identityProbeRows) Close() error { return nil }

func (r *identityProbeRows) Err() error { return nil }

func (r *identityProbeRows) HasData() bool { return true }

type identityProbeColumnType struct{}

func (identityProbeColumnType) Name() string             { return "identity_probe" }
func (identityProbeColumnType) Nullable() bool           { return false }
func (identityProbeColumnType) ScanType() reflect.Type   { return reflect.TypeOf("") }
func (identityProbeColumnType) DatabaseTypeName() string { return "String" }

// identityCapture records the connector config the pool opened with.
type identityCapture struct {
	cfg  models.ConnectorConfig
	used bool
	// result makes the pool hand out a connection that serves `rows` rows
	// instead of failing the query; block makes Query wait for cancellation.
	result bool
	rows   int
	block  bool
}

func newIdentityCapturePool(capture *identityCapture) *executor.ConnPool {
	return executor.NewConnPool(executor.PoolConfig{
		Open: func(cfg models.ConnectorConfig) (clickhouse.Conn, error) {
			capture.cfg = cfg
			capture.used = true
			if capture.block {
				return &identityBlockingConn{}, nil
			}
			if capture.result {
				return &identityResultConn{rows: capture.rows}, nil
			}
			return &identityProbeConn{}, nil
		},
	})
}

func allowAllPermissions(context.Context, string, string, string, string, string, string) (bool, error) {
	return true, nil
}

// createIdentityTestCHConnector inserts a ClickHouse connector with the given
// (encrypted) stored credential.
func createIdentityTestCHConnector(t *testing.T, db *database.DB, orgID, userID string, cfg models.ConnectorConfig) (string, []byte) {
	t.Helper()
	connID := uuid.New().String()
	cfgJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	masterKey := crypto.DeriveKey("test-master-key-for-tests-only!")
	encrypted, err := crypto.Encrypt(cfgJSON, masterKey)
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, 'clickhouse', $4, $5, NOW(), NOW())
	`, connID, orgID, "Identity Test CH", encrypted, userID)
	require.NoError(t, err)
	return connID, masterKey
}

func storedCredentialCfg() models.ConnectorConfig {
	return models.ConnectorConfig{
		Host: "stored-credential.invalid", Port: 9000, User: "stored_user", Password: "stored_secret", Database: "analytics",
	}
}

// requireIdentityClickHouse skips tests that need a real ClickHouse when the
// dev service is unreachable.
func requireIdentityClickHouse(t *testing.T) {
	t.Helper()
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{"localhost:9000"},
		Auth: clickhouse.Auth{Username: "dev", Password: "dev", Database: "analytics"},
	})
	if err != nil {
		t.Skipf("clickhouse unavailable at localhost:9000: %v", err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		t.Skipf("clickhouse unavailable at localhost:9000: %v", err)
	}
}

// A managed ClickHouse connector must execute as the acting user's warehouse
// identity on a pooled connection; the stored connector credential is never
// dialed.
func TestAgentExecuteSQLUsesUserIdentity(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())

	const chUser = "aether_test_wh_u_1234"
	var gotUser, gotConn uuid.UUID
	var gotPinned bool
	capture := &identityCapture{}
	tc := &ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "editor",
		DB:        db.Pool,
		MasterKey: masterKey,
		ResolveTarget: func(_ context.Context, u, c uuid.UUID, pinned bool) (*executor.ExecutionTarget, error) {
			gotUser, gotConn, gotPinned = u, c, pinned
			return &executor.ExecutionTarget{
				Endpoint: "warehouse.invalid:9000",
				CHUser:   chUser,
				Config: models.ConnectorConfig{
					Host: "warehouse.invalid", Port: 9000, User: chUser, Password: "derived_secret", Database: "analytics",
				},
			}, nil
		},
		ConnPool:            newIdentityCapturePool(capture),
		CheckPermissionFunc: allowAllPermissions,
	}

	handler := makeExecuteSQLHandler(db.Pool)
	args, err := json.Marshal(map[string]any{"connector_id": connID, "query": "SELECT 1"})
	require.NoError(t, err)
	_, err = handler(args, tc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "identity probe query", "the pooled per-user connection must be used")

	require.True(t, capture.used, "a managed connector must take the pooled path")
	require.Equal(t, chUser, capture.cfg.User)
	require.Equal(t, "derived_secret", capture.cfg.Password)
	require.NotEqual(t, "stored_user", capture.cfg.User, "the stored credential must never be dialed")
	require.Equal(t, userID, gotUser.String(), "resolution must use the acting user from the tool context")
	require.Equal(t, connID, gotConn.String())
	require.False(t, gotPinned, "agent execution routes by preference like HTTP, it does not pin")
}

// Resolver failures (and a missing resolver) must surface as tool errors and
// never silently fall back to the stored connector credential.
func TestAgentExecuteSQLFailsClosedWithoutManagedRoute(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())
	handler := makeExecuteSQLHandler(db.Pool)
	args, err := json.Marshal(map[string]any{"connector_id": connID, "query": "SELECT 1"})
	require.NoError(t, err)

	newCtx := func(capture *identityCapture) *ToolContext {
		return &ToolContext{
			Context: context.Background(), UserID: userID, OrgID: orgID, OrgRole: "editor",
			DB: db.Pool, MasterKey: masterKey,
			ConnPool:            newIdentityCapturePool(capture),
			CheckPermissionFunc: allowAllPermissions,
		}
	}

	t.Run("resolver error", func(t *testing.T) {
		capture := &identityCapture{}
		tc := newCtx(capture)
		tc.ResolveTarget = func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return nil, fmt.Errorf("warehouse wh-1: %w", executor.ErrProvisioningNotReady)
		}
		_, err := handler(args, tc)
		require.ErrorContains(t, err, "not ready")
		require.NotContains(t, err.Error(), "stored-credential.invalid", "must not dial the stored credential")
		require.False(t, capture.used, "a resolver failure must not open a connection")
	})

	t.Run("nil resolver", func(t *testing.T) {
		tc := newCtx(&identityCapture{})
		_, err := handler(args, tc)
		require.ErrorContains(t, err, "not configured")
	})

	t.Run("connector not found", func(t *testing.T) {
		tc := newCtx(&identityCapture{})
		tc.ResolveTarget = func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return nil, fmt.Errorf("connector: %w", executor.ErrConnectorNotFound)
		}
		_, err := handler(args, tc)
		require.ErrorContains(t, err, "not found")
	})

	t.Run("access denied", func(t *testing.T) {
		tc := newCtx(&identityCapture{})
		tc.ResolveTarget = func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return nil, fmt.Errorf("warehouse: %w", executor.ErrServiceAccessDenied)
		}
		_, err := handler(args, tc)
		require.ErrorContains(t, err, "do not have permission")
	})

	t.Run("choice required lists services", func(t *testing.T) {
		tc := newCtx(&identityCapture{})
		tc.ResolveTarget = func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return nil, &executor.ServiceChoiceError{
				WarehouseID: uuid.New(),
				Allowed:     []executor.ServiceChoice{{Name: "Service A"}, {Name: "Service B"}},
			}
		}
		_, err := handler(args, tc)
		require.ErrorContains(t, err, "Service A")
		require.ErrorContains(t, err, "Service B")
		require.ErrorContains(t, err, "service preference")
	})
}

// Unmanaged ClickHouse connectors keep the legacy stored-credential path.
func TestAgentExecuteSQLUnmanagedUsesStoredCredential(t *testing.T) {
	requireIdentityClickHouse(t)
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, models.ConnectorConfig{
		Host: "localhost", Port: 9000, User: "dev", Password: "dev", Database: "analytics",
	})

	capture := &identityCapture{}
	tc := &ToolContext{
		Context: context.Background(), UserID: userID, OrgID: orgID, OrgRole: "editor",
		DB: db.Pool, MasterKey: masterKey,
		ResolveTarget: func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return nil, fmt.Errorf("connector: %w", executor.ErrUnmanagedConnector)
		},
		ConnPool:            newIdentityCapturePool(capture),
		CheckPermissionFunc: allowAllPermissions,
	}

	handler := makeExecuteSQLHandler(db.Pool)
	rs := runExecuteSQL(t, handler, tc, map[string]any{
		"connector_id": connID,
		"query":        "SELECT currentUser() AS ch_user",
	})
	require.Len(t, rs.Rows, 1)
	require.Equal(t, "dev", rs.Rows[0][0], "unmanaged connectors keep the stored credential")
	require.False(t, capture.used, "unmanaged connectors must not use the warehouse pool")
}

// run_cell (executeCell) must also resolve the acting user's identity.
func TestAgentRunCellUsesUserIdentity(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())

	nbID := createTestNotebook(t, db, orgID, userID)
	cellID := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', $3, 'SELECT 1', 0, NOW(), NOW())
	`, cellID, nbID, connID)
	require.NoError(t, err)

	const chUser = "aether_test_wh_u_5678"
	warehouseID := uuid.New()
	connUUID := uuid.MustParse(connID)
	var gotUser uuid.UUID
	capture := &identityCapture{result: true, rows: 1}
	tc := &ToolContext{
		Context: context.Background(), UserID: userID, OrgID: orgID, OrgRole: "editor",
		DB: db.Pool, MasterKey: masterKey,
		ResolveTarget: func(_ context.Context, u, _ uuid.UUID, _ bool) (*executor.ExecutionTarget, error) {
			gotUser = u
			return &executor.ExecutionTarget{
				WarehouseID: warehouseID,
				ConnectorID: connUUID,
				Endpoint:    "warehouse.invalid:9000",
				CHUser:      chUser,
				Config: models.ConnectorConfig{
					Host: "warehouse.invalid", Port: 9000, User: chUser, Password: "derived_secret", Database: "analytics",
				},
			}, nil
		},
		ConnPool:            newIdentityCapturePool(capture),
		CheckPermissionFunc: allowAllPermissions,
	}

	handler := makeRunCellHandler(db.Pool)
	args, err := json.Marshal(map[string]any{"cell_id": cellID})
	require.NoError(t, err)
	result, err := handler(args, tc)
	require.NoError(t, err)
	m := result.(map[string]any)
	require.Equal(t, "completed", m["status"], "the pooled identity must serve the query")
	require.Equal(t, 1, m["rows"])
	require.Equal(t, userID, gotUser.String())
	require.True(t, capture.used)
	require.Equal(t, chUser, capture.cfg.User, "run_cell must dial the per-user identity")

	// The existing cell.run audit must record the routed warehouse identity.
	var metaJSON []byte
	require.NoError(t, db.Pool.QueryRow(context.Background(), `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'cell.run' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`, orgID, cellID).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, warehouseID.String(), meta["warehouse_id"])
	require.Equal(t, connID, meta["connector_id"])
	require.Equal(t, chUser, meta["ch_user"])
}

// A routed service carries its own MaxRows/TimeoutSeconds; agent execution
// must apply the routed values, mirroring HTTP's routed-limits coverage.
func TestAgentRoutedServiceLimitsApply(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)

	managedTarget := func(maxRows, timeoutSeconds int) *executor.ExecutionTarget {
		return &executor.ExecutionTarget{
			Endpoint: "warehouse.invalid:9000",
			CHUser:   "aether_test_wh_u_limits",
			Config: models.ConnectorConfig{
				Host: "warehouse.invalid", Port: 9000, User: "aether_test_wh_u_limits", Password: "derived_secret", Database: "analytics",
			},
			MaxRows:        maxRows,
			TimeoutSeconds: timeoutSeconds,
		}
	}

	t.Run("execute_sql caps rows at the routed max_rows", func(t *testing.T) {
		connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())
		capture := &identityCapture{result: true, rows: 5}
		tc := &ToolContext{
			Context: context.Background(), UserID: userID, OrgID: orgID, OrgRole: "editor",
			DB: db.Pool, MasterKey: masterKey,
			ResolveTarget: func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
				return managedTarget(2, 0), nil
			},
			ConnPool:            newIdentityCapturePool(capture),
			CheckPermissionFunc: allowAllPermissions,
		}
		handler := makeExecuteSQLHandler(db.Pool)
		args, err := json.Marshal(map[string]any{"connector_id": connID, "query": "SELECT number FROM numbers(5)"})
		require.NoError(t, err)
		result, err := handler(args, tc)
		require.NoError(t, err)
		rs, ok := result.(*executor.ResultSet)
		require.True(t, ok, "expected *executor.ResultSet, got %T", result)
		require.Len(t, rs.Rows, 2, "the routed service's max_rows must cap the tool result")
	})

	t.Run("run_cell honors the routed timeout_seconds", func(t *testing.T) {
		connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())
		nbID := createTestNotebook(t, db, orgID, userID)
		cellID := uuid.New().String()
		_, err := db.Pool.Exec(context.Background(), `
			INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, created_at, updated_at)
			VALUES ($1, $2, 'code', 'sql', $3, 'SELECT sleep(3)', 0, NOW(), NOW())
		`, cellID, nbID, connID)
		require.NoError(t, err)

		capture := &identityCapture{block: true}
		tc := &ToolContext{
			Context: context.Background(), UserID: userID, OrgID: orgID, OrgRole: "editor",
			DB: db.Pool, MasterKey: masterKey,
			ResolveTarget: func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
				return managedTarget(0, 1), nil
			},
			ConnPool:            newIdentityCapturePool(capture),
			CheckPermissionFunc: allowAllPermissions,
		}
		handler := makeRunCellHandler(db.Pool)
		args, err := json.Marshal(map[string]any{"cell_id": cellID})
		require.NoError(t, err)
		result, err := handler(args, tc)
		require.NoError(t, err)
		m := result.(map[string]any)
		require.Equal(t, "error", m["status"])
		require.Equal(t, true, m["timed_out"])
		require.Contains(t, m["error"], "execution timed out after 1000ms")
	})
}

// explore_schema must open its connection through the pooled per-user identity
// for managed ClickHouse connectors.
func TestAgentExploreSchemaUsesUserIdentity(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())

	const chUser = "aether_test_wh_u_9012"
	var gotUser uuid.UUID
	capture := &identityCapture{}
	tc := &ToolContext{
		Context: context.Background(), UserID: userID, OrgID: orgID, OrgRole: "editor",
		DB: db.Pool, MasterKey: masterKey,
		ResolveTarget: func(_ context.Context, u, _ uuid.UUID, _ bool) (*executor.ExecutionTarget, error) {
			gotUser = u
			return &executor.ExecutionTarget{
				Endpoint: "warehouse.invalid:9000",
				CHUser:   chUser,
				Config: models.ConnectorConfig{
					Host: "warehouse.invalid", Port: 9000, User: chUser, Password: "derived_secret", Database: "analytics",
				},
			}, nil
		},
		ConnPool:            newIdentityCapturePool(capture),
		CheckPermissionFunc: allowAllPermissions,
	}

	handler := makeExploreSchemaHandler(db.Pool)
	args, err := json.Marshal(map[string]any{"connector_id": connID})
	require.NoError(t, err)
	_, err = handler(args, tc)
	require.ErrorContains(t, err, "identity probe query")

	require.Equal(t, userID, gotUser.String())
	require.True(t, capture.used)
	require.Equal(t, chUser, capture.cfg.User, "explore_schema must dial the per-user identity")
	require.NotEqual(t, "stored_user", capture.cfg.User)
}

// CheckPermission must delegate to the shared resolver whenever one is wired;
// in particular an admin org role must not bypass ACLs on its own.
func TestToolContextCheckPermissionDelegatesToSharedResolver(t *testing.T) {
	var got [6]string
	called := false
	tc := &ToolContext{
		Context: context.Background(),
		UserID:  "user-1", OrgID: "org-1", OrgRole: "admin",
		CheckPermissionFunc: func(_ context.Context, userID, orgID, orgRole, resourceType, resourceID, action string) (bool, error) {
			called = true
			got = [6]string{userID, orgID, orgRole, resourceType, resourceID, action}
			return false, nil
		},
	}
	err := tc.CheckPermission("connector", "conn-1", "use")
	require.ErrorContains(t, err, "permission denied")
	require.True(t, called, "the shared resolver must be consulted")
	require.Equal(t, [6]string{"user-1", "org-1", "admin", "connector", "conn-1", "use"}, got)

	// Without a resolver, even an org admin must be denied.
	bare := &ToolContext{Context: context.Background(), UserID: "user-1", OrgID: "org-1", OrgRole: "admin"}
	err = bare.CheckPermission("connector", "conn-1", "use")
	require.ErrorContains(t, err, "permission resolver not configured")
}

// The engine must propagate the warehouse execution hooks and the session's
// admin mode into every ToolContext.
func TestEnginePropagatesWarehouseExecutionHooks(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)

	engine.ResolveTarget = func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
		return nil, executor.ErrUnmanagedConnector
	}
	engine.ConnPool = executor.NewConnPool(executor.PoolConfig{})
	engine.CheckPermissionFunc = allowAllPermissions

	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	engine.session.SetAdminMode(sid, true)

	var sawResolver, sawPool, sawCheck, sawAdmin bool
	probe := &ToolDef{Type: "function"}
	probe.Function.Name = "probe_warehouse_hooks"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(args json.RawMessage, ctx *ToolContext) (any, error) {
		sawResolver = ctx.ResolveTarget != nil
		sawPool = ctx.ConnPool != nil
		sawCheck = ctx.CheckPermissionFunc != nil
		sawAdmin = executor.AdminModeFromContext(ctx.Context)
		return map[string]any{"ok": true}, nil
	}

	masterKey := make([]byte, 32)
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{
						ID:   "call-warehouse-hooks-1",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "probe_warehouse_hooks", Arguments: `{}`},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, err := crypto.Encrypt([]byte("sk-test"), masterKey)
	require.NoError(t, err)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))

	if _, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "probe", nil, []*ToolDef{probe}, masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	require.True(t, sawResolver, "ResolveTarget must propagate into tool contexts")
	require.True(t, sawPool, "ConnPool must propagate into tool contexts")
	require.True(t, sawCheck, "CheckPermissionFunc must propagate into tool contexts")
	require.True(t, sawAdmin, "the session's admin mode must ride the tool context")
}
