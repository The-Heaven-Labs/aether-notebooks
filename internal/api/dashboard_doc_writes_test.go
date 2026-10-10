package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
