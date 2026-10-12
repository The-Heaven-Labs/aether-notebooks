package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/models"
)

// @Summary Get public resource
// @Description Returns a publicly shared notebook or dashboard by token
// @Tags public
// @Produce json
// @Param token path string true "Public sharing token"
// @Success 200 {object} map[string]any
// @Failure 404 {object} map[string]string
// @Router /api/v1/public/{token} [get]
func (s *Server) handlePublicResource(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	ctx := r.Context()

	var resourceType, resourceID, orgID string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT pt.resource_type, pt.resource_id, pt.org_id
		 FROM public_tokens pt
		 JOIN orgs o ON o.id = pt.org_id
		 WHERE pt.token = $1 AND o.public_sharing_enabled = true`,
		token,
	).Scan(&resourceType, &resourceID, &orgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "resource not found or sharing disabled")
		return
	}

	switch resourceType {
	case "notebook":
		s.servePublicNotebook(w, r, resourceID, orgID)
	case "dashboard":
		s.servePublicDashboard(w, r, resourceID)
	default:
		writeError(w, http.StatusNotFound, "unknown resource type")
	}
}

// @Summary Get public attachment
// @Description Stream an org attachment (app logo) through a public share token
// @Tags public
// @Produce application/octet-stream
// @Param token path string true "Public sharing token"
// @Param id path string true "Attachment ID"
// @Success 200 {file} binary
// @Failure 404 {object} map[string]string
// @Router /api/v1/public/{token}/attachments/{id} [get]
func (s *Server) handlePublicAttachment(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	attID := r.PathValue("id")
	ctx := r.Context()

	if !isValidUUID(attID) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	// The token's org must own the attachment, so a token never exposes another
	// org's files, and public sharing must still be enabled for that org.
	var mimeType, filename string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT a.mime_type, a.filename
		 FROM attachments a
		 JOIN public_tokens pt ON pt.org_id = a.org_id
		 JOIN orgs o ON o.id = pt.org_id
		 WHERE a.id = $1 AND pt.token = $2 AND o.public_sharing_enabled = true`,
		attID, token,
	).Scan(&mimeType, &filename)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	s.serveAttachment(w, attID, mimeType, filename)
}

func (s *Server) servePublicNotebook(w http.ResponseWriter, r *http.Request, nbID, orgID string) {
	ctx := r.Context()

	var nb models.Notebook
	var params []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, title, COALESCE(description,''), parameters, created_at, updated_at
		 FROM notebooks WHERE id=$1 AND org_id=$2`,
		nbID, orgID,
	).Scan(&nb.ID, &nb.Title, &nb.Description, &params, &nb.CreatedAt, &nb.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	json.Unmarshal(params, &nb.Parameters)

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, position, type, language, source, outputs, parameters, metadata, outputs_hidden
		 FROM cells WHERE notebook_id=$1 ORDER BY position ASC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type publicCell struct {
		ID            string                 `json:"id"`
		Position      int                    `json:"position"`
		Type          string                 `json:"type"`
		Language      string                 `json:"language,omitempty"`
		Source        string                 `json:"source"`
		Outputs       json.RawMessage        `json:"outputs"`
		Parameters    []models.Parameter     `json:"parameters"`
		Metadata      map[string]interface{} `json:"metadata"`
		OutputsHidden bool                   `json:"outputs_hidden"`
	}
	var cells []publicCell
	for rows.Next() {
		var c publicCell
		var lang *string
		var outputs, cellParams, meta []byte
		if err := rows.Scan(&c.ID, &c.Position, &c.Type, &lang, &c.Source, &outputs, &cellParams, &meta, &c.OutputsHidden); err != nil {
			continue
		}
		if lang != nil {
			c.Language = *lang
		}
		c.Outputs = outputs
		json.Unmarshal(cellParams, &c.Parameters)
		json.Unmarshal(meta, &c.Metadata)
		if len(c.Outputs) == 0 {
			c.Outputs = json.RawMessage("[]")
		}
		if c.Metadata == nil {
			c.Metadata = map[string]interface{}{}
		}
		cells = append(cells, c)
	}
	if cells == nil {
		cells = []publicCell{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"type":     "notebook",
		"notebook": nb,
		"cells":    cells,
	})
}
