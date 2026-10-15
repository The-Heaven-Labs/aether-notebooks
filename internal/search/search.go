// Package search provides whole-catalog content matching behind a swappable
// Searcher interface. The PostgreSQL implementation (PGSearcher) uses core
// full-text search only — no extensions — so instances on locked-down managed
// Postgres can use it as-is.
package search

import "context"

// ContentHit is one content match: the resource that owns the matched text
// plus a plain-text snippet (highlight markers stripped; the client does the
// highlighting).
type ContentHit struct {
	ResourceID string
	Snippet    string
}

// Searcher matches resource content. types is a subset of
// {"notebook","dashboard"}; implementation-specific backends may support more.
// The PostgreSQL implementation uses core FTS (to_tsvector('simple', …)).
// A future ClickHouse implementation can replace it without API/UI changes.
type Searcher interface {
	ContentMatches(ctx context.Context, orgID string, types []string, query string, limitPerType int) (map[string][]ContentHit, error)
}
