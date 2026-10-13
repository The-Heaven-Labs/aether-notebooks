package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type reviewFindingBody struct {
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	ChapterID string `json:"chapter_id"`
	WidgetID  string `json:"widget_id"`
	Message   string `json:"message"`
	FixTool   string `json:"fix_tool"`
}

type reviewBody struct {
	DashboardID string              `json:"dashboard_id"`
	Count       int                 `json:"count"`
	ViewURL     string              `json:"view_url"`
	PreviewURL  string              `json:"preview_url"`
	PublicURL   string              `json:"public_url"`
	Findings    []reviewFindingBody `json:"findings"`
}

// TestDashboardReviewEndpoint pins GET /dashboards/{id}/review: editors get
// the findings JSON plus the app links (public link only once shared), a
// viewer without edit gets 403, and a missing dashboard is 404.
func TestDashboardReviewEndpoint(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	srv.SetFrontendURL("https://app.example.test")

	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("dash-review-%d@example.com", ts), "Dash Review Org")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, map[string]any{
		"tabs": []map[string]string{
			{"id": "overview", "title": "Overview"},
			{"id": "orders", "title": "Orders"},
		},
	})
	// One widget in overview → the orders chapter is empty.
	addVersionsQueryWidget(t, srv, token, dashID, connID, "SELECT 1", 0)

	rec := versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID+"/review", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body reviewBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, dashID, body.DashboardID)
	require.Equal(t, len(body.Findings), body.Count)
	require.GreaterOrEqual(t, body.Count, 1)

	var emptyChapter *reviewFindingBody
	for i := range body.Findings {
		if body.Findings[i].ChapterID == "orders" && strings.Contains(body.Findings[i].Message, "no widgets") {
			emptyChapter = &body.Findings[i]
		}
	}
	require.NotNil(t, emptyChapter, "the empty chapter must be reported: %+v", body.Findings)
	require.Equal(t, "set_dashboard_layout", emptyChapter.FixTool)
	require.Equal(t, "warning", emptyChapter.Severity)

	require.Equal(t, "https://app.example.test/dashboards/"+dashID+"/view", body.ViewURL)
	require.Equal(t, "https://app.example.test/dashboards/"+dashID+"/view?preview=draft", body.PreviewURL)
	require.Empty(t, body.PublicURL, "an unshared dashboard has no public link")

	// Sharing turns the public link on; the endpoint never mints a token.
	tokenValue := fmt.Sprintf("review-endpoint-%d", ts)
	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_by)
		VALUES ($1, 'dashboard', $2, $3, $4)
	`, orgID, dashID, tokenValue, userID)
	require.NoError(t, err)

	rec = versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+dashID+"/review", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "https://app.example.test/public/dashboards/"+tokenValue, body.PublicURL)

	// A viewer with only `view` on the dashboard cannot read the draft review.
	viewerID := insertUser(t, srv, fmt.Sprintf("dash-review-viewer-%d@example.com", ts), "Review Viewer")
	addOrgMember(t, srv, orgID, viewerID, "editor")
	grantACL(t, srv, orgID, "dashboard", dashID, "user", viewerID, "view")
	viewerToken := issueToken(t, viewerID, orgID, "editor")
	rec = versionsRequest(t, srv, viewerToken, "GET", "/api/v1/dashboards/"+dashID+"/review", nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// A missing dashboard is 404, even for its editor.
	rec = versionsRequest(t, srv, token, "GET", "/api/v1/dashboards/"+uuid.NewString()+"/review", nil)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
