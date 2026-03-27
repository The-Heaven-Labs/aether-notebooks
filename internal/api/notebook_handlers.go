package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type createNotebookRequest struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Parameters  []models.Parameter `json:"parameters"`
}

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

	params, _ := json.Marshal(req.Parameters)
	if req.Parameters == nil {
		params = []byte("[]")
	}

	ctx := r.Context()
	var nb models.Notebook
	var paramsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, description, parameters, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, title, COALESCE(description,''), parameters, created_by, created_at, updated_at`,
		claims.OrgID, req.Title, req.Description, params, claims.UserID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &paramsOut, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notebook")
		return
	}
	json.Unmarshal(paramsOut, &nb.Parameters)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.create", ResourceType: "notebook", ResourceID: nb.ID,
	})

	writeJSON(w, http.StatusCreated, nb)
}

func (s *Server) handleListNotebooks(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE org_id = $1 ORDER BY updated_at DESC`,
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
		if err := rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &connID, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if connID != nil {
			nb.ConnectorID = *connID
		}
		json.Unmarshal(params, &nb.Parameters)
		notebooks = append(notebooks, nb)
	}

	if notebooks == nil {
		notebooks = []models.Notebook{}
	}

	writeJSON(w, http.StatusOK, notebooks)
}

func (s *Server) handleGetNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	ctx := r.Context()
	var nb models.Notebook
	var params []byte
	var connID *string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at
		 FROM notebooks WHERE id = $1 AND org_id = $2`,
		nbID, claims.OrgID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &connID, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
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
	json.Unmarshal(params, &nb.Parameters)

	cellRows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, position, type, language, connector_id, source, outputs,
		        source_visible, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		        created_at, updated_at
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
		var outputs []byte
		if err := cellRows.Scan(&c.ID, &c.NotebookID, &c.Position, &c.Type, &lang, &connID, &c.Source, &outputs,
			&c.SourceVisible, &c.CellCollapsed, &c.Title, &c.Description, &c.Slug,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan cell failed")
			return
		}
		if lang != nil {
			c.Language = *lang
		}
		if connID != nil {
			c.ConnectorID = *connID
		}
		json.Unmarshal(outputs, &c.Outputs)
		cells = append(cells, c)
	}

	type notebookWithCells struct {
		models.Notebook
		Cells []models.Cell `json:"cells"`
	}

	resp := notebookWithCells{Notebook: nb, Cells: cells}
	if resp.Cells == nil {
		resp.Cells = []models.Cell{}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM notebooks WHERE id = $1 AND org_id = $2`,
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

type updateNotebookRequest struct {
	Title       *string            `json:"title,omitempty"`
	Description *string            `json:"description,omitempty"`
	ConnectorID *string            `json:"connector_id"`
	Parameters  []models.Parameter `json:"parameters,omitempty"`
}

func (s *Server) handleUpdateNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	var req updateNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == nil && req.Description == nil && req.ConnectorID == nil && req.Parameters == nil {
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

	query += fmt.Sprintf(" WHERE id = $%d AND org_id = $%d", argN, argN+1)
	args = append(args, nbID, claims.OrgID)
	query += " RETURNING id, org_id, title, COALESCE(description,''), connector_id, parameters, created_by, created_at, updated_at"

	var nb models.Notebook
	var paramsOut []byte
	var retConnID *string
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &retConnID, &paramsOut, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt,
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
	json.Unmarshal(paramsOut, &nb.Parameters)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.update", ResourceType: "notebook", ResourceID: nb.ID,
	})

	writeJSON(w, http.StatusOK, nb)
}
