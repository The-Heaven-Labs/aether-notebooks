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

// TestDashboardQueryCacheSharedAcrossIdenticalAccess pins the shared-cache
// contract: authenticated viewers with identical effective data access share a
// cache entry (the key carries an access fingerprint, not the viewer id),
// while different interpolated filters produce distinct entries.
func TestDashboardQueryCacheSharedAcrossIdenticalAccess(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("dash-cache-owner-%d@example.com", time.Now().UnixNano())
	tokenA := registerAndGetToken(t, srv, email, "Dash Cache Org")
	connID := createConnector(t, srv, tokenA)

	settings := map[string]any{
		"variables": []map[string]any{
			{"name": "who", "label": "Who", "type": "text", "default": "world"},
		},
	}
	dashID := createDashWithSettings(t, srv, tokenA, settings)
	widgetID := addQueryWidget(t, srv, tokenA, dashID, connID, "SELECT {{who}} AS greeting")

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	// Two more org members with view_with_data on the dashboard and use on the
	// unmanaged connector: the same execution identity as A, so all three must
	// share cache entries.
	grantViewer := func(label string) string {
		t.Helper()
		userID := insertUser(t, srv,
			fmt.Sprintf("dash-cache-%s-%d@example.com", label, time.Now().UnixNano()), "Cache Viewer")
		addOrgMember(t, srv, orgID, userID, "non-admin")
		grantACL(t, srv, orgID, "dashboard", dashID, "user", userID, "view", "view_with_data")
		grantACL(t, srv, orgID, "connector", connID, "user", userID, "view", "use")
		return issueToken(t, userID, orgID, "non-admin")
	}
	tokenB := grantViewer("b")
	tokenC := grantViewer("c")

	execute := func(token string, body map[string]any) map[string]any {
		t.Helper()
		rec := executeDashboardWidget(t, srv, token, dashID, body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		return resp
	}
	cachedFlag := func(resp map[string]any) bool {
		t.Helper()
		cached, ok := resp["cached"].(bool)
		require.True(t, ok, "cached flag missing from response: %v", resp)
		return cached
	}
	rowsOf := func(resp map[string]any) []any {
		t.Helper()
		outputs, ok := resp["outputs"].([]any)
		require.True(t, ok && len(outputs) > 0, "expected outputs, got %v", resp)
		data, ok := outputs[0].(map[string]any)["data"].(map[string]any)
		require.True(t, ok, "expected table data, got %v", outputs[0])
		rows, ok := data["rows"].([]any)
		require.True(t, ok, "expected rows, got %v", data)
		return rows
	}

	body := map[string]any{"widget_id": widgetID, "variables": map[string]any{"who": "shared"}}
	respA := execute(tokenA, body)
	require.False(t, cachedFlag(respA), "the first run must be a cache miss")

	respB := execute(tokenB, body)
	require.True(t, cachedFlag(respB), "identical access + identical filters must share the entry")
	require.Equal(t, rowsOf(respA), rowsOf(respB), "the shared entry must return the first runner's rows")

	// Different filter values interpolate into different SQL, so they must not
	// share the entry; a repeat of C's own values then hits C's entry.
	bodyC := map[string]any{"widget_id": widgetID, "variables": map[string]any{"who": "different"}}
	respC := execute(tokenC, bodyC)
	require.False(t, cachedFlag(respC), "different filters must not hit the shared entry")
	respC2 := execute(tokenC, bodyC)
	require.True(t, cachedFlag(respC2), "a repeated run of the same filters must hit the cache")
	require.Equal(t, rowsOf(respC), rowsOf(respC2))
}

// TestDashboardQueryCacheHitStillRequiresConnectorUse pins that a shared cache
// hit cannot bypass the connector `use` gate: a viewer with view_with_data on
// the dashboard but no `use` on the connector is denied even after another
// user has warmed the shared entry.
func TestDashboardQueryCacheHitStillRequiresConnectorUse(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("dash-cache-use-%d@example.com", time.Now().UnixNano())
	tokenA := registerAndGetToken(t, srv, email, "Dash Cache Use Org")
	connID := createConnector(t, srv, tokenA)

	dashID := createDashWithSettings(t, srv, tokenA, nil)
	widgetID := addQueryWidget(t, srv, tokenA, dashID, connID, "SELECT 1 AS x")

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	body := map[string]any{"widget_id": widgetID}
	recA := executeDashboardWidget(t, srv, tokenA, dashID, body)
	require.Equal(t, http.StatusOK, recA.Code, recA.Body.String())

	// B may view dashboard data but holds no `use` on the connector, so a
	// live run would be denied by openQuery.
	viewerID := insertUser(t, srv,
		fmt.Sprintf("dash-cache-nouse-%d@example.com", time.Now().UnixNano()), "No Use Viewer")
	addOrgMember(t, srv, orgID, viewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view", "view_with_data")
	tokenB := issueToken(t, viewerID, orgID, "non-admin")

	recB := executeDashboardWidget(t, srv, tokenB, dashID, body)
	require.Equal(t, http.StatusForbidden, recB.Code, recB.Body.String())
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

func shareDashboard(t *testing.T, srv *api.Server, token, dashID string) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/share", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["token"].(string)
}

func publicDashboardExecute(t *testing.T, srv *api.Server, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/public/"+token+"/execute", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.%d.%d.%d", time.Now().UnixNano()%250, (time.Now().UnixNano()/250)%250, 7))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestPublicDashboardExecuteRequiresOptIn(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("pub-optin-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pub OptIn Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")
	publicToken := shareDashboard(t, srv, token, dashID)

	rec := publicDashboardExecute(t, srv, publicToken, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "live queries are not enabled")
}

func TestPublicDashboardExecuteRunsAsCreator(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("pub-live-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pub Live Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, map[string]any{"public_live": true})
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")
	publicToken := shareDashboard(t, srv, token, dashID)

	rec := publicDashboardExecute(t, srv, publicToken, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	outputs := resp["outputs"].([]any)
	require.NotEmpty(t, outputs)
	rows := outputs[0].(map[string]any)["data"].(map[string]any)["rows"].([]any)
	require.Len(t, rows, 1)
}

func TestPublicDashboardExecuteRateLimited(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("pub-rl-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pub RL Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, map[string]any{"public_live": true})
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")
	publicToken := shareDashboard(t, srv, token, dashID)

	// Fresh client IP so the limiter key is isolated from other tests.
	ip := fmt.Sprintf("192.0.2.%d", time.Now().UnixNano()%250)
	dispatch := func() *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"widget_id": widgetID})
		req := httptest.NewRequest("POST", "/api/v1/public/"+publicToken+"/execute", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	first := dispatch()
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, "60", first.Header().Get("X-RateLimit-Limit"))

	got429 := false
	for i := 0; i < 65; i++ {
		if rec := dispatch(); rec.Code == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	require.True(t, got429, "expected 429 after exceeding the public execute rate limit")
}

func convertWidgetToQuery(t *testing.T, srv *api.Server, token, dashID, widgetID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID+"/convert-to-query", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func createCellWithSlug(t *testing.T, srv *api.Server, token, nbID, source, connID, slug string) string {
	t.Helper()
	cellID := createCell(t, srv, token, nbID, "sql", source, connID)
	if slug != "" {
		raw, _ := json.Marshal(map[string]any{"slug": slug})
		req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-AETHER-Admin-Mode", "true")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	return cellID
}

func setNotebookParameters(t *testing.T, srv *api.Server, token, nbID string, params []map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"parameters": params})
	req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestConvertCellWidgetToQueryWidget(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("convert-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Convert Org")
	connID := createConnector(t, srv, token)

	nbID := createNotebook(t, srv, token, "Convert NB")
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
	var resp struct {
		Widget struct {
			ConnectorID *string `json:"connector_id"`
			Query       *string `json:"query"`
			NotebookID  *string `json:"notebook_id"`
			CellID      *string `json:"cell_id"`
			Language    string  `json:"language"`
		} `json:"widget"`
		Variables []struct {
			Name    string      `json:"name"`
			Type    string      `json:"type"`
			Default interface{} `json:"default"`
		} `json:"variables"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotNil(t, resp.Widget.ConnectorID)
	require.Equal(t, connID, *resp.Widget.ConnectorID)
	require.NotNil(t, resp.Widget.Query)
	require.Equal(t, "SELECT {{who}} AS greeting", *resp.Widget.Query)
	require.Nil(t, resp.Widget.NotebookID)
	require.Nil(t, resp.Widget.CellID)
	require.Equal(t, "sql", resp.Widget.Language)
	require.Len(t, resp.Variables, 1)
	require.Equal(t, "who", resp.Variables[0].Name)
	require.Equal(t, "text", resp.Variables[0].Type)
	require.Equal(t, "world", resp.Variables[0].Default)

	// The converted widget now executes with a provided value.
	execRec := executeDashboardWidget(t, srv, token, dashID, map[string]any{
		"widget_id": widgetID,
		"variables": map[string]any{"who": "Aether"},
	})
	require.Equal(t, http.StatusOK, execRec.Code, execRec.Body.String())
	var execResp map[string]any
	require.NoError(t, json.NewDecoder(execRec.Body).Decode(&execResp))
	rows := execResp["outputs"].([]any)[0].(map[string]any)["data"].(map[string]any)["rows"].([]any)
	require.Equal(t, "Aether", rows[0].([]any)[0])

	// Persisted dashboard settings also carry the variable.
	req := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusOK, rec2.Code, rec2.Body.String())
	require.Contains(t, rec2.Body.String(), `"who"`)
}

func TestConvertCellWidgetInlinesSlugs(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("convert-slug-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Convert Slug Org")
	connID := createConnector(t, srv, token)

	nbID := createNotebook(t, srv, token, "Convert Slug NB")
	createCellWithSlug(t, srv, token, nbID, "SELECT 1 AS x", connID, "base")
	cellID := createCellWithSlug(t, srv, token, nbID, "SELECT * FROM {{base}}", connID, "")

	dashID := createDashWithSettings(t, srv, token, nil)
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"notebook_id": nbID,
		"cell_id":     cellID,
		"type":        "table",
		"layout":      map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})

	rec := convertWidgetToQuery(t, srv, token, dashID, widget["id"].(string))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "SELECT 1 AS x")
	require.NotContains(t, rec.Body.String(), "{{base}}")
}

func TestConvertRejectsNonCellWidget(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("convert-noncell-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Convert NonCell Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1")

	rec := convertWidgetToQuery(t, srv, token, dashID, widgetID)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "not linked to a notebook cell")
}

func TestUpdateWidgetTypeAndQuerySource(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("widget-update-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Widget Update Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")

	put := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-AETHER-Admin-Mode", "true")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	require.Equal(t, http.StatusNoContent, put(map[string]any{"type": "chart"}).Code)
	require.Equal(t, http.StatusNoContent, put(map[string]any{"query": "SELECT 2 AS x", "connector_id": connID}).Code)

	req := httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var dash struct {
		Widgets []struct {
			Type  string `json:"type"`
			Query string `json:"query"`
		} `json:"widgets"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&dash))
	require.Len(t, dash.Widgets, 1)
	require.Equal(t, "chart", dash.Widgets[0].Type)
	require.Equal(t, "SELECT 2 AS x", dash.Widgets[0].Query)

	require.Equal(t, http.StatusBadRequest, put(map[string]any{"type": "bogus"}).Code)
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
