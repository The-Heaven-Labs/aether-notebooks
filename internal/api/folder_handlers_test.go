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
)

func TestFolderCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("folder-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Folder Org")

	// Create folder at root
	body, _ := json.Marshal(map[string]string{"name": "Engineering"})
	req := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	json.NewDecoder(rec.Body).Decode(&folder)
	folderID := folder["id"].(string)

	// Create sub-folder
	body2, _ := json.Marshal(map[string]any{"name": "Backend", "parent_id": folderID})
	req2 := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("X-AETHER-Admin-Mode", "true")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create sub-folder: expected 201, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var subFolder map[string]any
	json.NewDecoder(rec2.Body).Decode(&subFolder)
	subFolderID := subFolder["id"].(string)

	// Get root contents
	req3 := httptest.NewRequest("GET", "/api/v1/folders", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("X-AETHER-Admin-Mode", "true")
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("list root: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// Get folder contents (has sub-folder)
	req4 := httptest.NewRequest("GET", "/api/v1/folders/"+folderID, nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	req4.Header.Set("X-AETHER-Admin-Mode", "true")
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("get folder: expected 200, got %d: %s", rec4.Code, rec4.Body.String())
	}
	var contents map[string]any
	json.NewDecoder(rec4.Body).Decode(&contents)
	folders := contents["folders"].([]any)
	if len(folders) != 1 {
		t.Errorf("expected 1 sub-folder, got %d", len(folders))
	}

	// Get ancestors breadcrumb
	req5 := httptest.NewRequest("GET", "/api/v1/folders/"+subFolderID+"/ancestors", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	req5.Header.Set("X-AETHER-Admin-Mode", "true")
	rec5 := httptest.NewRecorder()
	srv.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("ancestors: expected 200, got %d: %s", rec5.Code, rec5.Body.String())
	}
	var ancestors []any
	json.NewDecoder(rec5.Body).Decode(&ancestors)
	if len(ancestors) != 2 { // Engineering + Backend
		t.Errorf("expected 2 ancestors, got %d", len(ancestors))
	}

	// Rename folder
	renameBody, _ := json.Marshal(map[string]string{"name": "Engineering Team"})
	req6 := httptest.NewRequest("PUT", "/api/v1/folders/"+folderID, bytes.NewReader(renameBody))
	req6.Header.Set("Content-Type", "application/json")
	req6.Header.Set("Authorization", "Bearer "+token)
	req6.Header.Set("X-AETHER-Admin-Mode", "true")
	rec6 := httptest.NewRecorder()
	srv.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec6.Code, rec6.Body.String())
	}

	// Delete sub-folder (empty)
	req7 := httptest.NewRequest("DELETE", "/api/v1/folders/"+subFolderID, nil)
	req7.Header.Set("Authorization", "Bearer "+token)
	req7.Header.Set("X-AETHER-Admin-Mode", "true")
	rec7 := httptest.NewRecorder()
	srv.ServeHTTP(rec7, req7)
	if rec7.Code != http.StatusNoContent {
		t.Fatalf("delete empty: expected 204, got %d: %s", rec7.Code, rec7.Body.String())
	}

	// Create a child so parent is non-empty
	body8, _ := json.Marshal(map[string]any{"name": "Child", "parent_id": folderID})
	req8 := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body8))
	req8.Header.Set("Content-Type", "application/json")
	req8.Header.Set("Authorization", "Bearer "+token)
	req8.Header.Set("X-AETHER-Admin-Mode", "true")
	rec8 := httptest.NewRecorder()
	srv.ServeHTTP(rec8, req8)
	if rec8.Code != http.StatusCreated {
		t.Fatalf("re-create child: expected 201, got %d: %s", rec8.Code, rec8.Body.String())
	}

	// Delete non-empty folder without force → 409
	req9 := httptest.NewRequest("DELETE", "/api/v1/folders/"+folderID, nil)
	req9.Header.Set("Authorization", "Bearer "+token)
	req9.Header.Set("X-AETHER-Admin-Mode", "true")
	rec9 := httptest.NewRecorder()
	srv.ServeHTTP(rec9, req9)
	if rec9.Code != http.StatusConflict {
		t.Fatalf("delete non-empty: expected 409, got %d: %s", rec9.Code, rec9.Body.String())
	}

	// Force delete
	req10 := httptest.NewRequest("DELETE", "/api/v1/folders/"+folderID+"?force=true", nil)
	req10.Header.Set("Authorization", "Bearer "+token)
	req10.Header.Set("X-AETHER-Admin-Mode", "true")
	rec10 := httptest.NewRecorder()
	srv.ServeHTTP(rec10, req10)
	if rec10.Code != http.StatusNoContent {
		t.Fatalf("force delete: expected 204, got %d: %s", rec10.Code, rec10.Body.String())
	}
}

func TestListHomeFolders_DeeplySharedNotebook(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	now := time.Now().UnixNano()

	// Create user A (org admin)
	emailA := fmt.Sprintf("deepparent-%d@test.com", now)
	tokenA := registerAndGetToken(t, srv, emailA, "Deep Org")
	userA := userIDFromToken(t, srv, tokenA)
	orgID := orgIDFromUser(t, srv, userA)

	// Create user B (org member)
	emailB := fmt.Sprintf("deepchild-%d@test.com", now)
	userB := insertUser(t, srv, emailB, "User B")
	addOrgMember(t, srv, orgID, userB, "editor")
	tokenB := issueToken(t, userB, orgID, "editor")

	// Ensure home folder for user B (user A's was created during registration)
	code, _ := doRequest(t, srv, tokenB, "POST", "/api/v1/users/me/home", nil)
	require.Equal(t, http.StatusCreated, code)

	// Get User A's home folder ID
	var homeAID string
	err := srv.DB().Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true`, userA,
	).Scan(&homeAID)
	require.NoError(t, err)

	// User A creates a subfolder under their home
	code, resp := doRequest(t, srv, tokenA, "POST", "/api/v1/folders", map[string]any{
		"name":      "My Subfolder",
		"parent_id": homeAID,
	})
	require.Equal(t, http.StatusCreated, code)
	subfolderID := resp["id"].(string)

	// User A creates a notebook in the subfolder
	code, resp = doRequest(t, srv, tokenA, "POST", "/api/v1/notebooks", map[string]any{
		"title":     "Deeply Shared Notebook",
		"folder_id": subfolderID,
	})
	require.Equal(t, http.StatusCreated, code)
	notebookID := resp["id"].(string)

	// User A shares the notebook with User B (add ACL entry directly)
	_, err = srv.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view'])`,
		orgID, notebookID, userB,
	)
	require.NoError(t, err)

	// User B lists home folders — should include User A's home
	req := httptest.NewRequest("GET", "/api/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var entries []map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &entries)
	require.NoError(t, err)

	// Verify User A's home folder appears in User B's home folder list
	var homeAEntry map[string]any
	found := false
	for _, entry := range entries {
		if entry["id"].(string) == homeAID {
			homeAEntry = entry
			found = true
			break
		}
	}
	require.True(t, found, "User B should see User A's home folder (contains a shared notebook 2 levels deep)")

	// Verify the subfolder appears under User A's home folder
	subFolders, ok := homeAEntry["sub_folders"].([]any)
	require.True(t, ok, "homeA entry should have sub_folders")
	require.Equal(t, 1, len(subFolders), "homeA should have 1 subfolder visible to User B")

	subFolderEntry := subFolders[0].(map[string]any)
	require.Equal(t, subfolderID, subFolderEntry["id"].(string), "subfolder ID should match")

	// Also verify User B can get the contents of the subfolder (which contains the shared notebook)
	req2 := httptest.NewRequest("GET", "/api/v1/folders/"+subfolderID, nil)
	req2.Header.Set("Authorization", "Bearer "+tokenB)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var subContents map[string]any
	err = json.Unmarshal(rec2.Body.Bytes(), &subContents)
	require.NoError(t, err)
	nbs, ok := subContents["notebooks"].([]any)
	require.True(t, ok, "subfolder contents should have notebooks")
	require.Equal(t, 1, len(nbs), "subfolder should have 1 notebook visible to User B")
	nbEntry := nbs[0].(map[string]any)
	require.Equal(t, notebookID, nbEntry["id"].(string), "notebook ID should match")
}
