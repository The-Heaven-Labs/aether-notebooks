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

// @Summary List recent items
// @Description Returns recently accessed notebooks, dashboards, and connectors
// @Tags recent
// @Produce json
// @Success 200 {array} recentItem
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/recent [get]
func (s *Server) handleGetRecent(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx, `
		SELECT id::text, 'notebook' AS type, title AS name, updated_at
		FROM notebooks WHERE org_id = $1 AND deleted_at IS NULL
		UNION ALL
		SELECT id::text, 'dashboard', title, updated_at
		FROM dashboards WHERE org_id = $1 AND deleted_at IS NULL
		UNION ALL
		SELECT id::text, 'connector', name, updated_at
		FROM connectors WHERE org_id = $1 AND deleted_at IS NULL
		ORDER BY updated_at DESC
		LIMIT 20
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var candidates []recentItem
	for rows.Next() {
		var item recentItem
		if err := rows.Scan(&item.ID, &item.Type, &item.Name, &item.UpdatedAt); err != nil {
			continue
		}
		candidates = append(candidates, item)
	}

	// One batched view resolution per candidate type — instead of one check
	// per candidate — resolved once from a single caller identity. A
	// resolution failure is fail-loud (the former per-candidate checks
	// silently hid items on error). Types are walked in the UNION's order so
	// the queries stay deterministic.
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	permsByType := make(map[string]map[string]map[string]bool, 3)
	for _, typ := range []string{"notebook", "dashboard", "connector"} {
		var ids []string
		for _, item := range candidates {
			if item.Type == typ {
				ids = append(ids, item.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		perms, err := s.resolveActions(ctx, ident, typ, ids, []string{"view"})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		permsByType[typ] = perms
	}

	items := []recentItem{}
	for _, item := range candidates {
		if !actionAllowed(permsByType[item.Type], item.ID, "view") {
			continue
		}
		items = append(items, item)
		if len(items) >= 10 {
			break
		}
	}
	if items == nil {
		items = []recentItem{}
	}
	writeJSON(w, http.StatusOK, items)
}
