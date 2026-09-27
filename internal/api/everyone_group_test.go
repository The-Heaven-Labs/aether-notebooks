package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEveryoneGroupExists(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("everyone-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Everyone Org")

	req := httptest.NewRequest("GET", "/api/v1/groups", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var groups []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&groups); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := false
	for _, g := range groups {
		if g["name"] == "Everyone" {
			found = true
			if g["source"] != "system" {
				t.Errorf("Everyone source = %v, want system", g["source"])
			}
		}
	}
	if !found {
		t.Error("expected 'Everyone' group to exist after org creation")
	}
}

func TestEveryoneGroupIsUndeletableWithForce(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("everyone-force-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Everyone Force Org")

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

	req := httptest.NewRequest("DELETE", "/api/v1/groups/"+everyoneID+"?force=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete Everyone with force: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var exists bool
	if err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM groups WHERE id=$1)`, everyoneID,
	).Scan(&exists); err != nil {
		t.Fatalf("check Everyone existence: %v", err)
	}
	if !exists {
		t.Fatal("blocked Everyone delete must not remove the row")
	}
}
