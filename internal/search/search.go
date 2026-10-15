// Package search provides whole-catalog content matching behind a swappable
// Searcher interface. The PostgreSQL implementation (PGSearcher) uses core
// full-text search only — no extensions — so instances on locked-down managed
// Postgres can use it as-is.
//
// Latency is data-dependent, not a constant: identifier-like terms are served
// by the V135 GIN expression indexes (milliseconds), but a high-frequency term
// (e.g. "select") can flip the planner to a scan that re-evaluates
// to_tsvector per row — measured at ~134ms over 42k cells and scaling with the
// org's content size. Callers/UI should encourage refining the query rather
// than paging over broad terms.
package search

import "context"

// Content types supported by the PostgreSQL implementation. Callers pass
// these in the types slice of ContentMatches and read them back as map keys;
// other implementations may support more.
const (
	TypeNotebook  = "notebook"
	TypeDashboard = "dashboard"
)

// ContentHit is one content match: the resource that owns the matched text
// plus a plain-text snippet (highlight markers stripped; the client does the
// highlighting).
type ContentHit struct {
	ResourceID string
	Snippet    string
}

// Searcher matches resource content.
//
// Results are org-scoped but deliberately NOT ACL-filtered: callers own the
// visibility policy and must drop notebook hits the caller cannot view and
// dashboard hits the caller cannot edit (widget SQL is editor-only in the
// UI). types is a subset of {TypeNotebook, TypeDashboard};
// implementation-specific backends may support more, and unsupported types
// are skipped. limitPerType caps the resources returned per type
// (resource-level, after de-duplication): <= 0 means the 50 default, values
// above 200 are clamped. The returned map is sparse — a type with no hits has
// no key.
//
// The PostgreSQL implementation uses core FTS (to_tsvector('simple', …));
// a future ClickHouse implementation can replace it without API/UI changes.
type Searcher interface {
	ContentMatches(ctx context.Context, orgID string, types []string, query string, limitPerType int) (map[string][]ContentHit, error)
}
