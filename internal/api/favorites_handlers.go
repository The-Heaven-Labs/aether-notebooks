package api

import (
	"net/http"

	"github.com/the-heaven-labs/aether/internal/audit"
)

// favoritesListLimit caps one /favorites listing before grouping. The listing
// is ordered (created_at DESC) so the cap keeps the newest rows.
const favoritesListLimit = 500

// favoritesResponse is the grouped GET /favorites payload.
type favoritesResponse struct {
	Groups []summaryGroup `json:"groups"`
}

// @Summary Add a favorite
// @Description Marks a catalog resource as a favorite for the current user. Idempotent: re-adding keeps the original position. Requires view access; unknown, foreign-org, and trashed resources all return 404.
// @Tags favorites
// @Produce json
// @Param resource_type path string true "Resource type (folder, notebook, dashboard, connector, agent, model_config, skill, mcp_server, tool)"
// @Param resource_id path string true "Resource ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /favorites/{resource_type}/{resource_id} [put]
func (s *Server) handlePutFavorite(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")
	ctx := r.Context()

	if !favoriteResourceTypeSet[resourceType] {
		writeError(w, http.StatusBadRequest, "unknown resource type")
		return
	}
	// Unknown, foreign-org, and trashed resources are all indistinguishable
	// 404s: favorites never leak existence. A malformed id cannot name a live
	// resource, so it 404s without a query.
	if !isValidUUID(resourceID) {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	resolved, err := s.resolveActions(ctx, ident, resourceType, []string{resourceID}, []string{"view"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !actionAllowed(resolved, resourceID, "view") {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}
	// resolveActions treats a trashed resource as existing (its ACL rows
	// survive), so the live check is a second, explicit gate.
	live, err := s.resourceLive(ctx, claims.OrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource lookup failed")
		return
	}
	if !live {
		writeError(w, http.StatusNotFound, "resource not found")
		return
	}

	result, err := s.db.Pool.Exec(ctx, `
		INSERT INTO favorites (user_id, org_id, resource_type, resource_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, org_id, resource_type, resource_id) DO NOTHING
	`, claims.UserID, claims.OrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add favorite")
		return
	}
	// Audit only an actual insert: a re-favorite is a no-op and must not
	// produce a second event.
	if result.RowsAffected() == 1 {
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "favorite.add", ResourceType: resourceType, ResourceID: resourceID,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Remove a favorite
// @Description Removes a catalog resource from the current user's favorites. Idempotent: deleting a favorite that does not exist also returns 204.
// @Tags favorites
// @Produce json
// @Param resource_type path string true "Resource type (folder, notebook, dashboard, connector, agent, model_config, skill, mcp_server, tool)"
// @Param resource_id path string true "Resource ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /favorites/{resource_type}/{resource_id} [delete]
func (s *Server) handleDeleteFavorite(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")
	ctx := r.Context()

	if !favoriteResourceTypeSet[resourceType] {
		writeError(w, http.StatusBadRequest, "unknown resource type")
		return
	}
	// Idempotent: a malformed id cannot name a row, so it is a no-op 204.
	if !isValidUUID(resourceID) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	result, err := s.db.Pool.Exec(ctx, `
		DELETE FROM favorites
		WHERE user_id = $1 AND org_id = $2 AND resource_type = $3 AND resource_id = $4
	`, claims.UserID, claims.OrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove favorite")
		return
	}
	// Audit only an actual delete; a repeated DELETE is a no-op.
	if result.RowsAffected() == 1 {
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "favorite.remove", ResourceType: resourceType, ResourceID: resourceID,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary List favorites
// @Description Returns the current user's favorites, grouped by resource type in canonical order (folder first), newest first within each group, capped at 500. Entries whose resource is no longer visible or has been trashed are omitted.
// @Tags favorites
// @Produce json
// @Success 200 {object} favoritesResponse
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /favorites [get]
func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	// One ordered, capped scan of the caller's favorites. The cap applies
	// before grouping; the per-type id slices keep created_at DESC order.
	rows, err := s.db.Pool.Query(ctx, `
		SELECT resource_type, resource_id::text FROM favorites
		WHERE user_id = $1 AND org_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, claims.UserID, claims.OrgID, favoritesListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorites")
		return
	}
	defer rows.Close()
	idsByType := make(map[string][]string)
	for rows.Next() {
		var resourceType, resourceID string
		if err := rows.Scan(&resourceType, &resourceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load favorites")
			return
		}
		// Defensive: rows of a retired type are skipped, never rendered.
		if !favoriteResourceTypeSet[resourceType] {
			continue
		}
		idsByType[resourceType] = append(idsByType[resourceType], resourceID)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorites")
		return
	}

	requests := make([]summaryRequest, 0, len(favoriteResourceTypes))
	for _, resourceType := range favoriteResourceTypes {
		if len(idsByType[resourceType]) == 0 {
			continue
		}
		requests = append(requests, summaryRequest{ResourceType: resourceType, IDs: idsByType[resourceType]})
	}

	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	groups, err := s.buildResourceSummaries(ctx, ident, requests, nil, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build favorites")
		return
	}
	if groups == nil {
		groups = []summaryGroup{}
	}
	writeJSON(w, http.StatusOK, favoritesResponse{Groups: groups})
}
