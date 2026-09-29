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

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

func createDashWithSettings(t *testing.T, srv *api.Server, token string, settings map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"title": "Query Dash", "settings": settings})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var dash map[string]any
	json.NewDecoder(rec.Body).Decode(&dash)
	return dash["id"].(string)
}

func addWidgetRaw(t *testing.T, srv *api.Server, token, dashID string, body map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var widget map[string]any
	json.NewDecoder(rec.Body).Decode(&widget)
	return widget
}

func addQueryWidget(t *testing.T, srv *api.Server, token, dashID, connectorID, query string) string {
	t.Helper()
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connectorID,
		"query":        query,
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})
	return widget["id"].(string)
}

func executeDashboardWidget(t *testing.T, srv *api.Server, token, dashID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/execute", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestDashboardQueryExecuteEndToEnd(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	email := fmt.Sprintf("dash-query-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Query Org")
	connID := createConnector(t, srv, token)

	// Create a dashboard with a variable and a query widget.
	settings := map[string]any{
		"variables": []map[string]any{
			{"name": "who", "label": "Who", "type": "text", "default": "world"},
		},
	}
	dashID := createDashWithSettings(t, srv, token, settings)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT {{who}} AS greeting")

	// Execute with a provided variable value.
	rec := executeDashboardWidget(t, srv, token, dashID, map[string]any{
		"widget_id": widgetID,
		"variables": map[string]any{"who": "Aether"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	outputs, ok := resp["outputs"].([]any)
	require.True(t, ok && len(outputs) > 0, "expected outputs, got %v", resp)
	data := outputs[0].(map[string]any)["data"].(map[string]any)
	rows := data["rows"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Aether", rows[0].([]any)[0])

	// Execute without variables: the declaration default applies.
	rec = executeDashboardWidget(t, srv, token, dashID, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	rows = resp["outputs"].([]any)[0].(map[string]any)["data"].(map[string]any)["rows"].([]any)
	require.Equal(t, "world", rows[0].([]any)[0])
}

func TestDashboardQueryExecuteRequiresViewWithData(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("dash-403-owner-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash 403 Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	// Second user in the same org with only `view` on the dashboard.
	viewerEmail := fmt.Sprintf("dash-403-viewer-%d@example.com", time.Now().UnixNano())
	var viewerID string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Viewer', false) RETURNING id`, viewerEmail).Scan(&viewerID))
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'non-admin')`, orgID, viewerID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'dashboard', $2::uuid, 'user', $3, ARRAY['view'])`, orgID, dashID, viewerID)
	require.NoError(t, err)
	viewerToken, err := testJWT.Issue(viewerID, orgID, "non-admin")
	require.NoError(t, err)

	rec := executeDashboardWidget(t, srv, viewerToken, dashID, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// Granting view_with_data and connector use unlocks execution.
	_, err = db.Pool.Exec(ctx,
		`UPDATE acl_entries SET actions = ARRAY['view','view_with_data']
		 WHERE resource_type = 'dashboard' AND resource_id = $1::uuid AND subject_id = $2`, dashID, viewerID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'connector', $2::uuid, 'user', $3, ARRAY['view','use'])`, orgID, connID, viewerID)
	require.NoError(t, err)
	rec = executeDashboardWidget(t, srv, viewerToken, dashID, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func dashboardVariableOptions(t *testing.T, srv *api.Server, token, dashID, name string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/variables/"+name+"/options", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestDashboardVariableOptions(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-opts-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Opts Org")
	connID := createConnector(t, srv, token)

	settings := map[string]any{
		"variables": []map[string]any{
			{"name": "country", "type": "text", "default": "US"},
			{
				"name": "city", "label": "City", "type": "single_select", "depends_on": []string{"country"},
				"options": map[string]any{
					"mode": "query",
					"query": map[string]any{
						"connector_id": connID,
						"sql":          "SELECT 'Paris' AS label, 'paris' AS value",
					},
					"label_column": "label", "value_column": "value",
				},
			},
		},
	}
	dashID := createDashWithSettings(t, srv, token, settings)

	rec := dashboardVariableOptions(t, srv, token, dashID, "city", map[string]any{"variables": map[string]any{"country": "FR"}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Options []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"options"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Equal(t, []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}{{Label: "Paris", Value: "paris"}}, resp.Options)

	// Unknown variable → 404.
	rec = dashboardVariableOptions(t, srv, token, dashID, "nope", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestDashboardVariableOptionsRejectsStaticVariable(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("dash-opts-static-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Opts Static Org")
	settings := map[string]any{
		"variables": []map[string]any{
			{
				"name": "region", "type": "single_select",
				"options": map[string]any{
					"mode":   "static",
					"values": []map[string]any{{"label": "EMEA", "value": "EMEA"}},
				},
			},
		},
	}
	dashID := createDashWithSettings(t, srv, token, settings)

	rec := dashboardVariableOptions(t, srv, token, dashID, "region", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "no query-backed options")
}

func TestDashboardQueryExecuteUnknownVariableReturns400(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	email := fmt.Sprintf("dash-unkvar-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash UnkVar Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT {{nope}}")

	rec := executeDashboardWidget(t, srv, token, dashID, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.True(t, strings.Contains(rec.Body.String(), "nope"), rec.Body.String())
}

func TestDashboardQueryExecuteNonQueryWidgetReturns400(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	email := fmt.Sprintf("dash-cellwgt-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash CellWgt Org")
	connID := createConnector(t, srv, token)

	nbID := createNotebook(t, srv, token, "Legacy NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)
	dashID := createDashWithSettings(t, srv, token, nil)
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"notebook_id": nbID,
		"cell_id":     cellID,
		"type":        "table",
		"layout":      map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})

	rec := executeDashboardWidget(t, srv, token, dashID, map[string]any{"widget_id": widget["id"].(string)})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "not a query widget")
}
