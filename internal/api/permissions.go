package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
)

type aclCandidate struct {
	subjectType string
	subjectID   string
	actions     []string
	specificity int // -1 = on resource itself, 0 = immediate parent folder, 1+ = ancestor
	subjectRank int // user=0, group=1, org_role=2
}

var resourceTable = map[string]string{
	"notebook":     "notebooks",
	"connector":    "connectors",
	"dashboard":    "dashboards",
	"agent":        "agents",
	"model_config": "model_configs",
	"skill":        "skills",
	"mcp_server":   "mcp_servers",
}

// checkPermission returns true if userID has action on resourceType/resourceID within orgID.
func (s *Server) checkPermission(ctx context.Context, userID, orgID, orgRole, resourceType, resourceID, action string) (bool, error) {
	// 1. Collect user's group memberships
	rows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, userID)
	if err != nil {
		return false, fmt.Errorf("group query: %w", err)
	}
	var groupIDs []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan group_id: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	rows.Close()

	// Include "Everyone" groups: any org member implicitly belongs to these.
	everyoneRows, err := s.db.Pool.Query(ctx, `SELECT id FROM groups WHERE org_id = $1 AND name = 'Everyone'`, orgID)
	if err != nil {
		return false, fmt.Errorf("everyone group query: %w", err)
	}
	for everyoneRows.Next() {
		var gid string
		if err := everyoneRows.Scan(&gid); err != nil {
			everyoneRows.Close()
			return false, fmt.Errorf("scan everyone group_id: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	everyoneRows.Close()

	// Org admins bypass ACLs when admin mode is enabled
	if orgRole == "admin" && adminModeFromContext(ctx) {
		return true, nil
	}

	// 2. ACL entries directly on the resource (specificity = -1)
	var candidates []aclCandidate
	resRows, err := s.db.Pool.Query(ctx,
		`SELECT subject_type, subject_id, actions FROM acl_entries
		 WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, orgID)
	if err != nil {
		return false, fmt.Errorf("acl resource query: %w", err)
	}
	for resRows.Next() {
		var c aclCandidate
		c.specificity = -1
		if err := resRows.Scan(&c.subjectType, &c.subjectID, &c.actions); err != nil {
			resRows.Close()
			return false, fmt.Errorf("scan acl_entry: %w", err)
		}
		c.subjectRank = subjectRank(c.subjectType)
		candidates = append(candidates, c)
	}
	resRows.Close()

	// 3. Find the folder to start the ancestor walk from
	var ancestorFolderID *string
	if resourceType == "folder" {
		var pid *string
		err := s.db.Pool.QueryRow(ctx,
			`SELECT parent_id FROM folders WHERE id = $1 AND org_id = $2`,
			resourceID, orgID,
		).Scan(&pid)
		if err != nil && err != pgx.ErrNoRows {
			return false, fmt.Errorf("folder parent query: %w", err)
		}
		ancestorFolderID = pid
	} else if table, ok := resourceTable[resourceType]; ok {
		var fid *string
		err := s.db.Pool.QueryRow(ctx,
			fmt.Sprintf(`SELECT folder_id FROM %s WHERE id = $1 AND org_id = $2`, table),
			resourceID, orgID,
		).Scan(&fid)
		if err != nil && err != pgx.ErrNoRows {
			return false, fmt.Errorf("resource folder query: %w", err)
		}
		ancestorFolderID = fid
	}

	// 4. Walk ancestor folders collecting ACL entries
	if ancestorFolderID != nil {
		folderRows, err := s.db.Pool.Query(ctx, `
			WITH RECURSIVE ancestors AS (
				SELECT id, parent_id, 0 AS depth FROM folders WHERE id = $1
				UNION ALL
				SELECT f.id, f.parent_id, a.depth + 1
				FROM folders f JOIN ancestors a ON f.id = a.parent_id
			)
			SELECT ae.subject_type, ae.subject_id, ae.actions, a.depth
			FROM ancestors a
			JOIN acl_entries ae ON ae.resource_type = 'folder' AND ae.resource_id = a.id AND ae.org_id = $2
			ORDER BY a.depth ASC
		`, *ancestorFolderID, orgID)
		if err != nil {
			return false, fmt.Errorf("ancestor acl query: %w", err)
		}
		for folderRows.Next() {
			var c aclCandidate
			if err := folderRows.Scan(&c.subjectType, &c.subjectID, &c.actions, &c.specificity); err != nil {
				folderRows.Close()
				return false, fmt.Errorf("scan folder_acl: %w", err)
			}
			c.subjectRank = subjectRank(c.subjectType)
			candidates = append(candidates, c)
		}
		folderRows.Close()
		if err := folderRows.Err(); err != nil {
			return false, fmt.Errorf("ancestor rows error: %w", err)
		}
	}

	// 5. Sort: most specific first; within same specificity, user > group > org_role
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity < candidates[j].specificity
		}
		return candidates[i].subjectRank < candidates[j].subjectRank
	})

	// 6. Find first entry that matches this user
	var (
		matchedNonEveryoneEntry bool
		aclExistsButUserNotInIt bool
	)
	for _, c := range candidates {
		if !matchesUser(c, userID, orgRole, groupIDs) {
			aclExistsButUserNotInIt = true
			continue
		}
		for _, a := range c.actions {
			if a == action {
				return true, nil
			}
		}
		// org_role:everyone entries are not restrictive - they don't block org role defaults
		if c.subjectType == "org_role" && c.subjectID == "everyone" {
			aclExistsButUserNotInIt = false // org_role:everyone doesn't count as restrictive
			continue
		}
		// User matched entry but action not granted - this is restrictive, don't fall through
		matchedNonEveryoneEntry = true
		break
	}

	// 7. User matched a restrictive ACL but action not granted
	if matchedNonEveryoneEntry {
		return false, nil
	}

	// 8. ACL exists in chain but user not in it → DENY
	if aclExistsButUserNotInIt {
		return false, nil
	}

	// 9. No ACL matched user → DENY (deny-by-default)
	return false, nil
}

func matchesUser(c aclCandidate, userID, orgRole string, groupIDs []string) bool {
	switch c.subjectType {
	case "user":
		return c.subjectID == userID
	case "group":
		for _, gid := range groupIDs {
			if c.subjectID == gid {
				return true
			}
		}
	case "org_role":
		// "everyone" matches every member of the org; individual roles are deprecated
		return c.subjectID == "everyone"
	}
	return false
}

func subjectRank(subjectType string) int {
	switch subjectType {
	case "user":
		return 0
	case "group":
		return 1
	default:
		return 2
	}
}

// requirePermission returns middleware that checks if the authenticated user has
// the given action on the resource identified by the path parameter idParam.
func (s *Server) requirePermission(resourceType, idParam, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			resourceID := r.PathValue(idParam)
			allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, resourceType, resourceID, action)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "permission check failed")
				return
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
