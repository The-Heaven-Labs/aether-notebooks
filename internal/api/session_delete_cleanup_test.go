package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// createSessionForCleanup creates a session through the API and, optionally,
// shares it with one user so the session carries more than the owner ACL row.
func createSessionForCleanup(t *testing.T, f *sessionSharingFixture, token, agentID, notebookID, shareTo string) string {
	t.Helper()
	body := map[string]any{}
	if notebookID != "" {
		body["notebook_id"] = notebookID
	}
	if shareTo != "" {
		body["shares"] = []map[string]any{
			{"subject_type": "user", "subject_id": shareTo, "actions": []string{"view"}},
		}
	}
	code, resp := postCreateSession(t, f.srv, token, agentID, body)
	require.Equal(t, http.StatusCreated, code, "%v", resp)
	sessionID, ok := resp["session_id"].(string)
	require.True(t, ok, "response missing session_id: %v", resp)
	return sessionID
}

func TestDeleteAgentRemovesSessionACLs(t *testing.T) {
	f := setupSessionSharingFixture(t)
	ctx := context.Background()
	notebookA := createNotebook(t, f.srv, f.aliceToken, "Agent Delete NB A")
	notebookB := createNotebook(t, f.srv, f.aliceToken, "Agent Delete NB B")

	// One notebook each: empty sessions for the same user+agent+notebook are
	// swept at creation, and the sweep is outside what this test covers.
	sharedSession := createSessionForCleanup(t, f, f.aliceToken, f.agentID, notebookA, f.bobID)
	plainSession := createSessionForCleanup(t, f, f.aliceToken, f.agentID, notebookB, "")
	require.Equal(t, 2, sessionACLCount(t, f.srv, sharedSession))

	// A session of a different agent must not be touched.
	mcID := createModelConfig(t, f.srv, f.aliceToken)
	otherAgentID := createAgent(t, f.srv, f.aliceToken, mcID)
	otherSession := createSessionForCleanup(t, f, f.aliceToken, otherAgentID, notebookA, "")

	code, _ := doRequest(t, f.srv, f.aliceToken, "DELETE", "/api/v1/agents/"+f.agentID, nil)
	require.Equal(t, http.StatusNoContent, code)

	require.False(t, sessionExists(t, f.srv, sharedSession))
	require.False(t, sessionExists(t, f.srv, plainSession))
	require.Zero(t, sessionACLCount(t, f.srv, sharedSession))
	require.Zero(t, sessionACLCount(t, f.srv, plainSession))

	require.True(t, sessionExists(t, f.srv, otherSession), "another agent's session must survive")
	require.NotZero(t, sessionACLCount(t, f.srv, otherSession), "another agent's session ACLs must survive")

	var agentCount int
	require.NoError(t, f.srv.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM agents WHERE id = $1`, f.agentID).Scan(&agentCount))
	require.Zero(t, agentCount)
}

func TestHardDeleteNotebookRemovesSessionACLs(t *testing.T) {
	f := setupSessionSharingFixture(t)
	ctx := context.Background()
	notebookID := createNotebook(t, f.srv, f.aliceToken, "Hard Delete NB")
	sessionID := createSessionForCleanup(t, f, f.aliceToken, f.agentID, notebookID, f.bobID)
	require.NotZero(t, sessionACLCount(t, f.srv, sessionID))

	// The hard-delete path is the delete_notebook agent tool (MCP call).
	code, resp, _ := doMCPRequest(t, f.srv, f.aliceToken, "tools/call", map[string]any{
		"name":      "delete_notebook",
		"arguments": map[string]any{"notebook_id": notebookID},
	})
	require.Equal(t, http.StatusOK, code)
	result, _ := resp["result"].(map[string]any)
	require.NotNil(t, result, "delete_notebook must return a result: %v", resp)
	require.NotEqual(t, true, result["isError"], "delete_notebook must succeed: %v", resp)

	var deletedAt *time.Time
	err := f.srv.DB().Pool.QueryRow(ctx, `SELECT deleted_at FROM notebooks WHERE id = $1`, notebookID).Scan(&deletedAt)
	require.ErrorIs(t, err, pgx.ErrNoRows, "hard delete must remove the notebook row")

	require.False(t, sessionExists(t, f.srv, sessionID))
	require.Zero(t, sessionACLCount(t, f.srv, sessionID))
}

func TestSoftDeleteNotebookKeepsSessionACLs(t *testing.T) {
	f := setupSessionSharingFixture(t)
	ctx := context.Background()
	notebookID := createNotebook(t, f.srv, f.aliceToken, "Trash NB")
	sessionID := createSessionForCleanup(t, f, f.aliceToken, f.agentID, notebookID, f.bobID)
	require.NotZero(t, sessionACLCount(t, f.srv, sessionID))

	code, _ := doRequest(t, f.srv, f.aliceToken, "DELETE", "/api/v1/notebooks/"+notebookID, nil)
	require.Equal(t, http.StatusNoContent, code)

	var deletedAt *time.Time
	require.NoError(t, f.srv.DB().Pool.QueryRow(ctx,
		`SELECT deleted_at FROM notebooks WHERE id = $1`, notebookID).Scan(&deletedAt))
	require.NotNil(t, deletedAt, "REST delete is a soft delete")
	require.True(t, sessionExists(t, f.srv, sessionID), "trashing must keep the sessions")
	require.NotZero(t, sessionACLCount(t, f.srv, sessionID), "trashing must keep the session ACL rows")
}

func TestAdminDeleteUserRemovesSessionACLs(t *testing.T) {
	f := setupSessionSharingFixture(t)
	ctx := context.Background()

	// Bob may use Alice's agent so he can own sessions on it.
	_, err := f.srv.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'agent', $2::uuid, 'user', $3, ARRAY['view'])`,
		f.orgID, f.agentID, f.bobID)
	require.NoError(t, err)

	sessionID := createSessionForCleanup(t, f, f.bobToken, f.agentID, "", f.aliceID)
	require.Equal(t, 2, sessionACLCount(t, f.srv, sessionID), "owner grant plus Alice's share")

	platformAdminID := insertUser(t, f.srv, fmt.Sprintf("session-delete-admin-%d@example.com", time.Now().UnixNano()), "Platform Admin")
	addOrgMember(t, f.srv, f.orgID, platformAdminID, "admin")
	platToken, err := testJWT.IssuePlatformAdmin(platformAdminID, f.orgID, "admin")
	require.NoError(t, err)

	code, _ := doRequest(t, f.srv, platToken, "DELETE", "/api/v1/admin/users/"+f.bobID, nil)
	require.Equal(t, http.StatusNoContent, code)

	require.False(t, sessionExists(t, f.srv, sessionID))
	require.Zero(t, sessionACLCount(t, f.srv, sessionID))
}
