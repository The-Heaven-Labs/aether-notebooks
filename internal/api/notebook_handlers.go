package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
)

type createCellInput struct {
	Type     string `json:"type"`
	Language string `json:"language,omitempty"`
	Source   string `json:"source"`
}

type createNotebookRequest struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Parameters  []models.Parameter `json:"parameters"`
	FolderID    *string            `json:"folder_id,omitempty"`
	Cells       []createCellInput  `json:"cells,omitempty"`
}

// @Summary Create a notebook
// @Description Create a new notebook. Optionally include cells to populate the notebook with content in one request.
// @Tags notebooks
// @Accept json
// @Produce json
// @Param request body object true "Notebook details (title required; cells optional with type, language, source)"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks [post]
func (s *Server) handleCreateNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	params, _ := json.Marshal(req.Parameters)
	if req.Parameters == nil {
		params = []byte("[]")
	}

	ctx := r.Context()
	var nb models.Notebook
	var paramsOut []byte
	var folderID *string
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, description, parameters, created_by, folder_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, org_id, title, COALESCE(description,''), parameters, created_by, created_at, updated_at, folder_id`,
		claims.OrgID, req.Title, req.Description, params, claims.UserID, req.FolderID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &paramsOut, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt, &folderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notebook")
		return
	}
	json.Unmarshal(paramsOut, &nb.Parameters)
	nb.FolderID = folderID

	// Seed ACL entries for the creator and org admins
	_, aclErr := s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view','run','edit','share','delete','create']),
		        ($1, 'notebook', $2::uuid, 'org_role', 'admin', ARRAY['view','run','edit','share','delete','create'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, nb.ID, claims.UserID,
	)
	if aclErr != nil {
		// Log but don't fail the request — the notebook was created successfully
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "acl.seed_error", ResourceType: "notebook", ResourceID: nb.ID,
		})
	}

	if nb.ConnectorID == "" {
		var defaultID string
		err := s.db.Pool.QueryRow(ctx,
			`SELECT id FROM connectors WHERE org_id=$1 AND is_default=true LIMIT 1`,
			claims.OrgID,
		).Scan(&defaultID)
		if err == nil {
			_, _ = s.db.Pool.Exec(ctx,
				`UPDATE notebooks SET connector_id=$1 WHERE id=$2`,
				defaultID, nb.ID,
			)
			nb.ConnectorID = defaultID
		}
	}

	var createdCells []models.Cell
	for i, cell := range req.Cells {
		if cell.Type != "code" && cell.Type != "text" {
			continue
		}
		lang := cell.Language
		if lang == "" {
			if cell.Type == "text" {
				lang = "markdown"
			} else {
				lang = "sql"
			}
		}

		inserted := models.Cell{}
		var cellLang, cellConnID *string
		var cellOutputs, cellParams []byte
		err := s.db.Pool.QueryRow(ctx,
			`INSERT INTO cells (notebook_id, position, type, language, source, outputs)
			 VALUES ($1, $2, $3, $4, $5, '[]')
			 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
			           source_visible, outputs_hidden, cell_collapsed, slide_break, parameters,
			           COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), "limit",
			           COALESCE(metadata, '{}'), created_at, updated_at`,
			nb.ID, i, cell.Type, lang, cell.Source,
		).Scan(&inserted.ID, &inserted.NotebookID, &inserted.Position, &inserted.Type,
			&cellLang, &cellConnID, &inserted.Source, &cellOutputs,
			&inserted.SourceVisible, &inserted.OutputsHidden, &inserted.CellCollapsed, &inserted.SlideBreak, &cellParams,
			&inserted.Title, &inserted.Description, &inserted.Slug, &inserted.Limit,
			&inserted.Metadata, &inserted.CreatedAt, &inserted.UpdatedAt)
		if err != nil {
			continue
		}
		if cellLang != nil {
			inserted.Language = *cellLang
		}
		json.Unmarshal(cellOutputs, &inserted.Outputs)
		json.Unmarshal(cellParams, &inserted.Parameters)
		if inserted.Outputs == nil {
			inserted.Outputs = []models.Output{}
		}
		createdCells = append(createdCells, inserted)
	}
	if createdCells == nil {
		createdCells = []models.Cell{}
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.create", ResourceType: "notebook", ResourceID: nb.ID,
	})

	if len(req.Cells) > 0 {
		writeJSON(w, http.StatusCreated, map[string]any{
			"notebook": nb,
			"cells":    createdCells,
		})
	} else {
		writeJSON(w, http.StatusCreated, nb)
	}
}

// @Summary List notebooks
// @Description List all notebooks for the current organization
// @Tags notebooks
// @Produce json
// @Success 200 {array} models.Notebook
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks [get]
func (s *Server) handleListNotebooks(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at, folder_id
		 FROM notebooks WHERE org_id = $1 AND deleted_at IS NULL ORDER BY updated_at DESC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var notebooks []models.Notebook
	for rows.Next() {
		var nb models.Notebook
		var params []byte
		var connID *string
		var folderID *string
		if err := rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &connID, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt, &folderID); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if connID != nil {
			nb.ConnectorID = *connID
		}
		nb.FolderID = folderID
		json.Unmarshal(params, &nb.Parameters)
		allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nb.ID, "view")
		if !allowed {
			continue
		}
		notebooks = append(notebooks, nb)
	}

	if notebooks == nil {
		notebooks = []models.Notebook{}
	}

	writeJSON(w, http.StatusOK, notebooks)
}

// @Summary Get a notebook
// @Description Get a notebook with all its cells
// @Tags notebooks
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id} [get]
func (s *Server) handleGetNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	ctx := r.Context()
	var (
		nb         models.Notebook
		params     []byte
		connID     *string
		folderID   *string
		ownerName  string
		ownerEmail string
	)
	err := s.db.Pool.QueryRow(ctx,
		`SELECT n.id, n.org_id, n.title, COALESCE(n.description,''), n.connector_id, n.parameters,
		        n.created_by, n.created_at, n.updated_at, n.folder_id,
		        COALESCE(u.name, ''), COALESCE(u.email, '')
		 FROM notebooks n
		 LEFT JOIN users u ON u.id = n.created_by
		 WHERE n.id = $1 AND n.org_id = $2`,
		nbID, claims.OrgID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &connID, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt, &folderID,
		&ownerName, &ownerEmail)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if connID != nil {
		nb.ConnectorID = *connID
	}
	nb.FolderID = folderID
	json.Unmarshal(params, &nb.Parameters)

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "view")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit")
	runOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "run")
	shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "share")

	cellRows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, position, type, language, connector_id, source, outputs,
		        source_visible, outputs_hidden, cell_collapsed, slide_break, parameters, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), "limit",
		        COALESCE(metadata, '{}'), duration_ms, created_at, updated_at, agent_updated_at
		 FROM cells WHERE notebook_id = $1 ORDER BY position ASC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query cells failed")
		return
	}
	defer cellRows.Close()

	var cells []models.Cell
	for cellRows.Next() {
		var c models.Cell
		var lang, connID *string
		var outputs, cellParams []byte
		var cellLimit *int
		var durationMs *int
		var agentUpdatedAt *time.Time
		if err := cellRows.Scan(&c.ID, &c.NotebookID, &c.Position, &c.Type, &lang, &connID, &c.Source, &outputs,
			&c.SourceVisible, &c.OutputsHidden, &c.CellCollapsed, &c.SlideBreak, &cellParams, &c.Title, &c.Description, &c.Slug, &cellLimit,
			&c.Metadata, &durationMs, &c.CreatedAt, &c.UpdatedAt, &agentUpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan cell failed")
			return
		}
		if lang != nil {
			c.Language = *lang
		}
		if connID != nil {
			c.ConnectorID = *connID
		}
		if cellLimit != nil {
			c.Limit = cellLimit
		}
		c.AgentUpdatedAt = agentUpdatedAt
		c.DurationMs = durationMs
		json.Unmarshal(outputs, &c.Outputs)
		json.Unmarshal(cellParams, &c.Parameters)
		cells = append(cells, c)
	}

	type notebookWithCells struct {
		models.Notebook
		Cells      []models.Cell `json:"cells"`
		OwnerName  string        `json:"owner_name"`
		OwnerEmail string        `json:"owner_email"`
		CanEdit    bool          `json:"can_edit"`
		CanRun     bool          `json:"can_run"`
		CanShare   bool          `json:"can_share"`
	}

	resp := notebookWithCells{
		Notebook:   nb,
		Cells:      cells,
		OwnerName:  ownerName,
		OwnerEmail: ownerEmail,
		CanEdit:    editOK,
		CanRun:     runOK,
		CanShare:   shareOK,
	}
	if resp.Cells == nil {
		resp.Cells = []models.Cell{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// @Summary Get notebook permissions
// @Description Get the current user's permissions for a notebook
// @Tags notebooks
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 200 {object} map[string]bool
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id}/permissions [get]
func (s *Server) handleGetNotebookPermissions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	ctx := r.Context()

	var notebookOrgID string
	if err := s.db.Pool.QueryRow(ctx, "SELECT org_id FROM notebooks WHERE id=$1", nbID).Scan(&notebookOrgID); err != nil || notebookOrgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	viewOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "view")
	if !viewOK {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit")
	runOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "run")

	writeJSON(w, http.StatusOK, map[string]bool{
		"can_edit": editOK,
		"can_run":  runOK,
	})
}

// @Summary Delete a notebook
// @Description Delete a notebook by ID
// @Tags notebooks
// @Param id path string true "Notebook ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id} [delete]
func (s *Server) handleDeleteNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE notebooks SET deleted_at = NOW() WHERE id = $1 AND org_id = $2`,
		nbID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.delete", ResourceType: "notebook", ResourceID: nbID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// @Summary List trash
// @Description List all trashed items (notebooks, connectors, dashboards)
// @Tags notebooks
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /trash [get]
func (s *Server) handleListTrash(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	type trashItem struct {
		ID        string    `json:"id"`
		Type      string    `json:"type"`
		Name      string    `json:"name"`
		DeletedAt time.Time `json:"deleted_at"`
	}

	var items []trashItem

	nRows, err := s.db.Pool.Query(ctx, `SELECT id, title, deleted_at FROM notebooks WHERE org_id = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, claims.OrgID)
	if err == nil {
		for nRows.Next() {
			var item trashItem
			if err := nRows.Scan(&item.ID, &item.Name, &item.DeletedAt); err == nil {
				item.Type = "notebook"
				allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", item.ID, "view")
				if allowed {
					items = append(items, item)
				}
			}
		}
		nRows.Close()
	}

	cRows, err := s.db.Pool.Query(ctx, `SELECT id, name, deleted_at FROM connectors WHERE org_id = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, claims.OrgID)
	if err == nil {
		for cRows.Next() {
			var item trashItem
			if err := cRows.Scan(&item.ID, &item.Name, &item.DeletedAt); err == nil {
				item.Type = "connector"
				allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", item.ID, "view")
				if allowed {
					items = append(items, item)
				}
			}
		}
		cRows.Close()
	}

	dRows, err := s.db.Pool.Query(ctx, `SELECT id, title, deleted_at FROM dashboards WHERE org_id = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, claims.OrgID)
	if err == nil {
		for dRows.Next() {
			var item trashItem
			if err := dRows.Scan(&item.ID, &item.Name, &item.DeletedAt); err == nil {
				item.Type = "dashboard"
				allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", item.ID, "view")
				if allowed {
					items = append(items, item)
				}
			}
		}
		dRows.Close()
	}

	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.After(items[j].DeletedAt) })
	writeJSON(w, http.StatusOK, items)
}

// @Summary Restore from trash
// @Description Restore a trashed notebook, connector, or dashboard
// @Tags notebooks
// @Accept json
// @Produce json
// @Param request body object true "Restore details with type and id"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /trash/restore [post]
func (s *Server) handleRestoreFromTrash(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var req struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, req.Type, req.ID, "edit")
	if !allowed {
		writeError(w, http.StatusForbidden, "no permission to restore this resource")
		return
	}

	var table string
	switch req.Type {
	case "notebook":
		table = "notebooks"
	case "connector":
		table = "connectors"
	case "dashboard":
		table = "dashboards"
	default:
		writeError(w, http.StatusBadRequest, "invalid type")
		return
	}

	result, err := s.db.Pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET deleted_at = NULL WHERE id = $1 AND org_id = $2`, table), req.ID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: req.Type + ".restore", ResourceType: req.Type, ResourceID: req.ID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

type updateNotebookRequest struct {
	Title       *string            `json:"title,omitempty"`
	Description *string            `json:"description,omitempty"`
	ConnectorID *string            `json:"connector_id"`
	Parameters  []models.Parameter `json:"parameters,omitempty"`
	FolderID    json.RawMessage    `json:"folder_id"`
}

// @Summary Update a notebook
// @Description Update a notebook's title, description, or connector
// @Tags notebooks
// @Accept json
// @Produce json
// @Param id path string true "Notebook ID"
// @Param request body object true "Updates"
// @Success 200 {object} models.Notebook
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id} [put]
func (s *Server) handleUpdateNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	var req updateNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == nil && req.Description == nil && req.ConnectorID == nil && req.Parameters == nil && len(req.FolderID) == 0 {
		writeError(w, http.StatusBadRequest, "at least one field must be provided")
		return
	}

	ctx := r.Context()
	query := "UPDATE notebooks SET updated_at = NOW()"
	args := []any{}
	argN := 1

	if req.Title != nil {
		query += fmt.Sprintf(", title = $%d", argN)
		args = append(args, *req.Title)
		argN++
	}
	if req.Description != nil {
		query += fmt.Sprintf(", description = $%d", argN)
		args = append(args, *req.Description)
		argN++
	}
	if req.ConnectorID != nil {
		if *req.ConnectorID == "" {
			query += ", connector_id = NULL"
		} else {
			query += fmt.Sprintf(", connector_id = $%d", argN)
			args = append(args, *req.ConnectorID)
			argN++
		}
	}
	if req.Parameters != nil {
		paramsJSON, _ := json.Marshal(req.Parameters)
		query += fmt.Sprintf(", parameters = $%d", argN)
		args = append(args, paramsJSON)
		argN++
	}
	if len(req.FolderID) > 0 {
		if string(req.FolderID) == "null" {
			query += ", folder_id = NULL"
		} else {
			var fid string
			if err := json.Unmarshal(req.FolderID, &fid); err == nil {
				query += fmt.Sprintf(", folder_id = $%d", argN)
				args = append(args, fid)
				argN++
			}
		}
	}

	query += fmt.Sprintf(" WHERE id = $%d AND org_id = $%d", argN, argN+1)
	args = append(args, nbID, claims.OrgID)
	query += " RETURNING id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at, folder_id"

	var nb models.Notebook
	var paramsOut []byte
	var retConnID *string
	var retFolderID *string
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &retConnID, &paramsOut, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt, &retFolderID,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if retConnID != nil {
		nb.ConnectorID = *retConnID
	}
	nb.FolderID = retFolderID
	json.Unmarshal(paramsOut, &nb.Parameters)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.update", ResourceType: "notebook", ResourceID: nb.ID,
	})

	writeJSON(w, http.StatusOK, nb)
}

// @Summary Export a notebook
// @Description Export a notebook as .ipynb format
// @Tags notebooks
// @Produce application/json
// @Param id path string true "Notebook ID"
// @Success 200 {string} string "Jupyter notebook JSON"
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id}/export [get]
func (s *Server) handleExportNotebook(w http.ResponseWriter, r *http.Request) {
	notebookID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	if allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "notebook", notebookID, "view"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var title string
	err := s.db.Pool.QueryRow(r.Context(), `SELECT title FROM notebooks WHERE id = $1 AND org_id = $2`, notebookID, claims.OrgID).Scan(&title)
	if err != nil {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	rows, err := s.db.Pool.Query(r.Context(), `SELECT type, language, source, position FROM cells WHERE notebook_id = $1 ORDER BY position`, notebookID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get cells")
		return
	}
	defer rows.Close()

	type jupyterCell struct {
		CellType       string         `json:"cell_type"`
		Source         []string       `json:"source"`
		Metadata       map[string]any `json:"metadata"`
		Outputs        []any          `json:"outputs"`
		ExecutionCount *int           `json:"execution_count,omitempty"`
	}

	var cells []jupyterCell
	for rows.Next() {
		var cellType, lang, source string
		var pos int
		if err := rows.Scan(&cellType, &lang, &source, &pos); err != nil {
			continue
		}
		lines := strings.Split(source, "\n")
		// Add newline to all but last line (Jupyter format)
		for i := 0; i < len(lines)-1; i++ {
			lines[i] += "\n"
		}
		jc := jupyterCell{
			CellType: cellType,
			Source:   lines,
			Metadata: map[string]any{},
			Outputs:  []any{},
		}
		if cellType == "code" {
			jc.Metadata["language"] = lang
			ec := 0
			jc.ExecutionCount = &ec
		}
		cells = append(cells, jc)
	}
	if cells == nil {
		cells = []jupyterCell{}
	}

	notebook := map[string]any{
		"nbformat":       4,
		"nbformat_minor": 5,
		"metadata": map[string]any{
			"title": title,
			"kernelspec": map[string]any{
				"display_name": "SQL",
				"language":     "sql",
				"name":         "sql",
			},
		},
		"cells": cells,
	}

	safeTitle := strings.Map(func(r rune) rune {
		if r == '"' || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, title)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.ipynb"`, safeTitle))
	json.NewEncoder(w).Encode(notebook)
}

// @Summary Import a notebook
// @Description Import a notebook from .ipynb format
// @Tags notebooks
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Jupyter notebook file"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/import [post]
func (s *Server) handleImportNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	var jupyter struct {
		Metadata struct {
			Title string `json:"title"`
		} `json:"metadata"`
		Cells []struct {
			CellType string         `json:"cell_type"`
			Source   any            `json:"source"` // can be string or []string
			Metadata map[string]any `json:"metadata"`
		} `json:"cells"`
	}
	if err := json.NewDecoder(file).Decode(&jupyter); err != nil {
		writeError(w, http.StatusBadRequest, "invalid .ipynb file: "+err.Error())
		return
	}

	title := jupyter.Metadata.Title
	if title == "" {
		title = "Imported Notebook"
	}

	folderID := r.FormValue("folder_id")

	ctx := r.Context()
	var notebookID string
	if folderID != "" {
		err = s.db.Pool.QueryRow(ctx,
			`INSERT INTO notebooks (org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4) RETURNING id`,
			claims.OrgID, title, claims.UserID, folderID).Scan(&notebookID)
	} else {
		err = s.db.Pool.QueryRow(ctx, `INSERT INTO notebooks (org_id, title, created_by) VALUES ($1, $2, $3) RETURNING id`,
			claims.OrgID, title, claims.UserID).Scan(&notebookID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notebook")
		return
	}

	for i, jc := range jupyter.Cells {
		cellType := jc.CellType
		if cellType == "" {
			cellType = "code"
		}

		var source string
		switch src := jc.Source.(type) {
		case string:
			source = src
		case []any:
			for _, line := range src {
				if str, ok := line.(string); ok {
					source += str
				}
			}
		}
		// Remove trailing newlines from joined lines
		source = strings.TrimRight(source, "\n")

		lang := "sql"
		if jc.Metadata != nil {
			if l, ok := jc.Metadata["language"].(string); ok {
				lang = l
			}
		}
		if cellType == "markdown" {
			lang = "markdown"
		}

		s.db.Pool.Exec(ctx, `INSERT INTO cells (notebook_id, type, language, source, position) VALUES ($1, $2, $3, $4, $5)`,
			notebookID, cellType, lang, source, i)
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.import", ResourceType: "notebook", ResourceID: notebookID,
	})

	writeJSON(w, http.StatusCreated, map[string]string{"id": notebookID, "title": title})
}

type cloneNotebookRequest struct {
	Title    *string `json:"title,omitempty"`
	FolderID *string `json:"folder_id,omitempty"`
}

// @Summary Clone a notebook
// @Description Create a copy of a notebook with all its cells (without outputs)
// @Tags notebooks
// @Accept json
// @Produce json
// @Param id path string true "Source notebook ID"
// @Param request body object false "Optional new title and folder"
// @Success 201 {object} map[string]any
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id}/clone [post]
func (s *Server) handleCloneNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	sourceID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", sourceID, "view")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req cloneNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	var srcTitle, srcDesc string
	var srcParams []byte
	var srcConnID *string
	var srcFolderID *string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT title, COALESCE(description,''), parameters, connector_id, folder_id
		 FROM notebooks WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		sourceID, claims.OrgID,
	).Scan(&srcTitle, &srcDesc, &srcParams, &srcConnID, &srcFolderID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	newTitle := srcTitle + " (copy)"
	if req.Title != nil && *req.Title != "" {
		newTitle = *req.Title
	}
	newFolderID := srcFolderID
	if req.FolderID != nil {
		newFolderID = req.FolderID
	}

	var newNB models.Notebook
	var paramsOut []byte
	var retFolderID *string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, description, parameters, created_by, connector_id, folder_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, org_id, title, COALESCE(description,''), parameters, created_by, created_at, updated_at, folder_id`,
		claims.OrgID, newTitle, srcDesc, srcParams, claims.UserID, srcConnID, newFolderID,
	).Scan(&newNB.ID, &newNB.OrgID, &newNB.Title, &newNB.Description, &paramsOut, &newNB.CreatedBy, &newNB.CreatedAt, &newNB.UpdatedAt, &retFolderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notebook")
		return
	}
	json.Unmarshal(paramsOut, &newNB.Parameters)
	newNB.FolderID = retFolderID
	if srcConnID != nil {
		newNB.ConnectorID = *srcConnID
	}

	cellRows, err := s.db.Pool.Query(ctx,
		`SELECT type, language, connector_id, source, source_visible, outputs_hidden,
		        cell_collapsed, slide_break, parameters, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		        "limit", COALESCE(metadata, '{}')
		 FROM cells WHERE notebook_id = $1 ORDER BY position ASC`,
		sourceID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read source cells")
		return
	}
	defer cellRows.Close()

	var newCells []models.Cell
	pos := 0
	for cellRows.Next() {
		var cellType, source string
		var lang, connID, title, desc, slug *string
		var sourceVisible, outputsHidden, cellCollapsed, slideBreak bool
		var cellParams, metadata []byte
		var limit *int

		if err := cellRows.Scan(&cellType, &lang, &connID, &source,
			&sourceVisible, &outputsHidden, &cellCollapsed, &slideBreak, &cellParams,
			&title, &desc, &slug, &limit, &metadata); err != nil {
			continue
		}

		var newCell models.Cell
		var newParams []byte
		var newLimit *int
		err = s.db.Pool.QueryRow(ctx,
			`INSERT INTO cells (notebook_id, position, type, language, connector_id, source, outputs,
			                  source_visible, outputs_hidden, cell_collapsed, slide_break, parameters,
			                  title, description, slug, "limit", metadata)
			 VALUES ($1,$2,$3,$4,$5,$6,'[]',$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
			           source_visible, outputs_hidden, cell_collapsed, slide_break, parameters,
			           COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), "limit",
			           COALESCE(metadata, '{}'), created_at, updated_at`,
			newNB.ID, pos, cellType, lang, connID, source,
			sourceVisible, outputsHidden, cellCollapsed, slideBreak, cellParams,
			title, desc, slug, limit, metadata,
		).Scan(&newCell.ID, &newCell.NotebookID, &newCell.Position, &newCell.Type,
			&lang, &connID, &newCell.Source, &newCell.Outputs,
			&newCell.SourceVisible, &newCell.OutputsHidden, &newCell.CellCollapsed, &newCell.SlideBreak, &newParams,
			&newCell.Title, &newCell.Description, &newCell.Slug, &newLimit,
			&newCell.Metadata, &newCell.CreatedAt, &newCell.UpdatedAt)
		if err != nil {
			continue
		}
		if lang != nil {
			newCell.Language = *lang
		}
		if connID != nil {
			newCell.ConnectorID = *connID
		}
		if newLimit != nil {
			newCell.Limit = newLimit
		}
		json.Unmarshal(newParams, &newCell.Parameters)
		if newCell.Outputs == nil {
			newCell.Outputs = []models.Output{}
		}
		newCells = append(newCells, newCell)
		pos++
	}

	if newCells == nil {
		newCells = []models.Cell{}
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.clone", ResourceType: "notebook", ResourceID: newNB.ID,
		Metadata: map[string]any{"source_id": sourceID},
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"notebook": newNB,
		"cells":    newCells,
	})
}

// @Summary Share a notebook
// @Description Create or get a public share link for a notebook
// @Tags notebooks
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 200 {object} map[string]any
// @Success 201 {object} map[string]any
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id}/share [post]
func (s *Server) handleShareNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "share")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var token, createdBy string
	var createdAt time.Time
	err = s.db.Pool.QueryRow(ctx,
		`SELECT token, created_by, created_at FROM public_tokens WHERE resource_type='notebook' AND resource_id=$1`,
		nbID,
	).Scan(&token, &createdBy, &createdAt)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "created_by": createdBy, "created_at": createdAt})
		return
	}

	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token = hex.EncodeToString(tokenBytes)
	createdAt = time.Now()

	result, err := s.db.Pool.Exec(ctx,
		`INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_by)
		 SELECT $1, 'notebook', $2, $3, $4
		 WHERE (SELECT public_sharing_enabled FROM orgs WHERE id = $1) = true`,
		claims.OrgID, nbID, token, claims.UserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusForbidden, "public sharing is disabled for this organization")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.share", ResourceType: "notebook", ResourceID: nbID,
	})

	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "created_by": claims.UserID, "created_at": createdAt})
}

// @Summary Get notebook share link
// @Description Get the public share link for a notebook, if one exists
// @Tags notebooks
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 200 {object} map[string]any
// @Success 204
// @Security BearerAuth
// @Router /notebooks/{id}/share [get]
func (s *Server) handleGetNotebookShare(w http.ResponseWriter, r *http.Request) {
	// Requires "view" permission — anyone who can see the notebook can see the link
	nbID := r.PathValue("id")
	ctx := r.Context()

	// Verify the resource exists and belongs to the user's org
	claims := ClaimsFromContext(r.Context())
	var exists bool
	s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)`, nbID, claims.OrgID).Scan(&exists)
	if !exists {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var token, createdBy string
	var createdAt time.Time
	err := s.db.Pool.QueryRow(ctx,
		`SELECT token, created_by, created_at FROM public_tokens WHERE resource_type='notebook' AND resource_id=$1`,
		nbID,
	).Scan(&token, &createdBy, &createdAt)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"token": token, "created_by": createdBy, "created_at": createdAt})
}

// @Summary Revoke notebook share link
// @Description Revoke the public share link for a notebook
// @Tags notebooks
// @Param id path string true "Notebook ID"
// @Success 204
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id}/share [delete]
func (s *Server) handleRevokeNotebookShare(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "share")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`DELETE FROM public_tokens WHERE resource_type='notebook' AND resource_id=$1`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke share link")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.share_revoke", ResourceType: "notebook", ResourceID: nbID,
	})
	w.WriteHeader(http.StatusNoContent)
}
