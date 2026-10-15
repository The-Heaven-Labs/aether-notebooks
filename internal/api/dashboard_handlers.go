package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/the-heaven-labs/aether/internal/validate"
)

type createDashboardRequest struct {
	Title    string                   `json:"title"`
	Settings models.DashboardSettings `json:"settings,omitempty"`
	FolderID *string                  `json:"folder_id,omitempty"`
}

type updateDashboardRequest struct {
	Title *string `json:"title,omitempty"`
	// Settings is decoded as a raw map (not models.DashboardSettings) so an
	// explicit variables: [] clears the list and every provided key can be
	// shallow-merged over the document's current settings; the typed struct's
	// omitempty would silently drop both.
	Settings map[string]any `json:"settings,omitempty"`
	FolderID *string        `json:"folder_id,omitempty"`
}

type widgetCellData struct {
	CellID    string          `json:"cell_id"`
	Source    string          `json:"source"`
	Type      string          `json:"type"`
	Language  string          `json:"language"`
	Outputs   json.RawMessage `json:"outputs"`
	Metadata  json.RawMessage `json:"metadata"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type addWidgetRequest struct {
	NotebookID  *string                `json:"notebook_id"`
	CellID      *string                `json:"cell_id"`
	ConnectorID *string                `json:"connector_id"`
	Query       *string                `json:"query"`
	Language    *string                `json:"language"`
	Type        models.WidgetType      `json:"type"`
	Layout      models.WidgetLayout    `json:"layout"`
	Config      map[string]interface{} `json:"config,omitempty"`
}

// @Summary Create a dashboard
// @Description Create a new dashboard
// @Tags dashboards
// @Accept json
// @Produce json
// @Param request body object true "Dashboard details"
// @Success 201 {object} models.Dashboard
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards [post]
func (s *Server) handleCreateDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createDashboardRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	if err := validate.DashboardAppSettings(req.Settings.App); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validate.DashboardTabs(req.Settings.Tabs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Settings.App != nil {
		if writeLogoAttachmentError(w, s.validateLogoAttachment(r.Context(), claims.OrgID, req.Settings.App.LogoAttachmentID)) {
			return
		}
	}

	settingsJSON, _ := json.Marshal(req.Settings)
	ctx := r.Context()

	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO dashboards (org_id, title, settings, created_by, folder_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, title, settings, folder_id, created_by, created_at, updated_at`,
		claims.OrgID, req.Title, settingsJSON, claims.UserID, req.FolderID,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.FolderID,
		&dash.CreatedBy, &dash.CreatedAt, &dash.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create dashboard")
		return
	}
	json.Unmarshal(settingsOut, &dash.Settings)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.create", ResourceType: "dashboard", ResourceID: dash.ID,
	})

	_, aclErr := s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'dashboard', $2::uuid, 'user', $3, ARRAY['view','view_with_data','edit','delete','share'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, dash.ID, claims.UserID,
	)
	if aclErr != nil {
		slog.Warn("dashboard ACL seeding failed", "id", dash.ID, "error", aclErr)
	}

	writeJSON(w, http.StatusCreated, dash)
}

// @Summary Update a dashboard
// @Description Update a dashboard's title, settings, or folder
// @Tags dashboards
// @Accept json
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param request body object true "Dashboard updates"
// @Success 200 {object} models.Dashboard
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id} [put]
func (s *Server) handleUpdateDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	var req updateDashboardRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	if req.Title == nil && req.Settings == nil && req.FolderID == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}

	ctx := r.Context()

	// Title and settings live in the Yjs document (the source of truth), so
	// they are read-merged there and stored through MergeAndStore. The
	// settings merge is shallow: provided keys replace their projected
	// counterparts and absent keys stay, while variables are split out into
	// the document's separate ordered array.
	if req.Title != nil || req.Settings != nil {
		state, err := s.loadOrSeedDashboardDoc(ctx, claims.OrgID, dashID)
		if errors.Is(err, errDashboardDocNotFound) {
			writeError(w, http.StatusNotFound, "dashboard not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
		proj, err := dashboarddoc.Project(state)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}

		var mergedSettings map[string]any
		var variables []map[string]any
		if req.Settings != nil {
			if err := validate.DashboardSettingsValue(req.Settings); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			// The logo reference points at an org attachment; the existence
			// check needs the database, so it runs after the pure validators.
			if app, ok := req.Settings["app"].(map[string]any); ok {
				if id, _ := app["logo_attachment_id"].(string); id != "" {
					if writeLogoAttachmentError(w, s.validateLogoAttachment(ctx, claims.OrgID, id)) {
						return
					}
				}
			}
			mergedSettings = make(map[string]any, len(proj.Settings)+len(req.Settings))
			for k, v := range proj.Settings {
				mergedSettings[k] = v
			}
			for k, v := range req.Settings {
				if k == "variables" {
					continue
				}
				mergedSettings[k] = v
			}
			if v, ok := req.Settings["variables"]; ok {
				variables, err = dashboardVariablesFromJSON(v)
				if err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		}

		newState, err := dashboarddoc.UpdateMeta(state, req.Title, mergedSettings, variables)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
		if err := s.storeAndMaterializeDashboardDoc(ctx, dashID, newState); err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
	}

	// folder_id is not part of the document; it stays a direct derived-row
	// write (folder moves have no doc impact).
	if req.FolderID != nil {
		tag, err := s.db.Pool.Exec(ctx,
			`UPDATE dashboards SET folder_id = $1, updated_at = NOW()
			 WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL`,
			*req.FolderID, dashID, claims.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update failed")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "dashboard not found")
			return
		}
	}

	// The response is the materialized row, so title/settings reflect the
	// document store and updated_at is the stored value.
	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashID, claims.OrgID,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.FolderID,
		&dash.CreatedBy, &dash.CreatedAt, &dash.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	json.Unmarshal(settingsOut, &dash.Settings)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.update", ResourceType: "dashboard", ResourceID: dashID,
	})

	writeJSON(w, http.StatusOK, dash)
}

// @Summary List dashboards
// @Description List all dashboards for the current organization
// @Tags dashboards
// @Produce json
// @Success 200 {array} models.Dashboard
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards [get]
func (s *Server) handleListDashboards(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 AND deleted_at IS NULL ORDER BY updated_at DESC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var dashboards []models.Dashboard
	for rows.Next() {
		var d models.Dashboard
		var settingsOut []byte
		if err := rows.Scan(&d.ID, &d.OrgID, &d.Title, &settingsOut, &d.FolderID,
			&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		json.Unmarshal(settingsOut, &d.Settings)
		dashboards = append(dashboards, d)
	}

	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		// Deliberate fail-loud: the former per-item checks silently hid rows on error.
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	ids := make([]string, 0, len(dashboards))
	for _, d := range dashboards {
		ids = append(ids, d.ID)
	}
	perms, err := s.resolveActions(ctx, ident, "dashboard", ids, []string{"view", "edit"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}

	// Viewers of a published dashboard must not see draft-only content in
	// the list: substitute the latest snapshot's title and settings like
	// GET /dashboards/{id} does. A never-published dashboard has nothing
	// to substitute, so the draft serves everyone (the read behavior). One
	// batched snapshot load covers every non-editor row.
	var nonEditorIDs []string
	for _, d := range dashboards {
		if actionAllowed(perms, d.ID, "view") && !actionAllowed(perms, d.ID, "edit") {
			nonEditorIDs = append(nonEditorIDs, d.ID)
		}
	}
	snapshots := map[string]publishedDashboardMeta{}
	if len(nonEditorIDs) > 0 {
		snapshots, err = s.loadLatestDashboardSnapshots(ctx, claims.OrgID, nonEditorIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
	}

	out := make([]models.Dashboard, 0, len(dashboards))
	for _, d := range dashboards {
		if !actionAllowed(perms, d.ID, "view") {
			continue
		}
		if !actionAllowed(perms, d.ID, "edit") {
			if snapshot, ok := snapshots[d.ID]; ok {
				d.Title = snapshot.Title
				d.Settings = snapshot.Settings
			}
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// dashboardPublishedVersion identifies the published snapshot a read served,
// so clients can show provenance and detect staleness.
type dashboardPublishedVersion struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// snapshotWidgetToModel converts one published snapshot widget into the REST
// widget shape, scoped to its dashboard. Timestamps stay zero: a snapshot
// carries content, not row metadata.
func snapshotWidgetToModel(dashID string, sw dashboarddoc.SnapshotWidget) models.Widget {
	return models.Widget{
		ID:          sw.ID,
		DashboardID: dashID,
		NotebookID:  sw.NotebookID,
		CellID:      sw.CellID,
		ConnectorID: sw.ConnectorID,
		Query:       sw.Query,
		Language:    sw.Language,
		Type:        models.WidgetType(sw.Type),
		Layout: models.WidgetLayout{
			Row:    sw.Layout.Row,
			Col:    sw.Layout.Col,
			Width:  sw.Layout.Width,
			Height: sw.Layout.Height,
			Tab:    sw.Layout.Tab,
		},
		Config: sw.Config,
	}
}

// dashboardWidgetsFromSnapshot converts a whole snapshot widget list into the
// REST shape. The result is non-nil even when empty, so callers can tell a
// published-but-empty widget set from "no snapshot served".
func dashboardWidgetsFromSnapshot(dashID string, widgets []dashboarddoc.SnapshotWidget) []models.Widget {
	out := make([]models.Widget, 0, len(widgets))
	for _, sw := range widgets {
		out = append(out, snapshotWidgetToModel(dashID, sw))
	}
	return out
}

// @Summary Get a dashboard
// @Description Get a dashboard with its widgets. Viewers without dashboard edit permission always receive the latest published snapshot; editors receive the draft unless published=1 is set.
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param published query string false "1 serves the latest published snapshot for everyone (falls back to the draft when the dashboard has never been published)"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id} [get]
func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE id = $1 AND org_id = $2`,
		dashID, claims.OrgID,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.FolderID,
		&dash.CreatedBy, &dash.CreatedAt, &dash.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Check view permission (view_with_data implies view)
	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		allowed, err = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have permission to view this dashboard")
		return
	}
	json.Unmarshal(settingsOut, &dash.Settings)

	// Check view_with_data permission
	viewWithData, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
	shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "share")
	editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")

	// Wave-2 reads: viewers see the latest published snapshot and never draft
	// edits; editors see the draft unless ?published=1 explicitly asks for
	// the snapshot. A never-published dashboard has nothing to substitute, so
	// the draft serves everyone (the pre-publish behavior) and no
	// published_version is set. Execute's published: true deliberately does
	// not share this fallback: it 404s, so a run can never claim unpublished
	// data is published.
	var publishedVersion *dashboardPublishedVersion
	var widgets []models.Widget
	if r.URL.Query().Get("published") == "1" || !editOK {
		snapshot, snapErr := s.loadPublishedDashboardSnapshot(ctx, claims.OrgID, dashID)
		if snapErr == nil {
			dash.Title = snapshot.Title
			dash.Settings = snapshot.Settings
			widgets = dashboardWidgetsFromSnapshot(dashID, snapshot.Widgets)
			publishedVersion = &dashboardPublishedVersion{Version: snapshot.Version, CreatedAt: snapshot.CreatedAt}
		} else if !errors.Is(snapErr, errNoPublishedVersion) {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
	}
	if widgets == nil {
		widgets, err = s.loadWidgets(ctx, dashID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load widgets failed")
			return
		}
	}

	type dashboardWithWidgets struct {
		models.Dashboard
		Widgets           []models.Widget            `json:"widgets"`
		WidgetsData       map[string]widgetCellData  `json:"widgets_data,omitempty"`
		CanViewWithData   bool                       `json:"can_view_with_data"`
		CanShare          bool                       `json:"can_share"`
		CanEdit           bool                       `json:"can_edit"`
		PublishedVersion  *dashboardPublishedVersion `json:"published_version,omitempty"`
		OrgBranding       *models.OrgBranding        `json:"org_branding"`
		HTMLPanelsEnabled bool                       `json:"html_panels_enabled"`
		CustomCodeEnabled bool                       `json:"custom_code_enabled"`
	}

	resp := dashboardWithWidgets{
		Dashboard:         dash,
		Widgets:           widgets,
		CanViewWithData:   viewWithData,
		CanShare:          shareOK,
		CanEdit:           editOK,
		PublishedVersion:  publishedVersion,
		OrgBranding:       s.orgBranding(ctx, dash.OrgID),
		HTMLPanelsEnabled: s.orgHTMLPanelsEnabled(ctx, dash.OrgID),
		CustomCodeEnabled: s.orgCustomCodeEnabled(ctx, dash.OrgID),
	}

	if viewWithData && len(widgets) > 0 {
		// Collect unique cell IDs from all widgets
		cellIDs := make([]string, 0, len(widgets))
		for _, w := range widgets {
			if w.CellID != nil {
				cellIDs = append(cellIDs, *w.CellID)
			}
		}

		if len(cellIDs) > 0 {
			rows, err := s.db.Pool.Query(ctx,
				`SELECT c.id, c.source, c.type, c.language, c.outputs, COALESCE(c.metadata, '{}'), c.updated_at
				 FROM cells c
				 JOIN notebooks n ON n.id = c.notebook_id AND n.org_id = $1
				 WHERE c.id = ANY($2)`,
				claims.OrgID, cellIDs,
			)
			if err == nil {
				defer rows.Close()
				widgetsData := make(map[string]widgetCellData, len(cellIDs))
				for rows.Next() {
					var cd widgetCellData
					var outputs, metadata []byte
					if err := rows.Scan(&cd.CellID, &cd.Source, &cd.Type, &cd.Language, &outputs, &metadata, &cd.UpdatedAt); err != nil {
						continue
					}
					cd.Outputs = outputs
					cd.Metadata = metadata
					widgetsData[cd.CellID] = cd
				}
				resp.WidgetsData = widgetsData
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// @Summary Get dashboard permissions
// @Description Get the current user's permissions for a dashboard
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Success 200 {object} map[string]bool
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/permissions [get]
func (s *Server) handleGetDashboardPermissions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	var dashOrgID string
	if err := s.db.Pool.QueryRow(ctx, "SELECT org_id FROM dashboards WHERE id=$1", dashID).Scan(&dashOrgID); err != nil || dashOrgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	viewOK, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !viewOK {
		viewOK, err = s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
	}
	if !viewOK {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	editOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	deleteOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "delete")
	shareOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "share")
	viewWithDataOK, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")

	writeJSON(w, http.StatusOK, map[string]bool{
		"can_edit":           editOK,
		"can_delete":         deleteOK,
		"can_share":          shareOK,
		"can_view_with_data": viewWithDataOK,
	})
}

// @Summary Delete a dashboard
// @Description Delete a dashboard by ID
// @Tags dashboards
// @Param id path string true "Dashboard ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id} [delete]
func (s *Server) handleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`UPDATE dashboards SET deleted_at = NOW() WHERE id = $1 AND org_id = $2`,
		dashID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.delete", ResourceType: "dashboard", ResourceID: dashID,
	})

	// The dashboard is trashed: relay replicas must disconnect viewers and
	// drop any in-memory copy so a stale document cannot outlive the row.
	// Best-effort fan-out; a Redis outage never fails the delete.
	s.publishDashboardDocInvalidate(ctx, dashID, "trashed")

	w.WriteHeader(http.StatusNoContent)
}

// validateWidgetLayout checks that a widget layout is within grid bounds and does not
// overlap existing widgets. excludeWidgetID is empty for create, set for update.
func (s *Server) validateWidgetLayout(ctx context.Context, dashID string, layout models.WidgetLayout, excludeWidgetID string) error {
	return validate.WidgetLayout(ctx, s.db.Pool, dashID, layout, excludeWidgetID)
}

// validateWidgetLayoutBounds checks only grid bounds, without overlap validation.
// Used for updates where the frontend compactor already prevents overlap.
func (s *Server) validateWidgetLayoutBounds(ctx context.Context, dashID string, layout models.WidgetLayout) error {
	if layout.Col < 0 || layout.Row < 0 || layout.Width <= 0 || layout.Height <= 0 {
		return fmt.Errorf("invalid layout dimensions")
	}
	var gridCols int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COALESCE((settings->>'grid_cols')::int, 12) FROM dashboards WHERE id=$1`, dashID,
	).Scan(&gridCols)
	if err != nil {
		return fmt.Errorf("failed to read dashboard settings")
	}
	if gridCols <= 0 {
		gridCols = 12
	}
	if layout.Col+layout.Width > gridCols {
		return fmt.Errorf("layout exceeds grid columns")
	}
	return nil
}

// @Summary Add a widget
// @Description Add a widget to a dashboard
// @Tags dashboards
// @Accept json
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param request body object true "Widget details"
// @Success 201 {object} models.Widget
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/widgets [post]
func (s *Server) handleAddWidget(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")

	var req addWidgetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	// Check edit permission
	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have permission to edit this dashboard")
		return
	}

	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM dashboards WHERE id=$1 AND org_id=$2)", dashID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	// Validate the widget source: exactly one of notebook-cell or query.
	// Source-less widgets (e.g. text) remain allowed for backwards compatibility.
	if req.NotebookID != nil && req.ConnectorID != nil {
		writeError(w, http.StatusBadRequest, "widget cannot reference both a notebook cell and a query connector")
		return
	}
	if req.ConnectorID != nil {
		if req.CellID != nil {
			writeError(w, http.StatusBadRequest, "query widgets cannot reference a cell")
			return
		}
		if req.Query == nil || *req.Query == "" {
			writeError(w, http.StatusBadRequest, "query is required for query widgets")
			return
		}
	} else if req.NotebookID != nil || req.CellID != nil {
		if req.NotebookID == nil || req.CellID == nil {
			writeError(w, http.StatusBadRequest, "cell widgets require notebook_id and cell_id")
			return
		}
	}
	// The document format only carries known types and SQL; reject anything
	// else up front instead of failing inside the document write.
	switch req.Type {
	case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric, models.WidgetHTML, models.WidgetCustom:
	default:
		writeError(w, http.StatusBadRequest, "invalid widget type")
		return
	}
	if req.Type == models.WidgetHTML {
		if err := validate.ValidateWidgetHTML(widgetHTMLConfigFrom(req.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.orgHTMLPanelsEnabled(ctx, claims.OrgID) {
			writeError(w, http.StatusBadRequest, "html panels are disabled for this organization")
			return
		}
	}
	if req.Type == models.WidgetCustom {
		if err := validate.ValidateWidgetCustom(validate.WidgetCustomConfigFrom(req.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !s.orgCustomCodeEnabled(ctx, claims.OrgID) {
			writeError(w, http.StatusBadRequest, "custom code is disabled for this organization")
			return
		}
		if err := s.validateCustomBindingRefs(ctx, claims.OrgID, dashID, req.Config); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	lang := "sql"
	if req.Language != nil && *req.Language != "" {
		if *req.Language != "sql" {
			writeError(w, http.StatusBadRequest, "only sql widgets are supported")
			return
		}
		lang = *req.Language
	}

	// Validate widget references a notebook the user can view (prevents IDOR escalation)
	if req.NotebookID != nil {
		nbOK, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", *req.NotebookID, "view")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !nbOK {
			writeError(w, http.StatusForbidden, "you don't have permission to view this notebook")
			return
		}
	}

	// A dangling cell reference must fail the request: the document store
	// accepts the pair, but the materializer skips the widget (deleting its
	// derived row), so a success response would commit a phantom widget that
	// never materializes. The notebook view check above already established
	// the notebook is in the caller's org.
	if req.NotebookID != nil && req.CellID != nil {
		if err := s.validateWidgetCellRef(ctx, *req.NotebookID, *req.CellID); err != nil {
			writeCellRefError(w, err)
			return
		}
	}

	// A dangling connector reference must fail the request: the document
	// store accepts it, but the materializer skips the widget (deleting its
	// derived row), so a success response would silently drop it.
	if req.ConnectorID != nil {
		if err := s.validateWidgetConnectorRef(ctx, claims.OrgID, *req.ConnectorID); err != nil {
			writeConnectorRefError(w, err)
			return
		}
	}

	// Validate the widget's tab assignment (empty falls back to the first tab).
	if err := validate.WidgetTab(req.Layout.Tab); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate widget layout bounds and overlap.
	if err := s.validateWidgetLayout(ctx, dashID, req.Layout, ""); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Config == nil {
		req.Config = map[string]interface{}{}
	}

	// The widget is written through the dashboard document (the source of
	// truth); Postgres is a derived read cache materialized by the store.
	widgetID := uuid.NewString()
	state, err := s.loadOrSeedDashboardDoc(ctx, claims.OrgID, dashID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add widget")
		return
	}
	newState, err := dashboarddoc.UpsertWidget(state, dashboarddoc.WidgetDoc{
		ID:          widgetID,
		Type:        string(req.Type),
		Layout:      dashboarddoc.Layout{Row: req.Layout.Row, Col: req.Layout.Col, Width: req.Layout.Width, Height: req.Layout.Height, Tab: req.Layout.Tab},
		ConnectorID: req.ConnectorID,
		Query:       req.Query,
		Language:    lang,
		NotebookID:  req.NotebookID,
		CellID:      req.CellID,
		Config:      req.Config,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add widget")
		return
	}
	if err := s.storeAndMaterializeDashboardDoc(ctx, dashID, newState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add widget")
		return
	}

	// Build the response from the materialized row so id/timestamps are the
	// stored values.
	widget, err := s.loadQueryWidget(ctx, dashID, widgetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add widget")
		return
	}

	entry := audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "widget.create", ResourceType: "widget", ResourceID: widget.ID,
	}
	if widget.Type == models.WidgetCustom {
		entry.Metadata = map[string]any{"type": "custom", "js_bytes": len(validate.WidgetCustomConfigFrom(widget.Config).JS)}
	}
	s.audit.Log(ctx, entry)

	writeJSON(w, http.StatusCreated, widget)
}

// widgetHTMLConfigFrom extracts panel fields from a raw config map; non-string
// values are treated as absent (the document store keeps them, the renderer
// ignores them).
func widgetHTMLConfigFrom(config map[string]interface{}) validate.WidgetHTMLConfig {
	cfg := validate.WidgetHTMLConfig{}
	if s, ok := config["html"].(string); ok {
		cfg.HTML = s
	}
	if s, ok := config["css"].(string); ok {
		cfg.CSS = s
	}
	return cfg
}

// validateCustomBindingRefs rejects a custom widget whose bindings do not name
// widgets of this dashboard: the renderer would otherwise show permanent
// error states, and a cross-dashboard id would be a data-confusion bug (the
// execute pipeline would refuse it anyway — this fails at write time).
func (s *Server) validateCustomBindingRefs(ctx context.Context, orgID, dashID string, config map[string]interface{}) error {
	ids := validate.WidgetCustomBindingIDs(config)
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id FROM widgets WHERE dashboard_id = $1 AND id = ANY($2)`,
		dashID, ids)
	if err != nil {
		return fmt.Errorf("check binding references: %w", err)
	}
	defer rows.Close()
	found := make(map[string]bool, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("check binding references: %w", err)
		}
		found[id] = true
	}
	for _, id := range ids {
		if !found[id] {
			return fmt.Errorf("binding widget %s is not a widget of this dashboard", id)
		}
	}
	return nil
}

// @Summary Update a widget
// @Description Update a widget's layout or configuration
// @Tags dashboards
// @Accept json
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param widget_id path string true "Widget ID"
// @Param request body object true "Widget updates"
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/widgets/{widget_id} [put]
func (s *Server) handleUpdateWidget(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	widgetID := r.PathValue("widget_id")

	// Check edit permission
	allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have permission to edit this dashboard")
		return
	}

	// Path IDs are canonicalized before any lookup: document widget keys are
	// canonical lowercase UUIDs, so an uppercase or braced spelling must
	// address the same widget instead of 404ing.
	widgetUUID, err := uuid.Parse(widgetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	widgetID = widgetUUID.String()

	var req struct {
		Layout *struct {
			Row    int    `json:"row"`
			Col    int    `json:"col"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Tab    string `json:"tab,omitempty"`
		} `json:"layout,omitempty"`
		Config      map[string]interface{} `json:"config,omitempty"`
		Type        *models.WidgetType     `json:"type,omitempty"`
		ConnectorID *string                `json:"connector_id,omitempty"`
		Query       *string                `json:"query,omitempty"`
		Language    *string                `json:"language,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Layout == nil && req.Config == nil && req.Type == nil && req.ConnectorID == nil && req.Query == nil && req.Language == nil {
		writeError(w, http.StatusBadRequest, "layout, config, type, or query fields required")
		return
	}
	if req.Type != nil {
		switch *req.Type {
		case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric, models.WidgetHTML, models.WidgetCustom:
		default:
			writeError(w, http.StatusBadRequest, "invalid widget type")
			return
		}
		if *req.Type == models.WidgetHTML && !s.orgHTMLPanelsEnabled(r.Context(), claims.OrgID) {
			writeError(w, http.StatusBadRequest, "html panels are disabled for this organization")
			return
		}
		if *req.Type == models.WidgetCustom && !s.orgCustomCodeEnabled(r.Context(), claims.OrgID) {
			writeError(w, http.StatusBadRequest, "custom code is disabled for this organization")
			return
		}
	}
	if req.Config != nil {
		if err := validate.ValidateWidgetHTML(widgetHTMLConfigFrom(req.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// The custom block mirrors the html caps check: validate whatever the
		// request carries, and bindings against the *merged* config below
		// (a partial patch can introduce or keep bindings).
		if err := validate.ValidateWidgetCustom(validate.WidgetCustomConfigFrom(req.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Fail closed on panel-config writes while disabled: conversion and
		// content authoring both 400 so a disabled org cannot grow panels.
		_, hasHTMLKey := req.Config["html"].(string)
		_, hasCSSKey := req.Config["css"].(string)
		if (hasHTMLKey || hasCSSKey) && !s.orgHTMLPanelsEnabled(r.Context(), claims.OrgID) {
			writeError(w, http.StatusBadRequest, "html panels are disabled for this organization")
			return
		}
		// Custom-component authoring fails closed the same way. `js`/`bindings`
		// are unambiguous custom keys; `html`/`css` are shared with html
		// panels, so those are gated on the merged widget type below — a
		// disabled org can neither convert to nor author a custom widget
		// while html panels keep working.
		_, hasCustomJSKey := req.Config["js"].(string)
		_, hasCustomBindingsKey := req.Config["bindings"].([]interface{})
		if (hasCustomJSKey || hasCustomBindingsKey) && !s.orgCustomCodeEnabled(r.Context(), claims.OrgID) {
			writeError(w, http.StatusBadRequest, "custom code is disabled for this organization")
			return
		}
	}
	if req.ConnectorID != nil && *req.ConnectorID == "" {
		writeError(w, http.StatusBadRequest, "connector_id cannot be empty")
		return
	}
	if req.ConnectorID != nil {
		if err := s.validateWidgetConnectorRef(r.Context(), claims.OrgID, *req.ConnectorID); err != nil {
			writeConnectorRefError(w, err)
			return
		}
	}

	if req.Layout != nil {
		if err := validate.WidgetTab(req.Layout.Tab); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Validate widget layout bounds only (overlap is handled by the frontend compactor
		// during drag/resize, and the backend cannot know which widgets were moved together).
		if err := s.validateWidgetLayoutBounds(r.Context(), dashID, *req.Layout); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.Language != nil {
		if *req.Language != "sql" {
			writeError(w, http.StatusBadRequest, "only sql widgets are supported")
			return
		}
	}

	// The document is the source of truth and can be ahead of the derived
	// rows (the relay store debounces), so partial updates apply only the
	// fields the request carries, through field-level ops that mutate the
	// widget in place. UpsertWidget would replace the whole widget map,
	// dropping concurrent editor changes to fields the request does not
	// touch (for example SQL typed while this request was in flight).
	state, err := s.loadOrSeedDashboardDoc(r.Context(), claims.OrgID, dashID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update widget")
		return
	}
	proj, err := dashboarddoc.Project(state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update widget")
		return
	}
	existing, ok := proj.Widgets[widgetID]
	if !ok {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}

	// Merged view for validation only: the projected widget with the
	// request's fields overwritten, exactly as the field ops below will write
	// them. It never reaches the document.
	merged := existing
	if req.Layout != nil {
		merged.Layout = dashboarddoc.Layout{
			Row:    req.Layout.Row,
			Col:    req.Layout.Col,
			Width:  req.Layout.Width,
			Height: req.Layout.Height,
			Tab:    req.Layout.Tab,
		}
	}
	if req.Config != nil {
		merged.Config = req.Config
	}
	if req.Type != nil {
		merged.Type = string(*req.Type)
	}
	if req.ConnectorID != nil {
		merged.ConnectorID = req.ConnectorID
	}
	if req.Query != nil {
		merged.Query = req.Query
	}
	if req.Language != nil {
		merged.Language = *req.Language
	}

	// Custom components carry a bundle and bindings: validate the merged view
	// (a partial patch can introduce, keep, or convert to custom) and check
	// that every binding names a widget of this dashboard. The org gate runs
	// first so a disabled org can never author html/css on a custom widget.
	if merged.Type == string(models.WidgetCustom) {
		if !s.orgCustomCodeEnabled(r.Context(), claims.OrgID) {
			writeError(w, http.StatusBadRequest, "custom code is disabled for this organization")
			return
		}
		if err := validate.ValidateWidgetCustom(validate.WidgetCustomConfigFrom(merged.Config)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.validateCustomBindingRefs(r.Context(), claims.OrgID, dashID, merged.Config); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// A merged notebook/cell pair (the request has no ref fields, so it
	// comes from the existing widget) must still resolve to a real cell: the
	// materializer would otherwise skip the widget and delete its derived
	// row while the document keeps it.
	if merged.NotebookID != nil && merged.CellID != nil {
		if err := s.validateWidgetCellRef(r.Context(), *merged.NotebookID, *merged.CellID); err != nil {
			writeCellRefError(w, err)
			return
		}
	}

	// Enforce the source invariant on the merged result before the document
	// write: a connector widget without a query or with a notebook/cell link
	// cannot be materialized (the row would be dropped, or the write would
	// fail), so answer 400 instead of a silent success or a 500.
	if merged.ConnectorID != nil {
		if merged.NotebookID != nil || merged.CellID != nil {
			writeError(w, http.StatusBadRequest, "widget cannot reference both a notebook cell and a query connector")
			return
		}
		if merged.Query == nil || *merged.Query == "" {
			writeError(w, http.StatusBadRequest, "query is required for query widgets")
			return
		}
	}

	// One field-level op per request field, applied in sequence: each mutates
	// a single field of the live widget map, so every other field (and the
	// query Y.Text) keeps its CRDT identity.
	var ops []func([]byte) ([]byte, error)
	if req.Layout != nil {
		layout := dashboarddoc.Layout{
			Row:    req.Layout.Row,
			Col:    req.Layout.Col,
			Width:  req.Layout.Width,
			Height: req.Layout.Height,
			Tab:    req.Layout.Tab,
		}
		ops = append(ops, func(state []byte) ([]byte, error) {
			return dashboarddoc.UpdateLayout(state, widgetID, layout)
		})
	}
	if req.Config != nil {
		config := req.Config
		ops = append(ops, func(state []byte) ([]byte, error) {
			return dashboarddoc.SetWidgetConfig(state, widgetID, config)
		})
	}
	if req.Type != nil {
		widgetType := string(*req.Type)
		ops = append(ops, func(state []byte) ([]byte, error) {
			return dashboarddoc.SetWidgetType(state, widgetID, widgetType)
		})
	}
	if req.ConnectorID != nil {
		connectorID := *req.ConnectorID
		ops = append(ops, func(state []byte) ([]byte, error) {
			return dashboarddoc.SetWidgetConnector(state, widgetID, connectorID)
		})
	}
	if req.Query != nil {
		query := *req.Query
		ops = append(ops, func(state []byte) ([]byte, error) {
			return dashboarddoc.SetQuery(state, widgetID, query)
		})
	}
	if req.Language != nil {
		language := *req.Language
		ops = append(ops, func(state []byte) ([]byte, error) {
			return dashboarddoc.SetWidgetLanguage(state, widgetID, language)
		})
	}

	newState := state
	for _, apply := range ops {
		newState, err = apply(newState)
		if errors.Is(err, dashboarddoc.ErrWidgetNotFound) {
			writeError(w, http.StatusNotFound, "widget not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update widget")
			return
		}
	}
	if err := s.storeAndMaterializeDashboardDoc(r.Context(), dashID, newState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update widget")
		return
	}

	updateEntry := audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "widget.update", ResourceType: "widget", ResourceID: widgetID,
	}
	if merged.Type == string(models.WidgetCustom) {
		updateEntry.Metadata = map[string]any{"type": "custom", "js_bytes": len(validate.WidgetCustomConfigFrom(merged.Config).JS)}
	}
	s.audit.Log(r.Context(), updateEntry)

	w.WriteHeader(http.StatusNoContent)
}

// @Summary Delete a widget
// @Description Delete a widget from a dashboard
// @Tags dashboards
// @Param id path string true "Dashboard ID"
// @Param widget_id path string true "Widget ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/widgets/{widget_id} [delete]
func (s *Server) handleDeleteWidget(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	widgetID := r.PathValue("widget_id")
	ctx := r.Context()

	// Check edit permission
	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "edit")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you don't have permission to edit this dashboard")
		return
	}

	// Path IDs are canonicalized before any lookup, matching update and
	// convert: a non-UUID path can never address a widget, so it is a 404
	// rather than a document-store parse error.
	widgetUUID, err := uuid.Parse(widgetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	widgetID = widgetUUID.String()

	state, err := s.loadOrSeedDashboardDoc(ctx, claims.OrgID, dashID)
	if errors.Is(err, errDashboardDocNotFound) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	newState, err := dashboarddoc.DeleteWidget(state, widgetID)
	if errors.Is(err, dashboarddoc.ErrWidgetNotFound) {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if err := s.storeAndMaterializeDashboardDoc(ctx, dashID, newState); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "widget.delete", ResourceType: "widget", ResourceID: widgetID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// @Summary Share a dashboard
// @Description Create or get a public share link for a dashboard
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Success 200 {object} map[string]any
// @Success 201 {object} map[string]any
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/share [post]
func (s *Server) handleShareDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "share")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var sharingEnabled bool
	s.db.Pool.QueryRow(ctx, `SELECT public_sharing_enabled FROM orgs WHERE id=$1`, claims.OrgID).Scan(&sharingEnabled)
	if !sharingEnabled {
		writeError(w, http.StatusForbidden, "public sharing is disabled for this organization")
		return
	}

	var token, createdBy string
	var createdAt time.Time
	err = s.db.Pool.QueryRow(ctx,
		`SELECT token, created_by, created_at FROM public_tokens WHERE resource_type='dashboard' AND resource_id=$1`,
		dashID,
	).Scan(&token, &createdBy, &createdAt)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "created_by": createdBy, "created_at": createdAt})
		return
	}

	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token = hex.EncodeToString(tokenBytes)
	createdAt = time.Now()

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_by)
		 VALUES ($1, 'dashboard', $2, $3, $4)`,
		claims.OrgID, dashID, token, claims.UserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.share", ResourceType: "dashboard", ResourceID: dashID,
	})

	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "created_by": claims.UserID, "created_at": createdAt})
}

// @Summary Get dashboard share link
// @Description Get the public share link for a dashboard, if one exists
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
// @Success 200 {object} map[string]any
// @Success 204
// @Security BearerAuth
// @Router /dashboards/{id}/share [get]
func (s *Server) handleGetDashboardShare(w http.ResponseWriter, r *http.Request) {
	// Requires "view" permission — anyone who can see the dashboard can see the link
	dashID := r.PathValue("id")
	ctx := r.Context()

	claims := ClaimsFromContext(r.Context())
	var exists bool
	s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dashboards WHERE id=$1 AND org_id=$2)`, dashID, claims.OrgID).Scan(&exists)
	if !exists {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var token, createdBy string
	var createdAt time.Time
	err := s.db.Pool.QueryRow(ctx,
		`SELECT token, created_by, created_at FROM public_tokens WHERE resource_type='dashboard' AND resource_id=$1`,
		dashID,
	).Scan(&token, &createdBy, &createdAt)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"token": token, "created_by": createdBy, "created_at": createdAt})
}

// @Summary Revoke dashboard share link
// @Description Revoke the public share link for a dashboard
// @Tags dashboards
// @Param id path string true "Dashboard ID"
// @Success 204
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/share [delete]
func (s *Server) handleRevokeDashboardShare(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "share")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`DELETE FROM public_tokens WHERE resource_type='dashboard' AND resource_id=$1`,
		dashID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke share link")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.share_revoke", ResourceType: "dashboard", ResourceID: dashID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) servePublicDashboard(w http.ResponseWriter, r *http.Request, dashID string) {
	ctx := r.Context()

	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE id = $1`,
		dashID,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.FolderID,
		&dash.CreatedBy, &dash.CreatedAt, &dash.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	json.Unmarshal(settingsOut, &dash.Settings)

	// Public pages serve the latest published snapshot and never draft edits;
	// only a dashboard that has never been published falls back to the draft.
	var (
		widgets          []models.Widget
		publishedVersion *dashboardPublishedVersion
	)
	snapshot, snapErr := s.loadPublishedDashboardSnapshot(ctx, dash.OrgID, dashID)
	if snapErr == nil {
		dash.Title = snapshot.Title
		dash.Settings = snapshot.Settings
		widgets = dashboardWidgetsFromSnapshot(dashID, snapshot.Widgets)
		publishedVersion = &dashboardPublishedVersion{Version: snapshot.Version, CreatedAt: snapshot.CreatedAt}
	} else if errors.Is(snapErr, errNoPublishedVersion) {
		widgets, err = s.loadWidgets(ctx, dash.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load widgets failed")
			return
		}
	} else {
		writeError(w, http.StatusInternalServerError, "load widgets failed")
		return
	}

	// Load cell data for all served widgets
	widgetsData := make(map[string]widgetCellData)
	cellIDs := make([]string, 0, len(widgets))
	for _, w := range widgets {
		if w.CellID != nil {
			cellIDs = append(cellIDs, *w.CellID)
		}
	}
	if len(cellIDs) > 0 {
		rows, err := s.db.Pool.Query(ctx,
			`SELECT c.id, c.source, c.type, c.language, c.outputs, COALESCE(c.metadata, '{}'), c.updated_at
			 FROM cells c WHERE c.id = ANY($1)`,
			cellIDs,
		)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var cd widgetCellData
				var outputs, metadata []byte
				if err := rows.Scan(&cd.CellID, &cd.Source, &cd.Type, &cd.Language, &outputs, &metadata, &cd.UpdatedAt); err != nil {
					continue
				}
				cd.Outputs = outputs
				cd.Metadata = metadata
				widgetsData[cd.CellID] = cd
			}
		}
	}

	type publicDashboardWithWidgets struct {
		models.Dashboard
		Widgets           []models.Widget            `json:"widgets"`
		WidgetsData       map[string]widgetCellData  `json:"widgets_data"`
		PublishedVersion  *dashboardPublishedVersion `json:"published_version,omitempty"`
		OrgBranding       *models.OrgBranding        `json:"org_branding"`
		HTMLPanelsEnabled bool                       `json:"html_panels_enabled"`
		CustomCodeEnabled bool                       `json:"custom_code_enabled"`
	}
	writeJSON(w, http.StatusOK, publicDashboardWithWidgets{
		Dashboard:         dash,
		Widgets:           widgets,
		WidgetsData:       widgetsData,
		PublishedVersion:  publishedVersion,
		OrgBranding:       s.orgBranding(ctx, dash.OrgID),
		HTMLPanelsEnabled: s.orgHTMLPanelsEnabled(ctx, dash.OrgID),
		CustomCodeEnabled: s.orgCustomCodeEnabled(ctx, dash.OrgID),
	})
}

func (s *Server) loadWidgets(ctx context.Context, dashID string) ([]models.Widget, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config, created_at, updated_at
		 FROM widgets WHERE dashboard_id = $1 ORDER BY created_at ASC`,
		dashID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var widgets []models.Widget
	for rows.Next() {
		var wgt models.Widget
		var layoutOut, configOut []byte
		if err := rows.Scan(&wgt.ID, &wgt.DashboardID, &wgt.NotebookID, &wgt.CellID,
			&wgt.ConnectorID, &wgt.Query, &wgt.Language, &wgt.Type, &layoutOut, &configOut, &wgt.CreatedAt, &wgt.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(layoutOut, &wgt.Layout)
		json.Unmarshal(configOut, &wgt.Config)
		widgets = append(widgets, wgt)
	}
	if widgets == nil {
		widgets = []models.Widget{}
	}
	return widgets, nil
}
