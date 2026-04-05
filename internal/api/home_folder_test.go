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

func TestRegister_CreatesHomeFolder(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("home-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Home Org")

	// Get user ID
	req := httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get me: %d %s", rec.Code, rec.Body.String())
	}
	var me map[string]any
	json.NewDecoder(rec.Body).Decode(&me)
	userID := me["id"].(string)

	// Verify home folder exists
	var folderID string
	err := db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true`,
		userID,
	).Scan(&folderID)
	if err != nil {
		t.Fatalf("no home folder found for user: %v", err)
	}

	// Verify ACL entry was seeded for the home folder
	var count int
	db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM acl_entries
		 WHERE resource_type = 'folder' AND resource_id = $1::uuid
		   AND subject_type = 'user' AND subject_id = $2`,
		folderID, userID,
	).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 ACL entry for home folder, got %d", count)
	}
}
