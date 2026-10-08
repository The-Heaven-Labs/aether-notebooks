package api

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
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
		{"trims surrounding tabs and newlines", "\talice@example.com\n", "alice@example.com", true},
		{"rejects missing domain", "alice@", "", false},
		{"rejects missing local part", "@example.com", "", false},
		{"rejects double at", "alice@@example.com", "", false},
		{"rejects inner whitespace", "ali ce@example.com", "", false},
		{"rejects NUL", "ali\x00ce@example.com", "", false},
		{"rejects non-breaking space", "ali\u00a0ce@example.com", "", false},
		{"rejects zero-width space", "ali\u200bce@example.com", "", false},
		{"rejects BOM", "\ufeffalice@example.com", "", false},
		{"rejects right-to-left override", "ali\u202ece@example.com", "", false},
		{"rejects over-length email", strings.Repeat("a", 321) + "@example.com", "", false},
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

// TestApplyPendingAccessAuditsErrorsWithCancelledContext pins the durable audit
// writes: the request context is already cancelled, so every phase fails
// before doing SQL, yet the .error records must still land because audit
// writes detach from cancellation. A closed transaction keeps the phase
// failure deterministic (ErrTxClosed is checked before the context is used).
func TestApplyPendingAccessAuditsErrorsWithCancelledContext(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	orgID := insertSessionPermOrg(t, s, "pending-cancel")
	userID := insertSessionPermUser(t, s, "pending-cancel-user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tx, err := s.db.Pool.Begin(context.Background())
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(context.Background())) // closed transaction

	s.applyPendingAccess(ctx, tx, orgID.String(), userID.String(), "future@example.com")

	var n int
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'acl.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "a cancelled request context must not drop the ACL error audit")
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'warehouse.grant.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "a cancelled request context must not drop the warehouse error audit")
}

// TestApplyPendingAccessAuditsConsumedWarehouseGrantCount pins the audit count
// semantics: a staged grant deduped by ON CONFLICT DO NOTHING is still
// consumed, so the success audit reports everything materialized away, not
// just the inserted rows.
func TestApplyPendingAccessAuditsConsumedWarehouseGrantCount(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-wh-count")
	userID := insertSessionPermUser(t, s, "pending-wh-count-user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	whID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, 'Pending WH Count')`,
		whID.String(), orgID.String())
	require.NoError(t, err)

	// One staged grant dedupes against a real grant; one is new.
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, 'analytics', 'events')`,
		orgID.String(), whID.String(), userID.String())
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO pending_warehouse_table_grants (org_id, warehouse_id, email, database_name, table_name)
		VALUES ($1, $2, 'future@example.com', 'analytics', 'events'),
		       ($1, $2, 'future@example.com', 'analytics', 'users')`,
		orgID.String(), whID.String())
	require.NoError(t, err)

	tx, err := s.db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	s.applyPendingAccess(ctx, tx, orgID.String(), userID.String(), "future@example.com")
	require.NoError(t, tx.Commit(ctx))

	var audited string
	require.NoError(t, s.db.Pool.QueryRow(ctx, `
		SELECT metadata->>'count' FROM audit_logs
		WHERE org_id = $1 AND action = 'warehouse.grant.pending_materialize'`,
		orgID.String()).Scan(&audited))
	require.Equal(t, "2", audited, "deduped staged grants are consumed and must be counted")

	var grants, pending int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1 AND subject_id = $2`,
		whID.String(), userID.String()).Scan(&grants))
	require.Equal(t, 2, grants)
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`,
		whID.String()).Scan(&pending))
	require.Zero(t, pending)
}
