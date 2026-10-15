package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// favoritePath builds the PUT/DELETE path for one favorite.
func favoritePath(resourceType, resourceID string) string {
	return "/api/v1/favorites/" + resourceType + "/" + resourceID
}

// doAdminModeRequest issues a request as an org admin with admin mode on.
func doAdminModeRequest(t *testing.T, srv *api.Server, token, method, path string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec.Code
}

// countAuditEvents counts a caller's audit rows for one action and resource.
func countAuditEvents(t *testing.T, srv *api.Server, orgID, action, resourceID string) int {
	t.Helper()
	var count int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = $2 AND resource_id = $3`,
		orgID, action, resourceID).Scan(&count))
	return count
}

// favoriteResult is the subset of resourceSummary fields the tests assert.
type favoriteResult struct {
	ID         string
	Title      string
	Subtitle   string
	FolderPath string
	IsFavorite bool
	CanEdit    bool
	HasSnippet bool
}

// favoriteGroup is one parsed group of a GET /favorites response.
type favoriteGroup struct {
	ResourceType string
	Results      []favoriteResult
}

// parseFavoritesResponse decodes GET /favorites into typed groups, preserving
// group and result order.
func parseFavoritesResponse(t *testing.T, resp map[string]any) []favoriteGroup {
	t.Helper()
	raw, ok := resp["groups"].([]any)
	require.True(t, ok, "response missing groups array: %v", resp)
	groups := make([]favoriteGroup, 0, len(raw))
	for _, rawGroup := range raw {
		group := rawGroup.(map[string]any)
		parsed := favoriteGroup{ResourceType: group["resource_type"].(string)}
		results, ok := group["results"].([]any)
		require.True(t, ok, "group missing results: %v", group)
		require.NotEmpty(t, results, "empty groups must be omitted")
		for _, rawResult := range results {
			m := rawResult.(map[string]any)
			result := favoriteResult{
				ID:         m["id"].(string),
				Title:      m["title"].(string),
				Subtitle:   m["subtitle"].(string),
				IsFavorite: m["is_favorite"].(bool),
				CanEdit:    m["can_edit"].(bool),
			}
			if path, ok := m["folder_path"].(string); ok {
				result.FolderPath = path
			}
			if _, ok := m["snippet"]; ok {
				result.HasSnippet = true
			}
			parsed.Results = append(parsed.Results, result)
		}
		groups = append(groups, parsed)
	}
	return groups
}

// getFavorites fetches and parses the caller's favorites.
func getFavorites(t *testing.T, srv *api.Server, token string) []favoriteGroup {
	t.Helper()
	code, resp := doRequest(t, srv, token, "GET", "/api/v1/favorites", nil)
	require.Equal(t, http.StatusOK, code)
	return parseFavoritesResponse(t, resp)
}

func TestFavoritesIdempotencyAndAudit(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("fav-idem-%d@test.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Fav Org")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)

	code, resp := doRequest(t, srv, token, "POST", "/api/v1/notebooks", map[string]any{"title": "Idempotent"})
	notebookID := resourceID(t, code, resp)
	path := favoritePath("notebook", notebookID)

	// First PUT inserts and audits once.
	code, _ = doRequest(t, srv, token, "PUT", path, nil)
	require.Equal(t, http.StatusNoContent, code)
	var firstCreated time.Time
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT created_at FROM favorites WHERE user_id = $1 AND resource_type = 'notebook' AND resource_id = $2`,
		userID, notebookID).Scan(&firstCreated))
	require.Equal(t, 1, countAuditEvents(t, srv, orgID, "favorite.add", notebookID))

	// Second PUT is an idempotent 204: still one row, original created_at, no
	// extra audit event.
	code, _ = doRequest(t, srv, token, "PUT", path, nil)
	require.Equal(t, http.StatusNoContent, code)
	var count int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM favorites WHERE user_id = $1 AND resource_id = $2`,
		userID, notebookID).Scan(&count))
	require.Equal(t, 1, count)
	var secondCreated time.Time
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT created_at FROM favorites WHERE user_id = $1 AND resource_id = $2`,
		userID, notebookID).Scan(&secondCreated))
	require.True(t, firstCreated.Equal(secondCreated), "re-favorite must not bump created_at")
	require.Equal(t, 1, countAuditEvents(t, srv, orgID, "favorite.add", notebookID))

	// First DELETE removes and audits once; the second is a no-op 204.
	code, _ = doRequest(t, srv, token, "DELETE", path, nil)
	require.Equal(t, http.StatusNoContent, code)
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM favorites WHERE user_id = $1 AND resource_id = $2`,
		userID, notebookID).Scan(&count))
	require.Equal(t, 0, count)
	require.Equal(t, 1, countAuditEvents(t, srv, orgID, "favorite.remove", notebookID))

	code, _ = doRequest(t, srv, token, "DELETE", path, nil)
	require.Equal(t, http.StatusNoContent, code)
	require.Equal(t, 1, countAuditEvents(t, srv, orgID, "favorite.remove", notebookID))
}

func TestFavoritesValidationAndVisibility(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	now := time.Now().UnixNano()

	// Org A: creator A (org admin) + member B with no implicit permissions.
	tokenA := registerAndGetToken(t, srv, fmt.Sprintf("favval-a-%d@test.com", now), "Fav Val Org A")
	userA := userIDFromToken(t, srv, tokenA)
	orgA := orgIDFromUser(t, srv, userA)
	userB := insertUser(t, srv, fmt.Sprintf("favval-b-%d@test.com", now), "User B")
	addOrgMember(t, srv, orgA, userB, "editor")
	tokenB := issueToken(t, userB, orgA, "editor")

	// Org C: foreign resources.
	tokenC := registerAndGetToken(t, srv, fmt.Sprintf("favval-c-%d@test.com", now), "Fav Val Org C")

	code, resp := doRequest(t, srv, tokenA, "POST", "/api/v1/notebooks", map[string]any{"title": "Private"})
	privateID := resourceID(t, code, resp)
	code, resp = doRequest(t, srv, tokenC, "POST", "/api/v1/notebooks", map[string]any{"title": "Foreign Notebook"})
	foreignNotebookID := resourceID(t, code, resp)
	code, resp = doRequest(t, srv, tokenC, "POST", "/api/v1/folders", map[string]any{"name": "Foreign Folder"})
	foreignFolderID := resourceID(t, code, resp)

	// Unknown type: 400 on both verbs, before any lookup.
	for _, method := range []string{"PUT", "DELETE"} {
		code, _ = doRequest(t, srv, tokenA, method, favoritePath("bogus", privateID), nil)
		require.Equal(t, http.StatusBadRequest, code, method)
	}

	// Unknown id: 404. Malformed id: 404 on PUT, idempotent 204 on DELETE.
	code, _ = doRequest(t, srv, tokenA, "PUT", favoritePath("notebook", uuid.NewString()), nil)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = doRequest(t, srv, tokenA, "PUT", favoritePath("notebook", "not-a-uuid"), nil)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = doRequest(t, srv, tokenA, "DELETE", favoritePath("notebook", "not-a-uuid"), nil)
	require.Equal(t, http.StatusNoContent, code)

	// A private notebook is invisible (and unfavoritable) to B: 404, no leak.
	code, _ = doRequest(t, srv, tokenB, "PUT", favoritePath("notebook", privateID), nil)
	require.Equal(t, http.StatusNotFound, code)

	// Foreign-org resources 404 for a plain member and for an admin in admin
	// mode, on a folder and on a notebook.
	code, _ = doRequest(t, srv, tokenB, "PUT", favoritePath("folder", foreignFolderID), nil)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = doRequest(t, srv, tokenA, "PUT", favoritePath("notebook", foreignNotebookID), nil)
	require.Equal(t, http.StatusNotFound, code)
	require.Equal(t, http.StatusNotFound, doAdminModeRequest(t, srv, tokenA, "PUT", favoritePath("notebook", foreignNotebookID)))
	require.Equal(t, http.StatusNotFound, doAdminModeRequest(t, srv, tokenA, "PUT", favoritePath("folder", foreignFolderID)))

	// Trashed resources: 404 while trashed (even with view access), 204 after
	// restore.
	_, err := srv.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view'])`,
		orgA, privateID, userB)
	require.NoError(t, err)
	code, _ = doRequest(t, srv, tokenB, "PUT", favoritePath("notebook", privateID), nil)
	require.Equal(t, http.StatusNoContent, code)

	code, _ = doRequest(t, srv, tokenA, "DELETE", "/api/v1/notebooks/"+privateID, nil)
	require.Equal(t, http.StatusNoContent, code)
	code, _ = doRequest(t, srv, tokenB, "PUT", favoritePath("notebook", privateID), nil)
	require.Equal(t, http.StatusNotFound, code)

	code, _ = doRequest(t, srv, tokenA, "POST", "/api/v1/trash/restore", map[string]any{"type": "notebook", "id": privateID})
	require.Equal(t, http.StatusOK, code)
	code, _ = doRequest(t, srv, tokenB, "PUT", favoritePath("notebook", privateID), nil)
	require.Equal(t, http.StatusNoContent, code)
}

func TestFavoritesListGroupingAndFiltering(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	now := time.Now().UnixNano()

	tokenA := registerAndGetToken(t, srv, fmt.Sprintf("favlist-a-%d@test.com", now), "Fav List Org")
	userA := userIDFromToken(t, srv, tokenA)
	orgA := orgIDFromUser(t, srv, userA)
	userB := insertUser(t, srv, fmt.Sprintf("favlist-b-%d@test.com", now), "User B")
	addOrgMember(t, srv, orgA, userB, "editor")
	tokenB := issueToken(t, userB, orgA, "editor")

	// Folder tree A -> B with a notebook inside B, plus a root notebook.
	code, resp := doRequest(t, srv, tokenA, "POST", "/api/v1/folders", map[string]any{"name": "A"})
	folderAID := resourceID(t, code, resp)
	code, resp = doRequest(t, srv, tokenA, "POST", "/api/v1/folders", map[string]any{"name": "B", "parent_id": folderAID})
	folderBID := resourceID(t, code, resp)
	code, resp = doRequest(t, srv, tokenA, "POST", "/api/v1/notebooks", map[string]any{"title": "Nested Notebook", "folder_id": folderBID})
	nestedID := resourceID(t, code, resp)
	code, resp = doRequest(t, srv, tokenA, "POST", "/api/v1/notebooks", map[string]any{"title": "Root Notebook"})
	rootID := resourceID(t, code, resp)

	// A favorites, oldest first: folder A, nested notebook, root notebook.
	for _, path := range []string{favoritePath("folder", folderAID), favoritePath("notebook", nestedID)} {
		code, _ = doRequest(t, srv, tokenA, "PUT", path, nil)
		require.Equal(t, http.StatusNoContent, code)
		time.Sleep(2 * time.Millisecond)
	}
	code, _ = doRequest(t, srv, tokenA, "PUT", favoritePath("notebook", rootID), nil)
	require.Equal(t, http.StatusNoContent, code)

	groups := getFavorites(t, srv, tokenA)
	require.Len(t, groups, 2)
	require.Equal(t, "folder", groups[0].ResourceType) // canonical order: folders first
	require.Equal(t, "notebook", groups[1].ResourceType)

	require.Len(t, groups[0].Results, 1)
	folderResult := groups[0].Results[0]
	require.Equal(t, folderAID, folderResult.ID)
	require.Equal(t, "A", folderResult.Title)
	require.Equal(t, "Folder", folderResult.Subtitle)
	require.Empty(t, folderResult.FolderPath) // root-level folder has no containing path
	require.True(t, folderResult.IsFavorite)
	require.True(t, folderResult.CanEdit)

	// Notebooks newest first: the root notebook was favorited last.
	require.Len(t, groups[1].Results, 2)
	rootResult := groups[1].Results[0]
	require.Equal(t, rootID, rootResult.ID)
	require.Equal(t, "Root Notebook", rootResult.Title)
	require.Equal(t, "Notebook", rootResult.Subtitle)
	require.Empty(t, rootResult.FolderPath)
	require.True(t, rootResult.IsFavorite)
	require.True(t, rootResult.CanEdit)
	require.False(t, rootResult.HasSnippet, "favorites carry no snippets")

	nestedResult := groups[1].Results[1]
	require.Equal(t, nestedID, nestedResult.ID)
	require.Equal(t, "A / B", nestedResult.FolderPath)
	require.True(t, nestedResult.IsFavorite)

	// Favorites are id-based: a rename shows through.
	code, _ = doRequest(t, srv, tokenA, "PUT", "/api/v1/notebooks/"+nestedID, map[string]any{"title": "Renamed Nested"})
	require.Equal(t, http.StatusOK, code)
	groups = getFavorites(t, srv, tokenA)
	require.Equal(t, "Renamed Nested", groups[1].Results[1].Title)

	// Trashing a favorited notebook hides it while its row remains.
	code, _ = doRequest(t, srv, tokenA, "DELETE", "/api/v1/notebooks/"+rootID, nil)
	require.Equal(t, http.StatusNoContent, code)
	var favoriteRows int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM favorites WHERE user_id = $1 AND resource_id = $2`,
		userA, rootID).Scan(&favoriteRows))
	require.Equal(t, 1, favoriteRows)
	groups = getFavorites(t, srv, tokenA)
	require.Len(t, groups, 2)
	require.Len(t, groups[1].Results, 1)
	require.Equal(t, nestedID, groups[1].Results[0].ID)

	// Restoring brings it back: the favorite never went away.
	code, _ = doRequest(t, srv, tokenA, "POST", "/api/v1/trash/restore", map[string]any{"type": "notebook", "id": rootID})
	require.Equal(t, http.StatusOK, code)
	groups = getFavorites(t, srv, tokenA)
	require.Len(t, groups, 2)
	require.Len(t, groups[1].Results, 2)

	// B sees the notebook after a view grant, read-only.
	_, err := srv.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view'])`,
		orgA, nestedID, userB)
	require.NoError(t, err)
	code, _ = doRequest(t, srv, tokenB, "PUT", favoritePath("notebook", nestedID), nil)
	require.Equal(t, http.StatusNoContent, code)

	groups = getFavorites(t, srv, tokenB)
	require.Len(t, groups, 1)
	require.Equal(t, "notebook", groups[0].ResourceType)
	require.Len(t, groups[0].Results, 1)
	require.Equal(t, nestedID, groups[0].Results[0].ID)
	require.True(t, groups[0].Results[0].IsFavorite)
	require.False(t, groups[0].Results[0].CanEdit)
	require.Equal(t, "A / B", groups[0].Results[0].FolderPath)

	// Revoking the grant hides the entry while the favorite row remains.
	_, err = srv.DB().Pool.Exec(ctx,
		`DELETE FROM acl_entries WHERE resource_type = 'notebook' AND resource_id = $1::uuid AND subject_id = $2`,
		nestedID, userB)
	require.NoError(t, err)
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM favorites WHERE user_id = $1 AND resource_id = $2`,
		userB, nestedID).Scan(&favoriteRows))
	require.Equal(t, 1, favoriteRows)
	require.Empty(t, getFavorites(t, srv, tokenB))

	// A crafted/cross-org favorite row never leaks: metadata is org-scoped.
	tokenC := registerAndGetToken(t, srv, fmt.Sprintf("favlist-c-%d@test.com", now), "Fav Leak Org C")
	code, resp = doRequest(t, srv, tokenC, "POST", "/api/v1/notebooks", map[string]any{"title": "Foreign"})
	foreignID := resourceID(t, code, resp)
	_, err = srv.DB().Pool.Exec(ctx,
		`INSERT INTO favorites (user_id, org_id, resource_type, resource_id) VALUES ($1, $2, 'notebook', $3)`,
		userB, orgA, foreignID)
	require.NoError(t, err)
	require.Empty(t, getFavorites(t, srv, tokenB))
}

func TestFavoritesReFavoriteKeepsOrder(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("fav-order-%d@test.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Fav Order Org")

	code, resp := doRequest(t, srv, token, "POST", "/api/v1/notebooks", map[string]any{"title": "First"})
	firstID := resourceID(t, code, resp)
	code, resp = doRequest(t, srv, token, "POST", "/api/v1/notebooks", map[string]any{"title": "Second"})
	secondID := resourceID(t, code, resp)

	code, _ = doRequest(t, srv, token, "PUT", favoritePath("notebook", firstID), nil)
	require.Equal(t, http.StatusNoContent, code)
	time.Sleep(2 * time.Millisecond)
	code, _ = doRequest(t, srv, token, "PUT", favoritePath("notebook", secondID), nil)
	require.Equal(t, http.StatusNoContent, code)
	time.Sleep(2 * time.Millisecond)

	// Re-favoriting the older notebook must not bump it to the front.
	code, _ = doRequest(t, srv, token, "PUT", favoritePath("notebook", firstID), nil)
	require.Equal(t, http.StatusNoContent, code)

	groups := getFavorites(t, srv, token)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Results, 2)
	require.Equal(t, secondID, groups[0].Results[0].ID)
	require.Equal(t, firstID, groups[0].Results[1].ID)
}

func TestFavoritesListCap500(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("favcap-%d@test.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Fav Cap Org")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)

	// Seed 501 live, viewable notebooks and one favorite each with distinct
	// timestamps; the listing must return only the newest 500.
	_, err := srv.DB().Pool.Exec(ctx, `
		INSERT INTO notebooks (org_id, title, created_by)
		SELECT $1, 'cap-' || g, $2 FROM generate_series(1, 501) AS g
	`, orgID, userID)
	require.NoError(t, err)
	_, err = srv.DB().Pool.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		SELECT $1, 'notebook', n.id, 'user', $2, ARRAY['view']
		FROM notebooks n WHERE n.org_id = $1 AND n.title LIKE 'cap-%'
	`, orgID, userID)
	require.NoError(t, err)
	_, err = srv.DB().Pool.Exec(ctx, `
		INSERT INTO favorites (user_id, org_id, resource_type, resource_id, created_at)
		SELECT $1, $2, 'notebook', n.id,
		       now() - (row_number() OVER (ORDER BY n.title)) * interval '1 second'
		FROM notebooks n WHERE n.org_id = $2 AND n.title LIKE 'cap-%'
	`, userID, orgID)
	require.NoError(t, err)

	groups := getFavorites(t, srv, token)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Results, 500)
}
