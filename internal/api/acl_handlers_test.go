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

func TestACLGetAndPut(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("acl-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "ACL Org2")

	// Get current user ID
	meReq := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	srv.ServeHTTP(meRec, meReq)
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	userID := me["id"].(string)

	// Create a notebook
	nbID := createNotebook(t, srv, token, "ACL Test NB")

	// GET ACL — initially empty
	req := httptest.NewRequest("GET", "/api/v1/acl/notebook/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get acl: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []any
	json.NewDecoder(rec.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Errorf("expected 1 initial ACL entry (creator), got %d", len(entries))
	}

	// PUT ACL — set one entry
	body, _ := json.Marshal(map[string]any{
		"entries": []map[string]any{
			{"subject_type": "user", "subject_id": userID, "actions": []string{"view", "edit"}},
		},
	})
	req2 := httptest.NewRequest("PUT", "/api/v1/acl/notebook/"+nbID, bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("put acl: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// GET ACL again — should have 1 entry
	req3 := httptest.NewRequest("GET", "/api/v1/acl/notebook/"+nbID, nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("get acl after put: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}
	var entries2 []any
	json.NewDecoder(rec3.Body).Decode(&entries2)
	if len(entries2) != 1 {
		t.Errorf("expected 1 ACL entry after PUT, got %d", len(entries2))
	}

	// PUT ACL with empty entries — clears all
	clearBody, _ := json.Marshal(map[string]any{"entries": []any{}})
	req4 := httptest.NewRequest("PUT", "/api/v1/acl/notebook/"+nbID, bytes.NewReader(clearBody))
	req4.Header.Set("Content-Type", "application/json")
	req4.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("clear acl: expected 200, got %d: %s", rec4.Code, rec4.Body.String())
	}
	var cleared []any
	json.NewDecoder(rec4.Body).Decode(&cleared)
	if len(cleared) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(cleared))
	}
}
