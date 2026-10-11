# Data Apps — Design

**Date:** 2026-10-11
**Status:** Approved (design)
**Branch:** `feat/data-apps-design`

## Summary

Turn dashboards into **data apps**: a first-class, full-bleed presentation surface
("App view") plus the building blocks that make a dashboard read like a finished
product — real `metric` and `text` widgets, per-widget appearance, tabs, org and
dashboard theming, a draft → publish split, and two gated escape hatches
(sanitized HTML/CSS panels; sandboxed custom components and full custom pages).

The execution layer already exists — query widgets, dashboard variables with
type-aware interpolation, per-viewer warehouse identities, Redis query caching,
public live mode with rate limits, and the `view` / `view_with_data` split. This
program is therefore almost entirely **presentation**, delivered in four waves.
The App surface's composition — **The Story** — was shaped and locked in an
Impeccable pass on 2026-10-11 (see "The App surface" below and Appendix B).

## Context

What exists today (after `V121` and the #244 dashboard UX overhaul):

- Widgets own SQL + connector, execute as the viewer through `openQuery` /
  `resolveExecutionTarget`, are Redis-cached (`settings.query_cache_seconds`),
  and honor connector limits and org output caps.
- Dashboard variables: `text | number | boolean | date | date_range |
  single_select | multi_select`, query-backed options with `depends_on`
  chaining, server-side escaping-only interpolation, URL-synced values.
- Public dashboards: `public_tokens`, `settings.public_live` opt-in, rate limits
  (60/min execute, 120/min options per token + IP), runs as the creator.
- Widget types `chart | table | text | metric`; `text` and `metric` have no
  dedicated renderers — they fall through to table/chart.
- 13 ECharts chart types with per-widget config overrides
  (`config_edited_from_dashboard`) and notebook-config source of truth.
- The editor is dense instrument chrome (meta bars, reset/close controls, grid
  editor); the viewer reuses the same card composition. No embed, no
  presentation mode, no theming, no draft/publish, no escape hatches.

Research basis: a deep dive of Hex.tech Apps (classic + generative) and
comparables — Streamlit, Superset, Metabase, Grafana, marimo, Retool,
ThingsBoard, DashboardFox. The pattern that wins: **curated presentation layer +
sandboxed escape hatch + interactivity "for free" on standard widgets**.
Findings summarized in Appendix A.

## Goals

- An App view that serves non-technical stakeholders: full-bleed, chrome-free,
  answer-first, freshness visible, shareable.
- Real `metric` and `text` widgets; per-widget appearance; table column
  formatting.
- Tabs and a draft → publish split so viewers never see work-in-progress.
- Org + dashboard theming for app surfaces only.
- Escape hatch tier 1: sanitized HTML/CSS panels (script-free).
- Escape hatch tier 2: sandboxed custom components and full custom pages fed by
  the existing execution pipeline.
- Distribution: dashboard embedding, presentation mode, print/PDF.
- Agent tooling extended alongside each wave.

## Non-goals

- No new execution model: no server-side batch runs, result streaming, or
  persisted result snapshots in waves 1–3.
- No website-builder scope; no nested container sections (tabs + headings cover
  grouping).
- No arbitrary dashboard-wide CSS (tokens + panels only).
- No signed embedding / pass-through auth in this program (noted as future).
- No raw HTML in the `text` widget in wave 1.

## Decisions

| Question | Decision |
|---|---|
| Where does app config live? | `dashboards.settings.app` (+ `settings.tabs`, `settings.app.theme`) — additive JSONB, no new columns |
| App frame | Full-bleed, chrome-free; the editor keeps the shell |
| Primary moment | Glanceable status (answer-first) |
| App composition | **The Story** (locked via Impeccable surface roll; Board/Stage documented alternates) |
| Widget kinds | `metric` + `text` become real; `html` (wave 3), `custom` (wave 4) added to the type CHECK |
| Metric math | Client-side over the returned rows; comparison via a second column the author's SQL produces; no hidden second query |
| Draft/publish | `dashboard_versions` snapshot table; viewers render latest published (fallback: live state); execute accepts `published: true` and loads definitions server-side from the snapshot |
| Tabs | `settings.tabs` + `widgets.layout.tab` |
| Theming | Org `OrgSettings.branding` + dashboard `settings.app.theme`; app surfaces only; curated self-hosted fonts |
| Escape hatch 1 | `html` widget in a script-free sandboxed iframe + DOMPurify + escaped client-side `{{variable}}` interpolation |
| Escape hatch 2 | `custom` widget in a sandboxed iframe (`allow-scripts`, no `allow-same-origin`), strict CSP, postMessage data bridge through the existing execute pipeline; org flag `custom_code_enabled` default off |
| Full custom page | `settings.app.layout: "grid" \| "custom"`; custom mode = one full-bleed custom widget, all other widgets data-only |
| Reading type role | Recommend JetBrains Mono with tabular figures (~28–36px) for KPI values — **open for confirmation** |
| Build path | Code-led (no image generation in the shaping session); the ambition lives in the first viewport and the lede |

## Data model

### `dashboards.settings` (additive)

```jsonc
{
  "variables": [...],           // existing
  "auto_refresh_seconds": 30,   // existing
  "grid_cols": 12,              // existing
  "query_cache_seconds": 30,    // existing
  "public_live": false,         // existing

  "app": {                      // wave 1
    "title": "Q3 Revenue Review",
    "subtitle": "Sales ops · updated hourly",
    "logo_attachment_id": null,
    "width": "fixed",           // "fixed" | "full"
    "layout": "grid",           // "grid" | "custom" (wave 4)
    "show_header": true,
    "show_refresh": true,
    "show_last_updated": true,
    "theme": { ... }            // wave 2
  },
  "tabs": [ { "id": "overview", "title": "Overview" } ]  // wave 2
}
```

Settings remain a JSONB bag; unknown keys are dropped by the struct round-trip
today, so new keys are additive and safe. No new tables for waves 1 and 3.

### `widgets` (additive)

- `type` CHECK widens over time: `chart | table | text | metric` →
  `+ html` (wave 3) → `+ custom` (wave 4).
- `config` schemas per type (wave 1):

```jsonc
// metric (query-backed)
{ "metric": {
    "value_column": "revenue",
    "aggregation": "sum",            // none|sum|avg|min|max|median|count|count_distinct|first
    "label": "Total revenue",
    "format": { "style": "currency", "currency": "USD", "decimals": 0, "compact": true },
    "comparison": { "column": "revenue_prev", "mode": "percent" },  // optional
    "sparkline": { "x_column": "day", "y_column": "revenue" }       // optional
} }

// text (source-less allowed today by the V121 source CHECK)
{ "text": { "markdown": "## {{region}} overview\n\nWhat changed this period…" } }

// appearance (any widget)
{ "appearance": { "background": "card", "border": true, "padding": "md", "align": "left" } }

// table (column formatting)
{ "table": { "columns": [ { "name": "revenue", "format": { "style": "currency", "currency": "USD" } } ] } }
```

- `layout` gains an optional `tab` field (wave 2).

### `dashboard_versions` (wave 2, new table)

```sql
CREATE TABLE dashboard_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id UUID NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    version INT NOT NULL,
    settings JSONB NOT NULL,
    widgets JSONB NOT NULL,          -- full widget definitions incl. query/connector/config
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (dashboard_id, version)
);
```

Publish snapshots `settings` + every widget definition. Viewers and public pages
render the latest version when one exists, otherwise the live state (so existing
dashboards are unchanged until first publish). Execute and variable-options
endpoints accept `published: true` and load definitions **server-side from the
snapshot by widget id** — the client never supplies SQL in any mode. Version
history: list + restore (restore copies a snapshot back into the draft;
publishing stays explicit). Audit `dashboard.publish`.

## The App surface — The Story (locked direction)

Shaped in an Impeccable `shape` pass on 2026-10-11; the decision page locked
**The Story** (surface roll key `1ba38f7f`, question key `33aa5741`; hand at
`.impeccable/mocks/decision/hand.json`). Board and Stage remain documented
alternates (Appendix B).

**Thesis.** The app reads as a written brief: a lede that says what changed,
numbers as punctuation, evidence in chapters below. It refuses the category
default — a uniform KPI row over a wall of equal cards with no statement.

**First viewport.** Masthead (title, subtitle, freshness) → quiet filter strip →
the written lede stating what changed, with KPI readings beside it → full-width
hero chart, the next section's heading already in view.

**World.** The Observatory, fixed: dark frame where chrome exists, light canvas,
hairlines, flat surfaces, amethyst ≤10%, DM Sans for humans, JetBrains Mono for
data and instrument labels.

**Signature move.** The lede — authored text at reading size carrying the
finding, with readings set as instrument numbers beside it.

**States.** First-run/empty (teach, don't scold); per-widget skeletons with no
layout shift; filtered-zero (name the filter that emptied it); tile-local errors
with retry; freshness (cached vs live distinguished); `view`-only viewers get
cached evidence without dead tiles; public/read-only; dark/light; org token
overrides.

**Interaction.** Vertical editorial flow; tabs act as chapters when present;
filters quiet but always reachable; standard web controls only; 0.15s
state-only motion; focus = 2px amethyst outline; single column under 1024/767
with the lede above the fold on mobile; tap targets ≥36px.

**Durable system changes to record in DESIGN.md at build finish:** the reading
type role (metric values), the widget appearance vocabulary
(card/transparent/flush, padding, alignment), and the app-surface theming token
set.

## Wave 1 — App mode + widget kit + light theming

### Backend

- `settings.app` is accepted and validated by `handleUpdateDashboard` (bounds
  and enum validation only; unknown keys dropped as today). No new tables.
- No execution changes: `metric` and `text` are renderer choices over the
  existing `POST /dashboards/{id}/execute` result. `text` widgets may be
  source-less (allowed by the V121 source CHECK); `metric` widgets are
  query-backed like `chart`/`table`.

### Metric widget

- Client-side over returned rows: `aggregation` (`none` → first row value;
  `sum | avg | min | max | median | count | count_distinct | first`), `format`
  (number / currency / percent, decimals, compact `1.2M`), optional
  `comparison` (a second column the author's SQL produces; `delta` or
  `percent`, colored with semantic tokens and a direction icon), optional
  `sparkline` (x/y columns, a minimal ECharts instance).
- The config UI documents the convention: aggregate in SQL for large data; the
  client aggregates over the returned rows (bounded by output limits).
- Threshold colors are deliberately deferred.

### Text widget

- Markdown via the existing renderer (**no raw HTML in wave 1** — no
  `rehypeRaw`), so interpolation can never become a rendering surface.
- `{{variable}}` interpolation client-side from `DashboardVariablesContext`,
  values inserted as plain text; headings and dividers come free via markdown.
- Appearance applies, making the text panel the "blend into the page" workhorse.

### Appearance + table formatting

- Appearance on every widget: `background: card | transparent`, `border`,
  `padding: none | sm | md`, `align`. Defaults preserve today's card look.
- Table column formatting: per-column number/date/currency spec applied in
  `OutputRenderer`'s table.
- Reading type role (open): metric values at ~28–36px; recommended JetBrains
  Mono with tabular figures, comparison deltas in semantic color.

### Viewer / editor UX

- `/dashboards/:id/view` becomes the App surface: chrome-free header driven by
  `settings.app` (title, subtitle, logo via attachments, width, refresh,
  last-updated), the variable bar as a quiet filter strip, widgets composed per
  The Story.
- Editor gets **"Preview as viewer"**; the public dashboard page reuses the same
  renderer (with `public_live` gating data as today).
- Permissions unchanged: `view` sees structure and cached cell outputs;
  `view_with_data` unlocks live queries.

## Wave 2 — Composition & publish

### Tabs

- `settings.tabs: [{ id, title }]`; `widgets.layout.tab` (default: first tab).
- Viewer and public pages get a tab bar under the masthead; the editor gets
  per-widget "move to tab" plus drag-between-tabs. No nested containers.

### Draft → publish

- Editor shows Publish + an unpublished-changes indicator (content hash of
  settings + widgets vs the latest version).
- Publish snapshots into `dashboard_versions` (audit `dashboard.publish`).
- Viewers/public render the latest version; no version → live state.
- `POST /dashboards/{id}/execute` and `/variables/{name}/options` accept
  `published: true` and load definitions from the snapshot. Permission checks
  and viewer identities are identical in both modes.
- Version history: list + restore (into the draft).
- Known limitations (documented): cell-linked widgets render the notebook's
  current cell outputs (cells are not versioned); query widgets still require
  `view_with_data` or `public_live`; persisted last-good snapshots are future.

### Theming

- Org: `OrgSettings.branding` — `app_name`, `logo_attachment_id`,
  `accent_color`, `default_chart_palette`.
- Dashboard: `settings.app.theme` — `accent`, `background`, `surface`, `text`,
  `border`, `radius`, `font_family` (curated, self-hosted), `chart_palette[]`,
  `color_scheme: auto | light | dark`.
- Applied as scoped CSS variables on the app container only (editor unaffected);
  charts read tokens through the existing theme plumbing; contrast guardrails
  fall back to defaults for invalid pairs. Org font upload is future; fonts are
  a curated, self-hosted set (no third-party font requests).

## Wave 3 — HTML/CSS panel (escape hatch, tier 1)

- New widget type `html`, config `{ html, css }`.
- Rendered in a **script-free sandboxed iframe** (`sandbox=""`): scripts, forms,
  navigation, and same-origin access disabled; CSS is naturally isolated.
- The HTML is run through **DOMPurify** (strict allowlist) before injection —
  defense in depth (new dependency, approved 2026-10-11). Images only from
  `data:` URIs and same-origin attachments; no third-party network.
- `{{variable}}` interpolation client-side with HTML-escaped values.
- No admin gate (script-free); an org-wide disable switch exists. Public apps
  render panels (static).
- Deliberate boundary: **no dashboard-wide arbitrary CSS** — whole-app restyling
  is the theme-token system; bespoke sections are panels. Arbitrary dashboard
  CSS stays a future, admin-gated item.

## Wave 4 — Custom components & pages (escape hatch, tier 2)

- New widget type `custom`, config `{ html, css, js, bindings: [{ key, widget_id }] }`.
- `SandboxedFrame` with `sandbox="allow-scripts"` and **no**
  `allow-same-origin`: opaque origin, no cookies/localStorage, no authenticated
  fetch. CSP injected into the frame blocks all outbound network; inline scripts
  only; a size cap (~256 KB) applies to the stored bundle.
- **Data bridge (postMessage only).** Frame → host: `ready`, `request_data`,
  `open_link` (host-validated). Host → frame: `init` (variables, theme tokens,
  bound data), `data`, `error`. The host executes each bound widget through the
  existing dashboard execute pipeline **as the viewer** (per-viewer identity,
  `view_with_data`, interpolation); the server validates that bindings belong to
  the dashboard. The component never sees credentials or tokens.
- **Full custom page.** `settings.app.layout: "custom"`: the dashboard renders
  one full-bleed custom widget; every other widget becomes a data source only —
  hidden in the app view, visible in the editor with a "data source" badge.
- **Governance.** Org setting `custom_code_enabled` (default **off**, admin
  enables); audit on create/update; data pushes require `view_with_data`; public
  apps feed data only when `public_live` is on.
- **Authoring.** CodeMirror HTML/CSS/JS modes + live preview + binding picker.

## Distribution

- **Dashboard embedding**: `/embed/dashboards/:token` iframe-friendly route +
  Embed tab in the share modal (today only notebook cells embed).
- **Presentation mode**: `/dashboards/:id/present`, full-screen chrome-free app
  view borrowing `PresentationPage` patterns; slides are future.
- **Print/PDF**: print stylesheet on the app surface → browser print-to-PDF.
  Native export is future.
- **Future (explicitly deferred)**: signed embedding with pass-through auth and
  row-level security; scheduled snapshots (natural home: `dashboard_versions`);
  saved views.

## Agent tooling (threaded through every wave)

- Wave 1: extend `create_dashboard_widget` / `update_dashboard_widget` to
  query-backed widgets with metric/text configs (create is cell-linked only
  today); add `update_dashboard_settings` for the app block.
- Wave 2: `set_dashboard_layout` (tabs, placement, appearance) and
  `publish_dashboard`; the agent reads org theme tokens.
- Wave 3: author HTML panels.
- Wave 4: `create_custom_widget` behind the org flag — agent-written code lands
  in the draft for human review before publish.
- The culmination — "describe the app, get the app" — is demonstrable after
  wave 2 and improves through wave 4.

## Permissions & security

- The existing model is unchanged: `view` vs `view_with_data`, per-viewer
  execution identities, connector `use`, `public_live` opt-in + rate limits.
- Published mode performs identical permission checks; definitions come from the
  snapshot server-side.
- Custom code: opaque-origin sandbox, no network, no credentials, data only via
  the validated bridge; org flag default off; audit trail.
- Note (out of scope, flagged for follow-up): notebook/presentation/embed
  markdown surfaces use `rehypeRaw` without a sanitizer today; this program does
  not change them, but the sanitizer added in wave 3 makes fixing that cheap.

## Testing

- Go: settings validation; publish/versioning; published-mode execute (snapshot
  definitions, identical permission checks); variable options in published mode;
  binding validation (must belong to the dashboard); org-flag gating; audit
  events.
- Vitest: metric math/formatting, text interpolation escaping, appearance
  application, theme token resolution + contrast guardrails, tab logic,
  `SandboxedFrame` protocol (happy path + malformed messages).
- Playwright: app mode, metric/text, tabs, publish → viewer sees the snapshot,
  HTML panel, custom component bridge + isolation assertions (no external
  network, no cookies), embed route, public gating, theming.
- Real-browser validation is mandatory per repo policy; `task check`,
  `tsc --noEmit`, `npm run build`, `npm run test:run`, `task test:e2e`.

## Migration & rollout

- Migrations are additive: widen the widget type CHECK (wave 3/4), add
  `dashboard_versions` (wave 2). No backfills; no behavior change for existing
  dashboards until a dashboard publishes or opts into new settings.
- Custom code ships behind `custom_code_enabled` (default off).
- Public dashboards keep working unchanged; `public_live` semantics are
  untouched.

## Open decisions

1. **Reading type role** — JetBrains Mono tabular (~28–36px) vs DM Sans. The
   recommendation is mono (rule-aligned; today's BigNumber sans is a chart
   type, not a system role).
2. **Freshness semantics** — show client cache age, server executed-at, or
   both (the widget footer already reports executed-at today).
3. **Lede authoring** — confirm the lede is authored in the dashboard `text`
   panel (and later agent-drafted).
4. **Published snapshot surfacing** — how the app tells viewers/editors which
   version they are seeing (e.g., an editor-side "viewing published vN" chip).
5. **Custom component limits** — size cap value and allowed URL schemes for
   images/links.

## Out of scope / future

- Signed embedding with pass-through auth + RLS attributes.
- Scheduled snapshots and dashboard schedules.
- Saved views; persisted last-good results.
- Arbitrary dashboard-wide custom CSS; nested container sections.
- Org font uploads; native PDF export.
- Cross-filtering / drill-down interactions beyond dashboard variables.

## Appendix A — Research summary (2026-10-11)

| Product | Model | Takeaway |
|---|---|---|
| Hex.tech Classic Apps | Curated cells, auto-layout → drag-drop grid, tabs, input params, versioned publish, presentation mode, themes, signed embedding | The bar to match; notebook = engine, app = storefront |
| Hex.tech Generative Apps | Agent writes app code (`App.js` + styles), `design.md` tokens, sandboxed iframe, no network, data via one controlled channel | The escape-hatch model for custom code; inherits permissions automatically |
| Streamlit | Custom components = iframe + postMessage; token theming incl. chart palettes | Component-bridge pattern; theme-as-tokens |
| Superset 6 | Design-token themes per dashboard; CSS templates as escape hatch | Tokens first, scoped CSS as fallback |
| Metabase | Markdown-only text cards; custom viz = sandboxed JS bundles, disabled in public embeds | Trust tiers; fallbacks |
| Grafana | Text panel markdown / sanitized HTML + `{{variables}}`; unsanitized behind a flag | Sanitize by default |
| marimo | Notebook doubles as app; vertical, grid, slides layouts; anywidget custom elements | Closest OSS analog |
| Retool / ThingsBoard / DashboardFox | Components + JS escape hatches; `${data}` templating + scoped CSS; per-tenant branding policies | Templated panels; org-scoped theming precedent |

Full findings in the session record; product pages: `hex.tech/product/data-apps`,
`learn.hex.tech/docs/share-insights/apps/*`, `learn.hex.tech/docs/share-insights/apps/generative-apps`.

## Appendix B — Composition alternates (documented, not built)

- **The Board** — instrument wall: masthead, filter strip, four KPI tiles, 2×2
  evidence grid, no scrolling on desktop. Most familiar; risk: generic without
  tile discipline.
- **The Stage** — presentation canvas: fixed-aspect composition scaled to the
  viewport; title card + headline reading, dominant chart, supporting strip,
  overlay control dock. Best for meetings/embeds; risk: poor daily-laptop fit.

Both remain available as future variants or per-app layout choices; the App
surface builds The Story first.
