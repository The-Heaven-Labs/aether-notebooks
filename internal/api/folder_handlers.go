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

	// Folders at root
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, parent_id, name, is_home, owner_id, created_by, created_at, updated_at
		 FROM folders WHERE org_id = $1 AND parent_id IS NULL ORDER BY name`,
		claims.OrgID,
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
	rows.Close()

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
	for nbRows.Next() {
		nb, err := scanNotebook(nbRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Notebooks = append(contents.Notebooks, nb)
	}
	nbRows.Close()

	// Connectors at root
	cRows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id FROM connectors WHERE org_id = $1 AND folder_id IS NULL`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer cRows.Close()
	for cRows.Next() {
		var c folderConnector
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Connectors = append(contents.Connectors, c)
	}
	cRows.Close()

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
	for dRows.Next() {
		d, err := scanDashboard(dRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Dashboards = append(contents.Dashboards, d)
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
	for rows.Next() {
		var f models.Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.IsHome,
			&f.OwnerID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Folders = append(contents.Folders, f)
	}
	rows.Close()

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
	for nbRows.Next() {
		nb, err := scanNotebook(nbRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Notebooks = append(contents.Notebooks, nb)
	}
	nbRows.Close()

	// Connectors in folder
	cRows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, type, is_default, folder_id FROM connectors WHERE org_id = $1 AND folder_id = $2`,
		claims.OrgID, folderID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer cRows.Close()
	for cRows.Next() {
		var c folderConnector
		if err := cRows.Scan(&c.ID, &c.Name, &c.Type, &c.IsDefault, &c.FolderID); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Connectors = append(contents.Connectors, c)
	}
	cRows.Close()

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
	for dRows.Next() {
		d, err := scanDashboard(dRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		contents.Dashboards = append(contents.Dashboards, d)
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
		`INSERT INTO folders (org_id, parent_id, name, created_by)
		 VALUES ($1, $2, $3, $4)
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
		// Check if any children exist
		var hasChildren bool
		s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(
			   SELECT 1 FROM folders WHERE parent_id = $1
			   UNION ALL
			   SELECT 1 FROM notebooks WHERE folder_id = $1
			   UNION ALL
			   SELECT 1 FROM connectors WHERE folder_id = $1
			   UNION ALL
			   SELECT 1 FROM dashboards WHERE folder_id = $1
			 )`,
			folderID,
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
