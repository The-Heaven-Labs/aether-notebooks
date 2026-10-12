package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
	"github.com/the-heaven-labs/aether/internal/database"
)

// TestUpdatePermissionsRejectsAgentSessions pins the alias-hole guard: the raw
// ACL replace tool must reject agent_session, or an admin-mode actor could
// bypass the read-only share normalization (writing edit grants for a non-owner
// or deleting the owner row) through it.
func TestUpdatePermissionsRejectsAgentSessions(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)

	sessionID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2::uuid, 'user', $3, ARRAY['view','edit','share','delete','admin'])
	`, orgID, sessionID, userID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	def, ok := reg.Get("update_permissions")
	require.True(t, ok, "update_permissions must be registered")
	require.NotNil(t, def.Handler)

	ctx := setupToolContext(t, db, orgID, userID, "")
	args, err := json.Marshal(map[string]any{
		"resource_type": "agent_session",
		"resource_id":   sessionID,
		"entries": []map[string]any{
			{"subject_type": "user", "subject_id": userID, "actions": []string{"edit"}},
		},
	})
	require.NoError(t, err)

	_, err = def.Handler(args, ctx)
	require.Error(t, err, "update_permissions must reject agent_session")
	require.Contains(t, err.Error(), "ACL API")

	var actions []string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT actions FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1::uuid`,
		sessionID).Scan(&actions))
	require.Equal(t, []string{"view", "edit", "share", "delete", "admin"}, actions,
		"a rejected update must leave existing session ACL rows untouched")
}

// testDashboardDocStore is a real Postgres-backed agent.DashboardDocStore for
// tool tests. LoadOrSeed reads the stored Yjs state, lazily seeding it from the
// current dashboards/widgets rows when none exists yet; Store runs
// dashboarddoc.MergeAndStore, the same merge + materialize path the API server
// uses. No mocks: the tools are exercised against the real document and rows.
// Keep the seed projection in sync with api.Server.dashboardDocProjection
// (internal/api/dashboard_doc_service.go).
type testDashboardDocStore struct {
	pool *pgxpool.Pool
	// invalidated, when non-nil, records every Invalidate call in order so a
	// test can assert the hard-delete fan-out without a Redis-backed server.
	invalidated *[]string
}

func (s testDashboardDocStore) LoadOrSeed(ctx context.Context, orgID, dashboardID string) ([]byte, error) {
	var state []byte
	err := s.pool.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashboardID).Scan(&state)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	proj := &dashboarddoc.Projection{
		Settings: map[string]any{},
		Widgets:  map[string]dashboarddoc.WidgetDoc{},
	}
	var settingsRaw []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT title, settings FROM dashboards WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		dashboardID, orgID).Scan(&proj.Title, &settingsRaw); err != nil {
		return nil, fmt.Errorf("dashboard not found: %w", err)
	}
	settings := map[string]any{}
	if len(settingsRaw) > 0 {
		if err := json.Unmarshal(settingsRaw, &settings); err != nil {
			return nil, err
		}
	}
	// The document stores variables as an ordered array separate from the
	// settings map; split them out like the API seed projection does.
	if v, ok := settings["variables"]; ok {
		delete(settings, "variables")
		if arr, ok := v.([]any); ok {
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok {
					proj.Variables = append(proj.Variables, m)
				}
			}
		}
	}
	proj.Settings = settings

	rows, err := s.pool.Query(ctx, `
		SELECT id, notebook_id, cell_id, connector_id, query, language, type, layout, config
		FROM widgets WHERE dashboard_id = $1 ORDER BY id`, dashboardID)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		var layout dashboarddoc.Layout
		if err := json.Unmarshal(layoutRaw, &layout); err != nil {
			return nil, err
		}
		config := map[string]any{}
		if len(configRaw) > 0 {
			if err := json.Unmarshal(configRaw, &config); err != nil {
				return nil, err
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
		return nil, err
	}

	seeded, err := dashboarddoc.Seed(*proj)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO dashboard_yjs_documents (dashboard_id, state) VALUES ($1, $2)
		 ON CONFLICT (dashboard_id) DO NOTHING`, dashboardID, seeded); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashboardID).Scan(&state); err != nil {
		return nil, err
	}
	return state, nil
}

func (s testDashboardDocStore) Store(ctx context.Context, dashboardID string, state []byte) error {
	return dashboarddoc.MergeAndStore(ctx, s.pool, dashboardID, state)
}

func (s testDashboardDocStore) Invalidate(_ context.Context, dashboardID string) {
	if s.invalidated != nil {
		*s.invalidated = append(*s.invalidated, dashboardID)
	}
}

// dashboardToolContext wires the shared test ToolContext with the DB-backed
// dashboard document store, so dashboard-mutating tools run the real
// doc → materialize path.
func dashboardToolContext(t *testing.T, db *database.DB, orgID, userID string) *agent.ToolContext {
	t.Helper()
	tc := setupToolContext(t, db, orgID, userID, "")
	tc.DashboardDocStore = testDashboardDocStore{pool: db.Pool}
	return tc
}

// dashboardTool returns the registered handler for name.
func dashboardTool(t *testing.T, reg *agent.ToolRegistry, name string) agent.ToolHandler {
	t.Helper()
	def, ok := reg.Get(name)
	require.True(t, ok, "%s must be registered", name)
	require.NotNil(t, def.Handler)
	return def.Handler
}

// storedDashboardDocState reads the raw stored Yjs state bytes.
func storedDashboardDocState(t *testing.T, pool *pgxpool.Pool, dashID string) []byte {
	t.Helper()
	var state []byte
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&state))
	return state
}

// storedDashboardDoc reads and projects the stored Yjs document state, so a
// test can assert a write landed in the source of truth, not only in the
// derived rows.
func storedDashboardDoc(t *testing.T, pool *pgxpool.Pool, dashID string) *dashboarddoc.Projection {
	t.Helper()
	proj, err := dashboarddoc.Project(storedDashboardDocState(t, pool, dashID))
	require.NoError(t, err)
	return proj
}

func createTestDashboard(t *testing.T, pool *pgxpool.Pool, orgID, userID, title string) string {
	t.Helper()
	dashID := uuid.NewString()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO dashboards (id, org_id, title, settings, created_by)
		VALUES ($1, $2, $3, '{}', $4)
	`, dashID, orgID, title, userID)
	require.NoError(t, err)
	return dashID
}

func createTestCell(t *testing.T, pool *pgxpool.Pool, notebookID, source string, position int) string {
	t.Helper()
	cellID := uuid.NewString()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position)
		VALUES ($1, $2, 'code', 'sql', $3, $4)
	`, cellID, notebookID, source, position)
	require.NoError(t, err)
	return cellID
}

// TestAgentCreateDashboardWidgetWritesDocAndRow pins the doc-backed create:
// the widget must land in the stored document (the source of truth) and be
// materialized into the widgets row, with the tool's width/height defaults.
func TestAgentCreateDashboardWidgetWritesDocAndRow(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Agent Dashboard")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)

	args, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      cellID,
		"type":         "table",
		"row":          1,
		"col":          2,
	})
	require.NoError(t, err)
	out, err := dashboardTool(t, reg, "create_dashboard_widget")(args, ctx)
	require.NoError(t, err)
	widgetID := out.(map[string]any)["widget_id"].(string)
	_, err = uuid.Parse(widgetID)
	require.NoError(t, err, "the tool must return a UUID widget id")

	// Materialized row: defaults applied, layout stored.
	var nbOut, cellOut, typeOut, layoutOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT notebook_id, cell_id, type, layout::text FROM widgets WHERE id = $1`, widgetID).
		Scan(&nbOut, &cellOut, &typeOut, &layoutOut))
	require.Equal(t, nbID, nbOut)
	require.Equal(t, cellID, cellOut)
	require.Equal(t, "table", typeOut)
	require.JSONEq(t, `{"row":1,"col":2,"width":6,"height":4}`, layoutOut)

	// Stored document: the widget is in the source of truth, and the lazy seed
	// preserved the dashboard title.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	require.Empty(t, proj.Warnings)
	require.Equal(t, "Agent Dashboard", proj.Title)
	w, ok := proj.Widgets[widgetID]
	require.True(t, ok, "widget must be stored in the dashboard document")
	require.Equal(t, dashboarddoc.Layout{Row: 1, Col: 2, Width: 6, Height: 4}, w.Layout)
	require.Equal(t, "table", w.Type)
	require.NotNil(t, w.NotebookID)
	require.Equal(t, nbID, *w.NotebookID)
	require.NotNil(t, w.CellID)
	require.Equal(t, cellID, *w.CellID)
}

// TestAgentCreateDashboardWidgetRejectsDanglingCell pins the REST-parity
// validation: a notebook/cell pair that does not resolve to a real cell must
// fail before any write, or the materializer would silently drop the phantom
// widget.
func TestAgentCreateDashboardWidgetRejectsDanglingCell(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	otherNbID := createTestNotebook(t, db.Pool, orgID, userID)
	otherCellID := createTestCell(t, db.Pool, otherNbID, "SELECT 2", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Dangling")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "create_dashboard_widget")

	// The cell exists, but not in the referenced notebook.
	args, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      otherCellID,
		"type":         "table",
	})
	require.NoError(t, err)
	_, err = handler(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cell not found")

	// Nothing was written: no widget row and no document row.
	var widgets, docs int
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&widgets))
	require.Zero(t, widgets)
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&docs))
	require.Zero(t, docs, "a rejected cell reference must not seed or write the document")
}

// TestAgentUpdateDashboardWidgetLayoutOnlyPreservesSource pins the read-merge:
// a layout-only update must keep the widget's connector, query, and config in
// both the stored document and the materialized row.
func TestAgentUpdateDashboardWidgetLayoutOnlyPreservesSource(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Query Dashboard")

	// The agent create tool only makes cell widgets, so seed a query widget
	// row directly; the tool's load-or-seed projects it into the document.
	connID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		VALUES ($1, $2, 'test-connector', 'postgres', $3)
	`, connID, orgID, []byte("encrypted"))
	require.NoError(t, err)
	widgetID := uuid.NewString()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO widgets (id, dashboard_id, connector_id, query, language, type, layout, config)
		VALUES ($1, $2, $3, 'SELECT 1 AS x', 'sql', 'table',
		        '{"row":0,"col":0,"width":6,"height":6}', '{"showLegend":true}')
	`, widgetID, dashID, connID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)

	args, err := json.Marshal(map[string]any{
		"widget_id":    widgetID,
		"dashboard_id": dashID,
		"row":          3,
		"col":          1,
	})
	require.NoError(t, err)
	out, err := dashboardTool(t, reg, "update_dashboard_widget")(args, ctx)
	require.NoError(t, err)
	require.Equal(t, widgetID, out.(map[string]any)["widget_id"])

	// Materialized row: layout replaced, source untouched.
	var connectorOut, queryOut *string
	var layoutOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT connector_id, query, layout::text FROM widgets WHERE id = $1`, widgetID).
		Scan(&connectorOut, &queryOut, &layoutOut))
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT 1 AS x", *queryOut)
	require.JSONEq(t, `{"row":3,"col":1,"width":6,"height":6}`, layoutOut)

	// Stored document: same read-merge, config untouched.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	require.Empty(t, proj.Warnings)
	w, ok := proj.Widgets[widgetID]
	require.True(t, ok)
	require.Equal(t, dashboarddoc.Layout{Row: 3, Col: 1, Width: 6, Height: 6}, w.Layout)
	require.NotNil(t, w.ConnectorID)
	require.Equal(t, connID, *w.ConnectorID)
	require.NotNil(t, w.Query)
	require.Equal(t, "SELECT 1 AS x", *w.Query)
	require.Equal(t, map[string]any{"showLegend": true}, w.Config)

	// A type-only update must not touch the query or connector either.
	typeArgs, err := json.Marshal(map[string]any{
		"widget_id":    widgetID,
		"dashboard_id": dashID,
		"type":         "metric",
	})
	require.NoError(t, err)
	out, err = dashboardTool(t, reg, "update_dashboard_widget")(typeArgs, ctx)
	require.NoError(t, err)
	require.Equal(t, widgetID, out.(map[string]any)["widget_id"])

	proj = storedDashboardDoc(t, db.Pool, dashID)
	require.Empty(t, proj.Warnings)
	w = proj.Widgets[widgetID]
	require.Equal(t, "metric", w.Type)
	require.NotNil(t, w.ConnectorID)
	require.Equal(t, connID, *w.ConnectorID)
	require.NotNil(t, w.Query)
	require.Equal(t, "SELECT 1 AS x", *w.Query)
	require.Equal(t, map[string]any{"showLegend": true}, w.Config)

	var typeOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT type, connector_id, query FROM widgets WHERE id = $1`, widgetID).
		Scan(&typeOut, &connectorOut, &queryOut))
	require.Equal(t, "metric", typeOut)
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT 1 AS x", *queryOut)

	// A missing widget is reported, not silently ignored.
	missingArgs, err := json.Marshal(map[string]any{
		"widget_id":    uuid.NewString(),
		"dashboard_id": dashID,
		"row":          5,
	})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "update_dashboard_widget")(missingArgs, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "widget not found")
}

// TestAgentDeleteDashboardWidgetRemovesDocAndRow pins the doc-backed delete
// and the missing-widget error.
func TestAgentDeleteDashboardWidgetRemovesDocAndRow(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Delete Dashboard")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)

	createArgs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      cellID,
		"type":         "table",
	})
	require.NoError(t, err)
	created, err := dashboardTool(t, reg, "create_dashboard_widget")(createArgs, ctx)
	require.NoError(t, err)
	widgetID := created.(map[string]any)["widget_id"].(string)

	deleteArgs, err := json.Marshal(map[string]any{"widget_id": widgetID, "dashboard_id": dashID})
	require.NoError(t, err)
	out, err := dashboardTool(t, reg, "delete_dashboard_widget")(deleteArgs, ctx)
	require.NoError(t, err)
	require.Equal(t, "deleted", out.(map[string]any)["status"])

	// Gone from the document and from the derived rows.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	_, present := proj.Widgets[widgetID]
	require.False(t, present, "deleted widget must be removed from the document")
	var widgets int
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE id = $1`, widgetID).Scan(&widgets))
	require.Zero(t, widgets)

	// A second delete of the same widget reports it missing.
	_, err = dashboardTool(t, reg, "delete_dashboard_widget")(deleteArgs, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "widget not found")
}

// TestAgentUpdateDashboardWritesDocAndRow pins the meta write: title and
// grid_cols must land in the document and materialize, and a partial update
// must preserve every other setting.
func TestAgentUpdateDashboardWritesDocAndRow(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO dashboards (id, org_id, title, settings, created_by)
		VALUES ($1, $2, 'Old Title', '{"grid_cols":12,"auto_refresh_seconds":60}', $3)
	`, dashID, orgID, userID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "update_dashboard")

	// Title-only update: settings survive in the document and the row.
	args, err := json.Marshal(map[string]any{"dashboard_id": dashID, "title": "New Title"})
	require.NoError(t, err)
	out, err := handler(args, ctx)
	require.NoError(t, err)
	require.Equal(t, "updated", out.(map[string]any)["status"])

	proj := storedDashboardDoc(t, db.Pool, dashID)
	require.Empty(t, proj.Warnings)
	require.Equal(t, "New Title", proj.Title)
	require.EqualValues(t, 12, proj.Settings["grid_cols"])
	require.EqualValues(t, 60, proj.Settings["auto_refresh_seconds"])
	var rowTitle, settingsOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT title, settings::text FROM dashboards WHERE id = $1`, dashID).
		Scan(&rowTitle, &settingsOut))
	require.Equal(t, "New Title", rowTitle)
	require.JSONEq(t, `{"grid_cols":12,"auto_refresh_seconds":60,"variables":[]}`, settingsOut)

	// grid_cols-only update: title and every other setting survive.
	args, err = json.Marshal(map[string]any{"dashboard_id": dashID, "grid_cols": 6})
	require.NoError(t, err)
	_, err = handler(args, ctx)
	require.NoError(t, err)

	proj = storedDashboardDoc(t, db.Pool, dashID)
	require.Equal(t, "New Title", proj.Title)
	require.EqualValues(t, 6, proj.Settings["grid_cols"])
	require.EqualValues(t, 60, proj.Settings["auto_refresh_seconds"])
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT title, settings::text FROM dashboards WHERE id = $1`, dashID).
		Scan(&rowTitle, &settingsOut))
	require.Equal(t, "New Title", rowTitle)
	require.JSONEq(t, `{"grid_cols":6,"auto_refresh_seconds":60,"variables":[]}`, settingsOut)
}

// TestAgentUpdateDashboardWidgetRejectsDeletedCell pins the merged-reference
// check: a widget whose cell was hard-deleted must fail the update (the
// materializer would skip it), instead of reporting success for a document
// entry that never materializes.
func TestAgentUpdateDashboardWidgetRejectsDeletedCell(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Deleted Cell")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)

	createArgs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      cellID,
		"type":         "table",
	})
	require.NoError(t, err)
	created, err := dashboardTool(t, reg, "create_dashboard_widget")(createArgs, ctx)
	require.NoError(t, err)
	widgetID := created.(map[string]any)["widget_id"].(string)

	// Hard-delete the cell; the widgets row cascades away, while the document
	// entry remains (only a later store would materialize its removal).
	_, err = db.Pool.Exec(context.Background(), `DELETE FROM cells WHERE id = $1`, cellID)
	require.NoError(t, err)

	before := storedDashboardDocState(t, db.Pool, dashID)
	args, err := json.Marshal(map[string]any{
		"widget_id":    widgetID,
		"dashboard_id": dashID,
		"row":          2,
	})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "update_dashboard_widget")(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cell not found")

	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a rejected merged reference must not rewrite the document")
}

// TestAgentUpdateDashboardWidgetNoopLeavesDocUntouched pins the no-op guard: a
// request with no updatable fields must error without rewriting (or
// re-publishing) the stored document.
func TestAgentUpdateDashboardWidgetNoopLeavesDocUntouched(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Noop Dashboard")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)

	createArgs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      cellID,
		"type":         "table",
	})
	require.NoError(t, err)
	created, err := dashboardTool(t, reg, "create_dashboard_widget")(createArgs, ctx)
	require.NoError(t, err)
	widgetID := created.(map[string]any)["widget_id"].(string)

	before := storedDashboardDocState(t, db.Pool, dashID)
	args, err := json.Marshal(map[string]any{"widget_id": widgetID, "dashboard_id": dashID})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "update_dashboard_widget")(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nothing to update")

	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a no-op update must not rewrite the stored document")
}

// TestAgentDashboardWidgetRejectsInvalidTypeBeforeWrite pins the pre-write
// type validation on both widget write tools: an invalid type must be rejected
// without lazily seeding the document or rewriting it.
func TestAgentDashboardWidgetRejectsInvalidTypeBeforeWrite(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Invalid Type")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)

	// Create: rejected before any write, so no document row appears.
	args, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      cellID,
		"type":         "pie",
	})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "create_dashboard_widget")(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid widget type")
	var docs int
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&docs))
	require.Zero(t, docs, "a rejected type must not seed or write the document")

	// Update: rejected before the read-merge/write, so the document is
	// byte-identical afterwards.
	createArgs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"notebook_id":  nbID,
		"cell_id":      cellID,
		"type":         "table",
	})
	require.NoError(t, err)
	created, err := dashboardTool(t, reg, "create_dashboard_widget")(createArgs, ctx)
	require.NoError(t, err)
	widgetID := created.(map[string]any)["widget_id"].(string)

	before := storedDashboardDocState(t, db.Pool, dashID)
	args, err = json.Marshal(map[string]any{
		"widget_id":    widgetID,
		"dashboard_id": dashID,
		"type":         "pie",
	})
	require.NoError(t, err)
	_, err = dashboardTool(t, reg, "update_dashboard_widget")(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid widget type")
	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a rejected type must not rewrite the document")
}

// TestAgentDashboardToolsFailClosedWithoutDocStore pins the fail-closed guard:
// a ToolContext with no DashboardDocStore must return an error (never panic or
// silently write through SQL) for every dashboard-mutating tool.
func TestAgentDashboardToolsFailClosedWithoutDocStore(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1", 0)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "No Store")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	// setupToolContext deliberately leaves DashboardDocStore nil.
	ctx := setupToolContext(t, db, orgID, userID, "")

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"create_dashboard_widget", map[string]any{
			"dashboard_id": dashID, "notebook_id": nbID, "cell_id": cellID, "type": "table",
		}},
		{"update_dashboard_widget", map[string]any{
			"widget_id": uuid.NewString(), "dashboard_id": dashID, "row": 1,
		}},
		{"delete_dashboard_widget", map[string]any{
			"widget_id": uuid.NewString(), "dashboard_id": dashID,
		}},
		{"update_dashboard", map[string]any{"dashboard_id": dashID, "title": "New"}},
		{"update_dashboard_settings", map[string]any{
			"dashboard_id": dashID, "app": map[string]any{"title": "App"},
		}},
		{"set_dashboard_layout", map[string]any{
			"dashboard_id": dashID,
			"placements":   []map[string]any{{"widget_id": uuid.NewString(), "tab": "overview"}},
		}},
		{"publish_dashboard", map[string]any{"dashboard_id": dashID}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			args, err := json.Marshal(tc.args)
			require.NoError(t, err)
			_, err = dashboardTool(t, reg, tc.tool)(args, ctx)
			require.Error(t, err)
			require.Contains(t, err.Error(), "dashboard document store not configured")
		})
	}

	// Nothing was written by any rejected call.
	var docs, widgets int
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&docs))
	require.Zero(t, docs)
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&widgets))
	require.Zero(t, widgets)
}

// TestAgentDeleteDashboardInvalidatesDocStore pins the hard-delete fan-out:
// once the dashboards row is gone the tool must invalidate the deleted
// dashboard's document (so relay replicas drop any live copy) exactly once,
// and a failed delete must not invalidate.
func TestAgentDeleteDashboardInvalidatesDocStore(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Invalidate Me")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)

	var invalidated []string
	ctx := setupToolContext(t, db, orgID, userID, "")
	ctx.DashboardDocStore = testDashboardDocStore{pool: db.Pool, invalidated: &invalidated}

	args, err := json.Marshal(map[string]any{"dashboard_id": dashID})
	require.NoError(t, err)
	out, err := dashboardTool(t, reg, "delete_dashboard")(args, ctx)
	require.NoError(t, err)
	require.Equal(t, "deleted", out.(map[string]any)["status"])
	require.Equal(t, []string{dashID}, invalidated,
		"the deleted dashboard id must be invalidated exactly once")

	var count int
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboards WHERE id = $1`, dashID).Scan(&count))
	require.Zero(t, count, "the dashboard row must be hard-deleted")

	// A second delete finds nothing: no further invalidation is published.
	_, err = dashboardTool(t, reg, "delete_dashboard")(args, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dashboard not found")
	require.Equal(t, []string{dashID}, invalidated,
		"a failed delete must not invalidate")
}

// TestAgentDeleteDashboardWithoutDocStore pins the optional-store path: unlike
// the document-mutating dashboard tools, hard delete must still work (and not
// panic) when no document store is configured.
func TestAgentDeleteDashboardWithoutDocStore(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "No Store Delete")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	// setupToolContext deliberately leaves DashboardDocStore nil.
	ctx := setupToolContext(t, db, orgID, userID, "")

	args, err := json.Marshal(map[string]any{"dashboard_id": dashID})
	require.NoError(t, err)
	out, err := dashboardTool(t, reg, "delete_dashboard")(args, ctx)
	require.NoError(t, err)
	require.Equal(t, "deleted", out.(map[string]any)["status"])

	var count int
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboards WHERE id = $1`, dashID).Scan(&count))
	require.Zero(t, count)
}

// TestDashboardUpdateSettingsMaterializes pins the settings tool: app.title +
// tabs must land in the dashboard document (the source of truth) and
// materialize into the dashboards row, and an invalid theme must be rejected
// before the document is touched.
func TestDashboardUpdateSettingsMaterializes(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Settings Dashboard")

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "update_dashboard_settings")

	args, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"app": map[string]any{
			"title": "Sales App",
			"theme": map[string]any{"accent": "#7c5cff"},
		},
		"tabs": []map[string]any{
			{"id": "overview", "title": "Overview"},
			{"id": "deep-dive", "title": "Deep dive"},
		},
	})
	require.NoError(t, err)
	out, err := handler(args, ctx)
	require.NoError(t, err)
	require.Equal(t, "updated", out.(map[string]any)["status"])

	// Stored document: app and tabs are in the source of truth.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	app, ok := proj.Settings["app"].(map[string]any)
	require.True(t, ok, "app settings must be stored in the document")
	require.Equal(t, "Sales App", app["title"])
	theme, ok := app["theme"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "#7c5cff", theme["accent"])
	tabs, ok := proj.Settings["tabs"].([]any)
	require.True(t, ok, "tabs must be stored in the document")
	require.Len(t, tabs, 2)
	require.Equal(t, "overview", tabs[0].(map[string]any)["id"])
	require.Equal(t, "deep-dive", tabs[1].(map[string]any)["id"])

	// Materialized row: the same settings JSON.
	var settingsOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT settings::text FROM dashboards WHERE id = $1`, dashID).Scan(&settingsOut))
	require.JSONEq(t, `{
		"app":{"title":"Sales App","theme":{"accent":"#7c5cff"}},
		"tabs":[{"id":"overview","title":"Overview"},{"id":"deep-dive","title":"Deep dive"}],
		"variables":[]
	}`, settingsOut)

	// An invalid theme must be rejected without rewriting the document.
	before := storedDashboardDocState(t, db.Pool, dashID)
	badArgs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"app":          map[string]any{"theme": map[string]any{"accent": "red"}},
	})
	require.NoError(t, err)
	_, err = handler(badArgs, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "hex color")
	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a rejected theme must not rewrite the document")
}

// TestDashboardSetLayoutPlacesWidgets pins the layout tool: tabs land in the
// document settings, a placement moves the widget's layout.tab, and an
// appearance patch merges into the widget's config — all through one store.
// An invalid tab id must fail without rewriting the document.
func TestDashboardSetLayoutPlacesWidgets(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Layout Dashboard")

	widgetID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO widgets (id, dashboard_id, query, language, type, layout, config)
		VALUES ($1, $2, 'SELECT 1 AS x', 'sql', 'table',
		        '{"row":0,"col":0,"width":6,"height":6}', '{"showLegend":true}')
	`, widgetID, dashID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "set_dashboard_layout")

	args, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"tabs": []map[string]any{
			{"id": "overview", "title": "Overview"},
			{"id": "deep-dive", "title": "Deep dive"},
		},
		"placements": []map[string]any{{"widget_id": widgetID, "tab": "deep-dive"}},
		"appearance": []map[string]any{
			{"widget_id": widgetID, "appearance": map[string]any{"background": "transparent"}},
		},
	})
	require.NoError(t, err)
	out, err := handler(args, ctx)
	require.NoError(t, err)
	require.Equal(t, "updated", out.(map[string]any)["status"])

	// Stored document: tabs, tab placement, and merged appearance.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	tabs, ok := proj.Settings["tabs"].([]any)
	require.True(t, ok)
	require.Len(t, tabs, 2)
	w, ok := proj.Widgets[widgetID]
	require.True(t, ok)
	require.Equal(t, "deep-dive", w.Layout.Tab)
	require.Equal(t, true, w.Config["showLegend"], "unrelated config keys must survive")
	appearance, ok := w.Config["appearance"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "transparent", appearance["background"])

	// Materialized row mirrors the document.
	var layoutOut, configOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT layout::text, config::text FROM widgets WHERE id = $1`, widgetID).
		Scan(&layoutOut, &configOut))
	require.JSONEq(t, `{"row":0,"col":0,"width":6,"height":6,"tab":"deep-dive"}`, layoutOut)
	require.JSONEq(t, `{"showLegend":true,"appearance":{"background":"transparent"}}`, configOut)

	// A bad tab id is rejected before the document is rewritten.
	before := storedDashboardDocState(t, db.Pool, dashID)
	badArgs, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"placements":   []map[string]any{{"widget_id": widgetID, "tab": "No Spaces!"}},
	})
	require.NoError(t, err)
	_, err = handler(badArgs, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid tab id")
	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a rejected placement must not rewrite the document")
}

// TestDashboardPublishCreatesVersions pins the publish tool: each call
// snapshots the dashboard document into the next dashboard_versions row, so
// two calls produce versions 1 and 2 with the same content hash when nothing
// changed in between.
func TestDashboardPublishCreatesVersions(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Publish Dashboard")

	connID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		VALUES ($1, $2, 'publish-connector', 'postgres', $3)
	`, connID, orgID, []byte("encrypted"))
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO widgets (id, dashboard_id, connector_id, query, language, type, layout, config)
		VALUES ($1, $2, $3, 'SELECT 1 AS x', 'sql', 'table',
		        '{"row":0,"col":0,"width":6,"height":6}', '{}')
	`, uuid.NewString(), dashID, connID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "publish_dashboard")

	args, err := json.Marshal(map[string]any{"dashboard_id": dashID})
	require.NoError(t, err)

	first, err := handler(args, ctx)
	require.NoError(t, err)
	firstOut := first.(map[string]any)
	require.Equal(t, 1, firstOut["version"])
	hash, ok := firstOut["content_hash"].(string)
	require.True(t, ok)
	require.Len(t, hash, 64)

	second, err := handler(args, ctx)
	require.NoError(t, err)
	secondOut := second.(map[string]any)
	require.Equal(t, 2, secondOut["version"])
	require.Equal(t, hash, secondOut["content_hash"],
		"an unchanged dashboard must publish the same content hash")

	rows, err := db.Pool.Query(context.Background(),
		`SELECT version, content_hash FROM dashboard_versions WHERE dashboard_id = $1 ORDER BY version`, dashID)
	require.NoError(t, err)
	defer rows.Close()
	var versions []int
	for rows.Next() {
		var v int
		var h string
		require.NoError(t, rows.Scan(&v, &h))
		require.Len(t, h, 64)
		versions = append(versions, v)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int{1, 2}, versions)

	// A missing dashboard is an error, not a version row.
	missingArgs, err := json.Marshal(map[string]any{"dashboard_id": uuid.NewString()})
	require.NoError(t, err)
	_, err = handler(missingArgs, ctx)
	require.Error(t, err)
}

// TestDashboardCreateQueryWidget pins the query-backed create path: a widget
// with connector_id + query needs no notebook/cell and stores config.metric,
// while a request with neither source is rejected.
func TestDashboardCreateQueryWidget(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Query Widget Dashboard")

	connID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		VALUES ($1, $2, 'query-connector', 'postgres', $3)
	`, connID, orgID, []byte("encrypted"))
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "create_dashboard_widget")

	args, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"connector_id": connID,
		"query":        "SELECT 1 AS value",
		"type":         "metric",
		"config": map[string]any{
			"metric": map[string]any{"value_column": "value", "aggregation": "sum"},
		},
	})
	require.NoError(t, err)
	out, err := handler(args, ctx)
	require.NoError(t, err)
	widgetID := out.(map[string]any)["widget_id"].(string)

	// Materialized row: query source, no notebook/cell, metric config.
	var connectorOut, queryOut, typeOut, configOut string
	var notebookOut, cellOut *string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT connector_id, query, type, notebook_id, cell_id, config::text
		 FROM widgets WHERE id = $1`, widgetID).
		Scan(&connectorOut, &queryOut, &typeOut, &notebookOut, &cellOut, &configOut))
	require.Equal(t, connID, connectorOut)
	require.Equal(t, "SELECT 1 AS value", queryOut)
	require.Equal(t, "metric", typeOut)
	require.Nil(t, notebookOut)
	require.Nil(t, cellOut)
	require.JSONEq(t, `{"metric":{"value_column":"value","aggregation":"sum"}}`, configOut)

	// Stored document: same query source and config.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	require.Empty(t, proj.Warnings)
	w, ok := proj.Widgets[widgetID]
	require.True(t, ok)
	require.NotNil(t, w.ConnectorID)
	require.Equal(t, connID, *w.ConnectorID)
	require.NotNil(t, w.Query)
	require.Equal(t, "SELECT 1 AS value", *w.Query)
	require.Nil(t, w.NotebookID)
	require.Nil(t, w.CellID)
	metric, ok := w.Config["metric"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "value", metric["value_column"])

	// Without a connector/query source the tool still requires notebook/cell.
	noSource, err := json.Marshal(map[string]any{"dashboard_id": dashID, "type": "table"})
	require.NoError(t, err)
	_, err = handler(noSource, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires")

	// A dangling connector reference must fail before any write.
	before := storedDashboardDocState(t, db.Pool, dashID)
	badConn, err := json.Marshal(map[string]any{
		"dashboard_id": dashID,
		"connector_id": uuid.NewString(),
		"query":        "SELECT 1",
		"type":         "table",
	})
	require.NoError(t, err)
	_, err = handler(badConn, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "connector not found")
	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a rejected connector reference must not rewrite the document")
}

// TestDashboardUpdateWidgetMergesConfigAndTab pins the update extensions:
// config is shallow-merged into the widget's existing config (unrelated keys
// survive), tab sets layout.tab, and an invalid tab id is rejected.
func TestDashboardUpdateWidgetMergesConfigAndTab(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	dashID := createTestDashboard(t, db.Pool, orgID, userID, "Widget Update Dashboard")

	widgetID := uuid.NewString()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO widgets (id, dashboard_id, query, language, type, layout, config)
		VALUES ($1, $2, 'SELECT 1 AS x', 'sql', 'table',
		        '{"row":0,"col":0,"width":6,"height":6}', '{"showLegend":true}')
	`, widgetID, dashID)
	require.NoError(t, err)

	reg := agent.NewToolRegistry()
	agent.RegisterManageTools(reg, db.Pool)
	ctx := dashboardToolContext(t, db, orgID, userID)
	handler := dashboardTool(t, reg, "update_dashboard_widget")

	args, err := json.Marshal(map[string]any{
		"widget_id":    widgetID,
		"dashboard_id": dashID,
		"tab":          "overview",
		"config": map[string]any{
			"metric": map[string]any{"value_column": "x", "aggregation": "sum"},
		},
	})
	require.NoError(t, err)
	out, err := handler(args, ctx)
	require.NoError(t, err)
	require.Equal(t, widgetID, out.(map[string]any)["widget_id"])

	// Stored document: config merged, layout.tab set, query untouched.
	proj := storedDashboardDoc(t, db.Pool, dashID)
	w, ok := proj.Widgets[widgetID]
	require.True(t, ok)
	require.Equal(t, "overview", w.Layout.Tab)
	require.Equal(t, true, w.Config["showLegend"], "unrelated config keys must survive the merge")
	metric, ok := w.Config["metric"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "x", metric["value_column"])
	require.NotNil(t, w.Query)
	require.Equal(t, "SELECT 1 AS x", *w.Query)

	// Materialized row mirrors the document.
	var layoutOut, configOut string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT layout::text, config::text FROM widgets WHERE id = $1`, widgetID).
		Scan(&layoutOut, &configOut))
	require.JSONEq(t, `{"row":0,"col":0,"width":6,"height":6,"tab":"overview"}`, layoutOut)
	require.JSONEq(t, `{"showLegend":true,"metric":{"value_column":"x","aggregation":"sum"}}`, configOut)

	// A bad tab id is rejected before the document is rewritten.
	before := storedDashboardDocState(t, db.Pool, dashID)
	badArgs, err := json.Marshal(map[string]any{
		"widget_id":    widgetID,
		"dashboard_id": dashID,
		"tab":          "Not A Tab",
	})
	require.NoError(t, err)
	_, err = handler(badArgs, ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid tab id")
	require.Equal(t, before, storedDashboardDocState(t, db.Pool, dashID),
		"a rejected tab must not rewrite the document")
}
