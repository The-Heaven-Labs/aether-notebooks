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
)

// create_dashboard_widget must not snapshot the cell's metadata.chart into
// widgets.config: a creation-time copy would shadow later notebook chart-config
// edits on any surface that merges widget config. widgets.config holds only
// explicit per-widget overrides flagged with config_edited_from_dashboard.
func TestCreateDashboardWidgetDoesNotSnapshotCellChartConfig(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	email := fmt.Sprintf("dash-widget-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Widget Org")
	nbID := createNotebook(t, srv, token, "Chart NB")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1 AS x", connID)

	// Seed a non-empty chart config on the cell metadata.
	updateBody, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"chart": map[string]any{"chartType": "pie", "showLegend": false},
		},
	})
	req := httptest.NewRequest("PUT",
		fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "update cell metadata: %s", rec.Body.String())

	// Create a dashboard to host the widget.
	dashBody, _ := json.Marshal(map[string]string{"title": "Chart Dashboard"})
	req = httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(dashBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create dashboard: %s", rec.Body.String())
	var dashResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dashResp))
	dashID, _ := dashResp["id"].(string)
	require.NotEmpty(t, dashID)

	// Add the cell as a chart widget over MCP.
	code, resp, _ := doMCPRequest(t, srv, token, "tools/call", map[string]any{
		"name": "create_dashboard_widget",
		"arguments": map[string]any{
			"dashboard_id": dashID, "notebook_id": nbID, "cell_id": cellID,
			"type": "chart", "row": 0, "col": 0, "width": 6, "height": 4,
		},
	})
	require.Equal(t, http.StatusOK, code, "mcp tools/call: %v", resp)
	text := mcpResultText(t, resp)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &result))
	widgetID, _ := result["widget_id"].(string)
	require.NotEmpty(t, widgetID, "widget_id missing from result: %s", text)

	var cfg string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT config::text FROM widgets WHERE id = $1`, widgetID).Scan(&cfg))
	require.JSONEq(t, `{}`, cfg,
		"widget config must not snapshot the cell chart config")

	// The cell remains the source of truth and keeps its chart config.
	var meta string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT metadata::text FROM cells WHERE id = $1`, cellID).Scan(&meta))
	require.JSONEq(t, `{"chart":{"chartType":"pie","showLegend":false}}`, meta)
}

// TestDashboardWidgetOverlapIsChapterAware pins the tab-scoped overlap check:
// the app renders one independent grid per chapter, so widgets at identical
// coordinates in different tabs coexist, while an overlap within the same
// effective chapter is still rejected. Empty and unknown tabs fall back to the
// first chapter, and a dashboard without tabs keeps the global check.
func TestDashboardWidgetOverlapIsChapterAware(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	email := fmt.Sprintf("dash-tabs-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Tabs Org")
	dashID := createDashWithSettings(t, srv, token, map[string]any{
		"tabs": []map[string]any{
			{"id": "alpha", "title": "Alpha"},
			{"id": "beta", "title": "Beta"},
		},
	})

	add := func(dashID string, layout map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"type": "text", "layout": layout})
		req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-AETHER-Admin-Mode", "true")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	layout := func(tab string) map[string]any {
		l := map[string]any{"row": 0, "col": 0, "width": 6, "height": 6}
		if tab != "" {
			l["tab"] = tab
		}
		return l
	}

	// The first widget lands in chapter alpha.
	require.Equal(t, http.StatusCreated, add(dashID, layout("alpha")).Code)

	// Identical coordinates in chapter beta are accepted: a different grid.
	require.Equal(t, http.StatusCreated, add(dashID, layout("beta")).Code)

	// Identical coordinates in the same chapter are still rejected.
	rec := add(dashID, layout("alpha"))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "widget overlaps existing widget")

	// Empty and unknown tabs both resolve to the first chapter (alpha), so
	// they collide with the alpha widget.
	for _, tab := range []string{"", "gamma"} {
		rec = add(dashID, layout(tab))
		require.Equal(t, http.StatusBadRequest, rec.Code, "tab %q: %s", tab, rec.Body.String())
		require.Contains(t, rec.Body.String(), "widget overlaps existing widget")
	}

	// Without tabs the check stays global: the same coordinates in a
	// well-formed but undefined tab still overlap.
	plainID := createDashWithSettings(t, srv, token, nil)
	require.Equal(t, http.StatusCreated, add(plainID, layout("")).Code)
	rec = add(plainID, layout("beta"))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "widget overlaps existing widget")
}
