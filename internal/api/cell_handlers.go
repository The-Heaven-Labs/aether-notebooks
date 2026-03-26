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
}

type updateCellRequest struct {
	Source        *string `json:"source,omitempty"`
	Language      *string `json:"language,omitempty"`
	ConnectorID   *string `json:"connector_id,omitempty"`
	Type          *string `json:"type,omitempty"`
	SourceVisible *bool   `json:"source_visible,omitempty"`
	CellCollapsed *bool   `json:"cell_collapsed,omitempty"`
	Title         *string `json:"title,omitempty"`
	Description   *string `json:"description,omitempty"`
	Slug          *string `json:"slug,omitempty"`
}

func (s *Server) handleCreateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")

	var req createCellRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Type != models.CellTypeCode && req.Type != models.CellTypeText {
		writeError(w, http.StatusBadRequest, "type must be 'code' or 'text'")
		return
	}

	ctx := r.Context()

	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	var maxPos *int
	s.db.Pool.QueryRow(ctx, "SELECT MAX(position) FROM cells WHERE notebook_id=$1", nbID).Scan(&maxPos)
	nextPos := 0
	if maxPos != nil {
		nextPos = *maxPos + 1
	}

	var cell models.Cell
	var lang, connID *string
	if req.Language != "" {
		lang = &req.Language
	}
	if req.ConnectorID != "" {
		connID = &req.ConnectorID
	}

	var outputs []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO cells (notebook_id, position, type, language, connector_id, source, outputs)
		 VALUES ($1, $2, $3, $4, $5, $6, '[]')
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
		           source_visible, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		           created_at, updated_at`,
		nbID, nextPos, req.Type, lang, connID, req.Source,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID, &cell.Source, &outputs,
		&cell.SourceVisible, &cell.CellCollapsed, &cell.Title, &cell.Description, &cell.Slug,
		&cell.CreatedAt, &cell.UpdatedAt)
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
	json.Unmarshal(outputs, &cell.Outputs)

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

	var req updateCellRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

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

	query += fmt.Sprintf(" WHERE id = $%d AND notebook_id = $%d", argN, argN+1)
	args = append(args, cellID, nbID)
	query += " RETURNING id, notebook_id, position, type, language, connector_id, source, outputs, source_visible, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), created_at, updated_at"

	var cell models.Cell
	var lang, connID *string
	var outputs []byte
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cell.SourceVisible, &cell.CellCollapsed,
		&cell.Title, &cell.Description, &cell.Slug,
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
	json.Unmarshal(outputs, &cell.Outputs)

	if req.Source != nil {
		s.upsertCellVersion(ctx, cellID, *req.Source)
	}

	writeJSON(w, http.StatusOK, cell)
}

func (s *Server) handleDeleteCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")

	ctx := r.Context()

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

func nilIfEmptyStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
