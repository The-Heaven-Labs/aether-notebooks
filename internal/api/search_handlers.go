package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/the-heaven-labs/aether/internal/search"
)

const (
	// searchDefaultLimit is the per-type page size when the caller does not
	// choose one.
	searchDefaultLimit = 5
	// searchMaxLimit is the per-type page ceiling; larger asks clamp.
	searchMaxLimit = 20
	// searchMaxQueryRunes bounds the trimmed query length.
	searchMaxQueryRunes = 200
)

// searchGroupOrder is the fixed response group order for search results,
// independent of the order the caller listed requested types in.
var searchGroupOrder = []string{
	"notebook", "dashboard", "folder", "connector", "agent",
	"model_config", "skill", "tool", "mcp_server",
}

// searchTitleSQL holds the per-type title candidate queries. Each takes
// ($1 org, $2 the escaped ILIKE pattern, $3 the candidate window) and orders
// deterministically (updated_at DESC, id) so equal-timestamp truncation is
// stable. The tables carrying a deleted_at column (V072: notebooks, folders,
// connectors, dashboards) filter trashed rows; agents, model_configs, skills,
// tools, and mcp_servers have no deleted_at column, so they omit the clause.
var searchTitleSQL = map[string]string{
	"notebook": `SELECT id::text, title, updated_at FROM notebooks
		WHERE org_id = $1 AND deleted_at IS NULL
		  AND (title ILIKE $2 OR COALESCE(description, '') ILIKE $2)
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"folder": `SELECT id::text, name, updated_at FROM folders
		WHERE org_id = $1 AND deleted_at IS NULL AND name ILIKE $2
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"connector": `SELECT id::text, name, updated_at FROM connectors
		WHERE org_id = $1 AND deleted_at IS NULL AND name ILIKE $2
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"agent": `SELECT id::text, name, updated_at FROM agents
		WHERE org_id = $1 AND (name ILIKE $2 OR COALESCE(description, '') ILIKE $2)
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"model_config": `SELECT id::text, name, updated_at FROM model_configs
		WHERE org_id = $1 AND name ILIKE $2
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"skill": `SELECT id::text, name, updated_at FROM skills
		WHERE org_id = $1 AND (name ILIKE $2 OR COALESCE(description, '') ILIKE $2)
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"tool": `SELECT id::text, name, updated_at FROM tools
		WHERE org_id = $1 AND (name ILIKE $2 OR COALESCE(description, '') ILIKE $2)
		ORDER BY updated_at DESC, id::text LIMIT $3`,
	"mcp_server": `SELECT id::text, name, updated_at FROM mcp_servers
		WHERE org_id = $1 AND name ILIKE $2
		ORDER BY updated_at DESC, id::text LIMIT $3`,
}

// searchDashboardTitleSQL is the dashboard title candidate query: it unions
// draft-title and latest-snapshot-title matches, and the caller's effective
// title (resolved in Go) decides which rows actually survive. The LATERAL pick
// is the newest published version; a NULL result means the dashboard has never
// been published (viewers then keep the draft title, matching the dashboard
// read paths).
const searchDashboardTitleSQL = `
	SELECT d.id::text, d.title, d.folder_id::text, d.updated_at, v.title
	FROM dashboards d
	LEFT JOIN LATERAL (
		SELECT dv.title FROM dashboard_versions dv
		WHERE dv.dashboard_id = d.id
		ORDER BY dv.version DESC
		LIMIT 1
	) v ON true
	WHERE d.org_id = $1 AND d.deleted_at IS NULL
	  AND (d.title ILIKE $2 OR v.title ILIKE $2)
	ORDER BY d.updated_at DESC, d.id::text
	LIMIT $3`

// searchImplementedTypeSet is the set of resource types the search core can
// actually run: the types carrying a title query (searchTitleSQL, plus
// dashboard's dedicated query) that also hold a slot in searchGroupOrder. A
// type missing either half cannot be searched — searchTitleHits would fail
// with an unknown type, or the group loop would silently skip it.
var searchImplementedTypeSet = func() map[string]bool {
	implemented := make(map[string]bool, len(searchGroupOrder))
	for _, resourceType := range searchGroupOrder {
		if resourceType == "dashboard" || searchTitleSQL[resourceType] != "" {
			implemented[resourceType] = true
		}
	}
	return implemented
}()

// searchSupportedTypes is the ordered search whitelist and default type list,
// shared by GET /search and the search_resources tool: the favorites whitelist
// (the accepted resource universe, derived from resourceTable) intersected
// with the implemented types above. Deriving from the implemented source keeps
// a future favoritable type without search SQL from being accepted or
// defaulted — it is simply not searchable — instead of failing every default
// search with an internal error.
var searchSupportedTypes = func() []string {
	types := make([]string, 0, len(favoriteResourceTypes))
	for _, resourceType := range favoriteResourceTypes {
		if searchImplementedTypeSet[resourceType] {
			types = append(types, resourceType)
		}
	}
	return types
}()

// searchSupportedTypeSet is searchSupportedTypes as a lookup set.
var searchSupportedTypeSet = func() map[string]bool {
	set := make(map[string]bool, len(searchSupportedTypes))
	for _, resourceType := range searchSupportedTypes {
		set[resourceType] = true
	}
	return set
}()

// searchRequest is a validated GET /search request.
type searchRequest struct {
	Query         string
	Types         []string
	Limit         int
	FavoritesOnly bool
}

// searchParamsInput is one entry point's raw, unvalidated search arguments:
// GET /search's query parameters and the search_resources tool arguments both
// funnel into normalizeSearchParams.
type searchParamsInput struct {
	// QueryLabel names the query field in validation errors ("q" for HTTP,
	// "query" for the tool).
	QueryLabel string
	// Query is the raw search text; the normalizer trims and bounds it.
	Query string
	// Types is the raw requested-type list; empty means every searchable
	// type. The HTTP side splits its comma list into it, the tool side passes
	// its array as-is.
	Types []string
	// Limit is the raw limit in textual form; empty means the default.
	Limit string
}

// normalizeSearchParams applies the shared search argument rules to both entry
// points. q is trimmed and must be 1–searchMaxQueryRunes runes. types is an
// optional whitelist-validated list, deduped in caller order; unknown names
// (including blank entries) are rejected with "unknown resource type", and
// empty means every searchable type. limit defaults to searchDefaultLimit,
// rejects non-numeric and < 1, and clamps anything above searchMaxLimit. A
// non-empty error message is the client-facing rejection for both surfaces.
func normalizeSearchParams(in searchParamsInput) (searchRequest, string) {
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return searchRequest{}, in.QueryLabel + " is required"
	}
	if utf8.RuneCountInString(query) > searchMaxQueryRunes {
		return searchRequest{}, fmt.Sprintf("%s must be at most %d characters", in.QueryLabel, searchMaxQueryRunes)
	}

	req := searchRequest{Query: query, Limit: searchDefaultLimit}

	if len(in.Types) == 0 {
		req.Types = append(req.Types, searchSupportedTypes...)
	} else {
		seen := make(map[string]bool, len(in.Types))
		for _, raw := range in.Types {
			resourceType := strings.TrimSpace(raw)
			if !searchSupportedTypeSet[resourceType] {
				return searchRequest{}, fmt.Sprintf("unknown resource type %q", resourceType)
			}
			if seen[resourceType] {
				continue
			}
			seen[resourceType] = true
			req.Types = append(req.Types, resourceType)
		}
	}

	if raw := strings.TrimSpace(in.Limit); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return searchRequest{}, "limit must be a positive integer"
		}
		if n > searchMaxLimit {
			n = searchMaxLimit
		}
		req.Limit = n
	}

	return req, ""
}

// parseSearchRequest validates the GET /search query parameters through the
// shared normalizer and applies the endpoint-only favorites flag. A non-empty
// error message is the 400 body.
func parseSearchRequest(r *http.Request) (searchRequest, string) {
	values := r.URL.Query()

	var types []string
	if typesParam := strings.TrimSpace(values.Get("types")); typesParam != "" {
		types = strings.Split(typesParam, ",")
	}
	req, errMsg := normalizeSearchParams(searchParamsInput{
		QueryLabel: "q",
		Query:      values.Get("q"),
		Types:      types,
		Limit:      values.Get("limit"),
	})
	if errMsg != "" {
		return searchRequest{}, errMsg
	}

	req.FavoritesOnly = values.Get("favorites") == "1"
	return req, ""
}

// searchTitleHit is one title/name/description candidate row. Only ID and
// SnapshotTitle are read back: the SQL SELECT lists carry the title and
// updated_at for their deterministic ORDER BY, but the resource summary
// metadata fetch is authoritative for every rendered field.
type searchTitleHit struct {
	ID string
	// SnapshotTitle is the dashboard-only latest published title; nil when the
	// dashboard has never been published (and always nil for other types).
	SnapshotTitle *string
}

// searchTitleHits runs one type's title candidate query. The caller supplies
// the escaped ILIKE pattern and the candidate window.
func (s *Server) searchTitleHits(ctx context.Context, orgID, resourceType, pattern string, window int) ([]searchTitleHit, error) {
	if resourceType == "dashboard" {
		rows, err := s.db.Pool.Query(ctx, searchDashboardTitleSQL, orgID, pattern, window)
		if err != nil {
			return nil, fmt.Errorf("search dashboard titles: %w", err)
		}
		defer rows.Close()
		var hits []searchTitleHit
		for rows.Next() {
			var hit searchTitleHit
			// The folder id column is selected for parity with the title
			// queries' shape; the summary metadata fetch is authoritative.
			// Title and updated_at are scanned for the SQL ORDER BY only.
			var title string
			var folderID *string
			var updatedAt time.Time
			if err := rows.Scan(&hit.ID, &title, &folderID, &updatedAt, &hit.SnapshotTitle); err != nil {
				return nil, fmt.Errorf("scan dashboard title: %w", err)
			}
			hits = append(hits, hit)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("dashboard title rows: %w", err)
		}
		return hits, nil
	}

	statement, ok := searchTitleSQL[resourceType]
	if !ok {
		return nil, fmt.Errorf("unknown resource type %q", resourceType)
	}
	rows, err := s.db.Pool.Query(ctx, statement, orgID, pattern, window)
	if err != nil {
		return nil, fmt.Errorf("search %s titles: %w", resourceType, err)
	}
	defer rows.Close()
	var hits []searchTitleHit
	for rows.Next() {
		var hit searchTitleHit
		// Title and updated_at are scanned for the SQL ORDER BY only; the
		// summary metadata fetch is authoritative.
		var title string
		var updatedAt time.Time
		if err := rows.Scan(&hit.ID, &title, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan %s title: %w", resourceType, err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s title rows: %w", resourceType, err)
	}
	return hits, nil
}

// searchCandidateCap is the per-type candidate window: max(4×limit, 50),
// capped at 200. It bounds both the SQL title fetches and the searcher's
// per-type limit, and leaves the ranked slice headroom before has_more is
// decided.
func searchCandidateCap(limit int) int {
	window := limit * 4
	if window < 50 {
		window = 50
	}
	if window > 200 {
		window = 200
	}
	return window
}

// searchLikePattern builds the ILIKE pattern for a literal substring match.
// %, _, and \ are escaped (backslash first: it is LIKE's default escape
// character), so no user input can act as a wildcard.
func searchLikePattern(query string) string {
	escaped := strings.ReplaceAll(query, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `%`, `\%`)
	escaped = strings.ReplaceAll(escaped, `_`, `\_`)
	return "%" + escaped + "%"
}

// searchTitleMatches is the Go-side twin of the SQL ILIKE candidate filter:
// case-insensitive literal substring containment. It re-checks the effective
// dashboard title, which the caller-relative rule can reject even when the SQL
// union (draft OR snapshot) matched.
func searchTitleMatches(title, query string) bool {
	return strings.Contains(strings.ToLower(title), strings.ToLower(query))
}

// searchTitleRank buckets a candidate by how its effective title matches:
// 0 exact, 1 prefix, 2 contains, 3 everything else (description-only or
// content-only matches).
func searchTitleRank(title, query string) int {
	lowerTitle := strings.ToLower(title)
	lowerQuery := strings.ToLower(query)
	switch {
	case lowerTitle == lowerQuery:
		return 0
	case strings.HasPrefix(lowerTitle, lowerQuery):
		return 1
	case strings.Contains(lowerTitle, lowerQuery):
		return 2
	}
	return 3
}

// contentRequiresEdit names the content-bearing types whose matched content
// is visible only to callers holding the listed action: dashboard widget SQL
// is editor-only, while notebook cell SQL is view-gated and therefore absent
// here. The gate is applied once, where content hits become the snippet and
// has-content sets below, so a future content-bearing type cannot silently
// skip it.
var contentRequiresEdit = map[string]string{
	"dashboard": "edit",
}

// searchTypePage is one type's ranked result page after ACL and favorites
// filtering, ready for summary assembly.
type searchTypePage struct {
	IDs     []string
	HasMore bool
	// Snippets maps page ids to content snippets (empty for types without a
	// content pass or candidates without a content hit).
	Snippets map[string]string
	// CellIDs maps page ids to the matched cell id for notebook content hits
	// (empty for types without a content pass, dashboard widget hits, and
	// candidates without a content hit).
	CellIDs map[string]string
	// Meta carries the summary metadata for every page id, with dashboard
	// titles already resolved to the caller's effective title. The builder
	// consumes it as an override: its own fetch would serve draft titles.
	Meta map[string]resourceSummaryMeta
	// Perms carries the resolved view/edit verdicts for every candidate id
	// (a superset of the page ids). searchResourceGroups hands it to the
	// summary builder so the same permissions are never resolved twice.
	Perms map[string]map[string]bool
}

// searchTypePage builds one type's ranked page. Title candidates come from
// the type's table, content candidates from the searcher (notebook cell SQL
// for viewers, dashboard widget SQL for editors only), and the union is
// ACL-filtered, favorites-filtered, ranked, and sliced. The frame is
// deliberately permission- and visibility-first: every drop happens before
// ranking and the limit, so has_more never counts a resource the caller
// cannot see. Content is gated where hits become snippets and has-content
// entries (contentRequiresEdit), so a viewer can never receive a draft-SQL
// snippet and draft SQL can never keep or rank a candidate for one.
func (s *Server) searchTypePage(
	ctx context.Context,
	ident permIdentity,
	resourceType string,
	req searchRequest,
	window int,
	contentHits []search.ContentHit,
	favorites map[string]map[string]bool,
) (searchTypePage, error) {
	titleHits, err := s.searchTitleHits(ctx, ident.OrgID, resourceType, searchLikePattern(req.Query), window)
	if err != nil {
		return searchTypePage{}, err
	}

	// Candidate ids for the metadata fetch and the permission resolution:
	// title candidates first (deterministic SQL order), then content-only
	// ids. One entry per resource. This superset is only probed; the ranked
	// list is rebuilt from the surviving hits below.
	candidates := make([]string, 0, len(titleHits)+len(contentHits))
	candidateSeen := make(map[string]bool, len(titleHits)+len(contentHits))
	for _, hit := range titleHits {
		if !candidateSeen[hit.ID] {
			candidateSeen[hit.ID] = true
			candidates = append(candidates, hit.ID)
		}
	}
	for _, hit := range contentHits {
		if !candidateSeen[hit.ResourceID] {
			candidateSeen[hit.ResourceID] = true
			candidates = append(candidates, hit.ResourceID)
		}
	}

	// One metadata fetch and one batched permission resolution per type, over
	// every candidate id. view+edit for every type: edit feeds both the
	// content gate (dashboards) and the summaries' can_edit, and the batch
	// costs the same either way. The verdicts ride along in the page so the
	// summary builder does not resolve the same ids again.
	meta, err := s.fetchResourceSummaryMeta(ctx, ident.OrgID, resourceType, candidates)
	if err != nil {
		return searchTypePage{}, err
	}
	perms, err := s.resolveActions(ctx, ident, resourceType, candidates, []string{"view", "edit"})
	if err != nil {
		return searchTypePage{}, err
	}

	// The merged candidate list: title candidates first, then the surviving
	// content-only ids; the snippet and has-content maps cover gated content
	// hits only. This is the single place content hits turn into snippets and
	// keep-set entries: a type in contentRequiresEdit whose caller lacks the
	// required action contributes no snippet, no content-only candidate, and
	// no has-content keep reason downstream (notebooks are view-gated, so
	// their hits pass through).
	ids := make([]string, 0, len(titleHits)+len(contentHits))
	seen := make(map[string]bool, len(titleHits)+len(contentHits))
	for _, hit := range titleHits {
		if !seen[hit.ID] {
			seen[hit.ID] = true
			ids = append(ids, hit.ID)
		}
	}
	contentSnippets := make(map[string]string, len(contentHits))
	contentCellIDs := make(map[string]string, len(contentHits))
	contentIDs := make(map[string]bool, len(contentHits))
	contentAction := contentRequiresEdit[resourceType]
	for _, hit := range contentHits {
		if contentAction != "" && !actionAllowed(perms, hit.ResourceID, contentAction) {
			continue
		}
		contentIDs[hit.ResourceID] = true
		if hit.Snippet != "" {
			contentSnippets[hit.ResourceID] = hit.Snippet
		}
		if hit.CellID != "" {
			contentCellIDs[hit.ResourceID] = hit.CellID
		}
		if !seen[hit.ResourceID] {
			seen[hit.ResourceID] = true
			ids = append(ids, hit.ResourceID)
		}
	}

	titleHitByID := make(map[string]searchTitleHit, len(titleHits))
	for _, hit := range titleHits {
		titleHitByID[hit.ID] = hit
	}

	type rankedCandidate struct {
		id      string
		title   string
		updated time.Time
		rank    int
	}
	ranked := make([]rankedCandidate, 0, len(ids))
	pageMeta := make(map[string]resourceSummaryMeta, len(ids))
	for _, id := range ids {
		m, inOrg := meta[id]
		if !inOrg || !actionAllowed(perms, id, "view") {
			continue // unknown, trashed, or invisible
		}
		hasContent := contentIDs[id]
		_, isTitleHit := titleHitByID[id]
		canEdit := actionAllowed(perms, id, "edit")

		// Effective title: viewers see (and are matched against) the latest
		// published snapshot when one exists; editors and never-published
		// dashboards see the draft. The no-draft-leak rule lives here: a
		// viewer's draft-only title match is dropped, because the field the
		// caller matched was never the draft.
		effectiveTitle := m.Title
		if hit, ok := titleHitByID[id]; ok && resourceType == "dashboard" && hit.SnapshotTitle != nil && !canEdit {
			effectiveTitle = *hit.SnapshotTitle
		}

		// Keep rules. Non-dashboards: the SQL candidate matched title or
		// description; content-only candidates survive only with a gated
		// content hit. Dashboards: the effective title must match, or a
		// gated content hit must have survived (content is editor-only
		// there, so hasContent already implies the caller holds edit).
		var keep bool
		if isTitleHit {
			keep = resourceType != "dashboard" || searchTitleMatches(effectiveTitle, req.Query) || hasContent
		} else {
			keep = hasContent
		}
		if !keep {
			continue
		}

		// Rank on the effective title. A description-only match ranks 3 here
		// (the title does not contain the query), which is the intent: only
		// real title matches outrank content matches.
		m.Title = effectiveTitle
		pageMeta[id] = m
		ranked = append(ranked, rankedCandidate{
			id:      id,
			title:   effectiveTitle,
			updated: m.UpdatedAt,
			rank:    searchTitleRank(effectiveTitle, req.Query),
		})
	}

	// favorites=1 intersects before ranking and the limit: a hidden
	// non-favorite must not consume a page slot or distort has_more.
	if req.FavoritesOnly {
		favoriteSet := favorites[resourceType]
		kept := ranked[:0]
		for _, candidate := range ranked {
			if favoriteSet[candidate.id] {
				kept = append(kept, candidate)
			}
		}
		ranked = kept
	}

	// Rank order, then recency, then the resource id as the final
	// deterministic tiebreak (equal timestamps cannot shuffle across calls).
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if !a.updated.Equal(b.updated) {
			return a.updated.After(b.updated)
		}
		return a.id < b.id
	})

	page := searchTypePage{
		HasMore:  len(ranked) > req.Limit,
		Snippets: make(map[string]string),
		CellIDs:  make(map[string]string),
		Meta:     make(map[string]resourceSummaryMeta),
		Perms:    perms,
	}
	if page.HasMore {
		ranked = ranked[:req.Limit]
	}
	for _, candidate := range ranked {
		page.IDs = append(page.IDs, candidate.id)
		if snippet := contentSnippets[candidate.id]; snippet != "" {
			page.Snippets[candidate.id] = snippet
		}
		if cellID := contentCellIDs[candidate.id]; cellID != "" {
			page.CellIDs[candidate.id] = cellID
		}
		page.Meta[candidate.id] = pageMeta[candidate.id]
	}
	return page, nil
}

// searchGroup is one resource type's search results plus the truncation flag.
type searchGroup struct {
	ResourceType string            `json:"resource_type"`
	HasMore      bool              `json:"has_more"`
	Results      []resourceSummary `json:"results"`
}

// searchResponse is the grouped GET /search payload: only non-empty groups, in
// the fixed searchGroupOrder.
type searchResponse struct {
	Query  string        `json:"query"`
	Groups []searchGroup `json:"groups"`
}

// searchFailure is one search-core stage failure: message is the exact
// client-facing body the endpoint has always returned for that stage, while
// the wrapped cause stays available to logs and errors.Is/As — it is never
// part of what HTTP or the agent tool sees.
type searchFailure struct {
	message string
	err     error
}

func (e *searchFailure) Error() string { return e.message }
func (e *searchFailure) Unwrap() error { return e.err }

// searchFailureMessage extracts the client-facing message of a core failure,
// falling back to the generic search body for unexpected errors.
func searchFailureMessage(err error) string {
	var failure *searchFailure
	if errors.As(err, &failure) {
		return failure.message
	}
	return "search failed"
}

// searchResourceGroups is the search core: the content pass, the per-type
// ranked pages (ACL- and leak-filtered), and summary assembly. GET
// /api/v1/search and the search_resources agent/MCP tool both run it, so
// agents search exactly what their caller can see under the same dashboard
// rules.
func (s *Server) searchResourceGroups(ctx context.Context, ident permIdentity, req searchRequest) (searchResponse, error) {
	if s.searcher == nil {
		return searchResponse{}, &searchFailure{message: "search unavailable"}
	}

	// Unbounded on purpose: favorites=1 intersects candidates with these ids
	// before ranking, and the summary builder needs the full set for
	// is_favorite. A capped scan could silently hide a favorite match.
	favorites, err := s.loadFavoriteIDs(ctx, ident.UserID, ident.OrgID)
	if err != nil {
		return searchResponse{}, &searchFailure{message: "failed to load favorites", err: err}
	}

	requested := make(map[string]bool, len(req.Types))
	for _, resourceType := range req.Types {
		requested[resourceType] = true
	}

	// Content pass: notebooks (cell SQL, view-gated) and dashboards (widget
	// SQL, edit-gated), only for the types the caller asked for. The searcher
	// is neither org-external nor ACL-aware; both filters apply below.
	contentTypes := make([]string, 0, 2)
	for _, resourceType := range []string{search.TypeNotebook, search.TypeDashboard} {
		if requested[resourceType] {
			contentTypes = append(contentTypes, resourceType)
		}
	}
	window := searchCandidateCap(req.Limit)
	var contentByType map[string][]search.ContentHit
	if len(contentTypes) > 0 {
		contentByType, err = s.searcher.ContentMatches(ctx, ident.OrgID, contentTypes, req.Query, window)
		if err != nil {
			return searchResponse{}, &searchFailure{message: "search failed", err: err}
		}
	}

	pages := make(map[string]searchTypePage, len(searchGroupOrder))
	for _, resourceType := range searchGroupOrder {
		if !requested[resourceType] {
			continue
		}
		page, err := s.searchTypePage(ctx, ident, resourceType, req, window, contentByType[resourceType], favorites)
		if err != nil {
			return searchResponse{}, &searchFailure{message: "search failed", err: err}
		}
		pages[resourceType] = page
	}

	// Summaries for the ranked page ids, in the fixed group order. The
	// metadata and view/edit verdicts already resolved for ranking ride along
	// as overrides — every page id of every type is covered — so the builder
	// never re-fetches and never resolves the same permissions again, and
	// dashboard rows keep their caller-relative effective titles.
	requests := make([]summaryRequest, 0, len(searchGroupOrder))
	opts := summaryBuildOptions{
		Snippets:  map[string]map[string]string{},
		CellIDs:   map[string]map[string]string{},
		Overrides: summaryOverrides{},
		Favorites: favorites,
		Perms:     map[string]map[string]map[string]bool{},
	}
	for _, resourceType := range searchGroupOrder {
		page, ok := pages[resourceType]
		if !ok || len(page.IDs) == 0 {
			continue
		}
		requests = append(requests, summaryRequest{ResourceType: resourceType, IDs: page.IDs})
		if len(page.Snippets) > 0 {
			opts.Snippets[resourceType] = page.Snippets
		}
		if len(page.CellIDs) > 0 {
			opts.CellIDs[resourceType] = page.CellIDs
		}
		opts.Overrides[resourceType] = page.Meta
		opts.Perms[resourceType] = page.Perms
	}
	groups, err := s.buildResourceSummaries(ctx, ident, requests, opts)
	if err != nil {
		return searchResponse{}, &searchFailure{message: "failed to build results", err: err}
	}

	response := searchResponse{Query: req.Query, Groups: []searchGroup{}}
	for _, group := range groups {
		response.Groups = append(response.Groups, searchGroup{
			ResourceType: group.ResourceType,
			HasMore:      pages[group.ResourceType].HasMore,
			Results:      group.Results,
		})
	}
	return response, nil
}

// searchResourcesForTool is the agent/MCP entry point for catalog search,
// wired to ToolContext.SearchResourcesFunc. It validates the tool arguments
// through the same normalizer as GET /api/v1/search — the tool caller's limit
// arrives in its textual form so the HTTP limit rules (reject < 1, clamp
// > 20) apply verbatim — builds the caller identity directly from the tool
// context (group memberships plus the admin-mode flag riding the context),
// and runs the same core, so agent searches are ACL- and leak-safe exactly
// like HTTP searches.
func (s *Server) searchResourcesForTool(ctx context.Context, userID, orgID, orgRole, query string, types []string, limit int) (any, error) {
	req, errMsg := normalizeSearchParams(searchParamsInput{
		QueryLabel: "query",
		Query:      query,
		Types:      types,
		Limit:      strconv.Itoa(limit),
	})
	if errMsg != "" {
		return nil, errors.New(errMsg)
	}

	groupIDs, err := s.callerGroupIDs(ctx, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("permission check failed: %w", err)
	}
	ident := permIdentity{
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   orgRole,
		GroupIDs:  groupIDs,
		AdminMode: adminModeFromContext(ctx),
	}
	response, err := s.searchResourceGroups(ctx, ident, req)
	if err != nil {
		return nil, err
	}
	return response, nil
}

// @Summary Search the catalog
// @Description Searches notebooks, dashboards, folders, connectors, agents, models, skills, tools, and MCP servers by title/name/description, plus content (notebook cell SQL for viewers, dashboard widget SQL for editors only). Results are ACL-filtered, grouped in a fixed type order, capped per type (default 5, max 20) with has_more, and ranked exact title > prefix > contains > content-only, then most recent. has_more is window-bounded: the per-type candidate window is finite, so a very broad query can under-report and clients should refine the query rather than page exhaustively. Non-editors match dashboards on their effective title only: the latest published snapshot when one exists, never an unpublished draft title.
// @Tags search
// @Produce json
// @Param q query string true "Search query (1-200 characters)"
// @Param types query string false "Comma-separated resource types (default: all nine)"
// @Param limit query int false "Results per type (default 5, max 20)"
// @Param favorites query string false "Set to 1 to restrict results to the caller's favorites"
// @Success 200 {object} searchResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /search [get]
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims.OrgID == "" {
		writeError(w, http.StatusForbidden, "organization required — complete onboarding first")
		return
	}
	req, errMsg := parseSearchRequest(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if s.searcher == nil {
		writeError(w, http.StatusInternalServerError, "search unavailable")
		return
	}

	ctx := r.Context()
	ident, err := s.permIdentity(ctx, claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	response, err := s.searchResourceGroups(ctx, ident, req)
	if err != nil {
		// The client body stays a stable per-stage message; the cause lives
		// here, in the log.
		slog.Error("search failed",
			"error", err,
			"user_id", claims.UserID,
			"org_id", claims.OrgID,
			"query", req.Query)
		writeError(w, http.StatusInternalServerError, searchFailureMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, response)
}
