package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/reearth/ygo/crdt"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// versionSummaryBody mirrors one entry of the GET /dashboards/{id}/versions
// response.
type versionSummaryBody struct {
	Version        int       `json:"version"`
	CreatedByEmail string    `json:"created_by_email"`
	CreatedAt      time.Time `json:"created_at"`
	ContentHash    string    `json:"content_hash"`
}

type versionsListBody struct {
	Versions              []versionSummaryBody `json:"versions"`
	Latest                *versionSummaryBody  `json:"latest"`
	HasUnpublishedChanges bool                 `json:"has_unpublished_changes"`
	DraftHash             string               `json:"draft_hash"`
	Warnings              []string             `json:"warnings"`
}

type dashboardDetailBody struct {
	Title    string `json:"title"`
	Settings struct {
		AutoRefreshSeconds int `json:"auto_refresh_seconds"`
		Variables          []struct {
			Name string `json:"name"`
		} `json:"variables"`
	} `json:"settings"`
	Widgets []struct {
		ID     string `json:"id"`
		Query  string `json:"query"`
		Layout struct {
			Tab string `json:"tab"`
		} `json:"layout"`
		Config map[string]any `json:"config"`
	} `json:"widgets"`
	PublishedVersion *struct {
		Version   int       `json:"version"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"published_version"`
}

// versionsRequest dispatches one request against the dashboard routes with a
// bearer token and returns the recorder.
func versionsRequest(t *testing.T, srv *api.Server, token, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func addVersionsQueryWidget(t *testing.T, srv *api.Server, token, dashID, connectorID, query string, col int) string {
	t.Helper()
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connectorID,
		"query":        query,
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": col, "width": 6, "height": 6},
	})
	return widget["id"].(string)
}

func publishVersionsDashboard(t *testing.T, srv *api.Server, token, dashID string) (int, string, time.Time) {
	t.Helper()
	rec := versionsRequest(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/publish", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Version     int       `json:"version"`
		ContentHash string    `json:"content_hash"`
		CreatedAt   time.Time `json:"created_at"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp.Version, resp.ContentHash, resp.CreatedAt
}

func listVersions(t *testing.T, srv *api.Server, token, dashID string) versionsListBody {
	t.Helper()
	rec := versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID+"/versions", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp versionsListBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

func getVersionsDashboard(t *testing.T, srv *api.Server, token, dashID string) dashboardDetailBody {
	t.Helper()
	rec := versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp dashboardDetailBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

func TestDashboardPublishVersions(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("dash-versions-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Versions Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)

	var orgID, ownerID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&ownerID))

	// Before any publish the draft has no published counterpart.
	list := listVersions(t, srv, token, dashID)
	require.Empty(t, list.Versions)
	require.Nil(t, list.Latest)
	require.True(t, list.HasUnpublishedChanges)
	require.NotEmpty(t, list.DraftHash)

	// 1. Publish with no widgets → version 1.
	v1Version, v1Hash, v1CreatedAt := publishVersionsDashboard(t, srv, token, dashID)
	require.Equal(t, 1, v1Version)
	require.Len(t, v1Hash, 64, "content_hash must be a hex sha256")
	require.False(t, v1CreatedAt.IsZero())

	// 2. Add a query widget — with a tab placement and a non-empty config,
	// both of which must survive publish → restore — and publish again →
	// version 2.
	widget := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 1 AS x",
		"type":         "table",
		"layout":       map[string]any{"row": 0, "col": 0, "width": 6, "height": 6, "tab": "overview"},
		"config":       map[string]any{"chart": "bar"},
	})
	widgetID := widget["id"].(string)
	v2Version, v2Hash, _ := publishVersionsDashboard(t, srv, token, dashID)
	require.Equal(t, 2, v2Version)
	require.NotEqual(t, v1Hash, v2Hash, "a new widget must change the published hash")

	// 3. Newest-first list, latest is v2, and the draft matches it right after
	// publishing.
	list = listVersions(t, srv, token, dashID)
	require.Len(t, list.Versions, 2)
	require.Equal(t, 2, list.Versions[0].Version)
	require.Equal(t, 1, list.Versions[1].Version)
	require.Equal(t, email, list.Versions[0].CreatedByEmail)
	require.Equal(t, v2Hash, list.Versions[0].ContentHash)
	require.NotNil(t, list.Latest)
	require.Equal(t, 2, list.Latest.Version)
	require.Equal(t, v2Hash, list.Latest.ContentHash)
	require.False(t, list.HasUnpublishedChanges)
	require.Equal(t, list.Latest.ContentHash, list.DraftHash)

	// 4. A draft change (rename + settings + another widget) is detected and
	// does not publish.
	rec := versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID,
		map[string]any{"title": "Renamed", "settings": map[string]any{"auto_refresh_seconds": 30}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	addVersionsQueryWidget(t, srv, token, dashID, connID, "SELECT 2 AS x", 6)
	list = listVersions(t, srv, token, dashID)
	require.True(t, list.HasUnpublishedChanges)
	require.NotEqual(t, list.Latest.ContentHash, list.DraftHash)
	require.Len(t, list.Versions, 2, "changing the draft must not publish")

	// 5. Restore v1: the draft returns to the v1 snapshot (title, settings, no
	// widgets) and the version list is unchanged.
	rec = versionsRequest(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/versions/1/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var restored struct {
		Restored int `json:"restored"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&restored))
	require.Equal(t, 1, restored.Restored)

	dash := getVersionsDashboard(t, srv, token, dashID)
	require.Equal(t, "Query Dash", dash.Title, "the rename must revert to the v1 snapshot title")
	require.Empty(t, dash.Widgets)
	require.Equal(t, 0, dash.Settings.AutoRefreshSeconds, "v1 predates the settings change")

	list = listVersions(t, srv, token, dashID)
	require.Len(t, list.Versions, 2, "restore must not publish a version")
	require.Equal(t, 2, list.Latest.Version)
	require.True(t, list.HasUnpublishedChanges, "the restored draft now differs from v2")

	// 6. Restore v2 brings the widget back with its tab and config intact, and
	// the draft becomes hash-identical to the published snapshot.
	rec = versionsRequest(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/versions/2/restore", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	dash = getVersionsDashboard(t, srv, token, dashID)
	require.Equal(t, "Query Dash", dash.Title)
	require.Len(t, dash.Widgets, 1)
	require.Equal(t, widgetID, dash.Widgets[0].ID)
	require.Equal(t, "SELECT 1 AS x", dash.Widgets[0].Query)
	require.Equal(t, "overview", dash.Widgets[0].Layout.Tab, "layout.tab must survive publish → restore")
	require.Equal(t, "bar", dash.Widgets[0].Config["chart"], "config must survive publish → restore")

	list = listVersions(t, srv, token, dashID)
	require.False(t, list.HasUnpublishedChanges, "a restored latest version must be hash-equal to the draft")
	require.Equal(t, list.Latest.ContentHash, list.DraftHash)

	// 7. Unknown versions are a 404.
	rec = versionsRequest(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/versions/99/restore", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// 8. A view-only member cannot publish, list, or restore.
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-versions-viewer-%d@example.com", time.Now().UnixNano()), "Versions Viewer")
	addOrgMember(t, srv, orgID, viewerID, "editor")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "editor")

	// The viewer's plain read succeeds, so the 403s below pin that version
	// management needs more than view.
	require.Equal(t, http.StatusOK,
		versionsRequest(t, srv, viewerToken, "GET", "/api/v1/dashboards/"+dashID, nil).Code)

	require.Equal(t, http.StatusForbidden,
		versionsRequest(t, srv, viewerToken, "POST", "/api/v1/dashboards/"+dashID+"/publish", nil).Code)
	require.Equal(t, http.StatusForbidden,
		versionsRequest(t, srv, viewerToken, "GET", "/api/v1/dashboards/"+dashID+"/versions", nil).Code)
	require.Equal(t, http.StatusForbidden,
		versionsRequest(t, srv, viewerToken, "POST", "/api/v1/dashboards/"+dashID+"/versions/1/restore", nil).Code)

	// 9. Publishes and restores are audited.
	var publishCount, restoreCount int
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'dashboard.publish' AND resource_id = $2`,
		orgID, dashID).Scan(&publishCount))
	require.Equal(t, 2, publishCount)
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'dashboard.version_restore' AND resource_id = $2`,
		orgID, dashID).Scan(&restoreCount))
	require.Equal(t, 2, restoreCount)

	var metaJSON []byte
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT metadata FROM audit_logs
		 WHERE org_id = $1 AND action = 'dashboard.publish' AND resource_id = $2
		 ORDER BY id DESC LIMIT 1`,
		orgID, dashID).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, float64(2), meta["version"])
	require.Equal(t, v2Hash, meta["content_hash"])
}

// TestDashboardVersionsListFlagsCorruptDraft pins the warning gate: a draft
// document whose widgets fail projection can never be published, so the status
// endpoint must report has_unpublished_changes even when the skipped widget
// projects out and the draft hash equals the latest published hash. The
// corrupt document is crafted directly (crdt + a direct state write) because
// the REST/relay paths validate widgets and cannot produce one.
func TestDashboardVersionsListFlagsCorruptDraft(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("dash-versions-corrupt-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash Versions Corrupt Org")
	dashID := createDashWithSettings(t, srv, token, nil)

	// A clean v1 publish first: its snapshot content (title, no widgets) is
	// exactly what the corrupt draft below projects, so the hashes match and
	// only the warnings gate can force the unpublished flag.
	_, v1Hash, _ := publishVersionsDashboard(t, srv, token, dashID)

	doc := crdt.New()
	meta := doc.GetMap("meta")
	widgets := doc.GetMap("widgets")
	doc.Transact(func(txn *crdt.Transaction) {
		meta.Set(txn, "title", "Query Dash")
		wm := crdt.NewMapPrelim()
		wm.Set(txn, "type", "bogus")
		widgets.Set(txn, uuid.NewString(), wm)
	})
	corrupt := doc.EncodeStateAsUpdate()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO dashboard_yjs_documents (dashboard_id, state)
		VALUES ($1, $2)
		ON CONFLICT (dashboard_id) DO UPDATE SET state = EXCLUDED.state`,
		dashID, corrupt)
	require.NoError(t, err)

	list := listVersions(t, srv, token, dashID)
	require.Len(t, list.Versions, 1)
	require.NotNil(t, list.Latest)
	require.NotEmpty(t, list.Warnings, "the draft's invalid widget must be surfaced to the editor")
	require.Equal(t, v1Hash, list.Latest.ContentHash)
	require.Equal(t, list.Latest.ContentHash, list.DraftHash,
		"the skipped widget projects out, so the hash alone would match")
	require.True(t, list.HasUnpublishedChanges,
		"a draft that publish would refuse must count as unpublished")
}

// TestDashboardVersionRoutesNotFound pins the 404s for a dashboard that no
// longer exists. The routes' edit middleware runs before the handlers and
// denies a never-existing id with 403 (no ACL can match); a trashed dashboard
// keeps its ACL rows, so the request reaches the handler, whose liveness
// checks answer 404.
func TestDashboardVersionRoutesNotFound(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-versions-404-%d@example.com", time.Now().UnixNano()), "Dash Versions 404 Org")
	dashID := createDashWithSettings(t, srv, token, nil)

	req := httptest.NewRequest("DELETE", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	require.Equal(t, http.StatusNotFound,
		versionsRequest(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/publish", nil).Code)
	require.Equal(t, http.StatusNotFound,
		versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID+"/versions", nil).Code)
	require.Equal(t, http.StatusNotFound,
		versionsRequest(t, srv, token, "POST", "/api/v1/dashboards/"+dashID+"/versions/1/restore", nil).Code)
}

// updateWidgetQuery rewrites one widget's SQL through the REST widget update,
// the same partial write the editor UI uses.
func updateWidgetQuery(t *testing.T, srv *api.Server, token, dashID, widgetID, query, connectorID string) {
	t.Helper()
	rec := versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID+"/widgets/"+widgetID,
		map[string]any{"query": query, "connector_id": connectorID})
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// firstCellValue decodes a dashboard query response and returns the first
// result cell of its first table output.
func firstCellValue(t *testing.T, rec *httptest.ResponseRecorder) any {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	outputs, ok := resp["outputs"].([]any)
	require.True(t, ok && len(outputs) > 0, "expected outputs, got %v", resp)
	data, ok := outputs[0].(map[string]any)["data"].(map[string]any)
	require.True(t, ok, "expected table data, got %v", outputs[0])
	rows, ok := data["rows"].([]any)
	require.True(t, ok && len(rows) > 0, "expected rows, got %v", data)
	return rows[0].([]any)[0]
}

// getPublicDashboard fetches the public dashboard payload for a token.
func getPublicDashboard(t *testing.T, srv *api.Server, token string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/public/"+token, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}

// publicDashboardOptions posts to the public variable-options endpoint with a
// fresh client IP so the per-token rate limiter stays isolated from other
// tests.
func publicDashboardOptions(t *testing.T, srv *api.Server, token, name string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/public/"+token+"/variables/"+name+"/options", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.%d.%d.%d", time.Now().UnixNano()%250, (time.Now().UnixNano()/250)%250, 8))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestDashboardGetPublished pins the wave-2 read split: once a dashboard is
// published, a viewer's plain GET serves the latest snapshot (never draft
// edits) with published_version set, while an editor keeps the draft and
// ?published=1 serves the snapshot to anyone.
func TestDashboardGetPublished(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-getpub-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash GetPub Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, map[string]any{"auto_refresh_seconds": 15})
	widgetA := addVersionsQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x", 0)

	// Publish v1 with the original title, settings, and one widget.
	publishVersionsDashboard(t, srv, token, dashID)

	// Draft edits after the publish: rename, change a setting, change the
	// widget query, and add a second widget.
	rec := versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID,
		map[string]any{"title": "Renamed", "settings": map[string]any{"auto_refresh_seconds": 30}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updateWidgetQuery(t, srv, token, dashID, widgetA, "SELECT 2 AS x", connID)
	addVersionsQueryWidget(t, srv, token, dashID, connID, "SELECT 3 AS x", 6)

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-getpub-viewer-%d@example.com", ts), "GetPub Viewer")
	addOrgMember(t, srv, orgID, viewerID, "editor")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "editor")

	// The editor's plain GET is the draft: the rename, the changed setting,
	// both widgets, and no published_version.
	draft := getVersionsDashboard(t, srv, token, dashID)
	require.Equal(t, "Renamed", draft.Title)
	require.Equal(t, 30, draft.Settings.AutoRefreshSeconds)
	require.Nil(t, draft.PublishedVersion)
	require.Len(t, draft.Widgets, 2)
	foundDraftA := false
	for _, wgt := range draft.Widgets {
		if wgt.ID == widgetA {
			foundDraftA = true
			require.Equal(t, "SELECT 2 AS x", wgt.Query)
		}
	}
	require.True(t, foundDraftA, "the draft must keep the edited widget")

	// The view-only member's plain GET is the v1 snapshot.
	published := getVersionsDashboard(t, srv, viewerToken, dashID)
	require.Equal(t, "Query Dash", published.Title)
	require.Equal(t, 15, published.Settings.AutoRefreshSeconds)
	require.NotNil(t, published.PublishedVersion)
	require.Equal(t, 1, published.PublishedVersion.Version)
	require.False(t, published.PublishedVersion.CreatedAt.IsZero())
	require.Len(t, published.Widgets, 1)
	require.Equal(t, widgetA, published.Widgets[0].ID)
	require.Equal(t, "SELECT 1 AS x", published.Widgets[0].Query)

	// ?published=1 serves the snapshot to the editor too.
	rec = versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID+"?published=1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var explicit dashboardDetailBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&explicit))
	require.Equal(t, "Query Dash", explicit.Title)
	require.Equal(t, 15, explicit.Settings.AutoRefreshSeconds)
	require.NotNil(t, explicit.PublishedVersion)
	require.Equal(t, 1, explicit.PublishedVersion.Version)
	require.Len(t, explicit.Widgets, 1)
	require.Equal(t, "SELECT 1 AS x", explicit.Widgets[0].Query)

	// The view-only member's explicit request serves the same snapshot.
	rec = versionsRequest(t, srv, viewerToken, "GET", "/api/v1/dashboards/"+dashID+"?published=1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&explicit))
	require.NotNil(t, explicit.PublishedVersion)
	require.Equal(t, 1, explicit.PublishedVersion.Version)
	require.Len(t, explicit.Widgets, 1)

	// ?published=1 before the first publish has nothing to substitute: the
	// draft serves with no published_version (execute's published:true 404s
	// instead — a deliberate asymmetry).
	unpublishedID := createDashWithSettings(t, srv, token, nil)
	unpublishedWidget := addVersionsQueryWidget(t, srv, token, unpublishedID, connID, "SELECT 9 AS x", 0)
	rec = versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+unpublishedID+"?published=1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var unpublishedBody dashboardDetailBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&unpublishedBody))
	require.Nil(t, unpublishedBody.PublishedVersion)
	require.Len(t, unpublishedBody.Widgets, 1)
	require.Equal(t, unpublishedWidget, unpublishedBody.Widgets[0].ID)
	require.Equal(t, "SELECT 9 AS x", unpublishedBody.Widgets[0].Query)
}

// TestDashboardExecutePublished pins published-mode execution: an explicit
// published:true request runs the snapshot's SQL and variable defaults and
// 404s instead of silently falling back when nothing is published, while the
// editor's plain run keeps the draft.
func TestDashboardExecutePublished(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-execpub-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash ExecPub Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")

	publishVersionsDashboard(t, srv, token, dashID)
	updateWidgetQuery(t, srv, token, dashID, widgetID, "SELECT 2 AS x", connID)

	// published:true runs the snapshot's SQL.
	rec := executeDashboardWidget(t, srv, token, dashID, map[string]any{
		"widget_id": widgetID, "published": true, "bypass_cache": true,
	})
	require.Equal(t, float64(1), firstCellValue(t, rec))

	// The editor's plain run uses the draft.
	rec = executeDashboardWidget(t, srv, token, dashID, map[string]any{
		"widget_id": widgetID, "bypass_cache": true,
	})
	require.Equal(t, float64(2), firstCellValue(t, rec))

	// published:true with no published version is a 404, never a draft
	// fallback.
	dashUnpublished := createDashWithSettings(t, srv, token, nil)
	widgetUnpublished := addQueryWidget(t, srv, token, dashUnpublished, connID, "SELECT 3 AS x")
	rec = executeDashboardWidget(t, srv, token, dashUnpublished, map[string]any{
		"widget_id": widgetUnpublished, "published": true,
	})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// A widget added after the publish is not part of the snapshot: 404.
	widgetLater := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 4 AS x",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 6, "width": 6, "height": 6},
	})["id"].(string)
	rec = executeDashboardWidget(t, srv, token, dashID, map[string]any{
		"widget_id": widgetLater, "published": true,
	})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// The snapshot's variable defaults interpolate, not the draft's.
	dashVars := createDashWithSettings(t, srv, token, map[string]any{
		"variables": []map[string]any{
			{"name": "who", "label": "Who", "type": "text", "default": "published"},
		},
	})
	widgetVars := addQueryWidget(t, srv, token, dashVars, connID, "SELECT {{who}} AS v")
	publishVersionsDashboard(t, srv, token, dashVars)
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashVars, map[string]any{
		"settings": map[string]any{"variables": []map[string]any{
			{"name": "who", "label": "Who", "type": "text", "default": "draft"},
		}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = executeDashboardWidget(t, srv, token, dashVars, map[string]any{
		"widget_id": widgetVars, "published": true, "bypass_cache": true,
	})
	require.Equal(t, "published", firstCellValue(t, rec))
	rec = executeDashboardWidget(t, srv, token, dashVars, map[string]any{
		"widget_id": widgetVars, "bypass_cache": true,
	})
	require.Equal(t, "draft", firstCellValue(t, rec))

	// Variable options resolve the variable from the snapshot too.
	dashOpts := createDashWithSettings(t, srv, token, map[string]any{
		"variables": []map[string]any{{
			"name": "city", "type": "single_select",
			"options": map[string]any{
				"mode": "query",
				"query": map[string]any{
					"connector_id": connID,
					"sql":          "SELECT 'snap' AS label, 'snap' AS value",
				},
				"label_column": "label", "value_column": "value",
			},
		}},
	})
	publishVersionsDashboard(t, srv, token, dashOpts)
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashOpts, map[string]any{
		"settings": map[string]any{"variables": []map[string]any{{
			"name": "city", "type": "single_select",
			"options": map[string]any{
				"mode": "query",
				"query": map[string]any{
					"connector_id": connID,
					"sql":          "SELECT 'draft' AS label, 'draft' AS value",
				},
				"label_column": "label", "value_column": "value",
			},
		}}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var optsResp struct {
		Options []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"options"`
	}
	rec = dashboardVariableOptions(t, srv, token, dashOpts, "city", map[string]any{"published": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&optsResp))
	require.Len(t, optsResp.Options, 1)
	require.Equal(t, "snap", optsResp.Options[0].Value)

	rec = dashboardVariableOptions(t, srv, token, dashOpts, "city", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&optsResp))
	require.Len(t, optsResp.Options, 1)
	require.Equal(t, "draft", optsResp.Options[0].Value)

	// published:true options on a never-published dashboard are a 404.
	dashOptsUnpublished := createDashWithSettings(t, srv, token, map[string]any{
		"variables": []map[string]any{{
			"name": "city", "type": "single_select",
			"options": map[string]any{
				"mode": "query",
				"query": map[string]any{
					"connector_id": connID,
					"sql":          "SELECT 'x' AS label, 'x' AS value",
				},
				"label_column": "label", "value_column": "value",
			},
		}},
	})
	rec = dashboardVariableOptions(t, srv, token, dashOptsUnpublished, "city", map[string]any{"published": true})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// A snapshot widget deleted from the draft still executes in published
	// mode: the snapshot is authoritative, and the draft is never consulted.
	dashDeleted := createDashWithSettings(t, srv, token, nil)
	widgetDeleted := addQueryWidget(t, srv, token, dashDeleted, connID, "SELECT 6 AS x")
	publishVersionsDashboard(t, srv, token, dashDeleted)
	rec = versionsRequest(t, srv, token, "DELETE", "/api/v1/dashboards/"+dashDeleted+"/widgets/"+widgetDeleted, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rec = executeDashboardWidget(t, srv, token, dashDeleted, map[string]any{
		"widget_id": widgetDeleted, "published": true, "bypass_cache": true,
	})
	require.Equal(t, float64(6), firstCellValue(t, rec))
	// The draft no longer has it, so the editor's plain run is a 404.
	rec = executeDashboardWidget(t, srv, token, dashDeleted, map[string]any{"widget_id": widgetDeleted})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestPublicDashboardPublished pins the public read split: once a version
// exists, the public page and its live queries serve the snapshot, and a
// widget that only exists in the draft is a 404.
func TestPublicDashboardPublished(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-pubpub-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash PubPub Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, map[string]any{"public_live": true})
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS x")
	publicToken := shareDashboard(t, srv, token, dashID)

	publishVersionsDashboard(t, srv, token, dashID)

	// Draft edits after the publish: rename, change the widget query, and add
	// a widget.
	rec := versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID, map[string]any{"title": "Renamed"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updateWidgetQuery(t, srv, token, dashID, widgetID, "SELECT 2 AS x", connID)
	widgetLater := addWidgetRaw(t, srv, token, dashID, map[string]any{
		"connector_id": connID,
		"query":        "SELECT 3 AS x",
		"type":         "table",
		"layout":       map[string]int{"row": 0, "col": 6, "width": 6, "height": 6},
	})["id"].(string)

	// The public page serves the snapshot.
	resp := getPublicDashboard(t, srv, publicToken)
	require.Equal(t, "Query Dash", resp["title"])
	publishedVersion, ok := resp["published_version"].(map[string]any)
	require.True(t, ok, "published_version missing: %v", resp)
	require.Equal(t, float64(1), publishedVersion["version"])
	widgets, ok := resp["widgets"].([]any)
	require.True(t, ok)
	require.Len(t, widgets, 1)
	require.Equal(t, widgetID, widgets[0].(map[string]any)["id"])
	require.Equal(t, "SELECT 1 AS x", widgets[0].(map[string]any)["query"])

	// Public execution runs the snapshot's SQL.
	rec = publicDashboardExecute(t, srv, publicToken, map[string]any{"widget_id": widgetID, "bypass_cache": true})
	require.Equal(t, float64(1), firstCellValue(t, rec))

	// A widget that exists only in the draft is not served: 404, no fallback.
	rec = publicDashboardExecute(t, srv, publicToken, map[string]any{"widget_id": widgetLater})
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// The published snapshot governs public_live: toggling the draft OFF does
	// not disable live queries until a new version is published.
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID,
		map[string]any{"settings": map[string]any{"public_live": false}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = publicDashboardExecute(t, srv, publicToken, map[string]any{"widget_id": widgetID, "bypass_cache": true})
	require.Equal(t, float64(1), firstCellValue(t, rec), "the v1 snapshot still enables live queries")

	// Publishing the OFF toggle takes effect.
	v2, _, _ := publishVersionsDashboard(t, srv, token, dashID)
	require.Equal(t, 2, v2)
	rec = publicDashboardExecute(t, srv, publicToken, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// An unpublished draft toggle ON cannot enable live queries either: the
	// published snapshot says OFF until the toggle is published.
	dashOff := createDashWithSettings(t, srv, token, nil)
	widgetOff := addQueryWidget(t, srv, token, dashOff, connID, "SELECT 5 AS x")
	publicTokenOff := shareDashboard(t, srv, token, dashOff)
	publishVersionsDashboard(t, srv, token, dashOff)
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashOff,
		map[string]any{"settings": map[string]any{"public_live": true}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = publicDashboardExecute(t, srv, publicTokenOff, map[string]any{"widget_id": widgetOff})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// Public options resolve the variable from the snapshot too: the draft's
	// SQL change must not leak, and the snapshot's public_live gates the run.
	dashOpts := createDashWithSettings(t, srv, token, map[string]any{
		"public_live": true,
		"variables": []map[string]any{{
			"name": "city", "type": "single_select",
			"options": map[string]any{
				"mode": "query",
				"query": map[string]any{
					"connector_id": connID,
					"sql":          "SELECT 'snap' AS label, 'snap' AS value",
				},
				"label_column": "label", "value_column": "value",
			},
		}},
	})
	publicTokenOpts := shareDashboard(t, srv, token, dashOpts)
	publishVersionsDashboard(t, srv, token, dashOpts)
	rec = versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashOpts, map[string]any{
		"settings": map[string]any{"variables": []map[string]any{{
			"name": "city", "type": "single_select",
			"options": map[string]any{
				"mode": "query",
				"query": map[string]any{
					"connector_id": connID,
					"sql":          "SELECT 'draft' AS label, 'draft' AS value",
				},
				"label_column": "label", "value_column": "value",
			},
		}}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = publicDashboardOptions(t, srv, publicTokenOpts, "city", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var publicOpts struct {
		Options []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"options"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&publicOpts))
	require.Len(t, publicOpts.Options, 1)
	require.Equal(t, "snap", publicOpts.Options[0].Value)

	// A corrupt snapshot is a load failure (500), never a 400 "not a query
	// widget": content problems and infrastructure problems stay distinct.
	dashCorrupt := createDashWithSettings(t, srv, token, map[string]any{"public_live": true})
	widgetCorrupt := addQueryWidget(t, srv, token, dashCorrupt, connID, "SELECT 7 AS x")
	publicTokenCorrupt := shareDashboard(t, srv, token, dashCorrupt)
	publishVersionsDashboard(t, srv, token, dashCorrupt)
	_, err := db.Pool.Exec(ctx,
		`UPDATE dashboard_versions SET settings = '{"variables": "bogus"}'::jsonb WHERE dashboard_id = $1`,
		dashCorrupt)
	require.NoError(t, err)
	rec = publicDashboardExecute(t, srv, publicTokenCorrupt, map[string]any{"widget_id": widgetCorrupt})
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestDashboardDocAclPublished pins the draft-doc ACL change: once a
// dashboard has a published version, the live document is editor-only for
// both the relay's GET/PUT and the collab authorize endpoint, while a
// never-published dashboard keeps the #245 viewer behavior (read-only).
func TestDashboardDocAclPublished(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	ownerToken := registerAndGetToken(t, srv, fmt.Sprintf("dash-docacl-owner-%d@example.com", ts), "Dash DocACL Org")
	dashID := createDashWithSettings(t, srv, ownerToken, nil)
	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))

	viewerID := insertUser(t, srv, fmt.Sprintf("dash-docacl-viewer-%d@example.com", ts), "DocACL Viewer")
	addOrgMember(t, srv, orgID, viewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "non-admin")

	editorID := insertUser(t, srv, fmt.Sprintf("dash-docacl-editor-%d@example.com", ts), "DocACL Editor")
	addOrgMember(t, srv, orgID, editorID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", editorID, "edit")
	editorToken := issueToken(t, editorID, orgID, "non-admin")

	// Before any publish the #245 behavior stands: a viewer loads the draft
	// read-only through the relay and authorize says can_edit=false.
	rec := internalDashboardDocRequest(t, srv, viewerToken, "GET", dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = collabAuthorize(t, srv, viewerToken, "dashboard:"+dashID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, false, decodeCollabAuthorize(t, rec)["can_edit"])

	// Publish v1.
	publishVersionsDashboard(t, srv, ownerToken, dashID)

	// The viewer loses both draft reads and relay authorization.
	rec = internalDashboardDocRequest(t, srv, viewerToken, "GET", dashID, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	rec = collabAuthorize(t, srv, viewerToken, "dashboard:"+dashID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// A viewer's idempotent store is refused too: the draft is editor-only.
	rec = internalDashboardDocRequest(t, srv, viewerToken, "PUT", dashID, dashboardDocState(t, srv, dashID))
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// The editor keeps the draft, read-write.
	rec = internalDashboardDocRequest(t, srv, editorToken, "GET", dashID, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = collabAuthorize(t, srv, editorToken, "dashboard:"+dashID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, true, decodeCollabAuthorize(t, rec)["can_edit"])
}

// TestDashboardGetPublishedWidgetsData pins that widgets_data is computed from
// the substituted widget list: a view_with_data viewer's published read loads
// cell data for the snapshot's cell widget only, while the editor's draft read
// also loads the widget added after the publish.
func TestDashboardGetPublishedWidgetsData(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-getpub-data-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash GetPub Data Org")
	connID := createConnector(t, srv, token)

	nbID := createNotebook(t, srv, token, "GetPub Data NB")
	cellA := createCell(t, srv, token, nbID, "sql", "SELECT 1 AS a", connID)

	dashID := createDashWithSettings(t, srv, token, nil)
	addWidgetRaw(t, srv, token, dashID, map[string]any{
		"notebook_id": nbID,
		"cell_id":     cellA,
		"type":        "table",
		"layout":      map[string]int{"row": 0, "col": 0, "width": 6, "height": 6},
	})

	publishVersionsDashboard(t, srv, token, dashID)

	// A second cell widget added after the publish is draft-only.
	cellB := createCell(t, srv, token, nbID, "sql", "SELECT 2 AS b", connID)
	addWidgetRaw(t, srv, token, dashID, map[string]any{
		"notebook_id": nbID,
		"cell_id":     cellB,
		"type":        "table",
		"layout":      map[string]int{"row": 0, "col": 6, "width": 6, "height": 6},
	})

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-getpub-data-viewer-%d@example.com", ts), "GetPub Data Viewer")
	addOrgMember(t, srv, orgID, viewerID, "non-admin")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view", "view_with_data")
	viewerToken := issueToken(t, viewerID, orgID, "non-admin")

	// widgetsDataKeys returns the sorted cell IDs the response embeds.
	widgetsDataKeys := func(rec *httptest.ResponseRecorder) []string {
		t.Helper()
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp struct {
			WidgetsData map[string]any `json:"widgets_data"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		keys := make([]string, 0, len(resp.WidgetsData))
		for id := range resp.WidgetsData {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		return keys
	}

	// The viewer's explicit published read loads cell data for the snapshot
	// widget only.
	rec := versionsRequest(t, srv, viewerToken, "GET", "/api/v1/dashboards/"+dashID+"?published=1", nil)
	require.ElementsMatch(t, []string{cellA}, widgetsDataKeys(rec))

	// The viewer's plain read is published too (no edit permission): same key
	// set.
	rec = versionsRequest(t, srv, viewerToken, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.ElementsMatch(t, []string{cellA}, widgetsDataKeys(rec))

	// The editor's draft read includes both cell widgets.
	rec = versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID, nil)
	require.ElementsMatch(t, []string{cellA, cellB}, widgetsDataKeys(rec))
}

// TestDashboardListServesPublishedContentToViewers pins the list read for a
// published dashboard: callers without `edit` see the latest snapshot's title
// and settings, never the editor's draft-only edits; the editor keeps seeing
// the draft.
func TestDashboardListServesPublishedContentToViewers(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("dash-list-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Dash List Org")
	dashID := createDashWithSettings(t, srv, token, map[string]any{"auto_refresh_seconds": 30})

	// Publish the initial draft, then edit title + settings in the draft only.
	publishVersionsDashboard(t, srv, token, dashID)
	rec := versionsRequest(t, srv, token, "PUT", "/api/v1/dashboards/"+dashID,
		map[string]any{"title": "Draft Renamed", "settings": map[string]any{"auto_refresh_seconds": 60}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM dashboards WHERE id = $1`, dashID).Scan(&orgID))
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-list-viewer-%d@example.com", time.Now().UnixNano()), "List Viewer")
	addOrgMember(t, srv, orgID, viewerID, "editor")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "editor")

	find := func(t *testing.T, asToken string) map[string]any {
		t.Helper()
		rec := versionsRequest(t, srv, asToken, "GET", "/api/v1/dashboards", nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var list []map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
		for _, d := range list {
			if d["id"] == dashID {
				return d
			}
		}
		t.Fatalf("dashboard %s missing from the list", dashID)
		return nil
	}

	// The editor sees the draft title and draft settings.
	editorEntry := find(t, token)
	require.Equal(t, "Draft Renamed", editorEntry["title"])
	editorSettings, ok := editorEntry["settings"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 60, editorSettings["auto_refresh_seconds"])

	// The viewer sees the published snapshot only.
	viewerEntry := find(t, viewerToken)
	require.Equal(t, "Query Dash", viewerEntry["title"], "the draft title must not leak")
	viewerSettings, ok := viewerEntry["settings"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 30, viewerSettings["auto_refresh_seconds"], "the draft settings must not leak")
}
