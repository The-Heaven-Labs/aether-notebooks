package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

// sharedWarehouseServer is the process-wide test Server reused by warehouse
// resolution and execution tests. Building it once avoids paying database
// connect + migration startup for every case; each test still seeds uniquely
// identified rows, so cases stay independent.
type sharedWarehouseServer struct {
	server *Server
	db     *database.DB
	key    []byte
	err    error
}

var (
	sharedWarehouseOnce sync.Once
	sharedWarehouse     sharedWarehouseServer
)

// sharedWarehouseTestServer returns the shared Server, building it on first
// use. A build failure is fatal: unlike a missing dev ClickHouse (which
// skips), a missing Postgres means the test environment is broken.
func sharedWarehouseTestServer(t *testing.T) (*Server, []byte) {
	t.Helper()
	sharedWarehouseOnce.Do(func() {
		sharedWarehouse = buildSharedWarehouseTestServer()
	})
	if sharedWarehouse.err != nil {
		t.Fatalf("shared warehouse test server: %v", sharedWarehouse.err)
	}
	return sharedWarehouse.server, sharedWarehouse.key
}

func buildSharedWarehouseTestServer() sharedWarehouseServer {
	ctx := context.Background()
	db, err := database.Connect(ctx, warehouseSyncTestDSN(), "")
	if err != nil {
		return sharedWarehouseServer{err: fmt.Errorf("connect: %w", err)}
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return sharedWarehouseServer{err: fmt.Errorf("migrate: %w", err)}
	}
	key := crypto.DeriveKey(warehouseSyncTestMasterKey)
	s := NewServer(db, auth.NewJWTIssuer("test-secret", 15*time.Minute), audit.NewLogger(db), key, nil)
	return sharedWarehouseServer{server: s, db: db, key: key}
}

// TestMain closes the shared server and its database after all package tests.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedWarehouse.server != nil {
		sharedWarehouse.server.Close()
	}
	if sharedWarehouse.db != nil {
		sharedWarehouse.db.Close()
	}
	os.Exit(code)
}

// requireClickHouseReachable skips the test when the dev ClickHouse service is
// unreachable. Probing before any server construction keeps skipped tests from
// paying startup costs.
func requireClickHouseReachable(t *testing.T) {
	t.Helper()
	cfg := warehouseSyncTestClickHouseConfig()
	conn, err := openWarehouseProvisionerConn(context.Background(), cfg)
	if err != nil {
		t.Skipf("clickhouse unavailable at %s:%d: %v", cfg.Host, cfg.Port, err)
	}
	conn.Close()
}

// insertClickHouseService inserts a ClickHouse connector that copies the
// encrypted config of sourceConnectorID, optionally linked to a warehouse.
func insertClickHouseService(t *testing.T, s *Server, orgID, sourceConnectorID uuid.UUID, name string, warehouseID *uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var encrypted []byte
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT config_encrypted FROM connectors WHERE id = $1`,
		sourceConnectorID.String()).Scan(&encrypted))

	id := uuid.New()
	var wh any
	if warehouseID != nil {
		wh = warehouseID.String()
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
		VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
		id.String(), orgID.String(), name, encrypted, wh)
	require.NoError(t, err)
	return id
}

// insertPostgresConnector inserts a legacy Postgres connector pointing at the
// test database, optionally linked to a warehouse.
func insertPostgresConnector(t *testing.T, s *Server, key []byte, orgID uuid.UUID, name string, warehouseID *uuid.UUID) uuid.UUID {
	t.Helper()
	cfg := models.ConnectorConfig{
		Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether",
	}
	plain, err := json.Marshal(cfg)
	require.NoError(t, err)
	encrypted, err := crypto.Encrypt(plain, key)
	require.NoError(t, err)

	id := uuid.New()
	var wh any
	if warehouseID != nil {
		wh = warehouseID.String()
	}
	_, err = s.db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
		VALUES ($1, $2, $3, 'postgres', $4, $5)`,
		id.String(), orgID.String(), name, encrypted, wh)
	require.NoError(t, err)
	return id
}

// grantConnectorUse grants a user view+use on a connector.
func grantConnectorUse(t *testing.T, s *Server, orgID, userID, connectorID uuid.UUID) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'user', $3, ARRAY['view','use'])`,
		orgID.String(), connectorID.String(), userID.String())
	require.NoError(t, err)
}

// grantGroupConnectorUse grants a group use on a connector.
func grantGroupConnectorUse(t *testing.T, s *Server, orgID, groupID, connectorID uuid.UUID) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'group', $3, ARRAY['use'])`,
		orgID.String(), connectorID.String(), groupID.String())
	require.NoError(t, err)
}

// grantNotebookRun grants a user view+run on a notebook.
func grantNotebookRun(t *testing.T, s *Server, orgID, userID, notebookID uuid.UUID) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view','run'])`,
		orgID.String(), notebookID.String(), userID.String())
	require.NoError(t, err)
}

// preferWarehouseService records a user's routing preference for a warehouse.
func preferWarehouseService(t *testing.T, s *Server, userID, warehouseID, connectorID uuid.UUID) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_service_preferences (user_id, warehouse_id, connector_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, warehouse_id) DO UPDATE
		SET connector_id = EXCLUDED.connector_id, updated_at = now()`,
		userID.String(), warehouseID.String(), connectorID.String())
	require.NoError(t, err)
}
