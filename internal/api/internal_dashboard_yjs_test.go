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
