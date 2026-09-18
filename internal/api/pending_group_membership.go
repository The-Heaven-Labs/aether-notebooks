package api

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
)

// ApplyPendingGroups materializes pending group memberships staged for an email
// into real group_members rows for a user that just appeared in orgID, then
// consumes the pending rows. It must run inside the same transaction that
// creates the org membership and home folder.
//
// Semantics: org-isolated (matching is org_id + lower(email)), case-insensitive,
// and idempotent — ON CONFLICT DO NOTHING means a user already in a group does
// not get a duplicate row, and the pending row is consumed either way.
func ApplyPendingGroups(ctx context.Context, tx pgx.Tx, orgID, userID, email string) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id)
		 SELECT pgm.group_id, $2
		 FROM pending_group_members pgm
		 JOIN groups g ON g.id = pgm.group_id
		 WHERE pgm.org_id = $1 AND lower(pgm.email) = lower($3)
		 ON CONFLICT DO NOTHING`, orgID, userID, email); err != nil {
		return fmt.Errorf("materialize pending groups: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_group_members
		 WHERE org_id = $1 AND lower(email) = lower($2)`, orgID, email); err != nil {
		return fmt.Errorf("consume pending groups: %w", err)
	}
	return nil
}

// applyPendingGroups runs ApplyPendingGroups and audits (without failing) on
// error. A materialization failure must never block first login, so call sites
// keep committing their transaction regardless.
func (s *Server) applyPendingGroups(ctx context.Context, tx pgx.Tx, orgID, userID, email string) {
	if err := ApplyPendingGroups(ctx, tx, orgID, userID, email); err != nil {
		s.audit.Log(ctx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "group.pending_materialize.error", ResourceType: "group",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"error":   err.Error(),
			},
		})
	}
}
