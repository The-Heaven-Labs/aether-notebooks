package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fingerprintFor resolves a connector's nullable warehouse link and computes
// the viewer's access fingerprint, mirroring how the dashboard query path will
// call the helper.
func fingerprintFor(t *testing.T, s *Server, orgID, userID, connectorID uuid.UUID) string {
	t.Helper()
	var warehouseID *string
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT warehouse_id FROM connectors WHERE id = $1`,
		connectorID.String()).Scan(&warehouseID))
	fp, err := s.dashboardAccessFingerprint(context.Background(), orgID.String(), userID.String(), warehouseID)
	require.NoError(t, err)
	return fp
}

// insertFingerprintGrant writes one warehouse table grant directly; the
// fingerprint reads the same resolution the sync worker does, so the rows are
// the whole input.
func insertFingerprintGrant(t *testing.T, s *Server, orgID, warehouseID uuid.UUID, subjectType, subjectID, database, table string) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		orgID.String(), warehouseID.String(), subjectType, subjectID, database, table)
	require.NoError(t, err)
}

// TestDashboardAccessFingerprint pins the shared-cache access fingerprint: two
// viewers share a fingerprint exactly when their effective warehouse grants
// are equal, regardless of which subject rows produced the set, and connectors
// that never execute under a per-user warehouse identity always report the
// constant "unmanaged" fingerprint.
func TestDashboardAccessFingerprint(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	ctx := context.Background()
	s := fx.s

	// The fixture seeds a direct user grant for fx.userID ('analytics.events')
	// and a group grant for fx.groupID ('analytics.daily_revenue'). userA is
	// that fixture user (effective set: events + daily_revenue); every other
	// user below pins one fingerprint property through its grant rows.
	userA := fx.userID

	userB, _ := seedGrantOrgMember(t, s, fx.orgID, "editor") // identical effective set
	userC, _ := seedGrantOrgMember(t, s, fx.orgID, "editor") // one table more
	userD, _ := seedGrantOrgMember(t, s, fx.orgID, "editor") // group grant must not reach
	userE, _ := seedGrantOrgMember(t, s, fx.orgID, "editor") // same count, different tables
	userF, _ := seedGrantOrgMember(t, s, fx.orgID, "editor") // same set, different subject rows

	for _, user := range []uuid.UUID{userB, userC} {
		_, err := s.db.Pool.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
			fx.groupID.String(), user.String())
		require.NoError(t, err)
	}
	// B mirrors A's effective set through the same group plus its own direct
	// row, so both the group and direct branches must produce one fingerprint.
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userB.String(), "analytics", "events")
	// C sees one table more than A.
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userC.String(), "analytics", "events")
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userC.String(), "analytics", "extra")
	// D gets A's direct table but none of the group's.
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userD.String(), "analytics", "events")
	// E has the same effective-set size as A with disjoint table names; a
	// count-only hash would wrongly share its fingerprint.
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userE.String(), "analytics", "other1")
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userE.String(), "analytics", "other2")
	// F reaches A's exact table set entirely through direct rows; a hash that
	// mixes in subject identity would wrongly split the two.
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userF.String(), "analytics", "events")
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userF.String(), "analytics", "daily_revenue")

	fpA := fingerprintFor(t, s, fx.orgID, userA, fx.connA)
	fpB := fingerprintFor(t, s, fx.orgID, userB, fx.connA)
	fpC := fingerprintFor(t, s, fx.orgID, userC, fx.connA)
	fpD := fingerprintFor(t, s, fx.orgID, userD, fx.connA)
	fpE := fingerprintFor(t, s, fx.orgID, userE, fx.connA)
	fpF := fingerprintFor(t, s, fx.orgID, userF, fx.connA)

	require.Equal(t, fpA, fpB, "identical effective grants must share a fingerprint")
	require.NotEqual(t, fpA, fpC, "an extra table must change the fingerprint")
	require.NotEqual(t, fpA, fpD, "a group grant must not reach a non-member")
	require.NotEqual(t, fpA, fpE, "same grant count with different tables must not share a fingerprint")
	require.Equal(t, fpA, fpF, "the same effective set via different subject rows must share a fingerprint")

	// Determinism: the same viewer with unchanged grants hashes identically.
	require.Equal(t, fpA, fingerprintFor(t, s, fx.orgID, userA, fx.connA))

	// Unmanaged connectors execute with a shared stored credential.
	require.Equal(t, dashboardFingerprintUnmanaged, fingerprintFor(t, s, fx.orgID, userA, fx.unmanagedID))

	// With warehouse management disabled every connector behaves unmanaged.
	// The shared test server is reached only by sequential tests in this
	// package, and no background jobs run against it here, so toggling the
	// process-start flag and restoring it is safe; the cleanup also restores
	// the prior value when an assertion fails inside the window.
	prevManagement := s.warehouseManagementEnabled()
	s.SetCHTablePermissions(false)
	t.Cleanup(func() { s.SetCHTablePermissions(prevManagement) })
	require.Equal(t, dashboardFingerprintUnmanaged, fingerprintFor(t, s, fx.orgID, userA, fx.connA))
	s.SetCHTablePermissions(prevManagement)

	// A malformed warehouse id is an error (never a panic): the caller's
	// signal to fall back to a per-viewer cache key.
	badWarehouseID := "not-a-uuid"
	fp, err := s.dashboardAccessFingerprint(ctx, fx.orgID.String(), userA.String(), &badWarehouseID)
	require.Error(t, err)
	require.Empty(t, fp)
}

// TestDashboardQueryCacheKeyFingerprint pins that the shared-cache key is
// derived from the access fingerprint rather than the viewer identity:
// different viewers with the same fingerprint share a key, and changing only
// the fingerprint changes the key. An empty SQL never produces a key.
func TestDashboardQueryCacheKeyFingerprint(t *testing.T) {
	base := dashboardQueryParams{
		OrgID:             "org",
		Identity:          dashboardIdentity{UserID: "user-a"},
		ConnectorID:       "connector",
		SQL:               "SELECT 1",
		AccessFingerprint: "fp",
	}
	otherViewer := base
	otherViewer.Identity.UserID = "user-b"
	require.Equal(t, dashboardQueryCacheKey(base), dashboardQueryCacheKey(otherViewer),
		"viewer identity must not affect the key")

	otherAccess := base
	otherAccess.AccessFingerprint = "fp2"
	require.NotEqual(t, dashboardQueryCacheKey(base), dashboardQueryCacheKey(otherAccess),
		"the access fingerprint must affect the key")

	require.Empty(t, dashboardQueryCacheKey(dashboardQueryParams{}), "empty SQL must not produce a key")
}

// TestDashboardQueryAccessFingerprint pins the cache-sharing decision: only a
// successfully resolved effective-access fingerprint is shared. Public runs
// are discriminated by token scope, viewers without `use` on the served
// connector and runs whose fingerprint cannot be computed fall back to a
// per-user key (fail closed), and an unmanaged connector reports the constant
// shared value.
func TestDashboardQueryAccessFingerprint(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	// The fixture user may execute on connA; connB stays denied.
	fx.grantUse(t, fx.connA)

	base := dashboardQueryParams{
		OrgID:       fx.orgID.String(),
		Identity:    dashboardIdentity{UserID: fx.userID.String(), Role: "admin"},
		ConnectorID: fx.connA.String(),
	}
	userFallback := "user:" + fx.userID.String()

	// A malformed warehouse id makes the managed fingerprint resolution fail;
	// the run must fall back to the per-user key. The original implementation
	// pre-assigned the constant "unmanaged" before calling the helper, so this
	// error path kept a shared key.
	badWarehouse := "not-a-uuid"
	invalid := base
	invalid.WarehouseID = &badWarehouse
	require.Equal(t, userFallback, fx.s.dashboardQueryAccessFingerprint(context.Background(), invalid))

	// An unmanaged connector executes with a shared stored credential, so its
	// fingerprint is the constant shared value.
	unmanaged := base
	unmanaged.WarehouseID = nil
	require.Equal(t, dashboardFingerprintUnmanaged, fx.s.dashboardQueryAccessFingerprint(context.Background(), unmanaged))

	// Without `use` on the served connector the viewer could not execute, so
	// the key must stay private even though the connector is shareable.
	denied := base
	denied.ConnectorID = fx.connB.String()
	require.Equal(t, userFallback, fx.s.dashboardQueryAccessFingerprint(context.Background(), denied))

	// Public runs are discriminated by the token scope.
	public := base
	public.CacheScope = "token:t"
	require.Equal(t, "public", fx.s.dashboardQueryAccessFingerprint(context.Background(), public))
}
