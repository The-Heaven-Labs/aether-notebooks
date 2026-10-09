# Live Dashboards & Shared Query Cache — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** (1) Share dashboard query-cache entries across users with identical effective data access, with cold-burst single-flight; (2) make dashboards live co-editing documents (Yjs) with notebook-grade presence, where Postgres stays a derived read cache.

**Architecture:** Feature 1 replaces the viewer ID in the Redis cache key with an effective-access fingerprint and wraps misses in a per-key single flight. Feature 2 makes a per-dashboard Yjs document (`dashboard:{uuid}`) the source of truth: browser edits flow through the Hocuspocus relay; the Go backend seeds docs from existing rows, merges incoming state on store, materializes `dashboards`/`widgets` rows, and applies backend-originated writes through a new `internal/dashboarddoc` service that publishes updates for relays to fan out.

**Tech Stack:** Go (net/http, pgx, ygo), React+Vite+TypeScript, Yjs/Hocuspocus relay (TS), Redis, Postgres.

**Design doc:** `docs/plans/2026-10-09-dashboard-live-collab-and-shared-cache-design.md`

**Required skills during execution:** `test-driven-development`, `verification-before-completion`.

**Conventions:**
- All Go test commands include `-timeout 3m` (AGENTS.md requirement).
- Tests hit a real DB via `setupTestServer(t)` (`internal/api/testhelpers_test.go`); no mocks.
- Never commit to `main`; work on `feat/dashboard-live-collab`.
- Run `task fmt` before each commit. Run `task check` (or the narrower test target) before each phase's final commit.

---

## Spikes (timeboxed, do first)

### Task S1: Hocuspocus 4.4 read-only + doc auth verification

**Files:**
- Inspect: `relay/node_modules/@hocuspocus/server/dist/index.d.ts`
- Create (throwaway): `relay/scripts/spike-readonly.ts`

**Step 1:** Confirm the `connected` hook receives `connection` and that assigning `connection.readOnly = true` prevents the client from applying document updates (test with a raw `@hocuspocus/provider` or `y-websocket` script).
**Step 2:** Confirm `onAuthenticate` receives `documentName` and `token`.
**Step 3:** Record the outcome in this plan file under "Spike results" (append at the end). If `readOnly` does not block outbound updates, use a `beforeHandleMessage` guard that rejects `MessageType.Sync` messages from read-only connections (reference the enum in the dist types).
**Step 4:** Delete the throwaway script (or keep under `relay/scripts/` if useful). Commit nothing for this spike unless a helper file is created.

### Task S2: ygo nested structures ↔ JS Yjs compatibility

**Files:**
- Test: `internal/dashboarddoc/compat_test.go` (create; may be kept as a permanent test)
- Create (throwaway): `/tmp/opencode/verify-yjs.mjs`

**Step 1:** Write a Go test that builds a doc with `GetMap("meta")` → nested `GetMap("settings")` → `GetArray("variables")` of maps, `GetMap("widgets")` keyed by UUID → `GetText("query")`, encodes state.
**Step 2:** Run `go test ./internal/dashboarddoc -run TestCompat -v -timeout 3m`; dump the state to a file.
**Step 3:** In Node with the relay's `yjs` package, load the state, assert the structure, mutate it (add a widget, append text to the query), encode and dump.
**Step 4:** Go test loads the mutated state and asserts it projects correctly. If any structure fails to round-trip, note the limitation here and adjust the doc model before continuing.

---

# Phase 1 — Shared dashboard query cache

## Task 1: Effective-access fingerprint helper

**Files:**
- Create: `internal/api/dashboard_cache.go`
- Test: `internal/api/dashboard_cache_test.go` (package `api_test`)

**Step 1: Write the failing test**

Use the warehouse test fixtures (`internal/api/warehouse_test_fixtures_test.go`) to create a managed ClickHouse connector; use direct SQL to insert `warehouse_table_grants` rows for two users with the same group and one user with different grants. Assert:

```go
fp1 := fingerprintFor(t, srv, orgID, userA, connID) // same grants via group
fp2 := fingerprintFor(t, srv, orgID, userB, connID)
fp3 := fingerprintFor(t, srv, orgID, userC, connID) // extra table
require.Equal(t, fp1, fp2, "identical effective grants must share a fingerprint")
require.NotEqual(t, fp1, fp3, "different grants must not share a fingerprint")

fpUnmanaged := fingerprintFor(t, srv, orgID, userA, unmanagedConnID)
require.Equal(t, "unmanaged", fpUnmanaged)
```

**Step 2: Run it and watch it fail**

Run: `go test ./internal/api -run TestDashboardAccessFingerprint -v -timeout 3m`
Expected: FAIL — function/endpoint missing.

**Step 3: Implement**

```go
// dashboard_cache.go
const dashboardFingerprintUnmanaged = "unmanaged"

// dashboardAccessFingerprint returns a stable hash of the viewer's effective
// data access for a served connector. Managed ClickHouse connectors hash the
// effective table-grant set (user + groups + Everyone, org-membership gated);
// everything else executes with a shared stored credential, so the fingerprint
// is constant. Errors are the caller's signal to fall back to a per-viewer key.
func (s *Server) dashboardAccessFingerprint(ctx context.Context, orgID, userID string, warehouseID *string) (string, error) {
    if warehouseID == nil || !s.warehouseManagementEnabled() {
        return dashboardFingerprintUnmanaged, nil
    }
    wid, err := uuid.Parse(*warehouseID)
    if err != nil {
        return "", err
    }
    keys, _, err := s.loadEffectiveWarehouseGrants(ctx, wid, orgID, userID)
    if err != nil {
        return "", err
    }
    var b strings.Builder
    for _, k := range keys { // already sorted by (database, table)
        b.WriteString(k.Database)
        b.WriteByte(0)
        b.WriteString(k.Table)
        b.WriteByte('\n')
    }
    sum := sha256.Sum256([]byte(b.String()))
    return hex.EncodeToString(sum[:]), nil
}
```

**Step 4: Run and pass**

Run: `go test ./internal/api -run TestDashboardAccessFingerprint -v -timeout 3m` → PASS.

**Step 5: Commit**

```bash
git add internal/api/dashboard_cache.go internal/api/dashboard_cache_test.go
git commit -m "feat(api): effective-access fingerprint for shared dashboard query cache"
```

## Task 2: Thread the warehouse id through widget resolution

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go` (`resolveWidgetConnector`, both callers)
- Test: `internal/api/dashboard_query_test.go` (existing tests must still pass)

**Step 1:** Change the signature to return the served connector's `warehouse_id`:

```go
func (s *Server) resolveWidgetConnector(ctx context.Context, orgID, widgetConnectorID, viewerConnectorID string) (servedConnectorID string, warehouseID *string, err error)
```

`resolveWidgetConnector` already selects `warehouse_id` for both connectors; return the winner's value (nil-safe). Update both call sites (`handleExecuteDashboardWidget`, `handleDashboardVariableOptions`) and add `WarehouseID *string` to `dashboardQueryParams`.

**Step 2:** Run the existing suite to confirm no regression:

Run: `go test ./internal/api -run TestDashboardQuery -v -timeout 3m` → PASS.

**Step 3: Commit** `feat(api): return served connector warehouse from widget resolution`.

## Task 3: Fingerprint-keyed cache key + shared-hit behavior

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go` (`dashboardQueryCacheKey`, `runDashboardQuery`, params)
- Test: `internal/api/dashboard_query_test.go`

**Step 1: Write the failing test** — two users in one org against one unmanaged connector, same widget and variables:

```go
recA := executeDashboardWidget(t, srv, tokenA, dashID, body)
recB := executeDashboardWidget(t, srv, tokenB, dashID, body) // tokenB has view_with_data via share
require.Equal(t, "cached", decode(recB)["cached"].(bool) ...) // assert resp["cached"] == true
```

Also assert the reverse: a third user with different filter values gets `cached == false` on first run.

**Step 2: Run and fail.**

**Step 3: Implement**

- `dashboardQueryCacheKey` takes `AccessFingerprint string` instead of `Identity.UserID`:

```go
func dashboardQueryCacheKey(p dashboardQueryParams) string {
    if p.SQL == "" { return "" }
    sum := sha256.Sum256([]byte(strings.Join([]string{
        p.OrgID, p.AccessFingerprint, p.CacheScope, p.ConnectorID, p.SQL,
        fmt.Sprintf("%d", p.MaxRowsOverride),
    }, "\x00")))
    return dashboardCachePrefix + hex.EncodeToString(sum[:])
}
```

- In `runDashboardQuery`, before the cache lookup:

```go
fp := dashboardFingerprintUnmanaged
if p.CacheScope == "" { // authenticated runs only
    if computed, err := s.dashboardAccessFingerprint(ctx, p.OrgID, p.Identity.UserID, p.WarehouseID); err == nil {
        fp = computed
    } else { // fail closed: keep caching, never share on uncertainty
        fp = "user:" + p.Identity.UserID
    }
}
p.AccessFingerprint = fp
```

Public runs (`CacheScope = "token:…"`) keep `"public"` (creator identity + token scope already discriminate).

**Step 4:** Run `go test ./internal/api -run TestDashboardQueryCache -v -timeout 3m` → PASS.

**Step 5: Commit** `feat(api): share dashboard query cache across identical effective access`.

## Task 4: Refresh write-back semantics

**Files:** Modify `internal/api/dashboard_query_handlers.go`; test `internal/api/dashboard_query_test.go`

**Step 1:** Write a test using `SELECT random()` as the widget query:
- A fresh → R1; B → cached R1; A `bypass_cache: true` → fresh R2 (`cached: false`, rows differ); C → cached R2.
- Assert `cache_expires_at` is present on cached responses.

**Step 2:** Run and confirm the behavior already holds (write-back exists); if C gets R1, fix the bypass path to write the fresh result to the shared key. Fix if needed.

**Step 3: Commit** `test(api): pin shared-cache refresh write-back semantics`.

## Task 5: Per-key single-flight

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go`, `internal/api/router.go` (Server struct: add `dashboardCacheSF singleflight.Group` or a dedicated small struct)
- Create: `internal/api/dashboard_query_internal_test.go` (package `api`)
- Modify: `internal/api/dashboard_cache.go` (test seam)

**Step 1: Write the failing test (internal package):**

```go
func TestDashboardQuerySingleFlight(t *testing.T) {
    s := newTestServerWithCache(t) // mirror setupTestCache wiring from testhelpers_test.go:69
    var calls atomic.Int32
    old := dashboardQueryComputeHook
    dashboardQueryComputeHook = func(srv *Server, ctx context.Context, p dashboardQueryParams) (*dashboardQueryResponse, error) {
        calls.Add(1)
        time.Sleep(200 * time.Millisecond)
        return dashboardQueryResult(&executor.ResultSet{Columns: nil, Rows: [][]any{{1}}}, 5, false, nil), nil
    }
    t.Cleanup(func() { dashboardQueryComputeHook = old })

    p := dashboardQueryParams{OrgID: "o", Identity: dashboardIdentity{UserID: "u"}, ConnectorID: "c", SQL: "SELECT 1", AccessFingerprint: "fp"}
    var wg sync.WaitGroup
    for i := 0; i < 8; i++ { wg.Add(1); go func() { defer wg.Done(); _, _ = s.runDashboardQuery(context.Background(), p) }() }
    wg.Wait()
    require.Equal(t, int32(1), calls.Load(), "concurrent identical misses must run one query")
}
```

**Step 2:** Run and fail (`dashboardQueryComputeHook` undefined).

**Step 3: Implement**

```go
// dashboard_cache.go
// dashboardQueryComputeHook is a test seam: when non-nil it replaces the real
// query computation under the single flight.
var dashboardQueryComputeHook func(*Server, context.Context, dashboardQueryParams) (*dashboardQueryResponse, error)
```

In `runDashboardQuery`, replace the direct miss-path execution with:

```go
v, err, _ := s.dashboardCacheSF.Do(cacheKey, func() (any, error) {
    if dashboardQueryComputeHook != nil {
        return dashboardQueryComputeHook(s, ctx, p)
    }
    return s.executeDashboardQuery(ctx, p) // extracted from the current body
})
```

Extract the existing openQuery→Execute→record-success→cache-set body into `executeDashboardQuery`. Bypass handling stays the same: bypass skips the cache read and therefore enters `Do` as a fresh leader (or joins a genuinely in-flight fresh run).

**Step 4:** Run `go test ./internal/api -run TestDashboardQuerySingleFlight -v -timeout 3m` → PASS.

**Step 5:** Run the whole cache test set + `task fmt`; commit `feat(api): dedupe concurrent dashboard query executions per cache key`.

## Task 6: Phase 1 verification

**Step 1:** Run `go test ./internal/api -run 'TestDashboard' -v -timeout 3m` → all pass.
**Step 2:** Run `go vet ./...` and `go test ./internal/api -run TestOpenQuery -timeout 3m` (routing regressions).
**Step 3:** Commit any fmt changes: `chore: fmt`.

---

# Phase 2 — Live plumbing

## Task 7: Migration V129

**Files:** Create `internal/database/migrations/V129__dashboard_yjs_documents.sql`

```sql
-- Yjs document state for live dashboard co-editing (binary CRDT state).
CREATE TABLE dashboard_yjs_documents (
    dashboard_id UUID PRIMARY KEY REFERENCES dashboards(id) ON DELETE CASCADE,
    state BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**Verify:** `go test ./internal/database -run TestMigrate -v -timeout 3m` (or the package's migration test) → PASS. Commit `feat(db): dashboard yjs document table`.

## Task 8: `internal/dashboarddoc` — projection types + seed + project

**Files:**
- Create: `internal/dashboarddoc/doc.go` (types + keys), `seed.go`, `project.go`
- Test: `internal/dashboarddoc/doc_test.go`

Key shapes (must match the design doc):

```go
type WidgetDoc struct {
    ID          string
    Type        string
    Layout      Layout
    ConnectorID *string
    Query       *string
    Language    string
    NotebookID  *string
    CellID      *string
    Config      map[string]any // stored as JSON string in the doc
}
type Projection struct {
    Title    string
    Settings map[string]any
    Variables []map[string]any
    Widgets  map[string]WidgetDoc
}
```

- `Seed(p Projection) ([]byte, error)`: root maps `meta`/`widgets`, settings map, variables `Y.Array` of maps, per-widget map with `layout` map and `query` `Y.Text`.
- `Project(state []byte) (*Projection, error)`: inverse; validates widget type ∈ {chart,table,metric,text}, layout non-negative ints, config JSON decodes, query language ∈ {"", "sql"}.

Tests: seed→project round-trip equality (including multi-variable widgets), empty seed, malformed doc rejection, and the S2 compat test.

**Commit** `feat: dashboarddoc seed/projection for live dashboards`.

## Task 9: Backend write ops (`apply.go`)

**Files:** Create `internal/dashboarddoc/apply.go`; test `internal/dashboarddoc/apply_test.go`

Functions (each returns the merged state bytes + the full-state update to publish):

- `UpsertWidget(state []byte, w WidgetDoc) ([]byte, error)`
- `DeleteWidget(state []byte, widgetID string) ([]byte, error)`
- `UpdateLayout(state []byte, widgetID string, l Layout) ([]byte, error)`
- `SetQuery(state []byte, widgetID, sql string) ([]byte, error)` (Y.Text replace)
- `UpdateMeta(state []byte, title *string, settings map[string]any, variables []map[string]any) ([]byte, error)`

Tests: apply→project assertions; two concurrent applies from the same base merged with `crdt.ApplyUpdateV1` lose neither change (widget A layout + widget B SQL).

**Commit** `feat: dashboarddoc mutation ops`.

## Task 10: Merge-on-store + materialize

**Files:** Create `internal/dashboarddoc/store.go`, `internal/dashboarddoc/materialize.go`; test `internal/dashboarddoc/store_test.go` (real DB via the database package helpers)

- `MergeAndStore(ctx, pool, dashboardID string, incoming []byte) error`:
  1. `SELECT state FROM dashboard_yjs_documents WHERE dashboard_id=$1` (may be absent),
  2. decode into a `crdt.Doc`, `ApplyUpdateV1(doc, incoming, nil)`,
  3. upsert merged state,
  4. call `Materialize` in the same transaction.
- `Materialize(ctx, tx, dashboardID, proj)`:
  - `UPDATE dashboards SET title=$, settings=$, updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL` (no resurrect on trash/purge),
  - diff widgets: upsert present, `DELETE FROM widgets WHERE dashboard_id=$1 AND id <> ALL($2)`,
  - preserve `created_at` from the doc when present, else now.

Tests: store merges a stale update without clobbering a backend write; materializer diff add/update/delete; trashed dashboard is a no-op; invalid doc returns an error and materializes nothing.

**Commit** `feat: merge-and-materialize dashboard docs`.

## Task 11: Internal doc endpoints

**Files:** Modify `internal/api/internal_handlers.go`, `internal/api/router.go`; test `internal/api/internal_dashboard_yjs_test.go`

- `GET /internal/dashboard-yjs/{dashboard_id}`: 401 without internal token; 404 unknown/trashed; lazy seed when no state (`SELECT` miss → `dashboarddoc.Seed` from rows → `INSERT … ON CONFLICT DO NOTHING` → re-read → return bytes), else return state.
- `PUT /internal/dashboard-yjs/{dashboard_id}`: read body; 404 unknown/trashed (check `dashboards.deleted_at IS NULL`); `MergeAndStore`; 204.

Route registration next to the existing `GET/PUT /internal/yjs/{notebook_id}` in `router.go:527-528`. Tests mirror `internal_handlers_test.go` (GET empty → PUT → GET returns state; seed round-trip; trashed → 404). Commit `feat(api): internal dashboard yjs endpoints`.

## Task 12: Collab authorize endpoint

**Files:** Modify `internal/api/internal_handlers.go`, `router.go`; test `internal/api/collab_authorize_test.go`

`POST /internal/collab/authorize` body `{"document_name": "dashboard:<uuid>"}`:
- 401 invalid/absent token (reuse `validateInternalToken`; OAuth tokens already rejected there);
- 400 non-dashboard doc name; 404 unknown/trashed dashboard or wrong org;
- `can_edit` = dashboard ACL `edit`; otherwise require `view`/`view_with_data`; no access → 403.
- Response `{"document_id": "...", "can_edit": bool}`.

Tests: edit/view/none matrix, cross-org, trashed, malformed names. Commit `feat(api): dashboard doc authorization endpoint`.

## Task 13: Redis update + invalidate publishers

**Files:** Create `internal/api/dashboard_doc_events.go`; test `internal/api/dashboard_doc_events_test.go`

- `publishDashboardDocUpdate(ctx, dashboardID string, update []byte)` → raw bytes on `aether:dashboard-doc:{id}`.
- `publishDashboardDocInvalidate(ctx, dashboardID string)` → JSON `{"reason":"..."}` on `aether:dashboard-doc-invalidate:{id}`.
- Best-effort: detach with `context.WithoutCancel`, 1s deadline, log on failure (mirror `warehouse_invalidation.go`).

Tests: subscribe first, assert payloads. Commit `feat(api): dashboard doc Redis event channels`.

## Task 14: REST handlers write through `dashboarddoc`

**Files:** Modify `internal/api/dashboard_handlers.go` (add/update/delete widget, update dashboard settings/title), `internal/api/dashboard_query_handlers.go` (convert-to-query), `internal/agent/tools_manage.go`; test updates in existing files.

Pattern for each handler:

```go
state, err := s.loadOrSeedDashboardDoc(ctx, orgID, dashID) // helper on Server using dashboarddoc.Seed
newState, err := dashboarddoc.UpsertWidget(state, dashboarddoc.WidgetDoc{...})
if err := s.storeAndMaterializeDashboardDoc(ctx, dashID, newState); err != nil { ... }
s.publishDashboardDocUpdate(ctx, dashID, newState)
```

Keep: permission checks, validations (notebook view IDOR, layout bounds, language), audit entries, response shapes. `loadWidgets`/GET keep reading materialized rows. `validateWidgetLayout` reads the materialized table (already updated by the previous store, so consecutive adds remain correct).

Split into three commits:
1. widget add/update/delete (`feat(api): route widget writes through dashboarddoc`)
2. dashboard title/settings (`feat(api): route dashboard settings through dashboarddoc`)
3. convert-to-query + agent tools (`feat: route agent dashboard writes through dashboarddoc`)

Run the full `TestDashboard*` API suite after each commit.

## Task 15: Relay — routing, authorize, readOnly, Redis fan-in

**Files:** Modify `relay/src/index.ts`; create `relay/src/dashboardDocs.ts` (pure helpers) and `relay/src/dashboardDocs.test.ts`; modify `relay/package.json` (add `vitest` devDependency + `"test": "vitest run"`; add `ioredis` if not resolvable transitively).

Behavior:
- `onLoadDocument` / `onStoreDocument`: route `dashboard:` names to `GET/PUT ${API_URL}/internal/dashboard-yjs/{id}`; notebook names keep the existing endpoint.
- `onAuthenticate`: call `${API_URL}/internal/collab/authorize` with the token; stash `canEdit` + `documentName` in `context`.
- `connected`: set `connection.readOnly = true` when `!context.canEdit` (or the S1 fallback guard).
- Redis subscriber (`ioredis`): psubscribe `aether:dashboard-doc:*` (binary update) → if the doc is loaded, `openDirectConnection(name).transact(doc => Y.applyUpdate(doc, update))`; psubscribe `aether:dashboard-doc-invalidate:*` → disconnect matching connections and `closeConnections`/unload the doc (use the Server API available in the pinned version; S1 notes the exact method).
- Re-validation timer (~60s) per connection calling authorize; disconnect on failure.

`dashboardDocs.test.ts` (vitest, no live server): document-name routing, context mapping, invalidate → target connection selection. `cd relay && npm test` and `npm run build`.

Commit `feat(relay): dashboard documents, auth, and live update fan-in`.

---

# Phase 3 — Editor page on the document

## Task 16: Collaboration runtime + `useDashboardDoc`

**Files:** Create `web/src/components/dashboardCollabRuntime.ts`, `web/src/hooks/useDashboardDoc.ts`, tests `web/src/hooks/useDashboardDoc.test.tsx`.

- Runtime: ref-counted `Y.Doc` + `HocuspocusProvider` per dashboard (name `dashboard:{id}`, token, awareness user identical to `collabRuntime.ts:31-56`). Lazily imported.
- Hook: `useDashboardDoc(dashboardId, { enabled })` → `{ synced, connected, title, settings, variables, widgets: Widget[], awareness }` using `useSyncExternalStore` over `doc.update` events + memoized projection; editor mutators (`updateLayout`, `setQuery`, `setConfig`, `setTitle` debounced) run local Y transactions.

Tests (mock/provider stub): projection updates on local transaction; remote update re-renders; mutators write expected types.

## Task 17: Editor wiring

**Files:** Modify `web/src/pages/DashboardEditorPage.tsx`, `web/src/components/WidgetConfigDrawer.tsx`; tests in existing files.

- Wait for `synced` before enabling edits; show "Reconnecting — editing is paused" banner when disconnected and disable mutators (drag/resize off, drawer save disabled).
- `saveLayout` writes `updateLayout` to the doc instead of `api.put`; drop the per-stop REST calls.
- SQL editor in `WidgetConfigDrawer` binds to the widget query `Y.Text` when the doc is available (seed-once pattern from `attachCollabToEditor`), falling back to local state for preview-mode runs.
- Chart config writes go to the doc; title input debounced to the doc.
- Add/delete widget, convert-to-query, settings/variables keep their REST calls (unchanged endpoints, now doc-backed server-side). Invalidate React Query `['dashboard', id]` after structural REST calls as today; for doc-sourced fields do not refetch.

## Task 18: Phase 3 verification

- `cd web && npx vitest run src/hooks/useDashboardDoc.test.tsx src/pages` (or repo test script).
- `cd web && npx tsc --noEmit`.
- Manual: two browser tabs, same editor; drag + type SQL; assert both update live; check console for Yjs errors.

---

# Phase 4 — Viewer live + presence + auto re-run

## Task 19: Viewer page renders the live doc

**Files:** Modify `web/src/pages/DashboardPage.tsx`; test `web/src/pages/DashboardPage.test.tsx` (existing).

- Use `useDashboardDoc`; when `synced`, render `title/settings/widgets` from the doc, else from REST. Keep `can_*`/`widgets_data` from REST and existing variable handling.
- Add `CollaboratorAvatars` to the header (adapt props to accept awareness from the dashboard provider).
- "Live" indicator while connected; silent REST fallback when not.

Tests: REST-first paint then doc override; presence renders.

## Task 20: Auto re-run affected widgets

**Files:** Create `web/src/utils/dashboardRunSignature.ts`; modify `web/src/pages/DashboardPage.tsx` (`QueryDataWidget` wiring), `web/src/components/QueryDataWidget.tsx`; test `web/src/utils/dashboardRunSignature.test.ts`.

- `runSignature(widget) = connector_id | language | query`.
- On doc change: widgets whose signature changed → `refresh()` after a per-widget 2s debounce; variable definition/default changes → re-run only widgets whose query contains the changed `{{token}}` (helper `referencedVariables(sql)`); cell ref changes → `qc.invalidateQueries(['notebook', nb])`; layout/config/title → no run.
- Tests with fake timers: typing SQL coalesces to one run; layout change runs nothing; variable change hits only referencing widgets.

## Task 21: Phase 4 verification

- `cd web && npx vitest run` for touched tests; `npx tsc --noEmit`.
- agent-browser manual validation: editor + viewer side by side; verify live layout/SQL propagation and presence; screenshot.

---

# Phase 5 — Lifecycle hardening, E2E, docs

## Task 22: Trash/purge invalidate + store guards

**Files:** Modify `internal/api/dashboard_handlers.go` (delete handler publishes invalidate), `internal/scheduler/scheduler.go` (purge publishes invalidate after deleting dashboards — see `purgeTrash` around line 187-194), relay invalidate handling (Task 15); tests in `internal/api/dashboard_handlers_test.go` and `internal/scheduler` tests.

Assert: trashed dashboard → authorize 403 + store 404; purge → doc row gone (FK) + invalidate published; restore → access resumes.

## Task 23: Revocation revalidation test

**Files:** `relay/src/dashboardDocs.test.ts` (timer logic with fake timers), `internal/api/collab_authorize_test.go`.

Assert: revoking view → next revalidation rejects; editor demoted → readOnly on next connection.

## Task 24: Playwright two-context E2E

**Files:** Create `e2e/dashboard-live.spec.ts` (follow `e2e/dashboard.spec.ts`).

Scenarios: (1) editor moves a widget and edits SQL, viewer sees both live within a few seconds; (2) viewer attempts a drag → no change persists; (3) editor adds a widget via REST → viewer renders it.

Run: `npx playwright test e2e/dashboard-live.spec.ts` (dev stack up per AGENTS.md).

## Task 25: Documentation + final verification

- Update `AGENTS.md`: dashboard cache keying + single flight; live dashboard doc architecture (doc name, endpoints, materialization, write-path split, degradation modes). Mirror in the design doc's status line.
- Run the full pre-PR checklist: `task check`, `cd web && npx tsc --noEmit`, `cd web && npm run build`, `cd relay && npm run build`, `task test:e2e`.
- Final commit; prepare PR description (squash-merge title: `feat: live dashboards and shared dashboard query cache (#<n>)`).

---

## Spike results

### S1 findings

Verified against the pinned build (`relay/node_modules/@hocuspocus/server` 4.4.0) by reading `dist/index.d.ts` + `dist/hocuspocus-server.cjs` and by running throwaway tests in `/tmp/opencode/` (`spike-readonly.cjs` in-process `MessageReceiver` test; `spike-readonly-e2e.cjs` full Server + real `@hocuspocus/provider`; `spike-direct.cjs` server-API exercise). Not committed.

**1. `readOnly` enforcement — complete; no fallback guard needed.**
- Enforced in `MessageReceiver.readSyncMessage`: SyncStep2 at `hocuspocus-server.cjs:296-308`, YjsUpdate at `:315-319`. A read-only connection's SyncStep2/Update is not applied to the server document, is not broadcast, and gets a `SyncStatus(false)` reply (`:304`, `:317`). SyncStep1, Awareness, QueryAwareness and Stateless are unaffected, so viewers still sync down and see presence.
- `Connection.readOnly` is public and mutable (`index.d.ts:815`) and is read at message-handling time, so setting it in a hook after construction works. `ConnectionConfiguration.readOnly` (`index.d.ts:372-375`) is copied into the `Connection` at `hocuspocus-server.cjs:971`.
- Nuance: rejection is server-side only. The provider applies local edits optimistically and does not revert on `SyncStatus(false)` (`web/node_modules/@hocuspocus/provider/dist/hocuspocus-provider.cjs:504-506`); `hasUnsyncedChanges` stays true. Keep viewer mutators disabled in the UI — `readOnly` is the security boundary, not a UI guarantee.
- Fallback if ever needed: use `beforeSync`, whose payload `type` is the inner y-protocols sync type (`0` SyncStep1 / `1` SyncStep2 / `2` YjsUpdate — `index.d.ts:675-695`) — not outer `MessageType.Sync` (`index.d.ts:352-366`, which also covers read-only SyncStep1) and not `beforeHandleMessage` (payload carries raw `update` bytes with no discriminator — `index.d.ts:614-625`). Throwing from either hook closes the connection (`hocuspocus-server.cjs:500-506`); it does not silently drop.
- Empirically: editor updates applied; SyncStep2/Update from read-only connections (set at construction or mutated afterwards) never reached the server doc; the E2E provider test rejected viewer edits under both `onAuthenticate` and `connected` wiring while viewers still received later editor edits; a raw Auth+Update burst queued during auth was also rejected.

**2. `onAuthenticate` payload** — includes `documentName`, `token`, and `requestHeaders` (`Headers`), plus `context`, `instance`, `request`, `requestParameters`, `socketId`, `connectionConfig`, `providerVersion` (`index.d.ts:525-536`; invoked at `hocuspocus-server.cjs:839-848`).

**3. Server APIs needed later** (signatures from `index.d.ts`; behavior confirmed empirically):
- Backend write: `const dc = await server.hocuspocus.openDirectConnection(documentName, context?)` (`:325`). `DirectConnection` = `{ document, instance, context, transact(cb): Promise<void>, disconnect({unloadImmediately?}?): Promise<void> }` (`:197-207`, `:793-796`; impl `hocuspocus-server.cjs:1042-1093`, `:1514-1519`). `transact` applies the update, broadcasts it to live clients, and schedules `onStoreDocument` (local origin does not skip store hooks). `disconnect()` defaults to `unloadImmediately: true` (immediate store + unload when no other connections); `{unloadImmediately:false}` keeps the doc warm.
- Enumerate: `server.hocuspocus.documents: Map<string, Document>` (`:263`); `document.getConnections(): Connection[]` (`:79`); also `document.connections`, `document.directConnectionsCount`, and per-connection `context`, `socketId`, `webSocket`.
- Disconnect: `server.hocuspocus.closeConnections(documentName?)` (`:293`; impl `hocuspocus-server.cjs:1272-1279`) closes all connections for a document (all docs when omitted). Per connection, `connection.close(event?)` sends a protocol CLOSE and removes it from the doc but does not close the socket (impl `:450-457`); `connection.webSocket.close(code, reason)` closes the actual socket (`webSocket` is public, `:800`). No `destroyConnection` API exists in 4.4.
- Unload: `server.hocuspocus.unloadDocument(document)` (`:324`; impl `hocuspocus-server.cjs:1486-1513`) — takes the Document instance (not the name), no-op while connections exist. Pattern: `closeConnections(name)` then an explicit unload if still loaded (the default close path often auto-unloads; observed).
- Re-validation timer: `connected` provides `instance`, `connection`, `context`, `documentName`, `socketId`, `requestHeaders`; create a per-connection `setInterval` there. `onDisconnect` (`:739-748`) has `socketId` but no `connection` — key a `Map` by `socketId+documentName` (or WeakMap/expando) to clear timers. On failure: `connection.close(ResetConnection)` (client reconnects and re-runs auth) or `connection.webSocket.close(...)` for hard termination; `instance.closeConnections(documentName)` drops all.

**4. `connected` hook payload** — `{ context, documentName, instance, request, requestHeaders, requestParameters, socketId, connectionConfig, connection, providerVersion }` (`index.d.ts:569-580`; invoked at `hocuspocus-server.cjs:794-799`, after the connection is created and pre-auth queued messages are replayed).

**Recommended enforcement for Task 15:** set `connectionConfig.readOnly = true` in `onAuthenticate`. It is read at `hocuspocus-server.cjs:851` for the scope the client sees (`"readonly"` vs `"read-write"` in the `authenticated` message) and at `:971` for the `Connection`, i.e. before any queued client message is replayed — race-free. Optionally re-assert `connection.readOnly = true` synchronously in `connected` as defense-in-depth (works empirically, but a client marked only there still sees `read-write` scope). No `beforeSync`/`beforeHandleMessage` fallback guard is required.

## Notes / risks carried from design

- Grant-lag window for the shared cache is accepted (documented in the design doc).
- In-process single-flight only in v1; distributed lock noted as future work.
- Public dashboards stay snapshot-based; no live channel in v1.
