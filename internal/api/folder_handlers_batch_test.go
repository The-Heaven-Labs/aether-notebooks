package api_test

// Coverage for the batched migration of the folder listing handlers
// (GET /folders/{id}, GET /folders, GET /home). Two concerns live here:
//
//  1. the query-count guard: a listing must resolve permissions and descendant
//     visibility in a bounded, item-count-independent number of queries, so the
//     per-item checkPermission loops cannot come back silently; and
//  2. behavior pins for the corners the batch swap could break: hidden children
//     with visible content, the wider root/home descendant predicate (a nested
//     folder the caller can reach keeps its ancestor listed), and the
//     can_edit/can_delete/can_share flags of a mixed-ACL caller.
//
// The fixture seeds rows with SQL (same approach as the white-box permission
// suites) but drives the HTTP handlers, since the response shape is the
// contract being pinned.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// folderBatchFixture is one org with a caller (org creator, no admin mode) and
// a second member who owns the seeded resources. Resources are created by
// direct SQL so tests stay fast; ACLs are written explicitly.
type folderBatchFixture struct {
	srv         *api.Server
	token       string
	orgID       string
	userID      string
	otherUserID string
}

func setupFolderBatchFixture(t *testing.T, srv *api.Server) *folderBatchFixture {
	t.Helper()
	now := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("folder-batch-%d@test.com", now), "Folder Batch Org")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)
	otherID := insertUser(t, srv, fmt.Sprintf("folder-batch-other-%d@test.com", now), "Other User")
	addOrgMember(t, srv, orgID, otherID, "editor")
	return &folderBatchFixture{
		srv: srv, token: token, orgID: orgID, userID: userID, otherUserID: otherID,
	}
}

func (f *folderBatchFixture) insertFolder(t *testing.T, name string, parentID *string, ownerID string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO folders (id, org_id, parent_id, name, owner_id, created_by)
		 VALUES ($1, $2, $3, $4, $5, $5)`,
		id, f.orgID, parentID, name, ownerID)
	require.NoError(t, err)
	return id
}

func (f *folderBatchFixture) insertHomeFolder(t *testing.T, name, ownerID string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO folders (id, org_id, name, is_home, owner_id, created_by)
		 VALUES ($1, $2, $3, true, $4, $4)`,
		id, f.orgID, name, ownerID)
	require.NoError(t, err)
	return id
}

func (f *folderBatchFixture) insertNotebook(t *testing.T, title string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO notebooks (id, org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4, $5)`,
		id, f.orgID, title, ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *folderBatchFixture) insertConnector(t *testing.T, name string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, folder_id)
		 VALUES ($1, $2, $3, 'postgres', $4, $5, $6)`,
		id, f.orgID, name, []byte("x"), ownerID, folderID)
	require.NoError(t, err)
	return id
}

func (f *folderBatchFixture) insertDashboard(t *testing.T, title string, folderID *string, ownerID string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO dashboards (id, org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4, $5)`,
		id, f.orgID, title, ownerID, folderID)
	require.NoError(t, err)
	return id
}

// grant writes one user ACL entry for the caller.
func (f *folderBatchFixture) grant(t *testing.T, resourceType, resourceID string, actions []string) {
	t.Helper()
	_, err := f.srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, $2, $3::uuid, 'user', $4, $5)`,
		f.orgID, resourceType, resourceID, f.userID, actions)
	require.NoError(t, err)
}

// get performs an authenticated GET and returns the status and raw body.
func (f *folderBatchFixture) get(t *testing.T, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// folderRow is the common row shape of the folders/notebooks/connectors/
// dashboards sections.
type folderRow struct {
	ID        string `json:"id"`
	CanEdit   bool   `json:"can_edit"`
	CanDelete bool   `json:"can_delete"`
	CanShare  bool   `json:"can_share"`
}

// folderListResponse mirrors folderContents for the fields these tests assert.
type folderListResponse struct {
	Folders    []folderRow `json:"folders"`
	Notebooks  []folderRow `json:"notebooks"`
	Connectors []folderRow `json:"connectors"`
	Dashboards []folderRow `json:"dashboards"`
}

func (f *folderBatchFixture) listFolder(t *testing.T, folderID string) folderListResponse {
	t.Helper()
	status, body := f.get(t, "/api/v1/folders/"+folderID)
	require.Equal(t, http.StatusOK, status, string(body))
	var contents folderListResponse
	require.NoError(t, json.Unmarshal(body, &contents))
	return contents
}

func (f *folderBatchFixture) listRoot(t *testing.T) folderListResponse {
	t.Helper()
	status, body := f.get(t, "/api/v1/folders")
	require.Equal(t, http.StatusOK, status, string(body))
	var contents folderListResponse
	require.NoError(t, json.Unmarshal(body, &contents))
	return contents
}

func requireRow(t *testing.T, rows []folderRow, id string) folderRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	require.Failf(t, "row missing", "expected row %s in %v", id, rows)
	return folderRow{}
}

func rowIDs(rows []folderRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

// TestFolderContentsQueryCount is the N+1 guard: the same folder listing shape
// with 12 and with 120 visible notebooks must issue the same bounded number of
// queries, and 100 hidden child folders must not add a probe per child.
func TestFolderContentsQueryCount(t *testing.T) {
	srv, counter := setupTestServerWithQueryCounter(t)
	f := setupFolderBatchFixture(t, srv)

	seedNotebookFolder := func(name string, notebooks int) string {
		t.Helper()
		folderID := f.insertFolder(t, name, nil, f.otherUserID)
		f.grant(t, "folder", folderID, []string{"view"})
		for i := 0; i < notebooks; i++ {
			nb := f.insertNotebook(t, fmt.Sprintf("%s notebook %d", name, i), &folderID, f.otherUserID)
			f.grant(t, "notebook", nb, []string{"view"})
		}
		return folderID
	}

	countListing := func(folderID string) int64 {
		t.Helper()
		counter.n.Store(0)
		status, body := f.get(t, "/api/v1/folders/"+folderID)
		require.Equal(t, http.StatusOK, status, string(body))
		return counter.n.Load()
	}

	smallFolder := seedNotebookFolder("query-count-small", 12)
	largeFolder := seedNotebookFolder("query-count-large", 120)

	// 100 hidden children with no visible content anywhere: the descendant
	// probe must stay one query, not one per child. The parent carries no
	// folder ACL (which children would inherit) and is reached through a
	// notebook directly inside it, so every child is genuinely hidden.
	hiddenParent := f.insertFolder(t, "query-count-hidden", nil, f.otherUserID)
	parentNotebook := f.insertNotebook(t, "query-count-hidden notebook", &hiddenParent, f.otherUserID)
	f.grant(t, "notebook", parentNotebook, []string{"view"})
	for i := 0; i < 100; i++ {
		f.insertFolder(t, fmt.Sprintf("query-count-hidden-child-%03d", i), &hiddenParent, f.otherUserID)
	}

	// Measure every listing before asserting so a red run reports all three
	// counts.
	countSmall := countListing(smallFolder)
	countLarge := countListing(largeFolder)
	countHidden := countListing(hiddenParent)
	t.Logf("query counts: 12 notebooks=%d, 120 notebooks=%d, 100 hidden children=%d",
		countSmall, countLarge, countHidden)

	require.Lessf(t, countSmall, int64(40),
		"a 12-notebook folder must resolve in a bounded number of queries (got %d)", countSmall)
	require.LessOrEqualf(t, countLarge, countSmall+8,
		"query count must not grow with the item count (12 items: %d, 120 items: %d)", countSmall, countLarge)
	require.LessOrEqualf(t, countHidden, countSmall+8,
		"100 hidden children must not add per-child probes (baseline: %d, hidden: %d)", countSmall, countHidden)

	contents := f.listFolder(t, hiddenParent)
	require.Empty(t, contents.Folders, "children with no visible content must stay hidden")
}

// TestRootAndHomeListQueryCount extends the N+1 guard to the other two
// migrated handlers: GET /folders and GET /home must resolve their per-item
// permissions and descendants in a bounded number of queries regardless of how
// many root folders / home folders exist.
func TestRootAndHomeListQueryCount(t *testing.T) {
	srv, counter := setupTestServerWithQueryCounter(t)
	f := setupFolderBatchFixture(t, srv)

	// Root half: 30 root folders the caller can see, plus 30 root notebooks:
	// the root listing previously ran three to four checks per row.
	for i := 0; i < 30; i++ {
		folderID := f.insertFolder(t, fmt.Sprintf("root-count-folder-%02d", i), nil, f.otherUserID)
		f.grant(t, "folder", folderID, []string{"view", "edit"})
		nb := f.insertNotebook(t, fmt.Sprintf("root-count-notebook-%02d", i), nil, f.otherUserID)
		f.grant(t, "notebook", nb, []string{"view"})
	}

	countGet := func(f *folderBatchFixture, path string) int64 {
		t.Helper()
		counter.n.Store(0)
		status, body := f.get(t, path)
		require.Equal(t, http.StatusOK, status, string(body))
		return counter.n.Load()
	}

	countRoot := countGet(f, "/api/v1/folders")
	require.Lessf(t, countRoot, int64(40),
		"the root listing must resolve in a bounded number of queries (got %d)", countRoot)

	// Home half: differential across two fresh orgs (5 vs 50 other homes), so
	// a one-query-per-home regression cannot hide under an absolute bound.
	// Each org's caller owns one home, so the listing sees 6 and 51 homes.
	small := seedHomeCountOrg(t, srv, 5)
	large := seedHomeCountOrg(t, srv, 50)
	countHomeSmall := countGet(small, "/api/v1/home")
	countHomeLarge := countGet(large, "/api/v1/home")
	t.Logf("query counts: root listing=%d, home listing (5 homes)=%d, home listing (50 homes)=%d",
		countRoot, countHomeSmall, countHomeLarge)

	require.Lessf(t, countHomeSmall, int64(40),
		"the home listing must resolve in a bounded number of queries (got %d)", countHomeSmall)
	require.LessOrEqualf(t, countHomeLarge, countHomeSmall+4,
		"query count must not grow with the home-folder count (5 homes: %d, 50 homes: %d)",
		countHomeSmall, countHomeLarge)
}

// seedHomeCountOrg creates a fresh org whose caller sees `homes` other home
// folders, each with a subfolder holding a notebook the caller can view, so
// the /home visibility, ACL, and subfolder probes all do real work. It returns
// the fixture for that org's caller.
func seedHomeCountOrg(t *testing.T, srv *api.Server, homes int) *folderBatchFixture {
	t.Helper()
	f := setupFolderBatchFixture(t, srv)
	for i := 0; i < homes; i++ {
		ownerID := insertUser(t, srv, fmt.Sprintf("home-count-owner-%02d-%d@test.com", i, time.Now().UnixNano()), "Home Owner")
		addOrgMember(t, srv, f.orgID, ownerID, "editor")
		homeID := f.insertHomeFolder(t, fmt.Sprintf("home-count-%02d", i), ownerID)
		childID := f.insertFolder(t, fmt.Sprintf("home-count-child-%02d", i), &homeID, ownerID)
		nb := f.insertNotebook(t, fmt.Sprintf("home-count-notebook-%02d", i), &childID, ownerID)
		f.grant(t, "notebook", nb, []string{"view"})
	}
	return f
}

// TestFolderContentsBatchListing pins the GET /folders/{id} semantics after the
// batch migration: hidden children appear only when they hold visible
// descendant content (the content-only probe), and the can_* flags come from
// the batched resolver — including a mixed-ACL caller.
func TestFolderContentsBatchListing(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	parent := f.insertFolder(t, "batch parent", nil, f.otherUserID)
	// The parent itself carries no folder ACL: the caller reaches it through
	// visible descendant content, exactly the deep-sharing shape. A folder
	// grant on the parent would be inherited by every child and nothing would
	// stay hidden.

	visibleChild := f.insertFolder(t, "batch visible child", &parent, f.otherUserID)
	f.grant(t, "folder", visibleChild, []string{"view", "edit"})

	// Hidden itself, but holds a notebook the caller can view: listed with
	// all flags false (the child folder itself grants nothing).
	contentChild := f.insertFolder(t, "batch content child", &parent, f.otherUserID)
	contentNotebook := f.insertNotebook(t, "batch content notebook", &contentChild, f.otherUserID)
	f.grant(t, "notebook", contentNotebook, []string{"view"})

	emptyChild := f.insertFolder(t, "batch empty child", &parent, f.otherUserID)

	// Hidden, and its only visible descendant is a folder stem (a grandchild
	// with a folder ACL): the content-only child probe must keep it hidden
	// here — this is exactly where the root/home listings are wider.
	stemChild := f.insertFolder(t, "batch stem child", &parent, f.otherUserID)
	stemGrandchild := f.insertFolder(t, "batch stem grandchild", &stemChild, f.otherUserID)
	f.grant(t, "folder", stemGrandchild, []string{"view"})

	viewerNotebook := f.insertNotebook(t, "batch viewer notebook", &parent, f.otherUserID)
	f.grant(t, "notebook", viewerNotebook, []string{"view"})
	editorNotebook := f.insertNotebook(t, "batch editor notebook", &parent, f.otherUserID)
	f.grant(t, "notebook", editorNotebook, []string{"view", "edit"})
	managerNotebook := f.insertNotebook(t, "batch manager notebook", &parent, f.otherUserID)
	f.grant(t, "notebook", managerNotebook, []string{"view", "edit", "share", "delete"})
	hiddenNotebook := f.insertNotebook(t, "batch hidden notebook", &parent, f.otherUserID)

	connector := f.insertConnector(t, "batch connector", &parent, f.otherUserID)
	f.grant(t, "connector", connector, []string{"view", "edit"})

	dashboard := f.insertDashboard(t, "batch dashboard", &parent, f.otherUserID)
	f.grant(t, "dashboard", dashboard, []string{"view", "edit", "share", "delete"})

	contents := f.listFolder(t, parent)

	// Subfolders are ordered by name and are exactly: visible child, content
	// child. The empty child and the folder-stem-only child stay hidden.
	require.Equal(t, []string{contentChild, visibleChild}, rowIDs(contents.Folders),
		"subfolders must be the visible child plus the content-bearing hidden child")
	require.NotContains(t, rowIDs(contents.Folders), emptyChild)

	visibleRow := requireRow(t, contents.Folders, visibleChild)
	require.True(t, visibleRow.CanEdit, "folder edit grant must surface as can_edit")
	require.False(t, visibleRow.CanDelete)
	require.False(t, visibleRow.CanShare, "folder can_share comes from the manage action")

	contentRow := requireRow(t, contents.Folders, contentChild)
	require.False(t, contentRow.CanEdit, "visible via content must keep all flags false")
	require.False(t, contentRow.CanDelete)
	require.False(t, contentRow.CanShare)

	// Notebooks: exactly the three granted ones, hidden notebook absent, flags
	// per action set.
	require.Len(t, contents.Notebooks, 3)
	require.ElementsMatch(t, []string{viewerNotebook, editorNotebook, managerNotebook}, rowIDs(contents.Notebooks))
	require.NotContains(t, rowIDs(contents.Notebooks), hiddenNotebook)

	viewerRow := requireRow(t, contents.Notebooks, viewerNotebook)
	require.False(t, viewerRow.CanEdit)
	require.False(t, viewerRow.CanDelete)
	require.False(t, viewerRow.CanShare)

	editorRow := requireRow(t, contents.Notebooks, editorNotebook)
	require.True(t, editorRow.CanEdit)
	require.False(t, editorRow.CanDelete, "edit must not imply delete")
	require.False(t, editorRow.CanShare, "edit must not imply share")

	managerRow := requireRow(t, contents.Notebooks, managerNotebook)
	require.True(t, managerRow.CanEdit)
	require.True(t, managerRow.CanDelete)
	require.True(t, managerRow.CanShare)

	require.Len(t, contents.Connectors, 1)
	connectorRow := requireRow(t, contents.Connectors, connector)
	require.True(t, connectorRow.CanEdit)
	require.False(t, connectorRow.CanDelete)
	require.False(t, connectorRow.CanShare)

	require.Len(t, contents.Dashboards, 1)
	dashboardRow := requireRow(t, contents.Dashboards, dashboard)
	require.True(t, dashboardRow.CanEdit)
	require.True(t, dashboardRow.CanDelete)
	require.True(t, dashboardRow.CanShare)
}

// TestFolderContentsHiddenChildWithoutContent checks the negative case on its
// own: a hidden child whose subtree holds no visible content at any depth is
// never listed.
func TestFolderContentsHiddenChildWithoutContent(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	parent := f.insertFolder(t, "hidden parent", nil, f.otherUserID)
	// The parent is reachable through a notebook directly inside it; the
	// hidden child's own subtree holds nothing the caller can see.
	parentNotebook := f.insertNotebook(t, "shared parent notebook", &parent, f.otherUserID)
	f.grant(t, "notebook", parentNotebook, []string{"view"})
	child := f.insertFolder(t, "hidden child", &parent, f.otherUserID)
	// Content exists but carries no ACL for the caller (and owner_id is
	// deliberately not consulted by the probe).
	grandchild := f.insertFolder(t, "hidden grandchild", &child, f.otherUserID)
	f.insertNotebook(t, "unshared notebook", &grandchild, f.otherUserID)

	contents := f.listFolder(t, parent)
	require.Empty(t, contents.Folders)
}

// TestFolderListingsFolderStemVisibility pins the wider root/home descendant
// predicate: a folder whose only visible descendant is a nested folder the
// caller can reach (folder ACL or ownership) must stay listed — unlike
// GET /folders/{id}, whose child probe is content-only. /home's top-level
// filter is folder-aware, while its sub_folders list keeps the content-only
// deep branch.
func TestFolderListingsFolderStemVisibility(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	// Root: an ACL-reachable grandchild keeps its ancestor listed.
	rootParent := f.insertFolder(t, "stem root parent", nil, f.otherUserID)
	rootChild := f.insertFolder(t, "stem root child", &rootParent, f.otherUserID)
	rootGrandchild := f.insertFolder(t, "stem root grandchild", &rootChild, f.otherUserID)
	f.grant(t, "folder", rootGrandchild, []string{"view"})

	// Root: an owned descendant keeps its ancestor listed too.
	ownedParent := f.insertFolder(t, "stem owned parent", nil, f.otherUserID)
	f.insertFolder(t, "stem owned child", &ownedParent, f.userID)

	// A root notebook exercises the batched flags in the root listing.
	rootNotebook := f.insertNotebook(t, "stem root notebook", nil, f.otherUserID)
	f.grant(t, "notebook", rootNotebook, []string{"view", "edit"})

	root := f.listRoot(t)
	require.Contains(t, rowIDs(root.Folders), rootParent,
		"a root folder with an ACL-reachable nested folder must stay listed")
	require.Contains(t, rowIDs(root.Folders), ownedParent,
		"a root folder with a caller-owned nested folder must stay listed")
	parentRow := requireRow(t, root.Folders, rootParent)
	require.False(t, parentRow.CanEdit, "the nested folder's ACL must not leak flags onto its ancestor")
	require.False(t, parentRow.CanDelete)
	require.False(t, parentRow.CanShare)

	notebookRow := requireRow(t, root.Notebooks, rootNotebook)
	require.True(t, notebookRow.CanEdit, "root notebook flags must come from the batch resolver")
	require.False(t, notebookRow.CanDelete)
	require.False(t, notebookRow.CanShare)

	// /home: another user's home with the same shape must appear; its
	// sub_folders list keeps the content-only deep branch, so the folder-only
	// stem does not surface there.
	home := f.insertHomeFolder(t, "stem home", f.otherUserID)
	homeChild := f.insertFolder(t, "stem home child", &home, f.otherUserID)
	homeGrandchild := f.insertFolder(t, "stem home grandchild", &homeChild, f.otherUserID)
	f.grant(t, "folder", homeGrandchild, []string{"view"})

	// /home: a direct ACL on a home folder is its own visibility branch.
	aclHome := f.insertHomeFolder(t, "stem acl home", f.otherUserID)
	f.grant(t, "folder", aclHome, []string{"view"})

	// /home: a caller-owned child folder is visible in the top-level filter
	// (via the descendant walk) and in the parent's sub_folders list.
	ownedHome := f.insertHomeFolder(t, "stem owned home", f.otherUserID)
	ownedHomeChild := f.insertFolder(t, "stem owned home child", &ownedHome, f.userID)

	status, body := f.get(t, "/api/v1/home")
	require.Equal(t, http.StatusOK, status, string(body))
	var homes []struct {
		ID         string `json:"id"`
		SubFolders []struct {
			ID string `json:"id"`
		} `json:"sub_folders"`
	}
	require.NoError(t, json.Unmarshal(body, &homes))

	var homeEntry, aclHomeEntry, ownedHomeEntry, ownEntry bool
	for _, entry := range homes {
		subIDs := make([]string, 0, len(entry.SubFolders))
		for _, sub := range entry.SubFolders {
			subIDs = append(subIDs, sub.ID)
		}
		switch entry.ID {
		case home:
			homeEntry = true
			require.NotContains(t, subIDs, homeChild,
				"a subfolder reachable only through a nested folder is not listed in sub_folders")
		case aclHome:
			aclHomeEntry = true
			require.Empty(t, subIDs, "a home visible only through its direct ACL has no sub_folders")
		case ownedHome:
			ownedHomeEntry = true
			require.Contains(t, subIDs, ownedHomeChild,
				"a caller-owned subfolder is listed in sub_folders")
		case f.ownHomeFolderID(t):
			ownEntry = true
		}
	}
	require.True(t, homeEntry, "a home folder with a folder-ACL-reachable descendant must stay listed")
	require.True(t, aclHomeEntry, "a home folder with a direct ACL must stay listed")
	require.True(t, ownedHomeEntry, "a home folder with a caller-owned descendant must stay listed")
	require.True(t, ownEntry, "the caller's own home folder must stay listed")
}

// ownHomeFolderID returns the caller's home folder.
func (f *folderBatchFixture) ownHomeFolderID(t *testing.T) string {
	t.Helper()
	var id string
	err := f.srv.DB().Pool.QueryRow(context.Background(),
		`SELECT id FROM folders WHERE org_id = $1 AND owner_id = $2 AND is_home = true`,
		f.orgID, f.userID).Scan(&id)
	require.NoError(t, err)
	return id
}

// TestFolderListingManageToCanShare pins the listing's folder can_share
// mapping: a folder's can_share comes from the manage action (matched by name
// at any ancestor depth), not from the share action. An accidental
// manage→share swap for folders would flip the direct-manage expectation below
// to false, because share neither matches a manage grant by name nor expands
// onto folder resources. Folder can_share has mapped to manage since before
// the batch migration (the migrated code replaced the former
// checkPermission(..., "folder", f.ID, "manage") calls one for one).
func TestFolderListingManageToCanShare(t *testing.T) {
	srv := setupTestServer(t)
	f := setupFolderBatchFixture(t, srv)

	// Direct manage on a child of the listed folder: can_share true, nothing
	// else. The listed folder grants only view, so the child's manage cannot
	// arrive by inheritance.
	directParent := f.insertFolder(t, "manage direct parent", nil, f.otherUserID)
	f.grant(t, "folder", directParent, []string{"view"})
	directChild := f.insertFolder(t, "manage direct child", &directParent, f.otherUserID)
	f.grant(t, "folder", directChild, []string{"manage"})

	contents := f.listFolder(t, directParent)
	childRow := requireRow(t, contents.Folders, directChild)
	require.True(t, childRow.CanShare, "a direct manage grant must map to can_share")
	require.False(t, childRow.CanEdit, "manage must not imply edit")
	require.False(t, childRow.CanDelete, "manage must not imply delete")

	// Ancestor manage (with view) on the listed folder's parent: a descendant
	// folder reaches can_share through the manage name match, while a notebook
	// in the same listed folder reaches share through the manage→share
	// ancestor expansion. (Folders are excluded from that expansion, but the
	// exclusion is not observable through can_share because the folder
	// mapping is manage, not share.)
	ancestorParent := f.insertFolder(t, "manage ancestor parent", nil, f.otherUserID)
	f.grant(t, "folder", ancestorParent, []string{"view", "manage"})
	ancestorListed := f.insertFolder(t, "manage ancestor listed", &ancestorParent, f.otherUserID)
	sibling := f.insertFolder(t, "manage ancestor sibling", &ancestorListed, f.otherUserID)
	notebook := f.insertNotebook(t, "manage ancestor notebook", &ancestorListed, f.otherUserID)

	contents = f.listFolder(t, ancestorListed)
	siblingRow := requireRow(t, contents.Folders, sibling)
	require.True(t, siblingRow.CanShare, "an ancestor manage reaches descendant folders by name")
	require.False(t, siblingRow.CanEdit)
	require.False(t, siblingRow.CanDelete)

	notebookRow := requireRow(t, contents.Notebooks, notebook)
	require.True(t, notebookRow.CanShare, "the ancestor manage expands to share on the notebook")
	require.False(t, notebookRow.CanEdit)
	require.False(t, notebookRow.CanDelete)
}
