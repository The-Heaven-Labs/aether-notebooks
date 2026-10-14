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

// TestNotebookCanRunInheritedFromFolderACL pins the reported bug end to end: a
// member granted the full folder action set can run a notebook created inside
// that folder, and the notebook read surfaces can_run/can_share to the UI even
// though the member holds no direct notebook entry.
func TestNotebookCanRunInheritedFromFolderACL(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	adminToken := registerAndGetToken(t, srv,
		fmt.Sprintf("folder-run-admin-%d@example.com", time.Now().UnixNano()), "Folder Run Org")

	// Root folder, then a notebook inside it, both created by the org admin.
	folderID := createFolderForRunTest(t, srv, adminToken, "Runnable Folder")
	nbID, orgID := createNotebookInFolderForRunTest(t, srv, adminToken, folderID)

	// A plain org member with no direct notebook or folder entry.
	var memberID string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Folder Member', false) RETURNING id`,
		fmt.Sprintf("folder-run-member-%d@example.com", time.Now().UnixNano())).Scan(&memberID))
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'editor')`,
		orgID, memberID)
	require.NoError(t, err)
	memberToken, err := testJWT.Issue(memberID, orgID, "editor")
	require.NoError(t, err)

	// Before the folder grant the member cannot see the notebook at all.
	rec := getNotebookAs(t, srv, memberToken, nbID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// The full folder grant: everything, including running the notebook.
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view','create','edit','manage','delete'])`,
		orgID, folderID, memberID)
	require.NoError(t, err)

	rec = getNotebookAs(t, srv, memberToken, nbID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		CanEdit  bool `json:"can_edit"`
		CanRun   bool `json:"can_run"`
		CanShare bool `json:"can_share"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.True(t, got.CanRun, "full folder access must surface can_run for a notebook inside the folder")
	require.True(t, got.CanEdit, "full folder access must surface can_edit")
	require.True(t, got.CanShare, "folder manage must surface can_share")

	// Downgrading to view-only revokes execution again.
	_, err = db.Pool.Exec(ctx,
		`UPDATE acl_entries SET actions = ARRAY['view']
		 WHERE resource_type = 'folder' AND resource_id = $1::uuid AND subject_id = $2`,
		folderID, memberID)
	require.NoError(t, err)
	rec = getNotebookAs(t, srv, memberToken, nbID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.False(t, got.CanRun, "a view-only folder grant must not run notebooks")
}

func createFolderForRunTest(t *testing.T, srv http.Handler, token, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var folder map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&folder))
	return folder["id"].(string)
}

func createNotebookInFolderForRunTest(t *testing.T, srv http.Handler, token, folderID string) (notebookID, orgID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": "Runnable Notebook", "folder_id": folderID})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var nb map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&nb))
	return nb["id"].(string), nb["org_id"].(string)
}

func getNotebookAs(t *testing.T, srv http.Handler, token, notebookID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+notebookID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}
