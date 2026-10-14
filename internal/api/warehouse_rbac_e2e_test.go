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
	require.NoError(t, conn.Exec(ctx, "CREATE DICTIONARY "+quotedDB+".`d1` (id UInt8, label String) PRIMARY KEY id "+
		"SOURCE(CLICKHOUSE(DB '"+database+"' TABLE 't1' USER '"+cfg.User+"' PASSWORD '"+cfg.Password+"')) "+
		"LIFETIME(MIN 0 MAX 0) LAYOUT(FLAT())"))
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

	// The provisioner is this warehouse's only connector; users execute as
	// their per-user identities, which requires the admin override.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+warehouseID.String(), adminToken,
		map[string]any{"allow_provisioner_execution": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

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

// requireClickHousePrivilegeCount asserts the number of observed system.grants
// rows of one managed access type on an object, for a subject that is either a
// user (user_name) or a role (role_name). Zero asserts the privilege is gone.
func requireClickHousePrivilegeCount(t *testing.T, conn clickhouse.Conn, subject, access, database, table string, want uint64) {
	t.Helper()
	var n uint64
	require.NoError(t, conn.QueryRow(context.Background(), `
		SELECT count() FROM system.grants
		WHERE access_type = ? AND database = ? AND table = ?
		  AND (user_name = ? OR role_name = ?)`,
		access, database, table, subject, subject).Scan(&n))
	require.Equal(t, want, n,
		"observed %s grants on %s.%s for %s (want %d, got %d)", access, database, table, subject, want, n)
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

// TestWarehouseDictionaryGrantsE2E proves the read bundle makes granted
// dictionaries usable: direct SELECT, dictGet-family functions, and
// SHOW CREATE DICTIONARY all work for the managed identity, and revocation
// removes all of it. It skips when ClickHouse is unreachable or lacks
// granular dictionary privileges.
func TestWarehouseDictionaryGrantsE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)

	mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn)
	require.NoError(t, err, "probe the ClickHouse privilege mode")
	if mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q)", mode)
	}

	// Grant only the dictionary to the group; t1 stays ungranted.
	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "d1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var createdGrant struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createdGrant))
	grantID, err := uuid.Parse(createdGrant.ID)
	require.NoError(t, err)
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	requireClickHousePrivilegeCount(t, fx.conn, groupRole, "dictGet", fx.database, "d1", 1)

	// Direct read, dictGet-family call, and SHOW CREATE all work as the
	// member's own identity.
	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT label FROM "+fx.table(t, "d1")+" WHERE id = 1")
	out := decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT dictGet('"+fx.database+".d1', 'label', toUInt8(1)) AS label")
	out = decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT dictHas('"+fx.database+".d1', toUInt8(1)) AS has_key")
	out = decodeExecuteOutputs(t, rec)
	require.Equal(t, float64(1), out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.memberID, http.StatusOK, "SHOW CREATE DICTIONARY "+fx.table(t, "d1"))
	out = decodeExecuteOutputs(t, rec)
	returnedDDL := false
	for _, row := range out.Outputs[0].Data.Rows {
		for _, cell := range row {
			if s, ok := cell.(string); ok && strings.Contains(s, "CREATE DICTIONARY") {
				returnedDDL = true
			}
		}
	}
	require.True(t, returnedDDL, "SHOW CREATE DICTIONARY must return the dictionary DDL")

	// The dictionary is a data grant of its own: the source table stays
	// denied.
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT count() FROM "+fx.table(t, "t1"))
	requireClickHouseDenied(t, rec)

	// Revocation through the API removes the dictionary's privileges again.
	// d1 is the member's only granted object right now, and a reconcile that
	// leaves a member with no effective grants also drops their ClickHouse
	// identity (TestWarehouseRBACE2E pins that lifecycle), which would turn
	// the run below into an authentication error instead of a denial. Grant
	// the untouched t1 to the group first so the identity survives the
	// revocation and the run proves dictionary privilege removal.
	rec = createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "t1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	rec = deleteGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID, grantID)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	// The direct read needs SELECT and dictGet, and the family function needs
	// dictGet: both must be denied, and no managed privilege may remain.
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT label FROM "+fx.table(t, "d1")+" WHERE id = 1")
	requireClickHouseDenied(t, rec)
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT dictGet('"+fx.database+".d1', 'label', toUInt8(1))")
	requireClickHouseDenied(t, rec)
	for _, access := range []string{"SELECT", "dictGet", "SHOW DICTIONARIES"} {
		requireClickHousePrivilegeCount(t, fx.conn, groupRole, access, fx.database, "d1", 0)
	}
}

// TestWarehouseDictionaryGrantUpgradeRepair simulates a deployment upgrading
// to the dictionary read bundle: the grant row already exists in Postgres,
// but ClickHouse still holds the pre-feature SELECT-only state. The first
// reconcile after upgrade must repair dictGet and SHOW DICTIONARIES on the
// existing grant, not just provision fresh grants with the full bundle.
func TestWarehouseDictionaryGrantUpgradeRepair(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)
	mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn)
	require.NoError(t, err, "probe the ClickHouse privilege mode")
	if mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q)", mode)
	}

	// The grant row is already there, exactly as an upgrading deployment
	// would have it; no reconcile has run for it yet.
	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "d1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Reconstruct the pre-feature ClickHouse state by hand: the group role,
	// the member's identity (same password derivation the server uses, so the
	// pooled execution path authenticates), and SELECT-only on d1.
	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	quotedRole, err := chaccess.QuoteIdent(groupRole)
	require.NoError(t, err)
	memberIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.memberID)
	quotedUser, err := chaccess.QuoteIdent(memberIdent)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE ROLE IF NOT EXISTS "+quotedRole))
	require.NoError(t, fx.conn.Exec(ctx, "CREATE USER IF NOT EXISTS "+quotedUser+
		" IDENTIFIED WITH sha256_password BY '"+
		chaccess.DerivePassword(fx.s.masterKey, fx.warehouseID, fx.memberID)+"' GRANTEES NONE"))
	require.NoError(t, fx.conn.Exec(ctx, "GRANT "+quotedRole+" TO "+quotedUser))
	require.NoError(t, fx.conn.Exec(ctx, "SET DEFAULT ROLE ALL TO "+quotedUser))
	require.NoError(t, fx.conn.Exec(ctx, "GRANT SELECT ON "+fx.table(t, "d1")+" TO "+quotedRole))

	// Sanity: the simulated state is genuinely SELECT-only.
	requireClickHousePrivilegeCount(t, fx.conn, groupRole, "dictGet", fx.database, "d1", 0)

	// A deployed warehouse has already synced once: seed the header so the
	// upgrade repairs in place instead of provisioning from scratch, with a
	// matching master-key fingerprint so no password re-key is due.
	fingerprint := chaccess.Fingerprint(string(fx.s.masterKey))
	_, err = fx.s.db.Pool.Exec(ctx, `
		UPDATE warehouses SET sync_status = 'ready', applied_master_fp = $1 WHERE id = $2`,
		fingerprint, fx.warehouseID.String())
	require.NoError(t, err)

	// The upgrade event: the first reconcile must repair the existing grant.
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	for _, access := range []string{"SELECT", "dictGet", "SHOW DICTIONARIES"} {
		requireClickHousePrivilegeCount(t, fx.conn, groupRole, access, fx.database, "d1", 1)
	}

	// The repair is drift: exactly one audit names the missing bundle.
	var driftAudits int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.drift' AND resource_id = $2
		  AND metadata->'missing_grants' @> $3::jsonb`,
		fx.orgID.String(), fx.warehouseID.String(),
		fmt.Sprintf(`[%q]`, fx.database+".d1 for "+groupRole)).Scan(&driftAudits))
	require.Equal(t, 1, driftAudits, "the repair must be audited as missing drift")

	// The matching fingerprint means the member's password was not re-keyed.
	var appliedFP *string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		"SELECT applied_master_fp FROM warehouses WHERE id = $1", fx.warehouseID.String()).Scan(&appliedFP))
	require.NotNil(t, appliedFP)
	require.Equal(t, fingerprint, *appliedFP)

	// The acceptance outcome: the member can use the repaired dictionary.
	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT label FROM "+fx.table(t, "d1")+" WHERE id = 1")
	out := decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT dictGet('"+fx.database+".d1', 'label', toUInt8(1)) AS label")
	out = decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])
}

// TestWarehouseDictionaryGuardE2E proves the drift guard now manages
// dictGet: an outside-of-Aether grant on an ungranted object is revoked, and
// a wildcard dictionary grant still fails the warehouse closed.
func TestWarehouseDictionaryGuardE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)
	mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn)
	require.NoError(t, err, "probe the ClickHouse privilege mode")
	if mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q)", mode)
	}

	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "t1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	quotedRole, err := chaccess.QuoteIdent(groupRole)
	require.NoError(t, err)

	// An extra dictGet on the ungranted dictionary is managed drift: the
	// next reconcile revokes it (same treatment as an extra SELECT).
	require.NoError(t, fx.conn.Exec(ctx,
		"GRANT dictGet ON "+fx.table(t, "d1")+" TO "+quotedRole))
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))
	requireClickHousePrivilegeCount(t, fx.conn, groupRole, "dictGet", fx.database, "d1", 0)

	// A wildcard dictGet is outside the model and fails closed.
	require.NoError(t, fx.conn.Exec(ctx,
		"GRANT dictGet ON "+fx.database+".* TO "+quotedRole))
	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manual remediation")
	var status string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		"SELECT sync_status FROM warehouses WHERE id = $1", fx.warehouseID.String()).Scan(&status))
	require.Equal(t, "error", status)
}

// TestWarehouseDictionaryPrefixWildcardGuardE2E pins the SHOW GRANTS fallback
// path used on servers without system.grants.is_wildcard (24.x): a prefix
// wildcard dictGet strips the star in system.grants, so only the SHOW GRANTS
// parser can recognise it and fail the warehouse closed.
func TestWarehouseDictionaryPrefixWildcardGuardE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)
	mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn)
	require.NoError(t, err, "probe the ClickHouse privilege mode")
	if mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q)", mode)
	}

	// Anchor the role with a real grant so the wildcard scan has an identity
	// to inspect.
	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "t1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	quotedRole, err := chaccess.QuoteIdent(groupRole)
	require.NoError(t, err)

	// On 24.x system.grants stores this as table='d1' (star stripped), so
	// only SHOW GRANTS FOR renders the wildcard.
	require.NoError(t, fx.conn.Exec(ctx,
		"GRANT dictGet ON "+fx.table(t, "d1")+"* TO "+quotedRole))
	requireClickHousePrivilegeCount(t, fx.conn, groupRole, "dictGet", fx.database, "d1", 1)

	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manual remediation")
	require.Contains(t, err.Error(), "wildcard grant")
	require.Contains(t, err.Error(), fx.database+".d1*")
	var status string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		"SELECT sync_status FROM warehouses WHERE id = $1", fx.warehouseID.String()).Scan(&status))
	require.Equal(t, "error", status)
}

// TestWarehouseDictionaryUserSubjectE2E proves a direct user grant (not just
// group roles) receives the dictionary bundle.
func TestWarehouseDictionaryUserSubjectE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)
	mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn)
	require.NoError(t, err, "probe the ClickHouse privilege mode")
	if mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q)", mode)
	}

	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("user", fx.memberID.String(), fx.database, "d1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	// Direct user grants are recorded on user_name, not through a role.
	memberIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.memberID)
	for _, access := range []string{"SELECT", "dictGet", "SHOW DICTIONARIES"} {
		requireClickHousePrivilegeCount(t, fx.conn, memberIdent, access, fx.database, "d1", 1)
	}

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT dictGet('"+fx.database+".d1', 'label', toUInt8(1)) AS label")
	out := decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])
}
