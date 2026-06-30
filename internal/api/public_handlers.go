package api

import (
	"encoding/json"
	"net/http"

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
		`SELECT position, type, language, source, outputs, parameters, metadata, outputs_hidden
		 FROM cells WHERE notebook_id=$1 ORDER BY position ASC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type publicCell struct {
		Position      int                    `json:"position"`
		Type          string                 `json:"type"`
		Language      string                 `json:"language,omitempty"`
		Source        string                 `json:"source"`
		Outputs       []models.Output        `json:"outputs"`
		Parameters    []models.Parameter     `json:"parameters"`
		Metadata      map[string]interface{} `json:"metadata"`
		OutputsHidden bool                   `json:"outputs_hidden"`
	}
	var cells []publicCell
	for rows.Next() {
		var c publicCell
		var lang *string
		var outputs, cellParams, meta []byte
		if err := rows.Scan(&c.Position, &c.Type, &lang, &c.Source, &outputs, &cellParams, &meta, &c.OutputsHidden); err != nil {
			continue
		}
		if lang != nil {
			c.Language = *lang
		}
		json.Unmarshal(outputs, &c.Outputs)
		json.Unmarshal(cellParams, &c.Parameters)
		json.Unmarshal(meta, &c.Metadata)
		if c.Outputs == nil {
			c.Outputs = []models.Output{}
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
