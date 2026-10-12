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
