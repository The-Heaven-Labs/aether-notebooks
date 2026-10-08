package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePendingEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"lowercases", "Alice@Example.com", "alice@example.com", true},
		{"trims surrounding whitespace", "  alice@example.com  ", "alice@example.com", true},
		{"rejects missing domain", "alice@", "", false},
		{"rejects missing local part", "@example.com", "", false},
		{"rejects double at", "alice@@example.com", "", false},
		{"rejects inner whitespace", "ali ce@example.com", "", false},
		{"rejects empty", "", "", false},
		{"rejects plain name", "alice", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizePendingEmail(tc.in)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestApplyPendingAccessAuditsErrorsWithoutBlocking(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-audit")
	userID := insertSessionPermUser(t, s, "pending-audit-user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	tx, err := s.db.Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx)) // closed transaction: every Exec fails

	// The wrapper must swallow the failure (first login is never blocked) and
	// audit both error events.
	require.NotPanics(t, func() {
		s.applyPendingAccess(ctx, tx, orgID.String(), userID.String(), "future@example.com")
	})

	var n int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'acl.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "ACL materialization failure must be audited")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'warehouse.grant.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "warehouse materialization failure must be audited")
}

// TestApplyPendingAccessPhaseFailureKeepsOuterTxCommittable pins the savepoint
// isolation the wrapper relies on: a phase SQL error must not leave the caller's
// join transaction aborted. Read-only makes both INSERT statements fail
// server-side (SQLSTATE 25006), the shape of any materialization error; without
// per-phase savepoints the first failure would abort the transaction, the second
// phase would fail with "current transaction is aborted", and Commit would
// return pgx.ErrTxCommitRollback — rolling back a registration.
func TestApplyPendingAccessPhaseFailureKeepsOuterTxCommittable(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-commit")
	userID := insertSessionPermUser(t, s, "pending-commit-user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	tx, err := s.db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY")
	require.NoError(t, err)

	s.applyPendingAccess(ctx, tx, orgID.String(), userID.String(), "future@example.com")

	require.NoError(t, tx.Commit(ctx), "phase failures must leave the join transaction committable")

	var n int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'acl.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "ACL materialization failure must be audited")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'warehouse.grant.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "warehouse materialization failure must be audited")
}
