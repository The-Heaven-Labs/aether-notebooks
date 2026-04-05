package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

type createDashboardRequest struct {
	Title    string                   `json:"title"`
	Settings models.DashboardSettings `json:"settings,omitempty"`
	FolderID *string                  `json:"folder_id,omitempty"`
}

type addWidgetRequest struct {
	NotebookID *string                `json:"notebook_id"`
	CellID     *string                `json:"cell_id"`
	Type       models.WidgetType      `json:"type"`
	Layout     models.WidgetLayout    `json:"layout"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

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

	settingsJSON, _ := json.Marshal(req.Settings)
	ctx := r.Context()

	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO dashboards (org_id, title, settings, created_by, folder_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, title, settings, public_token, folder_id, created_by, created_at, updated_at`,
		claims.OrgID, req.Title, settingsJSON, claims.UserID, req.FolderID,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.PublicToken, &dash.FolderID,
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

	writeJSON(w, http.StatusCreated, dash)
}

func (s *Server) handleListDashboards(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, title, settings, public_token, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE org_id = $1 ORDER BY updated_at DESC`,
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
		if err := rows.Scan(&d.ID, &d.OrgID, &d.Title, &settingsOut, &d.PublicToken, &d.FolderID,
			&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		json.Unmarshal(settingsOut, &d.Settings)
		dashboards = append(dashboards, d)
	}
	if dashboards == nil {
		dashboards = []models.Dashboard{}
	}
	writeJSON(w, http.StatusOK, dashboards)
}

func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, public_token, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE id = $1 AND org_id = $2`,
		dashID, claims.OrgID,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.PublicToken, &dash.FolderID,
		&dash.CreatedBy, &dash.CreatedAt, &dash.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	json.Unmarshal(settingsOut, &dash.Settings)

	widgets, err := s.loadWidgets(ctx, dashID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load widgets failed")
		return
	}

	type dashboardWithWidgets struct {
		models.Dashboard
		Widgets []models.Widget `json:"widgets"`
	}
	writeJSON(w, http.StatusOK, dashboardWithWidgets{Dashboard: dash, Widgets: widgets})
}

func (s *Server) handleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM dashboards WHERE id = $1 AND org_id = $2`,
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

func (s *Server) handleAddWidget(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")

	var req addWidgetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM dashboards WHERE id=$1 AND org_id=$2)", dashID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	if req.Config == nil {
		req.Config = map[string]interface{}{}
	}
	layoutJSON, _ := json.Marshal(req.Layout)
	configJSON, _ := json.Marshal(req.Config)

	var widget models.Widget
	var layoutOut, configOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO widgets (dashboard_id, notebook_id, cell_id, type, layout, config)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, dashboard_id, notebook_id, cell_id, type, layout, config, created_at, updated_at`,
		dashID, req.NotebookID, req.CellID, req.Type, layoutJSON, configJSON,
	).Scan(&widget.ID, &widget.DashboardID, &widget.NotebookID, &widget.CellID,
		&widget.Type, &layoutOut, &configOut, &widget.CreatedAt, &widget.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add widget")
		return
	}
	json.Unmarshal(layoutOut, &widget.Layout)
	json.Unmarshal(configOut, &widget.Config)

	writeJSON(w, http.StatusCreated, widget)
}

func (s *Server) handleUpdateWidget(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	widgetID := r.PathValue("widget_id")

	var req struct {
		Layout *struct {
			Row    int `json:"row"`
			Col    int `json:"col"`
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"layout,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Layout == nil {
		writeError(w, http.StatusBadRequest, "layout required")
		return
	}

	layout, err := json.Marshal(req.Layout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode layout")
		return
	}

	var id string
	err = s.db.Pool.QueryRow(r.Context(),
		`UPDATE widgets SET layout=$1, updated_at=NOW()
         WHERE id=$2 AND dashboard_id=$3
           AND dashboard_id IN (SELECT id FROM dashboards WHERE org_id=$4)
         RETURNING id`,
		layout, widgetID, dashID, claims.OrgID,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteWidget(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	widgetID := r.PathValue("widget_id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM widgets WHERE id = $1 AND dashboard_id = $2
		 AND dashboard_id IN (SELECT id FROM dashboards WHERE org_id = $3)`,
		widgetID, dashID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleShareDashboard(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	result, err := s.db.Pool.Exec(ctx,
		`UPDATE dashboards SET public_token = $1, updated_at = NOW()
		 WHERE id = $2 AND org_id = $3`,
		token, dashID, claims.OrgID,
	)
	if err != nil || result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"public_token": token})
}

func (s *Server) handlePublicDashboard(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	ctx := r.Context()

	var dash models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, public_token, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE public_token = $1`,
		token,
	).Scan(&dash.ID, &dash.OrgID, &dash.Title, &settingsOut, &dash.PublicToken, &dash.FolderID,
		&dash.CreatedBy, &dash.CreatedAt, &dash.UpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	json.Unmarshal(settingsOut, &dash.Settings)

	widgets, err := s.loadWidgets(ctx, dash.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load widgets failed")
		return
	}

	type dashboardWithWidgets struct {
		models.Dashboard
		Widgets []models.Widget `json:"widgets"`
	}
	writeJSON(w, http.StatusOK, dashboardWithWidgets{Dashboard: dash, Widgets: widgets})
}

func (s *Server) loadWidgets(ctx context.Context, dashID string) ([]models.Widget, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, dashboard_id, notebook_id, cell_id, type, layout, config, created_at, updated_at
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
			&wgt.Type, &layoutOut, &configOut, &wgt.CreatedAt, &wgt.UpdatedAt); err != nil {
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
