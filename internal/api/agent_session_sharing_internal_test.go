package api

// White-box session-sharing tests. The create-with-inheritance test asserts a
// session created with share_with_notebook_viewers is readable by a notebook
// viewer immediately after the 201; the normalization tests pin the share-list
// validation (dedup, owner drop, cap, error classification) without going
// through the HTTP layer. The external api_test package cannot reach these
// helpers, so the tests live in package api and reuse the session-permission
// helpers from permissions_internal_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateSessionInheritanceVisibleToNotebookViewer(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "create-inherit")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "admin")
	viewerID := insertSessionPermUser(t, s, "viewer")
	addSessionPermMember(t, s, orgID, viewerID, "editor")

	notebookID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
		notebookID.String(), orgID.String(), "Inherit Notebook", ownerID.String())
	require.NoError(t, err)
	grantSessionPermACL(t, s, orgID, "notebook", notebookID, viewerID, []string{"view"})

	agentID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO agents (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		agentID.String(), orgID.String(), "Inherit Agent", ownerID.String())
	require.NoError(t, err)

	token, err := s.jwt.Issue(ownerID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{
		"notebook_id":                 notebookID.String(),
		"share_with_notebook_viewers": true,
	})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/api/v1/agents/"+agentID.String()+"/session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	sessionID, _ := resp["session_id"].(string)
	require.NotEmpty(t, sessionID)

	// Immediately readable by a notebook viewer, with no follow-up ACL write.
	allowed, err := s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID, "view")
	require.NoError(t, err)
	require.True(t, allowed, "notebook viewer must read the just-created inherited session")

	// Inheritance remains view-only.
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID, "edit")
	require.NoError(t, err)
	require.False(t, allowed)
}

// TestNormalizeSessionShareEntriesDedupAndOwnerDrop pins the normalization
// result directly, independently of the acl_entries ON CONFLICT clause: the
// owner is dropped even when named with actions, duplicates collapse to a
// single entry, and the output keeps first-seen order.
func TestNormalizeSessionShareEntriesDedupAndOwnerDrop(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "normalize")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	memberID := insertSessionPermUser(t, s, "member")
	addSessionPermMember(t, s, orgID, memberID, "editor")

	groupID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Normalize Group")
	require.NoError(t, err)

	got, err := s.normalizeSessionShareEntries(ctx, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "user", SubjectID: memberID.String()},
		{SubjectType: "user", SubjectID: ownerID.String(), Actions: []string{"edit"}},
		{SubjectType: "user", SubjectID: memberID.String(), Actions: []string{"view"}},
		{SubjectType: "org_role", SubjectID: "everyone"},
		{SubjectType: "group", SubjectID: groupID.String(), Actions: []string{"view"}},
	})
	require.NoError(t, err)
	require.Equal(t, []aclEntryInput{
		{SubjectType: "user", SubjectID: memberID.String(), Actions: []string{"view"}},
		{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view"}},
		{SubjectType: "group", SubjectID: groupID.String(), Actions: []string{"view"}},
	}, got, "owner entries must be dropped and duplicates collapsed before any insert")
}

// TestNormalizeSessionShareEntriesCap asserts the cap is enforced before the
// per-entry work: entries naming the owner are otherwise silently dropped, so
// only the cap can make this list fail.
func TestNormalizeSessionShareEntriesCap(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	ownerID := uuid.NewString()

	entries := make([]aclEntryInput, maxSessionShares+1)
	for i := range entries {
		entries[i] = aclEntryInput{SubjectType: "user", SubjectID: ownerID, Actions: []string{"view"}}
	}

	_, err := s.normalizeSessionShareEntries(ctx, ownerID, uuid.NewString(), entries)
	require.Error(t, err)
	require.ErrorIs(t, err, errInvalidSessionShare, "oversized share lists must be rejected as invalid input")
}

// TestSessionShareDatabaseFailureMapsToServerError proves a database outage
// during share validation is classified as a server error, never as a 400.
func TestSessionShareDatabaseFailureMapsToServerError(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.normalizeSessionShareEntries(ctx, uuid.NewString(), uuid.NewString(), []aclEntryInput{
		{SubjectType: "user", SubjectID: uuid.NewString(), Actions: []string{"view"}},
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errInvalidSessionShare, "a database failure must not be classified as invalid input")

	rec := httptest.NewRecorder()
	writeSessionShareError(rec, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code,
		"a database failure must map to 500: %s", rec.Body.String())
}
