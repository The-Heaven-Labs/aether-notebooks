package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
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

// folderHasDescendantContent checks if any notebook, connector, or dashboard
// directly in folderID or in any descendant folder has an ACL entry granting access to the user.
func (s *Server) folderHasDescendantContent(ctx context.Context, folderID, orgID, userID string, groupIDs []string) (bool, error) {
	var has bool
	err := s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM notebooks nb
			WHERE nb.folder_id = $1 AND nb.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		) OR EXISTS (
			SELECT 1 FROM connectors c
			WHERE c.folder_id = $1 AND c.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		) OR EXISTS (
			SELECT 1 FROM dashboards d2
			WHERE d2.folder_id = $1 AND d2.org_id = $2
			AND (
				EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
				OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			)
			LIMIT 1
		) OR EXISTS (
			WITH RECURSIVE desc_ids AS (
				SELECT id FROM folders WHERE parent_id = $1 AND org_id = $2
				UNION ALL
				SELECT f.id FROM folders f
				JOIN desc_ids d ON f.parent_id = d.id
				WHERE f.org_id = $2
			)
			SELECT 1 FROM desc_ids d
			WHERE (
				EXISTS (
					SELECT 1 FROM notebooks nb
					WHERE nb.folder_id = d.id AND nb.org_id = $2
					AND (
						EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
						OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
						OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
					)
				)
				OR EXISTS (
					SELECT 1 FROM connectors c
					WHERE c.folder_id = d.id AND c.org_id = $2
					AND (
						EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
						OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
						OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
					)
				)
				OR EXISTS (
					SELECT 1 FROM dashboards d2
					WHERE d2.folder_id = d.id AND d2.org_id = $2
					AND (
						EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
						OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
						OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
					)
				)
			)
			LIMIT 1
		)`,
		folderID, orgID, userID, groupIDs,
	).Scan(&has)
	return has, err
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

	// Collect user's group memberships
	groupRows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var groupIDs []string
	for groupRows.Next() {
		var gid string
		if err := groupRows.Scan(&gid); err != nil {
			groupRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		groupIDs = append(groupIDs, gid)
	}
	groupRows.Close()

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
	for _, entry := range entries {
		// Check if user owns this home folder
		if entry.OwnerID != nil && *entry.OwnerID == claims.UserID {
			filteredEntries = append(filteredEntries, entry)
			continue
		}

		// Check if user has direct ACL access on this home folder (explicit entries only)
		var hasACL bool
		s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM acl_entries
				WHERE resource_type = 'folder' AND resource_id = $1
				AND (
					(subject_type = 'user' AND subject_id = $2::text)
					OR (subject_type = 'group' AND subject_id = ANY($3::text[]))
					OR (subject_type = 'org_role' AND subject_id = 'everyone')
				)
				LIMIT 1
			)`,
			entry.ID, claims.UserID, groupIDs,
		).Scan(&hasACL)
		if hasACL {
			filteredEntries = append(filteredEntries, entry)
			continue
		}

		// Check if user has access to any content inside this home folder at any depth
		var hasAccess bool
		err = s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS (
				WITH RECURSIVE desc_ids AS (
					SELECT id FROM folders WHERE parent_id = $1 AND org_id = $2
					UNION ALL
					SELECT f.id FROM folders f
					JOIN desc_ids d ON f.parent_id = d.id
					WHERE f.org_id = $2
				)
				SELECT 1 FROM desc_ids d
				WHERE (
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
					)
					OR EXISTS (
						SELECT 1 FROM connectors c
						WHERE c.folder_id = d.id AND c.org_id = $2
						AND (
							EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
							OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
							OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
						)
					)
					OR EXISTS (
						SELECT 1 FROM dashboards d2
						WHERE d2.folder_id = d.id AND d2.org_id = $2
						AND (
							EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
							OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
							OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
						)
					)
				)
				LIMIT 1
			)`,
			entry.ID, claims.OrgID, claims.UserID, groupIDs,
		).Scan(&hasAccess)
		if err != nil {
			continue
		}
		if hasAccess {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// For each filtered home folder, get sub-folders the user can access
	for i := range filteredEntries {
		subRows, err := s.db.Pool.Query(ctx,
			`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
			 FROM folders f
			 WHERE f.parent_id = $1 AND f.org_id = $2
			 AND (
			   f.owner_id = $3
			   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'user' AND subject_id = $3::text)
			   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
			   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'org_role' AND subject_id = 'everyone')
		   OR (
			   -- Direct accessible content in this folder
			   EXISTS (SELECT 1 FROM notebooks nb WHERE nb.folder_id = f.id AND nb.org_id = f.org_id AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (SELECT 1 FROM connectors c WHERE c.folder_id = f.id AND c.org_id = f.org_id AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (SELECT 1 FROM dashboards d2 WHERE d2.folder_id = f.id AND d2.org_id = f.org_id AND (
				   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
				   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
				   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
			   ) LIMIT 1)
			   OR EXISTS (
				   WITH RECURSIVE desc_ids AS (
					   SELECT id FROM folders WHERE parent_id = f.id AND org_id = f.org_id
					   UNION ALL
					   SELECT c.id FROM folders c
					   JOIN desc_ids d ON c.parent_id = d.id
					   WHERE c.org_id = f.org_id
				   )
				   SELECT 1 FROM desc_ids d
				   WHERE (
					   EXISTS (
						   SELECT 1 FROM notebooks nb
						   WHERE nb.folder_id = d.id AND nb.org_id = f.org_id
						   AND (
							   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'user' AND subject_id = $3::text)
							   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'notebook' AND resource_id = nb.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
							   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = nb.id AND subject_type = 'org_role' AND subject_id = 'everyone')
						   )
					   )
					   OR EXISTS (
						   SELECT 1 FROM connectors c
						   WHERE c.folder_id = d.id AND c.org_id = f.org_id
						   AND (
							   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'user' AND subject_id = $3::text)
							   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'connector' AND resource_id = c.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
							   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'connector' AND resource_id = c.id AND subject_type = 'org_role' AND subject_id = 'everyone')
						   )
					   )
					   OR EXISTS (
						   SELECT 1 FROM dashboards d2
						   WHERE d2.folder_id = d.id AND d2.org_id = f.org_id
						   AND (
							   EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'user' AND subject_id = $3::text)
							   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'dashboard' AND resource_id = d2.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($4::text[]))
							   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'dashboard' AND resource_id = d2.id AND subject_type = 'org_role' AND subject_id = 'everyone')
						   )
					   )
				   )
				   LIMIT 1
			   )
		   )
			 )
			 ORDER BY f.name`,
			filteredEntries[i].ID, claims.OrgID, claims.UserID, groupIDs,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		for subRows.Next() {
			var sub models.Folder
			if err := subRows.Scan(&sub.ID, &sub.OrgID, &sub.ParentID, &sub.Name, &sub.IsHome,
				&sub.OwnerID, &sub.CreatedBy, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
				subRows.Close()
				writeError(w, http.StatusInternalServerError, "scan failed")
				return
			}
			filteredEntries[i].SubFolders = append(filteredEntries[i].SubFolders, sub)
		}
		subRows.Close()
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
	ctx := r.Context()

	contents := folderContents{
		Folders:    []folderItemFolder{},
		Notebooks:  []folderNotebook{},
		Connectors: []folderConnector{},
		Dashboards: []folderDashboard{},
	}

	// Collect user's group memberships for permission checks
	groupRows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var groupIDs []string
	for groupRows.Next() {
		var gid string
		if err := groupRows.Scan(&gid); err != nil {
			groupRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		groupIDs = append(groupIDs, gid)
	}
	groupRows.Close()

	// Folders at root: include folders user can access AND has content inside, EXCLUDE is_home folders
	rows, err := s.db.Pool.Query(ctx,
		`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
		 FROM folders f
		 WHERE f.org_id = $1 AND f.parent_id IS NULL AND f.is_home = false
		 AND (
		   f.owner_id = $2
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'user' AND subject_id = $2::text)
		   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'org_role' AND subject_id = 'everyone')
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
		 ORDER BY f.name`,
		claims.OrgID, claims.UserID, groupIDs,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome,
			&f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "edit")
		deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "delete")
		shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "manage")
		contents.Folders = append(contents.Folders, folderItemFolder{Folder: f, CanEdit: editOK, CanDelete: deleteOK, CanShare: shareOK})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
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
	defer nbRows.Close()
	for nbRows.Next() {
		nb, err := scanNotebook(nbRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "view")
		if err == nil && ok {
			editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "edit")
			deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "delete")
			shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "share")
			contents.Notebooks = append(contents.Notebooks, folderNotebook{Notebook: nb, CanEdit: editOK, CanDelete: deleteOK, CanShare: shareOK})
		}
	}
	if err := nbRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
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
	defer cRows.Close()
	for cRows.Next() {
		var c folderConnector
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if err == nil && ok {
			c.CanEdit, _ = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "edit")
			c.CanDelete, _ = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "delete")
			c.CanShare, _ = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "share")
			contents.Connectors = append(contents.Connectors, c)
		}
	}
	if err := cRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Dashboards at root (no folder) - filter by permission
	dRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, public_token, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 AND folder_id IS NULL AND deleted_at IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer dRows.Close()
	for dRows.Next() {
		d, err := scanDashboard(dRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "view")
		if err == nil && ok {
			editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "edit")
			deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "delete")
			shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "share")
			contents.Dashboards = append(contents.Dashboards, folderDashboard{Dashboard: d, CanEdit: editOK, CanDelete: deleteOK, CanShare: shareOK})
		}
	}
	if err := dRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
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

	// Collect user's group memberships for descendant content checks
	groupRows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	var groupIDs []string
	for groupRows.Next() {
		var gid string
		if err := groupRows.Scan(&gid); err != nil {
			groupRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		groupIDs = append(groupIDs, gid)
	}
	groupRows.Close()

	// Permission check: user must have view on the folder OR have access to content inside
	pOK, pErr := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", folderID, "view")
	if pErr != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !pOK {
		hasContent, cErr := s.folderHasDescendantContent(ctx, folderID, claims.OrgID, claims.UserID, groupIDs)
		if cErr != nil || !hasContent {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
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
	for _, f := range subFolders {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "view")
		if err == nil && ok {
			editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "edit")
			deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "delete")
			shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", f.ID, "manage")
			contents.Folders = append(contents.Folders, folderItemFolder{Folder: f, CanEdit: editOK, CanDelete: deleteOK, CanShare: shareOK})
		} else {
			// Check if the folder has accessible descendant content (deep sharing)
			hasDescendant, dErr := s.folderHasDescendantContent(ctx, f.ID, claims.OrgID, claims.UserID, groupIDs)
			if dErr == nil && hasDescendant {
				contents.Folders = append(contents.Folders, folderItemFolder{Folder: f, CanEdit: false, CanDelete: false, CanShare: false})
			}
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
	for _, nb := range subNotebooks {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "view")
		if err == nil && ok {
			editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "edit")
			deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "delete")
			shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "share")
			contents.Notebooks = append(contents.Notebooks, folderNotebook{Notebook: nb, CanEdit: editOK, CanDelete: deleteOK, CanShare: shareOK})
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
	for _, c := range subConnectors {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if err == nil && ok {
			c.CanEdit, _ = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "edit")
			c.CanDelete, _ = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "delete")
			c.CanShare, _ = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "share")
			contents.Connectors = append(contents.Connectors, c)
		}
	}

	// Dashboards in folder
	dRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, public_token, created_by, created_at, updated_at
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
	for _, d := range subDashboards {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "view")
		if err == nil && ok {
			editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "edit")
			deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "delete")
			shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "share")
			contents.Dashboards = append(contents.Dashboards, folderDashboard{Dashboard: d, CanEdit: editOK, CanDelete: deleteOK, CanShare: shareOK})
		}
	}

	writeJSON(w, http.StatusOK, contents)
}

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

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "folder", folderID, "view"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
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

	writeJSON(w, http.StatusOK, folder)
}

// handleEnsureHomeFolder creates the user's home folder if it doesn't already exist.
// This allows home folder creation to be triggered via API without requiring login.
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
// @Success 200
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
	if err := rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description,
		&connectorID, &paramsJSON, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt); err != nil {
		return nb, err
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

// scanDashboard scans a dashboard row (id, org_id, title, settings, public_token,
// created_by, created_at, updated_at).
func scanDashboard(rows interface {
	Scan(dest ...any) error
}) (models.Dashboard, error) {
	var d models.Dashboard
	var settingsJSON []byte
	if err := rows.Scan(&d.ID, &d.OrgID, &d.Title, &settingsJSON, &d.PublicToken,
		&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return d, err
	}
	if settingsJSON != nil {
		json.Unmarshal(settingsJSON, &d.Settings)
	}
	return d, nil
}
