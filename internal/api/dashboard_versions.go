package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
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

// errSnapshotWarnings reports that a dashboard document cannot be published
// because one or more widgets failed projection validation. The concrete
// snapshotWarningsError carries the warnings for the 422 response;
// errors.Is(err, errSnapshotWarnings) matches it and errors.As retrieves them.
var errSnapshotWarnings = errors.New("dashboard snapshot refused: document has invalid widgets")

// snapshotWarningsError carries the per-widget projection warnings that
// refused a publish.
type snapshotWarningsError struct {
	Warnings []string
}

func (e *snapshotWarningsError) Error() string {
	return fmt.Sprintf("%s (%d warning(s))", errSnapshotWarnings.Error(), len(e.Warnings))
}

func (e *snapshotWarningsError) Unwrap() error { return errSnapshotWarnings }

// snapshotWidget is one widget's full definition inside a published snapshot.
// Its JSON shape matches the content fields of models.Widget, so a snapshot
// can be served through the existing dashboard REST shapes without
// translation.
type snapshotWidget struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Layout      models.WidgetLayout    `json:"layout"`
	ConnectorID *string                `json:"connector_id,omitempty"`
	Query       *string                `json:"query,omitempty"`
	Language    string                 `json:"language"`
	NotebookID  *string                `json:"notebook_id,omitempty"`
	CellID      *string                `json:"cell_id,omitempty"`
	Config      map[string]interface{} `json:"config"`
}

// dashboardSnapshot is the publishable content of a dashboard document: title,
// settings (with variables folded in as settings["variables"]), and widgets
// sorted by ID.
type dashboardSnapshot struct {
	Title    string
	Settings map[string]any
	Widgets  []snapshotWidget
}

// dashboardSnapshotFromProjection builds the canonical snapshot for a
// projection. Settings copy the projection's map and re-inject variables under
// settings["variables"] — the shape models.DashboardSettings stores and the
// shape published execute/options validate against. Widgets are sorted by ID
// so the snapshot encoding is deterministic.
func dashboardSnapshotFromProjection(proj *dashboarddoc.Projection) *dashboardSnapshot {
	settings := make(map[string]any, len(proj.Settings)+1)
	for k, v := range proj.Settings {
		settings[k] = v
	}
	variables := proj.Variables
	if variables == nil {
		variables = []map[string]any{}
	}
	settings["variables"] = variables

	widgets := make([]snapshotWidget, 0, len(proj.Widgets))
	for _, w := range proj.Widgets {
		widgets = append(widgets, snapshotWidget{
			ID:          w.ID,
			Type:        w.Type,
			Layout:      models.WidgetLayout{Row: w.Layout.Row, Col: w.Layout.Col, Width: w.Layout.Width, Height: w.Layout.Height, Tab: w.Layout.Tab},
			ConnectorID: w.ConnectorID,
			Query:       w.Query,
			Language:    w.Language,
			NotebookID:  w.NotebookID,
			CellID:      w.CellID,
			Config:      w.Config,
		})
	}
	sort.Slice(widgets, func(i, j int) bool { return widgets[i].ID < widgets[j].ID })
	return &dashboardSnapshot{Title: proj.Title, Settings: settings, Widgets: widgets}
}

// dashboardSnapshotFromDoc projects a dashboard document and refuses to
// snapshot it when any widget failed validation: publishing a document whose
// widget set is partially unreadable would silently drop those widgets from
// the published app. The refusal is errSnapshotWarnings via
// snapshotWarningsError, carrying the warnings.
//
// Two deliberate policies:
//
//   - Only widget warnings refuse a publish. Settings/variables warnings are
//     skipped by Project and are simply dropped from the snapshot, matching
//     the materializer, so one malformed setting can never block publishing.
//   - Restore cannot remove draft widgets that fail projection: the restore
//     diff is driven by Projection.Widgets, which omits warned widgets, so a
//     corrupt widget survives in the document (invisible to readers and the
//     materializer) until a raw-key document primitive can delete it by ID.
func dashboardSnapshotFromDoc(state []byte) (*dashboardSnapshot, error) {
	proj, err := dashboarddoc.Project(state)
	if err != nil {
		return nil, err
	}
	if len(proj.WidgetWarnings) > 0 {
		return nil, &snapshotWarningsError{Warnings: proj.WidgetWarnings}
	}
	return dashboardSnapshotFromProjection(proj), nil
}

// dashboardContentHash returns the canonical SHA-256 (hex) of a dashboard's
// publishable content. Both publish and the versions status endpoint use it,
// so "has unpublished changes" is a hash comparison. Canonicalization is
// JSON-based: encoding/json sorts map keys, and widgets are sorted by ID
// before hashing, so map iteration order and widget order never affect the
// hash while any content change (title, settings, a widget field) does.
func dashboardContentHash(title string, settings map[string]any, widgets []snapshotWidget) string {
	sorted := make([]snapshotWidget, len(widgets))
	copy(sorted, widgets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	payload := struct {
		Title    string           `json:"title"`
		Settings map[string]any   `json:"settings"`
		Widgets  []snapshotWidget `json:"widgets"`
	}{Title: title, Settings: settings, Widgets: sorted}

	raw, err := json.Marshal(payload)
	if err != nil {
		// Unreachable for projection/JSONB-domain values (Project normalizes
		// its output and snapshots are decoded from JSONB); hash the error so
		// the function stays total instead of panicking on the request path.
		raw = []byte("marshal-error:" + err.Error())
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// snapshotWidgetToDoc converts a stored snapshot widget into the document
// write shape. Layout fields map one-to-one, including the tab placement.
func snapshotWidgetToDoc(w snapshotWidget) dashboarddoc.WidgetDoc {
	return dashboarddoc.WidgetDoc{
		ID:          w.ID,
		Type:        w.Type,
		Layout:      dashboarddoc.Layout{Row: w.Layout.Row, Col: w.Layout.Col, Width: w.Layout.Width, Height: w.Layout.Height, Tab: w.Layout.Tab},
		ConnectorID: w.ConnectorID,
		Query:       w.Query,
		Language:    w.Language,
		NotebookID:  w.NotebookID,
		CellID:      w.CellID,
		Config:      w.Config,
	}
}

// dashboardVersionRow is one dashboard_versions row plus the publisher's email
// for the version-history response.
type dashboardVersionRow struct {
	models.DashboardVersion
	CreatedByEmail string
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
	out.Settings = map[string]any{}
	if len(settingsRaw) > 0 {
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
// new dashboard_versions row and audits the publish. The version number is
// allocated by the INSERT ... SELECT MAX(version)+1 and the UNIQUE
// (dashboard_id, version) constraint arbitrates concurrent publishes: a unique
// violation is retried once against the then-current max. A missing or trashed
// dashboard returns errDashboardDocNotFound; a document with invalid widgets
// returns errSnapshotWarnings.
func (s *Server) publishDashboardVersion(ctx context.Context, orgID, dashID, userID string) (version int, contentHash string, createdAt time.Time, err error) {
	state, err := s.loadOrSeedDashboardDoc(ctx, orgID, dashID)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	snap, err := dashboardSnapshotFromDoc(state)
	if err != nil {
		return 0, "", time.Time{}, err
	}
	settingsJSON, err := json.Marshal(snap.Settings)
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("encode dashboard version settings: %w", err)
	}
	widgetsJSON, err := json.Marshal(snap.Widgets)
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("encode dashboard version widgets: %w", err)
	}
	contentHash = dashboardContentHash(snap.Title, snap.Settings, snap.Widgets)

	for attempt := 0; attempt < 2; attempt++ {
		err = s.db.Pool.QueryRow(ctx, `
			INSERT INTO dashboard_versions
				(dashboard_id, org_id, version, title, settings, widgets, content_hash, created_by)
			SELECT $1, $2, COALESCE(MAX(version), 0) + 1, $3, $4, $5, $6, $7
			FROM dashboard_versions
			WHERE dashboard_id = $1
			RETURNING version, created_at`,
			dashID, orgID, snap.Title, settingsJSON, widgetsJSON, contentHash, userID,
		).Scan(&version, &createdAt)
		if err == nil {
			break
		}
		if attempt == 0 && isUniqueViolation(err) {
			continue
		}
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
	var warnings *snapshotWarningsError
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
	draft := dashboardSnapshotFromProjection(proj)
	draftHash := dashboardContentHash(draft.Title, draft.Settings, draft.Widgets)

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

	var widgets []snapshotWidget
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
	// deliberately left in the document (see dashboardSnapshotFromDoc).
	snapshotIDs := make(map[string]struct{}, len(widgets))
	for _, sw := range widgets {
		snapshotIDs[sw.ID] = struct{}{}
		newState, err = dashboarddoc.UpsertWidget(newState, snapshotWidgetToDoc(sw))
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
