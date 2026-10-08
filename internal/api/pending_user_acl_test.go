package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestACLPutStagesAndReplacesPendingEntries(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("acl-put-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending PUT Org")
	nbID := createNotebook(t, srv, token, "Pending PUT NB")
	userID := userIDFromToken(t, srv, token)

	putCode, inserted := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "user", "subject_id": userID, "actions": []string{"view", "edit"}},
		{"subject_type": "pending_user", "subject_id": "Future.User@Example.com", "actions": []string{"view", "edit"}},
		{"subject_type": "pending_user", "subject_id": "future.user@example.com", "actions": []string{"run"}},
		{"subject_type": "pending_user", "subject_id": "not-an-email", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, putCode)
	require.Len(t, inserted, 2, "invalid pending emails are skipped")

	var pending *aclEntryJSON
	for i := range inserted {
		if inserted[i].SubjectType == "pending_user" {
			pending = &inserted[i]
		}
	}
	require.NotNil(t, pending)
	require.Equal(t, "future.user@example.com", pending.SubjectID, "emails are lowercased")
	require.True(t, pending.Pending)
	require.ElementsMatch(t, []string{"view", "edit", "run"}, pending.Actions, "duplicate submissions union actions")

	// GET round-trips the staged row with the same pending row UUID.
	got := getACLViaAPI(t, srv, token, "notebook", nbID)
	found := false
	for _, e := range got {
		if e.SubjectType == "pending_user" {
			found = true
			require.Equal(t, pending.ID, e.ID)
		}
	}
	require.True(t, found)

	// The grant audit reuses acl.granted with the pending marker.
	var metaRaw []byte
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = 'acl.granted' AND metadata->>'subject_type' = 'pending_user'
		ORDER BY id DESC LIMIT 1`).Scan(&metaRaw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaRaw, &meta))
	require.Equal(t, true, meta["pending"])

	// Replace semantics: a PUT that omits staged rows removes them.
	clearCode, cleared := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{})
	require.Equal(t, http.StatusOK, clearCode)
	require.Empty(t, cleared)
	require.Empty(t, getACLViaAPI(t, srv, token, "notebook", nbID))
}

// TestACLPutConvertsExistingMemberPendingEmailToUser pins amendment A2: a
// pending_user entry whose email already belongs to an org member is written
// as a real user entry, never staged (a staged row for a member could never
// materialize), mirroring handleAddPendingGroupMembers.
func TestACLPutConvertsExistingMemberPendingEmailToUser(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("acl-put-member-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Member Org")
	nbID := createNotebook(t, srv, token, "Pending Member NB")
	userID := userIDFromToken(t, srv, token)

	// Uppercase input proves the membership lookup is case-insensitive.
	code, inserted := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": strings.ToUpper(email), "actions": []string{"view", "edit"}},
	})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, inserted, 1)
	require.Equal(t, "user", inserted[0].SubjectType)
	require.Equal(t, userID, inserted[0].SubjectID)
	require.False(t, inserted[0].Pending)
	require.ElementsMatch(t, []string{"view", "edit"}, inserted[0].Actions)

	var staged int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pending_acl_entries
		WHERE resource_type = 'notebook' AND resource_id = $1::uuid AND lower(email) = lower($2)`,
		nbID, email).Scan(&staged))
	require.Zero(t, staged, "an existing member's email must not be staged")

	// A direct entry and a pending email naming the same member merge into one
	// user entry (the unique constraint forbids two entries for one subject).
	code, inserted = putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "user", "subject_id": userID, "actions": []string{"view"}},
		{"subject_type": "pending_user", "subject_id": email, "actions": []string{"edit"}},
	})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, inserted, 1)
	require.Equal(t, "user", inserted[0].SubjectType)
	require.ElementsMatch(t, []string{"view", "edit"}, inserted[0].Actions)
}

// TestACLPutUpdatesPendingAuditOnResubmit pins the replace path for staged
// rows: resubmitting the same email with changed actions audits an
// acl.updated event carrying the pending marker.
func TestACLPutUpdatesPendingAuditOnResubmit(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("acl-put-update-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Update Org")
	nbID := createNotebook(t, srv, token, "Pending Update NB")
	orgID := orgIDFromUser(t, srv, userIDFromToken(t, srv, token))

	code, _ := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future.update@example.com", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, code)

	code, inserted := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future.update@example.com", "actions": []string{"view", "edit"}},
	})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, inserted, 1)
	require.Equal(t, "pending_user", inserted[0].SubjectType)
	require.ElementsMatch(t, []string{"view", "edit"}, inserted[0].Actions)

	var metaRaw []byte
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'acl.updated' AND metadata->>'subject_type' = 'pending_user'
		ORDER BY id DESC LIMIT 1`, orgID).Scan(&metaRaw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaRaw, &meta))
	require.Equal(t, true, meta["pending"])
	require.ElementsMatch(t, []any{"view"}, meta["old_actions"])
	require.ElementsMatch(t, []any{"view", "edit"}, meta["new_actions"])
}

// TestACLPutRevokesPendingAuditOnClear pins the other half of the replace
// semantics: dropping a staged row audits an acl.revoked event with the
// pending marker and the revoked actions.
func TestACLPutRevokesPendingAuditOnClear(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("acl-put-revoke-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Revoke Org")
	nbID := createNotebook(t, srv, token, "Pending Revoke NB")
	orgID := orgIDFromUser(t, srv, userIDFromToken(t, srv, token))

	code, _ := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future.revoke@example.com", "actions": []string{"view", "edit"}},
	})
	require.Equal(t, http.StatusOK, code)

	code, cleared := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{})
	require.Equal(t, http.StatusOK, code)
	require.Empty(t, cleared)

	var metaRaw []byte
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'acl.revoked' AND metadata->>'subject_type' = 'pending_user'
		ORDER BY id DESC LIMIT 1`, orgID).Scan(&metaRaw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaRaw, &meta))
	require.Equal(t, true, meta["pending"])
	require.ElementsMatch(t, []any{"view", "edit"}, meta["actions"])
}

// TestACLNonMemberEmailStaysStaged pins design §5.1.6: a registered user who
// is not a member of the caller's org is not a member entry, so the email is
// staged until they join that org. Unknown subject types are skipped instead
// of reaching the acl_entries check constraint.
func TestACLNonMemberEmailStaysStaged(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("acl-put-nonmember-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending Nonmember Org")
	nbID := createNotebook(t, srv, token, "Pending Nonmember NB")
	userID := userIDFromToken(t, srv, token)

	otherEmail := fmt.Sprintf("acl-other-org-%d@example.com", time.Now().UnixNano())
	_ = registerAndGetToken(t, srv, otherEmail, "Pending Other Org")

	code, inserted := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": strings.ToUpper(otherEmail), "actions": []string{"view"}},
		{"subject_type": "widget", "subject_id": userID, "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, code)
	require.Len(t, inserted, 1, "unknown subject types are skipped")
	require.Equal(t, "pending_user", inserted[0].SubjectType)
	require.True(t, inserted[0].Pending)
	require.Equal(t, otherEmail, inserted[0].SubjectID)

	got := getACLViaAPI(t, srv, token, "notebook", nbID)
	found := false
	for _, e := range got {
		if e.SubjectType == "pending_user" && e.SubjectID == otherEmail {
			found = true
		}
	}
	require.True(t, found, "a non-member email stays staged after GET")
}
