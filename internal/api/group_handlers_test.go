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

func TestGroupCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("group-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Group Org")

	// Create group
	body, _ := json.Marshal(map[string]string{"name": "Analytics"})
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var g map[string]any
	json.NewDecoder(rec.Body).Decode(&g)
	groupID := g["id"].(string)

	// List groups
	req2 := httptest.NewRequest("GET", "/api/v1/groups", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list groups: expected 200, got %d", rec2.Code)
	}
	var groups []any
	json.NewDecoder(rec2.Body).Decode(&groups)
	if len(groups) == 0 {
		t.Error("expected at least one group")
	}

	// Get current user ID
	meReq := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	srv.ServeHTTP(meRec, meReq)
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	userID := me["id"].(string)

	// Add member
	memberBody, _ := json.Marshal(map[string]string{"user_id": userID})
	req3 := httptest.NewRequest("POST", "/api/v1/groups/"+groupID+"/members", bytes.NewReader(memberBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// List members
	req3b := httptest.NewRequest("GET", "/api/v1/groups/"+groupID+"/members", nil)
	req3b.Header.Set("Authorization", "Bearer "+token)
	rec3b := httptest.NewRecorder()
	srv.ServeHTTP(rec3b, req3b)
	if rec3b.Code != http.StatusOK {
		t.Fatalf("list members: expected 200, got %d: %s", rec3b.Code, rec3b.Body.String())
	}
	var members []any
	json.NewDecoder(rec3b.Body).Decode(&members)
	if len(members) != 1 {
		t.Errorf("expected 1 member, got %d", len(members))
	}

	// Remove member
	req4 := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID+"/members/"+userID, nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNoContent {
		t.Fatalf("remove member: expected 204, got %d: %s", rec4.Code, rec4.Body.String())
	}

	// Rename group
	renameBody, _ := json.Marshal(map[string]string{"name": "Data Analytics"})
	req5 := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(renameBody))
	req5.Header.Set("Content-Type", "application/json")
	req5.Header.Set("Authorization", "Bearer "+token)
	rec5 := httptest.NewRecorder()
	srv.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec5.Code, rec5.Body.String())
	}

	// Delete group
	req6 := httptest.NewRequest("DELETE", "/api/v1/groups/"+groupID, nil)
	req6.Header.Set("Authorization", "Bearer "+token)
	rec6 := httptest.NewRecorder()
	srv.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusNoContent {
		t.Fatalf("delete group: expected 204, got %d: %s", rec6.Code, rec6.Body.String())
	}
}
