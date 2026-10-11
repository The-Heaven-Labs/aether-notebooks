package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"
)

var uuidRegexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

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
	"tool":         "tools",
}

// permissionQuerier is the read surface shared by *pgxpool.Pool and pgx.Tx.
// Permission checks run on the pool by default; callers that already hold a
// transaction (the internal dashboard store validator) pass the transaction so
// the check does not acquire a second pool connection, which could stall a
// saturated pool behind transactions waiting for one.
type permissionQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// resourceOrgID resolves the org that owns a resource for the admin-mode
// bypass. Folders, resourceTable types, and agent_session (via its agent) are
// supported; unknown resource types fail closed with an error, and a missing
// resource resolves to the empty string (no bypass).
func (s *Server) resourceOrgID(ctx context.Context, resourceType, resourceID string) (string, error) {
	return resourceOrgIDQ(ctx, s.db.Pool, resourceType, resourceID)
}

func resourceOrgIDQ(ctx context.Context, q permissionQuerier, resourceType, resourceID string) (string, error) {
	var query string
	switch resourceType {
	case "folder":
		query = "SELECT org_id FROM folders WHERE id = $1"
	case "agent_session":
		query = `SELECT a.org_id FROM agent_sessions s JOIN agents a ON a.id = s.agent_id WHERE s.id = $1`
	default:
		table, ok := resourceTable[resourceType]
		if !ok {
			return "", fmt.Errorf("unknown resource type %q", resourceType)
		}
		query = fmt.Sprintf("SELECT org_id FROM %s WHERE id = $1", table)
	}

	var resourceOrg string
	err := q.QueryRow(ctx, query, resourceID).Scan(&resourceOrg)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve %s org: %w", resourceType, err)
	}
	return resourceOrg, nil
}

// checkSessionPermission reports whether userID may perform action on the
// agent session identified by sessionID. Resolution order:
//
//  1. owner fallback — the session's user_id always passes for any action;
//  2. the agent_session ACL for the requested action, including the org-admin
//     admin-mode bypass inside checkPermission;
//  3. for "view" only, live notebook-viewer inheritance when the session's
//     share_with_notebook_viewers flag is set and a notebook is attached.
//
// Sharing is read-only: a non-owner subject can only ever hold view, so an
// ACL entry granting another action to a non-owner is ignored unless the
// caller is an org admin with admin mode enabled.
func (s *Server) checkSessionPermission(ctx context.Context, userID, orgID, orgRole, sessionID, action string) (bool, error) {
	var (
		ownerID    string
		notebookID *string
		inherit    bool
	)
	err := s.db.Pool.QueryRow(ctx, `
		SELECT user_id, notebook_id, share_with_notebook_viewers
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&ownerID, &notebookID, &inherit)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("session permission query: %w", err)
	}
	if ownerID == userID {
		return true, nil
	}

	if action != "view" {
		if orgRole != "admin" || !adminModeFromContext(ctx) {
			return false, nil
		}
		return s.checkPermission(ctx, userID, orgID, orgRole, "agent_session", sessionID, action)
	}

	granted, err := s.checkPermission(ctx, userID, orgID, orgRole, "agent_session", sessionID, "view")
	if err != nil || granted {
		return granted, err
	}
	if inherit && notebookID != nil {
		return s.checkPermission(ctx, userID, orgID, orgRole, "notebook", *notebookID, "view")
	}
	return false, nil
}

// callerGroupIDs returns the caller's explicit group memberships followed by
// their org's Everyone group. checkPermission and the batched session
// visibility filter both build their subject set from this helper so group
// resolution exists in exactly one place.
func (s *Server) callerGroupIDs(ctx context.Context, userID, orgID string) ([]string, error) {
	return callerGroupIDsQ(ctx, s.db.Pool, userID, orgID)
}

func callerGroupIDsQ(ctx context.Context, q permissionQuerier, userID, orgID string) ([]string, error) {
	groupIDs := []string{}
	rows, err := q.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("caller group query: %w", err)
	}
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan caller group: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("caller group rows: %w", err)
	}

	// Include "Everyone" groups: any org member implicitly belongs to these.
	everyoneRows, err := q.Query(ctx, `SELECT id FROM groups WHERE org_id = $1 AND name = 'Everyone'`, orgID)
	if err != nil {
		return nil, fmt.Errorf("everyone group query: %w", err)
	}
	for everyoneRows.Next() {
		var gid string
		if err := everyoneRows.Scan(&gid); err != nil {
			everyoneRows.Close()
			return nil, fmt.Errorf("scan everyone group: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	everyoneRows.Close()
	if err := everyoneRows.Err(); err != nil {
		return nil, fmt.Errorf("everyone group rows: %w", err)
	}
	return groupIDs, nil
}

// checkPermission returns true if userID has action on resourceType/resourceID within orgID.
func (s *Server) checkPermission(ctx context.Context, userID, orgID, orgRole, resourceType, resourceID, action string) (bool, error) {
	return checkPermissionQ(ctx, s.db.Pool, userID, orgID, orgRole, resourceType, resourceID, action)
}

// checkPermissionQ is checkPermission against an explicit querier, so callers
// that already hold a transaction can run the check on that connection.
func checkPermissionQ(ctx context.Context, q permissionQuerier, userID, orgID, orgRole, resourceType, resourceID, action string) (bool, error) {
	// 1. Collect user's group memberships
	groupIDs, err := callerGroupIDsQ(ctx, q, userID, orgID)
	if err != nil {
		return false, err
	}

	// Org admins bypass ACLs only when admin mode is enabled — scoped to their org
	if orgRole == "admin" && adminModeFromContext(ctx) {
		resourceOrg, err := resourceOrgIDQ(ctx, q, resourceType, resourceID)
		if err != nil {
			return false, fmt.Errorf("admin bypass org resolve: %w", err)
		}
		if resourceOrg == orgID {
			return true, nil
		}
	}

	// 2. ACL entries directly on the resource (specificity = -1)
	var candidates []aclCandidate
	resRows, err := q.Query(ctx,
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
		err := q.QueryRow(ctx,
			`SELECT parent_id FROM folders WHERE id = $1 AND org_id = $2`,
			resourceID, orgID,
		).Scan(&pid)
		if err != nil && err != pgx.ErrNoRows {
			return false, fmt.Errorf("folder parent query: %w", err)
		}
		ancestorFolderID = pid
	} else if table, ok := resourceTable[resourceType]; ok {
		var fid *string
		err := q.QueryRow(ctx,
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
		folderRows, err := q.Query(ctx, `
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
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity < candidates[j].specificity
		}
		return candidates[i].subjectRank < candidates[j].subjectRank
	})

	// 6. Evaluate candidates: if any matching entry grants the action → allow.
	//    A matching org_role:everyone entry that grants the action is sufficient
	//    even when a more-specific non-everyone entry lacks the action (restrictive).
	//    view is implied by use/edit/share/delete/admin.
	var (
		everyoneGrants   bool
		restrictiveMatch bool
	)
	for _, c := range candidates {
		if !matchesUser(c, userID, orgRole, groupIDs) {
			continue
		}

		if grantsAction(c.actions, action) {
			if c.subjectType == "org_role" && c.subjectID == "everyone" {
				everyoneGrants = true
			} else {
				return true, nil
			}
		} else if !(c.subjectType == "org_role" && c.subjectID == "everyone") {
			restrictiveMatch = true
		}
	}

	// 7. org_role:everyone grants the action → allow
	if everyoneGrants {
		return true, nil
	}

	// 8. Restrictive match without everyone grant → deny
	if restrictiveMatch {
		return false, nil
	}

	// 9. No matching entry → DENY (deny-by-default)
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

// isViewImpliedBy returns true if the given action implies "view" permission.
// Users who can use/edit/share/delete/admin a resource should always be able to see it.
func isViewImpliedBy(action string) bool {
	switch action {
	case "use", "edit", "share", "delete", "admin":
		return true
	}
	return false
}

// grantsAction reports whether actions contains action, applying the same
// view-implication rule as checkPermission: view is implied by
// use/edit/share/delete/admin. It is the single action-matching primitive for
// checkPermission and the batched visibility filters.
func grantsAction(actions []string, action string) bool {
	for _, a := range actions {
		if a == action || (action == "view" && isViewImpliedBy(a)) {
			return true
		}
	}
	return false
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

func isValidUUID(s string) bool {
	return uuidRegexp.MatchString(s)
}
