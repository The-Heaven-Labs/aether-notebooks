package agent

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DeleteAgentSessionACLs deletes every agent_session ACL row whose session is
// matched by sessionWhere, a predicate over agent_sessions with positional
// placeholders in args (for example "agent_id = $1"). The matched session rows
// are locked first so the agent_sessions -> acl_entries lock order matches
// session creation's empty-session sweep, and the ACL rows are deleted while
// the session rows still exist so the subquery can see them. Call it in the
// same transaction as — and before — the parent delete.
func DeleteAgentSessionACLs(ctx context.Context, tx pgx.Tx, sessionWhere string, args ...any) error {
	if _, err := tx.Exec(ctx, `
		WITH locked_sessions AS (
			SELECT id FROM agent_sessions WHERE `+sessionWhere+` FOR UPDATE
		)
		DELETE FROM acl_entries
		WHERE resource_type = 'agent_session'
		  AND resource_id IN (SELECT id FROM locked_sessions)
	`, args...); err != nil {
		return fmt.Errorf("delete agent session acls: %w", err)
	}
	return nil
}
