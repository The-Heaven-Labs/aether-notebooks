package agent

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DeleteAgentSessionACLs deletes every agent_session ACL row whose session is
// matched by sessionWhere, a predicate over agent_sessions with positional
// placeholders in args (for example "agent_id = $1"). The matched session rows
// are locked first and the same id set is used for both deletes, so cleanup
// acts on one snapshot. Staged pending rows are deleted before real ACL rows:
// materialization (applyPendingACL) locks pending_acl_entries before
// acl_entries, so this order keeps the lock order (agent_sessions -> pending ->
// real) shared with session creation's empty-session sweep and cannot deadlock
// with a concurrent join materializing the same email. The rows are deleted
// while the session rows still exist so the deletes can see them. Call it in
// the same transaction as — and before — the parent delete.
func DeleteAgentSessionACLs(ctx context.Context, tx pgx.Tx, sessionWhere string, args ...any) error {
	rows, err := tx.Query(ctx, `SELECT id FROM agent_sessions WHERE `+sessionWhere+` FOR UPDATE`, args...)
	if err != nil {
		return fmt.Errorf("lock agent sessions: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan agent session id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("lock agent sessions: %w", err)
	}
	rows.Close()

	if len(ids) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM pending_acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = ANY($1)
	`, ids); err != nil {
		return fmt.Errorf("delete agent session pending acls: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = ANY($1)
	`, ids); err != nil {
		return fmt.Errorf("delete agent session acls: %w", err)
	}
	return nil
}
