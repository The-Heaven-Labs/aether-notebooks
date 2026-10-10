package dashboarddoc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
)

// setupStoreTestDB connects to the dev/test Postgres with the same convention
// as the rest of the repo (AETHER_DATABASE_URL, falling back to the local dev
// default) and makes sure migrations through V129 are applied.
func setupStoreTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background()))
	t.Cleanup(db.Close)
	return db
}

// storeTestFixture is the minimal row set MergeAndStore needs: an org, a user
// (dashboards.created_by FK), a dashboard and a connector (widget source FK).
type storeTestFixture struct {
	orgID       string
	userID      string
	dashID      string
	connectorID string
}

func seedStoreTestFixture(t *testing.T, db *database.DB) storeTestFixture {
	t.Helper()
	ctx := context.Background()
	f := storeTestFixture{
		orgID:       uuid.NewString(),
		userID:      uuid.NewString(),
		dashID:      uuid.NewString(),
		connectorID: uuid.NewString(),
	}
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, 'Dashboarddoc Test Org', $2)`,
		f.orgID, "ddoc-"+f.orgID[:8])
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, 'Dashboarddoc User', 'hash')`,
		f.userID, "ddoc-"+f.userID[:8]+"@example.com")
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO connectors (id, org_id, name, type, config_encrypted) VALUES ($1, $2, 'Dashboarddoc Connector', 'postgres', '\x00')`,
		f.connectorID, f.orgID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO dashboards (id, org_id, title, created_by) VALUES ($1, $2, 'Fixture Dashboard', $3)`,
		f.dashID, f.orgID, f.userID)
	require.NoError(t, err)

	t.Cleanup(func() {
		// dashboards.created_by has no ON DELETE, so the org (and, by
		// cascade, its dashboards, widgets and doc states) must go first.
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, f.orgID)
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.userID)
	})
	return f
}

// storeTestBaseState seeds the dashboard document the store tests start from:
// a query widget (connector + SQL), a text widget, and ordered variables.
func storeTestBaseState(t *testing.T, connectorID string) []byte {
	t.Helper()
	query := "SELECT 1"
	state, err := Seed(Projection{
		Title:    "Base Title",
		Settings: map[string]any{"grid_cols": 12, "query_cache_seconds": 30},
		Variables: []map[string]any{
			{"name": "region", "label": "Region", "type": "single_select", "default": "us"},
			{"name": "threshold", "label": "Threshold", "type": "number", "default": 0.5},
		},
		Widgets: map[string]WidgetDoc{
			testUUID(1): {
				ID:          testUUID(1),
				Type:        "table",
				Layout:      Layout{Row: 0, Col: 0, Width: 6, Height: 4},
				ConnectorID: &connectorID,
				Query:       &query,
				Language:    "sql",
				Config:      map[string]any{"striped": true},
			},
			testUUID(2): {
				ID:     testUUID(2),
				Type:   "text",
				Layout: Layout{Row: 0, Col: 6, Width: 6, Height: 4},
				Config: map[string]any{"markdown": "# Notes"},
			},
		},
	})
	require.NoError(t, err)
	return state
}

// storeTestWidget is the materialized widget row shape the tests assert on.
type storeTestWidget struct {
	ID          string
	Type        string
	Language    string
	NotebookID  *string
	CellID      *string
	ConnectorID *string
	Query       *string
	Layout      Layout
	LayoutRaw   map[string]any
	Config      map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func loadStoreTestWidgets(t *testing.T, db *database.DB, dashID string) map[string]storeTestWidget {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(), `
		SELECT id, type, language, notebook_id, cell_id, connector_id, query, layout, config, created_at, updated_at
		FROM widgets WHERE dashboard_id = $1 ORDER BY id`, dashID)
	require.NoError(t, err)
	defer rows.Close()

	out := map[string]storeTestWidget{}
	for rows.Next() {
		var w storeTestWidget
		var layoutJSON, configJSON []byte
		require.NoError(t, rows.Scan(&w.ID, &w.Type, &w.Language, &w.NotebookID, &w.CellID,
			&w.ConnectorID, &w.Query, &layoutJSON, &configJSON, &w.CreatedAt, &w.UpdatedAt))
		require.NoError(t, json.Unmarshal(layoutJSON, &w.Layout))
		require.NoError(t, json.Unmarshal(layoutJSON, &w.LayoutRaw))
		require.NoError(t, json.Unmarshal(configJSON, &w.Config))
		out[w.ID] = w
	}
	require.NoError(t, rows.Err())
	return out
}

func dashboardTitle(t *testing.T, db *database.DB, dashID string) string {
	t.Helper()
	var title string
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT title FROM dashboards WHERE id = $1`, dashID).Scan(&title))
	return title
}

func dashboardSettings(t *testing.T, db *database.DB, dashID string) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, db.Pool.QueryRow(context.Background(),
		`SELECT settings FROM dashboards WHERE id = $1`, dashID).Scan(&raw))
	var settings map[string]any
	require.NoError(t, json.Unmarshal(raw, &settings))
	return settings
}

// dashboardDocState returns the stored document state, or nil when none is
// stored.
func dashboardDocState(t *testing.T, db *database.DB, dashID string) []byte {
	t.Helper()
	var state []byte
	err := db.Pool.QueryRow(context.Background(),
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	require.NoError(t, err)
	return state
}

// insertStoreTestWidgetRow inserts a materialized widget row directly, for
// tests that need existing rows without going through a store.
func insertStoreTestWidgetRow(t *testing.T, db *database.DB, dashID, widgetID string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO widgets (id, dashboard_id, type, layout)
		 VALUES ($1, $2, 'text', '{"row":0,"col":0,"width":3,"height":2}')`,
		widgetID, dashID)
	require.NoError(t, err)
}

func TestMergeAndStore_MergesStaleUpdateWithoutClobberingBackendWrite(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))

	// Backend-originated write: rename through the document.
	title := "Backend Title"
	backendState, err := UpdateMeta(base, &title, nil, nil)
	require.NoError(t, err)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, backendState))

	// A relay that loaded the document before the backend write stores its
	// stale full state, carrying a browser layout edit. The merge must keep
	// both: the backend title (causally after the base title) and the relay's
	// layout change (causally after the base layout).
	relayState, err := UpdateLayout(base, testUUID(1), Layout{Row: 9, Col: 0, Width: 12, Height: 2})
	require.NoError(t, err)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, relayState))

	require.Equal(t, "Backend Title", dashboardTitle(t, db, f.dashID))
	widgets := loadStoreTestWidgets(t, db, f.dashID)
	require.Equal(t, Layout{Row: 9, Col: 0, Width: 12, Height: 2}, widgets[testUUID(1)].Layout)

	stored := dashboardDocState(t, db, f.dashID)
	require.NotEmpty(t, stored)
	proj := mustProject(t, stored)
	require.Equal(t, "Backend Title", proj.Title)
	require.Equal(t, Layout{Row: 9, Col: 0, Width: 12, Height: 2}, proj.Widgets[testUUID(1)].Layout)
}

func TestMergeAndStore_MaterializesWidgetDiff(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	initial := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, initial, 2)

	// Backdate widget 1 so created_at preservation is observable.
	_, err := db.Pool.Exec(ctx,
		`UPDATE widgets SET created_at = TIMESTAMPTZ '2020-01-01 00:00:00+00',
		        updated_at = TIMESTAMPTZ '2020-01-01 00:00:00+00'
		 WHERE id = $1`, testUUID(1))
	require.NoError(t, err)

	// Diff: update widget 1, delete widget 2, add widget 3.
	query := "SELECT count(*) FROM sales"
	updated := WidgetDoc{
		ID:          testUUID(1),
		Type:        "metric",
		Layout:      Layout{Row: 2, Col: 0, Width: 3, Height: 2},
		ConnectorID: &f.connectorID,
		Query:       &query,
		Language:    "sql",
		Config:      map[string]any{"field": "count"},
	}
	state, err := UpsertWidget(base, updated)
	require.NoError(t, err)
	state, err = DeleteWidget(state, testUUID(2))
	require.NoError(t, err)
	state, err = UpsertWidget(state, WidgetDoc{
		ID:     testUUID(3),
		Type:   "text",
		Layout: Layout{Row: 4, Col: 0, Width: 12, Height: 2},
		Config: map[string]any{"markdown": "# Added"},
	})
	require.NoError(t, err)

	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, state))

	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, 2)
	require.NotContains(t, after, testUUID(2), "deleted widget row must be removed")

	a := after[testUUID(1)]
	require.Equal(t, "metric", a.Type)
	require.Equal(t, Layout{Row: 2, Col: 0, Width: 3, Height: 2}, a.Layout)
	require.Equal(t, map[string]any{
		"row": float64(2), "col": float64(0), "width": float64(3), "height": float64(2),
	}, a.LayoutRaw, "layout JSONB must match the REST-written shape")
	require.Equal(t, map[string]any{"field": "count"}, a.Config)
	require.Equal(t, f.connectorID, *a.ConnectorID)
	require.Equal(t, "SELECT count(*) FROM sales", *a.Query)
	require.Equal(t, "sql", a.Language)
	require.Equal(t, "2020-01-01T00:00:00Z", a.CreatedAt.UTC().Format(time.RFC3339),
		"an upsert must preserve the existing created_at")
	require.True(t, a.UpdatedAt.After(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)),
		"an upsert must refresh updated_at")

	c := after[testUUID(3)]
	require.Equal(t, "text", c.Type)
	require.Equal(t, Layout{Row: 4, Col: 0, Width: 12, Height: 2}, c.Layout)
	require.Equal(t, map[string]any{"markdown": "# Added"}, c.Config)
	require.Nil(t, c.ConnectorID)
	require.True(t, c.CreatedAt.After(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)),
		"a new row must get a fresh created_at")
}

func TestMergeAndStore_TrashedOrMissingDashboardIsNoOp(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))

	// Missing dashboard: no state, no rows, no error.
	missing := uuid.NewString()
	require.NoError(t, MergeAndStore(ctx, db.Pool, missing, base))
	require.Nil(t, dashboardDocState(t, db, missing))

	// Trashed dashboard: the same store that works on a live dashboard must
	// not write state or touch rows.
	storedBefore := dashboardDocState(t, db, f.dashID)
	widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)
	titleBefore := dashboardTitle(t, db, f.dashID)
	_, err := db.Pool.Exec(ctx, `UPDATE dashboards SET deleted_at = NOW() WHERE id = $1`, f.dashID)
	require.NoError(t, err)

	title := "After Trash"
	trashedState, err := UpdateMeta(base, &title, nil, nil)
	require.NoError(t, err)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, trashedState))

	require.Equal(t, titleBefore, dashboardTitle(t, db, f.dashID))
	require.Equal(t, storedBefore, dashboardDocState(t, db, f.dashID))
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
}

func TestMergeAndStore_InvalidDocErrorsAndMaterializesNothing(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))

	storedBefore := dashboardDocState(t, db, f.dashID)
	widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)

	err := MergeAndStore(ctx, db.Pool, f.dashID, []byte("this is not a yjs update"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "apply incoming state")

	require.Equal(t, "Base Title", dashboardTitle(t, db, f.dashID))
	require.Equal(t, storedBefore, dashboardDocState(t, db, f.dashID))
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
}

func TestMergeAndStore_RefusesNoWidgetsWithWarnings(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	// Existing materialized rows with no stored state: the store must not
	// interpret "every widget failed validation" as "delete every row".
	insertStoreTestWidgetRow(t, db, f.dashID, testUUID(1))
	insertStoreTestWidgetRow(t, db, f.dashID, testUUID(2))
	widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)

	corrupt := buildState(t, func(txn *crdt.Transaction, _, widgets *crdt.YMap) {
		putRawWidget(txn, widgets, "not-a-uuid", nil)
	})
	err := MergeAndStore(ctx, db.Pool, f.dashID, corrupt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to materialize")
	require.Contains(t, err.Error(), "no widgets and 1 widget warning")

	require.Equal(t, "Fixture Dashboard", dashboardTitle(t, db, f.dashID))
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
	require.Nil(t, dashboardDocState(t, db, f.dashID), "the refused store must roll back the state write")
}

func TestMergeAndStore_RefusesEntirelyEmptyProjection(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	insertStoreTestWidgetRow(t, db, f.dashID, testUUID(1))
	widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)

	// A valid but entirely empty document (the shape a wrong-root-kind
	// corruption projects to: no title, settings, variables, widgets or
	// warnings) must not wipe existing rows.
	empty, err := Seed(Projection{})
	require.NoError(t, err)

	err = MergeAndStore(ctx, db.Pool, f.dashID, empty)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to materialize")
	require.Contains(t, err.Error(), "projection is empty but the dashboard still has")
	require.Contains(t, err.Error(), "1 widget row(s)")

	require.Equal(t, "Fixture Dashboard", dashboardTitle(t, db, f.dashID))
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
	require.Nil(t, dashboardDocState(t, db, f.dashID))
}

// TestMergeAndStore_RefusesWarningTaintedEmptyProjection pins the guard-2
// extension: a doc that projects zero widgets but carries settings/variables
// warnings must not have its empty widget set interpreted as "delete every
// row" while the dashboard still has content. This was the hole where a
// settings-only warning (grid_cols = NaN) exempted the projection from the
// total-wipe guard.
func TestMergeAndStore_RefusesWarningTaintedEmptyProjection(t *testing.T) {
	db := setupStoreTestDB(t)
	ctx := context.Background()

	// No widget entries; settings.grid_cols = NaN is dropped by Project with
	// a settings-only warning.
	corrupt := buildState(t, func(txn *crdt.Transaction, meta, _ *crdt.YMap) {
		settings := crdt.NewMapPrelim()
		settings.Set(txn, "grid_cols", math.NaN())
		meta.Set(txn, keySettings, settings)
	})

	t.Run("populated dashboard refuses", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		insertStoreTestWidgetRow(t, db, f.dashID, testUUID(1))
		widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)
		settingsBefore := dashboardSettings(t, db, f.dashID)
		titleBefore := dashboardTitle(t, db, f.dashID)

		err := MergeAndStore(ctx, db.Pool, f.dashID, corrupt)
		require.Error(t, err)
		require.Contains(t, err.Error(), "refusing to materialize")
		require.Contains(t, err.Error(), "no widgets and 1 warning(s) while the dashboard still has content")

		require.Equal(t, titleBefore, dashboardTitle(t, db, f.dashID))
		require.Equal(t, settingsBefore, dashboardSettings(t, db, f.dashID))
		require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
		require.Nil(t, dashboardDocState(t, db, f.dashID), "the refused store must roll back the state write")
	})

	t.Run("pristine dashboard still materializes", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		_, err := db.Pool.Exec(ctx,
			`UPDATE dashboards SET title = '', settings = '{}' WHERE id = $1`, f.dashID)
		require.NoError(t, err)

		// Nothing to wipe: the warning-tainted projection is allowed.
		require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, corrupt))
		require.Empty(t, loadStoreTestWidgets(t, db, f.dashID))
		require.Equal(t, "", dashboardTitle(t, db, f.dashID))
		require.Equal(t, map[string]any{"variables": []any{}}, dashboardSettings(t, db, f.dashID))
	})
}

func TestMergeAndStore_AllowsDeletingEveryWidget(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	require.Len(t, loadStoreTestWidgets(t, db, f.dashID), 2)

	// The legitimate delete-all path: delete each widget through the doc, then
	// store. The title/settings survive, so the total-wipe guard must not fire
	// and the rows must actually be removed.
	state, err := DeleteWidget(base, testUUID(1))
	require.NoError(t, err)
	state, err = DeleteWidget(state, testUUID(2))
	require.NoError(t, err)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, state))

	require.Empty(t, loadStoreTestWidgets(t, db, f.dashID))
	require.Equal(t, "Base Title", dashboardTitle(t, db, f.dashID))

	proj := mustProject(t, dashboardDocState(t, db, f.dashID))
	require.Empty(t, proj.Widgets)
	require.Equal(t, "Base Title", proj.Title)
}

func TestMergeAndStore_MaterializesSettingsAndVariables(t *testing.T) {
	db := setupStoreTestDB(t)
	ctx := context.Background()

	t.Run("settings and ordered variables", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, storeTestBaseState(t, f.connectorID)))

		// The doc's Y.Array variables are injected under settings.variables,
		// preserving order, alongside the plain settings keys.
		require.Equal(t, map[string]any{
			"grid_cols":           float64(12),
			"query_cache_seconds": float64(30),
			"variables": []any{
				map[string]any{"name": "region", "label": "Region", "type": "single_select", "default": "us"},
				map[string]any{"name": "threshold", "label": "Threshold", "type": "number", "default": float64(0.5)},
			},
		}, dashboardSettings(t, db, f.dashID))
	})

	t.Run("no variables materializes as an empty array", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		state, err := Seed(Projection{Title: "No Variables", Settings: map[string]any{"grid_cols": 6}})
		require.NoError(t, err)
		require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, state))

		require.Equal(t, map[string]any{
			"grid_cols": float64(6),
			"variables": []any{},
		}, dashboardSettings(t, db, f.dashID))
	})
}

// TestMergeAndStore_OneBadWidgetDoesNotAbortStore pins per-widget isolation:
// a widget whose shape fails validation is skipped (and its row diffed away)
// while the rest of the store, including unrelated edits, still commits.
func TestMergeAndStore_OneBadWidgetDoesNotAbortStore(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	require.Len(t, loadStoreTestWidgets(t, db, f.dashID), 2)

	// The editor-cleared drawer shape: widget 1 keeps its connector but its
	// SQL text is now empty, which violates widgets_source_check. Project
	// skips it with a widget warning; the store must still succeed.
	badState, err := SetQuery(base, testUUID(1), "")
	require.NoError(t, err)
	title := "Renamed With Bad Widget"
	badState, err = UpdateMeta(badState, &title, nil, nil)
	require.NoError(t, err)

	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, badState))

	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, 1, "the bad widget's row is diffed away")
	require.Contains(t, after, testUUID(2), "the valid sibling still materializes")
	require.Equal(t, "Renamed With Bad Widget", dashboardTitle(t, db, f.dashID),
		"unrelated edits in the same store persist")
}

// insertNotebookCell inserts one notebook and one cell into orgID (owned by
// userID) and returns their IDs. The notebook/cell rows are cleaned up by the
// org cascade when the org is deleted.
func insertNotebookCell(t *testing.T, db *database.DB, orgID, userID string) (notebookID, cellID string) {
	t.Helper()
	notebookID = uuid.NewString()
	cellID = uuid.NewString()
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, 'Fixture Notebook', $3)`,
		notebookID, orgID, userID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(context.Background(),
		`INSERT INTO cells (id, notebook_id, position, type, language) VALUES ($1, $2, 0, 'code', 'sql')`,
		cellID, notebookID)
	require.NoError(t, err)
	return notebookID, cellID
}

// TestMaterialize_ScopesReferencesToDashboardOrg pins the C1 fix: a widget
// referencing another org's connector/notebook/cell, or a same-org cell whose
// notebook does not match the widget's notebook_id, is treated as dangling —
// skipped with a warning and never materialized — so the derived rows can
// never surface another org's data (or another notebook's cell) through the
// dashboard read paths.
func TestMaterialize_ScopesReferencesToDashboardOrg(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	// A second org with its own connector, notebook and cell.
	otherOrgID := uuid.NewString()
	otherConnectorID := uuid.NewString()
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, 'Dashboarddoc Other Org', $2)`,
		otherOrgID, "ddoc-other-"+otherOrgID[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, otherOrgID)
	})
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO connectors (id, org_id, name, type, config_encrypted) VALUES ($1, $2, 'Other Connector', 'postgres', '\x00')`,
		otherConnectorID, otherOrgID)
	require.NoError(t, err)
	otherNotebookID, otherCellID := insertNotebookCell(t, db, otherOrgID, f.userID)

	// Two same-org notebooks: the cell of one must not materialize through a
	// widget that names the other.
	notebookA, cellA := insertNotebookCell(t, db, f.orgID, f.userID)
	_, cellB := insertNotebookCell(t, db, f.orgID, f.userID)

	query := "SELECT 1"
	proj := &Projection{
		Title: "Scoped",
		Widgets: map[string]WidgetDoc{
			testUUID(1): {ID: testUUID(1), Type: "table", ConnectorID: &otherConnectorID, Query: &query, Language: "sql"},
			testUUID(2): {ID: testUUID(2), Type: "table", NotebookID: &otherNotebookID, CellID: &otherCellID, Language: "sql"},
			testUUID(3): {ID: testUUID(3), Type: "table", NotebookID: &notebookA, CellID: &cellB, Language: "sql"},
			testUUID(4): {ID: testUUID(4), Type: "table", ConnectorID: &f.connectorID, Query: &query, Language: "sql"},
			testUUID(5): {ID: testUUID(5), Type: "table", NotebookID: &notebookA, CellID: &cellA, Language: "sql"},
		},
	}

	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	warnings, err := Materialize(ctx, tx, f.dashID, proj)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.Len(t, warnings, 3, "cross-org and mismatched-pair widgets must warn")
	require.Contains(t, warnings[0], testUUID(1))
	require.Contains(t, warnings[0], "connector_id "+otherConnectorID+" does not exist")
	require.Contains(t, warnings[1], testUUID(2))
	require.Contains(t, warnings[1], "notebook_id "+otherNotebookID+" does not exist")
	require.Contains(t, warnings[1], "cell_id "+otherCellID+" does not exist")
	require.Contains(t, warnings[2], testUUID(3))
	require.Contains(t, warnings[2], "cell_id "+cellB+" does not belong to notebook_id "+notebookA)

	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, 2)
	require.Contains(t, after, testUUID(4), "the same-org connector widget survives")
	require.Contains(t, after, testUUID(5), "the same-org notebook/cell pair survives")
}

// TestMaterialize_SoftDeletedReferencesAreDangling pins that soft-deleted
// connectors and notebooks (deleted_at IS NOT NULL) are skipped like missing
// references: a trashed row must never be resurrected through a widget's
// derived row.
func TestMaterialize_SoftDeletedReferencesAreDangling(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	notebookID, cellID := insertNotebookCell(t, db, f.orgID, f.userID)
	_, err := db.Pool.Exec(ctx, `UPDATE connectors SET deleted_at = NOW() WHERE id = $1`, f.connectorID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE notebooks SET deleted_at = NOW() WHERE id = $1`, notebookID)
	require.NoError(t, err)

	query := "SELECT 1"
	proj := &Projection{
		Title: "Soft Deleted",
		Widgets: map[string]WidgetDoc{
			testUUID(1): {ID: testUUID(1), Type: "table", ConnectorID: &f.connectorID, Query: &query, Language: "sql"},
			testUUID(2): {ID: testUUID(2), Type: "table", NotebookID: &notebookID, CellID: &cellID, Language: "sql"},
		},
	}

	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	warnings, err := Materialize(ctx, tx, f.dashID, proj)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.Len(t, warnings, 2)
	require.Contains(t, warnings[0], "connector_id "+f.connectorID+" does not exist")
	require.Contains(t, warnings[1], "notebook_id "+notebookID+" does not exist")
	require.Empty(t, loadStoreTestWidgets(t, db, f.dashID))
}

// TestMergeAndStore_ScopesCrossOrgReferencesEndToEnd pins the same C1 fix
// through the full store path: a document carrying a widget for another org's
// notebook/cell stores and materializes its valid widgets, while the cross-org
// widget is skipped and never gains a derived row.
func TestMergeAndStore_ScopesCrossOrgReferencesEndToEnd(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	otherOrgID := uuid.NewString()
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, 'Dashboarddoc Cross Org', $2)`,
		otherOrgID, "ddoc-cross-"+otherOrgID[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, otherOrgID)
	})
	otherNotebookID, otherCellID := insertNotebookCell(t, db, otherOrgID, f.userID)

	base := storeTestBaseState(t, f.connectorID)
	state, err := UpsertWidget(base, WidgetDoc{
		ID:         testUUID(3),
		Type:       "table",
		Layout:     Layout{Row: 4, Col: 0, Width: 6, Height: 4},
		NotebookID: &otherNotebookID,
		CellID:     &otherCellID,
		Language:   "sql",
	})
	require.NoError(t, err)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, state))

	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, 2, "only the same-org widgets materialize")
	require.NotContains(t, after, testUUID(3), "the cross-org widget must never gain a row")
	require.Contains(t, after, testUUID(1))
	require.Contains(t, after, testUUID(2))
}

// TestMaterialize_SkipsDanglingWidgetReferences pins that missing (or
// malformed) connector/notebook/cell references skip only their widget, with
// a materialization warning, instead of failing the FK insert.
func TestMaterialize_SkipsDanglingWidgetReferences(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	missing := uuid.NewString()
	badRef := "not-a-uuid"
	query := "SELECT 1"
	proj := &Projection{
		Title: "Dangling",
		Widgets: map[string]WidgetDoc{
			testUUID(1): {ID: testUUID(1), Type: "table", ConnectorID: &f.connectorID, Query: &query, Language: "sql"},
			testUUID(2): {ID: testUUID(2), Type: "table", ConnectorID: &missing, Query: &query, Language: "sql"},
			testUUID(3): {ID: testUUID(3), Type: "table", NotebookID: &missing, CellID: &missing, Language: "sql"},
			testUUID(4): {ID: testUUID(4), Type: "table", ConnectorID: &badRef, Query: &query, Language: "sql"},
		},
	}

	tx, err := db.Pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	warnings, err := Materialize(ctx, tx, f.dashID, proj)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	require.Len(t, warnings, 3)
	require.Contains(t, warnings[0], testUUID(2))
	require.Contains(t, warnings[0], "connector_id "+missing+" does not exist")
	require.Contains(t, warnings[1], testUUID(3))
	require.Contains(t, warnings[1], "notebook_id "+missing+" does not exist")
	require.Contains(t, warnings[1], "cell_id "+missing+" does not exist")
	require.Contains(t, warnings[2], testUUID(4))
	require.Contains(t, warnings[2], "is not a valid UUID")

	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, 1)
	require.Contains(t, after, testUUID(1))
}

// TestMergeAndStore_SkipsWidgetsWithHardDeletedReferences covers the stale
// reference case end to end: a connector that existed when the widget was
// materialized is hard-deleted, and the next store drops the widget's row
// instead of failing on the FK.
func TestMergeAndStore_SkipsWidgetsWithHardDeletedReferences(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	require.Len(t, loadStoreTestWidgets(t, db, f.dashID), 2)

	_, err := db.Pool.Exec(ctx, `DELETE FROM connectors WHERE id = $1`, f.connectorID)
	require.NoError(t, err)

	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, 1)
	require.Contains(t, after, testUUID(2), "the widget with a live reference survives")
}

// TestMergeAndStore_RefusesEmptyProjectionWhenDashboardHasContent extends
// guard 2 beyond widget rows: an entirely empty projection must not wipe a
// dashboard title or settings, while a pristine empty dashboard still
// materializes.
func TestMergeAndStore_RefusesEmptyProjectionWhenDashboardHasContent(t *testing.T) {
	db := setupStoreTestDB(t)
	ctx := context.Background()
	empty, err := Seed(Projection{})
	require.NoError(t, err)

	t.Run("non-empty title", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		err := MergeAndStore(ctx, db.Pool, f.dashID, empty)
		require.Error(t, err)
		require.Contains(t, err.Error(), "refusing to materialize")
		require.Contains(t, err.Error(), "still has a title")
		require.Equal(t, "Fixture Dashboard", dashboardTitle(t, db, f.dashID))
		require.Nil(t, dashboardDocState(t, db, f.dashID))
	})

	t.Run("non-empty settings", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		_, err := db.Pool.Exec(ctx,
			`UPDATE dashboards SET title = '', settings = '{"grid_cols": 6}' WHERE id = $1`, f.dashID)
		require.NoError(t, err)

		err = MergeAndStore(ctx, db.Pool, f.dashID, empty)
		require.Error(t, err)
		require.Contains(t, err.Error(), "still has settings")
		require.Equal(t, "", dashboardTitle(t, db, f.dashID))
		require.Equal(t, map[string]any{"grid_cols": float64(6)}, dashboardSettings(t, db, f.dashID))
	})

	t.Run("settings with only empty variables materializes", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		_, err := db.Pool.Exec(ctx,
			`UPDATE dashboards SET title = '', settings = '{"variables": []}' WHERE id = $1`, f.dashID)
		require.NoError(t, err)

		require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, empty))
		require.Equal(t, map[string]any{"variables": []any{}}, dashboardSettings(t, db, f.dashID))
	})

	t.Run("pristine dashboard", func(t *testing.T) {
		f := seedStoreTestFixture(t, db)
		_, err := db.Pool.Exec(ctx,
			`UPDATE dashboards SET title = '', settings = '{}' WHERE id = $1`, f.dashID)
		require.NoError(t, err)

		require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, empty))
		require.Equal(t, "", dashboardTitle(t, db, f.dashID))
		require.Equal(t, map[string]any{"variables": []any{}}, dashboardSettings(t, db, f.dashID))
		require.Empty(t, loadStoreTestWidgets(t, db, f.dashID))
	})
}

// TestMergeAndStore_ConcurrentStoresBothSurvive pins the FOR UPDATE
// serialization: two overlapping stores built from the same base must not
// lose either update to a last-write-wins state overwrite.
func TestMergeAndStore_ConcurrentStoresBothSurvive(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))

	layoutState, err := UpdateLayout(base, testUUID(1), Layout{Row: 7, Col: 1, Width: 5, Height: 3})
	require.NoError(t, err)
	queryState, err := SetQuery(base, testUUID(2), "SELECT 42")
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, state := range [][]byte{layoutState, queryState} {
		wg.Add(1)
		go func(s []byte) {
			defer wg.Done()
			errs <- MergeAndStore(ctx, db.Pool, f.dashID, s)
		}(state)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	proj := mustProject(t, dashboardDocState(t, db, f.dashID))
	require.Equal(t, Layout{Row: 7, Col: 1, Width: 5, Height: 3}, proj.Widgets[testUUID(1)].Layout)
	require.Equal(t, "SELECT 42", widgetQuery(t, proj.Widgets[testUUID(2)]))
}

// TestMergeAndStore_IdempotentRestore pins that re-storing the same state is
// a no-op for the stored bytes and derived rows.
func TestMergeAndStore_IdempotentRestore(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	stateFirst := dashboardDocState(t, db, f.dashID)
	widgetsFirst := loadStoreTestWidgets(t, db, f.dashID)
	settingsFirst := dashboardSettings(t, db, f.dashID)

	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))

	require.Equal(t, stateFirst, dashboardDocState(t, db, f.dashID))
	require.Equal(t, settingsFirst, dashboardSettings(t, db, f.dashID))
	require.Equal(t, "Base Title", dashboardTitle(t, db, f.dashID))

	after := loadStoreTestWidgets(t, db, f.dashID)
	require.Len(t, after, len(widgetsFirst))
	for id, before := range widgetsFirst {
		got := after[id]
		require.Equal(t, before.CreatedAt, got.CreatedAt)
		require.Equal(t, before.Type, got.Type)
		require.Equal(t, before.Language, got.Language)
		require.Equal(t, before.Layout, got.Layout)
		require.Equal(t, before.Config, got.Config)
	}
}

// TestMergeAndStoreValidated_ValidatorAbortsWithoutWriting pins the validated
// store contract: the validator sees the stored state read under the row lock
// and the incoming bytes, and a non-nil return aborts the store with nothing
// written or materialized.
func TestMergeAndStoreValidated_ValidatorAbortsWithoutWriting(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))
	storedBefore := dashboardDocState(t, db, f.dashID)
	widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)

	title := "Rejected"
	state, err := UpdateMeta(base, &title, nil, nil)
	require.NoError(t, err)

	var (
		gotStored   []byte
		gotIncoming []byte
		ran         bool
	)
	err = MergeAndStoreValidated(ctx, db.Pool, f.dashID, state,
		func(_ context.Context, _ pgx.Tx, stored, incoming []byte) error {
			ran = true
			gotStored = stored
			gotIncoming = incoming
			return ErrStoreForbidden
		})
	require.ErrorIs(t, err, ErrStoreForbidden)
	require.True(t, ran)
	require.Equal(t, storedBefore, gotStored, "the validator must see the stored state read under the lock")
	require.Equal(t, state, gotIncoming)

	require.Equal(t, storedBefore, dashboardDocState(t, db, f.dashID),
		"a rejected store must not change the stored state")
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
	require.Equal(t, "Base Title", dashboardTitle(t, db, f.dashID))
}

// TestMergeAndStore_CorruptStoredStateErrors pins the fail-closed decision:
// an undecodable stored state errors and commits nothing rather than being
// silently replaced (recovery is an explicit operational action).
func TestMergeAndStore_CorruptStoredStateErrors(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	insertStoreTestWidgetRow(t, db, f.dashID, testUUID(1))
	widgetsBefore := loadStoreTestWidgets(t, db, f.dashID)

	corrupt := []byte("this is not a yjs update")
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO dashboard_yjs_documents (dashboard_id, state) VALUES ($1, $2)`,
		f.dashID, corrupt)
	require.NoError(t, err)

	state, err := Seed(Projection{
		Title: "Incoming",
		Widgets: map[string]WidgetDoc{
			testUUID(2): {ID: testUUID(2), Type: "text"},
		},
	})
	require.NoError(t, err)

	err = MergeAndStore(ctx, db.Pool, f.dashID, state)
	require.Error(t, err)
	require.Contains(t, err.Error(), f.dashID, "errors must be correlated with the dashboard")
	require.Contains(t, err.Error(), "decode stored state")

	require.Equal(t, corrupt, dashboardDocState(t, db, f.dashID))
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
	require.Equal(t, "Fixture Dashboard", dashboardTitle(t, db, f.dashID))
}

// captureHandler is a minimal slog.Handler that records entries, so tests can
// assert on warning logging.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// TestMergeAndStore_LogsWarningsOncePerStore pins the non-noisy warning log:
// one line per store, carrying the dashboard ID and the warning count.
func TestMergeAndStore_LogsWarningsOncePerStore(t *testing.T) {
	db := setupStoreTestDB(t)
	f := seedStoreTestFixture(t, db)
	ctx := context.Background()

	base := storeTestBaseState(t, f.connectorID)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, base))

	h := &captureHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(previous) })

	badState, err := SetQuery(base, testUUID(1), "")
	require.NoError(t, err)
	require.NoError(t, MergeAndStore(ctx, db.Pool, f.dashID, badState))

	require.Len(t, h.records, 1, "exactly one log line per store with warnings")
	attrs := map[string]any{}
	h.records[0].Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	require.Equal(t, f.dashID, attrs["dashboard_id"])
	require.Equal(t, int64(1), attrs["count"])
	warnings, ok := attrs["warnings"].([]string)
	require.True(t, ok)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "connector widget has no query")
}
