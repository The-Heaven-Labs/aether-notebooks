package search

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
)

// searchTestDSN mirrors the repo-wide test convention: scripts/go-test.sh
// exports AETHER_DATABASE_URL pointing at the disposable per-worktree
// database, and a bare `go test` falls back to the local dev database.
func searchTestDSN() string {
	if dsn := os.Getenv("AETHER_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
}

// setupSearchTestDB is the one setup helper used by every test in this file.
// The search package has no Server dependency, so it follows the pattern of
// internal/scheduler and internal/dashboarddoc: connect straight to Postgres
// and apply migrations through the exported database.Migrate entry point
// (internal/database's own tests use it the same way). Migrate is idempotent
// and advisory-locked, so calling it per test is cheap.
func setupSearchTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db, err := database.Connect(context.Background(), searchTestDSN(), "")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background()))
	t.Cleanup(db.Close)
	return db.Pool
}

// seedSearchOrg inserts an org plus a user that owns the seeded resources.
// Cleanup deletes the org first: notebooks/dashboards.created_by has no
// ON DELETE, so the user can only go after its org's resources cascade away.
func seedSearchOrg(t *testing.T, pool *pgxpool.Pool, label string) (orgID, userID string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	orgID = uuid.NewString()
	userID = uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID, label+" Org", strings.ToLower(strings.ReplaceAll(label, " ", "-"))+"-"+suffix)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID, "search-"+suffix+"@test.local", label+" User")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return orgID, userID
}

// seedSearchNotebook creates a notebook whose cells are the given sources, in
// order.
func seedSearchNotebook(t *testing.T, pool *pgxpool.Pool, orgID, userID, title string, sources ...string) string {
	t.Helper()
	ctx := context.Background()
	notebookID := uuid.NewString()
	_, err := pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
		notebookID, orgID, title, userID)
	require.NoError(t, err)
	for i, source := range sources {
		_, err := pool.Exec(ctx,
			`INSERT INTO cells (notebook_id, position, type, language, source) VALUES ($1, $2, 'code', 'sql', $3)`,
			notebookID, i, source)
		require.NoError(t, err)
	}
	return notebookID
}

// seedSearchDashboard creates a dashboard whose query widgets are the given
// SQL strings. A connector is created per dashboard because
// widgets_source_check requires query-backed widgets to reference one.
func seedSearchDashboard(t *testing.T, pool *pgxpool.Pool, orgID, userID string, queries ...string) string {
	t.Helper()
	ctx := context.Background()
	dashboardID := uuid.NewString()
	connectorID := uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO connectors (id, org_id, name, type, config_encrypted) VALUES ($1, $2, 'Search Test Connector', 'postgres', '\x'::bytea)`,
		connectorID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO dashboards (id, org_id, title, created_by) VALUES ($1, $2, 'Search Test Dashboard', $3)`,
		dashboardID, orgID, userID)
	require.NoError(t, err)
	for _, query := range queries {
		_, err := pool.Exec(ctx,
			`INSERT INTO widgets (dashboard_id, type, layout, config, connector_id, query)
			 VALUES ($1, 'table', '{}', '{}', $2, $3)`,
			dashboardID, connectorID, query)
		require.NoError(t, err)
	}
	return dashboardID
}

func TestPGSearcherNotebookContent(t *testing.T) {
	pool := setupSearchTestDB(t)
	searcher := NewPGSearcher(pool)
	ctx := context.Background()

	orgA, userA := seedSearchOrg(t, pool, "PGSearcher A")
	orgB, userB := seedSearchOrg(t, pool, "PGSearcher B")

	// Two matching cells plus one non-matching cell: the notebook must be
	// reported once, with the first matching cell's snippet.
	notebookA := seedSearchNotebook(t, pool, orgA, userA, "Revenue notebook",
		"SELECT revenue FROM orders",
		"SELECT REVENUE FROM orders",
		"SELECT secret_metric FROM hidden_table",
		"SELECT 1")
	notebookB := seedSearchNotebook(t, pool, orgB, userB, "Other org notebook",
		"SELECT revenue FROM orders")

	hits, err := searcher.ContentMatches(ctx, orgA, []string{"notebook"}, "Revenue", 0)
	require.NoError(t, err)
	require.Len(t, hits["notebook"], 1, "two matching cells must dedupe to one notebook hit")
	hit := hits["notebook"][0]
	require.Equal(t, notebookA, hit.ResourceID)
	require.NotEmpty(t, hit.Snippet)
	require.Contains(t, strings.ToLower(hit.Snippet), "revenue", "snippet must contain the matched word")
	for _, marker := range []string{"<b>", "</b>", "«", "»"} {
		require.NotContains(t, hit.Snippet, marker, "snippet must be plain text")
	}
	require.Len(t, hits, 1)

	// Org isolation: org B sees its own notebook for the shared term...
	hitsB, err := searcher.ContentMatches(ctx, orgB, []string{"notebook"}, "revenue", 0)
	require.NoError(t, err)
	require.Len(t, hitsB["notebook"], 1)
	require.Equal(t, notebookB, hitsB["notebook"][0].ResourceID)

	// ...and nothing at all for a term that only exists in org A's cells.
	hitsB, err = searcher.ContentMatches(ctx, orgB, []string{"notebook"}, "secret_metric", 0)
	require.NoError(t, err)
	require.Empty(t, hitsB["notebook"], "org B must never see org A's cell content")

	// Trashed notebooks disappear from content matches.
	_, err = pool.Exec(ctx, `UPDATE notebooks SET deleted_at = NOW() WHERE id = $1`, notebookA)
	require.NoError(t, err)
	hits, err = searcher.ContentMatches(ctx, orgA, []string{"notebook"}, "secret_metric", 0)
	require.NoError(t, err)
	require.Empty(t, hits["notebook"], "soft-deleted notebooks must not match")
}

func TestPGSearcherDashboardContent(t *testing.T) {
	pool := setupSearchTestDB(t)
	searcher := NewPGSearcher(pool)
	ctx := context.Background()

	orgA, userA := seedSearchOrg(t, pool, "PGSearcher Dash A")
	orgB, userB := seedSearchOrg(t, pool, "PGSearcher Dash B")

	// Two matching widgets must dedupe to one dashboard hit; each org also
	// gets a term that exists nowhere else so cross-org leaks are visible.
	dashboardA := seedSearchDashboard(t, pool, orgA, userA,
		"SELECT sum(amount) AS total FROM payments",
		"SELECT avg(amount) AS mean FROM payments",
		"SELECT secret_rollup FROM ledger",
		"SELECT 1")
	dashboardB := seedSearchDashboard(t, pool, orgB, userB,
		"SELECT sum(amount) AS total FROM payments",
		"SELECT other_rollup FROM ledger")

	hits, err := searcher.ContentMatches(ctx, orgA, []string{"dashboard"}, "amount", 0)
	require.NoError(t, err)
	require.Len(t, hits["dashboard"], 1, "two matching widgets must dedupe to one dashboard hit")
	hit := hits["dashboard"][0]
	require.Equal(t, dashboardA, hit.ResourceID)
	require.Contains(t, strings.ToLower(hit.Snippet), "amount")
	for _, marker := range []string{"<b>", "</b>", "«", "»"} {
		require.NotContains(t, hit.Snippet, marker, "snippet must be plain text")
	}
	require.Len(t, hits, 1)

	// Org isolation, both directions.
	hitsB, err := searcher.ContentMatches(ctx, orgB, []string{"dashboard"}, "amount", 0)
	require.NoError(t, err)
	require.Len(t, hitsB["dashboard"], 1)
	require.Equal(t, dashboardB, hitsB["dashboard"][0].ResourceID)

	hitsB, err = searcher.ContentMatches(ctx, orgB, []string{"dashboard"}, "secret_rollup", 0)
	require.NoError(t, err)
	require.Empty(t, hitsB["dashboard"], "org B must never see org A's widget SQL")

	hitsA, err := searcher.ContentMatches(ctx, orgA, []string{"dashboard"}, "other_rollup", 0)
	require.NoError(t, err)
	require.Empty(t, hitsA["dashboard"], "org A must never see org B's widget SQL")

	// Soft-deleted dashboards disappear from content matches.
	_, err = pool.Exec(ctx, `UPDATE dashboards SET deleted_at = NOW() WHERE id = $1`, dashboardA)
	require.NoError(t, err)
	hits, err = searcher.ContentMatches(ctx, orgA, []string{"dashboard"}, "amount", 0)
	require.NoError(t, err)
	require.Empty(t, hits["dashboard"], "soft-deleted dashboards must not match")
}

func TestPGSearcherPrefixMatching(t *testing.T) {
	pool := setupSearchTestDB(t)
	searcher := NewPGSearcher(pool)
	ctx := context.Background()

	org, user := seedSearchOrg(t, pool, "PGSearcher Prefix")
	notebookID := seedSearchNotebook(t, pool, org, user, "Prefix notebook",
		"SELECT * FROM order_items WHERE revenue > 0")

	// "rev" is a prefix of "revenue"; the builder's quoted runs split on
	// underscores exactly like the document parser, so "order" and "items"
	// both find the identifier — and so does its whole spelling.
	for _, q := range []string{"rev", "order", "items", "order_items"} {
		hits, err := searcher.ContentMatches(ctx, org, []string{"notebook"}, q, 0)
		require.NoError(t, err, "query %q", q)
		require.Len(t, hits["notebook"], 1, "query %q", q)
		require.Equal(t, notebookID, hits["notebook"][0].ResourceID, "query %q", q)
	}

	// Mid-word matching is a documented non-goal (core FTS is word/prefix
	// based): "tem" must not find "items".
	hits, err := searcher.ContentMatches(ctx, org, []string{"notebook"}, "tem", 0)
	require.NoError(t, err)
	require.Empty(t, hits["notebook"])
}

func TestPGSearcherTypeFiltering(t *testing.T) {
	pool := setupSearchTestDB(t)
	searcher := NewPGSearcher(pool)
	ctx := context.Background()

	org, user := seedSearchOrg(t, pool, "PGSearcher Types")
	seedSearchNotebook(t, pool, org, user, "Types notebook", "SELECT revenue FROM orders")
	seedSearchDashboard(t, pool, org, user, "SELECT revenue FROM orders")

	// Requesting only notebooks must not run (or report) the dashboard pass.
	hits, err := searcher.ContentMatches(ctx, org, []string{"notebook"}, "revenue", 0)
	require.NoError(t, err)
	require.Len(t, hits["notebook"], 1)
	require.Len(t, hits, 1)
	_, hasDashboard := hits["dashboard"]
	require.False(t, hasDashboard)

	// Unknown types are ignored; supported ones in the same list still run.
	hits, err = searcher.ContentMatches(ctx, org, []string{"warehouse", "dashboard", "folder"}, "revenue", 0)
	require.NoError(t, err)
	require.Len(t, hits["dashboard"], 1)
	require.Len(t, hits, 1)

	// No requested types → empty map, no error.
	for _, types := range [][]string{nil, {}} {
		hits, err = searcher.ContentMatches(ctx, org, types, "revenue", 0)
		require.NoError(t, err)
		require.Empty(t, hits)
	}
}

func TestPGSearcherLimitPerType(t *testing.T) {
	pool := setupSearchTestDB(t)
	ctx := context.Background()

	// Clamp/default behavior is pure math, asserted directly.
	require.Equal(t, defaultLimitPerType, clampLimitPerType(0))
	require.Equal(t, defaultLimitPerType, clampLimitPerType(-3))
	require.Equal(t, 7, clampLimitPerType(7))
	require.Equal(t, maxLimitPerType, clampLimitPerType(10_000))
	require.Equal(t, 10, fetchLimitPerType(2))
	require.Equal(t, maxLimitPerType, fetchLimitPerType(maxLimitPerType))

	searcher := NewPGSearcher(pool)
	org, user := seedSearchOrg(t, pool, "PGSearcher Limit")
	for i := 0; i < 3; i++ {
		seedSearchNotebook(t, pool, org, user, fmt.Sprintf("Limit notebook %d", i),
			"SELECT revenue FROM orders",
			"SELECT revenue FROM invoices")
	}

	// Six matching cells across three notebooks: fetch (10) covers them all,
	// dedupe leaves three, and the slice to 2 is applied last.
	hits, err := searcher.ContentMatches(ctx, org, []string{"notebook"}, "revenue", 2)
	require.NoError(t, err)
	require.Len(t, hits["notebook"], 2)

	// limit <= 0 falls back to the default (50), which finds all three.
	hits, err = searcher.ContentMatches(ctx, org, []string{"notebook"}, "revenue", 0)
	require.NoError(t, err)
	require.Len(t, hits["notebook"], 3)

	// Above the cap clamps to 200 without error (three seeded).
	hits, err = searcher.ContentMatches(ctx, org, []string{"notebook"}, "revenue", 10_000)
	require.NoError(t, err)
	require.Len(t, hits["notebook"], 3)
}

func TestPGSearcherEmptyOrUnusableQuerySkipsTheDatabase(t *testing.T) {
	pool := setupSearchTestDB(t)
	org, _ := seedSearchOrg(t, pool, "PGSearcher Empty")

	// A closed pool can never serve a query: if ContentMatches attempted one,
	// it would error instead of returning. The empty-tsquery short-circuit
	// must return before any database access.
	closed, err := pgxpool.New(context.Background(), searchTestDSN())
	require.NoError(t, err)
	closed.Close()
	searcher := NewPGSearcher(closed)

	for _, q := range []string{"", "   ", "\t\n", "你好世界", "%_", ":", "()", "-", "&|"} {
		hits, err := searcher.ContentMatches(context.Background(), org, []string{"notebook", "dashboard"}, q, 0)
		require.NoError(t, err, "query %q", q)
		require.Empty(t, hits, "query %q", q)
	}
}

func TestPGSearcherInjectionSafety(t *testing.T) {
	pool := setupSearchTestDB(t)
	searcher := NewPGSearcher(pool)
	ctx := context.Background()

	org, user := seedSearchOrg(t, pool, "PGSearcher Injection")
	notebookID := seedSearchNotebook(t, pool, org, user, "Injection notebook",
		"SELECT revenue FROM orders WHERE region = 'EMEA'")

	// None of these may error, and none may over-match the fixture (the cell
	// contains no a/b/c/drop/table/cells lexemes).
	for _, q := range []string{"'", "%_", "a & b | c", ":*", "'; DROP TABLE cells; --", "((", "\\"} {
		hits, err := searcher.ContentMatches(ctx, org, []string{"notebook", "dashboard"}, q, 0)
		require.NoError(t, err, "query %q must never error", q)
		require.Empty(t, hits, "query %q must not match the fixture", q)
	}

	// The table (and the row) survived every query.
	var cells int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM cells WHERE notebook_id = $1`, notebookID).Scan(&cells))
	require.Equal(t, 1, cells)
}

func TestPGSearcherSnippetIsBoundedAndPlain(t *testing.T) {
	pool := setupSearchTestDB(t)
	searcher := NewPGSearcher(pool)
	ctx := context.Background()

	org, user := seedSearchOrg(t, pool, "PGSearcher Snippet")

	// ~200 words of SQL-ish text with the matched word in the middle: the
	// snippet must be a bounded fragment (MaxWords=24), not the whole cell.
	words := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		words = append(words, fmt.Sprintf("word%03d_alpha", i))
	}
	words[100] = "revenue_metric"
	source := "SELECT " + strings.Join(words, ", ") + " FROM big_table"

	notebookID := seedSearchNotebook(t, pool, org, user, "Big cell notebook", source)

	hits, err := searcher.ContentMatches(ctx, org, []string{"notebook"}, "revenue_metric", 0)
	require.NoError(t, err)
	require.Len(t, hits["notebook"], 1)
	require.Equal(t, notebookID, hits["notebook"][0].ResourceID)

	snippet := hits["notebook"][0].Snippet
	require.NotEmpty(t, snippet)
	require.Contains(t, snippet, "revenue_metric", "the matched word stays in the snippet")
	require.Less(t, utf8.RuneCountInString(snippet), 400, "MaxWords=24 must bound the snippet length")
	for _, marker := range []string{"<b>", "</b>", "«", "»", "StartSel", "StopSel"} {
		require.NotContains(t, snippet, marker, "snippet must be plain text")
	}
}
