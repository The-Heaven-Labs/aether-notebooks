package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/dashboard"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

const (
	defaultDashboardQueryCacheSeconds = 30
	dashboardOptionMaxRows            = 1000
	dashboardOptionTimeout            = 30 * time.Second
	dashboardCachePrefix              = "dashq:"
)

type dashboardExecuteRequest struct {
	WidgetID    string         `json:"widget_id"`
	Variables   map[string]any `json:"variables,omitempty"`
	BypassCache bool           `json:"bypass_cache,omitempty"`
}

type dashboardVariableOptionsRequest struct {
	Variables map[string]any `json:"variables,omitempty"`
}

type dashboardQueryResponse struct {
	Outputs        []models.Output        `json:"outputs"`
	Metrics        map[string]interface{} `json:"metrics"`
	Routing        map[string]interface{} `json:"routing,omitempty"`
	Cached         bool                   `json:"cached"`
	CacheExpiresAt *time.Time             `json:"cache_expires_at,omitempty"`
}

// httpQueryError carries an HTTP status out of runDashboardQuery.
type httpQueryError struct {
	status  int
	message string
}

func (e *httpQueryError) Error() string { return e.message }

type dashboardIdentity struct {
	UserID string
	Role   string
}

type dashboardQueryParams struct {
	OrgID           string
	Identity        dashboardIdentity
	ConnectorID     string
	SQL             string
	BypassCache     bool
	CacheSeconds    *int
	MaxRowsOverride int
	Timeout         time.Duration
	CacheScope      string // public token; empty for authenticated runs
}

// @Summary Execute a dashboard query widget
// @Description Runs one query widget as the requesting user, interpolating dashboard variables server-side. Results are cached in Redis for the dashboard's query_cache_seconds (default 30, 0 disables).
// @Tags dashboards
// @Accept json
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param request body object true "widget_id, variables, bypass_cache"
// @Success 200 {object} map[string]interface{} "outputs, metrics, cached"
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]interface{} "service_choice_required with warehouse_id and services"
// @Security BearerAuth
// @Router /dashboards/{id}/execute [post]
func (s *Server) handleExecuteDashboardWidget(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	ctx := r.Context()

	var req dashboardExecuteRequest
	if err := decodeJSON(r, &req); err != nil || req.WidgetID == "" {
		writeError(w, http.StatusBadRequest, "widget_id is required")
		return
	}

	dash, err := s.loadDashboardSettings(ctx, claims.OrgID, dashID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you need view_with_data permission to run dashboard queries")
		return
	}

	widget, err := s.loadQueryWidget(ctx, dashID, req.WidgetID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "widget not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if widget.ConnectorID == nil || widget.Query == nil {
		writeError(w, http.StatusBadRequest, "widget is not a query widget")
		return
	}
	if widget.Language != "" && widget.Language != "sql" {
		writeError(w, http.StatusBadRequest, "only SQL query widgets can be executed")
		return
	}

	if err := dashboard.ValidateVariables(dash.Settings.Variables); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sqlText, err := dashboard.Interpolate(*widget.Query, dash.Settings.Variables, req.Variables)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := s.runDashboardQuery(ctx, dashboardQueryParams{
		OrgID:        claims.OrgID,
		Identity:     dashboardIdentity{UserID: claims.UserID, Role: claims.Role},
		ConnectorID:  *widget.ConnectorID,
		SQL:          sqlText,
		BypassCache:  req.BypassCache,
		CacheSeconds: dash.Settings.QueryCacheSeconds,
	})
	if err != nil {
		writeDashboardQueryError(w, err)
		return
	}

	rowCount := 0
	if len(resp.Outputs) > 0 {
		if rs, ok := resp.Outputs[0].Data.(*executor.ResultSet); ok {
			rowCount = len(rs.Rows)
		}
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.query", ResourceType: "dashboard", ResourceID: dashID,
		Metadata: map[string]any{
			"widget_id":    req.WidgetID,
			"connector_id": *widget.ConnectorID,
			"query":        sqlText,
			"row_count":    rowCount,
			"duration_ms":  time.Since(startTime).Milliseconds(),
			"cache_hit":    resp.Cached,
		},
	})
	writeJSON(w, http.StatusOK, resp)
}

// @Summary Run a dashboard variable's options query
// @Description Executes the query-backed options for a dashboard variable and returns label/value pairs
// @Tags dashboards
// @Accept json
// @Produce json
// @Param id path string true "Dashboard ID"
// @Param name path string true "Variable name"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /dashboards/{id}/variables/{name}/options [post]
func (s *Server) handleDashboardVariableOptions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	dashID := r.PathValue("id")
	name := r.PathValue("name")
	ctx := r.Context()

	var req dashboardVariableOptionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	dash, err := s.loadDashboardSettings(ctx, claims.OrgID, dashID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "view_with_data")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "you need view_with_data permission to run dashboard queries")
		return
	}

	v := findDashboardVariable(dash.Settings.Variables, name)
	if v == nil {
		writeError(w, http.StatusNotFound, "variable not found")
		return
	}
	if v.Options == nil || v.Options.Mode != "query" || v.Options.Query == nil {
		writeError(w, http.StatusBadRequest, "variable has no query-backed options")
		return
	}
	if err := dashboard.ValidateVariables(dash.Settings.Variables); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sqlText, err := dashboard.Interpolate(v.Options.Query.SQL, dash.Settings.Variables, req.Variables)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := s.runDashboardQuery(ctx, dashboardQueryParams{
		OrgID:           claims.OrgID,
		Identity:        dashboardIdentity{UserID: claims.UserID, Role: claims.Role},
		ConnectorID:     v.Options.Query.ConnectorID,
		SQL:             sqlText,
		CacheSeconds:    dash.Settings.QueryCacheSeconds,
		MaxRowsOverride: dashboardOptionMaxRows,
		Timeout:         dashboardOptionTimeout,
	})
	if err != nil {
		writeDashboardQueryError(w, err)
		return
	}

	options, err := optionsFromResult(resp.Outputs, v.Options.LabelColumn, v.Options.ValueColumn)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "dashboard.variable_options", ResourceType: "dashboard", ResourceID: dashID,
		Metadata: map[string]any{"variable": name, "option_count": len(options)},
	})
	writeJSON(w, http.StatusOK, map[string]any{"options": options})
}

func findDashboardVariable(vars []models.DashboardVariable, name string) *models.DashboardVariable {
	for i := range vars {
		if vars[i].Name == name {
			return &vars[i]
		}
	}
	return nil
}

func optionsFromResult(outputs []models.Output, labelCol, valueCol string) ([]models.OptionValue, error) {
	if len(outputs) == 0 {
		return []models.OptionValue{}, nil
	}
	rs, ok := outputs[0].Data.(*executor.ResultSet)
	if !ok || rs == nil {
		return []models.OptionValue{}, nil
	}
	labelIdx, valueIdx := 0, 1
	for i, c := range rs.Columns {
		if c.Name == labelCol && labelCol != "" {
			labelIdx = i
		}
		if c.Name == valueCol && valueCol != "" {
			valueIdx = i
		}
	}
	if len(rs.Columns) == 1 {
		valueIdx = 0
	}
	options := make([]models.OptionValue, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		get := func(i int) string {
			if i < 0 || i >= len(row) || row[i] == nil {
				return ""
			}
			return fmt.Sprintf("%v", row[i])
		}
		options = append(options, models.OptionValue{Label: get(labelIdx), Value: get(valueIdx)})
	}
	return options, nil
}

func (s *Server) loadDashboardSettings(ctx context.Context, orgID, dashID string) (*models.Dashboard, error) {
	var d models.Dashboard
	var settingsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, title, settings, folder_id, created_by, created_at, updated_at
		 FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashID, orgID,
	).Scan(&d.ID, &d.OrgID, &d.Title, &settingsOut, &d.FolderID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(settingsOut, &d.Settings)
	return &d, nil
}

func (s *Server) loadQueryWidget(ctx context.Context, dashID, widgetID string) (*models.Widget, error) {
	var w models.Widget
	var layoutOut, configOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, dashboard_id, notebook_id, cell_id, connector_id, query, language, type, layout, config, created_at, updated_at
		 FROM widgets WHERE id = $1 AND dashboard_id = $2`,
		widgetID, dashID,
	).Scan(&w.ID, &w.DashboardID, &w.NotebookID, &w.CellID, &w.ConnectorID, &w.Query,
		&w.Language, &w.Type, &layoutOut, &configOut, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(layoutOut, &w.Layout)
	json.Unmarshal(configOut, &w.Config)
	return &w, nil
}

func (s *Server) runDashboardQuery(ctx context.Context, p dashboardQueryParams) (*dashboardQueryResponse, error) {
	ttl := defaultDashboardQueryCacheSeconds
	if p.CacheSeconds != nil {
		ttl = *p.CacheSeconds
	}
	cacheKey := dashboardQueryCacheKey(p)
	if s.Cache != nil && ttl > 0 && !p.BypassCache {
		if rs, expires, ok := s.dashboardQueryCacheGet(ctx, cacheKey); ok {
			return dashboardQueryResult(rs, 0, true, &expires), nil
		}
	}

	opened, err := s.openQuery(ctx, p.OrgID, p.Identity.UserID, p.Identity.Role, p.ConnectorID, false)
	if err != nil {
		return nil, err
	}
	defer opened.Exec.Close()

	maxRows := opened.MaxRows
	if p.MaxRowsOverride > 0 && (maxRows == 0 || p.MaxRowsOverride < maxRows) {
		maxRows = p.MaxRowsOverride
	}
	timeout := time.Duration(opened.TimeoutSecs) * time.Second
	if p.Timeout > 0 && (timeout <= 0 || p.Timeout < timeout) {
		timeout = p.Timeout
	}
	maxBytes, err := s.orgCellOutputMaxBytes(ctx, p.OrgID)
	if err != nil {
		return nil, err
	}

	execCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(execCtx, timeout)
		defer cancel()
	}
	execCtx = context.WithValue(execCtx, executor.CtxUserEmail{}, s.userEmail(ctx, p.Identity.UserID))

	queryStart := time.Now()
	result, err := opened.Exec.Execute(execCtx, p.SQL, nil, executor.OutputLimits{MaxBytes: maxBytes, MaxRows: maxRows})
	queryMS := time.Since(queryStart).Milliseconds()
	if err != nil {
		return nil, mapDashboardExecError(err)
	}
	if cacheKey != "" {
		// Detach from the request context so an aborted response can still warm
		// the cache; the write is best-effort.
		s.dashboardQueryCacheSet(context.WithoutCancel(ctx), cacheKey, result, ttl)
	}
	return dashboardQueryResult(result, queryMS, false, nil), nil
}

func mapDashboardExecError(err error) error {
	msg := err.Error()
	isCancelled := errors.Is(err, context.Canceled) || strings.Contains(msg, "context canceled") || strings.Contains(msg, "cancelled")
	switch {
	case isCancelled:
		return &httpQueryError{status: http.StatusUnprocessableEntity, message: "Query cancelled"}
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "context deadline exceeded"):
		return &httpQueryError{status: http.StatusUnprocessableEntity, message: "Query timed out"}
	case executor.IsClickHouseAccessDenied(err):
		return &httpQueryError{status: http.StatusForbidden, message: msg}
	default:
		return &httpQueryError{status: http.StatusUnprocessableEntity, message: msg}
	}
}

func dashboardQueryResult(rs *executor.ResultSet, queryMS int64, cached bool, expires *time.Time) *dashboardQueryResponse {
	return &dashboardQueryResponse{
		Outputs: []models.Output{{Type: "table", Data: rs}},
		Metrics: map[string]interface{}{
			"query_time_ms": queryMS,
			"total_time_ms": queryMS,
		},
		Cached:         cached,
		CacheExpiresAt: expires,
	}
}

func writeDashboardQueryError(w http.ResponseWriter, err error) {
	var httpErr *httpQueryError
	var choice *queryServiceChoiceError
	switch {
	case errors.As(err, &httpErr):
		writeError(w, httpErr.status, httpErr.message)
	case errors.As(err, &choice):
		writeServiceChoiceRequired(w, choice.Choice)
	default:
		writeOpenQueryError(w, err, false)
	}
}

func dashboardQueryCacheKey(p dashboardQueryParams) string {
	if p.SQL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		p.OrgID, p.Identity.UserID, p.CacheScope, p.ConnectorID, p.SQL,
		fmt.Sprintf("%d", p.MaxRowsOverride),
	}, "\x00")))
	return dashboardCachePrefix + hex.EncodeToString(sum[:])
}

func (s *Server) dashboardQueryCacheGet(ctx context.Context, key string) (*executor.ResultSet, time.Time, bool) {
	if s.Cache == nil || key == "" {
		return nil, time.Time{}, false
	}
	getCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	raw, err := s.Cache.Client().Get(getCtx, key).Bytes()
	if err != nil {
		return nil, time.Time{}, false
	}
	var rs executor.ResultSet
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, time.Time{}, false
	}
	expires := time.Now().Add(defaultDashboardQueryCacheSeconds * time.Second)
	if ttl, err := s.Cache.Client().TTL(getCtx, key).Result(); err == nil && ttl > 0 {
		expires = time.Now().Add(ttl)
	}
	return &rs, expires, true
}

func (s *Server) dashboardQueryCacheSet(ctx context.Context, key string, rs *executor.ResultSet, ttlSeconds int) {
	if s.Cache == nil || key == "" || ttlSeconds <= 0 || rs == nil {
		return
	}
	raw, err := json.Marshal(rs)
	if err != nil {
		return
	}
	setCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	s.Cache.Client().Set(setCtx, key, raw, time.Duration(ttlSeconds)*time.Second)
}
