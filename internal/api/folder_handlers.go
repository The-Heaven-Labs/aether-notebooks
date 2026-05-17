package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type folderContents struct {
	Folder     *models.Folder     `json:"folder,omitempty"`
	Folders    []models.Folder    `json:"folders"`
	Notebooks  []models.Notebook  `json:"notebooks"`
	Connectors []folderConnector  `json:"connectors"`
	Dashboards []models.Dashboard `json:"dashboards"`
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
}

// handleListHomeFolders returns all home folders for the org, grouped by owner.
// Each entry includes the home folder info and a list of sub-folders the user can access.
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

	// For each home folder, get sub-folders the user can access
	for i := range entries {
		subRows, err := s.db.Pool.Query(ctx,
			`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
			 FROM folders f
			 WHERE f.parent_id = $1
			 AND (
			   f.owner_id = $2
			   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'user' AND subject_id = $2::text)
			   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
			 )
			 ORDER BY f.name`,
			entries[i].ID, claims.UserID, groupIDs,
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
			entries[i].SubFolders = append(entries[i].SubFolders, sub)
		}
		subRows.Close()
	}

	if entries == nil {
		entries = []homeFolderEntry{}
	}

	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleListRootContents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	contents := folderContents{
		Folders:    []models.Folder{},
		Notebooks:  []models.Notebook{},
		Connectors: []folderConnector{},
		Dashboards: []models.Dashboard{},
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

	// Folders at root: include folders user can access, but EXCLUDE is_home folders (those appear under /home)
	rows, err := s.db.Pool.Query(ctx,
		`SELECT f.id, f.org_id, f.parent_id, f.name, f.is_home, f.owner_id, f.created_by, f.created_at, f.updated_at
		 FROM folders f
		 WHERE f.org_id = $1 AND f.parent_id IS NULL AND f.is_home = false
		 AND (
		   f.owner_id = $2
		   OR EXISTS (SELECT 1 FROM acl_entries WHERE resource_type = 'folder' AND resource_id = f.id AND subject_type = 'user' AND subject_id = $2::text)
		   OR EXISTS (SELECT 1 FROM acl_entries ae WHERE resource_type = 'folder' AND resource_id = f.id AND ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
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
		contents.Folders = append(contents.Folders, f)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Notebooks at root
	nbRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, description, connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE org_id = $1 AND folder_id IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer nbRows.Close()
	var allNotebooks []models.Notebook
	for nbRows.Next() {
		nb, err := scanNotebook(nbRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		allNotebooks = append(allNotebooks, nb)
	}
	nbRows.Close()
	for _, nb := range allNotebooks {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "view")
		if err == nil && ok {
			contents.Notebooks = append(contents.Notebooks, nb)
		}
	}

	// Connectors at root
	cRows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id, COALESCE(created_by::text, ''), created_at::text FROM connectors WHERE org_id = $1 AND folder_id IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer cRows.Close()
	var allConnectors []folderConnector
	for cRows.Next() {
		var c folderConnector
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID, &c.CreatedBy, &c.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		allConnectors = append(allConnectors, c)
	}
	cRows.Close()
	for _, c := range allConnectors {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if err == nil && ok {
			contents.Connectors = append(contents.Connectors, c)
		}
	}

	// Dashboards at root
	dRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, public_token, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 AND folder_id IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer dRows.Close()
	var allDashboards []models.Dashboard
	for dRows.Next() {
		d, err := scanDashboard(dRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		allDashboards = append(allDashboards, d)
	}
	for _, d := range allDashboards {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "view")
		if err == nil && ok {
			contents.Dashboards = append(contents.Dashboards, d)
		}
	}

	writeJSON(w, http.StatusOK, contents)
}

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
		Folders:    []models.Folder{},
		Notebooks:  []models.Notebook{},
		Connectors: []folderConnector{},
		Dashboards: []models.Dashboard{},
	}

	// Sub-folders
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE org_id = $1 AND parent_id = $2 ORDER BY name`,
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
			contents.Folders = append(contents.Folders, f)
		}
	}

	// Notebooks in folder
	nbRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, description, connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE org_id = $1 AND folder_id = $2`,
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
			contents.Notebooks = append(contents.Notebooks, nb)
		}
	}

	// Connectors in folder
	cRows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id, COALESCE(created_by::text, ''), created_at::text FROM connectors WHERE org_id = $1 AND folder_id = $2`,
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
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID, &c.CreatedBy, &c.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		subConnectors = append(subConnectors, c)
	}
	cRows.Close()
	for _, c := range subConnectors {
		ok, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if err == nil && ok {
			contents.Connectors = append(contents.Connectors, c)
		}
	}

	// Dashboards in folder
	dRows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, public_token, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 AND folder_id = $2`,
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
			contents.Dashboards = append(contents.Dashboards, d)
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

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	folderID := r.PathValue("id")
	ctx := r.Context()

	var req struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.ParentID == nil {
		writeError(w, http.StatusBadRequest, "name or parent_id required")
		return
	}

	// Cycle guard: ensure the new parent is not the folder itself or a descendant.
	if req.ParentID != nil {
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
		`, folderID, *req.ParentID, claims.OrgID).Scan(&isCycle)
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

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.ParentID != nil {
		setClauses = append(setClauses, fmt.Sprintf("parent_id = $%d", argIdx))
		args = append(args, *req.ParentID)
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
			   SELECT 1 FROM folders WHERE parent_id = $1 AND org_id = $2
			   UNION ALL
			   SELECT 1 FROM notebooks WHERE folder_id = $1 AND org_id = $2
			   UNION ALL
			   SELECT 1 FROM connectors WHERE folder_id = $1 AND org_id = $2
			   UNION ALL
			   SELECT 1 FROM dashboards WHERE folder_id = $1 AND org_id = $2
			 )`,
			folderID, claims.OrgID,
		).Scan(&hasChildren)
		if hasChildren {
			writeError(w, http.StatusConflict, "folder is not empty; use ?force=true to delete recursively")
			return
		}
	}

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM folders WHERE id = $1 AND org_id = $2`,
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
