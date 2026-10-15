# Search, Favorites, and Fast Folder Listings — Design

**Date:** 2026-10-14
**Status:** 📐 Proposed
**Related:** `docs/designs/yjs-source-of-truth.md` (cells.source is the documented searchable cache),
`2026-10-09-dashboard-live-collab-and-shared-cache-plan.md` (draft → publish read semantics),
AGENTS.md (ACL semantics, list endpoints)

## Problem

Three production reports, all in the "finding things in a big workspace" family:

1. **Large folders are slow to open.** `GET /folders/{id}` runs `checkPermission` per item for
   four actions (view/edit/delete/share). Each check costs ~5 queries (group memberships ×2,
   direct ACL, folder lookup, ancestor-recursive CTE), so a 1,000-item folder issues roughly
   **20,000 queries** before responding — and the endpoint has no limit, so the payload and the
   client render also scale linearly. The same per-item pattern runs in every list endpoint
   (`/folders`, `/home`, `/notebooks`, `/dashboards`, `/connectors`, `/recent`, `/trash`).
2. **No search.** There is no endpoint, no index, and no UI for finding notebooks or dashboards
   by title or content; the Files search box filters only the already-loaded current folder,
   client-side, title-only. Nothing is searchable in the rest of the catalog either.
3. **No favorites.** There is no way to mark a resource and filter it back later — no table, no
   API, no UI, no per-user resource preferences of any kind.

## Goals

- Folder pages respond in a small, item-count-independent number of queries; large folders are
  **paginated** (load more) instead of fully loaded, with exact visible totals.
- Global search across the whole catalog (notebooks, dashboards, folders, connectors, agents,
  models, skills, tools, MCP servers) by title/name/description and by content (notebook cell
  SQL; dashboard widget SQL for editors), reachable from a top-bar field, `Ctrl/Cmd+K`, and a
  deep-linkable `/search` page — and from agents/MCP via a `search_resources` tool.
- Per-user favorites for every catalog resource: bookmark toggle on list surfaces, favorites
  filters, and palette quick access — ACL-aware, server-side.
- No PostgreSQL extensions, no new services: everything runs on stock Postgres + Redis.

## Non-goals

- Mid-word content matching ("tem" → "items"); content search is word/prefix based (core FTS).
  Title search stays exact-substring. CJK content search is a documented degradation.
- Row virtualization or page-number pagers for folder views (load-more only in this wave).
- Paginating subfolders: the tree needs all children, and folder counts are structural
  (follow-up if a real 1k-subfolder case appears).
- Search-result pagination: results are capped per type with `has_more`; refine beats scroll.
- Favorites in the agent tool surface (`search_resources` only, for now).
- Per-user "recently visited" history (the existing `/recent` is org-wide last-updated).
- Text-panel / HTML-panel / custom-component content in dashboard search (widget SQL only).
- A ClickHouse search backend: the interface is built for it, the implementation is not.

## Decisions

| Decision | Choice | Alternatives rejected |
|---|---|---|
| Listing permission cost | Batched set-based resolver (`resolveActions`) sharing the existing evaluator | Per-item checks (status quo); materialized effective-permissions table (invalidation surface for no gain at this scale); Redis-caching list responses (staleness + invalidation complexity) |
| Folder listing UX | Server pagination, **"Load more" per section** with `N of M` counters; subfolders stay complete | Full loads (multi-MB payloads, linear server + DOM cost); infinite scroll (thrashy with 4 sections); numbered pagers (ceremony) |
| Page-size default | 50 per section (queryable up to 200) | 200 default (heavier first paint); no cap (reintroduces the problem) |
| Search engine | Stock Postgres: `ILIKE` for titles/names/descriptions, core FTS (`to_tsvector('simple')` + GIN, prefix lexemes) for content, behind a `Searcher` interface | pg_trgm (extension — **not installable in production**); tsvector ranking with stemming (tokenizes SQL badly); OpenSearch/Elasticsearch (new required service); app-side n-gram tables (storage + write amplification); Meilisearch/Typesense (another service now) |
| Search scope | Whole catalog; content = notebook cell SQL + dashboard widget SQL | Notebooks/dashboards only (user chose full catalog) |
| Dashboard content access | Widget SQL searchable by **editors only** (SQL is editor-only in the UI); viewers match **effective published titles** (latest snapshot, draft only pre-publish) | Searching drafts for everyone (leaks unpublished titles and SQL); snapshot-only search (misses pre-publish dashboards) |
| Favorites storage | Server-side `favorites` table + endpoints | localStorage (per-browser, invisible to API/agents, no cross-device) |
| Favorites glyph | **Bookmark** (filled/outline) | Star (already means "default connector") |
| Shortcuts | `Ctrl/Cmd+K` = search palette; agent panel rebinds to `Ctrl/Cmd+J` | Palette on Ctrl+P (search loses the universal chord); dropping the agent chord (loses a habit) |
| Search affordance | Persistent top-bar search field that opens the overlay (one implementation), `Ctrl/Cmd+K` anywhere | Inline top-bar typing with dropdown (more states, duplicate implementation) |
| Agent access | In scope: `search_resources` builtin + MCP allowlist entry | Follow-up (user explicitly pulled it into scope) |
| Provisioner connectors in execution pickers | Hidden when not executable (`is_provisioner && can_use === false`); a currently selected hidden connector stays as one disabled option so nothing blanks | Status quo disabled option "(provisioner — no access)" (the report: why show an unusable choice); hiding every provisioner even when `allow_provisioner_execution` is on (hides a legitimately allowed target); hiding non-provisioner "view only" connectors (their disabled + reason treatment stays) |

## Design

### 1. Batched permission resolution (`internal/api/permissions.go`)

The evaluation core already exists as pure functions (`candidateGrants`, `matchesUser`,
`grantsAction`, `subjectRank`, `folderActionImplications`); only the fetching loop is per-item.
Refactor:

- **`permIdentity`** — `{userID, orgID, orgRole, groupIDs []string, adminMode bool}` resolved
  once per request via `callerGroupIDsQ` (2 queries) + `adminModeFromContext`.
- **`evaluateCandidates(candidates []aclCandidate, userID, orgRole string, groupIDs []string, resourceType, action string) bool`** — steps 5–9 of `checkPermissionQ` extracted verbatim
  (specificity sort, user > group > role, Everyone exception, view implication, folder
  implications). `checkPermissionQ` keeps its own fetch path and calls it; the batch path builds
  candidate slices per resource and calls the same function. **Batch semantics equal single-check
  semantics by construction**, pinned by a parity test matrix.
- **`resolveActions(ctx, ident permIdentity, resourceType string, ids []string, actions []string) (map[string]map[string]bool, error)`** — for a page or a whole section:
  1. resource→folder map, one query:
     `SELECT id, folder_id FROM <table> WHERE id = ANY($1::uuid[]) AND org_id = $2`
     (folders: `SELECT id, parent_id FROM folders WHERE id = ANY($1::uuid[]) AND org_id = $2`;
     a NULL parent means direct ACL only, matching today's `parent_id` seed);
  2. direct ACLs, one query: `acl_entries WHERE resource_type = $1 AND resource_id = ANY($2::uuid[]) AND org_id = $3`;
  3. folder ancestry for **all** distinct folder ids in one recursive CTE seeded with the folder
     set, returning `(seed_id, depth, subject_type, subject_id, actions)` joined to
     `acl_entries` — depth 0 = the resource's immediate parent, matching today's walk;
  4. admin mode (`orgRole == admin && adminMode`) grants every requested action to ids that
     exist in the org (query 1 is the org scoping), mirroring the bypass;
  5. per resource: candidates = direct (specificity −1) + ancestors (depth 0…n, `subjectRank`),
     then `evaluateCandidates` per requested action.

  Constant ~5 queries per (type × request) regardless of item count, group and admin identity
  work shared across types.

- **Batched descendant-content filter** — `folderHasDescendantContent` (folder_handlers.go:103)
  gains a set-based sibling for all child folders that fail the batch view check: one recursive
  CTE over the seed set with today's content-visibility EXISTS predicates, aggregated per seed.
- **Call sites migrated**: `handleGetFolderContents`, `handleListRootContents`,
  `handleListHomeFolders`, `handleListNotebooks`, `handleListDashboards`,
  `handleListConnectors`, `handleGetRecent`, `handleListTrash`. Dashboards additionally
  **batch-fetch the latest published snapshot** for the non-editor subset (one
  `DISTINCT ON (dashboard_id) … ORDER BY dashboard_id, version DESC` query replacing the per-row
  `loadPublishedDashboardSnapshot` call). `checkSessionPermission` and session listings keep their
  current paths (different resource model, no folder walk).
- **Invariant** (documented in AGENTS.md): list endpoints resolve permissions via
  `resolveActions`; adding a per-item `checkPermission` loop back into a listing is a regression.

### 2. Index migration (V135)

```sql
-- list queries: org + folder + sort, alive rows only
CREATE INDEX IF NOT EXISTS idx_notebooks_org_folder_updated
  ON notebooks (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dashboards_org_folder_updated
  ON dashboards (org_id, folder_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_connectors_org_folder
  ON connectors (org_id, folder_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_folders_org_parent
  ON folders (org_id, parent_id) WHERE deleted_at IS NULL;

-- content search: stock FTS, no extensions
CREATE INDEX IF NOT EXISTS idx_cells_source_fts
  ON cells USING gin (to_tsvector('simple', source));
CREATE INDEX IF NOT EXISTS idx_widgets_query_fts
  ON widgets USING gin (to_tsvector('simple', query));
```

`to_tsvector('simple', …)` expression indexes are plain DDL (no `CREATE EXTENSION`), valid on
locked-down managed Postgres. NULL/empty `query` rows simply contribute no index entries.

### 3. Folder listing pagination

**Two-phase handler** (exact totals and pages even with hidden items interleaved):

1. *Resolve* — per section, fetch ids + sort columns only
   (`SELECT id, updated_at FROM notebooks WHERE org_id = $1 AND folder_id = $2 AND deleted_at IS NULL ORDER BY updated_at DESC` — connectors order by `name`, folders by `name`), run
   `resolveActions`, filter to `view`, producing the exact visible count **total** and the ordered
   visible id list.
2. *Hydrate* — fetch full rows for the requested `[offset, offset+limit)` slice only
   (`WHERE id = ANY($pageIds)`), restore page order, attach `can_edit`/`can_delete`/`can_share`
   from the resolved action map (dashboards apply the batched snapshot substitution for
   non-editors on the page).

**API**:

- `GET /folders/{id}` and `GET /folders` (root): `folders` stays complete; `notebooks`,
  `connectors`, `dashboards` return the first page (`limit` default **50**, max 200) plus
  `total` and `has_more` maps added to the existing `folderContents` shape
  (`folders`/`notebooks`/`connectors`/`dashboards` keys unchanged; `folderNotebook`,
  `folderConnector`, `folderDashboard` shapes unchanged).
- Load-more: **`GET /folders/{id}/contents?type=notebook|connector|dashboard&offset&limit`** →
  `{items: […], total, has_more}` (root variant `GET /folders/contents?…`; Go 1.22 ServeMux
  resolves the literal segment over `{id}`). Same row shapes as the sections.
- Unknown `type` → 400; invalid offset/limit → clamped (offset ≥ 0, 1 ≤ limit ≤ 200).

**Verified visible counts** use only id + sort columns in phase 1 — cheap even for 10k rows
(a few hundred KB of tuples, two queries + in-memory evaluation), while payloads and hydration
stay page-sized.

### 4. Search backend (`internal/search`)

```go
// Searcher matches resource content for a query. The PostgreSQL implementation
// uses core FTS; a future ClickHouse implementation can replace it without API
// or UI changes.
type Searcher interface {
    ContentMatches(ctx context.Context, orgID string, types []string, query string, limitPerType int) (map[string][]ContentHit, error)
}
type ContentHit struct {
    ResourceID string
    Snippet    string // plain text, markers stripped; the client highlights
}
```

**Matcher semantics**

- **Titles/names/descriptions**: `ILIKE '%' || $q || '%'` with `%`, `_`, `\` escaped. Fields:
  notebook title + description; folder name; connector name; agent name + description; model
  config name; skill name + description; tool name + description; MCP server name; dashboard
  **effective title** (below).
- **Content (FTS)**: the query is tokenized server-side into `[a-z0-9_]+` runs, lowercased,
  wrapped as quoted lexemes with prefix flags and joined with `&`:
  `'order_items':* & 'rev':*`. Zero tokens → content pass skipped. Titles are matched for every
  type even when `q` has no FTS tokens.
- **Notebook content**: `cells.source` for every caller with notebook `view`; snippet via core
  `ts_headline('simple', source, tsq, 'StartSel=, StopSel=')` (markers stripped, length-bounded).
- **Dashboard content**: `widgets.query` (**draft**) and **editors only** — widget SQL is
  editor-only in the UI, so content matches never reach viewers. Viewers are matched against
  their **effective title** only.
- **Effective dashboard title per caller**: editors → draft `dashboards.title`; non-editors →
  latest `dashboard_versions.title` when a version exists, falling back to the draft for
  never-published dashboards (the same rule `GET /dashboards` and `GET /dashboards/{id}` apply).
  Searching a viewer's query against draft titles is deliberately never done — no unpublished
  title leak.
- **Ranking** (per type, applied in Go after visibility filtering): 0 = case-insensitive exact
  title, 1 = title prefix, 2 = title contains, 3 = content-only; tiebreak `updated_at DESC`.
  Candidate fetch is capped per type (max(4×limit, 50), ≤ 200) before filtering; `has_more`
  reports a possibly-truncated result set ("refine your search" hint on the results page).

**Endpoint** — `GET /api/v1/search` (see §5).

### 5. Search + resource summaries API (`internal/api/search_handlers.go`)

`GET /api/v1/search?q=&types=&favorites=1&limit=`

- `q`: required, 1–200 runes after trim; `types`: comma list, default all; `limit`: per type,
  default 5, max 20; `favorites=1`: restrict to the caller's favorites.
- Response groups in fixed type order (notebook, dashboard, folder, connector, agent,
  model_config, skill, tool, mcp_server):

```json
{ "query": "revenue",
  "groups": [
    { "resource_type": "notebook",
      "has_more": true,
      "results": [
        { "id": "…", "title": "Revenue by region", "subtitle": "Notebook",
          "snippet": "SELECT region, sum(revenue) …", "folder_id": "…",
          "folder_path": "Home / Finance", "updated_at": "…",
          "is_favorite": true, "can_edit": true } ] } ] }
```

- `subtitle` carries the type-specific descriptor (connector type, agent, model
  provider/model, tool kind …). `folder_path` is resolved for the distinct folders of the page
  in one query. `is_favorite` comes from the caller's favorites (one small query). All results
  pass `resolveActions` `view` — search can never surface an invisible resource.
- Shared `resourceSummary` builder reused by `/favorites` and the folder load-more rows where
  shapes overlap.

### 6. Favorites (V136 + `internal/api/favorites_handlers.go`)

```sql
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

`resource_id` carries no FK (the repo's ACL convention), so cleanup is explicit:

- `PUT /api/v1/favorites/{resource_type}/{resource_id}` — requires `view` on the resource
  (404 when unknown or invisible); idempotent upsert; audited `favorite.add`.
- `DELETE /api/v1/favorites/{resource_type}/{resource_id}` — idempotent; audited
  `favorite.remove` on an actual delete.
- `GET /api/v1/favorites` — grouped by type, ordered `created_at DESC`, ACL-filtered through
  `resolveActions`, capped at 500 rows total. Revoked access hides the entry; the row remains
  until purge.
- `resource_type` whitelist: folder, notebook, dashboard, connector, agent, model_config,
  skill, mcp_server, tool.
- Cleanup: a `deleteFavoritesFor(ctx, q, resourceType, ids)` helper called from `purgeTrash`
  (including the recursive folder-subtree purge) and from the hard-delete paths where the
  corresponding rows/ACL entries are deleted (agents, models, skills, tools, MCP servers,
  notebooks). User deletion and org deletion cascade via the FKs.

### 7. Frontend: search UX

- **TopBar field** (`web/src/components/TopBar.tsx`): centered, 320–480px, magnifier icon,
  placeholder "Search files, dashboards, SQL…", `⌘K`/`Ctrl K` kbd chip; `aria-label="Search"`;
  click/focus opens the palette (single implementation, two affordances). Below 768px it
  collapses to an icon button.
- **`SearchPalette`** (`web/src/components/SearchPalette.tsx`, lazy-loaded from `AppShell` like
  the agent panel): portal overlay above the top bar (z-index 1700, floating 8px radius,
  floating-large shadow, scrim), focus-trapped dialog whose input takes focus on open;
  150ms debounce, in-flight request cancelled on new input. Results grouped by type with
  instrument-label headers, type icon in accent, match highlighting (`--accent-light`
  background), one-line snippet, folder path + `subtitle` meta. Keyboard: ↑/↓ across all rows,
  ↵ opens (notebook → `/notebooks/:id`, dashboard → `/dashboards/:id/view`, others → their
  page), esc closes (via `useEscapeToClose`), Tab cycled by the trap. Footer: kbd hints +
  "View all results ↵". **Empty state**: the caller's Favorites (≤5) and org Recents (≤5, from
  `/recent`) with quiet headers and a one-line hint. Errors render one quiet inline line with a
  retry affordance; loading shows skeleton rows — never a blank panel.
- **`/search` page** (`web/src/pages/SearchPage.tsx`, lazy route): URL-synced
  `(?q=&type=&favorites=1)`; large search field (autofocused when `q` empty), type chips (All +
  the types present in the org's catalog) and a Favorites chip; grouped results at 20/type with
  snippets (2 lines), folder paths, bookmark toggles and open actions; `has_more` renders a
  "More matches — refine your search" line; empty state names what was searched and offers the
  favorite filter; skeleton and error states match house patterns.
- **Shortcuts**: `AppShell` binds `Ctrl/Cmd+K` → palette and `Ctrl/Cmd+J` → agent panel
  (existing guards: skip in `bare` mode and inside INPUT/TEXTAREA/SELECT/contentEditable, stay
  enabled on `/chats`). `ShortcutsModal` and the agent FAB tooltip ("Open AI Agent (Ctrl+J)")
  update together; tests asserting the old binding update too.
- **Highlighting**: client-side, case-insensitive, wraps query terms in title/snippet with a
  `<mark>`-style span using `--accent-light` (accent budget respected; no fills elsewhere).

### 8. Frontend: favorites UX

- **`FavoriteButton`** (`web/src/components/FavoriteButton.tsx`): 26×26px (36 on touch)
  bookmark icon button matching `RowActions` metrics; filled accent when active; `aria-pressed`;
  labels "Add to favorites"/"Remove from favorites"; optimistic toggle via
  `useToggleFavorite()`.
- **Placement**: Files rows (folders, notebooks, dashboards, connectors — hover-revealed on
  desktop, always visible on touch), Dashboards page cards and rows, and the catalog pages
  (Connectors, Agents, Models, Skills, Tools, MCPs) rows.
- **State**: `useFavorites()` (`['favorites']` query) gives per-type id sets; toggling
  optimistically updates the cache and invalidates `['favorites']`. One source of truth, so
  the star state is consistent across Files, Dashboards, palette, search page, and catalog
  pages.
- **Filters**: a "Favorites" chip on the Files page switches the view to a flat cross-folder
  list (grouped by type, each row showing its folder path, ≤500, `created_at DESC`); the
  Dashboards page gets the same chip; `/search` has the favorites chip. Catalog pages keep
  their existing filters; the bookmark toggle there feeds the same surfaces.
- **Files notes**: when the local name filter is active under a section with `has_more`, a
  muted hint says it filters loaded items and points at global search.

### 9. Frontend: folder pagination

- Sections render their first page from `GET /folders/{id}`; each paginated section shows an
  instrument-label counter (`50 of 1,247`) and a secondary "Load more" button when `has_more`.
- Load more fetches `/folders/{id}/contents?type=…&offset=…` and appends (React Query
  infinite-style accumulation seeded from the first page; invalidation stays prefix-based on
  `['folder-contents']` so mutations refresh every loaded page).
- The folder tree keeps using subfolders from the same endpoint (complete) — no tree changes
  beyond the batched backend.
- Per-row render cost is unchanged; virtualization remains a follow-up, since sections are now
  bounded by what the user actually loaded.

### 10. Agent + MCP: `search_resources`

- Builtin tool (`internal/agent/tools_platform.go` or a sibling) with args `query` (string,
  required), `types` (array, optional), `limit` (int, optional, default 5, max 20); returns the
  same grouped summaries with snippets, ACL-filtered through `resolveActions` — agents search
  exactly what the caller can see. No `ConfirmRequired`; default timeout.
- Added to the **MCP allowlist** (read-only tool), with `builtinSeedRev` bumped so existing orgs
  receive it, and a description that tells the agent to refine instead of paginate.
- `search_resources` supersedes nothing: `list_notebooks`/`list_connectors` keep working.

### 11. Provisioner connectors out of execution pickers

A warehouse provisioner is reserved for the reconcile worker. Unless the warehouse sets
`allow_provisioner_execution` **and** management is enabled, execution through it fails closed
(`resolveExecutionTarget` → `ErrProvisionerNotExecutable`), and `handleListConnectors` already
reports exactly that predicate as `can_use: false` (connector_handlers.go:242-244). Today the
pickers still render it as a disabled option labeled "(provisioner — no access)" — noise, since
it can never be chosen.

- **Rule**: execution-target pickers hide connectors with `is_provisioner === true && can_use === false`.
  A provisioner with `can_use === true` (admin override on, management enabled) stays, carrying
  today's "(provisioner)" label.
- **Current-selection exception**: when the current value points at a hidden connector, it
  renders as a single disabled option with its name and the existing reason label — a cell,
  widget, or variable query that references a provisioner keeps displaying its target instead of
  silently blanking; running requires picking another connector.
- **Shared helper** (`web/src/utils/connectors.ts`, e.g. `executableConnectorOptions(connectors, selectedId)`)
  applied to the shared `ConnectorSelector` (notebook header in `NotebookPage`, `DashboardPage`,
  `DashboardEditorPage` picker, `DashboardVariablesPanel` options query, `WidgetConfigDrawer`
  SQL) and to the cell inline select (`Cell.tsx`), plus the `ToolsPage` tool-connector picker —
  one rule for every execution surface.
- **Unchanged**: management and discovery surfaces keep listing provisioners — ConnectorsPage
  and WarehouseSettingsPage badges/pickers, PermissionsPanel, search results, and favorites.
  Server-side enforcement is untouched; this is presentation only.

## Testing

- **Resolver parity** (`internal/api/permissions_batch_test.go`, white-box, real DB): matrix of
  subjects (direct user/group/everyone, folder depth 0/1/2, deny-over-allow, view implication,
  folder edit/manage expansion, admin mode on/off, cross-org) × actions (view/edit/delete/
  share/run/use) asserting `resolveActions` == `checkPermission` for every cell; plus empty
  sets, missing ids, and the descendant-content batch.
- **Query-count guard**: the test server wraps its pool with a pgx `QueryTracer`; a folder
  listing with 10 items and with 120 items must issue the same (bounded) number of queries —
  the N+1 cannot come back silently.
- **Folder pagination** (`folder_handlers_test.go`): exact `total`/`has_more` with hidden items
  interleaved between pages (no skips/duplicates), stable order, limit/offset clamping, type
  validation, root variant, and the batched snapshot substitution per page.
- **Search** (`search_handlers_test.go`): title substring; FTS prefix ("dash" → dashboard);
  ranking order; snippets; `types`/`q` validation; LIKE and tsquery injection safety (quotes,
  `%`, colons, `&`); visibility (stranger, group grant, folder-inherited grant); **leak tests**:
  viewer cannot find a draft-only dashboard title or draft widget SQL, editor can; viewer
  searching the published title gets the snapshot title back; never-published dashboards stay
  findable by title for viewers; notebook content visible to viewers.
- **Favorites** (`favorites_handlers_test.go`): put/delete idempotency, 404 on invisible/unknown,
  ACL-filtered list (revocation hides), unique constraint, audit events, purge + hard-delete
  cleanup (including recursive folder purge), cascade on user delete.
- **Agent/MCP**: `search_resources` returns filtered results; allowlist test covers the new
  entry; seed revision refresh covered by existing seeding tests.
- **Web unit** (Vitest + MSW): palette (debounce, grouping, keyboard nav, empty state with
  favorites/recents), search page (URL params, chips, favorites filter), `FavoriteButton`
  (optimistic toggle, aria), TopBar field, Files pagination (append + counters), shortcuts
  rebind, highlight helper.
- **Execution pickers** (Vitest): a non-executable provisioner is hidden; an allowed provisioner
  (`can_use === true`) stays with its label; a currently selected hidden provisioner renders as
  a disabled option (select never blanks); non-provisioner "(view only)" options are unchanged.
  Existing assertions on the old "(provisioner — no access)" option in `Cell.test.tsx` and
  `ConnectorSelector.test.tsx` update with the behavior.
- **E2E** (Playwright): `e2e/search.spec.ts` (Ctrl+K palette flow, Ctrl+J still opens the
  agent, `/search` deep link, favorites chip), `e2e/favorites.spec.ts` (toggle on Files +
  Dashboards, filter chip, palette empty state, persists after reload),
  `e2e/folder-pagination.spec.ts` (seed ~120 notebooks in a folder via API, load more, counter
  text, no duplicate rows).
- **Real-browser validation** with `agent-browser` for every UI change (AGENTS.md mandate) and
  the impeccable detector once after the UI is complete.
- **Perf evidence**: before/after timings for a seeded 1,000-item folder (curl + browser),
  recorded in the PR.

## Rollout

- Migrations V135 (indexes) and V136 (favorites) apply automatically at startup; both are
  additive.
- `GET /folders/{id}` and `GET /folders` gain `total`/`has_more` and now default to 50 items
  per paginated section (previously unbounded). The web app ships pagination in the same
  release; the `aether` CLI and external API consumers page through `/contents` if they need
  more (check `cmd/aether` folder commands during implementation).
- New endpoints are additive; swagger regenerated (`swag init`); AGENTS.md gains the search,
  favorites, and "listings use the batched resolver" sections.
- No feature flags: nothing new to operate, nothing to turn on.

## Follow-ups

- ClickHouse `Searcher` implementation if content grows beyond stock-Postgres comfort.
- Subfolder pagination + row virtualization for truly enormous folders.
- Mid-word content matching / CJK content strategy (n-gram table or a search service).
- Favorites in the agent tools; per-user recents surface in the palette.
- Search result pagination if "refine your search" proves insufficient.
- Text/HTML/custom panel content in dashboard search.
