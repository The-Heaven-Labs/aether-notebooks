package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
)

// permissionCheckSQL returns a SQL fragment that checks if the user has access to a resource.
// The resource is referenced by the alias in resourceAlias (e.g., 'f' for folders).
func permissionCheckSQL(resourceType, resourceAlias, userIDVar string, groupIDsVar string) string {
	return fmt.Sprintf(`(
		%s.owner_id = %s
		OR EXISTS (
			SELECT 1 FROM acl_entries 
			WHERE resource_type = '%s' AND resource_id = %s.id 
			AND subject_type = 'user' AND subject_id = %s::text
		)
		OR EXISTS (
			SELECT 1 FROM acl_entries ae 
			WHERE resource_type = '%s' AND resource_id = %s.id 
			AND ae.subject_type = 'group' AND ae.subject_id = ANY(%s::text[])
		)
		OR EXISTS (
			SELECT 1 FROM acl_entries 
			WHERE resource_type = '%s' AND resource_id = %s.id 
			AND subject_type = 'org_role' AND subject_id = 'everyone'
		)
	)`, resourceType, userIDVar, resourceType, resourceAlias, userIDVar, resourceType, resourceAlias, groupIDsVar, resourceType, resourceAlias)
}

// hasAccessibleContentSQL returns a SQL fragment that checks if a folder has any accessible content
// directly inside it or at any depth in the descendant hierarchy (using a recursive CTE).
func hasAccessibleContentSQL(folderAlias, userIDVar string, groupIDsVar string) string {
	return fmt.Sprintf(`(
		-- Direct accessible content in this folder
		EXISTS (SELECT 1 FROM notebooks nb WHERE nb.folder_id = %[1]s.id AND nb.org_id = %[1]s.org_id AND (
			EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = %[2]s::text)
			OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY(%[3]s::text[]))
			OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
		) LIMIT 1)
		OR EXISTS (SELECT 1 FROM connectors c WHERE c.folder_id = %[1]s.id AND c.org_id = %[1]s.org_id AND (
			EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = %[2]s::text)
			OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY(%[3]s::text[]))
			OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
		) LIMIT 1)
		OR EXISTS (SELECT 1 FROM dashboards d WHERE d.folder_id = %[1]s.id AND d.org_id = %[1]s.org_id AND (
			EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d.id AND subject_type = 'user' AND subject_id = %[2]s::text)
			OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d.id AND ae.subject_type = 'group' AND ae.subject_id = ANY(%[3]s::text[]))
			OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d.id AND subject_type = 'org_role' AND subject_id = 'everyone')
		) LIMIT 1)
		OR EXISTS (
			WITH RECURSIVE desc_ids AS (
				SELECT id FROM folders WHERE parent_id = %[1]s.id AND org_id = %[1]s.org_id
				UNION ALL
				SELECT f.id FROM folders f
				JOIN desc_ids d ON f.parent_id = d.id
				WHERE f.org_id = %[1]s.org_id
			)
			SELECT 1 FROM desc_ids d
			WHERE (
				EXISTS (
					SELECT 1 FROM notebooks nb
					WHERE nb.folder_id = d.id AND nb.org_id = %[1]s.org_id
					AND (
						EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = %[2]s::text)
						OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY(%[3]s::text[]))
						OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
					)
				)
				OR EXISTS (
					SELECT 1 FROM connectors c
					WHERE c.folder_id = d.id AND c.org_id = %[1]s.org_id
					AND (
						EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = %[2]s::text)
						OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY(%[3]s::text[]))
						OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
					)
				)
				OR EXISTS (
					SELECT 1 FROM dashboards d2
					WHERE d2.folder_id = d.id AND d2.org_id = %[1]s.org_id
					AND (
						EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = %[2]s::text)
						OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY(%[3]s::text[]))
						OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
					)
				)
			)
			LIMIT 1
		)
	)`, folderAlias, userIDVar, groupIDsVar)
}

// filterFoldersWithVisibleDescendantContent answers folderHasDescendantContent
// for many folders in one query: one recursive walk seeded with all requested
// folder ids, aggregated per seed. Callers use it for child folders that fail
// their own view check, to decide whether the folder is still worth showing
// because something inside it is visible. Admin mode is deliberately not
// considered: callers only probe folders that failed resolution, which cannot
// include an admin-mode-visible folder.
//
// The content predicates are lifted verbatim from folderHasDescendantContent:
// every seed's descendants (the seed itself included) are probed for
// notebooks, connectors, and dashboards whose folder_id matches and that carry
// an ACL entry for the caller's user id, one of their groups, or Everyone. As
// in the single-folder query, owner_id, deleted_at, and the granted actions
// are deliberately not consulted — an ACL row's subject match alone counts.
func (s *Server) filterFoldersWithVisibleDescendantContent(ctx context.Context, ident permIdentity, folderIDs []string) (map[string]bool, error) {
	// Dedupe the requested seeds: the result map keys dedupe anyway, but the
	// query should see each id once. Every requested id gets a false entry
	// before any probe runs, so the caller always sees a uniform shape.
	unique := make([]string, 0, len(folderIDs))
	byLower := make(map[string][]string, len(folderIDs))
	result := make(map[string]bool, len(folderIDs))
	for _, id := range folderIDs {
		if _, seen := result[id]; seen {
			continue
		}
		unique = append(unique, id)
		result[id] = false
		lower := strings.ToLower(id)
		byLower[lower] = append(byLower[lower], id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE descendants(seed_id, id, parent_id) AS (
			SELECT id, id, parent_id FROM folders WHERE id = ANY($1::uuid[])
			UNION ALL
			SELECT d.seed_id, f.id, f.parent_id
			FROM folders f
			JOIN descendants d ON f.parent_id = d.id
			WHERE f.org_id = $2
		)
		SELECT DISTINCT d.seed_id::text
		FROM descendants d
		WHERE
		EXISTS (
			SELECT 1 FROM notebooks nb
			WHERE nb.folder_id = d.id AND nb.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM connectors c
			WHERE c.folder_id = d.id AND c.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM dashboards d2
			WHERE d2.folder_id = d.id AND d2.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
	`, unique, ident.OrgID, ident.UserID, ident.GroupIDs)
	if err != nil {
		return nil, fmt.Errorf("descendant content query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seedID string
		if err := rows.Scan(&seedID); err != nil {
			return nil, fmt.Errorf("scan descendant content seed: %w", err)
		}
		// The query returns canonical lowercase uuid text while a requested id
		// may carry an uppercase spelling (path parameters); Postgres parses
		// both to the same uuid, so match case-insensitively to keep the
		// single-folder wrapper equivalent to its former query.
		for _, id := range byLower[strings.ToLower(seedID)] {
			result[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("descendant content rows: %w", err)
	}
	return result, nil
}

// folderHasDescendantContent checks if any notebook, connector, or dashboard
// directly in folderID or in any descendant folder has an ACL entry granting access to the user.
// It is a thin wrapper over filterFoldersWithVisibleDescendantContent so the
// single-folder and batched answers can never drift.
func (s *Server) folderHasDescendantContent(ctx context.Context, folderID, orgID, userID string, groupIDs []string) (bool, error) {
	got, err := s.filterFoldersWithVisibleDescendantContent(ctx, permIdentity{
		UserID:   userID,
		OrgID:    orgID,
		GroupIDs: groupIDs,
	}, []string{folderID})
	if err != nil {
		return false, err
	}
	return got[folderID], nil
}

// filterFoldersWithACLSubjectMatch answers, for many folders in one query,
// whether the caller is named by any ACL entry on the folder itself. It exists
// for the /home listing, whose former inline EXISTS matched ACL subjects
// without evaluating the entry's actions, the resolution order, or the folder
// ancestry — semantics deliberately wider than a view check — so this helper
// reproduces that predicate instead of calling resolveActions. The result
// carries an entry for every requested id (deduped); ids without a matching
// entry are false.
func (s *Server) filterFoldersWithACLSubjectMatch(ctx context.Context, folderIDs []string, userID string, groupIDs []string) (map[string]bool, error) {
	unique := make([]string, 0, len(folderIDs))
	byLower := make(map[string][]string, len(folderIDs))
	result := make(map[string]bool, len(folderIDs))
	for _, id := range folderIDs {
		if _, seen := result[id]; seen {
			continue
		}
		unique = append(unique, id)
		result[id] = false
		lower := strings.ToLower(id)
		byLower[lower] = append(byLower[lower], id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT resource_id::text
		FROM acl_entries
		WHERE resource_type = 'folder'
		  AND resource_id = ANY($1::uuid[])
		  AND (
			(subject_type = 'user' AND subject_id = $2::text)
			OR (subject_type = 'group' AND subject_id = ANY($3::text[]))
			OR (subject_type = 'org_role' AND subject_id = 'everyone')
		  )
	`, unique, userID, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("folder acl subject query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan folder acl subject: %w", err)
		}
		// The query returns canonical lowercase uuid text; match
		// case-insensitively so an uppercase requested spelling still lands.
		for _, requested := range byLower[strings.ToLower(id)] {
			result[requested] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("folder acl subject rows: %w", err)
	}
	return result, nil
}

// filterFoldersWithVisibleDescendantContentOrFolders is the folder-aware
// descendant probe used by the /home listing. For every requested seed it
// answers whether any STRICTLY DESCENDANT folder is visible to the caller: as a
// folder the caller owns or holds a folder ACL entry on (any actions), or as
// the home of a notebook, connector, or dashboard carrying a matching ACL
// entry. It reproduces the listing's former inline predicate exactly and is
// deliberately WIDER than filterFoldersWithVisibleDescendantContent, which is
// content-only and seed-inclusive: here a descendant folder counts even when
// nothing inside it is visible, so a folder whose only reachable descendant is
// a folder stem stays listed. The seed itself is never probed (the /home
// listing checks a direct ACL subject match separately). Admin mode is
// deliberately not considered: callers only probe folders that failed
// resolution, which cannot include an admin-mode-visible folder.
func (s *Server) filterFoldersWithVisibleDescendantContentOrFolders(ctx context.Context, ident permIdentity, folderIDs []string) (map[string]bool, error) {
	unique := make([]string, 0, len(folderIDs))
	byLower := make(map[string][]string, len(folderIDs))
	result := make(map[string]bool, len(folderIDs))
	for _, id := range folderIDs {
		if _, seen := result[id]; seen {
			continue
		}
		unique = append(unique, id)
		result[id] = false
		lower := strings.ToLower(id)
		byLower[lower] = append(byLower[lower], id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		WITH RECURSIVE descendants(seed_id, id) AS (
			SELECT p.id, c.id
			FROM folders p
			JOIN folders c ON c.parent_id = p.id AND c.org_id = p.org_id
			WHERE p.id = ANY($1::uuid[]) AND p.org_id = $2
			UNION ALL
			SELECT d.seed_id, f.id
			FROM folders f
			JOIN descendants d ON f.parent_id = d.id
			WHERE f.org_id = $2
		)
		SELECT DISTINCT d.seed_id::text
		FROM descendants d
		WHERE
		EXISTS (
			SELECT 1 FROM folders f2
			WHERE f2.id = d.id
			AND (
				f2.owner_id = $3
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
		)
		OR EXISTS (
			SELECT 1 FROM notebooks nb
			WHERE nb.folder_id = d.id AND nb.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM connectors c
			WHERE c.folder_id = d.id AND c.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
		OR EXISTS (
			SELECT 1 FROM dashboards d2
			WHERE d2.folder_id = d.id AND d2.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		)
	`, unique, ident.OrgID, ident.UserID, ident.GroupIDs)
	if err != nil {
		return nil, fmt.Errorf("folder-aware descendant query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seedID string
		if err := rows.Scan(&seedID); err != nil {
			return nil, fmt.Errorf("scan folder-aware descendant seed: %w", err)
		}
		for _, id := range byLower[strings.ToLower(seedID)] {
			result[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("folder-aware descendant rows: %w", err)
	}
	return result, nil
}

type folderNotebook struct {
	models.Notebook
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanShare  bool `json:"can_share"`
}

type folderDashboard struct {
	models.Dashboard
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanShare  bool `json:"can_share"`
}

type folderItemFolder struct {
	models.Folder
	CanEdit   bool `json:"can_edit"`
	CanDelete bool `json:"can_delete"`
	CanShare  bool `json:"can_share"`
}

type folderContents struct {
	Folder     *models.Folder     `json:"folder,omitempty"`
	Folders    []folderItemFolder `json:"folders"`
	Notebooks  []folderNotebook   `json:"notebooks"`
	Connectors []folderConnector  `json:"connectors"`
	Dashboards []folderDashboard  `json:"dashboards"`
}

// folderConnector is a lightweight connector listing (no encrypted credentials).
type folderConnector struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	IsDefault bool    `json:"is_default"`
	FolderID  *string `json:"folder_id,omitempty"`
	CreatedBy string  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	CanEdit   bool    `json:"can_edit"`
	CanDelete bool    `json:"can_delete"`
	CanShare  bool    `json:"can_share"`
}

// handleListHomeFolders returns all home folders for the org, grouped by owner.
// Each entry includes the home folder info and a list of sub-folders the user can access.
// @Summary List home folders
// @Description List all home folders for the current user
// @Tags folders
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /home [get]
func (s *Server) handleListHomeFolders(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	type homeFolderEntry struct {
		models.Folder
		OwnerName  string          `json:"owner_name"`
		SubFolders []models.Folder `json:"sub_folders"`
	}

	// One identity per request: group memberships (including the implicit
	// Everyone group) are resolved once and shared by every batched probe.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Get all home folders for the org with owner names
	rows, err := s.db.Pool.Query(ctx,
		`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at,
		        COALESCE(u.name, '') as owner_name
		 FROM folders f
		 JOIN users u ON u.id = f.owner_id
		 WHERE f.org_id = $1 AND f.is_home = true
		 ORDER BY f.name`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var entries []homeFolderEntry
	for rows.Next() {
		var e homeFolderEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ParentID, &e.Name, &e.IsHome,
			&e.OwnerID, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.OwnerName); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		entries = append(entries, e)
	}

	// Filter home folders: show if user owns it OR has ACL access OR has access to any content inside
	var filteredEntries []homeFolderEntry
	// Org admins bypass ACLs only when admin mode is enabled — they see all home folders
	isOrgAdmin := ident.AdminMode && ident.OrgRole == "admin"
	if isOrgAdmin {
		filteredEntries = entries
	} else {
		// Batched visibility resolution, replacing the former per-folder
		// queries: ownership is decided in Go, the direct ACL subject match
		// runs once over every candidate, and the folder-aware descendant
		// walk runs once over the remaining candidates.
		candidates := make([]string, 0, len(entries))
		owned := make(map[string]bool, len(entries))
		for _, entry := range entries {
			if entry.OwnerID != nil && *entry.OwnerID == ident.UserID {
				owned[entry.ID] = true
				continue
			}
			candidates = append(candidates, entry.ID)
		}
		aclMatch, err := s.filterFoldersWithACLSubjectMatch(ctx, candidates, ident.UserID, ident.GroupIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		remaining := make([]string, 0, len(candidates))
		for _, id := range candidates {
			if !aclMatch[id] {
				remaining = append(remaining, id)
			}
		}
		descendantMatch, err := s.filterFoldersWithVisibleDescendantContentOrFolders(ctx, ident, remaining)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		for _, entry := range entries {
			if owned[entry.ID] || aclMatch[entry.ID] || descendantMatch[entry.ID] {
				filteredEntries = append(filteredEntries, entry)
			}
		}
	}

	// For every filtered home folder, list the sub-folders the user can
	// access. The predicates are resolved in batch: one query loads all direct
	// sub-folders, then a batched ACL subject match plus the content-only
	// descendant filter decide visibility (admins skip both).
	if len(filteredEntries) > 0 {
		homeIDs := make([]string, 0, len(filteredEntries))
		for i := range filteredEntries {
			homeIDs = append(homeIDs, filteredEntries[i].ID)
		}
		subRows, err := s.db.Pool.Query(ctx,
			`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
			 FROM folders f
			 WHERE f.parent_id = ANY($1::uuid[]) AND f.org_id = $2
			 ORDER BY f.name`,
			homeIDs, claims.OrgID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		var subs []models.Folder
		for subRows.Next() {
			var sub models.Folder
			if err := subRows.Scan(&sub.ID, &sub.OrgID, &sub.ParentID, &sub.Name, &sub.IsHome,
				&sub.OwnerID, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
				subRows.Close()
				writeError(w, http.StatusInternalServerError, "scan failed")
				return
			}
			subs = append(subs, sub)
		}
		subRows.Close()
		if err := subRows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}

		visible := make(map[string]bool, len(subs))
		if isOrgAdmin {
			for _, sub := range subs {
				visible[sub.ID] = true
			}
		} else {
			candidates := make([]string, 0, len(subs))
			for _, sub := range subs {
				candidates = append(candidates, sub.ID)
			}
			aclMatch, err := s.filterFoldersWithACLSubjectMatch(ctx, candidates, ident.UserID, ident.GroupIDs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "query failed")
				return
			}
			// Only folders that are neither owned nor ACL-matched need the
			// content probe.
			remaining := make([]string, 0, len(candidates))
			for _, sub := range subs {
				ownedByCaller := sub.OwnerID != nil && *sub.OwnerID == ident.UserID
				if !ownedByCaller && !aclMatch[sub.ID] {
					remaining = append(remaining, sub.ID)
				}
			}
			contentMatch, err := s.filterFoldersWithVisibleDescendantContent(ctx, ident, remaining)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "query failed")
				return
			}
			for _, sub := range subs {
				ownedByCaller := sub.OwnerID != nil && *sub.OwnerID == ident.UserID
				visible[sub.ID] = ownedByCaller || aclMatch[sub.ID] || contentMatch[sub.ID]
			}
		}

		// Append in scan (name) order per parent so each entry keeps the
		// single-query ORDER BY f.name it had before batching.
		byParent := make(map[string][]models.Folder, len(filteredEntries))
		for _, sub := range subs {
			if visible[sub.ID] && sub.ParentID != nil {
				byParent[*sub.ParentID] = append(byParent[*sub.ParentID], sub)
			}
		}
		for i := range filteredEntries {
			filteredEntries[i].SubFolders = byParent[filteredEntries[i].ID]
		}
	}

	if filteredEntries == nil {
		filteredEntries = []homeFolderEntry{}
	}

	writeJSON(w, http.StatusOK, filteredEntries)
}

// @Summary List root contents
// @Description List folders and resources at the root level
// @Tags folders
// @Produce json
// @Success 200 {object} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /folders [get]
func (s *Server) handleListRootContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims.OrgID == "" {
		writeError(w, http.StatusForbidden, "organization required — complete onboarding first")
		return
	}
	ctx := r.Context()

	// One identity per request: group memberships (including the implicit
	// Everyone group) and admin mode are resolved once and shared by the batch
	// resolver calls below.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	contents := folderContents{
		Folders:    []folderItemFolder{},
		Notebooks:  []folderNotebook{},
		Connectors: []folderConnector{},
		Dashboards: []folderDashboard{},
	}

	isOrgAdmin := ident.AdminMode && ident.OrgRole == "admin"

	// Folders at root: include folders user can access AND has content inside, EXCLUDE is_home folders
	var folderQuery string
	var folderArgs []any
	if isOrgAdmin {
		folderQuery = `SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
			 FROM folders f
			 WHERE f.org_id = $1 AND f.parent_id IS NULL AND f.is_home = false AND f.deleted_at IS NULL
			 ORDER BY f.name`
		folderArgs = []any{claims.OrgID}
	} else {
		folderQuery = `SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
		 FROM folders f
		 WHERE f.org_id = $1 AND f.parent_id IS NULL AND f.is_home = false AND f.deleted_at IS NULL
		 AND (
		   f.owner_id = $2
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'user' AND subject_id = $2::text)
		   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'org_role' AND subject_id = 'everyone')
		   OR (
			   -- Direct accessible content in this folder
			   EXISTS (SELECT 1 FROM notebooks nb WHERE nb.folder_id = f.id AND nb.org_id = $1 AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $2::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (SELECT 1 FROM connectors c WHERE c.folder_id = f.id AND c.org_id = $1 AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $2::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (SELECT 1 FROM dashboards d2 WHERE d2.folder_id = f.id AND d2.org_id = $1 AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $2::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
		   )
		   OR (
			   -- Has accessible content at any depth in the hierarchy
			   EXISTS (
					WITH RECURSIVE desc_ids AS (
						SELECT id FROM folders WHERE parent_id = f.id AND org_id = f.org_id
						UNION ALL
						SELECT f2.id FROM folders f2
						JOIN desc_ids d ON f2.parent_id = d.id
						WHERE f2.org_id = f.org_id
					)
					SELECT 1 FROM desc_ids d
					WHERE (
						EXISTS (
							SELECT 1 FROM folders f3
							WHERE f3.id = d.id
							AND (
								f3.owner_id = $2
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f3.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f3.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f3.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
						OR EXISTS (
							SELECT 1 FROM notebooks nb
							WHERE nb.folder_id = d.id AND nb.org_id = f.org_id
							AND (
								EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
						OR EXISTS (
							SELECT 1 FROM connectors c
							WHERE c.folder_id = d.id AND c.org_id = f.org_id
							AND (
								EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
						OR EXISTS (
							SELECT 1 FROM dashboards d2
							WHERE d2.folder_id = d.id AND d2.org_id = f.org_id
							AND (
								EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $2::text)
								OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
								OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
							)
						)
					)
					LIMIT 1
			   )
		   )
		 )
		 ORDER BY f.name`
		folderArgs = []any{claims.OrgID, ident.UserID, ident.GroupIDs}
	}
	rows, err := s.db.Pool.Query(ctx, folderQuery, folderArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var rootFolders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome,
			&f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		rootFolders = append(rootFolders, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Batched edit/delete/manage flags for every listed folder. Visibility
	// itself stays decided by the query above (owner / ACL-subject predicates
	// are deliberately wider than a resolved view).
	rootFolderIDs := make([]string, 0, len(rootFolders))
	for _, f := range rootFolders {
		rootFolderIDs = append(rootFolderIDs, f.ID)
	}
	folderPerms, err := s.resolveActions(ctx, ident, "folder", rootFolderIDs, []string{"edit", "delete", "manage"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, f := range rootFolders {
		contents.Folders = append(contents.Folders, folderItemFolder{
			Folder:    f,
			CanEdit:   actionAllowed(folderPerms, f.ID, "edit"),
			CanDelete: actionAllowed(folderPerms, f.ID, "delete"),
			CanShare:  actionAllowed(folderPerms, f.ID, "manage"),
		})
	}

	// Notebooks at root (no folder) - filter by permission
	nbRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, description, connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE org_id = $1 AND folder_id IS NULL AND deleted_at IS NULL
		 ORDER BY updated_at DESC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var rootNotebooks []models.Notebook
	for nbRows.Next() {
		nb, err := scanNotebook(nbRows)
		if err != nil {
			nbRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		rootNotebooks = append(rootNotebooks, nb)
	}
	nbRows.Close()
	if err := nbRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	rootNotebookIDs := make([]string, 0, len(rootNotebooks))
	for _, nb := range rootNotebooks {
		rootNotebookIDs = append(rootNotebookIDs, nb.ID)
	}
	notebookPerms, err := s.resolveActions(ctx, ident, "notebook", rootNotebookIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, nb := range rootNotebooks {
		if actionAllowed(notebookPerms, nb.ID, "view") {
			contents.Notebooks = append(contents.Notebooks, folderNotebook{
				Notebook:  nb,
				CanEdit:   actionAllowed(notebookPerms, nb.ID, "edit"),
				CanDelete: actionAllowed(notebookPerms, nb.ID, "delete"),
				CanShare:  actionAllowed(notebookPerms, nb.ID, "share"),
			})
		}
	}

	// Connectors at root (no folder) - filter by permission
	cRows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id, COALESCE(created_by::text, ''), created_at::text, COALESCE(updated_at::text, '') FROM connectors WHERE org_id = $1 AND folder_id IS NULL AND deleted_at IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var rootConnectors []folderConnector
	for cRows.Next() {
		var c folderConnector
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			cRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		rootConnectors = append(rootConnectors, c)
	}
	cRows.Close()
	if err := cRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	rootConnectorIDs := make([]string, 0, len(rootConnectors))
	for _, c := range rootConnectors {
		rootConnectorIDs = append(rootConnectorIDs, c.ID)
	}
	connectorPerms, err := s.resolveActions(ctx, ident, "connector", rootConnectorIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, c := range rootConnectors {
		if actionAllowed(connectorPerms, c.ID, "view") {
			c.CanEdit = actionAllowed(connectorPerms, c.ID, "edit")
			c.CanDelete = actionAllowed(connectorPerms, c.ID, "delete")
			c.CanShare = actionAllowed(connectorPerms, c.ID, "share")
			contents.Connectors = append(contents.Connectors, c)
		}
	}

	// Dashboards at root (no folder) - filter by permission
	dRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 AND folder_id IS NULL AND deleted_at IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var rootDashboards []models.Dashboard
	for dRows.Next() {
		d, err := scanDashboard(dRows)
		if err != nil {
			dRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		rootDashboards = append(rootDashboards, d)
	}
	dRows.Close()
	if err := dRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	rootDashboardIDs := make([]string, 0, len(rootDashboards))
	for _, d := range rootDashboards {
		rootDashboardIDs = append(rootDashboardIDs, d.ID)
	}
	dashboardPerms, err := s.resolveActions(ctx, ident, "dashboard", rootDashboardIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, d := range rootDashboards {
		if actionAllowed(dashboardPerms, d.ID, "view") {
			contents.Dashboards = append(contents.Dashboards, folderDashboard{
				Dashboard: d,
				CanEdit:   actionAllowed(dashboardPerms, d.ID, "edit"),
				CanDelete: actionAllowed(dashboardPerms, d.ID, "delete"),
				CanShare:  actionAllowed(dashboardPerms, d.ID, "share"),
			})
		}
	}

	writeJSON(w, http.StatusOK, contents)
}

// @Summary Get folder contents
// @Description Get all items in a folder (subfolders, notebooks, connectors, dashboards)
// @Tags folders
// @Produce json
// @Param id path string true "Folder ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id} [get]
func (s *Server) handleGetFolderContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE id = $1 AND org_id = $2`,
		folderID, claims.OrgID,
	).Scan(&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	contents := folderContents{
		Folder:     &folder,
		Folders:    []folderItemFolder{},
		Notebooks:  []folderNotebook{},
		Connectors: []folderConnector{},
		Dashboards: []folderDashboard{},
	}

	// One identity per request: group memberships (including the implicit
	// Everyone group) and admin mode are resolved once and shared by the
	// batched resolution below.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Sub-folders
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE org_id = $1 AND parent_id = $2 AND deleted_at IS NULL ORDER BY name`,
		claims.OrgID, folderID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	var subFolders []models.Folder
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome,
			&f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		subFolders = append(subFolders, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Batched view/edit/delete/manage for the requested folder and every
	// child. folder.ID (the canonical scan result) is the resolution key, so
	// an uppercase path spelling still resolves like the uuid cast did.
	childFolderIDs := make([]string, 0, len(subFolders)+1)
	childFolderIDs = append(childFolderIDs, folder.ID)
	for _, f := range subFolders {
		childFolderIDs = append(childFolderIDs, f.ID)
	}
	folderPerms, err := s.resolveActions(ctx, ident, "folder", childFolderIDs, []string{"view", "edit", "delete", "manage"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}

	// Permission check: user must have view on the folder OR have access to
	// content inside (the single-folder content probe, as before). A folder
	// resolved visible is never probed.
	if !actionAllowed(folderPerms, folder.ID, "view") {
		hasContent, cErr := s.folderHasDescendantContent(ctx, folder.ID, ident.OrgID, ident.UserID, ident.GroupIDs)
		if cErr != nil || !hasContent {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	// Children that fail their own view check are probed in one batch for
	// visible descendant content (content-only, matching the former per-child
	// probe). A probe error keeps them hidden, exactly as the per-child error
	// path did.
	var hiddenChildIDs []string
	for _, f := range subFolders {
		if !actionAllowed(folderPerms, f.ID, "view") {
			hiddenChildIDs = append(hiddenChildIDs, f.ID)
		}
	}
	var descendantVisible map[string]bool
	if len(hiddenChildIDs) > 0 {
		descendantVisible, err = s.filterFoldersWithVisibleDescendantContent(ctx, ident, hiddenChildIDs)
		if err != nil {
			descendantVisible = nil
		}
	}
	for _, f := range subFolders {
		if actionAllowed(folderPerms, f.ID, "view") {
			contents.Folders = append(contents.Folders, folderItemFolder{
				Folder:    f,
				CanEdit:   actionAllowed(folderPerms, f.ID, "edit"),
				CanDelete: actionAllowed(folderPerms, f.ID, "delete"),
				CanShare:  actionAllowed(folderPerms, f.ID, "manage"),
			})
		} else if descendantVisible[f.ID] {
			// Hidden folder with accessible descendant content (deep sharing).
			contents.Folders = append(contents.Folders, folderItemFolder{Folder: f, CanEdit: false, CanDelete: false, CanShare: false})
		}
	}

	// Notebooks in folder
	nbRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, description, connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE org_id = $1 AND folder_id = $2 AND deleted_at IS NULL
		 ORDER BY updated_at DESC`,
		claims.OrgID, folderID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer nbRows.Close()
	var subNotebooks []models.Notebook
	for nbRows.Next() {
		nb, err := scanNotebook(nbRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		subNotebooks = append(subNotebooks, nb)
	}
	nbRows.Close()
	subNotebookIDs := make([]string, 0, len(subNotebooks))
	for _, nb := range subNotebooks {
		subNotebookIDs = append(subNotebookIDs, nb.ID)
	}
	notebookPerms, err := s.resolveActions(ctx, ident, "notebook", subNotebookIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, nb := range subNotebooks {
		if actionAllowed(notebookPerms, nb.ID, "view") {
			contents.Notebooks = append(contents.Notebooks, folderNotebook{
				Notebook:  nb,
				CanEdit:   actionAllowed(notebookPerms, nb.ID, "edit"),
				CanDelete: actionAllowed(notebookPerms, nb.ID, "delete"),
				CanShare:  actionAllowed(notebookPerms, nb.ID, "share"),
			})
		}
	}

	// Connectors in folder
	cRows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id, COALESCE(created_by::text, ''), created_at::text, COALESCE(updated_at::text, '') FROM connectors WHERE org_id = $1 AND folder_id = $2 AND deleted_at IS NULL`,
		claims.OrgID, folderID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer cRows.Close()
	var subConnectors []folderConnector
	for cRows.Next() {
		var c folderConnector
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		subConnectors = append(subConnectors, c)
	}
	cRows.Close()
	subConnectorIDs := make([]string, 0, len(subConnectors))
	for _, c := range subConnectors {
		subConnectorIDs = append(subConnectorIDs, c.ID)
	}
	connectorPerms, err := s.resolveActions(ctx, ident, "connector", subConnectorIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, c := range subConnectors {
		if actionAllowed(connectorPerms, c.ID, "view") {
			c.CanEdit = actionAllowed(connectorPerms, c.ID, "edit")
			c.CanDelete = actionAllowed(connectorPerms, c.ID, "delete")
			c.CanShare = actionAllowed(connectorPerms, c.ID, "share")
			contents.Connectors = append(contents.Connectors, c)
		}
	}

	// Dashboards in folder
	dRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 AND folder_id = $2 AND deleted_at IS NULL`,
		claims.OrgID, folderID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer dRows.Close()
	var subDashboards []models.Dashboard
	for dRows.Next() {
		d, err := scanDashboard(dRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		subDashboards = append(subDashboards, d)
	}
	dRows.Close()
	if err := dRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	subDashboardIDs := make([]string, 0, len(subDashboards))
	for _, d := range subDashboards {
		subDashboardIDs = append(subDashboardIDs, d.ID)
	}
	dashboardPerms, err := s.resolveActions(ctx, ident, "dashboard", subDashboardIDs, []string{"view", "edit", "delete", "share"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	for _, d := range subDashboards {
		if actionAllowed(dashboardPerms, d.ID, "view") {
			contents.Dashboards = append(contents.Dashboards, folderDashboard{
				Dashboard: d,
				CanEdit:   actionAllowed(dashboardPerms, d.ID, "edit"),
				CanDelete: actionAllowed(dashboardPerms, d.ID, "delete"),
				CanShare:  actionAllowed(dashboardPerms, d.ID, "share"),
			})
		}
	}

	writeJSON(w, http.StatusOK, contents)
}

// @Summary Get folder ancestors
// @Description Get the ancestor chain for a folder (root first, leaf last)
// @Tags folders
// @Produce json
// @Param id path string true "Folder ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id}/ancestors [get]
func (s *Server) handleGetFolderAncestors(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	// Verify folder exists in org
	var exists bool
	s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id = $1 AND org_id = $2)`,
		folderID, claims.OrgID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	groupIDs := []string{claims.UserID}
	groupRows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, claims.UserID)
	if err == nil {
		for groupRows.Next() {
			var gid string
			groupRows.Scan(&gid)
			groupIDs = append(groupIDs, gid)
		}
		groupRows.Close()
	}

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", folderID, "view"); err != nil || !allowed {
		hasContent, cErr := s.folderHasDescendantContent(ctx, folderID, claims.OrgID, claims.UserID, groupIDs)
		if cErr != nil || !hasContent {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	// Recursive CTE: start from folder itself (depth=0), walk up via parent_id
	// ORDER BY depth DESC gives root first, leaf last
	rows, err := s.db.Pool.Query(ctx,
		`WITH RECURSIVE ancestors AS (
		   SELECT id, name, parent_id, 0 AS depth
		   FROM folders WHERE id = $1 AND org_id = $2
		   UNION ALL
		   SELECT f.id, f.name, f.parent_id, a.depth + 1
		   FROM folders f
		   JOIN ancestors a ON f.id = a.parent_id
		   WHERE f.org_id = $2
		 )
		 SELECT id, name FROM ancestors ORDER BY depth DESC`,
		folderID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type ancestorItem struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var ancestors []ancestorItem
	for rows.Next() {
		var a ancestorItem
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		ancestors = append(ancestors, a)
	}
	if ancestors == nil {
		ancestors = []ancestorItem{}
	}

	writeJSON(w, http.StatusOK, ancestors)
}

// @Summary Create a folder
// @Description Create a new folder
// @Tags folders
// @Accept json
// @Produce json
// @Param request body object true "Folder details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /folders [post]
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var req struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Check create permission on the parent folder (or root-level if no parent)
	if req.ParentID != nil && *req.ParentID != "" {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", *req.ParentID, "create")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions to create folder here")
			return
		}
	}

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO folders (org_id, parent_id, name, owner_id, created_by)
		 VALUES ($1, $2, $3, $4, $4)
		 RETURNING id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at`,
		claims.OrgID, req.ParentID, req.Name, claims.UserID,
	).Scan(&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}

	// Seed ACL entry so the creator can manage this folder and create subfolders
	s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view','create','edit','manage','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, folder.ID, claims.UserID,
	)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "folder.create", ResourceType: "folder", ResourceID: folder.ID,
	})

	writeJSON(w, http.StatusCreated, folder)
}

// @Summary Update a folder
// @Description Update a folder's name or parent
// @Tags folders
// @Accept json
// @Produce json
// @Param id path string true "Folder ID"
// @Param request body object true "Folder updates"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id} [put]
func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	var rawReq map[string]json.RawMessage
	if err := decodeJSON(r, &rawReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var name *string
	if nameRaw, ok := rawReq["name"]; ok {
		json.Unmarshal(nameRaw, &name)
	}

	setParentID := false
	var parentID *string
	if parentRaw, ok := rawReq["parent_id"]; ok {
		setParentID = true
		json.Unmarshal(parentRaw, &parentID)
	}

	if name == nil && !setParentID {
		writeError(w, http.StatusBadRequest, "name or parent_id required")
		return
	}

	// Cycle guard: ensure the new parent is not the folder itself or a descendant.
	if setParentID && parentID != nil {
		var isCycle bool
		err := s.db.Pool.QueryRow(ctx, `
			WITH RECURSIVE desc AS (
				SELECT id FROM folders WHERE id = $1
				UNION ALL
				SELECT f.id FROM folders f
				JOIN desc d ON f.parent_id = d.id
				WHERE f.org_id = $3
			)
			SELECT EXISTS(SELECT 1 FROM desc WHERE id = $2)
		`, folderID, *parentID, claims.OrgID).Scan(&isCycle)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cycle check failed")
			return
		}
		if isCycle {
			writeError(w, http.StatusBadRequest, "cannot move folder under itself or a descendant")
			return
		}
	}

	// Build dynamic SET clause
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *name)
		argIdx++
	}
	if setParentID {
		setClauses = append(setClauses, fmt.Sprintf("parent_id = $%d", argIdx))
		args = append(args, parentID)
		argIdx++
	}

	args = append(args, folderID, claims.OrgID)
	query := fmt.Sprintf(
		`UPDATE folders SET %s WHERE id = $%d AND org_id = $%d
		 RETURNING id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at`,
		strings.Join(setClauses, ", "), argIdx, argIdx+1,
	)

	var folder models.Folder
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&folder.ID, &folder.OrgID, &folder.ParentID, &folder.Name, &folder.IsHome,
		&folder.OwnerID, &folder.CreatedBy, &folder.CreatedAt, &folder.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update folder")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "folder.update", ResourceType: "folder", ResourceID: folderID,
	})

	writeJSON(w, http.StatusOK, folder)
}

// @Summary Ensure home folder
// @Description Create the current user's home folder if it doesn't exist
// @Tags folders
// @Produce json
// @Success 200 {object} map[string]string
// @Success 201 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /users/me/home [post]
func (s *Server) handleEnsureHomeFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var userName string
	if err := s.db.Pool.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&userName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch user")
		return
	}

	// Get user's org membership
	var orgID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT om.org_id FROM org_members om WHERE om.user_id = $1 ORDER BY om.created_at LIMIT 1`,
		claims.UserID,
	).Scan(&orgID)
	if err != nil {
		writeError(w, http.StatusForbidden, "user is not a member of any org")
		return
	}

	// Check if home folder already exists
	var existingID string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true LIMIT 1`,
		claims.UserID,
	).Scan(&existingID)
	if err == nil {
		// Home folder already exists
		writeJSON(w, http.StatusOK, map[string]string{"id": existingID})
		return
	}
	if err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Create the home folder
	if err := createHomeFolder(ctx, s.db.Pool, orgID, claims.UserID, userName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create home folder")
		return
	}

	// Fetch the newly created folder ID
	var folderID string
	s.db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true LIMIT 1`,
		claims.UserID,
	).Scan(&folderID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": folderID})
}

// @Summary Delete a folder
// @Description Delete a folder and all its contents
// @Tags folders
// @Param id path string true "Folder ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /folders/{id} [delete]
func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"
	ctx := r.Context()

	// Verify folder exists in org
	var exists bool
	s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM folders WHERE id = $1 AND org_id = $2)`,
		folderID, claims.OrgID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	if !force {
		// Check if any children exist (scoped to org to avoid cross-org false positives)
		var hasChildren bool
		s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(
			   SELECT 1 FROM folders WHERE parent_id = $1 AND org_id = $2 AND deleted_at IS NULL
			   UNION ALL
			   SELECT 1 FROM notebooks WHERE folder_id = $1 AND org_id = $2 AND deleted_at IS NULL
			   UNION ALL
			   SELECT 1 FROM connectors WHERE folder_id = $1 AND org_id = $2 AND deleted_at IS NULL
			   UNION ALL
			   SELECT 1 FROM dashboards WHERE folder_id = $1 AND org_id = $2 AND deleted_at IS NULL
			 )`,
			folderID, claims.OrgID,
		).Scan(&hasChildren)
		if hasChildren {
			writeError(w, http.StatusConflict, "folder is not empty; use ?force=true to delete recursively")
			return
		}
	}

	result, err := s.db.Pool.Exec(ctx,
		`UPDATE folders SET deleted_at = NOW() WHERE id = $1 AND org_id = $2`,
		folderID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "folder.delete", ResourceType: "folder", ResourceID: folderID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// scanNotebook scans a notebook row (id, org_id, title, description, connector_id, parameters,
// created_by, created_at, updated_at).
func scanNotebook(rows interface {
	Scan(dest ...any) error
}) (models.Notebook, error) {
	var nb models.Notebook
	var connectorID *string
	var paramsJSON []byte
	var description *string
	if err := rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &description,
		&connectorID, &paramsJSON, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt); err != nil {
		return nb, err
	}
	if description != nil {
		nb.Description = *description
	}
	if connectorID != nil {
		nb.ConnectorID = *connectorID
	}
	if paramsJSON != nil {
		json.Unmarshal(paramsJSON, &nb.Parameters)
	}
	if nb.Parameters == nil {
		nb.Parameters = []models.Parameter{}
	}
	return nb, nil
}

// scanDashboard scans a dashboard row (id, org_id, title, settings,
// created_by, created_at, updated_at).
func scanDashboard(rows interface {
	Scan(dest ...any) error
}) (models.Dashboard, error) {
	var d models.Dashboard
	var settingsJSON []byte
	if err := rows.Scan(&d.ID, &d.OrgID, &d.Title, &settingsJSON,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return d, err
	}
	if settingsJSON != nil {
		json.Unmarshal(settingsJSON, &d.Settings)
	}
	return d, nil
}
