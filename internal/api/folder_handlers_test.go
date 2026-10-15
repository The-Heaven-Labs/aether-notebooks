package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
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

func TestListRootContentsExcludesDeletedFolders(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("folder-delete-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Folder Delete Org")

	createFolder := func(name string) string {
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

	listRoot := func(adminMode bool) []string {
		req := httptest.NewRequest("GET", "/api/v1/folders", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if adminMode {
			req.Header.Set("X-AETHER-Admin-Mode", "true")
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var contents struct {
			Folders []struct {
				ID string `json:"id"`
			} `json:"folders"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&contents))
		ids := make([]string, 0, len(contents.Folders))
		for _, f := range contents.Folders {
			ids = append(ids, f.ID)
		}
		return ids
	}

	keepID := createFolder("Keep Me")
	trashID := createFolder("Trash Me")

	require.Contains(t, listRoot(true), keepID)
	require.Contains(t, listRoot(true), trashID)
	require.Contains(t, listRoot(false), trashID)

	// Deleting a root folder moves it to trash; the trash page has its own
	// endpoint, so it must disappear from the root listing in both listing
	// branches.
	reqDel := httptest.NewRequest("DELETE", "/api/v1/folders/"+trashID+"?force=true", nil)
	reqDel.Header.Set("Authorization", "Bearer "+token)
	recDel := httptest.NewRecorder()
	srv.ServeHTTP(recDel, reqDel)
	require.Equal(t, http.StatusNoContent, recDel.Code, recDel.Body.String())

	require.Contains(t, listRoot(true), keepID)
	require.NotContains(t, listRoot(true), trashID)
	require.NotContains(t, listRoot(false), trashID)
}

// ——— Task B1: two-phase pagination of folder listing sections ———

// TestFolderContentsPaginationSmallFolder pins the always-present pagination
// shape: a folder whose section fits on one page reports exact totals, no
// has_more, every section key (including the empty ones), and an empty folder
// still carries the full shape.
func TestFolderContentsPaginationSmallFolder(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	folderID := f.insertFolder(t, "pagination small", nil, f.otherUserID)
	f.grant(t, "folder", folderID, []string{"view"})

	var want []string
	for i := 0; i < 3; i++ {
		id := f.insertNotebook(t, fmt.Sprintf("pagination small notebook %d", i), &folderID, f.otherUserID)
		f.grant(t, "notebook", id, []string{"view"})
		want = append(want, id)
	}

	contents := f.listFolder(t, folderID)
	require.Len(t, contents.Notebooks, 3)
	require.ElementsMatch(t, want, rowIDs(contents.Notebooks))
	require.Equal(t, map[string]int{"notebooks": 3, "connectors": 0, "dashboards": 0}, contents.Totals)
	require.Equal(t, map[string]bool{"notebooks": false, "connectors": false, "dashboards": false}, contents.HasMore)

	// An empty folder still carries the full shape: present (non-null) section
	// arrays, zero totals, false flags — never missing keys. Raw JSON is
	// decoded because require.Empty cannot tell [] from null or a missing key.
	emptyFolder := f.insertFolder(t, "pagination small empty", nil, f.otherUserID)
	f.grant(t, "folder", emptyFolder, []string{"view"})
	status, body := f.get(t, "/api/v1/folders/"+emptyFolder)
	require.Equal(t, http.StatusOK, status, string(body))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	for _, key := range []string{"folders", "notebooks", "connectors", "dashboards"} {
		require.JSONEq(t, "[]", string(raw[key]), "section %q must be a present JSON array", key)
	}
	require.JSONEq(t, `{"notebooks":0,"connectors":0,"dashboards":0}`, string(raw["totals"]))
	require.JSONEq(t, `{"notebooks":false,"connectors":false,"dashboards":false}`, string(raw["has_more"]))
}

// TestFolderContentsPaginationHiddenItems pins that totals count only visible
// rows: hidden notebooks interleaved between visible ones neither inflate the
// total nor reserve page slots.
func TestFolderContentsPaginationHiddenItems(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	folderID := f.insertFolder(t, "pagination hidden", nil, f.otherUserID)
	// No folder ACL on purpose: a folder grant is inherited by every notebook
	// inside, which would make the un-ACLed ones visible. The folder is
	// reached through its visible descendant content instead.

	var visible []string
	for i := 0; i < 12; i++ {
		id := f.insertNotebook(t, fmt.Sprintf("pagination hidden notebook %02d", i), &folderID, f.otherUserID)
		if i%3 == 0 {
			continue // every third notebook carries no ACL for the caller
		}
		f.grant(t, "notebook", id, []string{"view"})
		visible = append(visible, id)
	}

	contents := f.listFolder(t, folderID)
	require.Equal(t, 8, contents.Totals["notebooks"])
	require.Len(t, contents.Notebooks, 8, "the page holds only visible rows")
	require.ElementsMatch(t, visible, rowIDs(contents.Notebooks))
	require.False(t, contents.HasMore["notebooks"])
}

// TestFolderContentsPaginationLimit pins the limit contract: 50 default, 200
// clamp for larger asks, 400 for non-numeric or below-1 values.
func TestFolderContentsPaginationLimit(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	folderID := f.insertFolder(t, "pagination limit", nil, f.otherUserID)
	f.grant(t, "folder", folderID, []string{"view"})
	for i := 0; i < 55; i++ {
		id := f.insertNotebook(t, fmt.Sprintf("pagination limit notebook %02d", i), &folderID, f.otherUserID)
		f.grant(t, "notebook", id, []string{"view"})
	}

	// Default: a 50-row first page of 55 visible rows.
	contents := f.listFolder(t, folderID)
	require.Equal(t, 55, contents.Totals["notebooks"])
	require.Len(t, contents.Notebooks, 50)
	require.True(t, contents.HasMore["notebooks"])

	// An explicit smaller page.
	contents = f.listPath(t, "/api/v1/folders/"+folderID+"?limit=10")
	require.Equal(t, 55, contents.Totals["notebooks"])
	require.Len(t, contents.Notebooks, 10)
	require.True(t, contents.HasMore["notebooks"])

	// A page larger than the section returns everything.
	contents = f.listPath(t, "/api/v1/folders/"+folderID+"?limit=200")
	require.Equal(t, 55, contents.Totals["notebooks"])
	require.Len(t, contents.Notebooks, 55)
	require.False(t, contents.HasMore["notebooks"])

	// Above the maximum clamps instead of failing.
	contents = f.listPath(t, "/api/v1/folders/"+folderID+"?limit=201")
	require.Equal(t, 55, contents.Totals["notebooks"])
	require.Len(t, contents.Notebooks, 55)
	require.False(t, contents.HasMore["notebooks"])

	// An enormous value (Atoi overflows) also clamps to the maximum.
	contents = f.listPath(t, "/api/v1/folders/"+folderID+"?limit=99999999999999999999")
	require.Equal(t, 55, contents.Totals["notebooks"])
	require.Len(t, contents.Notebooks, 55)
	require.False(t, contents.HasMore["notebooks"])

	// Invalid values are rejected.
	status, body := f.get(t, "/api/v1/folders/"+folderID+"?limit=0")
	require.Equal(t, http.StatusBadRequest, status, string(body))
	status, body = f.get(t, "/api/v1/folders/"+folderID+"?limit=abc")
	require.Equal(t, http.StatusBadRequest, status, string(body))

	// Boundary: a section exactly at the page size has no next page.
	exactFolder := f.insertFolder(t, "pagination limit exact", nil, f.otherUserID)
	f.grant(t, "folder", exactFolder, []string{"view"})
	for i := 0; i < 50; i++ {
		id := f.insertNotebook(t, fmt.Sprintf("pagination exact notebook %02d", i), &exactFolder, f.otherUserID)
		f.grant(t, "notebook", id, []string{"view"})
	}
	exact := f.listFolder(t, exactFolder)
	require.Equal(t, 50, exact.Totals["notebooks"])
	require.Len(t, exact.Notebooks, 50)
	require.False(t, exact.HasMore["notebooks"], "a section exactly at the page size has no next page")

	// The limit applies to the connector and dashboard sections too.
	sectionedFolder := f.insertFolder(t, "pagination limit sections", nil, f.otherUserID)
	f.grant(t, "folder", sectionedFolder, []string{"view"})
	for i := 0; i < 3; i++ {
		c := f.insertConnector(t, fmt.Sprintf("pagination limit connector %d", i), &sectionedFolder, f.otherUserID)
		f.grant(t, "connector", c, []string{"view"})
	}
	for i := 0; i < 2; i++ {
		d := f.insertDashboard(t, fmt.Sprintf("pagination limit dashboard %d", i), &sectionedFolder, f.otherUserID)
		f.grant(t, "dashboard", d, []string{"view"})
	}
	sectioned := f.listPath(t, "/api/v1/folders/"+sectionedFolder+"?limit=2")
	require.Equal(t, 3, sectioned.Totals["connectors"])
	require.Len(t, sectioned.Connectors, 2)
	require.True(t, sectioned.HasMore["connectors"])
	require.Equal(t, 2, sectioned.Totals["dashboards"])
	require.Len(t, sectioned.Dashboards, 2)
	require.False(t, sectioned.HasMore["dashboards"], "a dashboard section exactly at the page size has no next page")
}

// TestFolderContentsPaginationStableOrder pins the ordering tiebreaker: two
// notebooks sharing updated_at keep a deterministic (id-ascending) relative
// order across listings, so offset paging can never skip or duplicate them.
func TestFolderContentsPaginationStableOrder(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	folderID := f.insertFolder(t, "pagination order", nil, f.otherUserID)
	f.grant(t, "folder", folderID, []string{"view"})

	tieA := f.insertNotebook(t, "pagination order tie a", &folderID, f.otherUserID)
	tieB := f.insertNotebook(t, "pagination order tie b", &folderID, f.otherUserID)
	newer := f.insertNotebook(t, "pagination order newer", &folderID, f.otherUserID)
	for _, id := range []string{tieA, tieB, newer} {
		f.grant(t, "notebook", id, []string{"view"})
	}
	shared := time.Now().Add(-2 * time.Hour)
	for _, id := range []string{tieA, tieB} {
		_, err := f.srv.DB().Pool.Exec(context.Background(),
			`UPDATE notebooks SET updated_at = $2 WHERE id = $1`, id, shared)
		require.NoError(t, err)
	}

	ties := []string{tieA, tieB}
	sort.Strings(ties) // ids ascend within one updated_at
	want := append([]string{newer}, ties...)

	contents := f.listFolder(t, folderID)
	require.Equal(t, want, rowIDs(contents.Notebooks))
	contents = f.listFolder(t, folderID)
	require.Equal(t, want, rowIDs(contents.Notebooks), "repeated listings must return the same order")
}

// dashboardListingRow is the dashboard section row shape with the fields the
// snapshot-substitution test asserts.
type dashboardListingRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	CanEdit  bool   `json:"can_edit"`
	Settings struct {
		AutoRefreshSeconds int `json:"auto_refresh_seconds"`
	} `json:"settings"`
}

func decodeDashboardListing(t *testing.T, body []byte) []dashboardListingRow {
	t.Helper()
	var contents struct {
		Dashboards []dashboardListingRow `json:"dashboards"`
	}
	require.NoError(t, json.Unmarshal(body, &contents))
	return contents.Dashboards
}

func requireDashboardListingRow(t *testing.T, rows []dashboardListingRow, id string) dashboardListingRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	require.Failf(t, "dashboard row missing", "expected dashboard %s in %v", id, rows)
	return dashboardListingRow{}
}

// TestFolderContentsDashboardSnapshotSubstitution pins the viewer-safety rule
// in folder listings: a published dashboard serves its latest snapshot's
// title/settings to non-editors, editors always see the draft, and a
// never-published dashboard keeps its draft for everyone — matching
// GET /dashboards and GET /dashboards/{id}.
func TestFolderContentsDashboardSnapshotSubstitution(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	folderID := f.insertFolder(t, "pagination dashboards", nil, f.otherUserID)
	f.grant(t, "folder", folderID, []string{"view"})
	editorToken := issueToken(t, f.otherUserID, f.orgID, "editor")

	published := f.insertDashboard(t, "draft title of published", &folderID, f.otherUserID)
	f.grant(t, "dashboard", published, []string{"view"})
	f.grantFor(t, "dashboard", published, f.otherUserID, []string{"view", "edit"})
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO dashboard_versions (dashboard_id, org_id, version, title, settings, widgets, content_hash, created_by)
		 VALUES ($1, $2, 1, 'snapshot title', '{"auto_refresh_seconds": 5}', '[]', 'pagination-hash', $3)`,
		published, f.orgID, f.otherUserID)
	require.NoError(t, err)

	neverPublished := f.insertDashboard(t, "never published draft", &folderID, f.otherUserID)
	f.grant(t, "dashboard", neverPublished, []string{"view"})
	f.grantFor(t, "dashboard", neverPublished, f.otherUserID, []string{"view", "edit"})

	// Viewer: the published dashboard serves the snapshot on the page.
	status, body := f.get(t, "/api/v1/folders/"+folderID)
	require.Equal(t, http.StatusOK, status, string(body))
	viewerRows := decodeDashboardListing(t, body)
	viewerPublished := requireDashboardListingRow(t, viewerRows, published)
	require.Equal(t, "snapshot title", viewerPublished.Title)
	require.Equal(t, 5, viewerPublished.Settings.AutoRefreshSeconds)
	require.False(t, viewerPublished.CanEdit)
	require.Equal(t, "never published draft", requireDashboardListingRow(t, viewerRows, neverPublished).Title)

	// Editor: both keep the draft.
	status, body = f.getAs(t, editorToken, "/api/v1/folders/"+folderID)
	require.Equal(t, http.StatusOK, status, string(body))
	editorRows := decodeDashboardListing(t, body)
	editorPublished := requireDashboardListingRow(t, editorRows, published)
	require.Equal(t, "draft title of published", editorPublished.Title)
	require.True(t, editorPublished.CanEdit)
	require.Zero(t, editorPublished.Settings.AutoRefreshSeconds)
	require.Equal(t, "never published draft", requireDashboardListingRow(t, editorRows, neverPublished).Title)
}

// TestFolderRootContentsPagination pins the root variant (GET /folders): the
// same totals/has_more/limit contract, with in-folder and hidden rows excluded
// from root section counts.
func TestFolderRootContentsPagination(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	// 55 visible root notebooks, one hidden root notebook, and one notebook
	// inside a folder: only the visible root rows may count.
	for i := 0; i < 55; i++ {
		id := f.insertNotebook(t, fmt.Sprintf("root pagination notebook %02d", i), nil, f.otherUserID)
		f.grant(t, "notebook", id, []string{"view"})
	}
	f.insertNotebook(t, "root pagination hidden notebook", nil, f.otherUserID)
	folderID := f.insertFolder(t, "root pagination folder", nil, f.otherUserID)
	inFolder := f.insertNotebook(t, "root pagination in-folder notebook", &folderID, f.otherUserID)
	f.grant(t, "notebook", inFolder, []string{"view"})

	// Root connectors and dashboards exercise the other two sections.
	for i := 0; i < 3; i++ {
		c := f.insertConnector(t, fmt.Sprintf("root pagination connector %d", i), nil, f.otherUserID)
		f.grant(t, "connector", c, []string{"view"})
		d := f.insertDashboard(t, fmt.Sprintf("root pagination dashboard %d", i), nil, f.otherUserID)
		f.grant(t, "dashboard", d, []string{"view"})
	}

	root := f.listRoot(t)
	require.Equal(t, 55, root.Totals["notebooks"])
	require.Len(t, root.Notebooks, 50)
	require.True(t, root.HasMore["notebooks"])
	require.Equal(t, 3, root.Totals["connectors"])
	require.Len(t, root.Connectors, 3)
	require.False(t, root.HasMore["connectors"])
	require.Equal(t, 3, root.Totals["dashboards"])
	require.Len(t, root.Dashboards, 3)
	require.False(t, root.HasMore["dashboards"])

	// Explicit limits apply to the root variant too.
	root = f.listPath(t, "/api/v1/folders?limit=10")
	require.Equal(t, 55, root.Totals["notebooks"])
	require.Len(t, root.Notebooks, 10)
	require.True(t, root.HasMore["notebooks"])

	root = f.listPath(t, "/api/v1/folders?limit=201")
	require.Len(t, root.Notebooks, 55)
	require.False(t, root.HasMore["notebooks"])

	status, body := f.get(t, "/api/v1/folders?limit=0")
	require.Equal(t, http.StatusBadRequest, status, string(body))
	status, body = f.get(t, "/api/v1/folders?limit=abc")
	require.Equal(t, http.StatusBadRequest, status, string(body))
}
