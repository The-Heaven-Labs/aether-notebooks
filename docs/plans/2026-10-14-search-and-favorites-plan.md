# Search, Favorites, and Fast Folder Listings — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make folder browsing fast and paginated, add whole-catalog search (titles + notebook/dashboard content) with a Ctrl/Cmd+K palette and `/search` page, add per-user favorites, expose search to agents/MCP, and keep non-executable provisioner connectors out of execution pickers.

**Architecture:** A batched, set-based permission resolver replaces per-item `checkPermission` calls in every list endpoint (the N+1 root cause); folder sections become server-paginated with exact visible totals; search uses stock-Postgres `ILIKE` for titles plus core FTS (`to_tsvector('simple')` GIN) for content behind a swappable `internal/search.Searcher`; favorites are a small server-side table; the React app gains a top-bar search field, command palette, `/search` page, and bookmark toggles.

**Tech Stack:** Go (net/http ServeMux, pgx, real-Postgres tests), Postgres core FTS (no extensions), React + TypeScript + React Query + Vitest/MSW, Playwright e2e.

**Design doc (the spec):** `docs/plans/2026-10-14-search-and-favorites-design.md` — read it before starting; this plan is the task ledger.

**Worktree:** `/home/jesus/Projects/aether-notebooks/.worktrees/search-favorites` (branch `feat/search-favorites`). Baseline is green (`task test` passed on `origin/main` @ `23fc9ffe`).

---

## How to run things

```bash
# Focused Go test (from the worktree root). Includes -timeout 3m per AGENTS.md.
bash scripts/go-test.sh ./internal/api/ -run 'TestName' -count=1 -timeout 3m

# Re-runs without dropping/recreating the test DB (faster; DB must already exist):
AETHER_TEST_DB_RESET=0 bash scripts/go-test.sh ./internal/api/ -run 'TestName' -count=1 -timeout 3m

# Full Go suite (~3 min budget):        task test
# Format/vet/tidy:                      task fmt && task vet
# Web unit tests:                       cd web && npm run test:run -- src/components/SearchPalette.test.tsx
# TypeScript:                           cd web && npx tsc --noEmit
# Web build (stricter tsc -b):          cd web && npm run build
# E2E (dev stack must be up on :5173):  npx playwright test --config=e2e/playwright.config.ts e2e/search.spec.ts
```

**E2E stack note:** start the full dev stack **from this worktree**
(`docker compose -f docker-compose.dev.yml up -d`) so containers bind-mount this worktree's
code. The compose project name is shared (`aether-dev`), so never do this while a dev stack
from another checkout is running — stop it first (`docker compose -f docker-compose.dev.yml down`
in that checkout).

**Commit rhythm:** every task ends with a commit. Conventional types matching the repo
(`feat(api):`, `perf(api):`, `feat(web):`, `test:`, `docs:`).

**Rules that apply to every task:** tests hit a real DB (no mocks); never add a per-item
`checkPermission` loop to a listing (use `resolveActions`); UI changes are validated in the real
browser with `agent-browser` before the phase is called done; never use `any` in TypeScript.

---

# Phase A — Batched permission resolution (the keystone)

### Task A1: Extract `evaluateCandidates` from `checkPermissionQ`

Behavior-preserving refactor: pull steps 5–9 of `checkPermissionQ` into a pure function so the
batch path can reuse the exact semantics.

**Files:**
- Modify: `internal/api/permissions.go` (functions `checkPermissionQ` at :182, helpers after it)
- Create: `internal/api/permissions_evaluate_test.go`

**Step 1: Write the failing unit tests** (`permissions_evaluate_test.go`, package `api`)

Cover the evaluation rules directly with hand-built `aclCandidate` slices:
specificity ordering (resource −1 beats folder 0 beats folder 1), subject rank within a depth
(user > group > org_role), Everyone grant surviving a restrictive more-specific match, a
restrictive more-specific match denying when no Everyone grant exists, `view` implied by
`use/edit/share/delete/admin`, folder-only implications (`edit` → `run`/`use`/`view_with_data`
for descendants; `manage` → `share`; folders excluded from expansion), and deny-by-default on
an empty candidate list.

```go
func TestEvaluateCandidates(t *testing.T) {
	c := func(subjectType, subjectID string, actions []string, specificity int) aclCandidate {
		return aclCandidate{subjectType: subjectType, subjectID: subjectID, actions: actions,
			specificity: specificity, subjectRank: subjectRank(subjectType)}
	}
	// Everyone grant wins over a restrictive specific entry
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"view"}, -1),
		c("org_role", "everyone", []string{"view"}, 1),
	}, "u1", "editor", []string{"g1"}, "notebook", "view"))
	// view is implied by edit
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, -1),
	}, "u1", "editor", nil, "notebook", "view"))
	// folder edit implies notebook run; a direct notebook edit never implies run
	require.True(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, 0),
	}, "u1", "editor", nil, "notebook", "run"))
	require.False(t, evaluateCandidates([]aclCandidate{
		c("user", "u1", []string{"edit"}, -1),
	}, "u1", "editor", nil, "notebook", "run"))
	// ... deny-by-default, group match, Everyone+restrictive interplay
}
```

**Step 2: Run it and verify it fails**

```bash
bash scripts/go-test.sh ./internal/api/ -run 'TestEvaluateCandidates' -count=1 -timeout 3m
```
Expected: compile error (`undefined: evaluateCandidates`).

**Step 3: Extract the evaluator**

In `permissions.go`, move steps 5–9 verbatim into:

```go
// evaluateCandidates is the single permission-evaluation core: most specific
// first, user > group > org_role within a depth, the Everyone exception, view
// implication, and ancestor-folder action expansion. checkPermissionQ and the
// batched resolver must both evaluate through this function so batch results
// equal single-check results by construction.
func evaluateCandidates(candidates []aclCandidate, userID, orgRole string, groupIDs []string, resourceType, action string) bool {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity < candidates[j].specificity
		}
		return candidates[i].subjectRank < candidates[j].subjectRank
	})
	var everyoneGrants, restrictiveMatch bool
	for _, c := range candidates {
		if !matchesUser(c, userID, orgRole, groupIDs) {
			continue
		}
		if candidateGrants(c, resourceType, action) {
			if c.subjectType == "org_role" && c.subjectID == "everyone" {
				everyoneGrants = true
			} else {
				return true
			}
		} else if !(c.subjectType == "org_role" && c.subjectID == "everyone") {
			restrictiveMatch = true
		}
	}
	if everyoneGrants {
		return true
	}
	_ = restrictiveMatch
	return false // deny-by-default; restrictiveMatch only documents the deny
}
```

`checkPermissionQ` ends with `return evaluateCandidates(candidates, userID, orgRole, groupIDs, resourceType, action), nil`.

**Step 4: Run tests** — the new test passes **and the existing permission suites stay green**:

```bash
bash scripts/go-test.sh ./internal/api/ -run 'TestEvaluateCandidates|TestFolderPermission|TestSessionPermission' -count=1 -timeout 3m
```

**Step 5: Commit**

```bash
git add internal/api/permissions.go internal/api/permissions_evaluate_test.go
git commit -m "refactor(api): extract evaluateCandidates as the shared permission core"
```

---

### Task A2: `permIdentity` + `resolveActions` batch resolver

**Files:**
- Modify: `internal/api/permissions.go`
- Create: `internal/api/permissions_batch_test.go`

**Step 1: Write the failing parity test**

White-box (package `api`) parity matrix. Reuse the insert helpers in
`permissions_folder_inheritance_test.go` (`insertPermFolder`, `insertPermNotebook`,
`insertPermConnector`, `insertPermDashboard`, `newSessionPermissionTestServer`) to build:

- a folder tree `root → parent → child`;
- resources: notebook/connector/dashboard in `child`, one in `parent`, one at root;
- ACL entries at every depth with subjects: direct user, group (user belongs), Everyone
  (`org_role/everyone`), plus a **restrictive direct-user entry without the action** above an
  Everyone grant;
- a second user with no access; an org admin with admin mode on/off; a cross-org resource id.

Then for each resource × action in `{view, edit, delete, share, run, use}` compare:

```go
ident, err := srv.permIdentity(ctx, &Claims{UserID: userID, OrgID: orgID, Role: "editor"})
require.NoError(t, err)
got, err := srv.resolveActions(ctx, ident, "notebook", ids, actions)
require.NoError(t, err)
for _, id := range ids {
	for _, action := range actions {
		want, err := srv.checkPermission(ctx, userID, orgID, "editor", "notebook", id, action)
		require.NoError(t, err)
		require.Equal(t, want, got[id][action], "resource %s action %s", id, action)
	}
}
```

Also assert: empty id list → empty map, no error; unknown ids → present-or-absent with all-false;
admin mode on (claims role admin + `adminMode` set in identity) → all actions true for org
resources, false for cross-org; same queries for `folder`, `connector`, `dashboard`.

**Step 2: Run it — fails** (`undefined: permIdentity/resolveActions`).

**Step 3: Implement**

```go
// permIdentity is the caller-side of a permission evaluation, resolved once
// per request instead of once per check.
type permIdentity struct {
	UserID    string
	OrgID     string
	OrgRole   string
	GroupIDs  []string
	AdminMode bool
}

func (s *Server) permIdentity(ctx context.Context, claims *Claims) (permIdentity, error) {
	groups, err := s.callerGroupIDs(ctx, claims.UserID, claims.OrgID)
	if err != nil {
		return permIdentity{}, err
	}
	return permIdentity{
		UserID: claims.UserID, OrgID: claims.OrgID, OrgRole: claims.Role,
		GroupIDs: groups, AdminMode: adminModeFromContext(ctx),
	}, nil
}

// resolveActions resolves actions for many resources of one type in a constant
// number of queries: resource→folder map, direct ACLs, one recursive ancestry
// walk for all distinct folders. Evaluation goes through evaluateCandidates,
// so results match checkPermission exactly.
func (s *Server) resolveActions(ctx context.Context, ident permIdentity, resourceType string, ids []string, actions []string) (map[string]map[string]bool, error)
```

Implementation notes (exact SQL):

1. `resourceType == "folder"`: `SELECT id, parent_id FROM folders WHERE id = ANY($1::uuid[]) AND org_id = $2`;
   else `<table> = resourceTable[resourceType]`: `SELECT id, folder_id FROM <table> WHERE id = ANY($1::uuid[]) AND org_id = $2`.
   Build `folderOf map[string]*string`; ids absent from the result are not in the org → all false.
2. Direct ACLs:
   ```sql
   SELECT resource_id::text, subject_type, subject_id, actions
   FROM acl_entries
   WHERE resource_type = $1 AND resource_id = ANY($2::uuid[]) AND org_id = $3
   ```
   → `direct map[string][]aclCandidate` with `specificity: -1`, `subjectRank`.
3. Distinct non-null folder ids → one ancestry query (seed = the folder itself at depth 0,
   exactly like `checkPermissionQ` steps 3–4):
   ```sql
   WITH RECURSIVE ancestors(seed_id, id, parent_id, depth) AS (
       SELECT id, id, parent_id, 0 FROM folders WHERE id = ANY($1::uuid[])
       UNION ALL
       SELECT a.seed_id, f.id, f.parent_id, a.depth + 1
       FROM folders f JOIN ancestors a ON f.id = a.parent_id
   )
   SELECT a.seed_id, a.depth, ae.subject_type, ae.subject_id, ae.actions
   FROM ancestors a
   JOIN acl_entries ae ON ae.resource_type = 'folder' AND ae.resource_id = a.id AND ae.org_id = $2
   ```
   → `ancestors map[string][]aclCandidate` (`specificity: depth`). Note the seed uses the
   resource's parent folder for non-folder types and the folder's own `parent_id` for folders,
   matching the single path.
4. Admin mode: if `ident.AdminMode && ident.OrgRole == "admin"`, every id present in step 1
   gets `true` for every requested action (short-circuit; no ACL queries needed beyond step 1 —
   but keep the implementation running step 1 regardless).
5. Per id: `candidates := append(direct[id], ancestors[folderOf[id]]...)`, then for each action
   `evaluateCandidates(candidates, ident.UserID, ident.OrgRole, ident.GroupIDs, resourceType, action)`.

**Step 4: Run** the parity test green; also rerun the folder-inheritance suite.

**Step 5: Commit** — `feat(api): add batched resolveActions permission resolver`

---

### Task A3: Batched descendant-content filter

**Files:**
- Modify: `internal/api/folder_handlers.go` (`folderHasDescendantContent` at :103)
- Test: `internal/api/permissions_batch_test.go`

**Step 1: Failing test** — build a folder with 3 hidden children where only child #2 contains a
notebook the user can view (grant on the notebook, not the folder). Assert
`filterFoldersWithVisibleDescendantContent(ctx, ident, []string{child1, child2, child3})`
returns `{child2: true}` and matches calling `folderHasDescendantContent` per child.

**Step 2: Implement** a set-based sibling: one recursive CTE seeded with the child folder ids
(`seed_id`), walking descendants; for each descendant run the same three EXISTS predicates as
`folderHasDescendantContent` (notebooks/connectors/dashboards joined to `acl_entries` for
user/group/everyone + folder-ACL inheritance on the descendant chain), then
`SELECT DISTINCT seed_id WHERE <any predicate>`. The single-folder function becomes a thin
wrapper over the set version (DRY), keeping its signature for existing callers.

**Step 3: Run + commit** — `perf(api): batch the descendant-content visibility probe`

---

### Task A4: Migrate folder handlers + query-count guard

**Files:**
- Modify: `internal/api/folder_handlers.go` (`handleGetFolderContents` :747, `handleListRootContents` :494, `handleListHomeFolders` :232)
- Modify: `internal/api/testhelpers_test.go` (tracer helper)
- Create: `internal/api/folder_handlers_batch_test.go`

**Step 1: Write the guard test first (red).** Add a counting tracer:

```go
type queryCounter struct{ n atomic.Int64 }
func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.n.Add(1); return ctx
}
func (q *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
```

Helper `setupTestServerWithQueryCounter(t) (*Server, *queryCounter)`: like `setupTestServer` but
builds the pool with `pgxpool.ParseConfig` + `cfg.ConnConfig.Tracer = counter` and assigns
`srv.db.Pool = pool` (white-box, package `api`).

Test: create folder + 12 notebooks (granted to the user) → `GET /folders/{id}` → count A.
Create folder + 120 notebooks → `GET /folders/{id}` → count B. Assert `B <= A + 8` (constant
overhead, not linear) and `A < 40`. Run → fails badly (hundreds of queries).

**Step 2: Migrate the three handlers.** Replace every per-item `checkPermission` loop with one
`resolveActions` call per resource type per request (actions `[view, edit, delete, share]`),
keeping the response structs (`folderNotebook`, `folderConnector`, `folderDashboard`,
`folderItemFolder`) and using `filterFoldersWithVisibleDescendantContent` for children that fail
`view` (replacing the per-child `folderHasDescendantContent`). Keep `folderHasDescendantContent`
for the requested-folder fallback (single id).

**Step 3: Run the guard + existing folder suites**:
```bash
bash scripts/go-test.sh ./internal/api/ -run 'TestFolder|TestListRoot|TestHomeFolders|TestFolderContentsQueryCount' -count=1 -timeout 3m
```

**Step 4: Commit** — `perf(api): resolve folder listing permissions in batch`

---

### Task A5: Migrate the remaining list endpoints

**Files:**
- Modify: `internal/api/notebook_handlers.go` (`handleListNotebooks` :301, `handleListTrash` :578)
- Modify: `internal/api/dashboard_handlers.go` (`handleListDashboards` :287)
- Modify: `internal/api/connector_handlers.go` (`handleListConnectors` :199)
- Modify: `internal/api/recent_handlers.go` (`handleGetRecent` :23)
- Modify: `internal/api/dashboard_query_handlers.go` (`loadPublishedDashboardSnapshot` :60 — add a batch sibling)
- Modify: `internal/api/notebook_handlers.go` (trash in-memory sort stays — only permissions change)

**Steps**

1. Add `loadLatestDashboardSnapshots(ctx, orgID string, ids []string) (map[string]dashboardSnapshot, error)`:
   ```sql
   SELECT DISTINCT ON (dashboard_id) dashboard_id::text, title, settings
   FROM dashboard_versions WHERE dashboard_id = ANY($1::uuid[]) AND org_id = $2
   ORDER BY dashboard_id, version DESC
   ```
   (match the existing snapshot struct fields; `errNoPublishedVersion` semantics = id missing from map).
2. Notebooks list: one `resolveActions("notebook", allIDs, ["view"])`; keep the scan loop but drop the per-row check.
3. Dashboards list: `resolveActions("dashboard", allIDs, ["view","edit"])`; for non-editor rows, substitute title/settings from the batch snapshot map (missing = never published → keep draft), preserving the exact behavior at dashboard_handlers.go:316-330.
4. Connectors list: `resolveActions("connector", allIDs, ["view","use"])`, keep the provisioner `canUse` override at :242-244 and the per-row config masking.
5. Recent + trash: batch `view` checks; keep the existing caps (`recent` limit 20 candidates → first 10 visible; trash sort unchanged).

**Tests:** existing suites must pass unchanged (`TestListNotebooks`, dashboard list tests, recent, trash). Add one new dashboard test: viewer (no edit) with a published version sees the snapshot title in `GET /dashboards`; never-published keeps the draft title.

**Commit** — `perf(api): batch permission resolution across list endpoints`

---

### Task A6: V135 — list + search indexes

**Files:**
- Create: `internal/database/migrations/V135__list_and_search_indexes.sql`
- Modify/verify: `internal/database/database_test.go` (it pins some index shapes; run it)

```sql
-- V135: listing performance (org+folder+sort, alive rows) and content-search
-- GIN indexes. Stock Postgres only: no CREATE EXTENSION anywhere.
-- NOTE (post-review): the folders index originally sketched here was dropped —
-- V009's UNIQUE (org_id, parent_id, name) already covers the folder scan.

CREATE INDEX IF NOT EXISTS idx_notebooks_org_folder_updated
  ON notebooks (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dashboards_org_folder_updated
  ON dashboards (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_connectors_org_folder
  ON connectors (org_id, folder_id) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_cells_source_fts
  ON cells USING gin (to_tsvector('simple', source));
CREATE INDEX IF NOT EXISTS idx_widgets_query_fts
  ON widgets USING gin (to_tsvector('simple', query));
```

**Steps:** write migration → `bash scripts/go-test.sh ./internal/database/ -count=1 -timeout 3m`
(migration runner + pinned-shape tests must pass; fix expectations if the pin covers these
tables) → add a test asserting the six indexes exist after migration
(`SELECT indexname FROM pg_indexes WHERE ...`).

**Commit** — `perf(api): add listing composite indexes and content-search GIN indexes`

---

# Phase B — Folder pagination

### Task B1: Two-phase pagination for `GET /folders/{id}` and `GET /folders`

**Files:**
- Modify: `internal/api/folder_handlers.go` (both handlers + `folderContents` struct :199)
- Test: `internal/api/folder_handlers_test.go` (extend)

**Response additions** (existing keys unchanged):

```go
type folderContents struct {
	Folder     *models.Folder     `json:"folder,omitempty"`
	Folders    []folderItemFolder `json:"folders"`     // complete (tree correctness)
	Notebooks  []folderNotebook   `json:"notebooks"`   // first page
	Connectors []folderConnector  `json:"connectors"`  // first page
	Dashboards []folderDashboard  `json:"dashboards"`  // first page
	Totals     map[string]int     `json:"totals"`      // notebooks/connectors/dashboards
	HasMore    map[string]bool    `json:"has_more"`
}
```

**Handler algorithm** (per paginated type): fetch `SELECT id, updated_at` (connectors:
`id, name`; dashboards: `id, updated_at`) for ALL rows of the section → `resolveActions` →
filter `view` → `totals[type] = len(visible)`, ordered visible id slice → slice
`[0, limit)` → hydrate `WHERE id = ANY($page)` (dashboards re-apply the batch snapshot
substitution) → restore order → attach `can_*`. Query params: `limit` (default **50**,
clamped 1–200). Note the extra `run` action is NOT needed in listings.

**Tests:**
- 60 notebooks, 30 visible: `totals.notebooks == 30`, 30 returned, `has_more.notebooks == false`.
- 120 notebooks with hidden ones interleaved: page 1 + page 2 via the Task B2 endpoint cover
  every visible id exactly once (no skips/duplicates) and `totals` is exact.
- `limit=0` → 400 or clamp (pick clamp consistent with the repo's audit endpoint — it rejects;
  match `/audit`: invalid → 400); `limit=999` → clamped to 200.
- Root listing: same assertions via `GET /folders`.

**Commit** — `feat(api): paginate folder listing sections with exact visible totals`

### Task B2: Load-more endpoints

**Files:**
- Modify: `internal/api/router.go` (register `GET /api/v1/folders/contents` and `GET /api/v1/folders/{id}/contents` — Go 1.22 ServeMux resolves the literal over `{id}`)
- Modify: `internal/api/folder_handlers.go`
- Test: `internal/api/folder_handlers_test.go`

Contract: `?type=notebook|connector|dashboard&offset=0&limit=50` →
`{"items": [...], "total": 57, "has_more": false, "offset": 50}` where `items` uses the same row
shapes as the sections (`folderNotebook`/`folderConnector`/`folderDashboard`), produced by the
same B1 section helpers (`folderSectionIDs`/`folderSectionPageIDs` + per-type hydration) so the
dashboard snapshot substitution applies to every page. Root variant reads `folder_id IS NULL`.
Validation matches the design's settled contract: 400 on unknown `type`, non-numeric or negative
`offset` (missing `offset` = 0), and non-numeric or `< 1` `limit` (missing `limit` = 50); a
`limit` above 200 clamps to 200.

Tests: pagination walk (all pages, exact totals with hidden rows), root variant, invalid inputs,
and that `/folders/contents` does not shadow `/folders/{id}` in the router.

**Commit** — `feat(api): add folder section load-more endpoints`

### Task B3: Frontend — section counters + Load more

**Files:**
- Modify: `web/src/pages/HomePage.tsx` (sections at :1139-1317; queries at :513-523)
- Modify: `web/src/types/index.ts` (`HasMore`, `Totals` on the folder-contents type)
- Modify: `web/src/test/handlers.ts` (folder fixtures gain totals/has_more)
- Test: `web/src/test/HomePage.test.tsx`

Behavior:
- Each section header shows an instrument-label counter `50 of 1,247` when `total > loaded`;
  plain count otherwise.
- "Load more" (secondary button) renders when `has_more[type]`; click fetches
  `GET /folders/{id}/contents?type=…&offset=<loaded>&limit=50` (root: `/folders/contents`),
  appends into local per-folder state, disables while fetching, single-flight.
- Any `['folder-contents']` invalidation/refetch clears accumulated extras for that folder
  (first page wins; no duplicates).
- When the local name filter is active and the section has unloaded items, a muted hint appears:
  "Filtering loaded items — press ⌘K to search everything" (with Ctrl variant on non-Mac).
- Missing `totals`/`has_more` in a response (older fixtures) degrade to no counter/no button.

Tests: counter text, append behavior (no duplicates after `refetchType: 'all'` invalidation),
button hidden without `has_more`, hint rendering, and keyboard focusability of the button.

**Commit** — `feat(web): paginate folder sections with load more`

---

# Phase C — Search backend

### Task C1: `internal/search` package (interface + PG FTS matcher)

**Files:**
- Create: `internal/search/search.go` (types + interface), `internal/search/tsquery.go` (pure builder), `internal/search/pg.go` (PG implementation)
- Create: `internal/search/tsquery_test.go`, `internal/search/pg_test.go`

```go
// ContentHit is one content match: the resource that owns the matched text plus
// a plain-text snippet (highlight markers stripped; the client highlights).
type ContentHit struct {
	ResourceID string
	Snippet    string
}

// Searcher matches resource content. types is a subset of
// {"notebook","dashboard"}; the PostgreSQL implementation uses core FTS.
// A future ClickHouse implementation can replace it without API/UI changes.
type Searcher interface {
	ContentMatches(ctx context.Context, orgID string, types []string, query string, limitPerType int) (map[string][]ContentHit, error)
}
```

**tsquery builder** (pure, heavily tested):

```go
// BuildTSQuery turns free text into a safe to_tsquery('simple', …) expression:
// [a-z0-9_]+ runs, lowercased, each quoted and prefix-flagged, joined with &.
// Returns "" when there are no usable tokens.
func BuildTSQuery(q string) string
```
Rules: split on anything outside `[a-zA-Z0-9_]`; lowercase; wrap each token in single quotes
(`'order_items':*`); join ` & `; no escaping needed after the split. Tests: `Order Items` →
`'order':* & 'items':*`; `don't` → `'don':* & 't':*`; `%_&;:` → ""; long input capped
(256 chars) to bound tsquery size; CJK → "" (documented degradation).

**PG implementation** (`pg.go`, constructor `NewPGSearcher(pool *pgxpool.Pool) *PGSearcher`):
- notebooks:
  ```sql
  SELECT c.notebook_id::text, c.source
  FROM cells c JOIN notebooks n ON n.id = c.notebook_id
  WHERE n.org_id = $1 AND n.deleted_at IS NULL AND c.source <> ''
    AND to_tsvector('simple', c.source) @@ to_tsquery('simple', $2)
  ORDER BY n.updated_at DESC
  LIMIT $3
  ```
  Dedupe by notebook (first cell wins), snippet via a second-pass `ts_headline('simple', source, q, 'MaxWords=24, MinWords=8, ShortWord=2, StartSel=, StopSel=')` on the same query
  (`SELECT ts_headline(…) … WHERE id = $cellID`), or inline `ts_headline` in the original query —
  choose the inline form to avoid a second round trip:
  `SELECT c.notebook_id::text, ts_headline('simple', c.source, to_tsquery('simple', $2), 'MaxWords=24, MinWords=8, ShortWord=2, StartSel=, StopSel=')`.
- dashboards:
  ```sql
  SELECT w.dashboard_id::text, ts_headline('simple', w.query, to_tsquery('simple', $2), 'MaxWords=24, MinWords=8, ShortWord=2, StartSel=, StopSel=')
  FROM widgets w JOIN dashboards d ON d.id = w.dashboard_id
  WHERE d.org_id = $1 AND d.deleted_at IS NULL AND w.query IS NOT NULL AND w.query <> ''
    AND to_tsvector('simple', w.query) @@ to_tsquery('simple', $2)
  ORDER BY d.updated_at DESC
  LIMIT $3
  ```
- `limitPerType` capped at 200; per-type results capped after dedupe.

**Tests (real DB):** seed a notebook with cells (`SELECT revenue FROM …`) and a dashboard with a
query widget; assert exact hits, dedupe (two matching cells → one notebook hit), snippet
plainness (no `<b>`/markers), org isolation (other org's rows never returned),
`BuildTSQuery("")` → no content pass; injection safety: `q` values `"'; DROP"`, `"a & b"`,
`"(:*)"` produce no error and match literally or not at all.

**Commit** — `feat(search): add the swappable Searcher interface with a Postgres FTS implementation`

### Task C2: Search endpoint + resource summaries

**Files:**
- Create: `internal/api/resource_summaries.go` (`resourceSummary` + builders: per-type subtitle, folder path resolution `folderPathsFor(ctx, orgID, folderIDs) map[string]string` via one recursive CTE, favorites flag loader)
- Create: `internal/api/search_handlers.go`
- Modify: `internal/api/router.go` (`GET /api/v1/search`)
- Test: `internal/api/search_handlers_test.go`

**Handler algorithm:**

1. Parse/validate: `q` trimmed 1–200 runes (400 otherwise); `types` default all nine
   (`notebook, dashboard, folder, connector, agent, model_config, skill, tool, mcp_server`);
   `limit` default 5 max 20; `favorites=1`.
2. Titles per type (one bounded query each; cap = max(4×limit, 50), ≤ 200): `ILIKE '%'||$q||'%'`
   with `%`, `_`, `\` escaped (`replaceAll` order: `\` first). Notebooks match
   `title OR COALESCE(description,'')`; folders `name`; connectors `name`; agents
   `name OR COALESCE(description,'')`; model_configs `name`; skills `name OR description`;
   tools `name OR description`; mcp_servers `name`.
3. Dashboards (draft/publish rule): fetch candidates with
   ```sql
   SELECT d.id::text, d.title, d.folder_id::text, d.updated_at,
          LATERAL (SELECT v.title FROM dashboard_versions v
                   WHERE v.dashboard_id = d.id ORDER BY v.version DESC LIMIT 1) AS snapshot_title
   FROM dashboards d
   WHERE d.org_id = $1 AND d.deleted_at IS NULL
     AND (d.title ILIKE $2 OR snapshot_title ILIKE $2)
   LIMIT $3
   ```
   then per row: `effective = snapshot_title IS NOT NULL AND !canEdit ? snapshot_title : draft_title`;
   keep only rows where `effective ILIKE q`; rank on `effective`. This is the no-leak rule:
   viewers never match draft titles.
4. Content: `searcher.ContentMatches(org, ["notebook","dashboard"], q, cap)`; dashboard hits kept
   only for `edit` holders; notebook hits for `view` holders.
5. ACL filter: `resolveActions` per type over all candidate ids (`view`, `edit` for dashboards);
   drop invisible rows.
6. If `favorites=1`: one query loading the caller's favorite ids for the candidate types and
   intersect.
7. Merge title + content hits per resource (title match wins; content adds snippet), rank
   exact > prefix > contains > content-only, tiebreak `updated_at DESC`, slice to `limit`,
   `has_more = visibleCandidates > limit`.
8. Build `resourceSummary` rows: id, title, subtitle, snippet (nullable), folder_id,
   folder_path, updated_at, is_favorite, can_edit. Response groups only for types with results,
   fixed order.

**Tests (the leak suite is the point):**
- Title substring: `"revenue"` finds `Revenue by region`; case-insensitive; description match.
- Content: notebook with cell `SELECT sum(amount) FROM orders` found by `sum am` (prefix);
  snippet contains the match text; viewer (view-only) finds it too.
- **Leak 1**: viewer cannot find a published dashboard by its draft-only title (draft renamed
  after publish, `total` for dashboards is 0 for the viewer; editor finds it).
- **Leak 2**: viewer searching draft widget SQL gets no dashboard hit; editor gets one.
- **Leak 3**: never-published dashboard IS findable by title for a viewer, and the result title
  is the draft title (that's what they see).
- Published title match: viewer searching the snapshot title gets the snapshot title back.
- Visibility: stranger sees nothing; group-granted and folder-inherited notebook found.
- `favorites=1` filters; `types=notebook` filters; invalid type → 400; `q` too long → 400.
- Injection: `%`, `_`, `'`, `&`, `:*` in `q` never error, never over-match.
- Folder paths appear for nested items.

**Commit** — `feat(api): add the catalog search endpoint with viewer-safe dashboard semantics`

### Task C3: `search_resources` agent tool + MCP allowlist

**Files:**
- Modify: `internal/agent/tools_platform.go` (add the ToolDef next to `list_notebooks`)
- Modify: `internal/agent/tools_seed.go` (`builtinSeedRev = 4`, add seed entry)
- Modify: `internal/api/mcp.go` (`mcpToolAllowlist["search_resources"]`)
- Verify: `internal/oauth/scopes.go` (map `search_resources` to the read scope)
- Test: `internal/agent/tools_search_test.go` (or extend `tools_platform_test.go`), plus the existing `TestMCPToolsListMatchesAllowlist` and OAuth scope coverage tests

**ToolDef:**

```go
{
	Name:        "search_resources",
	Description: "Search the org's resources (notebooks, dashboards, folders, connectors, agents, models, skills, tools, MCP servers) by title and content. Returns grouped results with snippets. Refine the query instead of paging.",
	HandlerName: "search_resources", // same registry pattern as list_notebooks
	// Parameters: query (string, required), types (array of strings, optional),
	//             limit (integer, optional, 1-20, default 5)
	// Execute: call the same internal search service the HTTP handler uses,
	// scoped to the tool caller (ACL enforcement via resolveActions), return
	// compact JSON groups.
}
```

Seed entry: `{Name: "search_resources", Description: "<same text>", HandlerName: "search_resources", Rev: builtinSeedRev}`.

**Tests:** tool returns ACL-filtered results (stranger sees none; grantee sees the notebook);
types/limit validation; `TestMCPToolsListMatchesAllowlist` passes (it reads the allowlist);
OAuth scope test passes with the scope mapping. Bump check: seeding test asserts `seed_rev`
refresh for the new tool.

**Commit** — `feat(agent): add the search_resources tool and expose it over MCP`

---

# Phase D — Favorites backend

### Task D1: V136 favorites migration

**Files:** Create `internal/database/migrations/V136__favorites.sql`; test in `internal/database/database_test.go`

```sql
-- V136: per-user favorites for catalog resources. resource_id is polymorphic
-- and deliberately carries no FK (same convention as acl_entries); cleanup is
-- explicit in purge/hard-delete paths.
CREATE TABLE favorites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL,
  resource_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, org_id, resource_type, resource_id)
);
CREATE INDEX idx_favorites_user_org ON favorites (user_id, org_id, created_at DESC);
```

Test: table + index exist; unique constraint enforced; user delete cascades.

**Commit** — `feat(api): add the favorites table`

### Task D2: Favorites endpoints

**Files:** Create `internal/api/favorites_handlers.go`; modify `internal/api/router.go`; test `internal/api/favorites_handlers_test.go`

- `PUT /api/v1/favorites/{type}/{id}` — validate type against the nine-type whitelist; `404` when
  the resource is unknown or not `view`-visible; `INSERT … ON CONFLICT DO NOTHING`; respond `204`;
  audit `favorite.add` (only when inserted).
- `DELETE /api/v1/favorites/{type}/{id}` — idempotent `204`; audit `favorite.remove` when a row
  was deleted.
- `GET /api/v1/favorites` — grouped like search (order `created_at DESC`, only non-empty groups),
  ACL-filtered via `resolveActions`, cap 500, summary rows via `resourceSummaries`.

Tests: put/delete idempotency; 404 on invisible (user B cannot favorite A's private notebook) and
unknown; list is per-user and ACL-filtered (revocation hides); audit events written; unique
constraint under double PUT; favorites survive rename (id-based).

**Commit** — `feat(api): add favorites endpoints with audit and ACL filtering`

### Task D3: Favorites cleanup on purge/hard delete

**Files:** Modify `internal/scheduler/scheduler.go` (`purgeTrash` :156), the hard-delete paths for agents/models/skills/tools/mcp_servers (locate with `grep -n "DELETE FROM acl_entries" internal/ -r` and follow each resource type), plus notebook hard delete

Add helper:

```go
// deleteFavoritesFor removes favorite rows for hard-deleted resources. Called
// wherever the resource's ACL rows are deleted (resource_id has no FK).
func deleteFavoritesFor(ctx context.Context, q permissionQuerier, resourceType string, ids []string)
```

Wire it: purgeTrash (notebooks/connectors/dashboards + recursive folder subtree: delete favorites
for the folder ids AND their descendant resources), agent/model/skill/tool/mcp hard deletes.
Tests: favorite a notebook → trash → purge → row gone; favorite a folder → purge subtree → row
gone; favorite an agent → hard delete → gone; user delete cascade already covered in D1.

**Commit** — `fix(api): clean favorites on purge and hard deletes`

---

# Phase E — Search UI

### Task E1: `useResourceSearch` + `SearchPalette`

**Files:**
- Create: `web/src/hooks/useResourceSearch.ts` (debounced query hook, `['search', q, types, favorites]`, `keepPreviousData`, abort on new input)
- Create: `web/src/components/SearchPalette.tsx` + co-located `SearchPalette.test.tsx`
- Create: `web/src/utils/highlight.tsx` (term highlighting helper + test)
- Modify: `web/src/types/index.ts` (`SearchResult`, `SearchGroup`)
- Modify: `web/src/test/handlers.ts` (MSW `/api/v1/search`, `/api/v1/favorites`)

**Component spec** (copy from `VariableSelect` for keyboard mechanics, `Modal` for the overlay):
portal overlay (z-index 1700, scrim `--bg-overlay`, 8px radius, floating-large shadow,
hairline border, width `min(640px, 92vw)`, top offset 15vh), `role="dialog"` + `aria-modal`,
focus trap (`useFocusTrap`), Escape via `useEscapeToClose`; input autofocus; 150ms debounce;
groups with instrument-label headers and type icons; row shows title (highlighted), one-line
snippet, `folder_path · subtitle`; keyboard ↑↓/Home/End/↵/Esc across all rows with
`aria-activedescendant`; Enter navigates (`notebook → /notebooks/:id`, `dashboard →
/dashboards/:id/view`, `folder → /?folder=:id`, `connector → /connectors`, `agent → /agents`,
`model_config → /models`, `skill → /skills`, `tool → /tools`, `mcp_server → /mcps`); footer kbd
hints + "View all results" row → `/search?q=`; empty query renders Favorites + Recents sections
(reuse `useFavorites` once Task F1 lands — until then render Recents only and add Favorites in
F1); loading = skeleton rows; error = one muted line + retry; nothing-found = "No matches for
“q”" with a hint line.

Tests: debounce (only one request for rapid typing), grouping + order, keyboard navigation
wrapping, Enter navigation targets, Escape closes, empty-state Recents, error retry,
highlight helper cases (case-insensitive, multiple terms, no HTML injection — render as spans,
never `dangerouslySetInnerHTML`).

**Commit** — `feat(web): add the search palette component`

### Task E2: Shortcut rebind + AppShell wiring

**Files:**
- Modify: `web/src/components/AppShell.tsx` (:135-164 shortcuts, lazy imports :11-13)
- Modify: `web/src/components/ShortcutsModal.tsx` (:34-37), the agent FAB tooltip (:228)
- Modify: `web/src/test/AppShell.test.tsx` (+ any test asserting Ctrl+K → agent)

Behavior: `Ctrl/Cmd+K` → palette (lazy `SearchPalette`), `Ctrl/Cmd+J` → agent panel toggle;
both keep the existing guards (skip in `bare` mode and inside INPUT/TEXTAREA/SELECT
`contentEditable`; agent keeps its `/chats` suppression, search stays enabled there). Layered
escapes keep working via the existing stack.

Tests: Ctrl+K opens palette (not agent), Ctrl+J toggles agent, guards hold when typing in an
input, `bare` mode binds nothing, ShortcutsModal lists the new bindings.

**Commit** — `feat(web): bind Ctrl/Cmd+K to search and move the agent panel to Ctrl/Cmd+J`

### Task E3: TopBar search field

**Files:** Modify `web/src/components/TopBar.tsx`; test `web/src/test/TopBar.test.tsx` (extend)

A field-styled button between the wordmark and the right cluster: magnifier, placeholder
"Search files, dashboards, SQL…", `⌘K`/`Ctrl K` kbd chip; `aria-label="Search"`; click/focus →
opens the palette (AppShell passes an `onOpenSearch` callback); below 768px it collapses to an
icon-only button; the palette trigger never renders inside `bare` mode.

Tests: renders, opens palette on click and on Enter/Space, kbd chip platform text, icon-only
below the breakpoint (jsdom + matchMedia mock pattern used elsewhere).

**Commit** — `feat(web): add the top-bar search field`

### Task E4: `/search` page

**Files:** Create `web/src/pages/SearchPage.tsx` (+ test `web/src/test/SearchPage.test.tsx`); modify `web/src/App.tsx` (lazy route `/search`)

Behavior: URL-synced `?q=&type=&favorites=1`; large autofocused input when `q` is empty; type
chips (All + types present in results) and a Favorites chip; groups at `limit=20/type`
with 2-line snippets, folder paths, `FavoriteButton`s and open actions; `has_more` line
"More matches — refine your search"; empty/no-results/error states per house patterns; results
respect debounce; navigation preserves query in history.

Tests: deep link `?q=x` fetches and renders; chip filters update the URL; favorites chip sends
`favorites=1`; empty state; has_more line; error retry.

**Commit** — `feat(web): add the search results page`

---

# Phase F — Favorites UI

### Task F1: `useFavorites` + `useToggleFavorite` + `FavoriteButton`

**Files:** Create `web/src/hooks/useFavorites.ts`, `web/src/components/FavoriteButton.tsx` (+ tests `web/src/test/FavoriteButton.test.tsx`); extend MSW handlers

- `useFavorites()` → `{ ids: Record<ResourceType, Set<string>>, groups, isLoading }` from
  `['favorites']` (staleTime 60s).
- `useToggleFavorite()` → optimistic cache update + PUT/DELETE + invalidate `['favorites']` +
  rollback on error; surfaces a quiet toast on failure (match the app's existing toast pattern —
  grep `toast` first; if none, revert silently with `console.error` like existing mutations).
- `FavoriteButton`: 26×26 (36 touch) bookmark, filled accent when active, `aria-pressed`,
  labels "Add to favorites"/"Remove from favorites", tooltip; stops row-click propagation.

Tests: toggle optimistic + rollback on 500; aria-pressed flips; labels; keyboard activation.

**Commit** — `feat(web): add favorites state and the bookmark toggle`

### Task F2: Place toggles across surfaces

**Files:**
- Modify `web/src/pages/HomePage.tsx` (folder/notebook/connector/dashboard rows)
- Modify `web/src/pages/DashboardsPage.tsx` (rows + grid cards)
- Modify `web/src/pages/ConnectorsPage.tsx`, `web/src/pages/AgentsPage.tsx`, `web/src/pages/ModelsPage.tsx`, `web/src/pages/SkillsPage.tsx`, `web/src/pages/ToolsPage.tsx`, `web/src/pages/MCPPage.tsx`
- Tests: extend `HomePage.test.tsx` + `ConnectorsPage.test.tsx` (representative); assert
  bookmark state comes from `['favorites']` and no duplicate fetches per row.

Placement rule: hover-revealed on desktop rows, always visible on touch and on cards; bookmark
sits left of the `⋯`/RowActions cluster; never shifts layout (reserve 26px).

**Commit** — `feat(web): bookmark toggles on every catalog surface`

### Task F3: Favorites filters (Files + Dashboards)

**Files:** Modify `web/src/pages/HomePage.tsx` (chip row), `web/src/pages/DashboardsPage.tsx`; tests

- A "Favorites" chip (bookmark icon) next to the Files search box: toggles a flat cross-folder
  view from `['favorites']` grouped by type (≤500), each row showing its folder path and open
  action; the local name filter applies to loaded favorites; the chip is URL-synced (`?fav=1`)
  so it survives refresh/back.
- Dashboards page: same chip filters rows/cards client-side by `useFavorites` ids (no new
  endpoint).
- Empty state for favorites: "No favorites yet — bookmark anything with the ✓… " bookmarks copy.

Tests: chip toggles view, URL sync, empty state, Dashboard filter hides non-favorites.

**Commit** — `feat(web): favorites filters for files and dashboards`

---

# Phase G — Provisioner connectors out of execution pickers

### Task G1: Shared filter + apply to every execution picker

**Files:**
- Create `web/src/utils/connectors.ts` + test `web/src/utils/connectors.test.ts`
- Modify `web/src/components/ConnectorSelector.tsx` (:60), `web/src/components/Cell.tsx` (:551), `web/src/pages/ToolsPage.tsx` (:424)
- Modify tests: `web/src/test/ConnectorSelector.test.tsx`, `web/src/components/Cell.test.tsx` (:800)

```ts
// Hides provisioner connectors that cannot execute for anyone
// (can_use === false, computed server-side). A currently-selected hidden
// connector is kept as a single disabled option so selects never blank out.
export function executableConnectorOptions<T extends { id: string; is_provisioner?: boolean; can_use?: boolean }>(
  connectors: T[], selectedId?: string | null,
): T[] {
  return connectors.filter((c) =>
    !(c.is_provisioner === true && c.can_use === false) || c.id === selectedId)
}
```

Both selects render `is_provisioner && can_use === false` connectors only as the disabled
selected option, preserving the existing "(provisioner — no access)" label for that one case;
the "(provisioner)" label stays for `can_use === true` ones; "(view only)" behavior for
non-provisioners is unchanged. Management surfaces (ConnectorsPage, WarehouseSettingsPage,
PermissionsPanel, search, favorites) are untouched.

Tests: hidden when non-executable; allowed provisioner visible with label; selected hidden
renders disabled with reason; view-only non-provisioner unchanged; ToolsPage select filtered.

**Commit** — `feat(web): hide non-executable provisioner connectors from execution pickers`

---

# Phase H — E2E, validation, docs

### Task H1–H3: E2E specs

- `e2e/search.spec.ts`: login → Ctrl+K opens palette → type a notebook title → result row →
  Enter opens the notebook; Ctrl+J opens the agent panel; `/search?q=…` deep link renders
  groups; favorites chip filters.
- `e2e/favorites.spec.ts`: bookmark a notebook from Files → chip shows it → reload persists →
  palette empty state lists it → unbookmark removes it.
- `e2e/folder-pagination.spec.ts`: use a new helper `seedNotebooks(request, token, folderId, n)`
  in `e2e/helpers.ts` (API loop) to create a folder with 120 notebooks under a fresh user; open
  the folder → counter `50 of 120` → Load more → 100 → Load more → 120, no duplicates
  (`getByTestId` count).

Run each with `npx playwright test --config=e2e/playwright.config.ts e2e/<spec>.ts` against the
worktree dev stack; specs that need collab config inject `window.__AETHER_CONFIG__` via
`context.addInitScript` as the dashboard specs do.

**Commits** — one per spec (`test(e2e): …`).

### Task H4: Real-browser validation + detector + perf evidence

1. `agent-browser` pass over: folder page (counters + load more), palette (open, type, keyboard,
   Enter, empty state), top-bar field, `/search` page, favorites toggles + filters,
   notebook connector picker (provisioner absent; selected provisioner still shown disabled).
   Use `agent-browser errors` / `console` to confirm nothing throws.
2. Run the impeccable detector once over the changed UI:
   `/home/jesus/.config/opencode/skills/impeccable/scripts/impeccable detect --json <changed targets>`;
   fix what it surfaces (light-mode and dark-mode screenshots for: palette, files page sections,
   favorites chips, connector dropdown).
3. Perf evidence: seed a 1,000-notebook folder (API loop against the dev stack), record
   `curl -w '%{time_total}'` for `GET /folders/{id}` before (git stash or main build) and after;
   record browser first-paint timing; paste numbers into the PR body.

**Commit** — `test: browser validation notes` (only if any code changed; otherwise no commit).

### Task H5: Docs + CI checklist

- AGENTS.md: add sections for (a) batched resolver invariant ("listings resolve via
  `resolveActions`; never add per-item permission checks"), (b) search architecture (stock FTS,
  `Searcher` interface, dashboard draft/publish viewer-safe semantics, endpoint), (c) favorites
  (table, endpoints, cleanup invariant), (d) folder pagination defaults, (e) the provisioner
  picker rule. Update the env tables only if any new var appears (none planned).
- Regenerate swagger: `swag init -g cmd/aether-server/main.go -o internal/api/docs`.
- Full local CI per AGENTS.md:
  ```bash
  task check
  cd web && npx tsc --noEmit && npm run build
  cd relay && npm run build
  task test:e2e
  ```
- Verify the full Go suite stays inside the 3-minute budget: `task test`.

**Commit** — `docs: document search, favorites, and listing invariants`

---

## Definition of done

- All Go + web unit tests green; `task check` clean; web + relay builds clean; full e2e green.
- Folder with 1,000+ items: server responds with a bounded query count and page-sized payloads;
  UI pages through with load-more; measured before/after documented.
- Search finds notebooks/dashboards by title and by content with correct ACLs; viewers can never
  see unpublished dashboard titles or SQL; agents/MCP can search with the same guarantees.
- Favorites work on every catalog surface and filter everywhere specified; revoked resources
  disappear from lists; purged resources leave no rows behind.
- Non-executable provisioner connectors never appear in an execution picker; an existing
  selection still renders (disabled) instead of blanking.
- Real-browser validation + detector pass recorded on the PR.
