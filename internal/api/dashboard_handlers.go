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
		allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", d.ID, "view")
		if !allowed {
			continue
		}
		dashboards = append(dashboards, d)
	}
	if dashboards == nil {
		dashboards = []models.Dashboard{}
	}
	writeJSON(w, http.StatusOK, dashboards)
}

// @Summary Get a dashboard
// @Description Get a dashboard with its widgets
// @Tags dashboards
// @Produce json
// @Param id path string true "Dashboard ID"
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

	widgets, err := s.loadWidgets(ctx, dashID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load widgets failed")
		return
	}

	type dashboardWithWidgets struct {
		models.Dashboard
		Widgets         []models.Widget           `json:"widgets"`
		WidgetsData     map[string]widgetCellData `json:"widgets_data,omitempty"`
		CanViewWithData bool                      `json:"can_view_with_data"`
		CanShare        bool                      `json:"can_share"`
		CanEdit         bool                      `json:"can_edit"`
	}

	resp := dashboardWithWidgets{
		Dashboard:       dash,
		Widgets:         widgets,
		CanViewWithData: viewWithData,
		CanShare:        shareOK,
		CanEdit:         editOK,
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
	case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric:
	default:
		writeError(w, http.StatusBadRequest, "invalid widget type")
		return
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
		Layout:      dashboarddoc.Layout{Row: req.Layout.Row, Col: req.Layout.Col, Width: req.Layout.Width, Height: req.Layout.Height},
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

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "widget.create", ResourceType: "widget", ResourceID: widget.ID,
	})

	writeJSON(w, http.StatusCreated, widget)
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

	var req struct {
		Layout *struct {
			Row    int `json:"row"`
			Col    int `json:"col"`
			Width  int `json:"width"`
			Height int `json:"height"`
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
		case models.WidgetChart, models.WidgetTable, models.WidgetText, models.WidgetMetric:
		default:
			writeError(w, http.StatusBadRequest, "invalid widget type")
			return
		}
	}
	if req.ConnectorID != nil && *req.ConnectorID == "" {
		writeError(w, http.StatusBadRequest, "connector_id cannot be empty")
		return
	}

	if req.Layout != nil {
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

	// Read-merge: the request is partial, so the projected widget is the base
	// and only the provided fields are overwritten — a layout-only update must
	// keep query/connector/notebook untouched. UpsertWidget then writes the
	// whole widget as one LWW unit (Task 9's documented semantics).
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
	// A connector must not be added on top of a notebook-cell link; the
	// document write would reject the mixed shape anyway.
	if req.ConnectorID != nil && (existing.NotebookID != nil || existing.CellID != nil) {
		writeError(w, http.StatusBadRequest, "widget cannot reference both a notebook cell and a query connector")
		return
	}

	if req.Layout != nil {
		existing.Layout = dashboarddoc.Layout{
			Row:    req.Layout.Row,
			Col:    req.Layout.Col,
			Width:  req.Layout.Width,
			Height: req.Layout.Height,
		}
	}
	if req.Config != nil {
		existing.Config = req.Config
	}
	if req.Type != nil {
		existing.Type = string(*req.Type)
	}
	if req.ConnectorID != nil {
		existing.ConnectorID = req.ConnectorID
	}
	if req.Query != nil {
		existing.Query = req.Query
	}
	if req.Language != nil {
		existing.Language = *req.Language
	}

	newState, err := dashboarddoc.UpsertWidget(state, existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update widget")
		return
	}
	if err := s.storeAndMaterializeDashboardDoc(r.Context(), dashID, newState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update widget")
		return
	}

	s.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "widget.update", ResourceType: "widget", ResourceID: widgetID,
	})

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

	widgets, err := s.loadWidgets(ctx, dash.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load widgets failed")
		return
	}

	// Load cell data for all widgets
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
		Widgets     []models.Widget           `json:"widgets"`
		WidgetsData map[string]widgetCellData `json:"widgets_data"`
	}
	writeJSON(w, http.StatusOK, publicDashboardWithWidgets{Dashboard: dash, Widgets: widgets, WidgetsData: widgetsData})
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
