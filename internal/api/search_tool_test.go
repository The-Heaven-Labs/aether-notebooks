package api_test

// search_resources over MCP: the tool must run the same leak-safe core as
// GET /api/v1/search, with the caller identity built inside the MCP
// ToolContext (groups + admin mode). Real DB, real ACLs.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// callSearchResourcesTool runs an MCP tools/call for search_resources and
// returns the decoded response. When the tool itself failed, the MCP error
// text is returned instead of a decoded body.
func callSearchResourcesTool(t *testing.T, f *searchTestFixture, token string, args map[string]any) (searchResponseBody, string) {
	t.Helper()
	code, resp := mcpToolsCall(t, f.srv, token, "search_resources", args)
	require.Equal(t, http.StatusOK, code, "mcp tools/call: %v", resp)
	result, _ := resp["result"].(map[string]any)
	text := mcpResultText(t, resp)
	if isErr, _ := result["isError"].(bool); isErr {
		return searchResponseBody{}, text
	}
	var body searchResponseBody
	require.NoError(t, json.Unmarshal([]byte(text), &body))
	return body, ""
}

func TestSearchResourcesToolACLFiltered(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	notebookID := f.seedNotebook(t, "Revenue notebook", nil, f.userID)
	f.grant(t, "notebook", notebookID, f.viewerID, []string{"view"})

	// The grantee finds the notebook; the stranger sees nothing.
	body, errText := callSearchResourcesTool(t, f, f.viewerToken, map[string]any{"query": "revenue notebook"})
	require.Empty(t, errText)
	notebooks := requireSearchGroup(t, body, "notebook")
	require.Equal(t, []string{notebookID}, searchResultIDs(notebooks.Results))
	require.False(t, notebooks.Results[0].CanEdit)

	strangerBody, errText := callSearchResourcesTool(t, f, f.strangerToken, map[string]any{"query": "revenue notebook"})
	require.Empty(t, errText)
	require.Empty(t, strangerBody.Groups)

	// The types filter restricts the groups the tool returns.
	filtered, errText := callSearchResourcesTool(t, f, f.viewerToken, map[string]any{
		"query": "revenue notebook", "types": []string{"connector"},
	})
	require.Empty(t, errText)
	require.Empty(t, filtered.Groups, "types=connector must not return the notebook group")
}

func TestSearchResourcesToolDashboardLeakRules(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	dashboardID := f.seedDashboard(t, "Published alpha", nil, f.userID)
	connectorID := f.seedConnector(t, "Metrics connector", nil, f.userID)
	f.seedWidget(t, dashboardID, connectorID, "SELECT secret_metric FROM ledger")
	f.grant(t, "dashboard", dashboardID, f.viewerID, []string{"view"})
	f.grant(t, "dashboard", dashboardID, f.userID, []string{"view", "edit"})
	f.publishDashboard(t, dashboardID, "Published alpha", 1, f.userID)
	f.renameDashboard(t, dashboardID, "Draft-only secret")

	// Viewer: the draft-only title and the draft widget SQL are invisible.
	body, errText := callSearchResourcesTool(t, f, f.viewerToken, map[string]any{"query": "draft-only secret"})
	require.Empty(t, errText)
	require.Empty(t, body.Groups, "a viewer must not match an unpublished draft title")
	body, errText = callSearchResourcesTool(t, f, f.viewerToken, map[string]any{"query": "secret_metric"})
	require.Empty(t, errText)
	require.NotContains(t, searchGroupTypes(body), "dashboard", "a viewer must not match draft widget SQL")

	// Viewer: the published title finds the dashboard under the snapshot title.
	body, errText = callSearchResourcesTool(t, f, f.viewerToken, map[string]any{"query": "published alpha"})
	require.Empty(t, errText)
	dashboards := requireSearchGroup(t, body, "dashboard")
	require.Equal(t, dashboardID, dashboards.Results[0].ID)
	require.Equal(t, "Published alpha", dashboards.Results[0].Title)
	require.False(t, dashboards.Results[0].CanEdit)

	// Editor: the draft title matches and the widget SQL arrives with a snippet.
	body, errText = callSearchResourcesTool(t, f, f.token, map[string]any{"query": "draft-only secret"})
	require.Empty(t, errText)
	dashboards = requireSearchGroup(t, body, "dashboard")
	require.Equal(t, "Draft-only secret", dashboards.Results[0].Title)
	require.True(t, dashboards.Results[0].CanEdit)
	body, errText = callSearchResourcesTool(t, f, f.token, map[string]any{"query": "secret_metric"})
	require.Empty(t, errText)
	dashboards = requireSearchGroup(t, body, "dashboard")
	require.Equal(t, dashboardID, dashboards.Results[0].ID)
	require.NotNil(t, dashboards.Results[0].Snippet)
}

// Tool argument validation mirrors the HTTP endpoint: a missing query, an
// unknown type, and an over-long query are errors, never silently-empty
// results. The query bound is the shared normalizer's, pinned here at the
// tool entry.
func TestSearchResourcesToolValidation(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	_, errText := callSearchResourcesTool(t, f, f.viewerToken, map[string]any{})
	require.Contains(t, errText, "query is required")

	_, errText = callSearchResourcesTool(t, f, f.viewerToken, map[string]any{"query": "x", "types": []string{"bogus"}})
	require.Contains(t, errText, `unknown resource type "bogus"`)

	_, errText = callSearchResourcesTool(t, f, f.viewerToken, map[string]any{"query": strings.Repeat("a", 201)})
	require.Contains(t, errText, "query must be at most 200 characters")
}

// The tool shares the HTTP limit rules end to end: an explicit limit above
// the ceiling clamps to 20 (has_more reports the truncation) and a limit
// below 1 is an error, never a silent default.
func TestSearchResourcesToolLimitRules(t *testing.T) {
	srv := setupTestServer(t)
	f := setupSearchTestFixture(t, srv)

	for i := 0; i < 25; i++ {
		id := f.seedNotebook(t, fmt.Sprintf("Limit probe %02d", i), nil, f.userID)
		f.grant(t, "notebook", id, f.userID, []string{"view"})
	}

	body, errText := callSearchResourcesTool(t, f, f.token, map[string]any{"query": "limit probe", "limit": 999})
	require.Empty(t, errText)
	notebooks := requireSearchGroup(t, body, "notebook")
	require.Len(t, notebooks.Results, 20, "the shared normalizer must clamp to the per-type ceiling")
	require.True(t, notebooks.HasMore)

	_, errText = callSearchResourcesTool(t, f, f.token, map[string]any{"query": "limit probe", "limit": 0})
	require.Contains(t, errText, "limit must be a positive integer", "limit 0 must be rejected, not clamped to 1")
}
