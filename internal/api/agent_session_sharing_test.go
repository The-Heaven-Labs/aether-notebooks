package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// sessionSharingFixture is one org with an org-admin owner (Alice) and two
// plain members (Bob, Carol), plus an agent Alice owns. Bob and Carol are
// inserted directly and carry JWTs for Alice's org so tests never depend on a
// second registration.
type sessionSharingFixture struct {
	srv        *api.Server
	orgID      string
	aliceID    string
	aliceToken string
	bobID      string
	bobToken   string
	carolID    string
	carolToken string
	agentID    string
}

func setupSessionSharingFixture(t *testing.T) *sessionSharingFixture {
	t.Helper()
	// Registration is rate-limited per IP (default 5/min); a full-package run
	// registers more users than that from the same test client address.
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	// The attachment read paths store real files, so the fixture needs a
	// per-test storage directory.
	srv := setupTestServerWithAttachDir(t)
	ts := time.Now().UnixNano()

	aliceToken := registerAndGetToken(t, srv,
		fmt.Sprintf("session-share-alice-%d@example.com", ts), "Session Sharing Org")
	aliceID := userIDFromToken(t, srv, aliceToken)
	orgID := orgIDFromUser(t, srv, aliceID)

	bobID := insertUser(t, srv, fmt.Sprintf("session-share-bob-%d@example.com", ts), "Bob Sharer")
	addOrgMember(t, srv, orgID, bobID, "editor")
	bobToken := issueToken(t, bobID, orgID, "editor")

	carolID := insertUser(t, srv, fmt.Sprintf("session-share-carol-%d@example.com", ts), "Carol Outsider")
	addOrgMember(t, srv, orgID, carolID, "editor")
	carolToken := issueToken(t, carolID, orgID, "editor")

	mcID := createModelConfig(t, srv, aliceToken)
	agentID := createAgent(t, srv, aliceToken, mcID)

	return &sessionSharingFixture{
		srv: srv, orgID: orgID,
		aliceID: aliceID, aliceToken: aliceToken,
		bobID: bobID, bobToken: bobToken,
		carolID: carolID, carolToken: carolToken,
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

// grantACL upserts one ACL row directly; the ACL PUT handler that would
// normally write these is out of scope for the read-path task.
func grantACL(t *testing.T, srv *api.Server, orgID, resourceType, resourceID, subjectType, subjectID string, actions ...string) {
	t.Helper()
	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, $2, $3::uuid, $4, $5, $6)
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = EXCLUDED.actions
	`, orgID, resourceType, resourceID, subjectType, subjectID, actions)
	require.NoError(t, err)
}

// revokeAgentACLs removes a user's ACL rows on an agent so tests can prove the
// session owner does not depend on agent-level permissions.
func revokeAgentACLs(t *testing.T, srv *api.Server, agentID, userID string) {
	t.Helper()
	_, err := srv.DB().Pool.Exec(context.Background(),
		`DELETE FROM acl_entries WHERE resource_type = 'agent' AND resource_id = $1::uuid
		   AND subject_type = 'user' AND subject_id = $2`,
		agentID, userID)
	require.NoError(t, err)
}

// createSession creates a session owned by the fixture owner (Alice) and
// returns its ID.
func (f *sessionSharingFixture) createSession(t *testing.T, body map[string]any) string {
	t.Helper()
	code, resp := postCreateSession(t, f.srv, f.aliceToken, f.agentID, body)
	require.Equal(t, http.StatusCreated, code, "%v", resp)
	id, _ := resp["session_id"].(string)
	require.NotEmpty(t, id)
	return id
}

func seedSessionMessage(t *testing.T, srv *api.Server, sessionID, content string) {
	t.Helper()
	_, err := srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO agent_messages (session_id, role, content, created_at) VALUES ($1, 'user', $2, NOW())`,
		sessionID, content)
	require.NoError(t, err)
}

// rawRequest performs an authenticated JSON request without decoding the
// response, for endpoints whose success payload is an array.
func rawRequest(t *testing.T, srv *api.Server, token, method, path string, body map[string]any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func uploadSessionAttachment(t *testing.T, srv *api.Server, token, sessionID string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "test.png")
	require.NoError(t, err)
	_, err = io.WriteString(fw, "fake-image-bytes")
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest("POST", "/api/v1/agent-sessions/"+sessionID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&resp)
	}
	return rec.Code, resp
}

func fetchAgentAttachment(t *testing.T, srv *api.Server, token, attachmentID string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/agent-attachments/"+attachmentID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func getSubagentMessages(t *testing.T, srv *api.Server, token, taskID string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/agents/subagent/"+taskID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestSessionReadAuthorization pins the session-level read model: the owner and
// direct shares read, while an org member who can view the agent but holds no
// session share is denied. Agent ACLs must never grant session reads.
func TestSessionReadAuthorization(t *testing.T) {
	f := setupSessionSharingFixture(t)
	sessionID := f.createSession(t, map[string]any{})
	seedSessionMessage(t, f.srv, sessionID, "hello")

	// Bob holds a direct view share. Carol can view the agent but not the
	// session: under the old agent-level check she could read every session.
	grantACL(t, f.srv, f.orgID, "agent_session", sessionID, "user", f.bobID, "view")
	grantACL(t, f.srv, f.orgID, "agent", f.agentID, "user", f.carolID, "view")

	// The owner must pass even with every agent ACL revoked.
	revokeAgentACLs(t, f.srv, f.agentID, f.aliceID)

	reads := []struct {
		name string
		path string
	}{
		{"get", "/api/v1/sessions/" + sessionID},
		{"messages", "/api/v1/sessions/" + sessionID + "/messages"},
		{"usage", "/api/v1/agents/sessions/" + sessionID + "/usage"},
	}
	for _, read := range reads {
		t.Run("owner "+read.name, func(t *testing.T) {
			code, body := rawRequest(t, f.srv, f.aliceToken, "GET", read.path, nil)
			require.Equal(t, http.StatusOK, code, "%v", body)
		})
		t.Run("shared "+read.name, func(t *testing.T) {
			code, body := rawRequest(t, f.srv, f.bobToken, "GET", read.path, nil)
			require.Equal(t, http.StatusOK, code, "%v", body)
		})
		t.Run("unrelated "+read.name, func(t *testing.T) {
			code, body := rawRequest(t, f.srv, f.carolToken, "GET", read.path, nil)
			require.Equal(t, http.StatusForbidden, code, "%v", body)
		})
	}

	t.Run("unknown session is 404", func(t *testing.T) {
		code, body := doRequest(t, f.srv, f.aliceToken, "GET", "/api/v1/sessions/"+uuid.NewString(), nil)
		require.Equal(t, http.StatusNotFound, code, "%v", body)
	})
}

// TestSessionReadNotebookInheritance pins the live, opt-in notebook-viewer
// inheritance: it grants reads only when the flag is set, and only view.
func TestSessionReadNotebookInheritance(t *testing.T) {
	f := setupSessionSharingFixture(t)
	notebookID := createNotebook(t, f.srv, f.aliceToken, "Read Inherit NB")
	grantACL(t, f.srv, f.orgID, "notebook", notebookID, "user", f.carolID, "view")

	inheritID := f.createSession(t, map[string]any{
		"notebook_id":                 notebookID,
		"share_with_notebook_viewers": true,
	})
	seedSessionMessage(t, f.srv, inheritID, "inherit")
	privateID := f.createSession(t, map[string]any{"notebook_id": notebookID})
	seedSessionMessage(t, f.srv, privateID, "private")

	// Carol can view the agent — the old path let her read both sessions.
	grantACL(t, f.srv, f.orgID, "agent", f.agentID, "user", f.carolID, "view")

	t.Run("notebook viewer reads inherited session", func(t *testing.T) {
		code, body := doRequest(t, f.srv, f.carolToken, "GET", "/api/v1/sessions/"+inheritID, nil)
		require.Equal(t, http.StatusOK, code, "%v", body)
	})
	t.Run("notebook viewer denied when flag is off", func(t *testing.T) {
		code, body := doRequest(t, f.srv, f.carolToken, "GET", "/api/v1/sessions/"+privateID, nil)
		require.Equal(t, http.StatusForbidden, code, "%v", body)
	})
	t.Run("member without notebook view denied on inherited session", func(t *testing.T) {
		code, body := doRequest(t, f.srv, f.bobToken, "GET", "/api/v1/sessions/"+inheritID, nil)
		require.Equal(t, http.StatusForbidden, code, "%v", body)
	})
	t.Run("notebook viewer cannot rename inherited session", func(t *testing.T) {
		code, body := rawRequest(t, f.srv, f.carolToken, "PATCH",
			"/api/v1/sessions/"+inheritID+"/title",
			map[string]any{"title": "carol-renamed"})
		require.Equal(t, http.StatusForbidden, code, "%v", body)
	})
	t.Run("notebook viewer cannot upload to inherited session", func(t *testing.T) {
		code, body := uploadSessionAttachment(t, f.srv, f.carolToken, inheritID)
		require.Equal(t, http.StatusForbidden, code, "%v", body)
	})
}

// TestSessionRenameAuthorization: only the session owner (or admin mode) can
// rename; direct shares stay read-only, and the owner does not need agent edit.
func TestSessionRenameAuthorization(t *testing.T) {
	f := setupSessionSharingFixture(t)
	sessionID := f.createSession(t, map[string]any{})

	// Bob can edit the agent itself but only view the session.
	grantACL(t, f.srv, f.orgID, "agent_session", sessionID, "user", f.bobID, "view")
	grantACL(t, f.srv, f.orgID, "agent", f.agentID, "user", f.bobID, "edit")

	code, body := doRequest(t, f.srv, f.bobToken, "PATCH",
		"/api/v1/sessions/"+sessionID+"/title", map[string]any{"title": "bob-edited"})
	require.Equal(t, http.StatusForbidden, code, "%v", body)

	var title *string
	require.NoError(t, f.srv.DB().Pool.QueryRow(context.Background(),
		`SELECT title FROM agent_sessions WHERE id = $1`, sessionID).Scan(&title))
	if title != nil {
		require.NotEqual(t, "bob-edited", *title, "a shared viewer must not rename the session")
	}

	// The owner renames with no agent ACL at all.
	revokeAgentACLs(t, f.srv, f.agentID, f.aliceID)
	code, body = doRequest(t, f.srv, f.aliceToken, "PATCH",
		"/api/v1/sessions/"+sessionID+"/title", map[string]any{"title": "owner-renamed"})
	require.Equal(t, http.StatusOK, code, "%v", body)

	require.NoError(t, f.srv.DB().Pool.QueryRow(context.Background(),
		`SELECT title FROM agent_sessions WHERE id = $1`, sessionID).Scan(&title))
	require.NotNil(t, title)
	require.Equal(t, "owner-renamed", *title)
}

// TestSessionAttachmentAuthorization: upload needs session edit (owner only),
// fetch needs session view, and neither is satisfied by agent ACLs.
func TestSessionAttachmentAuthorization(t *testing.T) {
	f := setupSessionSharingFixture(t)
	sessionID := f.createSession(t, map[string]any{})

	grantACL(t, f.srv, f.orgID, "agent_session", sessionID, "user", f.bobID, "view")
	grantACL(t, f.srv, f.orgID, "agent", f.agentID, "user", f.bobID, "edit")

	// The owner uploads with no agent ACL at all.
	revokeAgentACLs(t, f.srv, f.agentID, f.aliceID)
	code, resp := uploadSessionAttachment(t, f.srv, f.aliceToken, sessionID)
	require.Equal(t, http.StatusCreated, code, "%v", resp)
	attID, _ := resp["id"].(string)
	require.NotEmpty(t, attID)

	t.Run("shared viewer with agent edit cannot upload", func(t *testing.T) {
		code, body := uploadSessionAttachment(t, f.srv, f.bobToken, sessionID)
		require.Equal(t, http.StatusForbidden, code, "%v", body)
	})
	t.Run("owner fetches", func(t *testing.T) {
		code, _ := fetchAgentAttachment(t, f.srv, f.aliceToken, attID)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("shared viewer fetches", func(t *testing.T) {
		code, _ := fetchAgentAttachment(t, f.srv, f.bobToken, attID)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("unrelated member fetch denied", func(t *testing.T) {
		code, _ := fetchAgentAttachment(t, f.srv, f.carolToken, attID)
		require.Equal(t, http.StatusForbidden, code)
	})
	t.Run("unknown session upload is 404", func(t *testing.T) {
		code, body := uploadSessionAttachment(t, f.srv, f.aliceToken, uuid.NewString())
		require.Equal(t, http.StatusNotFound, code, "%v", body)
	})
	t.Run("unknown attachment fetch is 404", func(t *testing.T) {
		code, _ := fetchAgentAttachment(t, f.srv, f.aliceToken, uuid.NewString())
		require.Equal(t, http.StatusNotFound, code)
	})
}

// TestSubagentMessagesAuthorization: subagent detail is readable whenever the
// parent session is; org membership alone is no longer enough.
func TestSubagentMessagesAuthorization(t *testing.T) {
	f := setupSessionSharingFixture(t)
	sessionID := f.createSession(t, map[string]any{})
	taskID := uuid.NewString()
	_, err := f.srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO subagent_tasks (id, parent_session_id, goal, status, result, created_at, completed_at)
		VALUES ($1, $2, 'goal', 'completed', '{}', NOW(), NOW())
	`, taskID, sessionID)
	require.NoError(t, err)
	_, err = f.srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO subagent_messages (subagent_task_id, role, content, created_at)
		VALUES ($1, 'assistant', 'subagent says hi', NOW())
	`, taskID)
	require.NoError(t, err)

	t.Run("owner reads", func(t *testing.T) {
		code, body := getSubagentMessages(t, f.srv, f.aliceToken, taskID)
		require.Equal(t, http.StatusOK, code, "%v", body)
	})
	t.Run("shared viewer reads", func(t *testing.T) {
		grantACL(t, f.srv, f.orgID, "agent_session", sessionID, "user", f.bobID, "view")
		code, body := getSubagentMessages(t, f.srv, f.bobToken, taskID)
		require.Equal(t, http.StatusOK, code, "%v", body)
	})
	t.Run("unrelated org member denied", func(t *testing.T) {
		code, body := getSubagentMessages(t, f.srv, f.carolToken, taskID)
		require.Equal(t, http.StatusForbidden, code, "%v", body)
	})
	t.Run("missing task is 404", func(t *testing.T) {
		code, body := getSubagentMessages(t, f.srv, f.aliceToken, uuid.NewString())
		require.Equal(t, http.StatusNotFound, code, "%v", body)
	})
	t.Run("cross-org is 404", func(t *testing.T) {
		otherToken := registerAndGetToken(t, f.srv,
			fmt.Sprintf("session-share-other-%d@example.com", time.Now().UnixNano()),
			"Session Share Other Org")
		code, body := getSubagentMessages(t, f.srv, otherToken, taskID)
		require.Equal(t, http.StatusNotFound, code, "%v", body)
	})
}
