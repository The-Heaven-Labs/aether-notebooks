package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/auth"
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

	// 5. Evaluate the candidates. The evaluation rules — most specific first,
	//    the Everyone exception, view implication, and ancestor-folder action
	//    expansion — live in evaluateCandidates so the batched resolver shares
	//    them by construction.
	return evaluateCandidates(candidates, userID, orgRole, groupIDs, resourceType, action), nil
}

// permIdentity is the caller-side of a permission evaluation, resolved once
// per request instead of once per check.
type permIdentity struct {
	UserID    string
	OrgID     string
	OrgRole   string
	GroupIDs  []string
	AdminMode bool
}

// permIdentity resolves the caller's group memberships (explicit + Everyone)
// and admin-mode flag once, so a batched resolution shares them across
// resources.
func (s *Server) permIdentity(ctx context.Context, claims *auth.Claims) (permIdentity, error) {
	groups, err := s.callerGroupIDs(ctx, claims.UserID, claims.OrgID)
	if err != nil {
		return permIdentity{}, err
	}
	return permIdentity{
		UserID:    claims.UserID,
		OrgID:     claims.OrgID,
		OrgRole:   claims.Role,
		GroupIDs:  groups,
		AdminMode: adminModeFromContext(ctx),
	}, nil
}

// resolveActions resolves the requested actions for many resources of one type
// in a constant number of queries: a resource→folder map query, one direct-ACL
// query, and one recursive ancestry walk seeded with every distinct folder.
// Evaluation goes through evaluateCandidates, so results match checkPermission
// exactly, with two deliberate fail-closed divergences: ids that do not exist
// in the caller's org resolve all-false even when orphaned acl_entries rows
// exist for them, and only canonical lowercase UUID spellings resolve (the
// queries match on id::text), so an uppercase-spelling input is denied where
// checkPermission's uuid cast would allow it. Callers pass ids read from
// org-scoped tables, so both divergences are inert in practice. The result
// contains an entry for every requested id (deduped); ids that are not in the
// org (or fail every rule) get all requested actions false. Unknown resource
// types fail closed with an error.
func (s *Server) resolveActions(ctx context.Context, ident permIdentity, resourceType string, ids []string, actions []string) (map[string]map[string]bool, error) {
	table := ""
	switch {
	case resourceType == "folder":
		table = "folders"
	default:
		t, ok := resourceTable[resourceType]
		if !ok {
			return nil, fmt.Errorf("unknown resource type %q", resourceType)
		}
		table = t
	}

	// Dedupe the requested ids: the result map keys dedupe anyway, but the
	// queries should see each id once. Every requested id gets an all-false
	// entry before any rule runs, so the caller always sees a uniform shape.
	unique := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	result := make(map[string]map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
		entry := make(map[string]bool, len(actions))
		for _, action := range actions {
			entry[action] = false
		}
		result[id] = entry
	}
	if len(unique) == 0 {
		return result, nil
	}

	// 1. Resource → folder map, also the org scoping for the admin bypass.
	//    For a folder resource the ancestor walk starts at its parent; for
	//    other types it starts at the resource's folder.
	folderColumn := "folder_id"
	if resourceType == "folder" {
		folderColumn = "parent_id"
	}
	folderRows, err := s.db.Pool.Query(ctx,
		fmt.Sprintf(`SELECT id::text, %s FROM %s WHERE id = ANY($1::uuid[]) AND org_id = $2`, folderColumn, table),
		unique, ident.OrgID)
	if err != nil {
		return nil, fmt.Errorf("resolve %s folders: %w", resourceType, err)
	}
	seed := make(map[string]*string, len(unique))
	for folderRows.Next() {
		var id string
		var folderID *string
		if err := folderRows.Scan(&id, &folderID); err != nil {
			folderRows.Close()
			return nil, fmt.Errorf("scan %s folder: %w", resourceType, err)
		}
		seed[id] = folderID
	}
	folderRows.Close()
	if err := folderRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s folders: %w", resourceType, err)
	}

	// 2. Org admins in admin mode bypass ACLs for every resource that exists
	//    in their org (query 1 is the org scoping, mirroring the single
	//    check's resourceOrgID comparison). Ids absent from the map keep their
	//    all-false entries.
	if ident.AdminMode && ident.OrgRole == "admin" {
		for id := range seed {
			for _, action := range actions {
				result[id][action] = true
			}
		}
		return result, nil
	}

	// 3. ACL entries directly on the resources (specificity -1).
	direct := make(map[string][]aclCandidate)
	aclRows, err := s.db.Pool.Query(ctx, `
		SELECT resource_id::text, subject_type, subject_id, actions
		FROM acl_entries
		WHERE resource_type = $1 AND resource_id = ANY($2::uuid[]) AND org_id = $3
	`, resourceType, unique, ident.OrgID)
	if err != nil {
		return nil, fmt.Errorf("load %s acls: %w", resourceType, err)
	}
	for aclRows.Next() {
		var id string
		var c aclCandidate
		if err := aclRows.Scan(&id, &c.subjectType, &c.subjectID, &c.actions); err != nil {
			aclRows.Close()
			return nil, fmt.Errorf("scan %s acl: %w", resourceType, err)
		}
		c.specificity = -1
		c.subjectRank = subjectRank(c.subjectType)
		direct[id] = append(direct[id], c)
	}
	aclRows.Close()
	if err := aclRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s acls: %w", resourceType, err)
	}

	// 4. One recursive walk seeded with every distinct folder: depth 0 is the
	//    resource's immediate folder, matching checkPermissionQ's walk.
	seeds := make([]string, 0, len(seed))
	seenSeed := make(map[string]bool, len(seed))
	for _, folderID := range seed {
		if folderID == nil || seenSeed[*folderID] {
			continue
		}
		seenSeed[*folderID] = true
		seeds = append(seeds, *folderID)
	}
	ancestors := make(map[string][]aclCandidate)
	if len(seeds) > 0 {
		ancRows, err := s.db.Pool.Query(ctx, `
			WITH RECURSIVE ancestors(seed_id, id, parent_id, depth) AS (
				SELECT id, id, parent_id, 0 FROM folders WHERE id = ANY($1::uuid[])
				UNION ALL
				SELECT a.seed_id, f.id, f.parent_id, a.depth + 1
				FROM folders f JOIN ancestors a ON f.id = a.parent_id
			)
			SELECT a.seed_id::text, a.depth, ae.subject_type, ae.subject_id, ae.actions
			FROM ancestors a
			JOIN acl_entries ae ON ae.resource_type = 'folder' AND ae.resource_id = a.id AND ae.org_id = $2
		`, seeds, ident.OrgID)
		if err != nil {
			return nil, fmt.Errorf("load %s ancestor acls: %w", resourceType, err)
		}
		for ancRows.Next() {
			var seedID string
			var c aclCandidate
			if err := ancRows.Scan(&seedID, &c.specificity, &c.subjectType, &c.subjectID, &c.actions); err != nil {
				ancRows.Close()
				return nil, fmt.Errorf("scan %s ancestor acl: %w", resourceType, err)
			}
			c.subjectRank = subjectRank(c.subjectType)
			ancestors[seedID] = append(ancestors[seedID], c)
		}
		ancRows.Close()
		if err := ancRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate %s ancestor acls: %w", resourceType, err)
		}
	}

	// 5. Evaluate each requested id through the shared core. Build a fresh
	//    candidate slice per resource: evaluateCandidates sorts in place, so
	//    one resource's sort must never reorder another's candidates.
	for _, id := range unique {
		folderID, inOrg := seed[id]
		if !inOrg {
			continue // not in the org: the all-false entry stands
		}
		candidates := make([]aclCandidate, 0, len(direct[id]))
		candidates = append(candidates, direct[id]...)
		if folderID != nil {
			candidates = append(candidates, ancestors[*folderID]...)
		}
		for _, action := range actions {
			if evaluateCandidates(candidates, ident.UserID, ident.OrgRole, ident.GroupIDs, resourceType, action) {
				result[id][action] = true
			}
		}
	}
	return result, nil
}

// actionAllowed reports one action from a resolveActions result map; a missing
// id or action is false.
func actionAllowed(resolved map[string]map[string]bool, id, action string) bool {
	return resolved[id][action]
}

// evaluateCandidates is the shared permission-evaluation core: most specific
// first (resource before its parent folder before ancestors), user > group >
// org_role within a depth, the Everyone exception, view implication, and
// ancestor-folder action expansion. Grants are additive — any matching entry
// that covers the action allows — so the ordering is retained for parity with
// the documented resolution order and does not itself deny beyond the
// Everyone-deferral rule. The candidates slice is sorted in place, so callers
// sharing a backing array across resources must copy it first.
// checkPermissionQ and the batched resolver must both evaluate through this
// function so batch results equal single-check results by construction.
func evaluateCandidates(candidates []aclCandidate, userID, orgRole string, groupIDs []string, resourceType, action string) bool {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity < candidates[j].specificity
		}
		return candidates[i].subjectRank < candidates[j].subjectRank
	})
	everyoneGrants := false
	for _, c := range candidates {
		if !matchesUser(c, userID, orgRole, groupIDs) {
			continue
		}
		if candidateGrants(c, resourceType, action) {
			if c.subjectType == "org_role" && c.subjectID == "everyone" {
				everyoneGrants = true
			} else {
				return true
			}
		}
	}
	// Only an Everyone grant can allow past this point; a restrictive
	// more-specific match and a no-match state both deny by default.
	return everyoneGrants
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

// folderActionImplications expands an ancestor folder grant onto the actions
// descendant resources support but the folder chip set cannot name directly.
// Folders are granted with a coarse five-action vocabulary
// (view/create/edit/manage/delete), so the rungs carry their whole content
// capability to the items inside:
//
//   - edit:   operate on the contents — run notebooks, use connectors and
//     tools, and read a dashboard with its underlying data.
//   - manage: control access to the contents — share notebooks, connectors,
//     and dashboards.
//
// Together with the same-named matches, a full folder grant therefore covers
// every action of every resource inside the folder, while a partial grant
// covers exactly its rung (a view-only folder grant still cannot run).
// The expansion is deliberately ancestor-only: an entry on the resource
// itself keeps its exact action list, so a direct notebook edit grant never
// implies run.
var folderActionImplications = map[string][]string{
	"edit":   {"run", "use", "view_with_data"},
	"manage": {"share"},
}

// candidateGrants reports whether one ACL candidate grants action on the
// resource being checked. Candidates on the resource itself (specificity -1)
// match by name and the view implication only; ancestor folder candidates
// additionally expand through folderActionImplications. Folders are excluded
// from the expansion because they have no descendant actions to imply.
func candidateGrants(c aclCandidate, resourceType, action string) bool {
	if grantsAction(c.actions, action) {
		return true
	}
	if c.specificity < 0 || resourceType == "folder" {
		return false
	}
	for folderAction, implied := range folderActionImplications {
		if !grantsAction(c.actions, folderAction) {
			continue
		}
		for _, a := range implied {
			if a == action {
				return true
			}
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
