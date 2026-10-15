package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/models"
)

// This file implements the dashboard draft → publish split: publishing
// snapshots the dashboard document (the source of truth) into
// dashboard_versions, listing reports the version history and whether the
// draft has unpublished changes, and restoring copies a snapshot back into the
// draft (publishing stays explicit). Viewers read the published snapshot
// through the existing REST shapes; the document itself is never rewritten by
// a read.

// dashboardVersionRow is one dashboard_versions row plus the publisher's email
// for the version-history response.
type dashboardVersionRow struct {
	models.DashboardVersion
	CreatedByEmail string
	// SettingsRaw retains the raw settings column so typed snapshot reads
	// (decodeSnapshotSettings) decode the JSONB bytes directly instead of
	// round-tripping through the map envelope below.
	SettingsRaw json.RawMessage
}

// dashboardVersionSelect reads one version scoped to its dashboard and org,
// joined to the publisher's email. Callers append the version predicate.
const dashboardVersionSelect = `
	SELECT v.id, v.dashboard_id, v.org_id, v.version, v.title, v.settings, v.widgets,
	       v.content_hash, v.created_by, v.created_at, COALESCE(u.email, '')
	FROM dashboard_versions v
	LEFT JOIN users u ON u.id = v.created_by
	WHERE v.dashboard_id = $1 AND v.org_id = $2`

// scanDashboardVersion decodes one version row. The raw pgx error (including
// pgx.ErrNoRows) is returned unchanged so callers can distinguish "no such
// version" from a real failure.
func scanDashboardVersion(row pgx.Row) (*dashboardVersionRow, error) {
	var out dashboardVersionRow
	var settingsRaw, widgetsRaw []byte
	if err := row.Scan(&out.ID, &out.DashboardID, &out.OrgID, &out.Version, &out.Title,
		&settingsRaw, &widgetsRaw, &out.ContentHash, &out.CreatedBy, &out.CreatedAt,
		&out.CreatedByEmail); err != nil {
		return nil, err
	}
	out.SettingsRaw = settingsRaw
	out.Settings = map[string]any{}
	if len(settingsRaw) > 0 {
		// This decodes the map envelope DashboardVersion exposes to the
		// restore/version reads — a different target than
		// decodeSnapshotSettings' typed shape — so it stays separate.
		if err := json.Unmarshal(settingsRaw, &out.Settings); err != nil {
			return nil, fmt.Errorf("decode dashboard version settings: %w", err)
		}
	}
	out.Widgets = widgetsRaw
	return &out, nil
}

// loadLatestDashboardVersion returns the dashboard's highest published
// version, or pgx.ErrNoRows when it has never been published. The version
// status endpoint derives its latest entry from the list query instead (the
// snapshot payload is not needed there); published-mode reads use this.
func (s *Server) loadLatestDashboardVersion(ctx context.Context, orgID, dashID string) (*dashboardVersionRow, error) {
	return scanDashboardVersion(s.db.Pool.QueryRow(ctx,
		dashboardVersionSelect+` ORDER BY v.version DESC LIMIT 1`, dashID, orgID))
}

// loadDashboardVersion returns one specific published version, or
// pgx.ErrNoRows when it does not exist for the dashboard/org.
func (s *Server) loadDashboardVersion(ctx context.Context, orgID, dashID string, version int) (*dashboardVersionRow, error) {
	return scanDashboardVersion(s.db.Pool.QueryRow(ctx,
		dashboardVersionSelect+` AND v.version = $3`, dashID, orgID, version))
}

// publishDashboardVersion snapshots the dashboard's current document into a
// new dashboard_versions row and audits the publish. The snapshot, content
// hash, and version numbering are shared with the agent publish tool through
// dashboarddoc.PublishVersion (internal/agent cannot import internal/api).
// A missing or trashed dashboard returns errDashboardDocNotFound; a document
// with invalid widgets returns dashboarddoc.ErrSnapshotWarnings.
func (s *Server) publishDashboardVersion(ctx context.Context, orgID, dashID, userID string) (version int, contentHash string, createdAt time.Time, err error) {
	state, err := s.loadOrSeedDashboardDoc(ctx, orgID, dashID)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	version, contentHash, createdAt, err = dashboarddoc.PublishVersion(ctx, s.db.Pool, orgID, dashID, userID, state)
	if err != nil {
		return 0, "", time.Time{}, err
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: userID,
		Action: "dashboard.publish", ResourceType: "dashboard", ResourceID: dashID,
		Metadata: map[string]any{"version": version, "content_hash": contentHash},
	})
	return version, contentHash, createdAt, nil
}

// dashboardVersionSummary is one entry of the version-history response: the
// version number and its provenance, without the snapshot payload.
type dashboardVersionSummary struct {
	Version        int       `json:"version"`
	CreatedByEmail string    `json:"created_by_email"`
	CreatedAt      time.Time `json:"created_at"`
	ContentHash    string    `json:"content_hash"`
}

// dashboardVersionsResponse is the GET /dashboards/{id}/versions body.
// Warnings carries the draft's widget projection warnings: a non-empty list
// forces HasUnpublishedChanges, because a draft that publish would refuse has
// unpublished changes by definition and the editor needs to know why.
type dashboardVersionsResponse struct {
	Versions              []dashboardVersionSummary `json:"versions"`
	Latest                *dashboardVersionSummary  `json:"latest"`
	HasUnpublishedChanges bool                      `json:"has_unpublished_changes"`
	DraftHash             string                    `json:"draft_hash"`
	Warnings              []string                  `json:"warnings,omitempty"`
}

// @Summary Publish a dashboard version
// @Description Snapshot the dashboard's current document (title, settings, widgets) as a new published version
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} map[string]string
// @Failure 422 {object} map[string]any
// @Security BearerAuth
// @Router /dashboards/{id}/publish [post]
func (s *Server) handlePublishDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")

	version, contentHash, createdAt, err := s.publishDashboardVersion(r.Context(), claims.OrgID, dashID, claims.UserID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	var warnings *dashboarddoc.SnapshotWarningsError
	if errors.As(err, &warnings) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":    "dashboard document has invalid widgets",
			"warnings": warnings.Warnings,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "publish failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":      version,
		"content_hash": contentHash,
		"created_at":   createdAt,
	})
}

// @Summary List dashboard versions
// @Description List published versions (newest first) and whether the draft has unpublished changes
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Success 200 {object} dashboardVersionsResponse
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/versions [get]
func (s *Server) handleListDashboardVersions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	// The draft hash always reflects a real projection: the document is
	// lazily seeded from the materialized rows when no state exists yet.
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
	draft := dashboarddoc.SnapshotFromProjection(proj)
	draftHash := dashboarddoc.ContentHash(draft.Title, draft.Settings, draft.Widgets)

	rows, err := s.db.Pool.Query(ctx, `
		SELECT v.version, v.content_hash, v.created_at, COALESCE(u.email, '')
		FROM dashboard_versions v
		LEFT JOIN users u ON u.id = v.created_by
		WHERE v.dashboard_id = $1 AND v.org_id = $2
		ORDER BY v.version DESC
		LIMIT 50`,
		dashID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load versions")
		return
	}
	defer rows.Close()

	versions := []dashboardVersionSummary{}
	for rows.Next() {
		var v dashboardVersionSummary
		if err := rows.Scan(&v.Version, &v.ContentHash, &v.CreatedAt, &v.CreatedByEmail); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load versions")
			return
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load versions")
		return
	}

	// The list is newest-first, so its first row is the latest version; no
	// separate query is needed (and the snapshot payload stays undecoded).
	var latest *dashboardVersionSummary
	if len(versions) > 0 {
		latest = &versions[0]
	}

	// A draft whose widgets fail projection can never be published, so it
	// always counts as having unpublished changes — the hash alone would miss
	// it (warned widgets project out) and the editor needs the warnings.
	resp := dashboardVersionsResponse{
		Versions:              versions,
		Latest:                latest,
		HasUnpublishedChanges: latest == nil || draftHash != latest.ContentHash || len(proj.WidgetWarnings) > 0,
		DraftHash:             draftHash,
		Warnings:              proj.WidgetWarnings,
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary Restore a dashboard version
// @Description Copy a published snapshot back into the dashboard draft (does not publish)
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param version path int true "Version number"
// @Success 200 {object} map[string]int
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/versions/{version}/restore [post]
func (s *Server) handleRestoreDashboardVersion(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	ctx := r.Context()

	row, err := s.loadDashboardVersion(ctx, claims.OrgID, dashID, version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}

	var widgets []dashboarddoc.SnapshotWidget
	if err := json.Unmarshal(row.Widgets, &widgets); err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}

	state, err := s.loadOrSeedDashboardDoc(ctx, claims.OrgID, dashID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	proj, err := dashboarddoc.Project(state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}

	// Settings and variables are stored folded together in the snapshot; the
	// document keeps them separate, so split them back apart.
	settings := make(map[string]any, len(row.Settings))
	for k, v := range row.Settings {
		if k == "variables" {
			continue
		}
		settings[k] = v
	}
	variables, err := dashboardVariablesFromJSON(row.Settings["variables"])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	title := row.Title
	newState, err := dashboarddoc.UpdateMeta(state, &title, settings, variables)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}

	// Upsert every snapshot widget, then delete the draft widgets the snapshot
	// does not contain, so the draft's widget set is exactly the snapshot's.
	// Draft widgets that fail projection are absent from proj.Widgets and are
	// deliberately left in the document (see dashboarddoc.SnapshotFromDoc).
	snapshotIDs := make(map[string]struct{}, len(widgets))
	for _, sw := range widgets {
		snapshotIDs[sw.ID] = struct{}{}
		newState, err = dashboarddoc.UpsertWidget(newState, dashboarddoc.SnapshotWidgetToDoc(sw))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "restore failed")
			return
		}
	}
	for id := range proj.Widgets {
		if _, ok := snapshotIDs[id]; ok {
			continue
		}
		newState, err = dashboarddoc.DeleteWidget(newState, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "restore failed")
			return
		}
	}

	if err := s.storeAndMaterializeDashboardDoc(ctx, dashID, newState); err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.version_restore", ResourceType: "dashboard", ResourceID: dashID,
		Metadata: map[string]any{"version": version},
	})

	writeJSON(w, http.StatusOK, map[string]any{"restored": version})
}
