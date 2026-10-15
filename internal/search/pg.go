package search

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// defaultLimitPerType applies when the caller passes <= 0; maxLimitPerType
	// clamps anything above it so one search can never fan out unbounded.
	defaultLimitPerType = 50
	maxLimitPerType     = 200

	// The SQL window is resource-level (the picked CTE collapses each resource
	// to one row before the outer LIMIT): max(fetchLimitFactor×limit,
	// minFetchLimitPerType), clamped to maxLimitPerType. The factor follows
	// the design's max(4×limit, 50) spirit — it leaves the slice step
	// something to cut — and the floor keeps a small caller limit from
	// shrinking the window into a starvation trap.
	fetchLimitFactor     = 5
	minFetchLimitPerType = 50
)

// The @@ predicates spell the V135 GIN expression indexes exactly —
// to_tsvector('simple', <column>) @@ to_tsquery('simple', $2) — which keeps
// identifier-like terms at index speeds (milliseconds). Latency is still
// data-dependent: a high-frequency term can flip the planner to a scan that
// re-evaluates to_tsvector per row (measured ~134ms over 42k cells, scaling
// with the org's content), so callers should encourage refining the query.
//
// The picked CTE de-duplicates per resource in SQL, and the outer query
// computes ts_headline only for the one picked row of each resource. Keeping
// ts_headline outside the CTE is deliberate: inside the DISTINCT ON subquery
// the planner evaluates it for every matched cell (verified with EXPLAIN),
// which is exactly the work the pick exists to avoid. Within a resource the
// pick is deterministic — the topmost cell (c.position, unique per notebook)
// and the oldest widget (w.created_at then w.id; widgets have no position).
//
// The ts_headline option string uses the quoted-empty form
// (StartSel="", StopSel=""): Postgres rejects an unquoted empty option value
// ("invalid parameter list format") and mis-parses the combined unquoted
// form, so this is the supported way to ask for marker-free snippets. The
// snippet is plain text; the client does the highlighting.
const notebookContentSQL = `
	WITH picked AS (
	    SELECT DISTINCT ON (c.notebook_id) c.id AS cell_id, c.notebook_id, n.updated_at
	    FROM cells c
	    JOIN notebooks n ON n.id = c.notebook_id
	    WHERE n.org_id = $1 AND n.deleted_at IS NULL AND c.source <> ''
	      AND to_tsvector('simple', c.source) @@ to_tsquery('simple', $2)
	    ORDER BY c.notebook_id, n.updated_at DESC, c.position
	)
	SELECT p.notebook_id::text,
	       ts_headline('simple', c.source, to_tsquery('simple', $2),
	                   'MaxWords=24, MinWords=8, ShortWord=2, StartSel="", StopSel=""')
	FROM picked p
	JOIN cells c ON c.id = p.cell_id
	ORDER BY p.updated_at DESC
	LIMIT $3`

const dashboardContentSQL = `
	WITH picked AS (
	    SELECT DISTINCT ON (w.dashboard_id) w.id AS widget_id, w.dashboard_id, d.updated_at
	    FROM widgets w
	    JOIN dashboards d ON d.id = w.dashboard_id
	    WHERE d.org_id = $1 AND d.deleted_at IS NULL AND w.query IS NOT NULL AND w.query <> ''
	      AND to_tsvector('simple', w.query) @@ to_tsquery('simple', $2)
	    ORDER BY w.dashboard_id, d.updated_at DESC, w.created_at, w.id
	)
	SELECT p.dashboard_id::text,
	       ts_headline('simple', w.query, to_tsquery('simple', $2),
	                   'MaxWords=24, MinWords=8, ShortWord=2, StartSel="", StopSel=""')
	FROM picked p
	JOIN widgets w ON w.id = p.widget_id
	ORDER BY p.updated_at DESC
	LIMIT $3`

// PGSearcher implements Searcher with PostgreSQL core full-text search over
// notebook cell sources and dashboard widget queries.
type PGSearcher struct {
	pool *pgxpool.Pool
}

// NewPGSearcher builds a Searcher backed by an existing pgx pool.
func NewPGSearcher(pool *pgxpool.Pool) *PGSearcher {
	return &PGSearcher{pool: pool}
}

// ContentMatches runs one bounded FTS query per supported requested type and
// returns the matching resources with plain-text snippets. Results are
// org-scoped but not ACL-filtered — callers apply view/edit policy (see
// Searcher). The map holds an entry only for supported, requested types that
// produced hits. An empty tsquery (punctuation-only, CJK-only,
// whitespace-only input) short-circuits without touching the database.
func (s *PGSearcher) ContentMatches(ctx context.Context, orgID string, types []string, query string, limitPerType int) (map[string][]ContentHit, error) {
	hits := make(map[string][]ContentHit)

	tsquery := BuildTSQuery(query)
	if tsquery == "" {
		return hits, nil
	}

	limit := clampLimitPerType(limitPerType)
	fetch := fetchLimitPerType(limit)

	seen := make(map[string]struct{}, len(types))
	for _, resourceType := range types {
		if _, ok := seen[resourceType]; ok {
			continue
		}
		seen[resourceType] = struct{}{}

		var (
			results []ContentHit
			err     error
		)
		switch resourceType {
		case TypeNotebook:
			results, err = s.contentMatches(ctx, notebookContentSQL, orgID, tsquery, fetch, limit)
			if err != nil {
				return nil, fmt.Errorf("search notebook content: %w", err)
			}
		case TypeDashboard:
			results, err = s.contentMatches(ctx, dashboardContentSQL, orgID, tsquery, fetch, limit)
			if err != nil {
				return nil, fmt.Errorf("search dashboard content: %w", err)
			}
		default:
			continue
		}
		if len(results) > 0 {
			hits[resourceType] = results
		}
	}
	return hits, nil
}

// contentMatches runs one content query and scans the per-resource rows. The
// SQL already collapses each resource to one row (DISTINCT ON in the picked
// CTE); the in-loop de-duplication stays as a cheap safety net so a query
// shape regression can never surface duplicate resources to callers. Result
// order is preserved and the list is sliced to limit.
func (s *PGSearcher) contentMatches(ctx context.Context, sql, orgID, tsquery string, fetch, limit int) ([]ContentHit, error) {
	rows, err := s.pool.Query(ctx, sql, orgID, tsquery, fetch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := make([]ContentHit, 0, limit)
	seen := make(map[string]struct{}, limit)
	for rows.Next() {
		var hit ContentHit
		if err := rows.Scan(&hit.ResourceID, &hit.Snippet); err != nil {
			return nil, err
		}
		if _, ok := seen[hit.ResourceID]; ok {
			continue
		}
		seen[hit.ResourceID] = struct{}{}
		hits = append(hits, hit)
		if len(hits) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// clampLimitPerType normalizes the per-type cap: <= 0 means "caller did not
// choose" (default 50), and anything above 200 is clamped.
func clampLimitPerType(limit int) int {
	if limit <= 0 {
		return defaultLimitPerType
	}
	if limit > maxLimitPerType {
		return maxLimitPerType
	}
	return limit
}

// fetchLimitPerType is the resource-level SQL window:
// clamp(max(5×limit, 50), 50, 200). The floor keeps small caller limits from
// starving the result set, the factor leaves the slice step headroom, and the
// ceiling bounds the work one search can trigger.
func fetchLimitPerType(limit int) int {
	fetch := limit * fetchLimitFactor
	if fetch < minFetchLimitPerType {
		fetch = minFetchLimitPerType
	}
	if fetch > maxLimitPerType {
		fetch = maxLimitPerType
	}
	return fetch
}
