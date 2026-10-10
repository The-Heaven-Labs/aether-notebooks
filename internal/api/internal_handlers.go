package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
)

// validateInternalToken validates a session/internal JWT. OAuth access
// tokens (ClientID set) are bound to the MCP endpoint and must never be
// accepted by the relay's internal endpoints.
func (s *Server) validateInternalToken(token string) (*auth.Claims, error) {
	claims, err := s.jwt.Validate(token)
	if err != nil {
		return nil, err
	}
	if claims.ClientID != "" {
		return nil, errors.New("oauth access tokens are not valid for internal endpoints")
	}
	return claims, nil
}

// @Summary Get Yjs document
// @Description Returns the Yjs document state for a notebook (internal relay endpoint)
// @Tags internal
// @Produce octet-stream
// @Param notebook_id path string true "Notebook ID"
// @Success 200 {string} binary
// @Failure 500 {object} map[string]string
// @Router /internal/yjs/{notebook_id} [get]
func (s *Server) handleInternalYjsGet(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if _, err := s.validateInternalToken(token); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	var state []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT state FROM yjs_documents WHERE notebook_id = $1`,
		nbID,
	).Scan(&state)
	if err == pgx.ErrNoRows {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(state)
}

// @Summary Update Yjs document
// @Description Stores the Yjs document state for a notebook (internal relay endpoint)
// @Tags internal
// @Accept octet-stream
// @Param notebook_id path string true "Notebook ID"
// @Success 204 {string} string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /internal/yjs/{notebook_id} [put]
func (s *Server) handleInternalYjsPut(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if _, err := s.validateInternalToken(token); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	state, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO yjs_documents (notebook_id, state)
		 VALUES ($1, $2)
		 ON CONFLICT (notebook_id) DO UPDATE SET state = $2, updated_at = NOW()`,
		nbID, state,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary Get dashboard Yjs document
// @Description Returns the Yjs document state for a dashboard, lazily seeding it from the dashboards/widgets rows on first access (internal relay endpoint)
// @Tags internal
// @Produce octet-stream
// @Param dashboard_id path string true "Dashboard ID"
// @Success 200 {string} binary
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /internal/dashboard-yjs/{dashboard_id} [get]
func (s *Server) handleInternalDashboardYjsGet(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if _, err := s.validateInternalToken(token); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	dashID := r.PathValue("dashboard_id")
	ctx := r.Context()

	// The dashboards row is locked FOR UPDATE exactly like MergeAndStore, so a
	// lazy seed and a concurrent store serialize on the same row: without the
	// lock, a store that read "no state" before this seed committed could
	// overwrite the freshly seeded document.
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "begin failed")
		return
	}
	defer tx.Rollback(ctx)

	var locked string
	err = tx.QueryRow(ctx,
		`SELECT id FROM dashboards WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		dashID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var state []byte
	err = tx.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`,
		dashID).Scan(&state)
	if err == nil {
		// Already seeded or stored: return the winner as-is. The deferred
		// rollback releases the row lock.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(state)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// No state yet: seed it from the current dashboards/widgets rows.
	proj, err := buildDashboardDocProjection(ctx, tx, dashID)
	if err != nil {
		slog.Error("internal dashboard yjs: build projection", "dashboard_id", dashID, "error", err)
		writeError(w, http.StatusInternalServerError, "seed failed")
		return
	}
	seeded, err := dashboarddoc.Seed(*proj)
	if err != nil {
		slog.Error("internal dashboard yjs: seed", "dashboard_id", dashID, "error", err)
		writeError(w, http.StatusInternalServerError, "seed failed")
		return
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO dashboard_yjs_documents (dashboard_id, state)
		 VALUES ($1, $2)
		 ON CONFLICT (dashboard_id) DO NOTHING`,
		dashID, seeded); err != nil {
		writeError(w, http.StatusInternalServerError, "seed failed")
		return
	}

	// Re-read so concurrent seeds converge on the first committed state. The
	// ON CONFLICT above makes a lost race a no-op, and the fallback to the
	// freshly built bytes keeps a GET answering even if the winner's insert is
	// still uncommitted.
	winner := seeded
	if err := tx.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`,
		dashID).Scan(&winner); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(winner)
}

// @Summary Update dashboard Yjs document
// @Description Merges and stores the Yjs document state for a dashboard (internal relay endpoint)
// @Tags internal
// @Accept octet-stream
// @Param dashboard_id path string true "Dashboard ID"
// @Success 204 {string} string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /internal/dashboard-yjs/{dashboard_id} [put]
func (s *Server) handleInternalDashboardYjsPut(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing or invalid authorization")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if _, err := s.validateInternalToken(token); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	dashID := r.PathValue("dashboard_id")
	ctx := r.Context()

	state, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	if len(state) == 0 {
		writeError(w, http.StatusBadRequest, "empty body")
		return
	}

	// MergeAndStore treats a missing or trashed dashboard as a silent no-op
	// (so a stale relay can never resurrect one); the endpoint must answer 404
	// explicitly, so check liveness first.
	var locked string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id FROM dashboards WHERE id = $1 AND deleted_at IS NULL`,
		dashID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "dashboard not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	if err := dashboarddoc.MergeAndStore(ctx, s.db.Pool, dashID, state); err != nil {
		slog.Error("internal dashboard yjs: merge and store", "dashboard_id", dashID, "error", err)
		writeError(w, http.StatusInternalServerError, "store failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// buildDashboardDocProjection reads the current dashboards row and its widgets
// into the projection dashboarddoc.Seed consumes. It must run inside the
// transaction holding the dashboard's FOR UPDATE lock so the projection cannot
// race a concurrent store.
//
// dashboards.settings keeps variables under its own key
// (models.DashboardSettings); the document stores them as an ordered array
// separate from the settings map, so they are split out here and re-injected
// by Materialize. Widgets map each row to a WidgetDoc; NULL query/refs become
// nil pointers.
func buildDashboardDocProjection(ctx context.Context, tx pgx.Tx, dashID string) (*dashboarddoc.Projection, error) {
	proj := &dashboarddoc.Projection{
		Settings: map[string]any{},
		Widgets:  map[string]dashboarddoc.WidgetDoc{},
	}

	var settingsRaw []byte
	if err := tx.QueryRow(ctx,
		`SELECT title, settings FROM dashboards WHERE id = $1 AND deleted_at IS NULL`,
		dashID).Scan(&proj.Title, &settingsRaw); err != nil {
		return nil, fmt.Errorf("read dashboard: %w", err)
	}

	settings := map[string]any{}
	if len(settingsRaw) > 0 {
		if err := json.Unmarshal(settingsRaw, &settings); err != nil {
			return nil, fmt.Errorf("decode settings: %w", err)
		}
	}
	if v, ok := settings["variables"]; ok {
		delete(settings, "variables")
		if v != nil {
			arr, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("settings.variables: got %T, want array", v)
			}
			proj.Variables = make([]map[string]any, 0, len(arr))
			for i, e := range arr {
				m, ok := e.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("settings.variables[%d]: got %T, want object", i, e)
				}
				proj.Variables = append(proj.Variables, m)
			}
		}
	}
	proj.Settings = settings

	rows, err := tx.Query(ctx, `
		SELECT id, notebook_id, cell_id, connector_id, query, language, type, layout, config
		FROM widgets WHERE dashboard_id = $1 ORDER BY id`, dashID)
	if err != nil {
		return nil, fmt.Errorf("query widgets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                                     string
			notebookID, cellID, connectorID, query *string
			language, widgetType                   string
			layoutRaw, configRaw                   []byte
		)
		if err := rows.Scan(&id, &notebookID, &cellID, &connectorID, &query,
			&language, &widgetType, &layoutRaw, &configRaw); err != nil {
			return nil, fmt.Errorf("scan widget: %w", err)
		}
		var layout dashboarddoc.Layout
		if err := json.Unmarshal(layoutRaw, &layout); err != nil {
			return nil, fmt.Errorf("widget %s: decode layout: %w", id, err)
		}
		config := map[string]any{}
		if len(configRaw) > 0 {
			if err := json.Unmarshal(configRaw, &config); err != nil {
				return nil, fmt.Errorf("widget %s: decode config: %w", id, err)
			}
		}
		proj.Widgets[id] = dashboarddoc.WidgetDoc{
			ID:          id,
			Type:        widgetType,
			Layout:      layout,
			ConnectorID: connectorID,
			Query:       query,
			Language:    language,
			NotebookID:  notebookID,
			CellID:      cellID,
			Config:      config,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate widgets: %w", err)
	}
	return proj, nil
}

// @Summary Validate authentication
// @Description Validates a JWT token and returns user info (internal relay endpoint)
// @Tags internal
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /internal/auth/validate [get]
func (s *Server) handleInternalAuthValidate(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing token")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	claims, err := s.validateInternalToken(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id": claims.UserID,
		"org_id":  claims.OrgID,
		"role":    claims.Role,
	})
}
