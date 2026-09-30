package api

// End-to-end coverage for create-with-inheritance: a session created with
// share_with_notebook_viewers must be readable by a notebook viewer
// immediately after the 201. The external api_test package cannot reach
// checkSessionPermission, so this test lives in package api and reuses the
// session-permission helpers from permissions_internal_test.go.

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
