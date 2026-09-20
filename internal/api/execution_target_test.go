package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// executionTargetFixture seeds a ready warehouse with two service connectors
// ("Service A", "Service B") plus one unmanaged connector. No service carries
// a `use` grant until a test grants one, so each test controls the routing
// inputs exactly.
type executionTargetFixture struct {
	s             *Server
	key           []byte
	orgID         uuid.UUID
	userID        uuid.UUID
	groupID       uuid.UUID
	warehouseID   uuid.UUID
	provisionerID uuid.UUID
	connA         uuid.UUID
	connB         uuid.UUID
	unmanagedID   uuid.UUID
}

// setupExecutionTargetFixture reuses the warehouse sync row seeder, so
// resolution tests need only Postgres (no ClickHouse).
func setupExecutionTargetFixture(t *testing.T) *executionTargetFixture {
	t.Helper()
	ctx := context.Background()

	s, key := newWarehouseSyncTestServer(t)
	seed := seedWarehouseFixtureRows(t, s, key)

	var encrypted []byte
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT config_encrypted FROM connectors WHERE id = $1`,
		seed.connectorID.String()).Scan(&encrypted))

	insertConnector := func(name string, warehouseID *uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		var wh any
		if warehouseID != nil {
			wh = warehouseID.String()
		}
		_, err := s.db.Pool.Exec(ctx, `
			INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
			VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
			id.String(), seed.orgID.String(), name, encrypted, wh)
		require.NoError(t, err)
		return id
	}

	fx := &executionTargetFixture{
		s:             s,
		key:           key,
		orgID:         seed.orgID,
		userID:        seed.userID,
		groupID:       seed.groupID,
		warehouseID:   seed.warehouseID,
		provisionerID: seed.connectorID,
		connA:         insertConnector("Service A", &seed.warehouseID),
		connB:         insertConnector("Service B", &seed.warehouseID),
		unmanagedID:   insertConnector("Unmanaged", nil),
	}
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready' WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)
	return fx
}

// grantUse gives the fixture user the `use` action on one connector.
func (fx *executionTargetFixture) grantUse(t *testing.T, connectorID uuid.UUID) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'user', $3, ARRAY['view','use'])`,
		fx.orgID.String(), connectorID.String(), fx.userID.String())
	require.NoError(t, err)
}

// grantGroupUse gives the fixture group the `use` action on one connector.
func (fx *executionTargetFixture) grantGroupUse(t *testing.T, connectorID uuid.UUID) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'group', $3, ARRAY['use'])`,
		fx.orgID.String(), connectorID.String(), fx.groupID.String())
	require.NoError(t, err)
}

// revokeUse removes the fixture user's direct `use` grant on a connector.
func (fx *executionTargetFixture) revokeUse(t *testing.T, connectorID uuid.UUID) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		DELETE FROM acl_entries
		WHERE org_id = $1 AND resource_type = 'connector' AND resource_id = $2::uuid
		  AND subject_type = 'user' AND subject_id = $3`,
		fx.orgID.String(), connectorID.String(), fx.userID.String())
	require.NoError(t, err)
}

// prefer records the user's routing preference for the warehouse.
func (fx *executionTargetFixture) prefer(t *testing.T, connectorID uuid.UUID) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_service_preferences (user_id, warehouse_id, connector_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, warehouse_id) DO UPDATE
		SET connector_id = EXCLUDED.connector_id, updated_at = now()`,
		fx.userID.String(), fx.warehouseID.String(), connectorID.String())
	require.NoError(t, err)
}

func (fx *executionTargetFixture) setSyncStatus(t *testing.T, status string) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE warehouses SET sync_status = $1 WHERE id = $2`,
		status, fx.warehouseID.String())
	require.NoError(t, err)
}

func (fx *executionTargetFixture) resolve(t *testing.T, requested uuid.UUID, pinned bool) (*executor.ExecutionTarget, error) {
	t.Helper()
	return fx.s.resolveExecutionTarget(context.Background(), fx.userID, requested, pinned)
}

func TestResolveExecutionTargetUnmanagedConnector(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.unmanagedID)

	for _, pinned := range []bool{false, true} {
		target, err := fx.resolve(t, fx.unmanagedID, pinned)
		require.ErrorIs(t, err, executor.ErrUnmanagedConnector, "pinned=%v", pinned)
		require.Nil(t, target)
	}
}

func TestResolveExecutionTargetNotReadyFailsClosed(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)

	for _, status := range []string{"pending", "syncing", "error"} {
		t.Run(status, func(t *testing.T) {
			fx.setSyncStatus(t, status)
			target, err := fx.resolve(t, fx.connA, false)
			require.ErrorIs(t, err, executor.ErrProvisioningNotReady)
			require.Nil(t, target)

			target, err = fx.resolve(t, fx.connA, true)
			require.ErrorIs(t, err, executor.ErrProvisioningNotReady, "pins must not bypass readiness")
			require.Nil(t, target)
		})
	}
}

func TestResolveExecutionTargetRequiresServiceAccess(t *testing.T) {
	fx := setupExecutionTargetFixture(t)

	target, err := fx.resolve(t, fx.provisionerID, false)
	require.ErrorIs(t, err, executor.ErrServiceAccessDenied)
	require.Nil(t, target)
}

func TestResolveExecutionTargetUsesPreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	target, err := fx.resolve(t, fx.connA, false)
	require.NoError(t, err)
	require.Equal(t, fx.connB, target.ConnectorID)
}

func TestResolveExecutionTargetFallsBackToSoleService(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connB)

	// The requested connector needs no grant of its own: the warehouse's sole
	// permitted service is used when nothing is pinned.
	target, err := fx.resolve(t, fx.connA, false)
	require.NoError(t, err)
	require.Equal(t, fx.connB, target.ConnectorID)
}

func TestResolveExecutionTargetHonorsPin(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	target, err := fx.resolve(t, fx.connA, true)
	require.NoError(t, err)
	require.Equal(t, fx.connA, target.ConnectorID, "a pin must override the stored preference")

	fx.revokeUse(t, fx.connA)
	target, err = fx.resolve(t, fx.connA, true)
	require.ErrorIs(t, err, executor.ErrServiceAccessDenied)
	require.Nil(t, target, "a pin without use must not fall back to another service")

	// Without the pin the warehouse still routes through the permitted service.
	target, err = fx.resolve(t, fx.connA, false)
	require.NoError(t, err)
	require.Equal(t, fx.connB, target.ConnectorID)
}

func TestResolveExecutionTargetAmbiguousWithoutPreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)

	target, err := fx.resolve(t, fx.connA, false)
	require.ErrorIs(t, err, executor.ErrServiceChoiceRequired)
	require.Nil(t, target)

	var choice *executor.ServiceChoiceError
	require.ErrorAs(t, err, &choice)
	require.Equal(t, fx.warehouseID, choice.WarehouseID)
	require.ElementsMatch(t, []executor.ServiceChoice{
		{ConnectorID: fx.connA, Name: "Service A"},
		{ConnectorID: fx.connB, Name: "Service B"},
	}, choice.Allowed)
}

func TestResolveExecutionTargetExcludesSoftDeletedConnector(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connA)

	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, fx.connA.String())
	require.NoError(t, err)

	// The stale preference and the soft-deleted connector's `use` grant must
	// both be ignored, leaving Service B as the sole permitted service.
	target, err := fx.resolve(t, fx.connB, false)
	require.NoError(t, err)
	require.Equal(t, fx.connB, target.ConnectorID)

	// Requesting the soft-deleted connector is a miss, never a fallback.
	target, err = fx.resolve(t, fx.connA, false)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Nil(t, target)
}

func TestResolveExecutionTargetIdentityAndEndpoint(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connB)

	target, err := fx.resolve(t, fx.connB, false)
	require.NoError(t, err)
	require.Equal(t, fx.warehouseID, target.WarehouseID)
	require.Equal(t, fx.connB, target.ConnectorID)
	require.Equal(t, "Service B", target.ConnectorName)
	require.Equal(t, "localhost:9000", target.Endpoint)
	require.Equal(t, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID), target.CHUser)
	require.Equal(t, chaccess.DerivePassword(fx.key, fx.warehouseID, fx.userID), target.Password)

	// The target carries a usable config with the per-user credentials
	// substituted for the connector's stored ones.
	require.Equal(t, "localhost", target.Config.Host)
	require.Equal(t, 9000, target.Config.Port)
	require.Equal(t, "analytics", target.Config.Database)
	require.Equal(t, target.CHUser, target.Config.User)
	require.Equal(t, target.Password, target.Config.Password)
	require.NotEqual(t, "dev", target.Config.Password, "the stored connector credential must not leak")
}

func TestResolveExecutionTargetGroupGrantForViewer(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE org_members SET role = 'non-admin' WHERE org_id = $1 AND user_id = $2`,
		fx.orgID.String(), fx.userID.String())
	require.NoError(t, err)
	fx.grantGroupUse(t, fx.connB)

	target, err := fx.resolve(t, fx.connA, false)
	require.NoError(t, err, "a viewer's group ACL must authorize warehouse routing")
	require.Equal(t, fx.connB, target.ConnectorID)
}

func TestResolveExecutionTargetRejectsNonMember(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connB)

	_, err := fx.s.db.Pool.Exec(context.Background(),
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		fx.orgID.String(), fx.userID.String())
	require.NoError(t, err)

	target, err := fx.resolve(t, fx.connA, false)
	require.ErrorIs(t, err, executor.ErrServiceAccessDenied)
	require.Nil(t, target)
}

func TestResolveExecutionTargetRejectsCrossOrgWarehouseConnector(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	ctx := context.Background()

	// A connector injected outside the CRUD validation may not borrow another
	// org's warehouse, regardless of either org's ACL entries.
	otherOrgID := uuid.New()
	_, err := fx.s.db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		otherOrgID.String(), "Other Org", "other-"+uuid.NewString())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := fx.s.db.Pool.Exec(cleanupCtx,
			`DELETE FROM orgs WHERE id = $1`, otherOrgID.String()); err != nil {
			t.Logf("cleanup other org: %v", err)
		}
	})

	foreignID := uuid.New()
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
		VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
		foreignID.String(), otherOrgID.String(), "Foreign Service",
		[]byte("unused"), fx.warehouseID.String())
	require.NoError(t, err)

	target, err := fx.resolve(t, foreignID, false)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Nil(t, target)

	// The foreign row must not leak into the warehouse's service list either:
	// a sole permitted service still resolves unambiguously.
	fx.grantUse(t, fx.connB)
	target, err = fx.resolve(t, fx.connB, false)
	require.NoError(t, err)
	require.Equal(t, fx.connB, target.ConnectorID)
}
