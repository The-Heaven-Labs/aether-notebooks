package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/dashboarddoc"
)

// dashboardDocState reads the stored Yjs document state for a dashboard.
func dashboardDocState(t *testing.T, srv *api.Server, dashID string) []byte {
	t.Helper()
	var state []byte
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT state FROM dashboard_yjs_documents WHERE dashboard_id = $1`, dashID).Scan(&state))
	return state
}

// projectDashboardDoc projects the stored document state so tests can assert
// that a REST write landed in the source of truth, not only in the derived row.
func projectDashboardDoc(t *testing.T, srv *api.Server, dashID string) *dashboarddoc.Projection {
	t.Helper()
	proj, err := dashboarddoc.Project(dashboardDocState(t, srv, dashID))
	require.NoError(t, err)
	return proj
}

// postWidgetRaw sends a widget create and returns the recorder without
// asserting the status.
func postWidgetRaw(t *testing.T, srv *api.Server, token, dashID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// putWidgetRaw sends a partial widget update and returns the recorder without
// asserting the status.
func putWidgetRaw(t *testing.T, srv *api.Server, token, dashID, widgetID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestWidgetUpdateLayoutOnlyPreservesSource pins the read-merge regression: a
// partial PUT that only carries a layout must not clear the widget's query,
// connector, or config in either the derived row or the stored document.
func TestWidgetUpdateLayoutOnlyPreservesSource(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-layout-%d@example.com", time.Now().UnixNano()), "Dash Doc Layout Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 1 AS x",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
		"config":       map[string]any{"showLegend": true},
	})
	widgetID := widget["id"].(string)

	raw, _ := json.Marshal(map[string]any{
		"layout": map[string]int{"row": 2, "col": 0, "width": 6, "height": 4},
	})
	req := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	// Materialized row: the layout is replaced, the source survives.
	var connectorOut, queryOut *string
	var layoutOut string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT connector_id, query, layout::text FROM widgets WHERE id = $1`, widgetID).
		Scan(&connectorOut, &queryOut, &layoutOut))
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT 1 AS x", *queryOut)
	require.JSONEq(t, `{"row":2,"col":0,"width":6,"height":4}`, layoutOut)

	// Stored document: same read-merge, config untouched.
	proj := projectDashboardDoc(t, srv, dashID)
	require.Empty(t, proj.Warnings)
	w := proj.Widgets[widgetID]
	require.Equal(t, dashboarddoc.Layout{Row: 2, Col: 0, Width: 6, Height: 4}, w.Layout)
	require.NotNil(t, w.ConnectorID)
	require.Equal(t, connID, *w.ConnectorID)
	require.NotNil(t, w.Query)
	require.Equal(t, "SELECT 1 AS x", *w.Query)
	require.Equal(t, map[string]any{"showLegend": true}, w.Config)
}

// TestDashboardSettingsPartialUpdatePreservesOtherKeysAndVariables pins the
// shallow settings merge: a PUT carrying only query_cache_seconds keeps
// grid_cols, auto_refresh_seconds, and the variables array in both the
// derived row and the stored document.
func TestDashboardSettingsPartialUpdatePreservesOtherKeysAndVariables(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-settings-%d@example.com", time.Now().UnixNano()), "Dash Doc Settings Org")
	dashID := createDashWithSettings(t, srv, token, map[string]any{
		"grid_cols":            12,
		"auto_refresh_seconds": 60,
		"variables": []map[string]any{
			{"name": "region", "label": "Region", "type": "text", "default": "us"},
		},
	})

	raw, _ := json.Marshal(map[string]any{"settings": map[string]any{"query_cache_seconds": 15}})
	req := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Settings struct {
			GridCols           int  `json:"grid_cols"`
			AutoRefreshSeconds int  `json:"auto_refresh_seconds"`
			QueryCacheSeconds  *int `json:"query_cache_seconds"`
			Variables          []struct {
				Name string `json:"name"`
			} `json:"variables"`
		} `json:"settings"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, 12, resp.Settings.GridCols)
	require.Equal(t, 60, resp.Settings.AutoRefreshSeconds)
	require.NotNil(t, resp.Settings.QueryCacheSeconds)
	require.Equal(t, 15, *resp.Settings.QueryCacheSeconds)
	require.Len(t, resp.Settings.Variables, 1)
	require.Equal(t, "region", resp.Settings.Variables[0].Name)

	// Derived row carries the merged settings.
	var settingsOut string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT settings::text FROM dashboards WHERE id = $1`, dashID).Scan(&settingsOut))
	require.JSONEq(t, `{
		"grid_cols": 12,
		"auto_refresh_seconds": 60,
		"query_cache_seconds": 15,
		"variables": [{"name": "region", "label": "Region", "type": "text", "default": "us"}]
	}`, settingsOut)

	// Document: variables stay the separate ordered array, everything else is
	// the merged settings map.
	proj := projectDashboardDoc(t, srv, dashID)
	require.Empty(t, proj.Warnings)
	require.Equal(t, map[string]any{
		"grid_cols":            12,
		"auto_refresh_seconds": 60,
		"query_cache_seconds":  15,
	}, proj.Settings)
	require.Equal(t, []map[string]any{
		{"name": "region", "label": "Region", "type": "text", "default": "us"},
	}, proj.Variables)
}

// TestDashboardDocWidgetWriteRoundTrip walks add → update → delete and asserts
// each REST write lands in both the materialized widgets row and the stored
// document, and that deleting a widget missing from the document is a 404.
func TestDashboardDocWidgetWriteRoundTrip(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-widgets-%d@example.com", time.Now().UnixNano()), "Dash Doc Widgets Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	// Add: the widget exists as a row and in the stored document.
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 1",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})
	widgetID := widget["id"].(string)

	var count int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE id = $1`, widgetID).Scan(&count))
	require.Equal(t, 1, count)

	proj := projectDashboardDoc(t, srv, dashID)
	require.Empty(t, proj.Warnings)
	require.Contains(t, proj.Widgets, widgetID)
	require.NotNil(t, proj.Widgets[widgetID].Query)
	require.Equal(t, "SELECT 1", *proj.Widgets[widgetID].Query)

	// Update: the new SQL lands in the row and in the document.
	raw, _ := json.Marshal(map[string]any{"query": "SELECT 2"})
	req := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	var queryOut *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT query FROM widgets WHERE id = $1`, widgetID).Scan(&queryOut))
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT 2", *queryOut)

	proj = projectDashboardDoc(t, srv, dashID)
	require.NotNil(t, proj.Widgets[widgetID].Query)
	require.Equal(t, "SELECT 2", *proj.Widgets[widgetID].Query)

	// Delete: the row is gone and the document no longer carries the widget.
	req = httptest.NewRequest("DELETE", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE id = $1`, widgetID).Scan(&count))
	require.Zero(t, count)

	proj = projectDashboardDoc(t, srv, dashID)
	require.NotContains(t, proj.Widgets, widgetID)

	// Deleting a widget the document does not carry is a 404, not a silent
	// success.
	req = httptest.NewRequest("DELETE", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestConvertWidgetToQueryUpdatesDocument pins that convert-to-query applies
// the widget change and the appended variable to the stored document (and
// materializes both), not only to the derived rows.
func TestConvertWidgetToQueryUpdatesDocument(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-convert-%d@example.com", time.Now().UnixNano()), "Dash Doc Convert Org")
	connID := createConnector(t, srv, token)

	nbID := createNotebook(t, srv, token, "Convert Doc NB")
	setNotebookParameters(t, srv, token, nbID, []map[string]any{
		{"name": "who", "type": "string", "default": "world"},
	})
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT {{who}} AS greeting", connID)

	dashID := createDashWithSettings(t, srv, token, nil)
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"notebook_id": nbID,
		"cell_id":     cellID,
		"type":        "table",
		"layout":      map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})
	widgetID := widget["id"].(string)

	rec := convertWidgetToQuery(t, srv, token, dashID, widgetID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Materialized row is a query widget now.
	var connectorOut, queryOut, notebookOut, cellOut *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT connector_id, query, notebook_id, cell_id FROM widgets WHERE id = $1`, widgetID).
		Scan(&connectorOut, &queryOut, &notebookOut, &cellOut))
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT {{who}} AS greeting", *queryOut)
	require.Nil(t, notebookOut)
	require.Nil(t, cellOut)

	// Stored document carries the converted widget and the appended variable.
	proj := projectDashboardDoc(t, srv, dashID)
	require.Empty(t, proj.Warnings)
	w := proj.Widgets[widgetID]
	require.NotNil(t, w.ConnectorID)
	require.Equal(t, connID, *w.ConnectorID)
	require.Nil(t, w.NotebookID)
	require.Nil(t, w.CellID)
	require.NotNil(t, w.Query)
	require.Equal(t, "SELECT {{who}} AS greeting", *w.Query)
	require.Len(t, proj.Variables, 1)
	require.Equal(t, "who", proj.Variables[0]["name"])
	require.Equal(t, "world", proj.Variables[0]["default"])
}

// TestWidgetWriteRejectsDanglingConnectorRef pins connector reference
// validation on doc-backed widget writes: a connector that is missing or
// soft-deleted is a 404 before the document write, and neither the derived row
// nor the stored document changes. Without the check the document store would
// accept the widget and the materializer would drop its row after a success.
func TestWidgetWriteRejectsDanglingConnectorRef(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-conn-ref-%d@example.com", time.Now().UnixNano()), "Dash Doc Conn Ref Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	layout := map[string]int{"row": 0, "col": 0, "width": 6, "height": 6}
	missing := uuid.NewString()

	// A real widget first: it seeds the document and gives the dangling-ref
	// cases something to leave untouched.
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 1",
		"type":         "table",
		"layout":       layout,
	})
	widgetID := widget["id"].(string)

	// Add with a nonexistent connector: 404, nothing new materialized.
	rec := postWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": missing,
		"query":        "SELECT 1",
		"type":         "table",
		"layout":       map[string]int{"row": 6, "col": 0, "width": 6, "height": 6},
	})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	var count int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&count))
	require.Equal(t, 1, count)
	proj := projectDashboardDoc(t, srv, dashID)
	require.Len(t, proj.Widgets, 1)
	require.Contains(t, proj.Widgets, widgetID)

	// Add with a soft-deleted connector: also 404.
	deletedConn := createConnector(t, srv, token)
	_, err := srv.DB().Pool.Exec(context.Background(),
		`UPDATE connectors SET deleted_at = NOW() WHERE id = $1`, deletedConn)
	require.NoError(t, err)
	rec = postWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": deletedConn,
		"query":        "SELECT 1",
		"type":         "table",
		"layout":       map[string]int{"row": 6, "col": 0, "width": 6, "height": 6},
	})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM widgets WHERE dashboard_id = $1`, dashID).Scan(&count))
	require.Equal(t, 1, count)

	// Update to a nonexistent connector: 404, row and document keep the old
	// reference.
	rec = putWidgetRaw(t, srv, token, dashID, widgetID, map[string]any{"connector_id": missing})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	var connectorOut *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT connector_id FROM widgets WHERE id = $1`, widgetID).Scan(&connectorOut))
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	proj = projectDashboardDoc(t, srv, dashID)
	require.NotNil(t, proj.Widgets[widgetID].ConnectorID)
	require.Equal(t, connID, *proj.Widgets[widgetID].ConnectorID)

	// Update to a soft-deleted connector: 404, unchanged.
	rec = putWidgetRaw(t, srv, token, dashID, widgetID, map[string]any{"connector_id": deletedConn})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT connector_id FROM widgets WHERE id = $1`, widgetID).Scan(&connectorOut))
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	proj = projectDashboardDoc(t, srv, dashID)
	require.NotNil(t, proj.Widgets[widgetID].ConnectorID)
	require.Equal(t, connID, *proj.Widgets[widgetID].ConnectorID)
}

// TestWidgetUpdateEnforcesSourceInvariant pins the merged-widget source
// invariant: a connector widget must keep a non-empty query, so a PUT that
// would create a shape the document store cannot materialize is a 400 and
// changes nothing.
func TestWidgetUpdateEnforcesSourceInvariant(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-invariant-%d@example.com", time.Now().UnixNano()), "Dash Doc Invariant Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	// Clearing the query of a connector widget is rejected and nothing
	// changes in the row or the document.
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 1",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})
	widgetID := widget["id"].(string)
	rec := putWidgetRaw(t, srv, token, dashID, widgetID, map[string]any{"query": ""})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "query is required for query widgets")

	var connectorOut, queryOut *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT connector_id, query FROM widgets WHERE id = $1`, widgetID).Scan(&connectorOut, &queryOut))
	require.NotNil(t, connectorOut)
	require.Equal(t, connID, *connectorOut)
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT 1", *queryOut)
	proj := projectDashboardDoc(t, srv, dashID)
	require.NotNil(t, proj.Widgets[widgetID].Query)
	require.Equal(t, "SELECT 1", *proj.Widgets[widgetID].Query)

	// Adding a connector to a widget with no query is rejected: the merged
	// shape would be a connector widget without a query.
	textWidget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"type":   "text",
		"layout": map[string]int{"row": 6, "col": 0, "width": 6, "height": 2},
	})
	textID := textWidget["id"].(string)
	rec = putWidgetRaw(t, srv, token, dashID, textID, map[string]any{"connector_id": connID})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "query is required for query widgets")
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT connector_id FROM widgets WHERE id = $1`, textID).Scan(&connectorOut))
	require.Nil(t, connectorOut)
	proj = projectDashboardDoc(t, srv, dashID)
	require.Nil(t, proj.Widgets[textID].ConnectorID)
}

// TestWidgetWriteAcceptsNonCanonicalUUIDPath pins path-ID canonicalization:
// document widget keys are canonical lowercase UUIDs, so an uppercase spelling
// of the same UUID must address the widget for update and convert.
func TestWidgetWriteAcceptsNonCanonicalUUIDPath(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-uuid-%d@example.com", time.Now().UnixNano()), "Dash Doc UUID Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 1",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})
	widgetID := widget["id"].(string)

	// Uppercase UUID on update.
	rec := putWidgetRaw(t, srv, token, dashID, strings.ToUpper(widgetID), map[string]any{
		"layout": map[string]int{"row": 2, "col": 0, "width": 6, "height": 4},
	})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	var layoutOut string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT layout::text FROM widgets WHERE id = $1`, widgetID).Scan(&layoutOut))
	require.JSONEq(t, `{"row":2,"col":0,"width":6,"height":4}`, layoutOut)

	// Uppercase UUID on convert.
	nbID := createNotebook(t, srv, token, "UUID Convert NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 42", connID)
	cellWidget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"notebook_id": nbID,
		"cell_id":     cellID,
		"type":        "table",
		"layout":      map[string]int{"row": 6, "col": 0, "width": 6, "height": 6},
	})
	cellWidgetID := cellWidget["id"].(string)
	rec = convertWidgetToQuery(t, srv, token, dashID, strings.ToUpper(cellWidgetID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var queryOut *string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT query FROM widgets WHERE id = $1`, cellWidgetID).Scan(&queryOut))
	require.NotNil(t, queryOut)
	require.Equal(t, "SELECT 42", *queryOut)
}

// TestDashboardUpdateOnTrashedDashboardIs404 confirms the deliberate trashed
// behavior (Task 22): a soft-deleted dashboard is "not found" for title and
// settings updates, so a stale editor cannot write into a trashed document.
func TestDashboardUpdateOnTrashedDashboardIs404(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-doc-trashed-%d@example.com", time.Now().UnixNano()), "Dash Doc Trashed Org")
	dashID := createDashWithSettings(t, srv, token, nil)

	// Trash via the public endpoint.
	req := httptest.NewRequest("DELETE", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	// Title and settings updates are 404: the document loader treats a
	// trashed dashboard as missing.
	for _, body := range []map[string]any{
		{"title": "nope"},
		{"settings": map[string]any{"grid_cols": 6}},
	} {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-AETHER-Admin-Mode", "true")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	}

	// The derived row keeps the pre-trash title.
	var title string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT title FROM dashboards WHERE id = $1`, dashID).Scan(&title))
	require.Equal(t, "Query Dash", title)
}
