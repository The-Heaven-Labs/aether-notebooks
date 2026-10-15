package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

func TestWarehouseProvisionerTest(t *testing.T) {
	s, key := sharedWarehouseTestServer(t)
	requireClickHouseReachable(t)
	fx := setupWarehouseFixtureWithServer(t, s, key)
	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+fx.warehouseID.String()+"/test", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var res warehouseProvisionerTestResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.True(t, res.OK, "expected successful test, got error: %v", res.Error)
	require.Len(t, res.Steps, 5)

	stepNames := make([]string, len(res.Steps))
	for i, step := range res.Steps {
		stepNames[i] = step.Name
		require.True(t, step.OK, "step %q failed: %v", step.Name, step.Error)
	}
	require.Equal(t, []string{"connect", "create user", "grant access", "verify grants", "remove test user"}, stepNames)

	prefix := chaccess.IdentifierPrefix(fx.warehouseID)
	pattern := prefix + "test_%"

	var usersLeft uint64
	require.NoError(t, fx.conn.QueryRow(context.Background(),
		"SELECT count() FROM system.users WHERE name LIKE ?", pattern).Scan(&usersLeft))
	require.Equal(t, uint64(0), usersLeft, "test users must be removed")

	var grantsLeft uint64
	require.NoError(t, fx.conn.QueryRow(context.Background(),
		"SELECT count() FROM system.grants WHERE user_name LIKE ?", pattern).Scan(&grantsLeft))
	require.Equal(t, uint64(0), grantsLeft, "test user grants must be removed")
}

func TestWarehouseProvisionerTestNoProvisioner(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "No Provisioner Test WH")

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+wh.String()+"/test", admin, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var res warehouseProvisionerTestResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.False(t, res.OK)
	require.NotNil(t, res.Error)
	require.Contains(t, *res.Error, "provisioner")
}

func TestWarehouseProvisionerTestUnknownWarehouse(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+uuid.NewString()+"/test", admin, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestWarehouseProvisionerTestForeignWarehouse(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, adminA := seedWarehouseOrgAdmin(t, s)
	_, _, adminB := seedWarehouseOrgAdmin(t, s)
	whA := createWarehouseViaAPI(t, s, adminA, "Foreign WH A")

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+whA.String()+"/test", adminB, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestWarehouseProvisionerTestBlockedBySyncLock(t *testing.T) {
	ctx := context.Background()
	requireClickHouseReachable(t)
	s, key := sharedWarehouseTestServer(t)
	fx := setupWarehouseFixtureWithServer(t, s, key)
	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	lockConn, err := pgx.Connect(ctx, warehouseSyncTestDSN())
	require.NoError(t, err)
	t.Cleanup(func() { lockConn.Close(context.Background()) })

	var locked bool
	require.NoError(t, lockConn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtextextended($1::text, 0))`, fx.warehouseID.String()).Scan(&locked))
	require.True(t, locked, "test must hold the warehouse advisory lock")

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+fx.warehouseID.String()+"/test", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var res warehouseProvisionerTestResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.False(t, res.OK)
	require.NotNil(t, res.Error)
	require.Contains(t, *res.Error, "sync in progress")

	_, err = lockConn.Exec(ctx,
		`SELECT pg_advisory_unlock(hashtextextended($1::text, 0))`, fx.warehouseID.String())
	require.NoError(t, err)

	// After releasing the lock the warehouse is testable.
	rec = warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+fx.warehouseID.String()+"/test", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	require.True(t, res.OK, "expected test to pass after lock release, got error: %v", res.Error)
}

func TestWarehouseProvisionerTestAudit(t *testing.T) {
	s, key := sharedWarehouseTestServer(t)
	requireClickHouseReachable(t)
	fx := setupWarehouseFixtureWithServer(t, s, key)
	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+fx.warehouseID.String()+"/test", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	meta := warehouseAuditMetadata(t, s, "warehouse.provisioner_test", fx.warehouseID.String())
	require.Equal(t, fx.warehouseID.String(), meta["warehouse_id"])
	require.Equal(t, true, meta["ok"])
}

func TestWarehouseProvisionerTestResultJSONShape(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Result Shape WH")

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses/"+wh.String()+"/test", admin, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Contains(t, raw, "ok")
	require.Contains(t, raw, "steps")
	require.IsType(t, []any{}, raw["steps"])
	for _, st := range raw["steps"].([]any) {
		step := st.(map[string]any)
		require.Contains(t, step, "name")
		require.Contains(t, step, "ok")
	}
}
