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
	"github.com/the-heaven-labs/aether/internal/executor"
)

// disableCHTablePermissions flips the kill switch off for one test and
// restores the managed mode the shared fixtures assume afterwards.
func disableCHTablePermissions(t *testing.T, s *Server) {
	t.Helper()
	s.SetCHTablePermissions(false)
	t.Cleanup(func() { s.SetCHTablePermissions(true) })
}

// With the kill switch off, a managed connector must resolve as unmanaged so
// callers take the stored-credential path instead of the per-user identity.
func TestKillSwitchOffResolveExecutionTargetIsUnmanaged(t *testing.T) {
	s, key := sharedWarehouseTestServer(t)
	disableCHTablePermissions(t, s)

	fx := seedWarehouseFixtureRows(t, s, key)
	_, err := s.resolveExecutionTarget(context.Background(), fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrUnmanagedConnector)
}

// A disabled reconcile must not touch ClickHouse or rewrite sync bookkeeping,
// so a rollback leaves provisioned state alone until the switch is re-enabled.
func TestKillSwitchOffReconcileIsNoop(t *testing.T) {
	s, key := sharedWarehouseTestServer(t)
	disableCHTablePermissions(t, s)

	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready' WHERE id = $1`, fx.warehouseID.String())
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

// Every sync trigger is a no-op while the kill switch is off; the same calls
// enqueue normally once it is on again.
func TestKillSwitchControlsEnqueues(t *testing.T) {
	s, key := sharedWarehouseTestServer(t)
	disableCHTablePermissions(t, s)

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
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	rec := &warehouseEnqueueRecorder{}
	s.SetWarehouseSyncerForTest(rec)
	s.SetWarehouseReconcileInterval(time.Hour)

	oldJitter := warehouseReconcileJitterFn
	warehouseReconcileJitterFn = func(time.Duration) time.Duration { return 0 }
	t.Cleanup(func() { warehouseReconcileJitterFn = oldJitter })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.SetCHTablePermissions(false)
	s.startWarehouseReconcileLoop(ctx)
	require.Never(t, func() bool { return rec.contains(fx.warehouseID) },
		200*time.Millisecond, 20*time.Millisecond, "disabled loop must not enqueue")

	s.SetCHTablePermissions(true)
	s.startWarehouseReconcileLoop(ctx)
	require.Eventually(t, func() bool { return rec.contains(fx.warehouseID) },
		2*time.Second, 10*time.Millisecond, "enabled loop must enqueue the startup sweep")
}

// Admins must be able to stage warehouses and grants while the kill switch is
// off; only reconciliation and per-user execution are disabled.
func TestKillSwitchOffWarehouseCRUDStaysAvailable(t *testing.T) {
	s, key := sharedWarehouseTestServer(t)
	disableCHTablePermissions(t, s)

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
