package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

func putSessionACLViaAPI(t *testing.T, srv *api.Server, token, sessionID string, entries []map[string]any) (int, []aclEntryJSON) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"entries": entries})
	req := httptest.NewRequest("PUT", "/api/v1/acl/agent_session/"+sessionID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out []aclEntryJSON
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&out)
	}
	return rec.Code, out
}

func countPendingSessionShares(t *testing.T, srv *api.Server, sessionID string) int {
	t.Helper()
	var n int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pending_acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid`, sessionID).Scan(&n))
	return n
}

func TestSessionPendingShareRoundTrip(t *testing.T) {
	fx := setupSessionSharingFixture(t)

	// Session creation accepts a pending view-only share and stages it.
	code, resp := postCreateSession(t, fx.srv, fx.aliceToken, fx.agentID, map[string]any{
		"shares": []map[string]any{
			{"subject_type": "pending_user", "subject_id": "Future.Viewer@Example.com", "actions": []string{"view"}},
		},
	})
	require.Equal(t, http.StatusCreated, code, fmt.Sprint(resp))
	sessionID := resp["session_id"].(string)
	require.Equal(t, 1, countPendingSessionShares(t, fx.srv, sessionID))

	// GET surfaces the staged row as pending_user with the pending marker.
	req := httptest.NewRequest("GET", "/api/v1/acl/agent_session/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer "+fx.aliceToken)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var entries []aclEntryJSON
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	var pending *aclEntryJSON
	for i := range entries {
		if entries[i].SubjectType == "pending_user" {
			pending = &entries[i]
		}
	}
	require.NotNil(t, pending)
	require.Equal(t, "future.viewer@example.com", pending.SubjectID)
	require.True(t, pending.Pending)
	require.Equal(t, []string{"view"}, pending.Actions)

	// Non-view pending shares are rejected as invalid input.
	badCode, _ := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, sessionID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future.viewer@example.com", "actions": []string{"view", "edit"}},
	})
	require.Equal(t, http.StatusBadRequest, badCode)

	// PUT replaces non-owner entries: a real member share plus a pending share,
	// then dropping the pending one on the next PUT.
	putCode, putEntries := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, sessionID, []map[string]any{
		{"subject_type": "user", "subject_id": fx.bobID, "actions": []string{"view"}},
		{"subject_type": "pending_user", "subject_id": "other@example.com", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, putCode)
	require.Equal(t, 1, countPendingSessionShares(t, fx.srv, sessionID))
	require.Len(t, putEntries, 3, "owner + member share + staged pending share")

	lastCode, _ := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, sessionID, []map[string]any{
		{"subject_type": "user", "subject_id": fx.bobID, "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, lastCode)
	require.Zero(t, countPendingSessionShares(t, fx.srv, sessionID), "a PUT omitting the staged share removes it")
}

func TestEmptySessionSweepRemovesPendingShares(t *testing.T) {
	fx := setupSessionSharingFixture(t)

	code, resp := postCreateSession(t, fx.srv, fx.aliceToken, fx.agentID, map[string]any{})
	require.Equal(t, http.StatusCreated, code, fmt.Sprint(resp))
	first := resp["session_id"].(string)

	putCode, _ := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, first, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future@example.com", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, putCode)
	require.Equal(t, 1, countPendingSessionShares(t, fx.srv, first))

	// Creating another empty session for the same agent/user/notebook sweeps
	// the first session and must sweep its staged shares too.
	code2, _ := postCreateSession(t, fx.srv, fx.aliceToken, fx.agentID, map[string]any{})
	require.Equal(t, http.StatusCreated, code2)

	require.Zero(t, countPendingSessionShares(t, fx.srv, first))
	var sessions int
	require.NoError(t, fx.srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_sessions WHERE id = $1`, first).Scan(&sessions))
	require.Zero(t, sessions)
}
