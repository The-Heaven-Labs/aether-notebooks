package api

import (
	"net/http"
	"time"
)

type recentItem struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Server) handleGetRecent(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx, `
		SELECT id::text, 'notebook' AS type, title AS name, updated_at
		FROM notebooks WHERE org_id = $1
		UNION ALL
		SELECT id::text, 'dashboard', title, updated_at
		FROM dashboards WHERE org_id = $1
		UNION ALL
		SELECT id::text, 'connector', name, updated_at
		FROM connectors WHERE org_id = $1
		ORDER BY updated_at DESC
		LIMIT 10
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	items := []recentItem{}
	for rows.Next() {
		var item recentItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Name, &item.UpdatedAt); err != nil {
			continue
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}
