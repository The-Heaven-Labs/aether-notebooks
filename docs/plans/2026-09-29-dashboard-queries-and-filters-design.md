# Dashboard Queries and Filters — Design

**Date:** 2026-09-29
**Status:** Approved (design)
**Branch:** `feat/dashboard-queries-and-filters`

## Summary

Turn dashboards from static views over notebook cell outputs into Grafana-style
live dashboards: widgets own their SQL and connector, a dashboard-level variable
bar drives filters that re-run queries, and query-backed option lists can chain
off each other. Existing cell-linked widgets keep working unchanged.

## Context

Today:

- Widgets reference a notebook cell (`widgets.notebook_id` + `cell_id`) and render
  that cell's **cached** outputs. There is no dashboard execute endpoint.
  "Run all" simply re-runs the linked cells via
  `POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/execute`
  (`internal/api/execute_handlers.go`). A TODO in `web/src/pages/DashboardPage.tsx`
  marks the missing dashboard execution path.
- Input widgets (`date_picker`, `date_range`, `freetext`, `number`,
  `multi_select`) exist but values live only in
  `web/src/contexts/DashboardParamsContext.tsx`; nothing is substituted into SQL.
- `DashboardSettings.parameter_overrides` (`internal/models/dashboard.go`) is dead
  config with no readers or writers.
- Notebook placeholders are raw `{{name}}` string replacement
  (`internal/executor/params.go`); parameters are `{name, type, default}` and the
  type is metadata-only. `{{ref}}` is overloaded to inline a sibling cell's SQL
  (`internal/api/slug_substitution.go`).
- Execution targets resolve per-user identities for managed ClickHouse
  (`internal/api/execution_target.go`); unmanaged connectors execute with the
  stored credential and require connector `use` for cell runs.
- There is no query-result cache. Redis (`internal/cache`) is used for WS pub/sub,
  OIDC state, SSO provider cache, and rate limiting.
- Dashboard ACL actions: `view`, `view_with_data`, `edit`, `share`, `delete`
  (`internal/api/permissions.go`).

## Goals

- Widgets may embed their own SQL + connector ("query widgets") instead of
  referencing notebook cells.
- Dashboard-level variables with a top filter bar: static options, free text,
  numbers, booleans, dates, date ranges, and query-backed select options with
  chaining (`depends_on`).
- Live, Grafana-style refresh: queries execute server-side on dashboard open and
  on (debounced) variable changes; per-widget loading, error, retry, and
  cancellation.
- Type-aware, server-side interpolation that is safe for shared and public
  dashboards.
- Backward compatibility: existing cell-linked widgets and public tokens keep
  working; input widgets migrate 1:1 into variables.

## Non-goals (v1)

- Server-side batch execution / result streaming over WebSocket.
- Real `metric` / `text` widget renderers (they keep today's table/markdown
  behavior).
- Raw/identifier interpolation formatter (no `{{var:raw}}`).
- Persisted result snapshots.
- Per-widget variable scoping.

## Decisions

| Question | Decision |
|---|---|
| Refresh model | Live queries (Grafana-style), debounced, with cancellation |
| Query storage | Widget-owned `connector_id` + `query` + `language`; existing cell-linked widgets unchanged |
| Filter options | Static + query-backed + chaining (`depends_on`) |
| Interpolation | Type-aware, server-side, escaping by declared variable type |
| Execution identity | As the viewer (managed CH per-user; unmanaged needs connector `use`) |
| Cost control | Client debounce + per-user short-TTL Redis cache; no persistence |
| Filter UX | Dashboard-level variables in a fixed top bar; input widgets migrate 1:1 |
| Cell-linked widgets | Snapshot-only, unaffected by variables; "Convert to query widget" action |
| Public dashboards | Opt-in live mode per dashboard + rate limits; runs as creator identity |
| Architecture | Approach 1: extend widget rows, variables in `dashboards.settings` JSONB, shared `runQuery` helper, per-widget execute endpoint |

## Data model (migration `V121`)

Latest migration is `V120__warehouse_hidden_table_patterns.sql`, so this lands as
`V121`.

### `widgets` table

Add nullable columns:

```sql
ALTER TABLE widgets
    ADD COLUMN connector_id UUID NULL REFERENCES connectors(id) ON DELETE SET NULL,
    ADD COLUMN query TEXT NULL,
    ADD COLUMN language TEXT NOT NULL DEFAULT 'sql';
```

A widget is either cell-linked or query-backed:

```sql
ALTER TABLE widgets
    ADD CONSTRAINT widgets_source_check CHECK (
        NOT (notebook_id IS NOT NULL AND connector_id IS NOT NULL)
    );
```

- Cell-linked: `notebook_id` + `cell_id` (unchanged; renders cached outputs).
- Query-backed: `connector_id` + `query` (+ `language`).
- `config` JSONB keeps visualization options exactly as today.

### `dashboards.settings`

The variables list lives in `settings.variables`; the dead
`parameter_overrides` key is dropped:

```jsonc
{
  "auto_refresh_seconds": 30,
  "grid_cols": 12,
  "query_cache_seconds": 30,     // 0 disables server cache
  "public_live": false,          // anonymous visitors may run queries
  "variables": [
    {
      "name": "region",          // [a-zA-Z0-9_-]+, referenced as {{region}}
      "label": "Region",
      "type": "single_select",   // text|number|boolean|date|date_range|single_select|multi_select
      "default": "EMEA",         // type-dependent: string|number|bool|ISO date|[start,end]|string[]
      "required": false,
      "options": {               // select types only
        "mode": "query",         // "static" | "query"
        "values": [{"label": "EMEA", "value": "EMEA"}],
        "query": {
          "connector_id": "…",
          "sql": "SELECT region AS value, region AS label FROM sales GROUP BY 1"
        },
        "label_column": "label",
        "value_column": "value"
      },
      "depends_on": ["country"]  // re-run options query (debounced) when these change
    }
  ]
}
```

Static option values may be plain strings (`["EMEA", "AMER"]`) or
`{label, value}` pairs; they are normalized to pairs on read.

### Input widget migration

The SQL migration converts every widget of type `date_picker`, `date_range`,
`freetext`, `number`, `multi_select` into a variable in its dashboard's
`settings.variables`, using the existing `config.paramName` (fallback: widget id),
`label`, `placeholder`, and `options`. The converted widget rows are deleted, and
the type `CHECK` shrinks to `chart|table|text|metric`. `date_range` migrates to a
`date_range` variable; `multi_select` keeps its static options.

### Permission seed

`handleCreateDashboard` (`internal/api/dashboard_handlers.go`) additionally seeds
the creator with `view_with_data` so authors can see their own live widgets.
Existing dashboards are not backfilled; `edit` allows using the editor and org
admins bypass ACLs.

## Variables & interpolation spec

Interpolation happens **server-side only**; the browser sends raw variable values.
A new type-aware interpolator lives outside `executor.ResolveParams` (which stays
raw for notebooks) so that declared variable types drive escaping.

Syntax: `{{name}}` with optional internal whitespace. Every token must resolve to
a declared variable; otherwise the request fails 400 naming the token. Unlike
notebook SQL there is no slug-ref fallback — dashboards have no sibling cells.

| Type | Format rule | Example input → SQL |
|---|---|---|
| `text` | escaped quoted literal (`'` → `''`) | `O'Brien` → `'O''Brien'` |
| `single_select` | escaped quoted literal | `EMEA` → `'EMEA'` |
| `date` | validate ISO-8601/RFC3339, quoted | `2026-01-01` → `'2026-01-01'` |
| `number` | validate numeric (reject NaN/Inf) | `42.5` → `42.5` |
| `boolean` | literal | `true` → `true` |
| `multi_select` | escaped parenthesized list | `["EMEA","AMER"]` → `('EMEA','AMER')` |
| `date_range` | exposes two tokens `name_start` / `name_end` | `[a,b]` → quoted `'a'` / `'b'` |

Details:

- `multi_select` is intended for `WHERE region IN {{region}}`; an empty optional
  selection renders `(NULL)`, an empty required selection is a 400.
- Unknown tokens in query widgets are errors, not slug refs. The editor flags them
  live and offers "Define variable".
- Non-SQL widget languages skip interpolation (language is stored for future use;
  v1 executes SQL only, matching `handleExecuteCell`).
- "Convert to query widget" resolves notebook slug refs and named params at
  conversion time (server-side) before copying SQL into the widget.

## Backend execution & APIs

### Shared execution core

Extract the body of `handleExecuteCell` (connector load → permission checks →
`resolveExecutionTarget` → limits → execute → error mapping) into a reusable
`runQuery` service (`internal/api/query_runner.go`). Cell execution keeps raw
`ResolveParams` + slug resolution; dashboard execution passes already-interpolated
SQL. Both share result serialization, routing metadata, and audit conventions.

### Endpoints

Authenticated runs require dashboard `view_with_data`:

1. `POST /api/v1/dashboards/{id}/execute`
   - Body: `{ "widget_id": "…", "variables": { "name": value }, "bypass_cache": false }`
   - Server: verifies the widget belongs to the dashboard, validates + interpolates
     variables, runs as the viewer, returns
     `{ outputs, metrics, routing?, cached: bool, cache_expires_at? }`.
   - Errors follow cell-execute conventions: 400 unknown/invalid variable,
     403 no access, 409 `service_choice_required`, 503 warehouse not ready,
     422 query error.
2. `POST /api/v1/dashboards/{id}/variables/{name}/options`
   - Body: `{ "variables": { parent values } }` → `{ "options": [{label, value}] }`.
   - Runs the variable's option query through the same path on load and when a
     `depends_on` parent changes.
3. Public: `POST /api/v1/public/{token}/execute` and
   `POST /api/v1/public/{token}/variables/{name}/options`.
   - Enabled only when `settings.public_live == true`.
   - No viewer identity: managed ClickHouse runs as the dashboard creator;
     unmanaged connectors use the stored credential (no `use` check).
   - Rate-limited per token+IP via the existing rate limiter.

### Caching

Redis via `internal/cache`:

- Authenticated key: `sha256(org | viewerID | connectorID | resolved SQL | limits)`.
- Public key: `sha256(token | connectorID | resolved SQL)`.
- TTL: `settings.query_cache_seconds` (default 30; 0 disables). Only successful
  results are cached. Manual Refresh / Run all send `bypass_cache: true`.
- A nil Redis client skips caching (same pattern as warehouse invalidation),
  keeping tests and minimal deployments working.

### Limits & lifecycle

- Reuse connector `timeout_seconds`, `max_rows`, and org `cell_output_max_bytes`.
- Option queries additionally cap at 1,000 options and a short timeout.
- Client aborts in-flight requests when values change (`AbortController`); the
  server honors `r.Context()` cancellation.
- Frontend fan-out is limited to ~4 concurrent widget requests.

### Audit

`dashboard.query` and `dashboard.variable_options` events mirror `cell.execute`
fields: dashboard, widget, connector, identity, row count, duration, cache hit.

## Permissions & security

- Query widgets are hidden / non-runnable for viewers without `view_with_data`;
  cell-linked widgets render cached outputs as today for `view`.
- Authenticated runs execute as the viewer through `resolveExecutionTarget`; a
  viewer without `use` on an unmanaged connector gets 403 (tile-level error).
- Interpolation is escaping-only: no raw formatter means `{{var}}` can never
  become an identifier or SQL fragment.
- Cache keys include viewer identity for authenticated runs, so cached rows can
  never cross users.
- Public live mode is explicit opt-in per dashboard, rate-limited, and bounded by
  the authored queries; anonymous users cannot author SQL.

## Frontend UX

### Editor (`DashboardEditorPage.tsx`)

- Adding a widget chooses a **source**: notebook cell or query.
- Query widgets get a connector picker and CodeMirror SQL editor (reusing the cell
  editor), debounced save, and an explicit **Run** preview rendering through the
  existing `OutputRenderer` / `ChartView`.
- A config drawer holds source, detected `{{variables}}` (with "Define variable"
  shortcuts), and existing chart config.
- A **Variables** panel manages name/label/type/default/required, static or
  query-backed options (connector + SQL + label/value columns), and `depends_on`.
- Cell-linked widgets get **Convert to query widget**.
- Widget picker offers `chart | table | text | metric`.

### Viewer (`DashboardPage.tsx`)

- Variable bar under the title: dropdown, multi-select chips, text, number, date,
  date range. Changes auto-run affected widgets debounced ~300 ms (free text on
  Enter/blur); manual Refresh bypasses cache; existing auto-refresh selector stays.
- Values sync to the URL query string (`?region=EMEA&from=…`) with precedence
  URL → localStorage per dashboard → variable default.
- Per-widget lifecycle: skeleton, tile-local error + Retry, truncation indicator,
  independent requests.
- Cell-linked widgets behave exactly as today ("Run all" re-runs cells only).

### Public (`PublicDashboardPage.tsx`)

- Variable bar is interactive when `public_live` is on; otherwise query widgets
  are hidden as today.

## Error handling summary

- 400 invalid/unknown variable → highlight the offending control.
- 403 → tile message ("No access to connector").
- 409 `service_choice_required` → reuse the existing service-choice dialog, then
  retry.
- 422 query error → tile-local message + Retry.
- Cancellation never renders an error.
- Option-query failures render inline retry in the variable control.

## Testing

- Unit: interpolator (escaping `O'Brien`, number/boolean/date validation,
  multi-select list, empty optional vs required, `_start`/`_end`, unknown token,
  whitespace), variables JSON validation.
- Go handler tests against the real DB: run as viewer with/without `use`, 409
  routing, public-live on/off, cache hit/miss (nil and real cache paths), and a
  migration fixture proving input widgets become variables.
- Frontend Vitest: variable bar controls, URL/localStorage precedence, drawer
  `{{var}}` detection.
- Playwright `e2e/dashboard-filters.spec.ts`: create → define variable → run →
  change filter → widget updates; public toggle.
- Real-browser validation with agent-browser is mandatory before merge.
- PR checklist: `task check`, `cd web && npx tsc --noEmit && npm run build`,
  E2E; relay unchanged.

## Migration & rollout

- `V121` is additive: nullable `widgets.connector_id`, `widgets.query`,
  `widgets.language`; source CHECK; input-widget conversion; type CHECK shrink;
  `settings.parameter_overrides` removed.
- Existing cell-linked dashboards, layouts, and public tokens keep working.
- No changes required to the relay or notebook execution paths.

## Out of scope / future

- Server-side batch run endpoint and WS result streaming (can layer on top of the
  per-widget endpoint without a data-model change).
- Persisted last-good snapshots (stale-while-revalidate, public fallback).
- Real `metric` / `text` renderers.
- Query library / cross-dashboard query reuse.
- Raw/identifier interpolation escape hatch.
- Per-widget variable scoping.
