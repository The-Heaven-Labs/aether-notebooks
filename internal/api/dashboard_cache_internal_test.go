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
// are equal, and connectors that never execute under a per-user warehouse
// identity always report the constant "unmanaged" fingerprint.
func TestDashboardAccessFingerprint(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	ctx := context.Background()
	s := fx.s

	// The fixture seeds a direct user grant for fx.userID ('analytics.events')
	// and a group grant for fx.groupID ('analytics.daily_revenue'). The new
	// members below join that group, so their access flows through the same
	// grant row.
	userA, _ := seedGrantOrgMember(t, s, fx.orgID, "editor")
	userB, _ := seedGrantOrgMember(t, s, fx.orgID, "editor")
	userC, _ := seedGrantOrgMember(t, s, fx.orgID, "editor")
	userD, _ := seedGrantOrgMember(t, s, fx.orgID, "editor")
	for _, user := range []uuid.UUID{userA, userB, userC} {
		_, err := s.db.Pool.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
			fx.groupID.String(), user.String())
		require.NoError(t, err)
	}
	// C is a group peer that additionally sees one more table.
	insertFingerprintGrant(t, s, fx.orgID, fx.warehouseID, "user", userC.String(), "analytics", "extra")

	fpA := fingerprintFor(t, s, fx.orgID, userA, fx.connA)
	fpB := fingerprintFor(t, s, fx.orgID, userB, fx.connA)
	fpC := fingerprintFor(t, s, fx.orgID, userC, fx.connA)
	fpD := fingerprintFor(t, s, fx.orgID, userD, fx.connA)

	require.Equal(t, fpA, fpB, "identical effective grants must share a fingerprint")
	require.NotEqual(t, fpA, fpC, "different grants must not share a fingerprint")
	require.NotEqual(t, fpA, fpD, "a group grant must not reach a non-member")

	// Determinism: the same viewer with unchanged grants hashes identically.
	require.Equal(t, fpA, fingerprintFor(t, s, fx.orgID, userA, fx.connA))

	// Unmanaged connectors execute with a shared stored credential.
	require.Equal(t, dashboardFingerprintUnmanaged, fingerprintFor(t, s, fx.orgID, userA, fx.unmanagedID))

	// With warehouse management disabled every connector behaves unmanaged.
	// The shared test server is reached only by sequential tests in this
	// package, and no background jobs run against it here, so toggling the
	// process-start flag and restoring it is safe; the cleanup also restores
	// it when an assertion fails inside the window.
	s.SetCHTablePermissions(false)
	t.Cleanup(func() { s.SetCHTablePermissions(true) })
	require.Equal(t, dashboardFingerprintUnmanaged, fingerprintFor(t, s, fx.orgID, userA, fx.connA))
	s.SetCHTablePermissions(true)

	// A malformed warehouse id is an error (never a panic): the caller's
	// signal to fall back to a per-viewer cache key.
	badWarehouseID := "not-a-uuid"
	fp, err := s.dashboardAccessFingerprint(ctx, fx.orgID.String(), userA.String(), &badWarehouseID)
	require.Error(t, err)
	require.Empty(t, fp)
}
