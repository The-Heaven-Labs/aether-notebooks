package api_test

// HTTP-driven coverage for GET /api/v1/search. The leak/visibility suite is
// the point: a viewer must never match or receive an unpublished dashboard
// title, must never match draft widget SQL, and no result may bypass ACLs.
// Everything runs against the real database; resources are seeded with SQL
// and ACLs are written explicitly so each caller's view is unambiguous.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// searchTestFixture is one org with three members: the creator (admin role,
// never used with admin mode), a member used as the various "caller" roles,
// and a stranger with no grants. Direct-SQL seeding keeps tests fast; every
// permission is an explicit ACL entry.
type searchTestFixture struct {
	srv           *api.Server
	token         string
	orgID         string
	userID        string
	viewerID      string
	viewerToken   string
	strangerID    string
	strangerToken string
}

func setupSearchTestFixture(t *testing.T, srv *api.Server) *searchTestFixture {
	t.Helper()
	now := time.Now().UnixNano()

	token := registerAndGetToken(t, srv, fmt.Sprintf("search-%d@test.com", now), "Search Org")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)

	viewerID := insertUser(t, srv, fmt.Sprintf("search-viewer-%d@test.com", now), "Search Viewer")
	addOrgMember(t, srv, orgID, viewerID, "editor")
	strangerID := insertUser(t, srv, fmt.Sprintf("search-stranger-%d@test.com", now), "Search Stranger")
	addOrgMember(t, srv, orgID, strangerID, "editor")

	return &searchTestFixture{
		srv:           srv,
		token:         token,
		orgID:         orgID,
		userID:        userID,
		viewerID:      viewerID,
		viewerToken:   issueToken(t, viewerID, orgID, "editor"),
		strangerID:    strangerID,
		strangerToken: issueToken(t, strangerID, orgID, "editor"),
	}
}

func (f *searchTestFixture) seedNotebook(t *testing.T, title string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO notebooks (id, org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4, $5)`,
		id, f.orgID, title, ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) setNotebookDescription(t *testing.T, id, description string) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`UPDATE notebooks SET description = $2 WHERE id = $1`, id, description)
	require.NoError(t, err)
}

func (f *searchTestFixture) seedCell(t *testing.T, notebookID, source string, position int) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO cells (notebook_id, position, type, language, source) VALUES ($1, $2, 'code', 'sql', $3)`,
		notebookID, position, source)
	require.NoError(t, err)
}

func (f *searchTestFixture) seedFolder(t *testing.T, name string, parentID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO folders (id, org_id, parent_id, name, owner_id, created_by) VALUES ($1, $2, $3, $4, $5, $5)`,
		id, f.orgID, parentID, name, ownerID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedConnector(t *testing.T, name string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, folder_id)
		 VALUES ($1, $2, $3, 'postgres', $4, $5, $6)`,
		id, f.orgID, name, []byte("x"), ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedDashboard(t *testing.T, title string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO dashboards (id, org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4, $5)`,
		id, f.orgID, title, ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedWidget(t *testing.T, dashboardID, connectorID, query string) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO widgets (dashboard_id, type, layout, config, connector_id, query)
		 VALUES ($1, 'table', '{}', '{}', $2, $3)`,
		dashboardID, connectorID, query)
	require.NoError(t, err)
}

// publishDashboard writes a latest published snapshot directly, like the
// folder-listing tests do: the snapshot is exactly a dashboard_versions row.
func (f *searchTestFixture) publishDashboard(t *testing.T, dashboardID, snapshotTitle string, version int, ownerID string) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO dashboard_versions (dashboard_id, org_id, version, title, settings, widgets, content_hash, created_by)
		 VALUES ($1, $2, $3, $4, '{}', '[]', $5, $6)`,
		dashboardID, f.orgID, version, snapshotTitle, fmt.Sprintf("hash-%s-%d", dashboardID, version), ownerID)
	require.NoError(t, err)
}

func (f *searchTestFixture) renameDashboard(t *testing.T, dashboardID, title string) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`UPDATE dashboards SET title = $2 WHERE id = $1`, dashboardID, title)
	require.NoError(t, err)
}

func (f *searchTestFixture) seedAgent(t *testing.T, name, description string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO agents (id, org_id, name, description, created_by, folder_id) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, f.orgID, name, description, ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedModelConfig(t *testing.T, name string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, created_by, folder_id)
		 VALUES ($1, $2, $3, 'openai', 'http://localhost:9', 'test-model', $4, $5, $6)`,
		id, f.orgID, name, []byte("x"), ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedSkill(t *testing.T, name, description string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO skills (id, org_id, name, description, created_by, folder_id) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, f.orgID, name, description, ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedTool(t *testing.T, name, description string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO tools (id, org_id, name, description, type, created_by, folder_id)
		 VALUES ($1, $2, $3, $4, 'builtin', $5, $6)`,
		id, f.orgID, name, description, ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *searchTestFixture) seedMCPServer(t *testing.T, name string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO mcp_servers (id, org_id, name, type, command, created_by, folder_id)
		 VALUES ($1, $2, $3, 'stdio', 'echo', $4, $5)`,
		id, f.orgID, name, ownerID, folderID)
	require.NoError(t, err)
	return id
}

// grant writes one user ACL entry for an arbitrary org member.
func (f *searchTestFixture) grant(t *testing.T, resourceType, resourceID, userID string, actions []string) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, $2, $3::uuid, 'user', $4, $5)`,
		f.orgID, resourceType, resourceID, userID, actions)
	require.NoError(t, err)
}

// setNotebookUpdatedAt backdates a notebook for deterministic ranking.
func (f *searchTestFixture) setNotebookUpdatedAt(t *testing.T, id string, at time.Time) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(), `UPDATE notebooks SET updated_at = $2 WHERE id = $1`, id, at)
	require.NoError(t, err)
}

// setFolderUpdatedAt backdates a folder for deterministic ranking.
func (f *searchTestFixture) setFolderUpdatedAt(t *testing.T, id string, at time.Time) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(), `UPDATE folders SET updated_at = $2 WHERE id = $1`, id, at)
	require.NoError(t, err)
}

type searchResultBody struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Subtitle   string  `json:"subtitle"`
	Snippet    *string `json:"snippet"`
	FolderPath string  `json:"folder_path"`
	IsFavorite bool    `json:"is_favorite"`
	CanEdit    bool    `json:"can_edit"`
}

type searchGroupBody struct {
	ResourceType string             `json:"resource_type"`
	HasMore      bool               `json:"has_more"`
	Results      []searchResultBody `json:"results"`
}

type searchResponseBody struct {
	Query  string            `json:"query"`
	Groups []searchGroupBody `json:"groups"`
}

// searchRaw performs an authenticated GET /api/v1/search with a raw query
// string (already URL-safe).
func (f *searchTestFixture) searchRaw(t *testing.T, token, rawQuery string) (int, searchResponseBody) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/search?"+rawQuery, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	var body searchResponseBody
	if rec.Body.Len() > 0 {
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	}
	return rec.Code, body
}

func (f *searchTestFixture) search(t *testing.T, token, q string) (int, searchResponseBody) {
	t.Helper()
	return f.searchRaw(t, token, "q="+url.QueryEscape(q))
}

// requireGroup returns the named group, failing when it is absent or empty.
func requireSearchGroup(t *testing.T, resp searchResponseBody, resourceType string) searchGroupBody {
	t.Helper()
	for _, group := range resp.Groups {
		if group.ResourceType == resourceType {
			require.NotEmpty(t, group.Results, "group %s must not be empty", resourceType)
			return group
		}
	}
	require.Failf(t, "group missing", "expected group %s in %v", resourceType, searchGroupTypes(resp))
	return searchGroupBody{}
}

// searchGroupTypes lists the response group types in order.
func searchGroupTypes(resp searchResponseBody) []string {
	types := make([]string, 0, len(resp.Groups))
	for _, group := range resp.Groups {
		types = append(types, group.ResourceType)
	}
	return types
}

func searchResultTitles(results []searchResultBody) []string {
	titles := make([]string, 0, len(results))
	for _, result := range results {
		titles = append(titles, result.Title)
	}
	return titles
}

func searchResultIDs(results []searchResultBody) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}

func TestSearchTitleSubstringAcrossTypes(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	notebookID := f.seedNotebook(t, "Revenue by region", nil, f.userID)
	describedNotebookID := f.seedNotebook(t, "Forecast", nil, f.userID)
	f.setNotebookDescription(t, describedNotebookID, "Revenue growth by quarter")
	folderID := f.seedFolder(t, "Revenue docs", nil, f.userID)
	connectorID := f.seedConnector(t, "revenue warehouse", nil, f.userID)
	agentID := f.seedAgent(t, "revenue agent", "", nil, f.userID)
	modelID := f.seedModelConfig(t, "revenue model", nil, f.userID)
	skillID := f.seedSkill(t, "Skill alpha", "Revenue forecasting helper", nil, f.userID)
	toolID := f.seedTool(t, "Tool alpha", "Runs revenue queries", nil, f.userID)
	mcpID := f.seedMCPServer(t, "Revenue MCP", nil, f.userID)

	for _, resource := range [][2]string{
		{"notebook", notebookID}, {"notebook", describedNotebookID}, {"folder", folderID},
		{"connector", connectorID}, {"agent", agentID}, {"model_config", modelID},
		{"skill", skillID}, {"tool", toolID}, {"mcp_server", mcpID},
	} {
		f.grant(t, resource[0], resource[1], f.viewerID, []string{"view"})
	}

	status, resp := f.search(t, f.viewerToken, "revenue")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "revenue", resp.Query)
	require.Equal(t,
		[]string{"notebook", "folder", "connector", "agent", "model_config", "skill", "tool", "mcp_server"},
		searchGroupTypes(resp), "groups must follow the fixed order, omitting empty types")

	notebooks := requireSearchGroup(t, resp, "notebook")
	require.ElementsMatch(t, []string{"Revenue by region", "Forecast"}, searchResultTitles(notebooks.Results),
		"title and description matches both count")
	require.False(t, notebooks.HasMore)

	folder := requireSearchGroup(t, resp, "folder")
	require.Equal(t, []string{"Revenue docs"}, searchResultTitles(folder.Results))
	require.Equal(t, "Folder", folder.Results[0].Subtitle)

	connector := requireSearchGroup(t, resp, "connector")
	require.Equal(t, []string{"revenue warehouse"}, searchResultTitles(connector.Results))
	require.Equal(t, "postgres", connector.Results[0].Subtitle, "connectors subtitle on their type")

	require.Equal(t, []string{"revenue agent"}, searchResultTitles(requireSearchGroup(t, resp, "agent").Results))
	require.Equal(t, []string{"revenue model"}, searchResultTitles(requireSearchGroup(t, resp, "model_config").Results))
	require.Equal(t, []string{"Skill alpha"}, searchResultTitles(requireSearchGroup(t, resp, "skill").Results))
	require.Equal(t, []string{"Tool alpha"}, searchResultTitles(requireSearchGroup(t, resp, "tool").Results))
	require.Equal(t, []string{"Revenue MCP"}, searchResultTitles(requireSearchGroup(t, resp, "mcp_server").Results))

	// Case-insensitive: the same catalog comes back for an upper-case query.
	status, upper := f.search(t, f.viewerToken, "REVENUE")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, searchGroupTypes(resp), searchGroupTypes(upper))

	// A stranger with no grants sees nothing, not even group shells.
	status, stranger := f.search(t, f.strangerToken, "revenue")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, stranger.Groups)
}

func TestSearchNotebookContentVisibleToViewers(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	notebookID := f.seedNotebook(t, "Payments analysis", nil, f.userID)
	f.seedCell(t, notebookID, "SELECT sum(amount) AS payments_total FROM orders", 0)
	f.grant(t, "notebook", notebookID, f.viewerID, []string{"view"})
	f.grant(t, "notebook", notebookID, f.userID, []string{"view", "edit"})

	// Content-only match: the query tokens prefix-match the cell (sum, am →
	// amount). A viewer finds it and gets a snippet.
	status, resp := f.search(t, f.viewerToken, "sum am")
	require.Equal(t, http.StatusOK, status)
	notebooks := requireSearchGroup(t, resp, "notebook")
	require.Len(t, notebooks.Results, 1)
	result := notebooks.Results[0]
	require.Equal(t, notebookID, result.ID)
	require.Equal(t, "Payments analysis", result.Title, "the title comes from the row, not the snippet")
	require.False(t, result.CanEdit, "a view-only caller must report can_edit false")
	require.NotNil(t, result.Snippet)
	require.Contains(t, strings.ToLower(*result.Snippet), "amount")

	// A title match that is also a content match merges into one result and
	// keeps the content snippet.
	status, resp = f.search(t, f.viewerToken, "payments")
	require.Equal(t, http.StatusOK, status)
	notebooks = requireSearchGroup(t, resp, "notebook")
	require.Len(t, notebooks.Results, 1, "title and content matches must merge on the resource id")
	require.Equal(t, notebookID, notebooks.Results[0].ID)
	require.NotNil(t, notebooks.Results[0].Snippet, "a merged title+content hit still carries the snippet")

	// The editor's own view reports can_edit.
	status, resp = f.search(t, f.token, "sum am")
	require.Equal(t, http.StatusOK, status)
	require.True(t, requireSearchGroup(t, resp, "notebook").Results[0].CanEdit)
}

// TestSearchDashboardDraftTitleLeak pins Leak 1: after a publish, a draft
// rename must never make the dashboard findable by its draft-only title for a
// viewer, while the editor (who sees the draft) still finds it.
func TestSearchDashboardDraftTitleLeak(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	dashboardID := f.seedDashboard(t, "Published alpha", nil, f.userID)
	f.grant(t, "dashboard", dashboardID, f.viewerID, []string{"view"})
	f.grant(t, "dashboard", dashboardID, f.userID, []string{"view", "edit"})
	f.publishDashboard(t, dashboardID, "Published alpha", 1, f.userID)
	f.renameDashboard(t, dashboardID, "Draft-only secret")

	// Viewer: the draft-only title is invisible.
	status, resp := f.search(t, f.viewerToken, "draft-only secret")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, searchGroupTypes(resp), "a viewer must not match an unpublished draft title")

	// Viewer: the published title still finds the dashboard, and the returned
	// title is the snapshot title.
	status, resp = f.search(t, f.viewerToken, "published alpha")
	require.Equal(t, http.StatusOK, status)
	dashboards := requireSearchGroup(t, resp, "dashboard")
	require.Len(t, dashboards.Results, 1)
	require.Equal(t, dashboardID, dashboards.Results[0].ID)
	require.Equal(t, "Published alpha", dashboards.Results[0].Title)
	require.False(t, dashboards.Results[0].CanEdit)

	// Editor: the draft title matches and comes back as the draft.
	status, resp = f.search(t, f.token, "draft-only secret")
	require.Equal(t, http.StatusOK, status)
	dashboards = requireSearchGroup(t, resp, "dashboard")
	require.Len(t, dashboards.Results, 1)
	require.Equal(t, "Draft-only secret", dashboards.Results[0].Title)
	require.True(t, dashboards.Results[0].CanEdit)
}

// TestSearchDashboardEffectiveTitles pins Leak 3: never-published dashboards
// stay findable by their draft title, and a published dashboard is served
// under the caller's effective title (snapshot for viewers, draft for
// editors).
func TestSearchDashboardEffectiveTitles(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	neverPublished := f.seedDashboard(t, "Draft dashboard beta", nil, f.userID)
	f.grant(t, "dashboard", neverPublished, f.viewerID, []string{"view"})

	published := f.seedDashboard(t, "Published alpha (edited draft)", nil, f.userID)
	f.grant(t, "dashboard", published, f.viewerID, []string{"view"})
	f.grant(t, "dashboard", published, f.userID, []string{"view", "edit"})
	f.publishDashboard(t, published, "Published alpha", 1, f.userID)

	// Never published: a viewer finds it by the draft title and receives it.
	status, resp := f.search(t, f.viewerToken, "draft dashboard beta")
	require.Equal(t, http.StatusOK, status)
	dashboards := requireSearchGroup(t, resp, "dashboard")
	require.Len(t, dashboards.Results, 1)
	require.Equal(t, neverPublished, dashboards.Results[0].ID)
	require.Equal(t, "Draft dashboard beta", dashboards.Results[0].Title)

	// Published, searched by the snapshot title: the viewer gets the snapshot
	// title, never the edited draft.
	status, resp = f.search(t, f.viewerToken, "published alpha")
	require.Equal(t, http.StatusOK, status)
	dashboards = requireSearchGroup(t, resp, "dashboard")
	require.Len(t, dashboards.Results, 1)
	require.Equal(t, published, dashboards.Results[0].ID)
	require.Equal(t, "Published alpha", dashboards.Results[0].Title)
	require.NotContains(t, searchResultTitles(dashboards.Results), "Published alpha (edited draft)")

	// The same query as an editor returns the draft title.
	status, resp = f.search(t, f.token, "published alpha")
	require.Equal(t, http.StatusOK, status)
	dashboards = requireSearchGroup(t, resp, "dashboard")
	require.Len(t, dashboards.Results, 1)
	require.Equal(t, published, dashboards.Results[0].ID)
	require.Equal(t, "Published alpha (edited draft)", dashboards.Results[0].Title)
	require.True(t, dashboards.Results[0].CanEdit)
}

// TestSearchDashboardContentEditorsOnly pins Leak 2: draft widget SQL is
// editor-only, in both directions (the viewer sees nothing for the SQL term
// but still sees the dashboard by title).
func TestSearchDashboardContentEditorsOnly(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	dashboardID := f.seedDashboard(t, "Metrics dashboard", nil, f.userID)
	connectorID := f.seedConnector(t, "Metrics connector", nil, f.userID)
	f.seedWidget(t, dashboardID, connectorID, "SELECT secret_metric FROM ledger")
	f.grant(t, "dashboard", dashboardID, f.viewerID, []string{"view"})
	f.grant(t, "dashboard", dashboardID, f.userID, []string{"view", "edit"})

	// Viewer: no content hit for draft widget SQL.
	status, resp := f.search(t, f.viewerToken, "secret_metric")
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, searchGroupTypes(resp), "dashboard",
		"a viewer must never match draft widget SQL")

	// Editor: the content hit arrives with a snippet and the draft title.
	status, resp = f.search(t, f.token, "secret_metric")
	require.Equal(t, http.StatusOK, status)
	dashboards := requireSearchGroup(t, resp, "dashboard")
	require.Len(t, dashboards.Results, 1)
	require.Equal(t, dashboardID, dashboards.Results[0].ID)
	require.Equal(t, "Metrics dashboard", dashboards.Results[0].Title)
	require.True(t, dashboards.Results[0].CanEdit)
	require.NotNil(t, dashboards.Results[0].Snippet)
	require.Contains(t, strings.ToLower(*dashboards.Results[0].Snippet), "secret_metric")

	// The viewer still finds the dashboard by title.
	status, resp = f.search(t, f.viewerToken, "metrics")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, requireSearchGroup(t, resp, "dashboard").Results, 1)
}

// TestSearchVisibility covers stranger, revoked, group-granted,
// folder-inherited, and content-visibility filtering.
func TestSearchVisibility(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)
	ctx := context.Background()

	notebookID := f.seedNotebook(t, "Guardian notebook", nil, f.userID)
	f.grant(t, "notebook", notebookID, f.viewerID, []string{"view"})

	// A stranger sees nothing.
	status, resp := f.search(t, f.strangerToken, "guardian")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, resp.Groups)

	// The viewer sees it read-only; revoking the grant hides it immediately.
	status, resp = f.search(t, f.viewerToken, "guardian notebook")
	require.Equal(t, http.StatusOK, status)
	require.False(t, requireSearchGroup(t, resp, "notebook").Results[0].CanEdit)
	_, err := f.srv.DB().Pool.Exec(ctx,
		`DELETE FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = $1::uuid AND subject_id = $2`,
		notebookID, f.viewerID)
	require.NoError(t, err)
	status, resp = f.search(t, f.viewerToken, "guardian notebook")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, resp.Groups)

	// Group grant: a group ACL entry makes the notebook visible to its member.
	groupID := createGroup(t, f.srv, f.token, fmt.Sprintf("searchers-%d", time.Now().UnixNano()))
	addGroupMember(t, f.srv, f.token, groupID, f.strangerID)
	groupNotebook := f.seedNotebook(t, "Group guardian notebook", nil, f.userID)
	_, err = f.srv.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'group', $3, ARRAY['view'])`,
		f.orgID, groupNotebook, groupID)
	require.NoError(t, err)
	status, resp = f.search(t, f.strangerToken, "group guardian notebook")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{groupNotebook}, searchResultIDs(requireSearchGroup(t, resp, "notebook").Results))

	// Folder-inherited grant: a view grant on the containing folder reaches
	// the notebook inside it.
	folderID := f.seedFolder(t, "Guardian home", nil, f.userID)
	innerNotebook := f.seedNotebook(t, "Folder guardian notebook", &folderID, f.userID)
	f.grant(t, "folder", folderID, f.viewerID, []string{"view"})
	status, resp = f.search(t, f.viewerToken, "folder guardian notebook")
	require.Equal(t, http.StatusOK, status)
	notebooks := requireSearchGroup(t, resp, "notebook")
	require.Equal(t, []string{innerNotebook}, searchResultIDs(notebooks.Results))
	require.Equal(t, "Guardian home", notebooks.Results[0].FolderPath)

	// Content visibility: an ungranted notebook's cell SQL never surfaces,
	// while the granted one does.
	hiddenContent := f.seedNotebook(t, "Hidden content notebook", nil, f.userID)
	f.seedCell(t, hiddenContent, "SELECT forbidden_term FROM secrets", 0)
	visibleContent := f.seedNotebook(t, "Visible content notebook", nil, f.userID)
	f.seedCell(t, visibleContent, "SELECT allowed_term FROM public_data", 0)
	f.grant(t, "notebook", visibleContent, f.viewerID, []string{"view"})

	status, resp = f.search(t, f.viewerToken, "forbidden_term")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, resp.Groups, "content search must not bypass notebook view")
	status, resp = f.search(t, f.viewerToken, "allowed_term")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{visibleContent}, searchResultIDs(requireSearchGroup(t, resp, "notebook").Results))
}

func TestSearchFavoritesFilterAndFlags(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	first := f.seedNotebook(t, "Alpha one", nil, f.userID)
	second := f.seedNotebook(t, "Alpha two", nil, f.userID)
	third := f.seedNotebook(t, "Alpha three", nil, f.userID)
	unrelated := f.seedNotebook(t, "Omega report", nil, f.userID)
	for _, id := range []string{first, second, third, unrelated} {
		f.grant(t, "notebook", id, f.viewerID, []string{"view"})
		f.grant(t, "notebook", id, f.userID, []string{"view", "edit"})
	}

	// Favorite one matching notebook and one non-matching notebook.
	for _, id := range []string{second, unrelated} {
		code, _ := doRequest(t, srv, f.viewerToken, "PUT", favoritePath("notebook", id), nil)
		require.Equal(t, http.StatusNoContent, code)
	}

	status, resp := f.search(t, f.viewerToken, "alpha")
	require.Equal(t, http.StatusOK, status)
	notebooks := requireSearchGroup(t, resp, "notebook")
	require.Len(t, notebooks.Results, 3)
	favoriteFlags := map[string]bool{}
	for _, result := range notebooks.Results {
		favoriteFlags[result.ID] = result.IsFavorite
	}
	require.True(t, favoriteFlags[second], "is_favorite must reflect the caller's favorites")
	require.False(t, favoriteFlags[first])
	require.False(t, favoriteFlags[third])
	for _, result := range notebooks.Results {
		require.False(t, result.CanEdit, "the view-only viewer must not report can_edit")
	}

	// favorites=1 intersects before ranking and the limit: only the favorited
	// matching notebook survives.
	status, favoriteResp := f.searchRaw(t, f.viewerToken, "q=alpha&favorites=1")
	require.Equal(t, http.StatusOK, status)
	favoriteNotebooks := requireSearchGroup(t, favoriteResp, "notebook")
	require.Equal(t, []string{second}, searchResultIDs(favoriteNotebooks.Results))
	require.True(t, favoriteNotebooks.Results[0].IsFavorite)

	// favorites=1 with nothing favorited for a type omits the group entirely.
	status, none := f.searchRaw(t, f.strangerToken, "q=alpha&favorites=1")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, none.Groups)

	// The editor's own search reports can_edit.
	status, resp = f.search(t, f.token, "alpha")
	require.Equal(t, http.StatusOK, status)
	require.True(t, requireSearchGroup(t, resp, "notebook").Results[0].CanEdit)
}

func TestSearchValidation(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	// Missing and blank q.
	status, _ := f.searchRaw(t, f.token, "")
	require.Equal(t, http.StatusBadRequest, status)
	status, _ = f.searchRaw(t, f.token, "q="+url.QueryEscape("   "))
	require.Equal(t, http.StatusBadRequest, status)

	// q bounds: 200 runes accepted, 201 rejected.
	status, _ = f.search(t, f.token, strings.Repeat("a", 200))
	require.Equal(t, http.StatusOK, status)
	status, _ = f.search(t, f.token, strings.Repeat("a", 201))
	require.Equal(t, http.StatusBadRequest, status)

	// Invalid types.
	status, _ = f.searchRaw(t, f.token, "q=x&types=bogus")
	require.Equal(t, http.StatusBadRequest, status)
	status, _ = f.searchRaw(t, f.token, "q=x&types=notebook,bogus")
	require.Equal(t, http.StatusBadRequest, status)
	status, _ = f.searchRaw(t, f.token, "q=x&types=notebook,,folder")
	require.Equal(t, http.StatusBadRequest, status)

	// Invalid limits (non-numeric, zero, negative).
	for _, raw := range []string{"q=x&limit=abc", "q=x&limit=0", "q=x&limit=-5"} {
		status, _ = f.searchRaw(t, f.token, raw)
		require.Equal(t, http.StatusBadRequest, status, "raw query %q", raw)
	}

	// types limits the response groups to exactly the requested ones.
	f.grant(t, "notebook", f.seedNotebook(t, "Tally notebook", nil, f.userID), f.userID, []string{"view"})
	f.grant(t, "connector", f.seedConnector(t, "Tally connector", nil, f.userID), f.userID, []string{"view"})
	status, resp := f.searchRaw(t, f.token, "q=tally&types=notebook")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"notebook"}, searchGroupTypes(resp))
	status, resp = f.searchRaw(t, f.token, "q=tally&types=connector,notebook")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"notebook", "connector"}, searchGroupTypes(resp), "groups stay in the fixed order")

	// limit clamps: 25 visible matches with limit=999 (and limit=25) return 20
	// with has_more, and a small limit pages truthfully.
	for i := 0; i < 25; i++ {
		id := f.seedNotebook(t, fmt.Sprintf("Clamp notebook %02d", i), nil, f.userID)
		f.grant(t, "notebook", id, f.userID, []string{"view"})
	}
	status, resp = f.searchRaw(t, f.token, "q=clamp&limit=999")
	require.Equal(t, http.StatusOK, status)
	clamped := requireSearchGroup(t, resp, "notebook")
	require.Len(t, clamped.Results, 20)
	require.True(t, clamped.HasMore)
	status, resp = f.searchRaw(t, f.token, "q=clamp&limit=1")
	require.Equal(t, http.StatusOK, status)
	require.Len(t, requireSearchGroup(t, resp, "notebook").Results, 1)
	require.True(t, requireSearchGroup(t, resp, "notebook").HasMore)
}

func TestSearchInjectionSafety(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	percent := f.seedNotebook(t, "100% coverage report", nil, f.userID)
	underscore := f.seedNotebook(t, "under_score notes", nil, f.userID)
	plain := f.seedNotebook(t, "plain notes", nil, f.userID)
	for _, id := range []string{percent, underscore, plain} {
		f.grant(t, "notebook", id, f.userID, []string{"view"})
	}

	// % must match literally, not as a wildcard.
	status, resp := f.search(t, f.token, "%")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"100% coverage report"}, searchResultTitles(requireSearchGroup(t, resp, "notebook").Results),
		"% must not act as a wildcard")

	// _ must match literally, not as a single-character wildcard.
	status, resp = f.search(t, f.token, "_")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"under_score notes"}, searchResultTitles(requireSearchGroup(t, resp, "notebook").Results),
		"_ must not act as a wildcard")

	// Quote, ampersand, tsquery punctuation, and backslash never error and
	// never over-match a notebook (the org's seeded builtin tools legitimately
	// carry apostrophes in descriptions, so this probe is type-scoped).
	for _, q := range []string{"'", "&", ":*", `\`, "'; DROP TABLE cells; --", "(("} {
		status, resp = f.searchRaw(t, f.token, "q="+url.QueryEscape(q)+"&types=notebook")
		require.Equal(t, http.StatusOK, status, "query %q must never error", q)
		require.Empty(t, resp.Groups, "query %q must not over-match", q)
	}
}

func TestSearchFolderPaths(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	rootID := f.seedFolder(t, "Path root A", nil, f.userID)
	childID := f.seedFolder(t, "Path child B", &rootID, f.userID)
	notebookID := f.seedNotebook(t, "Path notebook C", &childID, f.userID)
	f.grant(t, "folder", rootID, f.viewerID, []string{"view"})
	f.grant(t, "folder", childID, f.viewerID, []string{"view"})

	// The nested notebook resolves its full ancestor path (inherited view).
	status, resp := f.search(t, f.viewerToken, "path notebook c")
	require.Equal(t, http.StatusOK, status)
	notebooks := requireSearchGroup(t, resp, "notebook")
	require.Equal(t, []string{notebookID}, searchResultIDs(notebooks.Results))
	require.Equal(t, "Path root A / Path child B", notebooks.Results[0].FolderPath)

	// A folder result carries its own containing path.
	status, resp = f.search(t, f.viewerToken, "path child b")
	require.Equal(t, http.StatusOK, status)
	folders := requireSearchGroup(t, resp, "folder")
	require.Equal(t, []string{childID}, searchResultIDs(folders.Results))
	require.Equal(t, "Path root A", folders.Results[0].FolderPath)
	require.Equal(t, "Folder", folders.Results[0].Subtitle)
}

func TestSearchRankingAndTiebreak(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	base := time.Now()
	exact := f.seedNotebook(t, "revenue", nil, f.userID)
	prefix := f.seedNotebook(t, "Revenue report", nil, f.userID)
	contains := f.seedNotebook(t, "Monthly revenue summary", nil, f.userID)
	content := f.seedNotebook(t, "Untitled notes", nil, f.userID)
	f.seedCell(t, content, "SELECT revenue FROM orders", 0)

	// Deliberately reversed recency: if rank did not dominate, the content
	// hit would come first.
	f.setNotebookUpdatedAt(t, exact, base.Add(-4*time.Hour))
	f.setNotebookUpdatedAt(t, prefix, base.Add(-3*time.Hour))
	f.setNotebookUpdatedAt(t, contains, base.Add(-2*time.Hour))
	f.setNotebookUpdatedAt(t, content, base.Add(-1*time.Hour))
	for _, id := range []string{exact, prefix, contains, content} {
		f.grant(t, "notebook", id, f.userID, []string{"view"})
	}

	status, resp := f.search(t, f.token, "revenue")
	require.Equal(t, http.StatusOK, status)
	notebooks := requireSearchGroup(t, resp, "notebook")
	require.Equal(t, []string{exact, prefix, contains, content}, searchResultIDs(notebooks.Results),
		"exact > prefix > contains > content-only, despite reverse recency")

	// Equal rank and equal timestamp: the resource id is the deterministic
	// tiebreak (ascending id text; random UUIDs, so sort them first).
	tieA := uuid.NewString()
	tieB := uuid.NewString()
	if tieA > tieB {
		tieA, tieB = tieB, tieA
	}
	tieAt := base.Add(-10 * time.Hour)
	for _, id := range []string{tieB, tieA} { // inserted in reverse id order to prove sorting, not insertion order
		_, err := f.srv.DB().Pool.Exec(context.Background(),
			`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
			id, f.orgID, "Revenue tie", f.userID)
		require.NoError(t, err)
		f.setNotebookUpdatedAt(t, id, tieAt)
		f.grant(t, "notebook", id, f.userID, []string{"view"})
	}
	status, resp = f.search(t, f.token, "revenue tie")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{tieA, tieB}, searchResultIDs(requireSearchGroup(t, resp, "notebook").Results))
}

// TestSearchHasMoreCountsOnlyVisible pins the truthfulness rule: hidden
// candidates are dropped before ranking and the limit, so they can never
// consume a slot or distort has_more.
func TestSearchHasMoreCountsOnlyVisible(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	visible := make([]string, 0, 3)
	for _, title := range []string{"Budget alpha", "Budget beta", "Budget gamma"} {
		id := f.seedNotebook(t, title, nil, f.userID)
		f.grant(t, "notebook", id, f.userID, []string{"view"})
		visible = append(visible, id)
	}
	for i := 0; i < 5; i++ {
		f.seedNotebook(t, fmt.Sprintf("Budget secret %d", i), nil, f.userID)
	}

	status, resp := f.searchRaw(t, f.token, "q=budget&limit=1")
	require.Equal(t, http.StatusOK, status)
	page := requireSearchGroup(t, resp, "notebook")
	require.Len(t, page.Results, 1)
	require.True(t, page.HasMore, "three visible matches with a limit of one must report has_more")

	status, resp = f.searchRaw(t, f.token, "q=budget&limit=5")
	require.Equal(t, http.StatusOK, status)
	page = requireSearchGroup(t, resp, "notebook")
	require.Len(t, page.Results, 3, "every visible match and nothing else")
	require.False(t, page.HasMore, "has_more must count visible candidates only")
	require.ElementsMatch(t, visible, searchResultIDs(page.Results))
	for _, result := range page.Results {
		require.NotContains(t, result.Title, "secret", "hidden candidates must never surface")
	}
}
