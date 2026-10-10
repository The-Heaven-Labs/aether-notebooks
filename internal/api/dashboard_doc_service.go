package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/models"
)

// errDashboardDocNotFound reports that a dashboard document could not be
// loaded or seeded because the dashboards row is missing or trashed. REST
// handlers translate it into a 404.
var errDashboardDocNotFound = errors.New("dashboard not found")

// errDashboardConnectorNotFound reports that a widget write referenced a
// connector that does not exist in the org (or is soft-deleted). REST
// handlers translate it into a 404 "connector not found".
var errDashboardConnectorNotFound = errors.New("connector not found")

// validateWidgetConnectorRef rejects widget writes whose connector reference
// is missing, soft-deleted, or outside the org. The document store does not
// check connector existence, and the materializer skips widgets with dangling
// references (deleting their derived rows), so without this check a success
// response would silently drop the widget.
func (s *Server) validateWidgetConnectorRef(ctx context.Context, orgID, connectorID string) error {
	if _, err := uuid.Parse(connectorID); err != nil {
		return errDashboardConnectorNotFound
	}
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM connectors WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL)`,
		connectorID, orgID).Scan(&exists); err != nil {
		return fmt.Errorf("check connector reference: %w", err)
	}
	if !exists {
		return errDashboardConnectorNotFound
	}
	return nil
}

// writeConnectorRefError maps a validateWidgetConnectorRef failure onto an
// HTTP response: 404 for a missing reference, 500 otherwise.
func writeConnectorRefError(w http.ResponseWriter, err error) {
	if errors.Is(err, errDashboardConnectorNotFound) {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "connector lookup failed")
}

// errDashboardCellNotFound reports that a widget write referenced a
// notebook/cell pair that does not resolve to a real cell. REST handlers
// translate it into a 404 "cell not found".
var errDashboardCellNotFound = errors.New("cell not found")

// validateWidgetCellRef rejects widget writes whose notebook/cell pair does
// not resolve to a cell in that notebook. The document store does not check
// reference existence, and the materializer skips widgets with dangling
// references (deleting their derived rows), so without this check a success
// response would commit a phantom widget to the document that never
// materializes.
func (s *Server) validateWidgetCellRef(ctx context.Context, notebookID, cellID string) error {
	if _, err := uuid.Parse(notebookID); err != nil {
		return errDashboardCellNotFound
	}
	if _, err := uuid.Parse(cellID); err != nil {
		return errDashboardCellNotFound
	}
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM cells WHERE id = $1 AND notebook_id = $2)`,
		cellID, notebookID).Scan(&exists); err != nil {
		return fmt.Errorf("check cell reference: %w", err)
	}
	if !exists {
		return errDashboardCellNotFound
	}
	return nil
}

// writeCellRefError maps a validateWidgetCellRef failure onto an HTTP
// response: 404 for a missing reference, 500 otherwise.
func writeCellRefError(w http.ResponseWriter, err error) {
	if errors.Is(err, errDashboardCellNotFound) {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "cell lookup failed")
}

// dashboardDocQuerier is the read surface shared by *pgxpool.Pool and pgx.Tx,
// so the projection builder can run standalone or inside the transaction that
// holds the dashboard's FOR UPDATE lock.
type dashboardDocQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// dashboardDocProjection reads the current dashboards row and its widgets
// into the projection dashboarddoc.Seed consumes. Callers on the write path
// run it inside the transaction holding the dashboard's FOR UPDATE lock so the
// projection cannot race a concurrent store. An empty orgID skips the org
// filter (internal relay reads carry no org context); REST callers pass their
// org as defense in depth on top of the permission checks.
//
// dashboards.settings keeps variables under its own key
// (models.DashboardSettings); the document stores them as an ordered array
// separate from the settings map, so they are split out here and re-injected
// by Materialize. Widgets map each row to a WidgetDoc; NULL query/refs become
// nil pointers.
func (s *Server) dashboardDocProjection(ctx context.Context, q dashboardDocQuerier, orgID, dashID string) (*dashboarddoc.Projection, error) {
	proj := &dashboarddoc.Projection{
		Settings: map[string]any{},
		Widgets:  map[string]dashboarddoc.WidgetDoc{},
	}

	dashQuery := `SELECT title, settings FROM dashboards WHERE id = $1 AND deleted_at IS NULL`
	dashArgs := []any{dashID}
	if orgID != "" {
		dashQuery = `SELECT title, settings FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`
		dashArgs = append(dashArgs, orgID)
	}
	var settingsRaw []byte
	if err := q.QueryRow(ctx, dashQuery, dashArgs...).Scan(&proj.Title, &settingsRaw); err != nil {
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

	rows, err := q.Query(ctx, `
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

// loadOrSeedDashboardDoc returns the stored Yjs document state for a
// dashboard, lazily seeding it from the current dashboards/widgets rows when
// no state exists yet. It is the single seed path shared by the internal relay
// GET and the REST write handlers, so the two can never diverge.
//
// The dashboards row is locked FOR UPDATE exactly like MergeAndStore, so a
// lazy seed and a concurrent store serialize on the same row: without the
// lock, a store that read "no state" before this seed committed could
// overwrite the freshly seeded document. An empty orgID skips the org filter
// (internal relay reads carry no org context); REST callers pass their org so
// a cross-org ID can never be read or seeded. A missing or trashed dashboard
// returns errDashboardDocNotFound.
func (s *Server) loadOrSeedDashboardDoc(ctx context.Context, orgID, dashID string) ([]byte, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("load dashboard document %s: begin: %w", dashID, err)
	}
	defer tx.Rollback(ctx)

	lockQuery := `SELECT id FROM dashboards WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	lockArgs := []any{dashID}
	if orgID != "" {
		lockQuery = `SELECT id FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL FOR UPDATE`
		lockArgs = append(lockArgs, orgID)
	}
	var locked string
	err = tx.QueryRow(ctx, lockQuery, lockArgs...).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errDashboardDocNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load dashboard document %s: lock dashboard: %w", dashID, err)
	}

	var state []byte
	err = tx.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`,
		dashID).Scan(&state)
	if err == nil {
		// Already seeded or stored: return the winner as-is. The deferred
		// rollback releases the row lock.
		return state, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load dashboard document %s: read state: %w", dashID, err)
	}

	// No state yet: seed it from the current dashboards/widgets rows.
	proj, err := s.dashboardDocProjection(ctx, tx, orgID, dashID)
	if err != nil {
		return nil, fmt.Errorf("load dashboard document %s: build projection: %w", dashID, err)
	}
	seeded, err := dashboarddoc.Seed(*proj)
	if err != nil {
		return nil, fmt.Errorf("load dashboard document %s: seed: %w", dashID, err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO dashboard_yjs_documents (dashboard_id, state)
		 VALUES ($1, $2)
		 ON CONFLICT (dashboard_id) DO NOTHING`,
		dashID, seeded); err != nil {
		return nil, fmt.Errorf("load dashboard document %s: store seed: %w", dashID, err)
	}

	// Re-read so concurrent seeds converge on the first committed state. The
	// ON CONFLICT above makes a lost race a no-op, and the fallback to the
	// freshly built bytes keeps the caller answering even if the winner's
	// insert is still uncommitted.
	winner := seeded
	if err := tx.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`,
		dashID).Scan(&winner); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load dashboard document %s: read seeded state: %w", dashID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("load dashboard document %s: commit seed: %w", dashID, err)
	}
	return winner, nil
}

// storeAndMaterializeDashboardDoc persists a backend-originated document
// state through dashboarddoc.MergeAndStore (CRDT-merge onto the stored state
// plus materialization of the derived rows) and, on success, fans the state
// out to relay replicas. The publish is best-effort and never fails the
// caller; the durable document and derived rows are already current.
func (s *Server) storeAndMaterializeDashboardDoc(ctx context.Context, dashboardID string, state []byte) error {
	if err := dashboarddoc.MergeAndStore(ctx, s.db.Pool, dashboardID, state); err != nil {
		return err
	}
	s.publishDashboardDocUpdate(ctx, dashboardID, state)
	return nil
}

// agentDashboardDocStore adapts the Server's dashboard document helpers to the
// agent.DashboardDocStore interface (the method names differ, and the adapter
// keeps the Server API clean). Agent and MCP dashboard-mutating tools write
// through it, so their edits land in the document (the source of truth) and
// are materialized exactly like REST writes — a direct SQL write would be
// clobbered by the next relay store.
type agentDashboardDocStore struct{ s *Server }

// LoadOrSeed delegates to the single lazy-seed path shared with the internal
// relay GET and the REST write handlers.
func (a agentDashboardDocStore) LoadOrSeed(ctx context.Context, orgID, dashboardID string) ([]byte, error) {
	return a.s.loadOrSeedDashboardDoc(ctx, orgID, dashboardID)
}

// Store delegates to the merge-and-materialize store shared with the REST
// write handlers, including the best-effort relay publish.
func (a agentDashboardDocStore) Store(ctx context.Context, dashboardID string, state []byte) error {
	return a.s.storeAndMaterializeDashboardDoc(ctx, dashboardID, state)
}

// dashboardVariablesFromJSON converts the JSON value of settings.variables
// into the []map[string]any the document stores. A JSON null clears the
// variable list; any other non-array value, or an array entry that is not an
// object, is an error.
func dashboardVariablesFromJSON(v any) ([]map[string]any, error) {
	if v == nil {
		return []map[string]any{}, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("settings.variables must be an array, got %T", v)
	}
	out := make([]map[string]any, 0, len(arr))
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("settings.variables[%d] must be an object, got %T", i, e)
		}
		out = append(out, m)
	}
	return out, nil
}

// dashboardVariablesFromMaps converts document variable maps into the typed
// form the validation and response layers use. A nil slice stays nil (the
// "leave unchanged" value for UpdateMeta callers); a malformed entry surfaces
// as an error so callers can answer 400 instead of silently dropping it.
func dashboardVariablesFromMaps(vars []map[string]any) ([]models.DashboardVariable, error) {
	if vars == nil {
		return nil, nil
	}
	raw, err := json.Marshal(vars)
	if err != nil {
		return nil, err
	}
	var out []models.DashboardVariable
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// dashboardVariableFromParameter maps a notebook/cell parameter onto a
// dashboard variable and flattens it into the document's map representation,
// mirroring the JSON shape of models.DashboardVariable.
func dashboardVariableFromParameter(p models.Parameter) (map[string]any, error) {
	raw, err := json.Marshal(variableFromParameter(p))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
