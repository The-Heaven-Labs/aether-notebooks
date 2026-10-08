package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
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
// It returns the number of real grant rows inserted: rows deduped by DO
// NOTHING are consumed but not counted.
func applyPendingWarehouseGrants(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH consumed AS (
			DELETE FROM pending_warehouse_table_grants
			WHERE org_id = $1 AND lower(email) = lower($3)
			RETURNING org_id, warehouse_id, database_name, table_name, created_by
		)
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
		SELECT org_id, warehouse_id, 'user', $2, database_name, table_name, created_by
		FROM consumed
		ON CONFLICT (warehouse_id, subject_type, subject_id, database_name, table_name) DO NOTHING`,
		orgID, userID, email)
	if err != nil {
		return 0, fmt.Errorf("materialize pending warehouse grants: %w", err)
	}
	return tag.RowsAffected(), nil
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
