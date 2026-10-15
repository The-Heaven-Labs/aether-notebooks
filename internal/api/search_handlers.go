package api

import (
	"context"
	"fmt"
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

// searchRequest is a validated GET /search request.
type searchRequest struct {
	Query         string
	Types         []string
	Limit         int
	FavoritesOnly bool
}

// parseSearchRequest validates the query parameters. q is trimmed and must be
// 1–200 runes. types is an optional comma list defaulting to every searchable
// type; an unknown name is rejected. limit defaults to 5, rejects non-numeric
// and < 1, and clamps anything above 20. favorites=1 restricts results to the
// caller's favorites. A non-empty error message is the 400 body.
func parseSearchRequest(r *http.Request) (searchRequest, string) {
	values := r.URL.Query()

	query := strings.TrimSpace(values.Get("q"))
	if query == "" {
		return searchRequest{}, "q is required"
	}
	if utf8.RuneCountInString(query) > searchMaxQueryRunes {
		return searchRequest{}, fmt.Sprintf("q must be at most %d characters", searchMaxQueryRunes)
	}

	req := searchRequest{Query: query, Limit: searchDefaultLimit}

	if typesParam := strings.TrimSpace(values.Get("types")); typesParam != "" {
		seen := make(map[string]bool)
		for _, part := range strings.Split(typesParam, ",") {
			resourceType := strings.TrimSpace(part)
			if !favoriteResourceTypeSet[resourceType] {
				return searchRequest{}, fmt.Sprintf("unknown resource type %q", resourceType)
			}
			if seen[resourceType] {
				continue
			}
			seen[resourceType] = true
			req.Types = append(req.Types, resourceType)
		}
	} else {
		req.Types = append(req.Types, favoriteResourceTypes...)
	}

	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return searchRequest{}, "limit must be a positive integer"
		}
		if n > searchMaxLimit {
			n = searchMaxLimit
		}
		req.Limit = n
	}

	req.FavoritesOnly = values.Get("favorites") == "1"
	return req, ""
}

// searchTitleHit is one title/name/description candidate row.
type searchTitleHit struct {
	ID        string
	Title     string
	UpdatedAt time.Time
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
			var folderID *string
			if err := rows.Scan(&hit.ID, &hit.Title, &folderID, &hit.UpdatedAt, &hit.SnapshotTitle); err != nil {
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
		if err := rows.Scan(&hit.ID, &hit.Title, &hit.UpdatedAt); err != nil {
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

// searchTypePage is one type's ranked result page after ACL and favorites
// filtering, ready for summary assembly.
type searchTypePage struct {
	IDs     []string
	HasMore bool
	// Snippets maps page ids to content snippets (empty for types without a
	// content pass or candidates without a content hit).
	Snippets map[string]string
	// Meta carries the summary metadata for every page id, with dashboard
	// titles already resolved to the caller's effective title. The builder
	// consumes it as an override: its own fetch would serve draft titles.
	Meta map[string]resourceSummaryMeta
}

// searchTypePage builds one type's ranked page. Title candidates come from
// the type's table, content candidates from the searcher (notebook cell SQL
// for viewers, dashboard widget SQL for editors only), and the union is
// ACL-filtered, favorites-filtered, ranked, and sliced. The frame is
// deliberately permission- and visibility-first: every drop happens before
// ranking and the limit, so has_more never counts a resource the caller
// cannot see.
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

	// Candidate ids: title candidates first (deterministic SQL order), then
	// content-only ids. One entry per resource.
	ids := make([]string, 0, len(titleHits)+len(contentHits))
	seen := make(map[string]bool, len(titleHits)+len(contentHits))
	for _, hit := range titleHits {
		if !seen[hit.ID] {
			seen[hit.ID] = true
			ids = append(ids, hit.ID)
		}
	}
	contentSnippets := make(map[string]string, len(contentHits))
	contentIDs := make(map[string]bool, len(contentHits))
	for _, hit := range contentHits {
		contentIDs[hit.ResourceID] = true
		if hit.Snippet != "" {
			contentSnippets[hit.ResourceID] = hit.Snippet
		}
		if !seen[hit.ResourceID] {
			seen[hit.ResourceID] = true
			ids = append(ids, hit.ResourceID)
		}
	}

	// One metadata fetch and one batched permission resolution per type, over
	// every candidate id.
	meta, err := s.fetchResourceSummaryMeta(ctx, ident.OrgID, resourceType, ids)
	if err != nil {
		return searchTypePage{}, err
	}
	actions := []string{"view"}
	if resourceType == "dashboard" {
		actions = append(actions, "edit")
	}
	perms, err := s.resolveActions(ctx, ident, resourceType, ids, actions)
	if err != nil {
		return searchTypePage{}, err
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
		// description, and content-only candidates carry a snippet.
		// Dashboards: the effective title must match, or the caller must hold
		// edit (content is editor-only) with a widget-SQL hit.
		var keep bool
		if isTitleHit {
			if resourceType == "dashboard" {
				keep = searchTitleMatches(effectiveTitle, req.Query) || (hasContent && canEdit)
			} else {
				keep = true
			}
		} else {
			keep = hasContent && (resourceType != "dashboard" || canEdit)
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
		Meta:     make(map[string]resourceSummaryMeta),
	}
	if page.HasMore {
		ranked = ranked[:req.Limit]
	}
	for _, candidate := range ranked {
		page.IDs = append(page.IDs, candidate.id)
		if snippet := contentSnippets[candidate.id]; snippet != "" {
			page.Snippets[candidate.id] = snippet
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

// @Summary Search the catalog
// @Description Searches notebooks, dashboards, folders, connectors, agents, models, skills, tools, and MCP servers by title/name/description, plus content (notebook cell SQL for viewers, dashboard widget SQL for editors only). Results are ACL-filtered, grouped in a fixed type order, capped per type (default 5, max 20) with has_more, and ranked exact title > prefix > contains > content-only, then most recent. Non-editors match dashboards on their effective title only: the latest published snapshot when one exists, never an unpublished draft title.
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
	// Unbounded on purpose: favorites=1 intersects candidates with these ids
	// before ranking, and the summary builder needs the full set for
	// is_favorite. A capped scan could silently hide a favorite match.
	favorites, err := s.loadFavoriteIDs(ctx, claims.UserID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load favorites")
		return
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
		contentByType, err = s.searcher.ContentMatches(ctx, claims.OrgID, contentTypes, req.Query, window)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
	}

	pages := make(map[string]searchTypePage, len(searchGroupOrder))
	for _, resourceType := range searchGroupOrder {
		if !requested[resourceType] {
			continue
		}
		page, err := s.searchTypePage(ctx, ident, resourceType, req, window, contentByType[resourceType], favorites)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "search failed")
			return
		}
		pages[resourceType] = page
	}

	// Summaries for the ranked page ids, in the fixed group order. The
	// metadata already fetched for ranking rides along as overrides — every
	// page id of every type is covered — so the builder never re-fetches and
	// dashboard rows keep their caller-relative effective titles.
	requests := make([]summaryRequest, 0, len(searchGroupOrder))
	opts := summaryBuildOptions{
		Snippets:  map[string]map[string]string{},
		Overrides: summaryOverrides{},
		Favorites: favorites,
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
		opts.Overrides[resourceType] = page.Meta
	}
	groups, err := s.buildResourceSummaries(ctx, ident, requests, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build results")
		return
	}

	response := searchResponse{Query: req.Query, Groups: []searchGroup{}}
	for _, group := range groups {
		response.Groups = append(response.Groups, searchGroup{
			ResourceType: group.ResourceType,
			HasMore:      pages[group.ResourceType].HasMore,
			Results:      group.Results,
		})
	}
	writeJSON(w, http.StatusOK, response)
}
