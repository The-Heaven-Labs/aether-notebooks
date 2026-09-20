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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// warehouseSyncTestDSN returns the Postgres DSN used by these tests.
func warehouseSyncTestDSN() string {
	if dsn := os.Getenv("AETHER_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
}

// warehouseSyncTestClickHouseConfig is the dev-stack ClickHouse connector
// config (see docker-compose.dev.yml).
func warehouseSyncTestClickHouseConfig() models.ConnectorConfig {
	return models.ConnectorConfig{
		Host: "localhost", Port: 9000, User: "dev", Password: "dev", Database: "analytics",
	}
}

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
	db, err := database.Connect(context.Background(), warehouseSyncTestDSN(), "")
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
	s, key := newWarehouseSyncTestServer(t)
	return setupWarehouseFixtureWithServer(t, s, key)
}

// setupWarehouseFixtureWithServer seeds the fixture against an existing
// Server, so tests can share one pool across warehouses.
func setupWarehouseFixtureWithServer(t *testing.T, s *Server, key []byte) *warehouseSyncFixture {
	t.Helper()
	ctx := context.Background()

	cfg := warehouseSyncTestClickHouseConfig()
	conn, err := openWarehouseProvisionerConn(ctx, cfg)
	if err != nil {
		t.Skipf("clickhouse unavailable at %s:%d: %v", cfg.Host, cfg.Port, err)
	}
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

func requireClickHouseUserAbsent(t *testing.T, conn clickhouse.Conn, name string) {
	t.Helper()
	var n uint64
	require.NoError(t, conn.QueryRow(context.Background(),
		"SELECT count() FROM system.users WHERE name = ?", name).Scan(&n))
	require.Equal(t, uint64(0), n, "clickhouse user %s should not exist", name)
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

func TestReconcileWarehouseRejectsForeignProvisioner(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	// A second warehouse in the same org owns its own connector. Pointing the
	// fixture warehouse at that connector must fail closed instead of
	// provisioning through another warehouse's credential namespace.
	var encrypted []byte
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT config_encrypted FROM connectors WHERE id = $1`, fx.connectorID.String()).Scan(&encrypted))

	otherWarehouseID := uuid.New()
	otherConnectorID := uuid.New()
	_, err := fx.s.db.Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, $3)`,
		otherWarehouseID.String(), fx.orgID.String(), "Foreign Provisioner Warehouse")
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
		VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
		otherConnectorID.String(), fx.orgID.String(), "Foreign Provisioner Connector",
		encrypted, otherWarehouseID.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET provisioner_connector_id = $1 WHERE id = $2`,
		otherConnectorID.String(), otherWarehouseID.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM warehouses WHERE id = $1`, otherWarehouseID.String()); err != nil {
			t.Logf("cleanup foreign warehouse: %v", err)
		}
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM connectors WHERE id = $1`, otherConnectorID.String()); err != nil {
			t.Logf("cleanup foreign connector: %v", err)
		}
	})

	// Cross-wire the fixture warehouse directly, bypassing the write path.
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET provisioner_connector_id = $1 WHERE id = $2`,
		otherConnectorID.String(), fx.warehouseID.String())
	require.NoError(t, err)

	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not belong to warehouse")

	var status string
	var syncErr *string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status, sync_error FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&status, &syncErr))
	require.Equal(t, "error", status)
	require.NotNil(t, syncErr)
	require.Contains(t, *syncErr, "does not belong to warehouse")

	// Nothing may be provisioned from the foreign connector.
	requireClickHouseUserAbsent(t, fx.conn, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID))
}

// pingWarehouseUser connects as the warehouse-provisioned identity using the
// derived password and pings. A non-nil error means authentication failed; on
// success the caller closes the returned connection.
func pingWarehouseUser(s *Server, warehouseID, orgID, userID uuid.UUID) (clickhouse.Conn, error) {
	cfg := warehouseSyncTestClickHouseConfig()
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth: clickhouse.Auth{
			Username: chaccess.UserIdent(warehouseID, orgID, userID),
			Password: chaccess.DerivePassword(s.masterKey, warehouseID, userID),
		},
		Protocol: clickhouse.Native,
	})
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// requireNoPrefixedEntities asserts the warehouse namespace is empty.
func requireNoPrefixedEntities(t *testing.T, conn clickhouse.Conn, prefix string) {
	t.Helper()
	for _, table := range []string{"users", "roles"} {
		var n uint64
		require.NoError(t, conn.QueryRow(context.Background(),
			"SELECT count() FROM system."+table+" WHERE startsWith(name, ?)", prefix).Scan(&n))
		require.Equal(t, uint64(0), n, "system.%s must have no entities with prefix %s", table, prefix)
	}
}

func TestReconcileWarehouseRekeysOnFingerprintMismatch(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	userIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID)
	quotedUser, err := chaccess.QuoteIdent(userIdent)
	require.NoError(t, err)

	// Break the stored password so successful authentication after the next
	// reconcile proves an ALTER USER was emitted.
	require.NoError(t, fx.conn.Exec(ctx,
		"ALTER USER "+quotedUser+" IDENTIFIED WITH sha256_password BY 'not-the-derived-password'"))
	staleConn, err := pingWarehouseUser(fx.s, fx.warehouseID, fx.orgID, fx.userID)
	require.Error(t, err, "stale password must not authenticate")
	require.Nil(t, staleConn)

	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET applied_master_fp = 'stale-fingerprint' WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	var appliedFP *string
	var status string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT applied_master_fp, sync_status FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&appliedFP, &status))
	require.Equal(t, "ready", status)
	require.NotNil(t, appliedFP)
	require.Equal(t, chaccess.Fingerprint(string(fx.s.masterKey)), *appliedFP)

	userConn, err := pingWarehouseUser(fx.s, fx.warehouseID, fx.orgID, fx.userID)
	require.NoError(t, err, "derived password must authenticate after the rekey")
	userConn.Close()

	// The rekey is one-shot: the next reconcile emits no statements.
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))
	var statements int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'statements')::int, -1)
		FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.sync' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`, fx.orgID.String(), fx.warehouseID.String()).Scan(&statements))
	require.Equal(t, 0, statements)
}

func TestReconcileWarehouseStatementFailureMarksError(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	// A provisioner that can read the actual state it needs and create roles,
	// but cannot create users: the plan fails mid-way (role created, user
	// denied), which proves partial execution is reported honestly.
	limitedUser := "whrev_limited_" + uuid.NewString()[:8]
	limitedPassword := "limited-password"
	require.NoError(t, fx.conn.Exec(ctx,
		fmt.Sprintf("CREATE USER %s IDENTIFIED WITH sha256_password BY '%s'", limitedUser, limitedPassword)))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := fx.conn.Exec(cleanupCtx, "DROP USER IF EXISTS "+limitedUser); err != nil {
			t.Logf("cleanup limited user: %v", err)
		}
		if err := fx.conn.Exec(cleanupCtx, "DROP ROLE IF EXISTS "+limitedUser); err != nil {
			t.Logf("cleanup limited role: %v", err)
		}
	})
	for _, grant := range []string{
		"GRANT SELECT ON system.grants TO " + limitedUser,
		"GRANT SELECT ON system.users TO " + limitedUser,
		"GRANT SELECT ON system.roles TO " + limitedUser,
		"GRANT SELECT ON system.role_grants TO " + limitedUser,
		"GRANT CREATE ROLE ON *.* TO " + limitedUser,
		"GRANT SELECT ON analytics.* TO " + limitedUser + " WITH GRANT OPTION",
	} {
		require.NoError(t, fx.conn.Exec(ctx, grant))
	}

	limitedCfg := warehouseSyncTestClickHouseConfig()
	limitedCfg.User = limitedUser
	limitedCfg.Password = limitedPassword
	configJSON, err := json.Marshal(limitedCfg)
	require.NoError(t, err)
	encrypted, err := crypto.Encrypt(configJSON, fx.s.masterKey)
	require.NoError(t, err)

	limitedConnectorID := uuid.New()
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
		VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
		limitedConnectorID.String(), fx.orgID.String(), "Limited Provisioner",
		encrypted, fx.warehouseID.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM connectors WHERE id = $1`, limitedConnectorID.String()); err != nil {
			t.Logf("cleanup limited connector: %v", err)
		}
	})
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET provisioner_connector_id = $1 WHERE id = $2`,
		limitedConnectorID.String(), fx.warehouseID.String())
	require.NoError(t, err)

	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)

	var status string
	var syncErr, appliedFP *string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status, sync_error, applied_master_fp FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&status, &syncErr, &appliedFP))
	require.Equal(t, "error", status)
	require.NotNil(t, syncErr)
	require.NotEmpty(t, *syncErr)
	require.NotContains(t, *syncErr, "Ae1_", "sync_error must not leak derived passwords")
	require.NotContains(t, *syncErr, "BY '", "sync_error must not leak DDL")
	require.Nil(t, appliedFP, "fingerprint must not advance on a failed run")

	// The role from the partial plan exists; the user was never created.
	requireClickHouseRoleExists(t, fx.conn, chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID))
	requireClickHouseUserAbsent(t, fx.conn, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID))
}

func TestReconcileWarehouseSoftDeletedProvisioner(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	_, err := fx.s.db.Pool.Exec(ctx,
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, fx.connectorID.String())
	require.NoError(t, err)

	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "soft-deleted")

	var status string
	var syncErr *string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status, sync_error FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&status, &syncErr))
	require.Equal(t, "error", status)
	require.NotNil(t, syncErr)
	require.Contains(t, *syncErr, "soft-deleted")

	requireNoPrefixedEntities(t, fx.conn, chaccess.IdentifierPrefix(fx.warehouseID))
}

func TestReconcileWarehouseIgnoresForeignSubjects(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	// Replace the fixture grants with ones naming a user and a group that do
	// not belong to the warehouse's org; neither may be provisioned.
	_, err := fx.s.db.Pool.Exec(ctx,
		`DELETE FROM warehouse_table_grants WHERE warehouse_id = $1`, fx.warehouseID.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES
			($1, $2, 'user', $3, 'analytics', 'events'),
			($1, $2, 'group', $4, 'analytics', 'daily_revenue')`,
		fx.orgID.String(), fx.warehouseID.String(), uuid.NewString(), uuid.NewString())
	require.NoError(t, err)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	var status string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status FROM warehouses WHERE id = $1`, fx.warehouseID.String()).Scan(&status))
	require.Equal(t, "ready", status)
	requireNoPrefixedEntities(t, fx.conn, chaccess.IdentifierPrefix(fx.warehouseID))

	var users, roles int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'users')::int, -1), COALESCE((metadata->>'roles')::int, -1)
		FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.sync' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`, fx.orgID.String(), fx.warehouseID.String()).Scan(&users, &roles))
	require.Equal(t, 0, users)
	require.Equal(t, 0, roles)
}

func TestReconcileWarehouseAuditsSkippedCatalogNames(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	// "my table" cannot be quoted for a GRANT; the reconcile must audit it as
	// drift and continue with the rest of the plan.
	_, err := fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, 'analytics', 'my table')`,
		fx.orgID.String(), fx.warehouseID.String(), fx.userID.String())
	require.NoError(t, err)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	var status string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status FROM warehouses WHERE id = $1`, fx.warehouseID.String()).Scan(&status))
	require.Equal(t, "ready", status)

	// The valid grant still applied, proving the skipped name did not abort.
	userIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID)
	requireClickHouseUserExists(t, fx.conn, userIdent)
	requireClickHouseGrantExists(t, fx.conn, userIdent, "analytics", "events")

	var driftAudits int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.drift' AND resource_id = $2
		  AND metadata->'skipped' @> $3::jsonb`,
		fx.orgID.String(), fx.warehouseID.String(), `["analytics.my table"]`).Scan(&driftAudits))
	require.Equal(t, 1, driftAudits)
}

func TestReconcileWarehouseSkipsWhenLockHeld(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseFixture(t)

	lockConn, err := pgx.Connect(ctx, warehouseSyncTestDSN())
	require.NoError(t, err)
	t.Cleanup(func() { lockConn.Close(context.Background()) })

	var locked bool
	require.NoError(t, lockConn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtextextended($1::text, 0))`, fx.warehouseID.String()).Scan(&locked))
	require.True(t, locked, "test must hold the warehouse advisory lock")

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	var status string
	var syncErr *string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT sync_status, sync_error FROM warehouses WHERE id = $1`, fx.warehouseID.String()).
		Scan(&status, &syncErr))
	require.Equal(t, "pending", status, "skipped reconcile must not touch sync_status")
	require.Nil(t, syncErr)
	requireNoPrefixedEntities(t, fx.conn, chaccess.IdentifierPrefix(fx.warehouseID))

	var skippedAudits int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.sync.skipped' AND resource_id = $2`,
		fx.orgID.String(), fx.warehouseID.String()).Scan(&skippedAudits))
	require.Equal(t, 1, skippedAudits)
}

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "create user statement",
			in:   "CREATE USER `aether_x_u_y` IDENTIFIED WITH sha256_password BY 'Ae1_supersecret' GRANTEES NONE",
			want: "CREATE USER `aether_x_u_y` IDENTIFIED WITH sha256_password BY '<redacted>' GRANTEES NONE",
		},
		{
			name: "alter user statement",
			in:   "ALTER USER u IDENTIFIED BY 'pw'",
			want: "ALTER USER u IDENTIFIED BY '<redacted>'",
		},
		{
			name: "no secret",
			in:   "code: 497, message: ACCESS_DENIED: not enough privileges",
			want: "code: 497, message: ACCESS_DENIED: not enough privileges",
		},
		{
			name: "word containing by is untouched",
			in:   "standby 'x'",
			want: "standby 'x'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, redactSecrets(tc.in))
		})
	}
}

// TestReconcileWarehouseDoesNotStarveSmallPool guards against the sync lock
// occupying a pooled connection: with MaxConns=2 two concurrent reconciles
// must still complete instead of self-deadlocking on the pool.
func TestReconcileWarehouseDoesNotStarveSmallPool(t *testing.T) {
	ctx := context.Background()

	poolCfg, err := pgxpool.ParseConfig(warehouseSyncTestDSN())
	require.NoError(t, err)
	poolCfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := &database.DB{Pool: pool}
	require.NoError(t, db.Migrate(ctx))

	key := crypto.DeriveKey(warehouseSyncTestMasterKey)
	s := NewServer(db, auth.NewJWTIssuer("test-secret", 15*time.Minute), audit.NewLogger(db), key, nil)
	fxA := setupWarehouseFixtureWithServer(t, s, key)
	fxB := setupWarehouseFixtureWithServer(t, s, key)

	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- s.reconcileWarehouse(runCtx, fxA.warehouseID) }()
	go func() { errCh <- s.reconcileWarehouse(runCtx, fxB.warehouseID) }()

	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(60 * time.Second):
			t.Fatal("reconcile starved: concurrent reconciles exhausted the 2-connection pool")
		}
	}
}
