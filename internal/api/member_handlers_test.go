package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMemberManagement(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()

	// 1. Register admin user and get token
	adminEmail := fmt.Sprintf("member-admin-%d@example.com", ts)
	adminToken := registerAndGetToken(t, srv, adminEmail, "Member Test Org")

	// 2. Register a second user (they create their own org, but we'll invite them)
	secondEmail := fmt.Sprintf("member-second-%d@example.com", ts)
	registerAndGetToken(t, srv, secondEmail, "Second User Org")

	// 3. GET /api/v1/members — should return at least 1 member (the admin)
	req := httptest.NewRequest("GET", "/api/v1/members", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list members: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var members []map[string]any
	json.NewDecoder(rec.Body).Decode(&members)
	if len(members) < 1 {
		t.Fatalf("list members: expected at least 1 member, got %d", len(members))
	}

	// 4. POST /api/v1/members — invite second user
	body, _ := json.Marshal(map[string]string{"email": secondEmail, "role": "non-admin"})
	req = httptest.NewRequest("POST", "/api/v1/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("invite member: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. GET /api/v1/members again — should now show 2 members
	req = httptest.NewRequest("GET", "/api/v1/members", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list members after invite: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var membersAfterInvite []map[string]any
	json.NewDecoder(rec.Body).Decode(&membersAfterInvite)
	if len(membersAfterInvite) != 2 {
		t.Fatalf("list members after invite: expected 2 members, got %d", len(membersAfterInvite))
	}

	// Find the second user's ID from the member list
	var secondUserID string
	for _, m := range membersAfterInvite {
		if m["email"].(string) == secondEmail {
			secondUserID = m["user_id"].(string)
			break
		}
	}
	if secondUserID == "" {
		t.Fatal("could not find second user in member list")
	}

	// Also grab the admin's own user_id
	var adminUserID string
	for _, m := range membersAfterInvite {
		if m["email"].(string) == adminEmail {
			adminUserID = m["user_id"].(string)
			break
		}
	}
	if adminUserID == "" {
		t.Fatal("could not find admin user in member list")
	}

	// 6. PUT /api/v1/members/{user_id} — change second user's role to "non-admin"
	body, _ = json.Marshal(map[string]string{"role": "non-admin"})
	req = httptest.NewRequest("PUT", "/api/v1/members/"+secondUserID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("update role: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// 7. DELETE /api/v1/members/{user_id} — remove the second user
	req = httptest.NewRequest("DELETE", "/api/v1/members/"+secondUserID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// 8. Cannot change own role — should return 400
	body, _ = json.Marshal(map[string]string{"role": "non-admin"})
	req = httptest.NewRequest("PUT", "/api/v1/members/"+adminUserID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("change own role: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// 9. Cannot remove self — should return 400
	req = httptest.NewRequest("DELETE", "/api/v1/members/"+adminUserID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("remove self: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
