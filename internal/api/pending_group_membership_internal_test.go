package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApplyPendingGroupsPhaseFailureKeepsOuterTxCommittable pins the savepoint
// isolation of the group phase: a SQL error in ApplyPendingGroups must not
// leave the caller's join transaction aborted. Without the savepoint the
// following applyPendingAccess phases would fail at savepoint begin and the
// caller's commit would roll back (pgx.ErrTxCommitRollback), defeating the
// "failures never block first login" guarantee.
func TestApplyPendingGroupsPhaseFailureKeepsOuterTxCommittable(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-group-commit")
	userID := insertSessionPermUser(t, s, "pending-group-commit-user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	tx, err := s.db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	// Read-only makes the materialization INSERT fail server-side (SQLSTATE
	// 25006), aborting the transaction until a savepoint rollback clears it.
	_, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY")
	require.NoError(t, err)

	s.applyPendingGroups(ctx, tx, orgID.String(), userID.String(), "future@example.com")

	require.NoError(t, tx.Commit(ctx), "a group phase failure must leave the join transaction committable")

	var n int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'group.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "group materialization failure must be audited")
}
