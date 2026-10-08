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
// with extra actions, matching the read-only session-share rule. It returns
// the number of real ACL rows inserted or updated.
func applyPendingACL(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		SELECT pae.org_id, pae.resource_type, pae.resource_id, 'user', $2,
		       CASE WHEN pae.resource_type = 'agent_session'
		            THEN ARRAY(SELECT DISTINCT a FROM unnest(pae.actions) AS a WHERE a = 'view')
		            ELSE pae.actions END
		FROM pending_acl_entries pae
		WHERE pae.org_id = $1 AND lower(pae.email) = lower($3)
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = (
			SELECT ARRAY(SELECT DISTINCT unnest(acl_entries.actions || EXCLUDED.actions))
		)`, orgID, userID, email)
	if err != nil {
		return 0, fmt.Errorf("materialize pending ACL entries: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_acl_entries WHERE org_id = $1 AND lower(email) = lower($2)`,
		orgID, email); err != nil {
		return 0, fmt.Errorf("consume pending ACL entries: %w", err)
	}
	return tag.RowsAffected(), nil
}

// applyPendingWarehouseGrants materializes pending warehouse table grants
// staged for email as real user grants and consumes them. An existing grant
// for the same (warehouse, user, database, table) dedupes with DO NOTHING. It
// returns the number of real grant rows inserted.
func applyPendingWarehouseGrants(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
		SELECT pgt.org_id, pgt.warehouse_id, 'user', $2, pgt.database_name, pgt.table_name, pgt.created_by
		FROM pending_warehouse_table_grants pgt
		WHERE pgt.org_id = $1 AND lower(pgt.email) = lower($3)
		ON CONFLICT (warehouse_id, subject_type, subject_id, database_name, table_name) DO NOTHING`,
		orgID, userID, email)
	if err != nil {
		return 0, fmt.Errorf("materialize pending warehouse grants: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_warehouse_table_grants WHERE org_id = $1 AND lower(email) = lower($2)`,
		orgID, email); err != nil {
		return 0, fmt.Errorf("consume pending warehouse grants: %w", err)
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
