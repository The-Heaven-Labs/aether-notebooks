package api

import (
	"net/http"
	"time"
)

type motdRequest struct {
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Priority    int      `json:"priority"`
	Visibility  string   `json:"visibility"`
	Pages       []string `json:"pages"`
	ShowOnLogin bool     `json:"show_on_login"`
	ExpiresAt   string   `json:"expires_at,omitempty"`
}

func (s *Server) handleListMOTD(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ctx := r.Context()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, title, content, priority, visibility, pages, show_on_login, created_at, expires_at
		 FROM motd_messages WHERE org_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY priority DESC, created_at DESC`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list MOTDs")
		return
	}
	defer rows.Close()

	var motds []map[string]any
	for rows.Next() {
		var id, content, visibility string
		var title string
		var priority int
		var pages []string
		var showOnLogin bool
		var createdAt time.Time
		var expiresAt *time.Time
		if err := rows.Scan(&id, &title, &content, &priority, &visibility, &pages, &showOnLogin, &createdAt, &expiresAt); err != nil {
			continue
		}
		motds = append(motds, map[string]any{
			"id": id, "title": title, "content": content, "priority": priority,
			"visibility": visibility, "pages": pages, "show_on_login": showOnLogin,
			"created_at": createdAt, "expires_at": expiresAt,
		})
	}
	if motds == nil {
		motds = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, motds)
}

func (s *Server) handleCreateMOTD(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var req motdRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "all"
	}

	ctx := r.Context()
	var id string
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err == nil {
			expiresAt = &t
		}
	}

	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO motd_messages (org_id, title, content, priority, visibility, pages, show_on_login, created_by, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		claims.OrgID, req.Title, req.Content, req.Priority, req.Visibility, req.Pages, req.ShowOnLogin, claims.UserID, expiresAt,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create MOTD")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleUpdateMOTD(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	motdID := r.PathValue("id")
	var req motdRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err == nil {
			expiresAt = &t
		}
	}

	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE motd_messages SET title=$1, content=$2, priority=$3, visibility=$4, pages=$5, show_on_login=$6, expires_at=$7
		 WHERE id=$8 AND org_id=$9`,
		req.Title, req.Content, req.Priority, req.Visibility, req.Pages, req.ShowOnLogin, expiresAt, motdID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update MOTD")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "MOTD not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteMOTD(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	motdID := r.PathValue("id")
	ctx := r.Context()
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM motd_messages WHERE id=$1 AND org_id=$2`, motdID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete MOTD")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "MOTD not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
