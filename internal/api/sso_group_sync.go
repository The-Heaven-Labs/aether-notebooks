package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/sso"
)

// SyncSSOGroups reconciles the user's SSO-tracked group memberships with the
// groups the IdP currently reports. It returns the IDs of groups whose
// membership actually changed (added or removed), deduplicated, so callers can
// reconcile warehouse access. Removals are included because a user no longer
// resolves to a group they were removed from, yet the group's grant must still
// be reconciled out of ClickHouse.
func SyncSSOGroups(ctx context.Context, pool *pgxpool.Pool, logger *audit.Logger, provider sso.Provider, orgID, userID string, idpGroups []string) []string {
	var filtered []string
	for _, g := range idpGroups {
		if provider.GroupPrefix == "" || strings.HasPrefix(g, provider.GroupPrefix) {
			filtered = append(filtered, g)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	changed := map[string]struct{}{}
	for _, groupName := range filtered {
		groupID, err := FindOrCreateGroup(ctx, pool, orgID, groupName)
		if err != nil {
			if logger != nil {
				logger.Log(ctx, audit.Entry{
					OrgID:        orgID,
					Action:       "group.sso.error",
					ResourceType: "group",
					ResourceName: groupName,
					Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
				})
			}
			continue
		}

		tag, err := pool.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			groupID, userID,
		)
		if err != nil {
			if logger != nil {
				logger.Log(ctx, audit.Entry{
					OrgID:        orgID,
					Action:       "group.sso.error",
					ResourceType: "group",
					ResourceID:   groupID,
					ResourceName: groupName,
					Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
				})
			}
			continue
		}
		// Only a fresh membership changes warehouse access; a login that
		// re-reports an existing group must not enqueue.
		if tag.RowsAffected() > 0 {
			changed[groupID] = struct{}{}
		}

		_, err = pool.Exec(ctx,
			`INSERT INTO sso_group_memberships (provider_id, group_id, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			provider.ID, groupID, userID,
		)
		if err != nil {
			if logger != nil {
				logger.Log(ctx, audit.Entry{
					OrgID:        orgID,
					Action:       "group.sso.error",
					ResourceType: "group",
					ResourceID:   groupID,
					ResourceName: groupName,
					Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
				})
			}
			continue
		}
	}

	staleGroups, err := FindStaleSSOGroups(ctx, pool, provider.ID, userID, filtered)
	if err != nil {
		if logger != nil {
			logger.Log(ctx, audit.Entry{
				OrgID:        orgID,
				Action:       "group.sso.error",
				ResourceType: "group",
				Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
			})
		}
		return changedGroupIDs(changed)
	}

	for _, groupID := range staleGroups {
		tag, err := pool.Exec(ctx,
			`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`,
			groupID, userID,
		)
		if err != nil {
			if logger != nil {
				logger.Log(ctx, audit.Entry{
					OrgID:        orgID,
					Action:       "group.sso.error",
					ResourceType: "group",
					ResourceID:   groupID,
					Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
				})
			}
			continue
		}
		// Record the change before touching the bookkeeping row: a later
		// failure there must not drop the warehouse trigger.
		if tag.RowsAffected() > 0 {
			changed[groupID] = struct{}{}
		}

		_, err = pool.Exec(ctx,
			`DELETE FROM sso_group_memberships WHERE provider_id=$1 AND group_id=$2 AND user_id=$3`,
			provider.ID, groupID, userID,
		)
		if err != nil {
			if logger != nil {
				logger.Log(ctx, audit.Entry{
					OrgID:        orgID,
					Action:       "group.sso.error",
					ResourceType: "group",
					ResourceID:   groupID,
					Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
				})
			}
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

func FindOrCreateGroup(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (string, error) {
	var id string
	err := pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE org_id=$1 AND LOWER(name)=LOWER($2)`,
		orgID, name,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", fmt.Errorf("lookup group: %w", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create group: %w", err)
	}

	return id, nil
}

func FindStaleSSOGroups(ctx context.Context, pool *pgxpool.Pool, providerID, userID string, currentGroups []string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT sgm.group_id
		 FROM sso_group_memberships sgm
		 JOIN groups g ON g.id = sgm.group_id
		 WHERE sgm.provider_id = $1 AND sgm.user_id = $2
		 AND g.name != ALL($3)`,
		providerID, userID, currentGroups,
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
