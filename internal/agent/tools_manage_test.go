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
