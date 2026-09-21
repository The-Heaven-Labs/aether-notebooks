package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// newKillSwitchTestServer builds a dedicated Server on the test database with
// the kill switch off. Kill-switch cases never toggle the shared warehouse
// fixtures, so they do not depend on tests staying sequential.
func newKillSwitchTestServer(t *testing.T) (*Server, []byte) {
	t.Helper()
	s, key := newWarehouseSyncTestServer(t)
	s.SetCHTablePermissions(false)
	return s, key
}

// pointProvisionerAtUnreachableHost rewrites a connector's encrypted config so
// any ClickHouse connection attempt fails fast. It makes "did not touch
// ClickHouse" observable: a path that skipped the kill-switch gate would fail
// instead of leaving the warehouse untouched.
func pointProvisionerAtUnreachableHost(t *testing.T, s *Server, key []byte, connectorID uuid.UUID) {
	t.Helper()
	cfg := warehouseSyncTestClickHouseConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = 1
	plain, err := json.Marshal(cfg)
	require.NoError(t, err)
	encrypted, err := crypto.Encrypt(plain, key)
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET config_encrypted = $1 WHERE id = $2`,
		encrypted, connectorID.String())
	require.NoError(t, err)
}

// With the kill switch off, a managed connector must resolve as unmanaged so
// callers take the stored-credential path instead of the per-user identity.
func TestKillSwitchOffResolveExecutionTargetIsUnmanaged(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	_, err := s.resolveExecutionTarget(context.Background(), fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrUnmanagedConnector)
}

// The off-mode fallback must not skip connector validation: a soft-deleted
// managed connector is rejected, never executed with its stored credential.
func TestKillSwitchOffResolveRejectsDeletedConnector(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, fx.connectorID.String())
	require.NoError(t, err)

	_, err = s.resolveExecutionTarget(ctx, fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrConnectorNotFound)
	require.NotErrorIs(t, err, executor.ErrUnmanagedConnector)
}

// The same guard must hold when the agent's execute_sql tool drives the real
// server resolver: a soft-deleted connector fails "not found" instead of
// dialing its stored credential.
func TestKillSwitchOffAgentRejectsDeletedConnector(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, fx.connectorID.String())
	require.NoError(t, err)

	def, ok := s.agentEngine.GetRegistry().Get("execute_sql")
	require.True(t, ok, "execute_sql must be registered")
	args, err := json.Marshal(map[string]any{
		"connector_id": fx.connectorID.String(),
		"query":        "SELECT currentUser()",
	})
	require.NoError(t, err)

	tc := &agent.ToolContext{
		Context:             ctx,
		UserID:              fx.userID.String(),
		OrgID:               fx.orgID.String(),
		OrgRole:             "admin",
		DB:                  s.db.Pool,
		MasterKey:           s.masterKey,
		ResolveTarget:       s.resolveExecutionTarget,
		ConnPool:            s.connPool,
		CheckPermissionFunc: s.checkPermission,
	}
	_, err = def.Execute(args, tc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

// A disabled reconcile must not touch ClickHouse or rewrite sync bookkeeping.
// The provisioner points at an unreachable host, so any connection attempt
// would flip the warehouse to error instead of leaving it ready.
func TestKillSwitchOffReconcileIsNoop(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	pointProvisionerAtUnreachableHost(t, s, key, fx.connectorID)
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready', applied_master_fp = 'deadbeef' WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)

	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))

	var status string
	var syncErr *string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT sync_status, sync_error FROM warehouses WHERE id = $1`,
		fx.warehouseID.String()).Scan(&status, &syncErr))
	require.Equal(t, "ready", status, "disabled reconcile must not rewrite sync_status")
	require.Nil(t, syncErr)
}

// Drift detection is part of the managed path and stays dormant too.
func TestKillSwitchOffDriftDetectionIsNoop(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	report, err := s.detectWarehouseDrift(context.Background(), fx.warehouseID)
	require.NoError(t, err)
	require.True(t, report.IsEmpty())
}

// Every sync trigger is a no-op while the kill switch is off; the same calls
// enqueue normally once it is on again.
func TestKillSwitchControlsEnqueues(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	rec := &warehouseEnqueueRecorder{}
	s.SetWarehouseSyncerForTest(rec)
	ctx := context.Background()

	s.enqueueWarehouseSync(fx.warehouseID)
	s.enqueueWarehouseSyncNow(fx.warehouseID)
	s.enqueueWarehouseSyncForOrg(ctx, fx.orgID.String())
	s.enqueueWarehouseSyncForUser(ctx, fx.userID.String())
	s.enqueueWarehouseSyncForGroup(ctx, fx.groupID.String())
	require.False(t, rec.contains(fx.warehouseID), "disabled kill switch must drop every enqueue")

	s.SetCHTablePermissions(true)
	s.enqueueWarehouseSync(fx.warehouseID)
	require.True(t, rec.contains(fx.warehouseID), "enabled kill switch must enqueue again")
}

// The periodic catch-up loop must not start while the kill switch is off, and
// the same call must enqueue the startup sweep once it is on.
func TestKillSwitchControlsReconcileLoop(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	rec := &warehouseEnqueueRecorder{}
	s.SetWarehouseSyncerForTest(rec)
	s.SetWarehouseReconcileInterval(time.Hour)

	oldJitter := warehouseReconcileJitterFn
	warehouseReconcileJitterFn = func(time.Duration) time.Duration { return 0 }
	t.Cleanup(func() { warehouseReconcileJitterFn = oldJitter })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.startWarehouseReconcileLoop(ctx)
	require.Never(t, func() bool { return rec.contains(fx.warehouseID) },
		200*time.Millisecond, 20*time.Millisecond, "disabled loop must not enqueue")

	s.SetCHTablePermissions(true)
	s.startWarehouseReconcileLoop(ctx)
	require.Eventually(t, func() bool { return rec.contains(fx.warehouseID) },
		2*time.Second, 10*time.Millisecond, "enabled loop must enqueue the startup sweep")
}

// Admins must be able to stage warehouses and grants while the kill switch is
// off; only reconciliation, drift detection, and per-user execution are
// disabled.
func TestKillSwitchOffWarehouseCRUDStaysAvailable(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	rec := &warehouseEnqueueRecorder{}
	s.SetWarehouseSyncerForTest(rec)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	createBody, _ := json.Marshal(map[string]any{"name": "Kill Switch Staged " + uuid.NewString()[:8]})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	s.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	var created warehouseJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	t.Cleanup(func() {
		if _, err := s.db.Pool.Exec(context.Background(),
			`DELETE FROM warehouses WHERE id = $1`, created.ID); err != nil {
			t.Logf("cleanup staged warehouse: %v", err)
		}
	})
	require.Equal(t, "pending", created.SyncStatus)
	require.False(t, rec.contains(uuid.MustParse(created.ID)),
		"disabled kill switch must not enqueue the staged warehouse")

	grantBody, _ := json.Marshal(map[string]any{
		"subject_type": "user",
		"subject_id":   fx.userID.String(),
		"database":     "analytics",
		"table":        "staged_events",
	})
	grantReq := httptest.NewRequest(http.MethodPost, "/api/v1/warehouses/"+created.ID+"/grants", bytes.NewReader(grantBody))
	grantReq.Header.Set("Content-Type", "application/json")
	grantReq.Header.Set("Authorization", "Bearer "+token)
	grantRec := httptest.NewRecorder()
	s.ServeHTTP(grantRec, grantReq)
	require.Equal(t, http.StatusCreated, grantRec.Code, grantRec.Body.String())
}

// Deleting a warehouse with the kill switch off is DB-only: no provisioner is
// required, no ClickHouse connection is opened, and the skipped identity
// cleanup is audited as deferred. The provisioner config is unreachable and
// the warehouse looks provisioned, so a path that still dropped identities
// would return 503.
func TestKillSwitchOffDeleteDefersIdentityCleanup(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	pointProvisionerAtUnreachableHost(t, s, key, fx.connectorID)
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready', applied_master_fp = 'deadbeef' WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/warehouses/"+fx.warehouseID.String()+"?force=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	var exists bool
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM warehouses WHERE id = $1)`,
		fx.warehouseID.String()).Scan(&exists))
	require.False(t, exists, "warehouse row must be deleted")

	var deferred bool
	require.NoError(t, s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'deferred')::boolean, false) FROM audit_logs
		WHERE action = 'warehouse.identities.cleanup' AND resource_id = $1
		ORDER BY id DESC LIMIT 1`, fx.warehouseID.String()).Scan(&deferred))
	require.True(t, deferred, "skipped identity cleanup must be audited as deferred")
}
