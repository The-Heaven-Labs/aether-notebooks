package api_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
)

// internalDashboardDocRequest performs an internal dashboard-yjs request with a
// raw binary body (nil for no body) and an optional bearer token.
func internalDashboardDocRequest(t *testing.T, srv *api.Server, token, method, dashID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, "/internal/dashboard-yjs/"+dashID, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestInternalDashboardYjsRequiresInternalToken(t *testing.T) {
	srv := setupTestServer(t)
	dashID := uuid.NewString()

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"missing", ""},
		{"invalid", "invalid.token.here"},
	} {
		t.Run("GET/"+tc.name, func(t *testing.T) {
			rec := internalDashboardDocRequest(t, srv, tc.token, "GET", dashID, nil)
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
		t.Run("PUT/"+tc.name, func(t *testing.T) {
			rec := internalDashboardDocRequest(t, srv, tc.token, "PUT", dashID, []byte{1, 2, 3})
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		})
	}
}

// TestInternalDashboardYjsSeedFromRows pins the lazy seed: a GET on a dashboard
// with no stored state builds the document from the current dashboards/widgets
// rows, persists it exactly once, and returns decodable bytes.
func TestInternalDashboardYjsSeedFromRows(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("dash-yjs-seed-%d@example.com", ts), "Dash Yjs Seed Org")
	connID := createConnector(t, srv, token)

	dashID := createDashWithSettings(t, srv, token, map[string]any{
		"grid_cols": 12,
		"variables": []map[string]any{
			{"name": "region", "label": "Region", "type": "text", "default": "us"},
		},
	})
	queryWidgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1")
	textWidgetID := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"type":   "text",
		"layout": map[string]int{"row": 6, "col": 0, "width": 6, "height": 4},
		"config": map[string]any{"markdown": "# Notes"},
	})["id"].(string)

	// First GET has no stored state: it seeds from the dashboards/widgets rows.
	rec := internalDashboardDocRequest(t, srv, token, "GET", dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	seeded := rec.Body.Bytes()
	require.NotEmpty(t, seeded)

	// The seed round-trips through Project back to the DB row shape.
	proj, err := dashboarddoc.Project(seeded)
	require.NoError(t, err)
	require.Empty(t, proj.Warnings)
	require.Equal(t, "Query Dash", proj.Title)
	require.Equal(t, map[string]any{"grid_cols": 12}, proj.Settings,
		"variables are split out of settings; the rest is carried as-is")
	require.Equal(t, []map[string]any{
		{"name": "region", "label": "Region", "type": "text", "default": "us"},
	}, proj.Variables)
	require.Len(t, proj.Widgets, 2)

	qw := proj.Widgets[queryWidgetID]
	require.Equal(t, "table", qw.Type)
	require.Equal(t, dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 6}, qw.Layout)
	require.NotNil(t, qw.ConnectorID)
	require.Equal(t, connID, *qw.ConnectorID)
	require.NotNil(t, qw.Query)
	require.Equal(t, "SELECT 1", *qw.Query)
	require.Equal(t, "sql", qw.Language)
	require.Nil(t, qw.NotebookID)
	require.Nil(t, qw.CellID)

	tw := proj.Widgets[textWidgetID]
	require.Equal(t, "text", tw.Type)
	require.Nil(t, tw.ConnectorID)
	require.Nil(t, tw.Query)
	require.Equal(t, dashboarddoc.Layout{Row: 6, Col: 0, Width: 6, Height: 4}, tw.Layout)
	require.Equal(t, map[string]any{"markdown": "# Notes"}, tw.Config)

	// The seed is persisted exactly once: a second GET returns identical
	// bytes. (Seed encodes a fresh random client ID per call, so a re-seed
	// would produce different bytes.)
	var stored []byte
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&stored))
	require.Equal(t, seeded, stored)

	rec = internalDashboardDocRequest(t, srv, token, "GET", dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, seeded, rec.Body.Bytes())
}

// TestInternalDashboardYjsPutStoresAndMaterializes pins the store path: the
// relay's full state is merged and materialized, and a subsequent GET returns
// the stored document.
func TestInternalDashboardYjsPutStoresAndMaterializes(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("dash-yjs-put-%d@example.com", ts), "Dash Yjs Put Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	widgetID := uuid.NewString()
	query := "SELECT 42"
	state, err := dashboarddoc.Seed(dashboarddoc.Projection{
		Title: "PUT Title",
		Widgets: map[string]dashboarddoc.WidgetDoc{
			widgetID: {
				ID:          widgetID,
				Type:        "table",
				Layout:      dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 4},
				ConnectorID: &connID,
				Query:       &query,
				Language:    "sql",
			},
		},
	})
	require.NoError(t, err)

	rec := internalDashboardDocRequest(t, srv, token, "PUT", dashID, state)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	// The stored state round-trips through GET.
	rec = internalDashboardDocRequest(t, srv, token, "GET", dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	proj, err := dashboarddoc.Project(rec.Body.Bytes())
	require.NoError(t, err)
	require.Equal(t, "PUT Title", proj.Title)
	require.Len(t, proj.Widgets, 1)
	require.NotNil(t, proj.Widgets[widgetID].Query)
	require.Equal(t, "SELECT 42", *proj.Widgets[widgetID].Query)

	// MergeAndStore materialized the derived rows in the same transaction.
	ctx := context.Background()
	var title string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT title FROM dashboards WHERE id = $1`, dashID).Scan(&title))
	require.Equal(t, "PUT Title", title)
	var widgetCount int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&widgetCount))
	require.Equal(t, 1, widgetCount)
}

// TestInternalDashboardYjsPutValidatesStoreActor pins the C1 store-actor
// validation: the internal PUT (the relay's document write path) must enforce
// dashboard edit for any content change and notebook view for every
// added/changed widget cell reference, while idempotent stores stay allowed
// for read-only tokens. Cross-org dashboard IDs are indistinguishable from
// missing ones.
func TestInternalDashboardYjsPutValidatesStoreActor(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ctx := context.Background()
	ts := time.Now().UnixNano()

	ownerToken := registerAndGetToken(t, srv, fmt.Sprintf("dash-yjs-actor-owner-%d@example.com", ts), "Dash Yjs Actor Org")
	dashID := createDashWithSettings(t, srv, ownerToken, nil)
	var orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	connID := createConnector(t, srv, ownerToken)
	nbID := createNotebook(t, srv, ownerToken, "Actor Notebook")
	cellID := createCell(t, srv, ownerToken, nbID, "sql", "SELECT 1", connID)

	// An editor with dashboard edit but no view on the notebook.
	editorNoViewID := insertUser(t, srv, fmt.Sprintf("dash-yjs-actor-editor-%d@example.com", ts), "Editor No View")
	addOrgMember(t, srv, orgID, editorNoViewID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", editorNoViewID, "edit")
	editorNoViewToken := issueToken(t, editorNoViewID, orgID, "non-admin")

	// An editor with both dashboard edit and notebook view.
	editorViewID := insertUser(t, srv, fmt.Sprintf("dash-yjs-actor-editor-view-%d@example.com", ts), "Editor With View")
	addOrgMember(t, srv, orgID, editorViewID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", editorViewID, "edit")
	grantACL(t, srv, orgID, "notebook", nbID, "user", editorViewID, "view")
	editorViewToken := issueToken(t, editorViewID, orgID, "non-admin")

	// A view-only member.
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-yjs-actor-viewer-%d@example.com", ts), "Viewer")
	addOrgMember(t, srv, orgID, viewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "non-admin")

	// The owner stores the base document (title, no widgets).
	base, err := dashboarddoc.Seed(dashboarddoc.Projection{Title: "Query Dash"})
	require.NoError(t, err)
	rec := internalDashboardDocRequest(t, srv, ownerToken, "PUT", dashID, base)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	storedBase := dashboardDocState(t, srv, dashID)

	widgetID := uuid.NewString()
	withCellWidget := func() []byte {
		state, err := dashboarddoc.Seed(dashboarddoc.Projection{
			Title: "Query Dash",
			Widgets: map[string]dashboarddoc.WidgetDoc{
				widgetID: {
					ID:         widgetID,
					Type:       "table",
					Layout:     dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 4},
					NotebookID: &nbID,
					CellID:     &cellID,
					Language:   "sql",
				},
			},
		})
		require.NoError(t, err)
		return state
	}

	t.Run("editor without notebook view is forbidden", func(t *testing.T) {
		rec := internalDashboardDocRequest(t, srv, editorNoViewToken, "PUT", dashID, withCellWidget())
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		require.Equal(t, storedBase, dashboardDocState(t, srv, dashID),
			"a rejected store must not change the stored state")
		var widgetCount int
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&widgetCount))
		require.Zero(t, widgetCount, "a rejected store must not materialize the widget")
	})

	t.Run("editor with notebook view stores and materializes", func(t *testing.T) {
		rec := internalDashboardDocRequest(t, srv, editorViewToken, "PUT", dashID, withCellWidget())
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

		var (
			gotNotebook, gotCell *string
			widgetCount          int
		)
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&widgetCount))
		require.Equal(t, 1, widgetCount)
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT notebook_id, cell_id FROM widgets WHERE id = $1`, widgetID).
			Scan(&gotNotebook, &gotCell))
		require.NotNil(t, gotNotebook)
		require.Equal(t, nbID, *gotNotebook)
		require.NotNil(t, gotCell)
		require.Equal(t, cellID, *gotCell)
	})

	t.Run("viewer changing the title is forbidden", func(t *testing.T) {
		// Derive the attempted change from the stored state so it is causally
		// after it and definitely wins the merge. A fresh standalone document
		// carries a concurrent title write that may lose CRDT last-write-wins
		// and merge into a no-op, which the validator correctly treats as an
		// idempotent store (nothing changes, nothing to authorize).
		stored := dashboardDocState(t, srv, dashID)
		viewerTitle := "Viewer Edit"
		viewerState, err := dashboarddoc.UpdateMeta(stored, &viewerTitle, nil, nil)
		require.NoError(t, err)
		rec := internalDashboardDocRequest(t, srv, viewerToken, "PUT", dashID, viewerState)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		var title string
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT title FROM dashboards WHERE id = $1`, dashID).Scan(&title))
		require.Equal(t, "Query Dash", title, "a rejected store must not change the title")
	})

	t.Run("viewer idempotent store is allowed", func(t *testing.T) {
		stored := dashboardDocState(t, srv, dashID)
		rec := internalDashboardDocRequest(t, srv, viewerToken, "PUT", dashID, stored)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	})

	t.Run("cross-org dashboard id is 404", func(t *testing.T) {
		otherToken := registerAndGetToken(t, srv,
			fmt.Sprintf("dash-yjs-actor-other-%d@example.com", ts), "Dash Yjs Actor Other Org")
		otherDashID := createDashWithSettings(t, srv, otherToken, nil)

		rec := internalDashboardDocRequest(t, srv, ownerToken, "PUT", otherDashID, base)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		var count int
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, otherDashID).Scan(&count))
		require.Zero(t, count, "a cross-org store must not write state")
	})
}

// dashboardDeltaToWidgetRefs decodes the stored dashboard document, rewrites
// one widget's notebook/cell references, and encodes only the new changes
// against the stored state vector — the shape of a relay delta update. The
// delta has no standalone projection (its structs are pending on parents that
// live only in the stored document) but integrates cleanly into the stored
// document, which is exactly the bypass shape the merged-projection validator
// must catch. The root and field keys are the document format pinned by
// dashboarddoc (internal/dashboarddoc/doc.go): widgets/notebook_id/cell_id.
func dashboardDeltaToWidgetRefs(t *testing.T, stored []byte, widgetID, notebookID, cellID string) []byte {
	t.Helper()
	doc := crdt.New()
	require.NoError(t, crdt.ApplyUpdateV1(doc, stored, nil))
	sv, err := crdt.DecodeStateVectorV1(crdt.EncodeStateVectorV1(doc))
	require.NoError(t, err)

	widgets := doc.GetMap("widgets")
	v, ok := widgets.Get(widgetID)
	require.True(t, ok, "stored document must contain widget %s", widgetID)
	wm, isMap := v.(*crdt.YMap)
	require.True(t, isMap, "widget %s must be a Y.Map", widgetID)

	doc.Transact(func(txn *crdt.Transaction) {
		wm.Set(txn, "notebook_id", notebookID)
		wm.Set(txn, "cell_id", cellID)
	})
	return crdt.EncodeStateAsUpdateV1(doc, sv)
}

// TestInternalDashboardYjsPutValidatesMergedProjection pins the C1 delta
// bypass fix: a crafted delta update has no standalone projection, so
// projecting the incoming bytes alone sees no widgets and skips the notebook
// checks, while the merge integrates the delta into the stored document. The
// validator must judge the merged projection, so a delta that repoints an
// existing widget at a notebook the editor cannot view is forbidden and
// materializes nothing, while a delta pointing at a viewable notebook
// succeeds.
func TestInternalDashboardYjsPutValidatesMergedProjection(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ctx := context.Background()
	ts := time.Now().UnixNano()

	ownerToken := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-yjs-merged-owner-%d@example.com", ts), "Dash Yjs Merged Org")
	dashID := createDashWithSettings(t, srv, ownerToken, nil)
	var orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	connID := createConnector(t, srv, ownerToken)
	nbVisible := createNotebook(t, srv, ownerToken, "Visible Notebook")
	cellVisible := createCell(t, srv, ownerToken, nbVisible, "sql", "SELECT 1", connID)
	nbVisible2 := createNotebook(t, srv, ownerToken, "Visible Notebook 2")
	cellVisible2 := createCell(t, srv, ownerToken, nbVisible2, "sql", "SELECT 1", connID)
	nbHidden := createNotebook(t, srv, ownerToken, "Hidden Notebook")
	cellHidden := createCell(t, srv, ownerToken, nbHidden, "sql", "SELECT 1", connID)

	// An editor with dashboard edit and notebook view on the visible
	// notebooks only.
	editorID := insertUser(t, srv, fmt.Sprintf("dash-yjs-merged-editor-%d@example.com", ts), "Merged Editor")
	addOrgMember(t, srv, orgID, editorID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", editorID, "edit")
	grantACL(t, srv, orgID, "notebook", nbVisible, "user", editorID, "view")
	grantACL(t, srv, orgID, "notebook", nbVisible2, "user", editorID, "view")
	editorToken := issueToken(t, editorID, orgID, "non-admin")

	// The owner stores a base document with one cell widget on nbVisible.
	widgetID := uuid.NewString()
	base, err := dashboarddoc.Seed(dashboarddoc.Projection{
		Title: "Query Dash",
		Widgets: map[string]dashboarddoc.WidgetDoc{
			widgetID: {
				ID:         widgetID,
				Type:       "table",
				Layout:     dashboarddoc.Layout{Row: 0, Col: 0, Width: 6, Height: 4},
				NotebookID: &nbVisible,
				CellID:     &cellVisible,
				Language:   "sql",
			},
		},
	})
	require.NoError(t, err)
	rec := internalDashboardDocRequest(t, srv, ownerToken, "PUT", dashID, base)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	storedBase := dashboardDocState(t, srv, dashID)

	hiddenDelta := dashboardDeltaToWidgetRefs(t, storedBase, widgetID, nbHidden, cellHidden)
	// The bypass shape: the delta alone projects no widgets, so an
	// incoming-only projection would skip every notebook check.
	deltaProj, err := dashboarddoc.Project(hiddenDelta)
	require.NoError(t, err)
	require.Empty(t, deltaProj.Widgets, "the crafted delta must have no standalone projection")

	t.Run("delta to an unviewable notebook is forbidden", func(t *testing.T) {
		rec := internalDashboardDocRequest(t, srv, editorToken, "PUT", dashID, hiddenDelta)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		require.Equal(t, storedBase, dashboardDocState(t, srv, dashID),
			"a rejected delta must not change the stored state")

		var gotNotebook, gotCell *string
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT notebook_id, cell_id FROM widgets WHERE id = $1`, widgetID).Scan(&gotNotebook, &gotCell))
		require.NotNil(t, gotNotebook)
		require.Equal(t, nbVisible, *gotNotebook, "the widget must keep its materialized reference")
		require.NotNil(t, gotCell)
		require.Equal(t, cellVisible, *gotCell)
	})

	t.Run("delta to a viewable notebook succeeds", func(t *testing.T) {
		visibleDelta := dashboardDeltaToWidgetRefs(t, storedBase, widgetID, nbVisible2, cellVisible2)
		rec := internalDashboardDocRequest(t, srv, editorToken, "PUT", dashID, visibleDelta)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

		var gotNotebook, gotCell *string
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT notebook_id, cell_id FROM widgets WHERE id = $1`, widgetID).Scan(&gotNotebook, &gotCell))
		require.NotNil(t, gotNotebook)
		require.Equal(t, nbVisible2, *gotNotebook)
		require.NotNil(t, gotCell)
		require.Equal(t, cellVisible2, *gotCell)
	})
}

// TestInternalDashboardYjsGetACLGated pins the read-side gate: the internal
// GET requires the same dashboard access the collab authorize endpoint
// accepts (edit, view, or view_with_data). An org member with no ACL on the
// dashboard gets 403, and the document is not lazily seeded for them.
func TestInternalDashboardYjsGetACLGated(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ctx := context.Background()
	ts := time.Now().UnixNano()

	ownerToken := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-yjs-get-owner-%d@example.com", ts), "Dash Yjs Get Org")
	dashID := createDashWithSettings(t, srv, ownerToken, nil)
	var orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	// An org member with no ACL on the dashboard at all.
	outsiderID := insertUser(t, srv, fmt.Sprintf("dash-yjs-get-outsider-%d@example.com", ts), "Outsider")
	addOrgMember(t, srv, orgID, outsiderID, "non-admin")
	outsiderToken := issueToken(t, outsiderID, orgID, "non-admin")

	// A viewer with `view`, and a data viewer with only `view_with_data`
	// (which the resolver does not treat as implying view).
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-yjs-get-viewer-%d@example.com", ts), "Viewer")
	addOrgMember(t, srv, orgID, viewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "non-admin")

	dataViewerID := insertUser(t, srv, fmt.Sprintf("dash-yjs-get-data-%d@example.com", ts), "Data Viewer")
	addOrgMember(t, srv, orgID, dataViewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", dataViewerID, "view_with_data")
	dataViewerToken := issueToken(t, dataViewerID, orgID, "non-admin")

	t.Run("no ACL is forbidden and does not seed", func(t *testing.T) {
		rec := internalDashboardDocRequest(t, srv, outsiderToken, "GET", dashID, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		var count int
		require.NoError(t, srv.DB().Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&count))
		require.Zero(t, count, "a forbidden GET must not read or seed state")
	})

	t.Run("view access loads and seeds", func(t *testing.T) {
		rec := internalDashboardDocRequest(t, srv, viewerToken, "GET", dashID, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotEmpty(t, rec.Body.Bytes())
	})

	t.Run("view_with_data access loads", func(t *testing.T) {
		rec := internalDashboardDocRequest(t, srv, dataViewerToken, "GET", dashID, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotEmpty(t, rec.Body.Bytes())
	})
}

// TestInternalDashboardYjsGetOrgScoped pins the internal GET org scoping: the
// relay presents the connecting user's session token, so a dashboard in
// another org must be a 404 — indistinguishable from unknown or trashed — and
// must not be read or lazily seeded.
func TestInternalDashboardYjsGetOrgScoped(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()

	ownerToken := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-yjs-org-owner-%d@example.com", ts), "Dash Yjs Org Owner")
	dashID := createDashWithSettings(t, srv, ownerToken, nil)

	otherToken := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-yjs-org-other-%d@example.com", ts), "Dash Yjs Org Other")

	// Another org's token sees a 404 and never seeds state.
	rec := internalDashboardDocRequest(t, srv, otherToken, "GET", dashID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	var count int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&count))
	require.Zero(t, count, "a cross-org GET must not read or seed state")

	// The owning org's token still seeds and reads the document.
	rec = internalDashboardDocRequest(t, srv, ownerToken, "GET", dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(t, rec.Body.Bytes())
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&count))
	require.Equal(t, 1, count)
}

// TestInternalDashboardYjsMissingTrashedAndEmptyBody pins the explicit error
// answers: unknown/trashed dashboards are 404 (never a silent seed or store),
// and an empty PUT body is a 400.
func TestInternalDashboardYjsMissingTrashedAndEmptyBody(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("dash-yjs-404-%d@example.com", ts), "Dash Yjs 404 Org")

	state, err := dashboarddoc.Seed(dashboarddoc.Projection{Title: "Valid"})
	require.NoError(t, err)

	t.Run("unknown dashboard", func(t *testing.T) {
		unknown := uuid.NewString()
		rec := internalDashboardDocRequest(t, srv, token, "GET", unknown, nil)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		rec = internalDashboardDocRequest(t, srv, token, "PUT", unknown, state)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		var count int
		require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, unknown).Scan(&count))
		require.Zero(t, count, "a 404 must not write state")
	})

	t.Run("trashed dashboard", func(t *testing.T) {
		dashID := createDashWithSettings(t, srv, token, nil)
		code, _ := doRequest(t, srv, token, "DELETE", "/api/v1/dashboards/"+dashID, nil)
		require.Equal(t, http.StatusNoContent, code)

		rec := internalDashboardDocRequest(t, srv, token, "GET", dashID, nil)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		rec = internalDashboardDocRequest(t, srv, token, "PUT", dashID, state)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

		var count int
		require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&count))
		require.Zero(t, count, "a trashed dashboard must not be seeded or stored")
	})

	t.Run("empty body", func(t *testing.T) {
		dashID := createDashWithSettings(t, srv, token, nil)
		rec := internalDashboardDocRequest(t, srv, token, "PUT", dashID, []byte{})
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})
}
