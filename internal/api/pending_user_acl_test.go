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

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// aclEntryJSON is the test-side shape of models.ACLEntry plus the synthesized
// pending marker.
type aclEntryJSON struct {
	ID          string   `json:"id"`
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
	Pending     bool     `json:"pending"`
}

func getACLViaAPI(t *testing.T, srv *api.Server, token, resourceType, resourceID string) []aclEntryJSON {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/acl/"+resourceType+"/"+resourceID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var entries []aclEntryJSON
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	return entries
}

func putACLViaAPI(t *testing.T, srv *api.Server, token, resourceType, resourceID string, entries []map[string]any) (int, []aclEntryJSON) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"entries": entries})
	req := httptest.NewRequest("PUT", "/api/v1/acl/"+resourceType+"/"+resourceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out []aclEntryJSON
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&out)
	}
	return rec.Code, out
}

func TestACLGetIncludesPendingRows(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("acl-get-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending GET Org")
	nbID := createNotebook(t, srv, token, "Pending GET NB")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)

	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions)
		VALUES ($1, 'notebook', $2, 'future@example.com', ARRAY['view','edit'])`, orgID, nbID)
	require.NoError(t, err)

	entries := getACLViaAPI(t, srv, token, "notebook", nbID)
	require.Equal(t, "user", entries[0].SubjectType, "real entries come first")

	var pending *aclEntryJSON
	for i := range entries {
		if entries[i].SubjectType == "pending_user" {
			pending = &entries[i]
			break
		}
	}
	require.NotNil(t, pending, "the staged row must be returned")
	require.Equal(t, "future@example.com", pending.SubjectID)
	require.True(t, pending.Pending)
	require.NotEmpty(t, pending.ID, "the request gets the pending row's UUID so it can key/remove it")
	require.ElementsMatch(t, []string{"view", "edit"}, pending.Actions)
}
