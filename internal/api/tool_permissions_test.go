package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestToolTestEndpointRequiresUsePermission pins the authorization gate on the
// tool probe: since the endpoint decrypts stored secret headers and replays
// them, invoking it is a `use` action, not something a `view`-only member may
// trigger. The multi-resource permission fixture doesn't cover tools, so this
// builds the smallest equivalent: the admin creator (creator ACL carries use)
// vs. a second org member granted view only.
func TestToolTestEndpointRequiresUsePermission(t *testing.T) {
	srv := setupTestServer(t)
	ownerEmail := fmt.Sprintf("tool-perm-owner-%d@example.com", time.Now().UnixNano())
	ownerToken := registerAndGetToken(t, srv, ownerEmail, "Tool Perm Org")

	var ownerID, orgID string
	if err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT u.id, om.org_id FROM users u JOIN org_members om ON om.user_id = u.id WHERE u.email = $1`,
		ownerEmail).Scan(&ownerID, &orgID); err != nil {
		t.Fatalf("lookup owner: %v", err)
	}

	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer hook.Close()

	toolID := createWebhookTool(t, srv, ownerToken, map[string]any{
		"url":     hook.URL,
		"method":  "POST",
		"headers": map[string]string{"X-Api-Key": "perm-secret"},
		"secrets": []string{"X-Api-Key"},
	})

	// Second member of the same org with exactly the `view` action on the tool.
	viewerID := insertUser(t, srv, fmt.Sprintf("tool-perm-viewer-%d@test.com", time.Now().UnixNano()), "Tool Viewer")
	addOrgMember(t, srv, orgID, viewerID, "editor")
	viewerToken := issueToken(t, viewerID, orgID, "editor")
	if _, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'tool', $2::uuid, 'user', $3, ARRAY['view'])
	`, orgID, toolID, viewerID); err != nil {
		t.Fatalf("insert viewer acl: %v", err)
	}

	probe := func(token string) int {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/v1/tools/"+toolID+"/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	// The view-only member must be rejected before the handler runs.
	if code := probe(viewerToken); code != http.StatusForbidden {
		t.Fatalf("view-only member probing the tool: expected 403, got %d", code)
	}
	// The creator's seeded ACL carries `use`, so the probe is allowed.
	if code := probe(ownerToken); code != http.StatusOK {
		t.Fatalf("owner probing the tool: expected 200, got %d", code)
	}
}
