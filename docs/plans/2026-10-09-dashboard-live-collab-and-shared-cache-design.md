# Design: Live Dashboards & Shared Dashboard Query Cache

**Status:** Approved (design)
**Date:** 2026-10-09
**Base:** `origin/main` @ `efb6be8d` (PR #244, dashboard UX overhaul)
**Branch:** `feat/dashboard-live-collab`

## Motivation

Two problems, raised together because they share a motive — making dashboards usable
by larger teams:

1. **Dashboard queries are cached per viewer.** `runDashboardQuery` keys the Redis
   cache as `sha256(org | viewer_id | scope | connector | SQL | maxRows)`. The viewer
   ID is deliberate (managed ClickHouse runs per-user identities with per-table
   grants), but it means 100 teammates opening the same dashboard run the same query
   100 times. Two users with the same query and filters should share a cache entry.
2. **Dashboards have no live collaboration.** Notebooks have the full live stack
   (Yjs/Hocuspocus for cell content, WS hub for events, presence avatars). Dashboards
   are REST + React Query: changes only appear on reload, and two editors are
   last-write-wins via separate REST calls. Dashboards should co-edit like notebooks:
   edits visible to everyone live, conflict-free, with presence.

## Decisions

| Decision | Choice |
|---|---|
| Cache sharing model | Replace viewer ID in the key with an **effective-access fingerprint** |
| Cold-burst handling | **Per-key single-flight** (in-process for v1) |
| Live scope | **Full Yjs co-editing** (not just event broadcast) |
| Page model | **Keep the view/editor split**; both pages subscribe to the same doc |
| Doc ownership | **Go owns the document** (ygo), relay stays thin |
| Data on live definition change | **Auto re-run affected widgets** for open viewers (debounced) |
| Public dashboards | **Out of scope for v1** (no JWT for the relay); unchanged behavior |
| Backend-originated writes | `dashboarddoc` service → store → materialize → Redis publish → relay applies |

---

# Feature 1 — Shared dashboard query cache

## Cache key change

Today (`internal/api/dashboard_query_handlers.go`):

```
sha256(org | viewer_id | cacheScope | connectorID | SQL | maxRowsOverride)
```

New:

```
sha256(org | accessFingerprint | cacheScope | connectorID | SQL | maxRowsOverride)
```

- Filters are interpolated into the SQL server-side, so different filter values
  already produce different keys. Same query + same filters + same effective access
  ⇒ same key ⇒ a shared hit. Widget identity is not in the key; identical SQL in two
  widgets shares an entry.
- Failed permission checks and missing `view_with_data` still fail before the cache is
  consulted, so the cache never bypasses authorization.

## Effective-access fingerprint

Computed in `runDashboardQuery` before the cache lookup. The served connector's
`warehouse_id` is already loaded by `resolveWidgetConnector`, so no extra round trip
for routing context.

| Case | Fingerprint |
|---|---|
| Unmanaged connector / kill switch off | constant — all executions use the stored credential and return identical data |
| Managed ClickHouse | hash of sorted, length-prefixed effective `(database, table)` keys |

No salt: the cache key already discriminates org and connector, and the
`"unmanaged"` constant cannot collide with a hex digest.

- Managed grants reuse `loadEffectiveWarehouseGrants()` (`internal/api/schema_visibility.go`),
  the exact user + group + Everyone union (org-membership gated) that execution and the
  reconcile worker rely on. Identical fingerprints ⇒ identical data access across the
  per-user ClickHouse identities the warehouse provisions.
- Provisioner execution with `allow_provisioner_execution` still runs as the per-user
  identity (`buildExecutionTarget` → `chaccess.UserIdent`), so the same grant
  fingerprint applies.
- **Fail-closed:** if the grant query fails, fall back to today's per-user key
  (caching keeps working, just unshared). Never fall back to a shared key.

### Accepted windows (documented trade-offs)

- A grant written in Postgres may not yet be applied by the ClickHouse reconcile. In
  that window a user whose fingerprint matches a teammate's could read cached rows for
  a table their ClickHouse role does not yet allow. Bounded by the cache TTL (default
  30s) and the reconcile cadence.
- Revocation changes the fingerprint immediately, so revoked users miss shared
  entries rather than hitting them; stale entries expire with the TTL.
- The cache remains a *performance* layer only; ClickHouse is the enforcement point.

## Refresh & bypass semantics (unchanged)

`bypass_cache` (manual refresh, Run all, auto-refresh) skips the cache read. The fresh
result **is written back to the shared entry**, so one user's refresh freshens data for
everyone in the TTL window — the same semantics public dashboards have today. Public
dashboards keep the `token:<token>` scope and creator identity.

## Single-flight (cold-burst control)

The shared cache only helps once the first result lands. Without single-flight, 100
users arriving within one query's runtime still execute 100 times.

- `runDashboardQuery` wraps the miss path in a per-cache-key single flight
  (`golang.org/x/sync/singleflight`, already a dependency): one leader computes,
  concurrent identical requests wait and share the result.
- Applies to widget runs and variable-options queries (same code path).
- `bypass_cache` semantics: if no run is in flight, a bypass request leads a fresh
  computation; if one is in flight, it rides it (the in-flight run is fresh by
  definition). A non-bypass request that finds a cache entry never enters the flight.
- v1 is **in-process**: worst case one warehouse run per API replica per key per burst.
  A Redis leader-lock for cross-replica dedupe is an optional phase 2 (not built now).

## Alternatives considered

- **Share only when per-user permissions are off** — safe but loses the win exactly on
  managed warehouses, which is where the herd problem was raised.
- **Always share (drop viewer identity)** — data-exposure regression when grants differ.
- **No single-flight** — leaves the cold-burst 100× duplicate execution unaddressed.

---

# Feature 2 — Live dashboards (Yjs co-editing)

## Document model

Relay document name: **`dashboard:{uuid}`** (prefix avoids collision with notebook
UUID doc names).

```
meta            Y.Map
 ├ title        string
 ├ settings     Y.Map  { grid_cols, auto_refresh_seconds, query_cache_seconds, public_live }
 └ variables    Y.Array<Y.Map>          (order matters in the parameter strip)
widgets         Y.Map<widgetUUID, Y.Map>
 ├ type, connector_id, language, notebook_id, cell_id
 ├ config       JSON string              (whole-value LWW; chart options don't merge field-wise)
 ├ layout       Y.Map { row, col, width, height }
 └ query        Y.Text                   (SQL — character-level co-editing in the drawer)
```

- Widgets are keyed by UUID (merge-safe identity; the grid is absolutely positioned so
  order is meaningless). Variables stay an ordered `Y.Array`.
- `query` is `Y.Text` so two editors in the same SQL drawer merge character-wise;
  layout fields are individual keys so moving widget A and resizing widget B merge.
- **Not in the doc:** query results, per-viewer variable values, ACLs, folder,
  share/public tokens, `created_by`, soft-delete state.

## Sync architecture (Path A: Go owns the document)

1. **Auth at connect.** Relay passes `documentName` to new
   `POST /internal/collab/authorize` (session JWT + doc name). Backend resolves the
   dashboard and returns `{canEdit}`: dashboard ACL `edit` → writer; `view` /
   `view_with_data` → reader; trashed/missing/cross-org → reject. Relay marks
   non-editors `connection.readOnly = true` and re-validates every ~60s
   (agent-WS pattern), disconnecting on failure. **Spike task:** confirm the pinned
   Hocuspocus 4.4 `connected` hook exposes a settable `readOnly` that rejects outbound
   document updates; fallback is a `beforeHandleMessage` guard.
2. **Load / lazy seed.** Relay `onLoadDocument` → `GET /internal/dashboard-yjs/{id}`.
   With no stored state, the backend seeds from the current `dashboards` + `widgets`
   rows using ygo, persists with `INSERT … ON CONFLICT DO NOTHING` then re-reads (so
   concurrent seeds converge on one state), and returns it. No backfill job; every
   dashboard is seeded on first touch.
3. **Store / merge.** Relay `onStoreDocument` → `PUT /internal/dashboard-yjs/{id}`
   with the full state. The backend **merges** the incoming update onto the stored
   state (`ApplyUpdateV1` is idempotent/commutative) instead of overwriting, so a
   relay holding a slightly stale doc can never clobber a backend-originated write.
4. **Materialize.** In the same transaction, parse the doc (ygo) and upsert the
   derived `dashboards`/`widgets` rows. Postgres stays a **derived read cache**
   (mirroring `cells.source`): `GET /dashboards/{id}`, list, CLI, agent reads,
   `validateWidgetLayout`, and trash all keep working. Materialization is the
   enforcement point for doc shape (widget type, layout bounds, config size): invalid
   entries are logged and never materialized.
5. **Backend-originated writes** (REST widget CRUD, convert-to-query, agent tools,
   settings/variables) go through a new `internal/dashboarddoc` service:
   seed → ygo transaction → store → materialize → publish the resulting Yjs update on
   Redis `aether:dashboard-doc:{id}`. The relay subscribes and applies the update to
   the doc **if that replica has it loaded** (idempotent; multi-replica safe; skipped
   when unloaded because the DB is already current).
6. Browser edits do not use step 5 at all: they go browser → relay → store → merge →
   materialize.

## Write-path split

| Change | Path |
|---|---|
| Layout drag/resize, SQL text, chart config, title (debounced) | **Direct Yjs** (browser → relay) — high-frequency, harmless |
| Widget add/delete, convert-to-query, settings/variables/grid-cols | **REST → `dashboarddoc`** — preserves the notebook-view IDOR check, layout bounds/overlap validation, audit (`widget.create`, …) |
| Share, ACLs, folder, trash | Unchanged |

Browser-direct writes skip `validateWidgetLayout`; the grid UI prevents overlap
client-side and the materializer rejects/clamps invalid entries.

## Permissions & lifecycle

- Read-only is enforced **server-side** at the connection, not by hiding UI.
- Revalidation covers ACL/role revocation (~60s window); upgrades to editor apply on
  reconnect/next validation.
- **Trash**: authorize treats trashed dashboards as gone; the delete handler publishes
  an invalidate event so relays disconnect viewers and unload immediately.
- **Purge**: `dashboards` hard-delete cascades `dashboard_yjs_documents`
  (`ON DELETE CASCADE`, mirroring `yjs_documents`); purge publishes the same invalidate
  event. The store endpoint rejects unknown/trashed dashboards so a stale in-memory
  doc cannot resurrect data.
- **Restore**: no doc change; access resumes on next connect.
- Folder moves / ACL edits / share have no doc impact and are picked up by
  revalidation.

## Frontend UX

- New runtime + `useDashboardDoc(dashboardId)` hook: shared Y.Doc + HocuspocusProvider
  (token, awareness user like `collabRuntime.ts`), `useSyncExternalStore`-style
  subscription to the doc projection, and editor mutation helpers. Lazily imported to
  keep the base bundle small.
- **Viewer page**: paints from REST first (definition + `widgets_data` + `can_*`),
  hands over to the synced doc. Per-viewer variables (URL → localStorage → default)
  unchanged. Auto-refresh/refresh unchanged. Falls back to REST snapshot when the
  relay is unavailable.
- **Editor page**: editing enabled only once `synced`; drag/resize, SQL, config, title
  write to the doc live. Relay down → "Reconnecting — editing is paused" banner and
  mutations disabled (no REST write fallback: it would fork the source of truth).
- **Presence**: existing `CollaboratorAvatars` (awareness) in both headers.
- **Auto re-run of affected widgets**: per-widget run signature
  (`connector_id + language + query text`); only widgets whose signature changed
  re-run, debounced ~2s per widget so a collaborator typing SQL doesn't trigger a
  query per keystroke. Variable definition/default changes re-run only widgets whose
  query references the changed `{{token}}`. Cell-linked reference changes refetch
  `widgets_data` (never execute). Layout/title/config changes re-render only. All
  re-runs flow through Feature 1's shared cache + single-flight.
- **Public dashboards**: unchanged (snapshot on load; `public_live` queries per
  request). A token-scoped live channel is a noted follow-up, not built.

## Degradation modes

| Outage | Effect |
|---|---|
| Relay down | Editors: paused + banner (local edits unsaved, no REST fallback). Viewers: REST snapshot |
| Redis down | Store/materialize still work; backend-originated live fan-out lost until reload; DB correct |
| One relay replica down | Clients on it retry/reconnect; other replicas and DB unaffected |

## Out of scope (v1)

- Public/anonymous live doc access.
- Distributed (cross-replica) single-flight for the query cache.
- Attribution ("updated by X") flashes; presence avatars only.
- Scheduler-triggered dashboard updates.

---

# Testing strategy

- **Go** (real DB, no mocks): fingerprint matrix (same grants → shared key/hit,
  different grants → miss, unmanaged constant, fail-closed fallback); single-flight
  (concurrent identical keys → one execution via a test seam); bypass semantics;
  `dashboarddoc` seed↔projection round-trip; store-merge never clobbers a backend
  write; materializer diff (add/update/delete); invalid-doc rejection; authorize
  matrix (edit/view/none/trashed/cross-org/OAuth token rejected); trash/purge
  invalidate + FK cascade.
- **Relay** (new minimal test setup): doc-name routing, authorize call + `readOnly`,
  Redis update application.
- **Web** (Vitest): `useDashboardDoc` store, run-signature/debounce logic, offline
  editor pause, viewer REST→doc handover.
- **E2E** (Playwright, two browser contexts): editor edits SQL + drags a widget while
  the viewer sees both live; viewer cannot mutate (server-side read-only); revocation
  disconnect. Mandatory agent-browser manual validation of both pages (AGENTS.md).

# Delivery order

1. **Shared cache + single-flight** (independent, shippable on its own).
2. **Live plumbing**: `dashboard_yjs_documents` migration, internal endpoints,
   relay changes, `dashboarddoc` service, materializer, lazy seeding.
3. **Editor on the doc**: direct Yjs writes + REST-backed structural ops.
4. **Viewer live**: doc rendering, presence, auto re-run, fallbacks.
5. **Lifecycle hardening + e2e**: trash/purge invalidate, revocation tests, Playwright.

# Risks

- **Materializer divergence** is the main correctness risk; store-merge + a single
  write path through `dashboarddoc`/relay are the mitigations.
- **Relay `readOnly` semantics** need the spike; fallback guard is specified.
- **Grant-lag window** for shared cache is accepted and documented above.
- **Yjs doc size**: widget config JSON is small; no concern expected, but the
  materializer should cap config size.
