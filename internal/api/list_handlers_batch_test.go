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
