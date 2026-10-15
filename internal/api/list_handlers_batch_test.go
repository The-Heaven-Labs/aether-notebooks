package api_test

// Query-count guard for the batched migration of the remaining list endpoints
// (Task A5). GET /dashboards must resolve view/edit permissions and the
// non-editor snapshot substitution in a bounded, item-count-independent number
// of queries, so the per-item checkPermission loop cannot come back silently.
//
// The fixture seeds resources with SQL (same approach as the white-box
// permission suites) but drives the HTTP handler, since the response shape is
// the contract being pinned. Every seeded dashboard is published and granted
// view-only, so the counted path is exactly the batch substitution path.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDashboardListQueryCount is the N+1 guard: a small (10) and a large (100)
// published view-only dashboard fixture must issue the same bounded number of
// queries, and the large response must be the snapshot-substituted listing.
func TestDashboardListQueryCount(t *testing.T) {
	srv, counter := setupTestServerWithQueryCounter(t)

	seedDashboards := func(n int) *folderBatchFixture {
		t.Helper()
		f := setupFolderBatchFixture(t, srv)
		for i := 0; i < n; i++ {
			id := f.insertDashboard(t, fmt.Sprintf("count-dash-%03d", i), nil, f.otherUserID)
			f.grant(t, "dashboard", id, []string{"view"})
			_, err := f.srv.DB().Pool.Exec(context.Background(),
				`INSERT INTO dashboard_versions (dashboard_id, org_id, version, title, settings, widgets, content_hash, created_by)
				 VALUES ($1, $2, 1, $3, '{"auto_refresh_seconds": 5}', '[]', 'count-hash', $4)`,
				id, f.orgID, fmt.Sprintf("Published %03d", i), f.otherUserID)
			require.NoError(t, err)
		}
		return f
	}

	countList := func(f *folderBatchFixture) int64 {
		t.Helper()
		counter.n.Store(0)
		status, body := f.get(t, "/api/v1/dashboards")
		require.Equal(t, http.StatusOK, status, string(body))
		return counter.n.Load()
	}

	small := seedDashboards(10)
	large := seedDashboards(100)
	countSmall := countList(small)
	countLarge := countList(large)
	t.Logf("query counts: 10 dashboards=%d, 100 dashboards=%d", countSmall, countLarge)

	require.Lessf(t, countSmall, int64(40),
		"listing 10 dashboards must resolve in a bounded number of queries (got %d)", countSmall)
	require.LessOrEqualf(t, countLarge, countSmall+8,
		"query count must not grow with the item count (10 dashboards: %d, 100 dashboards: %d)",
		countSmall, countLarge)

	// The counted response is the batch substitution path: a view-only caller
	// sees every seeded dashboard, each with its snapshot title.
	status, body := large.get(t, "/api/v1/dashboards")
	require.Equal(t, http.StatusOK, status, string(body))
	var list []map[string]any
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list, 100)
	for _, d := range list {
		title, _ := d["title"].(string)
		require.True(t, strings.HasPrefix(title, "Published "),
			"the view-only caller must see snapshot titles, got %q", title)
	}
}

// TestListEndpointsQueryCount extends the N+1 guard to the remaining list
// endpoints migrated in Task A5: each listing must resolve its batched
// permissions in a bounded, item-count-independent number of queries. Every
// fixture is a fresh org whose caller can view (and, for connectors, use)
// every seeded row, so no hidden-row discount masks a per-item loop.
func TestListEndpointsQueryCount(t *testing.T) {
	srv, counter := setupTestServerWithQueryCounter(t)

	countGet := func(f *folderBatchFixture, path string) int64 {
		t.Helper()
		counter.n.Store(0)
		status, body := f.get(t, path)
		require.Equal(t, http.StatusOK, status, string(body))
		return counter.n.Load()
	}

	seedNotebooks := func(t *testing.T, f *folderBatchFixture, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			id := f.insertNotebook(t, fmt.Sprintf("count-notebook-%03d", i), nil, f.otherUserID)
			f.grant(t, "notebook", id, []string{"view"})
		}
	}
	seedConnectors := func(t *testing.T, f *folderBatchFixture, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			id := f.insertConnector(t, fmt.Sprintf("count-connector-%03d", i), nil, f.otherUserID)
			f.grant(t, "connector", id, []string{"view", "use"})
		}
	}
	seedTrash := func(t *testing.T, f *folderBatchFixture, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			nb := f.insertNotebook(t, fmt.Sprintf("count-trash-notebook-%03d", i), nil, f.otherUserID)
			f.grant(t, "notebook", nb, []string{"view"})
			conn := f.insertConnector(t, fmt.Sprintf("count-trash-connector-%03d", i), nil, f.otherUserID)
			f.grant(t, "connector", conn, []string{"view"})
			dash := f.insertDashboard(t, fmt.Sprintf("count-trash-dashboard-%03d", i), nil, f.otherUserID)
			f.grant(t, "dashboard", dash, []string{"view"})
		}
		for _, table := range []string{"notebooks", "connectors", "dashboards"} {
			_, err := f.srv.DB().Pool.Exec(context.Background(),
				fmt.Sprintf(`UPDATE %s SET deleted_at = now() WHERE org_id = $1`, table), f.orgID)
			require.NoError(t, err)
		}
	}

	for _, tc := range []struct {
		name  string
		path  string
		small int // seeded rows per type in the small org
		large int // seeded rows per type in the large org
		seed  func(t *testing.T, f *folderBatchFixture, n int)
	}{
		{name: "notebooks", path: "/api/v1/notebooks", small: 10, large: 60, seed: seedNotebooks},
		{name: "connectors", path: "/api/v1/connectors", small: 10, large: 60, seed: seedConnectors},
		{name: "recent", path: "/api/v1/recent", small: 10, large: 60, seed: seedNotebooks},
		{name: "trash", path: "/api/v1/trash", small: 10, large: 30, seed: seedTrash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			smallOrg := setupFolderBatchFixture(t, srv)
			tc.seed(t, smallOrg, tc.small)
			largeOrg := setupFolderBatchFixture(t, srv)
			tc.seed(t, largeOrg, tc.large)

			countSmall := countGet(smallOrg, tc.path)
			countLarge := countGet(largeOrg, tc.path)
			t.Logf("%s query counts: %d/type=%d, %d/type=%d",
				tc.name, tc.small, countSmall, tc.large, countLarge)

			require.Lessf(t, countSmall, int64(40),
				"%s must resolve in a bounded number of queries (got %d)", tc.path, countSmall)
			require.LessOrEqualf(t, countLarge, countSmall+8,
				"%s query count must not grow with the item count (%d/type: %d, %d/type: %d)",
				tc.path, tc.small, countSmall, tc.large, countLarge)
		})
	}
}
