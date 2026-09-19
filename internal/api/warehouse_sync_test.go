package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

// warehouseSyncTestMasterKey mirrors setupTestServer's key so fixture
// encryption matches the Server under test.
const warehouseSyncTestMasterKey = "test-master-key-for-tests-only!"

// warehouseSyncFixture holds the DB rows and the provisioner connection
// backing a reconcile test.
type warehouseSyncFixture struct {
	s           *Server
	conn        clickhouse.Conn
	orgID       uuid.UUID
	userID      uuid.UUID
	groupID     uuid.UUID
	warehouseID uuid.UUID
	connectorID uuid.UUID
}

// newWarehouseSyncTestServer connects to the test Postgres and builds a Server
// directly: setupTestServer lives in package api_test and is not callable from
// this file.
func newWarehouseSyncTestServer(t *testing.T) (*Server, []byte) {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, db.Migrate(context.Background()))
	key := crypto.DeriveKey(warehouseSyncTestMasterKey)
	s := NewServer(db, auth.NewJWTIssuer("test-secret", 15*time.Minute), audit.NewLogger(db), key, nil)
	return s, key
}

// setupWarehouseFixture seeds one org, user, group (with membership),
// clickhouse provisioner connector, warehouse, and a direct user grant plus a
// group grant. All IDs are random so repeated runs never collide.
func setupWarehouseFixture(t *testing.T) *warehouseSyncFixture {
	t.Helper()
	ctx := context.Background()
	s, key := newWarehouseSyncTestServer(t)

	cfg := models.ConnectorConfig{
		Host: "localhost", Port: 9000, User: "dev", Password: "dev", Database: "analytics",
	}
	conn, err := openWarehouseProvisionerConn(ctx, cfg)
	require.NoError(t, err, "ClickHouse dev container unreachable at localhost:9000 "+
		"(start it with: docker compose -f docker-compose.dev.yml up -d aether-clickhouse)")
	t.Cleanup(func() { conn.Close() })

	suffix := uuid.NewString()
	orgID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Warehouse Sync Org", "whsync-"+suffix)
	require.NoError(t, err)

	userID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), "whsync-"+suffix+"@test.local", "Warehouse Sync User")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID.String(), userID.String())
	require.NoError(t, err)

	groupID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Warehouse Sync Group")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		groupID.String(), userID.String())
	require.NoError(t, err)

	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	encrypted, err := crypto.Encrypt(configJSON, key)
	require.NoError(t, err)

	connectorID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by)
		VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
		connectorID.String(), orgID.String(), "Warehouse Sync Provisioner", encrypted, userID.String())
	require.NoError(t, err)

	warehouseID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO warehouses (id, org_id, name, provisioner_connector_id)
		VALUES ($1, $2, $3, $4)`,
		warehouseID.String(), orgID.String(), "Warehouse Sync WH", connectorID.String())
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx,
		`UPDATE connectors SET warehouse_id = $1 WHERE id = $2`,
		warehouseID.String(), connectorID.String())
	require.NoError(t, err)

	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
		VALUES
			($1, $2, 'user', $3, 'analytics', 'events', $4),
			($1, $2, 'group', $5, 'analytics', 'daily_revenue', $4)`,
		orgID.String(), warehouseID.String(), userID.String(), userID.String(), groupID.String())
	require.NoError(t, err)

	prefix := chaccess.IdentifierPrefix(warehouseID)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := dropPrefixedEntities(cleanupCtx, conn, "users", "DROP USER IF EXISTS %s", prefix); err != nil {
			t.Logf("cleanup clickhouse users: %v", err)
		}
		if err := dropPrefixedEntities(cleanupCtx, conn, "roles", "DROP ROLE IF EXISTS %s", prefix); err != nil {
			t.Logf("cleanup clickhouse roles: %v", err)
		}
		for _, stmt := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`DELETE FROM warehouses WHERE id = $1`, warehouseID},
			{`DELETE FROM connectors WHERE id = $1`, connectorID},
			{`DELETE FROM groups WHERE id = $1`, groupID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM orgs WHERE id = $1`, orgID},
		} {
			if _, err := s.db.Pool.Exec(cleanupCtx, stmt.sql, stmt.id.String()); err != nil {
				t.Logf("cleanup %s: %v", stmt.sql, err)
			}
		}
	})

	return &warehouseSyncFixture{
		s:           s,
		conn:        conn,
		orgID:       orgID,
		userID:      userID,
		groupID:     groupID,
		warehouseID: warehouseID,
		connectorID: connectorID,
	}
}

// dropPrefixedEntities drops every ClickHouse user/role whose name carries the
// warehouse prefix, using the given DROP statement template (one %s).
func dropPrefixedEntities(ctx context.Context, conn clickhouse.Conn, table, dropFmt, prefix string) error {
	rows, err := conn.Query(ctx, "SELECT name FROM system."+table+" WHERE startsWith(name, ?)", prefix)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		quoted, err := chaccess.QuoteIdent(name)
		if err != nil {
			continue
		}
		if err := conn.Exec(ctx, fmt.Sprintf(dropFmt, quoted)); err != nil {
			return err
		}
	}
	return nil
}

// dialProvisioner opens an independent ClickHouse connection from the
// warehouse's decrypted provisioner connector config.
func dialProvisioner(t *testing.T, s *Server, warehouseID uuid.UUID) clickhouse.Conn {
	t.Helper()
	var encrypted []byte
	require.NoError(t, s.db.Pool.QueryRow(context.Background(), `
		SELECT c.config_encrypted
		FROM warehouses w JOIN connectors c ON c.id = w.provisioner_connector_id
		WHERE w.id = $1`, warehouseID.String()).Scan(&encrypted))
	plain, err := crypto.Decrypt(encrypted, s.masterKey)
	require.NoError(t, err)
	var cfg models.ConnectorConfig
	require.NoError(t, json.Unmarshal(plain, &cfg))
	conn, err := openWarehouseProvisionerConn(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func requireClickHouseUserExists(t *testing.T, conn clickhouse.Conn, name string) {
	t.Helper()
	var n uint64
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT count() FROM system.users WHERE name = ?", name).Scan(&n))
	require.Equal(t, uint64(1), n, "clickhouse user %s should exist", name)
}

func requireClickHouseRoleExists(t *testing.T, conn clickhouse.Conn, name string) {
	t.Helper()
	var n uint64
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT count() FROM system.roles WHERE name = ?", name).Scan(&n))
	require.Equal(t, uint64(1), n, "clickhouse role %s should exist", name)
}

func requireClickHouseGrantExists(t *testing.T, conn clickhouse.Conn, subject, database, table string) {
	t.Helper()
	var n uint64
	require.NoError(t, conn.QueryRow(context.Background(), `
		SELECT count() FROM system.grants
		WHERE access_type = 'SELECT' AND database = ? AND table = ?
		  AND (user_name = ? OR role_name = ?)`,
		database, table, subject, subject).Scan(&n))
	require.Equal(t, uint64(1), n, "grant on %s.%s for %s should exist", database, table, subject)
}

func TestReconcileWarehouseProvisionsUsersAndRoles(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	var status string
	var syncErr *string
	var appliedFP *string
	var lastSynced *time.Time
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT sync_status, sync_error, applied_master_fp, last_synced_at
		FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&status, &syncErr, &appliedFP, &lastSynced))
	require.Equal(t, "ready", status)
	require.Nil(t, syncErr)
	require.NotNil(t, lastSynced)
	require.NotNil(t, appliedFP)
	require.Equal(t, chaccess.Fingerprint(string(fx.s.masterKey)), *appliedFP)

	conn := dialProvisioner(t, fx.s, fx.warehouseID)
	userIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID)
	roleIdent := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	requireClickHouseUserExists(t, conn, userIdent)
	requireClickHouseRoleExists(t, conn, roleIdent)
	requireClickHouseGrantExists(t, conn, userIdent, "analytics", "events")
	requireClickHouseGrantExists(t, conn, roleIdent, "analytics", "daily_revenue")

	// First sync audit carries the applied counts.
	var statements, users, roles int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'statements')::int, -1),
		       COALESCE((metadata->>'users')::int, -1),
		       COALESCE((metadata->>'roles')::int, -1)
		FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.sync' AND resource_id = $2
		ORDER BY id ASC LIMIT 1`, fx.orgID.String(), fx.warehouseID.String()).
		Scan(&statements, &users, &roles))
	require.Greater(t, statements, 0)
	require.Equal(t, 1, users)
	require.Equal(t, 1, roles)

	// A second reconcile is a no-op: no error, still ready, zero statements.
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT sync_status FROM warehouses WHERE id = $1`, fx.warehouseID.String()).Scan(&status))
	require.Equal(t, "ready", status)
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'statements')::int, -1)
		FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.sync' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`, fx.orgID.String(), fx.warehouseID.String()).Scan(&statements))
	require.Equal(t, 0, statements)
}

func TestReconcileWarehouseFailsClosedOnWildcard(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	// Inject a database wildcard grant outside Aether's model.
	userIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID)
	quotedUser, err := chaccess.QuoteIdent(userIdent)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "GRANT SELECT ON `analytics`.* TO "+quotedUser))

	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manual remediation")
	require.Contains(t, err.Error(), userIdent)
	require.Contains(t, err.Error(), "analytics.*")

	var status string
	var syncErr *string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status, sync_error FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&status, &syncErr))
	require.Equal(t, "error", status)
	require.NotNil(t, syncErr)
	require.Contains(t, *syncErr, userIdent)
	require.Contains(t, *syncErr, "analytics.*")

	var driftAudits int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.drift' AND resource_id = $2`,
		fx.orgID.String(), fx.warehouseID.String()).Scan(&driftAudits))
	require.Greater(t, driftAudits, 0)
}
