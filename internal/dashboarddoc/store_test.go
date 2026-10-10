package dashboarddoc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
// a query widget (connector + SQL) and a text widget.
func storeTestBaseState(t *testing.T, connectorID string) []byte {
	t.Helper()
	query := "SELECT 1"
	state, err := Seed(Projection{
		Title:    "Base Title",
		Settings: map[string]any{"grid_cols": 12, "query_cache_seconds": 30},
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
	require.Contains(t, err.Error(), "no widgets and 1 warning")

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
	require.Contains(t, err.Error(), "projection is empty but 1 widget row")

	require.Equal(t, "Fixture Dashboard", dashboardTitle(t, db, f.dashID))
	require.Equal(t, widgetsBefore, loadStoreTestWidgets(t, db, f.dashID))
	require.Nil(t, dashboardDocState(t, db, f.dashID))
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
