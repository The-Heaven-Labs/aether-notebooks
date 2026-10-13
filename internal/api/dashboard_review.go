package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/dashboardreview"
)

// @Summary Review a dashboard's App composition
// @Description Returns cohesion findings (layout, content, surface) with the fixing tool call per finding, plus the app view, draft-preview, and public links
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Success 200 {object} dashboardreview.Report
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/review [get]
func (s *Server) handleDashboardReview(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	// Existence first (404), then edit permission (403), matching GET
	// /dashboards/{id}: a missing id must not read as "insufficient
	// permissions" for its editor.
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL)`,
		dashID, claims.OrgID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have permission to review this dashboard")
		return
	}

	// The review reads the draft document (that is what an editor reviews),
	// so it stays edit-gated like the draft document routes themselves.
	state, err := s.loadOrSeedDashboardDoc(ctx, claims.OrgID, dashID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dashboard")
		return
	}
	proj, err := dashboarddoc.Project(state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dashboard")
		return
	}

	// Published hash (latest version) vs the canonical draft hash — the same
	// comparison the versions status endpoint uses.
	var publishedHash string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT content_hash FROM dashboard_versions WHERE dashboard_id = $1 ORDER BY version DESC LIMIT 1`,
		dashID).Scan(&publishedHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load versions")
		return
	}

	// Read-only share lookup: the review never mints a public token.
	var publicToken string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT token FROM public_tokens WHERE resource_type = 'dashboard' AND resource_id = $1`,
		dashID).Scan(&publicToken)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load share token")
		return
	}

	snapshot := dashboarddoc.SnapshotFromProjection(proj)
	draftHash := dashboarddoc.ContentHash(snapshot.Title, snapshot.Settings, snapshot.Widgets)

	// Links prefer the public URL (the deployment's canonical address), then
	// the frontend URL.
	base := s.publicURL
	if base == "" {
		base = s.frontendURL
	}
	writeJSON(w, http.StatusOK, dashboardreview.BuildReport(dashID, proj, dashboardreview.Options{
		PublishedHash: publishedHash,
		DraftHash:     draftHash,
	}, base, publicToken))
}
