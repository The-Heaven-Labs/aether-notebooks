package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// sessionSharingFixture is one org with an org-admin owner (Alice) and a plain
// member (Bob), plus an agent Alice owns. Bob is inserted directly and carries
// a JWT for Alice's org so tests never depend on a second registration.
type sessionSharingFixture struct {
	srv        *api.Server
	orgID      string
	aliceID    string
	aliceToken string
	bobID      string
	bobToken   string
	agentID    string
}

func setupSessionSharingFixture(t *testing.T) *sessionSharingFixture {
	t.Helper()
	// Registration is rate-limited per IP (default 5/min); a full-package run
	// registers more users than that from the same test client address.
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()

	aliceToken := registerAndGetToken(t, srv,
		fmt.Sprintf("session-share-alice-%d@example.com", ts), "Session Sharing Org")
	aliceID := userIDFromToken(t, srv, aliceToken)
	orgID := orgIDFromUser(t, srv, aliceID)

	bobID := insertUser(t, srv, fmt.Sprintf("session-share-bob-%d@example.com", ts), "Bob Sharer")
	addOrgMember(t, srv, orgID, bobID, "editor")
	bobToken := issueToken(t, bobID, orgID, "editor")

	mcID := createModelConfig(t, srv, aliceToken)
	agentID := createAgent(t, srv, aliceToken, mcID)

	return &sessionSharingFixture{
		srv: srv, orgID: orgID,
		aliceID: aliceID, aliceToken: aliceToken,
		bobID: bobID, bobToken: bobToken,
		agentID: agentID,
	}
}

func postCreateSession(t *testing.T, srv *api.Server, token, agentID string, body map[string]any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/api/v1/agents/"+agentID+"/session", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&resp)
	}
	return rec.Code, resp
}

// sessionACLActions returns the stored actions for one subject on a session,
// or nil when no row exists.
func sessionACLActions(t *testing.T, srv *api.Server, sessionID, subjectType, subjectID string) []string {
	t.Helper()
	var actions []string
	err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT actions FROM acl_entries
		 WHERE resource_type = 'agent_session' AND resource_id = $1::uuid
		   AND subject_type = $2 AND subject_id = $3`,
		sessionID, subjectType, subjectID).Scan(&actions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	require.NoError(t, err)
	return actions
}

func sessionACLCount(t *testing.T, srv *api.Server, sessionID string) int {
	t.Helper()
	var count int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1::uuid`,
		sessionID).Scan(&count))
	return count
}

// sessionArtifactCounts returns how many sessions and agent_session ACL rows
// exist for one (user, agent) pair. Tests snapshot it around a rejected request
// to prove no session or ACL row was written.
func sessionArtifactCounts(t *testing.T, srv *api.Server, userID, agentID string) (sessions, acls int) {
	t.Helper()
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_sessions WHERE user_id = $1 AND agent_id = $2`,
		userID, agentID).Scan(&sessions))
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM acl_entries
		 WHERE resource_type = 'agent_session'
		   AND resource_id IN (SELECT id FROM agent_sessions WHERE user_id = $1 AND agent_id = $2)`,
		userID, agentID).Scan(&acls))
	return sessions, acls
}

func sessionExists(t *testing.T, srv *api.Server, sessionID string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM agent_sessions WHERE id = $1)`, sessionID).Scan(&exists))
	return exists
}

// requireACLReadStatus asserts the read-permission outcome for a token against
// the generic ACL endpoint, which resolves a direct agent_session view grant.
func requireACLReadStatus(t *testing.T, srv *api.Server, token, resourceType, resourceID string, want int) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/acl/"+resourceType+"/"+resourceID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, want, rec.Code, rec.Body.String())
}

func TestCreateSessionWithShares(t *testing.T) {
	f := setupSessionSharingFixture(t)
	ctx := context.Background()
	notebookID := createNotebook(t, f.srv, f.aliceToken, "Shares NB")

	t.Run("owner entry and user share commit with the session", func(t *testing.T) {
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{
			"notebook_id": notebookID,
			"shares": []map[string]any{
				{"subject_type": "user", "subject_id": f.bobID, "actions": []string{"view"}},
			},
		})
		require.Equal(t, http.StatusCreated, code, "%v", resp)
		sessionID, ok := resp["session_id"].(string)
		require.True(t, ok, "response missing session_id: %v", resp)
		require.Contains(t, resp, "context_window")
		require.Contains(t, resp, "auto_approve_tools")
		require.Contains(t, resp, "auto_answer_questions")

		require.Equal(t,
			[]string{"view", "edit", "share", "delete", "admin"},
			sessionACLActions(t, f.srv, sessionID, "user", f.aliceID),
			"owner ACL entry must be written with the session")

		require.Equal(t, []string{"view"},
			sessionACLActions(t, f.srv, sessionID, "user", f.bobID),
			"share entry must be written with the session")

		// The share is effective immediately: Bob's view check passes without
		// any follow-up ACL request.
		requireACLReadStatus(t, f.srv, f.bobToken, "agent_session", sessionID, http.StatusOK)
	})

	t.Run("defaults, duplicates, and owner shares normalize", func(t *testing.T) {
		groupID := createGroup(t, f.srv, f.aliceToken, "Share Group")
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{
			"notebook_id": notebookID,
			"shares": []map[string]any{
				{"subject_type": "group", "subject_id": groupID},
				{"subject_type": "group", "subject_id": groupID, "actions": []string{}},
				{"subject_type": "user", "subject_id": f.aliceID, "actions": []string{"edit"}},
			},
		})
		require.Equal(t, http.StatusCreated, code, "%v", resp)
		sessionID := resp["session_id"].(string)

		require.Equal(t, []string{"view"}, sessionACLActions(t, f.srv, sessionID, "group", groupID),
			"empty actions must default to view and duplicate subjects must collapse")
		require.Equal(t,
			[]string{"view", "edit", "share", "delete", "admin"},
			sessionACLActions(t, f.srv, sessionID, "user", f.aliceID),
			"the owner entry must keep full access")
	})

	t.Run("org_role everyone share", func(t *testing.T) {
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{
			"notebook_id": notebookID,
			"shares": []map[string]any{
				{"subject_type": "org_role", "subject_id": "everyone", "actions": []string{"view"}},
			},
		})
		require.Equal(t, http.StatusCreated, code, "%v", resp)
		sessionID := resp["session_id"].(string)
		require.Equal(t, []string{"view"}, sessionACLActions(t, f.srv, sessionID, "org_role", "everyone"))
	})

	t.Run("invalid shares are rejected", func(t *testing.T) {
		foreignGroupID := uuid.NewString()
		var foreignOrgID string
		require.NoError(t, f.srv.DB().Pool.QueryRow(ctx,
			`INSERT INTO orgs (name, slug) VALUES ('Foreign Org', 'foreign-' || gen_random_uuid()) RETURNING id`,
		).Scan(&foreignOrgID))
		_, err := f.srv.DB().Pool.Exec(ctx,
			`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, 'foreign')`,
			foreignGroupID, foreignOrgID)
		require.NoError(t, err)

		cases := []struct {
			name  string
			share map[string]any
		}{
			{"non-owner edit", map[string]any{"subject_type": "user", "subject_id": f.bobID, "actions": []string{"edit"}}},
			{"unknown action", map[string]any{"subject_type": "user", "subject_id": f.bobID, "actions": []string{"run"}}},
			{"unknown user", map[string]any{"subject_type": "user", "subject_id": uuid.NewString(), "actions": []string{"view"}}},
			{"malformed user id", map[string]any{"subject_type": "user", "subject_id": "not-a-uuid", "actions": []string{"view"}}},
			{"unknown group", map[string]any{"subject_type": "group", "subject_id": uuid.NewString(), "actions": []string{"view"}}},
			{"cross-org group", map[string]any{"subject_type": "group", "subject_id": foreignGroupID, "actions": []string{"view"}}},
			{"org_role not everyone", map[string]any{"subject_type": "org_role", "subject_id": "admin", "actions": []string{"view"}}},
			{"unknown subject type", map[string]any{"subject_type": "widget", "subject_id": f.bobID, "actions": []string{"view"}}},
		}
		sessionsBefore, aclsBefore := sessionArtifactCounts(t, f.srv, f.aliceID, f.agentID)
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{
					"shares": []map[string]any{tc.share},
				})
				require.Equal(t, http.StatusBadRequest, code, "%v", resp)

				sessions, acls := sessionArtifactCounts(t, f.srv, f.aliceID, f.agentID)
				require.Equal(t, sessionsBefore, sessions, "a rejected request must not create a session")
				require.Equal(t, aclsBefore, acls, "a rejected request must not create session ACL rows")
			})
		}
	})

	t.Run("oversized share list is rejected", func(t *testing.T) {
		sessionsBefore, aclsBefore := sessionArtifactCounts(t, f.srv, f.aliceID, f.agentID)
		shares := make([]map[string]any, 0, 101)
		for i := 0; i < 101; i++ {
			shares = append(shares, map[string]any{
				"subject_type": "user", "subject_id": f.bobID, "actions": []string{"view"},
			})
		}
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{"shares": shares})
		require.Equal(t, http.StatusBadRequest, code, "%v", resp)

		sessions, acls := sessionArtifactCounts(t, f.srv, f.aliceID, f.agentID)
		require.Equal(t, sessionsBefore, sessions, "a rejected request must not create a session")
		require.Equal(t, aclsBefore, acls, "a rejected request must not create session ACL rows")
	})

	t.Run("inherit flag requires a notebook", func(t *testing.T) {
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{
			"share_with_notebook_viewers": true,
		})
		require.Equal(t, http.StatusBadRequest, code, "%v", resp)
	})

	t.Run("inherit flag persists with a notebook", func(t *testing.T) {
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, map[string]any{
			"notebook_id":                 notebookID,
			"share_with_notebook_viewers": true,
		})
		require.Equal(t, http.StatusCreated, code, "%v", resp)
		sessionID := resp["session_id"].(string)

		var inherit bool
		require.NoError(t, f.srv.DB().Pool.QueryRow(ctx,
			`SELECT share_with_notebook_viewers FROM agent_sessions WHERE id = $1`, sessionID).Scan(&inherit))
		require.True(t, inherit)
	})
}

func TestCreateSessionNotebookPermission(t *testing.T) {
	f := setupSessionSharingFixture(t)
	notebookID := createNotebook(t, f.srv, f.aliceToken, "Notebook Permission NB")

	// Bob may use the agent but cannot view Alice's notebook.
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'agent', $2::uuid, 'user', $3, ARRAY['view'])`,
		f.orgID, f.agentID, f.bobID)
	require.NoError(t, err)

	t.Run("unviewable notebook is rejected", func(t *testing.T) {
		code, resp := postCreateSession(t, f.srv, f.bobToken, f.agentID, map[string]any{
			"notebook_id": notebookID,
		})
		require.Equal(t, http.StatusForbidden, code, "%v", resp)
	})

	t.Run("no notebook still creates", func(t *testing.T) {
		code, resp := postCreateSession(t, f.srv, f.bobToken, f.agentID, map[string]any{})
		require.Equal(t, http.StatusCreated, code, "%v", resp)
	})
}

func TestCreateSessionEmptyCleanupScopedToNotebook(t *testing.T) {
	f := setupSessionSharingFixture(t)
	notebookA := createNotebook(t, f.srv, f.aliceToken, "Cleanup NB A")
	notebookB := createNotebook(t, f.srv, f.aliceToken, "Cleanup NB B")

	create := func(notebookID string) string {
		t.Helper()
		body := map[string]any{}
		if notebookID != "" {
			body["notebook_id"] = notebookID
		}
		if notebookID == notebookA {
			body["shares"] = []map[string]any{
				{"subject_type": "user", "subject_id": f.bobID, "actions": []string{"view"}},
			}
		}
		code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, body)
		require.Equal(t, http.StatusCreated, code, "%v", resp)
		return resp["session_id"].(string)
	}

	sA1 := create(notebookA)
	sB1 := create(notebookB)

	// A new empty session in notebook A sweeps A's previous empty session and
	// its ACL rows, but leaves notebook B's session alone.
	sA2 := create(notebookA)
	require.False(t, sessionExists(t, f.srv, sA1), "empty session in the same notebook must be swept")
	require.True(t, sessionExists(t, f.srv, sB1), "empty session in another notebook must survive")
	require.True(t, sessionExists(t, f.srv, sA2))
	require.Zero(t, sessionACLCount(t, f.srv, sA1), "swept session ACL rows must be deleted")
	require.NotZero(t, sessionACLCount(t, f.srv, sB1))

	// A notebookless session only sweeps other notebookless sessions.
	sNoNB1 := create("")
	sNoNB2 := create("")
	require.False(t, sessionExists(t, f.srv, sNoNB1))
	require.True(t, sessionExists(t, f.srv, sNoNB2))
	require.True(t, sessionExists(t, f.srv, sA2), "notebook sessions must survive a notebookless create")
	require.True(t, sessionExists(t, f.srv, sB1))

	// Sessions with messages are never swept, even for the same notebook.
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO agent_messages (session_id, role, content, created_at) VALUES ($1, 'user', 'keep me', NOW())`,
		sA2)
	require.NoError(t, err)
	sA3 := create(notebookA)
	require.True(t, sessionExists(t, f.srv, sA2), "non-empty session must survive the sweep")
	require.True(t, sessionExists(t, f.srv, sA3))
	require.NotZero(t, sessionACLCount(t, f.srv, sA2))
}

// TestCreateSessionDatabaseFailureIsNotBadRequest guards the wire-level error
// mapping in one direction: with the database failing, the endpoint must never
// answer 400 (invalid input). The internal share-validation tests pin the
// per-error classification (sentinel → 400, database error → 500).
func TestCreateSessionDatabaseFailureIsNotBadRequest(t *testing.T) {
	f := setupSessionSharingFixture(t)

	// Canceling the request context makes every query fail like a database
	// outage without touching the shared pool other tests use.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	body, err := json.Marshal(map[string]any{
		"shares": []map[string]any{
			{"subject_type": "user", "subject_id": f.bobID, "actions": []string{"view"}},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/agents/"+f.agentID+"/session", bytes.NewReader(body)).WithContext(ctx)
	req.Host = "localhost" // skip the DB-backed subdomain lookup
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.aliceToken)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)

	require.NotEqual(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
