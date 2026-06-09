package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type createCellRequest struct {
	Type        models.CellType `json:"type"`
	Language    string          `json:"language,omitempty"`
	ConnectorID string          `json:"connector_id,omitempty"`
	Source      string          `json:"source"`
	Position    *int            `json:"position,omitempty"`
}

type updateCellRequest struct {
	Source        *string            `json:"source,omitempty"`
	Language      *string            `json:"language,omitempty"`
	ConnectorID   *string            `json:"connector_id,omitempty"`
	Type          *string            `json:"type,omitempty"`
	SourceVisible *bool              `json:"source_visible,omitempty"`
	CellCollapsed *bool              `json:"cell_collapsed,omitempty"`
	SlideBreak    *bool              `json:"slide_break,omitempty"`
	Parameters    []models.Parameter `json:"parameters,omitempty"`
	Title         *string            `json:"title,omitempty"`
	Description   *string            `json:"description,omitempty"`
	Slug          *string            `json:"slug,omitempty"`
	Limit         *int               `json:"limit,omitempty"`
	Metadata      json.RawMessage    `json:"metadata,omitempty"`
}

func (s *Server) handleCreateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "create"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "no permission to create cells in this notebook")
		return
	}

	var req createCellRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Type != models.CellTypeCode && req.Type != models.CellTypeText {
		writeError(w, http.StatusBadRequest, "type must be 'code' or 'text'")
		return
	}

	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	var insertPos int
	if req.Position != nil {
		insertPos = *req.Position
		// Shift to negative positions first to avoid UNIQUE(notebook_id, position) violations
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE cells SET position = -position - 1 WHERE notebook_id = $1 AND position >= $2`,
			nbID, insertPos,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE cells SET position = -position WHERE notebook_id = $1 AND position < 0`,
			nbID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	} else {
		var maxPos *int
		s.db.Pool.QueryRow(ctx, "SELECT MAX(position) FROM cells WHERE notebook_id=$1", nbID).Scan(&maxPos)
		insertPos = 0
		if maxPos != nil {
			insertPos = *maxPos + 1
		}
	}

	var cell models.Cell
	var lang, connID *string
	if req.Language != "" {
		lang = &req.Language
	}
	if req.ConnectorID != "" {
		connID = &req.ConnectorID
	}

	var outputs, cellParams []byte
	var limit *int
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO cells (notebook_id, position, type, language, connector_id, source, outputs)
		 VALUES ($1, $2, $3, $4, $5, $6, '[]')
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
		           source_visible, cell_collapsed, slide_break, parameters, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), "limit",
		           COALESCE(metadata, '{}'), created_at, updated_at`,
		nbID, insertPos, req.Type, lang, connID, req.Source,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID, &cell.Source, &outputs,
		&cell.SourceVisible, &cell.CellCollapsed, &cell.SlideBreak, &cellParams, &cell.Title, &cell.Description, &cell.Slug,
		&limit, &cell.Metadata, &cell.CreatedAt, &cell.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create cell")
		return
	}
	if lang != nil {
		cell.Language = *lang
	}
	if connID != nil {
		cell.ConnectorID = *connID
	}
	if limit != nil {
		cell.Limit = limit
	}
	json.Unmarshal(outputs, &cell.Outputs)
	json.Unmarshal(cellParams, &cell.Parameters)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.create", ResourceType: "cell", ResourceID: cell.ID,
	})

	writeJSON(w, http.StatusCreated, cell)
}

func (s *Server) handleUpdateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	ctx := r.Context()

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "no permission to edit cells in this notebook")
		return
	}

	var req updateCellRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	query := "UPDATE cells SET updated_at = NOW()"
	args := []interface{}{}
	argN := 1

	if req.Type != nil {
		query += fmt.Sprintf(", type = $%d", argN)
		args = append(args, *req.Type)
		argN++
	}
	if req.Source != nil {
		query += fmt.Sprintf(", source = $%d", argN)
		args = append(args, *req.Source)
		argN++
	}
	if req.Language != nil {
		query += fmt.Sprintf(", language = $%d", argN)
		args = append(args, *req.Language)
		argN++
	}
	if req.ConnectorID != nil {
		query += fmt.Sprintf(", connector_id = $%d", argN)
		args = append(args, *req.ConnectorID)
		argN++
	}
	if req.SourceVisible != nil {
		query += fmt.Sprintf(", source_visible = $%d", argN)
		args = append(args, *req.SourceVisible)
		argN++
	}
	if req.CellCollapsed != nil {
		query += fmt.Sprintf(", cell_collapsed = $%d", argN)
		args = append(args, *req.CellCollapsed)
		argN++
	}
	if req.SlideBreak != nil {
		query += fmt.Sprintf(", slide_break = $%d", argN)
		args = append(args, *req.SlideBreak)
		argN++
	}
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
	if req.Slug != nil {
		query += fmt.Sprintf(", slug = $%d", argN)
		args = append(args, nilIfEmptyStr(*req.Slug))
		argN++
	}
	if req.Parameters != nil {
		paramsJSON, _ := json.Marshal(req.Parameters)
		query += fmt.Sprintf(", parameters = $%d", argN)
		args = append(args, paramsJSON)
		argN++
	}
	if req.Limit != nil {
		query += fmt.Sprintf(", \"limit\" = $%d", argN)
		args = append(args, *req.Limit)
		argN++
	}

	if req.Metadata != nil {
		query += fmt.Sprintf(", metadata = $%d", argN)
		args = append(args, req.Metadata)
		argN++
	}

	query += fmt.Sprintf(" WHERE id = $%d AND notebook_id = $%d", argN, argN+1)
	args = append(args, cellID, nbID)
	query += " RETURNING id, notebook_id, position, type, language, connector_id, source, outputs, source_visible, cell_collapsed, slide_break, parameters, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), \"limit\", COALESCE(metadata, '{}'), created_at, updated_at"

	var cell models.Cell
	var lang, connID *string
	var outputs, cellParams []byte
	var limit *int
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cell.SourceVisible, &cell.CellCollapsed, &cell.SlideBreak, &cellParams,
		&cell.Title, &cell.Description, &cell.Slug, &limit, &cell.Metadata,
		&cell.CreatedAt, &cell.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if lang != nil {
		cell.Language = *lang
	}
	if connID != nil {
		cell.ConnectorID = *connID
	}
	if limit != nil {
		cell.Limit = limit
	}
	json.Unmarshal(outputs, &cell.Outputs)
	json.Unmarshal(cellParams, &cell.Parameters)

	if req.Source != nil {
		s.upsertCellVersion(ctx, cellID, *req.Source, claims.UserID)
	}

	// Log cell type change in version history and audit trail
	if req.Type != nil {
		typeNote := fmt.Sprintf("[type changed to %s]", *req.Type)
		_ = s.upsertCellVersion(ctx, cellID, typeNote, claims.UserID)
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "cell.type_change", ResourceType: "cell", ResourceID: cellID,
			Metadata: map[string]any{"new_type": *req.Type},
		})
	}
	if req.Source != nil {
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "cell.update", ResourceType: "cell", ResourceID: cellID,
		})
	}

	writeJSON(w, http.StatusOK, cell)
}

func (s *Server) handleDeleteCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	ctx := r.Context()

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "no permission to delete cells from this notebook")
		return
	}

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM cells WHERE id = $1 AND notebook_id = $2
		 AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $3)`,
		cellID, nbID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.delete", ResourceType: "cell", ResourceID: cellID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDuplicateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	ctx := r.Context()

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "create"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "no permission to duplicate cells in this notebook")
		return
	}

	var src models.Cell
	var outputs, params []byte
	var lang, connID, title, desc, slug *string
	var limit *int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, notebook_id, position, type, language, connector_id, source, outputs,
		        source_visible, cell_collapsed, slide_break, parameters,
		        COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), "limit",
		        COALESCE(metadata, '{}')
		 FROM cells WHERE id=$1 AND notebook_id=$2`,
		cellID, nbID,
	).Scan(&src.ID, &src.NotebookID, &src.Position, &src.Type,
		&lang, &connID, &src.Source, &outputs,
		&src.SourceVisible, &src.CellCollapsed, &src.SlideBreak, &params,
		&title, &desc, &slug, &limit, &src.Metadata)
	if err != nil {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	var orgID string
	s.db.Pool.QueryRow(ctx, `SELECT org_id FROM notebooks WHERE id=$1`, nbID).Scan(&orgID)
	if orgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	insertPos := src.Position + 1
	// Shift to negative positions first to avoid UNIQUE(notebook_id, position) violations
	if _, err := s.db.Pool.Exec(ctx,
		`UPDATE cells SET position = -position - 1 WHERE notebook_id = $1 AND position >= $2`,
		nbID, insertPos,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if _, err := s.db.Pool.Exec(ctx,
		`UPDATE cells SET position = -position WHERE notebook_id = $1 AND position < 0`,
		nbID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	var newCell models.Cell
	var newOutputs, newParams []byte
	var newLimit *int
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO cells (notebook_id, position, type, language, connector_id, source, outputs,
		                    source_visible, cell_collapsed, slide_break, parameters, title, description, slug, "limit", metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,'[]',$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
		           source_visible, cell_collapsed, slide_break, parameters,
		           COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), "limit",
		           COALESCE(metadata, '{}'), created_at, updated_at`,
		nbID, insertPos, src.Type, lang, connID, src.Source,
		src.SourceVisible, src.CellCollapsed, src.SlideBreak, params, title, desc, slug, limit, src.Metadata,
	).Scan(&newCell.ID, &newCell.NotebookID, &newCell.Position, &newCell.Type,
		&lang, &connID, &newCell.Source, &newOutputs,
		&newCell.SourceVisible, &newCell.CellCollapsed, &newCell.SlideBreak, &newParams,
		&newCell.Title, &newCell.Description, &newCell.Slug, &newLimit,
		&newCell.Metadata, &newCell.CreatedAt, &newCell.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to duplicate cell")
		return
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
	json.Unmarshal(newOutputs, &newCell.Outputs)
	json.Unmarshal(newParams, &newCell.Parameters)
	if newCell.Outputs == nil {
		newCell.Outputs = []models.Output{}
	}

	writeJSON(w, http.StatusCreated, newCell)
}

func nilIfEmptyStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
