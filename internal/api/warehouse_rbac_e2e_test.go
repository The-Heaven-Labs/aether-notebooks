package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/models"
)

// warehouseRBACE2EFixture is the end-to-end RBAC fixture: one org with an
// admin, a group member, and an outsider, plus a warehouse whose provisioner
// connector is also its only service. The warehouse's ClickHouse grants are
// seeded through the admin HTTP API, and the tables themselves live in a
// uniquely named database dropped on cleanup.
type warehouseRBACE2EFixture struct {
	s           *Server
	conn        clickhouse.Conn
	cfg         models.ConnectorConfig
	orgID       uuid.UUID
	adminToken  string
	memberID    uuid.UUID
	outsiderID  uuid.UUID
	groupID     uuid.UUID
	warehouseID uuid.UUID
	connectorID uuid.UUID
	database    string
}

// setupWarehouseRBACE2EFixture probes ClickHouse before building the server so
// an unreachable dev stack skips instead of paying startup cost, then creates
// the ClickHouse tables and drives warehouse/connector setup through the HTTP
// API as an org admin.
func setupWarehouseRBACE2EFixture(t *testing.T) *warehouseRBACE2EFixture {
	t.Helper()
	requireClickHouseReachable(t)
	s, recorder := warehouseHandlersServer(t)
	// Start pool accounting from a clean slate.
	s.connPool.CloseAll()

	ctx := context.Background()
	orgID, _, adminToken := seedWarehouseOrgAdmin(t, s)
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")
	outsiderID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	groupID := seedGrantGroup(t, s, orgID, "RBAC E2E Group "+suffix[:8], memberID)

	cfg := warehouseSyncTestClickHouseConfig()
	conn, err := openWarehouseProvisionerConn(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	database := "aether_rbac_e2e_" + suffix[:12]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, conn.Exec(ctx, "CREATE DATABASE "+quotedDB))
	require.NoError(t, conn.Exec(ctx, "CREATE TABLE "+quotedDB+".`t1` (id UInt8, label String) ENGINE = MergeTree ORDER BY id"))
	require.NoError(t, conn.Exec(ctx, "CREATE TABLE "+quotedDB+".`t2` (id UInt8) ENGINE = MergeTree ORDER BY id"))
	require.NoError(t, conn.Exec(ctx, "INSERT INTO "+quotedDB+".`t1` VALUES (1, 'one'), (2, 'two')"))
	require.NoError(t, conn.Exec(ctx, "INSERT INTO "+quotedDB+".`t2` VALUES (1), (2)"))

	// The admin creates the ClickHouse connector and a warehouse that adopts
	// it as both provisioner and first service, exactly as the UI does.
	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/connectors", adminToken, map[string]any{
		"name": "RBAC E2E Provisioner " + suffix[:8],
		"type": "clickhouse",
		"config": map[string]any{
			"host": cfg.Host, "port": cfg.Port, "user": cfg.User,
			"password": cfg.Password, "database": cfg.Database,
		},
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var createdConnector struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createdConnector))
	connectorID, err := uuid.Parse(createdConnector.ID)
	require.NoError(t, err)

	rec = warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", adminToken, map[string]any{
		"name":                     "RBAC E2E Warehouse " + suffix[:8],
		"provisioner_connector_id": connectorID.String(),
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	warehouseID, err := uuid.Parse(wh.ID)
	require.NoError(t, err)
	require.NotNil(t, wh.ProvisionerConnectorID)
	require.Equal(t, connectorID.String(), *wh.ProvisionerConnectorID)
	require.Equal(t, warehouseID.String(), *connectorWarehouse(t, s, connectorID))
	require.True(t, recorder.contains(warehouseID),
		"creating a warehouse with a provisioner must enqueue its first sync")

	// Both execution subjects need service access; the tables stay
	// ClickHouse-enforced.
	grantConnectorUse(t, s, orgID, memberID, connectorID)
	grantConnectorUse(t, s, orgID, outsiderID, connectorID)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		prefix := chaccess.IdentifierPrefix(warehouseID)
		if err := dropPrefixedEntities(cleanupCtx, conn, "users", "DROP USER IF EXISTS %s", prefix); err != nil {
			t.Logf("cleanup clickhouse users: %v", err)
		}
		if err := dropPrefixedEntities(cleanupCtx, conn, "roles", "DROP ROLE IF EXISTS %s", prefix); err != nil {
			t.Logf("cleanup clickhouse roles: %v", err)
		}
		if err := conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB); err != nil {
			t.Logf("cleanup clickhouse database: %v", err)
		}
	})

	// Registered after the ClickHouse cleanup: Cleanup runs LIFO, so the pool
	// closes before the identities it authenticated as are dropped.
	t.Cleanup(func() { s.connPool.CloseAll() })

	return &warehouseRBACE2EFixture{
		s:           s,
		conn:        conn,
		cfg:         cfg,
		orgID:       orgID,
		adminToken:  adminToken,
		memberID:    memberID,
		outsiderID:  outsiderID,
		groupID:     groupID,
		warehouseID: warehouseID,
		connectorID: connectorID,
		database:    database,
	}
}

// table returns the backtick-quoted database.table reference for a query.
func (fx *warehouseRBACE2EFixture) table(t *testing.T, name string) string {
	t.Helper()
	db, err := chaccess.QuoteObjectIdent(fx.database)
	require.NoError(t, err)
	tbl, err := chaccess.QuoteObjectIdent(name)
	require.NoError(t, err)
	return db + "." + tbl
}

// runAs creates a notebook cell with source and executes it through the full
// HTTP stack as userID with the non-admin role (no admin-mode bypass),
// asserting the response status. Reconcile invalidates pooled identities, so
// consecutive runs only reuse connections within the same access state, which
// is exactly what the RBAC steps below exercise.
func (fx *warehouseRBACE2EFixture) runAs(t *testing.T, userID uuid.UUID, wantStatus int, source string) *httptest.ResponseRecorder {
	t.Helper()
	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, userID, fx.connectorID, source, nil)
	grantNotebookRun(t, fx.s, fx.orgID, userID, nbID)

	token, err := fx.s.jwt.Issue(userID.String(), fx.orgID.String(), "non-admin")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/notebooks/"+nbID.String()+"/cells/"+cellID.String()+"/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	fx.s.ServeHTTP(rec, req)
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())
	return rec
}

// requireClickHouseDenied asserts the handler surfaced a ClickHouse
// access-denied failure as a 403.
func requireClickHouseDenied(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	body := strings.ToLower(rec.Body.String())
	require.True(t,
		strings.Contains(body, "not enough privileges") || strings.Contains(body, "access_denied"),
		"expected a ClickHouse access-denied message, got: %s", rec.Body.String())
}

// countRoleGrants counts a user's membership in one ClickHouse role.
func countRoleGrants(t *testing.T, conn clickhouse.Conn, user, role string) int {
	t.Helper()
	var n uint64
	require.NoError(t, conn.QueryRow(context.Background(), `
		SELECT count() FROM system.role_grants
		WHERE user_name = ? AND granted_role_name = ?`, user, role).Scan(&n))
	return int(n)
}

// TestWarehouseRBACE2E walks the managed ClickHouse RBAC matrix end to end
// against the dev stack: group grants, everyone grants, table-function
// denial, drift repair, revocation, and offboarding. It skips when ClickHouse
// is unreachable. The kill-switch-off legacy path is covered by
// TestKillSwitchOffRoutesManagedConnectorLegacy; drift detection by the
// TestDriftDetection* cases in warehouse_sync_test.go.
func TestWarehouseRBACE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)
	s := fx.s

	memberIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.memberID)
	outsiderIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.outsiderID)
	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	everyoneRole := chaccess.EveryoneRole(fx.warehouseID)

	// Step 1: the admin grants the group table t1 only, through the API.
	rec := createGrantViaAPI(t, s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "t1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var createdGrant struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createdGrant))
	groupGrantID, err := uuid.Parse(createdGrant.ID)
	require.NoError(t, err)

	// Step 2: sync. The group role and the member's identity materialize.
	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))
	requireClickHouseRoleExists(t, fx.conn, groupRole)
	requireClickHouseGrantExists(t, fx.conn, groupRole, fx.database, "t1")
	requireClickHouseUserExists(t, fx.conn, memberIdent)

	// Step 3: the member executes the granted table as their own identity.
	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT currentUser() AS ch_user, count() AS n FROM "+fx.table(t, "t1"))
	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 1)
	require.Equal(t, memberIdent, out.Outputs[0].Data.Rows[0][0])
	require.Equal(t, float64(2), out.Outputs[0].Data.Rows[0][1])
	require.Equal(t, 1, s.connPool.Len(),
		"the successful run must leave the member's connection pooled")

	// Step 4: t2 exists but is not granted; ClickHouse denies it.
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT count() FROM "+fx.table(t, "t2"))
	requireClickHouseDenied(t, rec)

	// Step 5: table functions stay ungranted, so remote() is a privilege
	// error rather than a way around the table grants.
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		fmt.Sprintf("SELECT count() FROM remote('127.0.0.1:%d', '%s.t1')", fx.cfg.Port, fx.database))
	requireClickHouseDenied(t, rec)

	// Step 6: an everyone grant on t2 provisions the outsider (no group) and
	// joins the member's union. The reconcile applies DDL, so it must drop the
	// pooled connection the member built in step 3; the next run then sees the
	// new role without waiting for idle eviction.
	rec = createGrantViaAPI(t, s, fx.adminToken, fx.warehouseID,
		grantBody("everyone", "everyone", fx.database, "t2"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))
	require.Zero(t, s.connPool.Len(),
		"a reconcile that applied DDL must invalidate the warehouse's pooled identities")
	requireClickHouseRoleExists(t, fx.conn, everyoneRole)
	requireClickHouseGrantExists(t, fx.conn, everyoneRole, fx.database, "t2")
	requireClickHouseUserExists(t, fx.conn, outsiderIdent)
	require.Equal(t, 1, countRoleGrants(t, fx.conn, memberIdent, everyoneRole))

	rec = fx.runAs(t, fx.outsiderID, http.StatusOK,
		"SELECT count() AS n FROM "+fx.table(t, "t2"))
	out = decodeExecuteOutputs(t, rec)
	require.Equal(t, float64(2), out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.outsiderID, http.StatusForbidden,
		"SELECT count() FROM "+fx.table(t, "t1"))
	requireClickHouseDenied(t, rec)

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT count() AS n FROM "+fx.table(t, "t2"))
	decodeExecuteOutputs(t, rec)
	require.Equal(t, 2, s.connPool.Len(),
		"both subjects' runs must be pooled after the invalidating reconcile")

	// Step 7: drift repair. Revoking the everyone grant outside Aether is
	// healed by the next reconcile, and reported as missing drift.
	driftLabel := fx.database + ".t2 for " + everyoneRole
	countDriftAudits := func() int {
		t.Helper()
		var n int
		require.NoError(t, s.db.Pool.QueryRow(ctx, `
			SELECT count(*) FROM audit_logs
			WHERE org_id = $1 AND action = 'warehouse.drift' AND resource_id = $2
			  AND metadata->'missing_grants' @> $3::jsonb`,
			fx.orgID.String(), fx.warehouseID.String(),
			fmt.Sprintf(`[%q]`, driftLabel)).Scan(&n))
		return n
	}
	driftAuditsBefore := countDriftAudits()

	quotedEveryone, err := chaccess.QuoteIdent(everyoneRole)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx,
		"REVOKE SELECT ON "+fx.table(t, "t2")+" FROM "+quotedEveryone))
	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))
	requireClickHouseGrantExists(t, fx.conn, everyoneRole, fx.database, "t2")
	require.Zero(t, s.connPool.Len(),
		"the repairing reconcile must invalidate the identities it re-granted")
	require.Equal(t, driftAuditsBefore+1, countDriftAudits(),
		"the repaired drift must be audited exactly once")

	// Step 8: the admin revokes the group grant; the member keeps their
	// identity through the everyone grant but loses t1 in ClickHouse. The
	// pooled connection from the t2 run must not keep the revoked role alive.
	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT count() AS n FROM "+fx.table(t, "t2"))
	decodeExecuteOutputs(t, rec)
	require.Equal(t, 1, s.connPool.Len())

	rec = deleteGrantViaAPI(t, s, fx.adminToken, fx.warehouseID, groupGrantID)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))
	require.Zero(t, s.connPool.Len(),
		"a revoking reconcile must invalidate the pooled identity")
	requireClickHouseUserExists(t, fx.conn, memberIdent)
	require.Zero(t, countRoleGrants(t, fx.conn, memberIdent, groupRole),
		"the revoked group role must no longer be granted to the member")

	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT count() FROM "+fx.table(t, "t1"))
	requireClickHouseDenied(t, rec)
	require.Equal(t, 1, s.connPool.Len(),
		"the denied run reopens a connection as the reduced identity")

	// Step 9: offboarding removes the member from the org, and the next
	// reconcile drops the ClickHouse identity; the outsider is untouched.
	rec = warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/members/"+fx.memberID.String(), fx.adminToken, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))
	require.Zero(t, s.connPool.Len(),
		"dropping the identity must invalidate its pooled connection")
	requireClickHouseUserAbsent(t, fx.conn, memberIdent)
	requireClickHouseUserExists(t, fx.conn, outsiderIdent)

	// The offboarded member's token no longer resolves a service, so the run
	// fails closed in Aether before reaching ClickHouse (and before pooling).
	fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT count() AS n FROM "+fx.table(t, "t2"))
	require.Zero(t, s.connPool.Len())
}
