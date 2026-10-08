package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
)

// normalizePendingEmail canonicalizes an email staged for a not-yet-registered
// user: trimmed, lowercased, required to have exactly one "@" with a
// non-empty local part and domain, and no whitespace. The lowercased form is
// what the pending tables' lower(email) indexes and all lookups use.
func normalizePendingEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if strings.ContainsAny(email, " \t\n\r") {
		return "", false
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || strings.Contains(domain, "@") {
		return "", false
	}
	if local == "" || domain == "" {
		return "", false
	}
	return email, true
}

// applyPendingACL materializes pending ACL entries staged for email as real
// user entries for userID and consumes them. Staged actions are unioned into
// any existing direct entry (never a downgrade); agent_session rows are
// defensively re-constrained to view-only even if a staged row was written
// with extra actions, matching the read-only session-share rule.
//
// Materialization and consumption are one data-modifying CTE so both halves
// share a snapshot: a grant staged concurrently after this statement's
// snapshot stays staged instead of being consumed unmaterialized (the DELETE
// of a two-statement version would see it under READ COMMITTED).
//
// It returns the number of consumed pending rows: each one inserts or updates
// exactly one real ACL row, and DO UPDATE affects every conflict.
func applyPendingACL(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH consumed AS (
			DELETE FROM pending_acl_entries
			WHERE org_id = $1 AND lower(email) = lower($3)
			RETURNING org_id, resource_type, resource_id, actions
		)
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		SELECT org_id, resource_type, resource_id, 'user', $2,
		       CASE WHEN resource_type = 'agent_session'
		            THEN ARRAY(SELECT DISTINCT a FROM unnest(actions) AS a WHERE a = 'view' ORDER BY 1)
		            ELSE actions END
		FROM consumed
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = (
			SELECT ARRAY(SELECT DISTINCT unnest(acl_entries.actions || EXCLUDED.actions) ORDER BY 1)
		)`, orgID, userID, email)
	if err != nil {
		return 0, fmt.Errorf("materialize pending ACL entries: %w", err)
	}
	return tag.RowsAffected(), nil
}

// applyPendingWarehouseGrants materializes pending warehouse table grants
// staged for email as real user grants and consumes them. An existing grant
// for the same (warehouse, user, database, table) dedupes with DO NOTHING.
//
// Like applyPendingACL, the delete and insert share one statement snapshot so
// a concurrently staged grant cannot be consumed unmaterialized.
//
// It returns the number of consumed pending rows — including rows deduped by
// DO NOTHING — so the success audit reports everything that was materialized
// away, consistent with applyPendingACL.
func applyPendingWarehouseGrants(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	var consumed int64
	err := tx.QueryRow(ctx, `
		WITH consumed AS (
			DELETE FROM pending_warehouse_table_grants
			WHERE org_id = $1 AND lower(email) = lower($3)
			RETURNING org_id, warehouse_id, database_name, table_name, created_by
		),
		inserted AS (
			INSERT INTO warehouse_table_grants
				(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
			SELECT org_id, warehouse_id, 'user', $2, database_name, table_name, created_by
			FROM consumed
			ON CONFLICT (warehouse_id, subject_type, subject_id, database_name, table_name) DO NOTHING
			RETURNING 1
		)
		SELECT count(*) FROM consumed`,
		orgID, userID, email).Scan(&consumed)
	if err != nil {
		return 0, fmt.Errorf("materialize pending warehouse grants: %w", err)
	}
	return consumed, nil
}

// ApplyPendingAccess materializes both pending ACL entries and pending
// warehouse table grants staged for email into real rows for a user that just
// appeared in orgID, then consumes the staged rows. It mirrors
// ApplyPendingGroups: org-isolated (org_id + lower(email)), case-insensitive,
// idempotent, and it must run inside the same transaction that creates the org
// membership and home folder.
func ApplyPendingAccess(ctx context.Context, tx pgx.Tx, orgID, userID, email string) error {
	if _, err := applyPendingACL(ctx, tx, orgID, userID, email); err != nil {
		return err
	}
	_, err := applyPendingWarehouseGrants(ctx, tx, orgID, userID, email)
	return err
}

// runPendingPhase executes one pending-materialization phase — pending groups,
// pending ACL entries, or pending warehouse grants — inside a savepoint of the
// caller's transaction and returns the phase's row count. On error it rolls
// back to the savepoint so the failure cannot abort the outer transaction —
// without it a phase SQL error would leave the transaction aborted, the
// caller's commit would roll back (pgx.ErrTxCommitRollback), and first login
// would be blocked. pgx implements Tx.Begin on a transaction as SAVEPOINT,
// Rollback as ROLLBACK TO SAVEPOINT, and Commit as RELEASE SAVEPOINT.
func runPendingPhase(ctx context.Context, tx pgx.Tx, fn func(pgx.Tx) (int64, error)) (int64, error) {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin savepoint: %w", err)
	}
	count, err := fn(sp)
	if err != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return 0, fmt.Errorf("%w (savepoint rollback failed: %v)", err, rbErr)
		}
		return 0, err
	}
	if err := sp.Commit(ctx); err != nil {
		return 0, fmt.Errorf("release savepoint: %w", err)
	}
	return count, nil
}

// applyPendingAccess runs the pending ACL and warehouse-grant materializers and
// audits the outcome without failing — a materialization error must never
// block first login, so call sites keep committing their transaction
// regardless. Each phase runs in its own savepoint (see runPendingPhase), so a
// failing phase is rolled back alone and the outer join transaction stays
// committable. Success emits acl.pending_materialize /
// warehouse.grant.pending_materialize with the materialized row count; each
// phase's failure emits its own .error event.
func (s *Server) applyPendingAccess(ctx context.Context, tx pgx.Tx, orgID, userID, email string) {
	// Audit writes are detached from request cancellation so a cancelled
	// context cannot silently drop a materialization outcome record.
	auditCtx := context.WithoutCancel(ctx)

	aclCount, aclErr := runPendingPhase(ctx, tx, func(phaseTx pgx.Tx) (int64, error) {
		return applyPendingACL(ctx, phaseTx, orgID, userID, email)
	})
	if aclErr != nil {
		s.audit.Log(auditCtx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "acl.pending_materialize.error", ResourceType: "acl",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"error":   aclErr.Error(),
			},
		})
	} else if aclCount > 0 {
		s.audit.Log(auditCtx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "acl.pending_materialize", ResourceType: "acl",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"count":   aclCount,
			},
		})
	}

	grantCount, grantErr := runPendingPhase(ctx, tx, func(phaseTx pgx.Tx) (int64, error) {
		return applyPendingWarehouseGrants(ctx, phaseTx, orgID, userID, email)
	})
	if grantErr != nil {
		s.audit.Log(auditCtx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "warehouse.grant.pending_materialize.error", ResourceType: "warehouse",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"error":   grantErr.Error(),
			},
		})
	} else if grantCount > 0 {
		s.audit.Log(auditCtx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "warehouse.grant.pending_materialize", ResourceType: "warehouse",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"count":   grantCount,
			},
		})
	}
}
