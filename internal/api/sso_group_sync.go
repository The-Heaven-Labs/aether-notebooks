package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/sso"
)

// resolveSSOGroupNames filters IdP group names by the provider prefix and
// optionally strips the prefix. Names that become empty are dropped.
func resolveSSOGroupNames(provider sso.Provider, idpGroups []string) []string {
	var resolved []string
	for _, g := range idpGroups {
		if provider.GroupPrefix != "" && !strings.HasPrefix(g, provider.GroupPrefix) {
			continue
		}
		name := g
		if provider.StripGroupPrefix {
			name = strings.TrimSpace(strings.TrimPrefix(g, provider.GroupPrefix))
		}
		if name == "" {
			continue
		}
		resolved = append(resolved, name)
	}
	return resolved
}

func logGroupSyncError(ctx context.Context, logger *audit.Logger, orgID, userID, groupID, groupName string, err error) {
	if logger == nil {
		return
	}
	logger.Log(ctx, audit.Entry{
		OrgID:        orgID,
		UserID:       userID,
		Action:       "group.sso.error",
		ResourceType: "group",
		ResourceID:   groupID,
		ResourceName: groupName,
		Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
	})
}

func logGroupSyncEvent(ctx context.Context, logger *audit.Logger, action, orgID, userID, groupID, groupName string) {
	if logger == nil {
		return
	}
	logger.Log(ctx, audit.Entry{
		OrgID:        orgID,
		UserID:       userID,
		Action:       action,
		ResourceType: "group",
		ResourceID:   groupID,
		ResourceName: groupName,
		Metadata:     map[string]any{"user_id": userID},
	})
}

// SyncSSOGroups reconciles the user's SSO-tracked group memberships with the
// groups the IdP currently reports. It returns the IDs of groups whose
// membership actually changed (added or removed), deduplicated, so callers can
// reconcile warehouse access. Removals are included because a user no longer
// resolves to a group they were removed from, yet the group's grant must still
// be reconciled out of ClickHouse. When provider.SyncEmptyGroups is true, an
// empty IdP group list is authoritative and removes all SSO-managed
// memberships. Names dropped by prefix filtering or prefix stripping count as
// empty here, so changing a provider's prefix can remove memberships that were
// previously tracked under the old name.
func SyncSSOGroups(ctx context.Context, pool *pgxpool.Pool, logger *audit.Logger, provider sso.Provider, orgID, userID string, idpGroups []string) []string {
	resolved := resolveSSOGroupNames(provider, idpGroups)
	if len(resolved) == 0 && !provider.SyncEmptyGroups {
		return nil
	}

	changed := map[string]struct{}{}
	for _, groupName := range resolved {
		groupID, created, err := FindOrCreateGroup(ctx, pool, orgID, groupName)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, "", groupName, err)
			continue
		}
		if created {
			logGroupSyncEvent(ctx, logger, "group.sso.create", orgID, userID, groupID, groupName)
		}

		tag, err := pool.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, groupName, err)
			continue
		}
		// Only a fresh membership changes warehouse access; a login that
		// re-reports an existing group must not enqueue.
		if tag.RowsAffected() > 0 {
			changed[groupID] = struct{}{}
			logGroupSyncEvent(ctx, logger, "group.sso.add_member", orgID, userID, groupID, groupName)
		}

		_, err = pool.Exec(ctx,
			`INSERT INTO sso_group_memberships (provider_id, group_id, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			provider.ID, groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, groupName, err)
			continue
		}
	}

	staleGroups, err := FindStaleSSOGroups(ctx, pool, provider.ID, userID, resolved)
	if err != nil {
		logGroupSyncError(ctx, logger, orgID, userID, "", "", err)
		return changedGroupIDs(changed)
	}

	for _, groupID := range staleGroups {
		tag, err := pool.Exec(ctx,
			`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`,
			groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, "", err)
			continue
		}
		// Record the change before touching the bookkeeping row: a later
		// failure there must not drop the warehouse trigger.
		if tag.RowsAffected() > 0 {
			changed[groupID] = struct{}{}
			// Emit the removal before deleting the tracking row so a failure
			// there cannot suppress the audit event for a membership that is
			// already gone.
			logGroupSyncEvent(ctx, logger, "group.sso.remove_member", orgID, userID, groupID, "")
		}

		_, err = pool.Exec(ctx,
			`DELETE FROM sso_group_memberships WHERE provider_id=$1 AND group_id=$2 AND user_id=$3`,
			provider.ID, groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, "", err)
			continue
		}
	}

	return changedGroupIDs(changed)
}

// changedGroupIDs flattens a group-ID set for enqueueing.
func changedGroupIDs(changed map[string]struct{}) []string {
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	return ids
}

// FindOrCreateGroup returns the ID of the org's group with the given name
// (case-insensitive), inserting it when absent. created reports whether this
// call inserted the row.
func FindOrCreateGroup(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (string, bool, error) {
	var id string
	err := pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE org_id=$1 AND LOWER(name)=LOWER($2)`,
		orgID, name,
	).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("lookup group: %w", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name,
	).Scan(&id)
	if err != nil {
		return "", false, fmt.Errorf("create group: %w", err)
	}

	return id, true, nil
}

func FindStaleSSOGroups(ctx context.Context, pool *pgxpool.Pool, providerID, userID string, currentGroups []string) ([]string, error) {
	lowered := make([]string, len(currentGroups))
	for i, g := range currentGroups {
		lowered[i] = strings.ToLower(g)
	}

	rows, err := pool.Query(ctx,
		`SELECT sgm.group_id
		 FROM sso_group_memberships sgm
		 JOIN groups g ON g.id = sgm.group_id
		 WHERE sgm.provider_id = $1 AND sgm.user_id = $2
		 AND LOWER(g.name) != ALL($3)`,
		providerID, userID, lowered,
	)
	if err != nil {
		return nil, fmt.Errorf("find stale groups: %w", err)
	}
	defer rows.Close()

	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale group: %w", err)
		}
		stale = append(stale, id)
	}
	return stale, rows.Err()
}
