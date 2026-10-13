# App Builder — Design

**Date:** 2026-10-12
**Status:** Approved (design)
**Branch:** `feat/app-builder` (planned)

## Summary

Turn the app-builder surfaces shipped in waves 1–2 into a **self-teaching, agent-assisted**
experience. Four workstreams:

1. **Per-chapter filter scoping** — filters are inferred from what a chapter's widgets actually
   use (queries + text panels + transitive option dependencies), with one toggle between
   "all filters everywhere" and "only this chapter's filters".
2. **Agent tooling** — typed widget-config authoring (`set_widget_config`), a cohesion review
   tool (`review_dashboard`) with preview links, a `dashboard_guide` knowledge tool, and a
   refresh of every stale tool description (including the mechanism that lets existing orgs
   receive description updates at all).
3. **A cohesion review engine** — one set of heuristics consumed by both the agent tool and a
   new editor-side **App health** panel.
4. **A human UX guidance pass** — every new app-builder surface teaches itself: empty states,
   the add-widget picker, the config drawer, the App settings panel (app/tabs/theme/filter
   scope), publish/versions, org branding — via a single field-help source shared with the
   agent's guide, crafted through an Impeccable pass.

## Context

Waves 1–2 (`docs/plans/2026-10-11-data-apps-design.md`) shipped the App surface (The Story),
the widget kit (`metric`/`text`, appearance, table column formats), chapters
(`settings.tabs` + `widgets.layout.tab`, per-chapter grids), draft → publish
(`dashboard_versions`, published reads, draft preview), org + dashboard theming, and the first
agent tools (`update_dashboard_settings`, `set_dashboard_layout`, `publish_dashboard`,
query-backed `create_dashboard_widget`/`update_dashboard_widget`).

Reviewing the result exposed five gaps:

- **Filters are dashboard-global.** One strip, one namespace, interpolated everywhere; chapters
  cannot scope them.
- **Agents cannot author widget configs.** `create_chart` speaks snake_case and targets notebook
  cells only; a query-backed widget stores the camelCase `ChartConfig` (plus snake_case
  `metric`/`table` blocks) and `update_dashboard_widget`'s `config` merge is blind — the agent
  would have to guess key names and shapes.
- **No cohesion assessment.** Nothing can tell an author (human or agent) that a chapter is
  empty, widgets overlap, a metric lacks a label, or the theme is unreadable.
- **Stale tool descriptions never refresh.** Builtin tools are seeded per org with
  `ON CONFLICT DO NOTHING` and seeding skips once the count matches, so *changed* descriptions
  and parameters never reach existing orgs (only newly added tools arrive).
- **Onboarding is thin** for the new types — "what is a metric, and what config does it expect?"
  is answerable neither from the tools nor from the UI.

## Goals

- Chapter-scoped filters that maintain themselves from usage, behind one explicit toggle.
- Typed, validated widget-config authoring for agents across all 13 chart types and the
  metric/table blocks.
- A cohesion review engine with prioritized, actionable findings, shared by the agent tool and
  an editor health panel.
- A builder guide for agents and a refresh path for every tool description.
- Every new app-builder surface teaches itself for humans, from one field-help source.

## Non-goals

- Screenshots / vision for the agent (deliberate future item; the report + preview links close
  the loop for now).
- Per-chapter variable *definitions* (independent sets per chapter) — inference only.
- An org-forkable guide-as-skill (the skill route is documented but not built).
- MCP allowlist additions for the new tools (existing follow-up).
- Any change to the execution model (query cache, warehouse routing, public live mode).

## Decisions

| Question | Decision |
|---|---|
| Filter scoping mechanism | **Inference from usage**, not a hand-maintained per-tab field |
| Scope toggle | `settings.app.filter_scope: "all" \| "tab"`, default `"all"` (existing dashboards unchanged) |
| Hidden-but-set filters | A muted "n filters set in other chapters" hint; no reveal control (switch to `"all"` is the escape) |
| Options loading | Only for visible variables under `"tab"` |
| Config authoring | New `set_widget_config` (typed `chart`/`metric`/`table`), snake_case mirroring `create_chart`; `create_dashboard_widget` gains the same optional blocks; server-side conversion + `internal/validate` |
| REST config validation | Unchanged (raw `config` merge stays); typed validation applies to the tool path |
| Cohesion engine | One Go heuristics package; agent tool + REST endpoint for the editor panel |
| Review output | Prioritized findings per chapter, each naming the fixing tool call; plus preview links |
| Guide | `dashboard_guide` tool serving embedded markdown (also committed under `docs/`), on-demand |
| Field help | One structured JSON source (label/hint/example per config field) → Go guide + TS tooltips |
| Builtin descriptions | Platform-managed; seed-revision upsert so existing orgs refresh (a user edit to a builtin is overwritten — documented) |
| Human track scope | **Every** new app-builder surface + a new App health panel |
| Craft process | Impeccable pass over the builder surfaces (shape → build → one bounded critique round) |

---

## A. Per-chapter filter scoping

### Model

`settings.app.filter_scope` (`"all"` default). Under `"tab"` the filter strip shows the
variables **used by the active chapter**; everything else (namespace, URL state, interpolation,
publish snapshots) is unchanged.

### Inference (pure, client-side)

Reuses the live-rerun machinery from #245 (`web/src/utils/dashboardRunSignature.ts`):

1. **Direct refs** — `referencedVariables()` over each of the chapter's widgets: query SQL and
   text-panel markdown (the same `{{token}}` grammar; date-range `_start`/`_end` tokens resolve
   to their variable via `variableTokens()`).
2. **Transitive closure** — for every included variable with `options.mode === "query"`, the
   tokens in its options SQL (and `depends_on` links) pull in their variables; repeat to a
   fixpoint, so a filter is never shown without its upstream filters.
3. **Fallbacks** — widgets with no/unknown `tab` count toward the first chapter (matching the
   render fallback); a chapter that uses nothing renders no strip.

### Behaviors

- Rewriting a query re-scopes the strip automatically — nothing to maintain.
- Values for hidden variables stay in the URL and remain harmless (no widget consumes them).
- Under `"tab"`, options queries load only for visible variables; switching chapters loads
  newly visible ones then.
- A muted hint ("n filters set in other chapters") appears when hidden variables carry
  non-default values.

### Agent + UI surfaces

- The `app` block of `update_dashboard_settings` documents `filter_scope`.
- The App settings panel exposes the toggle with an explanation of the inference.
- The guide (B3) documents the semantics.

---

## B. Agent tooling

### B1. `set_widget_config` (typed authoring)

`set_widget_config { dashboard_id, widget_id, chart?, metric?, table? }` — blocks use the
**same snake_case vocabulary as `create_chart`** (which the agent already knows), converted
server-side:

- `chart` → merged into the widget's `config` as camelCase `ChartConfig` keys
  (`chart_type` → `chartType`, `x_column` → `xAxis`, …); all 13 chart types with their
  type-specific fields, reusing `create_chart`'s parameter schema and description text.
- `metric` → the stored `config.metric` shape (value_column, aggregation, label,
  format {style, currency, decimals, compact}, comparison {column, mode}, sparkline
  {x_column, y_column}).
- `table` → `config.table.columns` entries with `format` specs.

`create_dashboard_widget` gains the same optional blocks so a fully-configured widget can be
created in one call. Validation lives in `internal/validate` (chart type enum, column refs,
hex colors, aggregation/format enums); errors are actionable. `appearance` stays in
`set_dashboard_layout` (no duplication). The REST raw-`config` merge path is unchanged.

### B2. `review_dashboard` (cohesion + preview links)

Returns prioritized findings per chapter, each naming the widget/chapter and the tool call that
fixes it, plus the app URLs (`/dashboards/{id}/view`, `?preview=draft`, and the public link when
sharing is on).

Finding categories (deterministic rules):

- **Layout** — overlapping widgets, out-of-bounds rows/columns, dead gaps, chapter balance
  (widget counts), empty chapters, widgets with no tab.
- **Content** — metric widgets missing a label/format/aggregation sanity, table columns with no
  format on numeric data, text panels without headings, source-less widgets with empty content.
- **Surface** — missing app title/subtitle, theme tokens failing the contrast guardrails,
  `filter_scope` interactions (variables used but never shown), unpublished changes, draft
  projection warnings.

### B3. `dashboard_guide`

An embedded markdown guide (also committed under `docs/`) served on demand: full config shapes
with examples (rendered from the shared field-help source), chapters, filters + scoping,
theming tokens + guardrails, publish/versions semantics, the troubleshooting checklist, and
composition guidance from The Story. No per-request prompt cost.

### B4. Description refresh + seed revision

- Refresh every dashboard-surface tool description to the app-era model (create/update/get,
  `update_dashboard`, `update_dashboard_settings`, `set_dashboard_layout`, `publish_dashboard`,
  `create_dashboard_widget`, `update_dashboard_widget`; `create_chart`'s cell-only framing gets a
  pointer to `set_widget_config`) plus the engine's dashboard hint line.
- **Mechanism:** a `seed_rev` on builtin tool rows; seeding upserts description/parameters when
  the code revision is newer. Builtins are platform-managed; an edit to a builtin is overwritten
  on the next seed (documented in AGENTS.md).

---

## C. Human UX track — every new app-builder surface teaches itself

- **Empty states** — a new dashboard teaches the chapter model (lede, readings, evidence); the
  app view's empty state explains what viewers will see.
- **Add-widget picker** — one-line type descriptors with micro-examples ("Metric — one number
  from a query"; "Text — markdown with live `{{filters}}`").
- **Config drawer** — field-level hints and examples for metric, text, appearance, table, and
  chart fields, via accessible help affordances (`aria-describedby`, keyboard and touch, not
  hover-only).
- **App settings panel** — what the app fields do, what chapters are, theme fields and
  guardrails, and the filter-scope toggle explaining the inference.
- **Publish surfaces** — versions empty state ("publish the draft to create v1"), the meaning of
  "Unpublished changes", and draft-preview vs published.
- **Org branding panel** — what each field affects across apps.
- **App health panel** (net-new surface) — the same heuristics as `review_dashboard`, as a quiet
  editor checklist with jump-to-widget.

**Field-help source:** one structured JSON (label/hint/example per config field). The Go guide
renders hints/examples into its markdown; the UI renders the same strings as tooltips. The exact
import mechanism (Vite import vs a generation step) is settled in the plan.

**Craft process:** an Impeccable pass (shape → build → one bounded critique round) over these
surfaces — quiet-instrument tone, progressive disclosure, no clutter; visual regression where
snapshots exist.

---

## D. Architecture & data flow

- `filter_scope` is plain settings JSONB — carried into publish snapshots like every other app
  setting; inference stays a pure client util.
- One Go heuristics package (working name `internal/dashboardreview`) behind both the
  `review_dashboard` tool and `GET /api/v1/dashboards/{id}/review` (editor-gated) for the health
  panel.
- Typed config validation lives in `internal/validate`, shared by the tool path (and reusable by
  REST later if desired).
- Field help: one JSON source consumed by Go (embed) and TS (import/generate).

## Testing

- **Go:** chart/metric/table validation + conversion; review heuristics (each rule);
  `dashboard_guide` content; seed-rev refresh.
- **TS:** inference (direct, closure, fallbacks), strip scoping + hint, field-help rendering,
  health panel states.
- **E2E:** chapter scoping (strip contents change per chapter; toggle `all`/`tab`), health panel
  findings, guidance presence + accessibility checks.
- **Impeccable:** one bounded critique round on the builder surfaces.

## Migration & rollout

- Additive settings key — no data migration; existing dashboards keep `"all"` behavior.
- Small migration for `seed_rev` on builtin tool rows.
- No behavior change until an author toggles scope or uses the new tools.

## Out of scope / future

- Screenshots/vision for the agent.
- Independent per-chapter variable definitions.
- Org-forkable guide (skill route).
- MCP allowlist additions for the new tools.
- Scheduled snapshots, saved views, embedding (program-level future items).
