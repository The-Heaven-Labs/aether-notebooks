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
	body, _ := json.Marshal(map[string]any{"name": "Analytics", "display_name": "Analytics Label"})
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
	if g["display_name"] != "Analytics Label" {
		t.Fatalf("expected create to return display_name, got %v", g["display_name"])
	}

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
	var listed map[string]any
	for _, raw := range groups {
		if m, ok := raw.(map[string]any); ok && m["id"] == groupID {
			listed = m
		}
	}
	if listed == nil {
		t.Fatal("created group missing from list")
	}
	if listed["display_name"] != "Analytics Label" {
		t.Fatalf("expected list to return display_name, got %v", listed["display_name"])
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

	// Display names: set on update, rendered alongside the identity name
	labelBody, _ := json.Marshal(map[string]any{"name": "Analytics", "display_name": "Data Analysts Infra"})
	labelReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(labelBody))
	labelReq.Header.Set("Content-Type", "application/json")
	labelReq.Header.Set("Authorization", "Bearer "+token)
	labelRec := httptest.NewRecorder()
	srv.ServeHTTP(labelRec, labelReq)
	if labelRec.Code != http.StatusOK {
		t.Fatalf("set display name: expected 200, got %d: %s", labelRec.Code, labelRec.Body.String())
	}
	var labeled map[string]any
	json.NewDecoder(labelRec.Body).Decode(&labeled)
	if labeled["display_name"] != "Data Analysts Infra" {
		t.Fatalf("expected display_name to be set, got %v", labeled["display_name"])
	}
	if labeled["name"] != "Analytics" {
		t.Fatalf("expected name unchanged, got %v", labeled["name"])
	}

	// Whitespace-only label clears to NULL
	spaceBody, _ := json.Marshal(map[string]any{"display_name": "   "})
	spaceReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(spaceBody))
	spaceReq.Header.Set("Content-Type", "application/json")
	spaceReq.Header.Set("Authorization", "Bearer "+token)
	spaceRec := httptest.NewRecorder()
	srv.ServeHTTP(spaceRec, spaceReq)
	if spaceRec.Code != http.StatusOK {
		t.Fatalf("whitespace display name: expected 200, got %d: %s", spaceRec.Code, spaceRec.Body.String())
	}
	var spaced map[string]any
	json.NewDecoder(spaceRec.Body).Decode(&spaced)
	if spaced["display_name"] != nil {
		t.Fatalf("expected whitespace display_name cleared to null, got %v", spaced["display_name"])
	}

	// Label-only update keeps the name
	clearBody, _ := json.Marshal(map[string]any{"display_name": ""})
	clearReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(clearBody))
	clearReq.Header.Set("Content-Type", "application/json")
	clearReq.Header.Set("Authorization", "Bearer "+token)
	clearRec := httptest.NewRecorder()
	srv.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear display name: expected 200, got %d: %s", clearRec.Code, clearRec.Body.String())
	}
	var cleared map[string]any
	json.NewDecoder(clearRec.Body).Decode(&cleared)
	if cleared["display_name"] != nil {
		t.Fatalf("expected cleared display_name null, got %v", cleared["display_name"])
	}
	if cleared["name"] != "Analytics" {
		t.Fatalf("expected name kept on label-only update, got %v", cleared["name"])
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

func TestUpdateEveryoneGroupRejectsNameAndLabel(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("everyone-label-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Everyone Label Org")

	listReq := httptest.NewRequest("GET", "/api/v1/groups", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	srv.ServeHTTP(listRec, listReq)
	var groups []map[string]any
	json.NewDecoder(listRec.Body).Decode(&groups)
	var everyoneID string
	for _, g := range groups {
		if g["name"] == "Everyone" {
			everyoneID = g["id"].(string)
		}
	}
	if everyoneID == "" {
		t.Fatal("expected the org's Everyone group")
	}

	labelBody, _ := json.Marshal(map[string]any{"display_name": "All Staff"})
	labelReq := httptest.NewRequest("PUT", "/api/v1/groups/"+everyoneID, bytes.NewReader(labelBody))
	labelReq.Header.Set("Content-Type", "application/json")
	labelReq.Header.Set("Authorization", "Bearer "+token)
	labelRec := httptest.NewRecorder()
	srv.ServeHTTP(labelRec, labelReq)
	if labelRec.Code != http.StatusBadRequest {
		t.Fatalf("label on Everyone: expected 400, got %d: %s", labelRec.Code, labelRec.Body.String())
	}

	renameBody, _ := json.Marshal(map[string]any{"name": "Staff"})
	renameReq := httptest.NewRequest("PUT", "/api/v1/groups/"+everyoneID, bytes.NewReader(renameBody))
	renameReq.Header.Set("Content-Type", "application/json")
	renameReq.Header.Set("Authorization", "Bearer "+token)
	renameRec := httptest.NewRecorder()
	srv.ServeHTTP(renameRec, renameReq)
	if renameRec.Code != http.StatusBadRequest {
		t.Fatalf("rename Everyone: expected 400, got %d: %s", renameRec.Code, renameRec.Body.String())
	}
}
