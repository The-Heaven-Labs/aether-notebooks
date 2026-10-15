package search

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Content types the PostgreSQL implementation supports. Anything else a
// caller requests is skipped (a future backend may serve more types).
const (
	notebookType  = "notebook"
	dashboardType = "dashboard"
)

const (
	// defaultLimitPerType applies when the caller passes <= 0; maxLimitPerType
	// clamps anything above it so one search can never fan out unbounded.
	defaultLimitPerType = 50
	maxLimitPerType     = 200

	// fetchLimitFactor over-fetches per type so de-duplication (several cells
	// of the same notebook matching) still leaves up to limitPerType
	// resources, while the SQL never loads an unbounded row set.
	fetchLimitFactor = 5
)

// The content queries spell the GIN-index expression exactly —
// to_tsvector('simple', <column>) @@ to_tsquery('simple', $2) — so the
// planner can use idx_cells_source_fts / idx_widgets_query_fts (V135).
//
// The ts_headline option string uses the quoted-empty form
// (StartSel="", StopSel=""): Postgres rejects an unquoted empty option value
// ("invalid parameter list format") and mis-parses the combined unquoted
// form, so this is the supported way to ask for marker-free snippets. The
// snippet is plain text; the client does the highlighting.
const notebookContentSQL = `
	SELECT c.notebook_id::text,
	       ts_headline('simple', c.source, to_tsquery('simple', $2),
	                   'MaxWords=24, MinWords=8, ShortWord=2, StartSel="", StopSel=""')
	FROM cells c
	JOIN notebooks n ON n.id = c.notebook_id
	WHERE n.org_id = $1 AND n.deleted_at IS NULL AND c.source <> ''
	  AND to_tsvector('simple', c.source) @@ to_tsquery('simple', $2)
	ORDER BY n.updated_at DESC
	LIMIT $3`

const dashboardContentSQL = `
	SELECT w.dashboard_id::text,
	       ts_headline('simple', w.query, to_tsquery('simple', $2),
	                   'MaxWords=24, MinWords=8, ShortWord=2, StartSel="", StopSel=""')
	FROM widgets w
	JOIN dashboards d ON d.id = w.dashboard_id
	WHERE d.org_id = $1 AND d.deleted_at IS NULL AND w.query IS NOT NULL AND w.query <> ''
	  AND to_tsvector('simple', w.query) @@ to_tsquery('simple', $2)
	ORDER BY d.updated_at DESC
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
// returns the matching resources with plain-text snippets. The map holds an
// entry only for supported, requested types that produced hits. An empty
// tsquery (punctuation-only, CJK-only, whitespace-only input) short-circuits
// without touching the database.
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
		case notebookType:
			results, err = s.contentMatches(ctx, notebookContentSQL, orgID, tsquery, fetch, limit)
			if err != nil {
				return nil, fmt.Errorf("search notebook content: %w", err)
			}
		case dashboardType:
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

// contentMatches runs one content query and collapses the raw cell/widget rows
// into per-resource hits: the first row for a resource wins, result order is
// preserved, and the list is sliced to limit after de-duplication.
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

// fetchLimitPerType is the SQL LIMIT: room for de-duplication (several cells
// of one notebook can match) while still bounded by maxLimitPerType.
func fetchLimitPerType(limit int) int {
	fetch := limit * fetchLimitFactor
	if fetch > maxLimitPerType {
		fetch = maxLimitPerType
	}
	return fetch
}
