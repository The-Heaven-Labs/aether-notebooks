# App Builder — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the app builder self-teaching and agent-assisted: inferred per-chapter filter scoping with one toggle, typed widget-config authoring for agents, a cohesion review engine shared by the agent tool and an editor health panel, a builder guide with refreshed tool descriptions, and a guidance pass over every new app-builder surface.

**Architecture:** Design in `docs/plans/2026-10-12-app-builder-design.md`. Filter scoping is client-side inference (pure TS) over settings JSONB; typed config validation lives in `internal/validate` and converts snake_case agent vocabulary into the stored config shapes; one Go heuristics package (`internal/dashboardreview`) backs both the `review_dashboard` tool and a REST endpoint for the editor panel; one structured field-help source feeds the Go guide and the UI tooltips; builtin tool rows gain a `seed_rev` so description updates reach existing orgs.

**Tech Stack:** Go (`net/http` ServeMux, pgx), React 19 + TypeScript + Vite, Vitest, Playwright, Impeccable (UX craft pass).

**Preconditions:**
- Branch `feat/app-builder` (stacked on `feat/data-apps-wave-2` @ `d22ac711`; rebase onto `main` after PR #247 merges).
- `task infra:up` for Go tests; full dev stack for browser/e2e (`docker compose -f docker-compose.dev.yml up -d`).
- All Go test commands include `-timeout 3m` and run via `bash scripts/go-test.sh <pkgs>`.
- `cd web && npx tsc -b` (never `--noEmit`) + `npm run test:run`.

---

## Phase 1 — Per-chapter filter scoping

### Task 1: `filter_scope` struct-first (Go + TS) + validation

**Files:**
- Modify: `internal/models/dashboard.go`
- Modify: `internal/validate/dashboard_settings.go`
- Modify: `internal/validate/…` tests (add to the existing settings validation tests, wherever `DashboardAppSettings` is covered)
- Modify: `web/src/types/index.ts`

**Step 1: Failing test** — add to the settings validation test file: PUT `settings.app.filter_scope = "tab"` → 200 + round-trips through GET; `"everywhere"` → 400; absent → omitted.

**Step 2:** `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardAppSettings|TestDashboardTabsAndTheme' -count=1 -timeout 3m` → FAIL.

**Step 3: Implement**

```go
// models/dashboard.go — DashboardAppSettings
FilterScope string `json:"filter_scope,omitempty"` // "all" | "tab"; empty = "all"
```

```go
// validate/dashboard_settings.go — inside DashboardAppSettings validation
if app.FilterScope != "" && app.FilterScope != "all" && app.FilterScope != "tab" {
    return fmt.Errorf(`app filter_scope must be "all" or "tab"`)
}
```

`web/src/types/index.ts`: `filter_scope?: 'all' | 'tab'` on `DashboardAppSettings`.

**Step 4:** the targeted run passes; `cd web && npx tsc -b`.

**Step 5:** commit `feat(api): filter_scope app setting (struct-first)`.

---

### Task 2: Chapter-filter inference util

**Files:**
- Create: `web/src/utils/chapterFilters.ts`
- Create: `web/src/utils/chapterFilters.test.ts`

**Step 1: Failing tests** — cover:
- direct refs: a query widget `SELECT … WHERE c = {{country}}` on chapter `threats` → `["country"]`
- text panels: markdown `## {{window}}` counts
- transitive closure: `country` has `options.mode === "query"` whose SQL references `region` → both included; `depends_on` targets included even without options
- fallbacks: widgets with no/unknown `tab` count toward the first chapter; no tabs → every variable; a chapter using nothing → `[]`
- date ranges: `{{window_start}}` resolves to `window`
- unknown tokens ignored; order follows the definitions list

**Step 2:** `cd web && npm run test:run -- src/utils/chapterFilters.test.ts` → FAIL.

**Step 3: Implement**

```ts
// web/src/utils/chapterFilters.ts
import { referencedVariables, variableTokens } from './dashboardRunSignature'
import type { DashboardVariable, Widget } from '../types'
import { widgetTabId, type DashboardTabLike } from './dashboardTabs'

/** Variables referenced by a widget: its SQL plus text-panel markdown. */
function widgetRefs(widget: Widget): string[] {
  const sql = referencedVariables(widget.query)
  const markdown = (widget.config as { text?: { markdown?: string } } | undefined)?.text?.markdown
  return markdown ? [...sql, ...referencedVariables(markdown)] : sql
}

/**
 * The variables a chapter's widgets use, plus everything those variables
 * depend on transitively (options queries, depends_on), so a visible filter
 * never hides the filters that feed it. Order follows `variables`.
 */
export function variablesForChapter(
  widgets: readonly Widget[],
  variables: readonly DashboardVariable[],
  tabs: readonly DashboardTabLike[],
  chapterId: string,
): DashboardVariable[]
```

Rules to implement: build `token → variable` from `variableTokens`; seed from `widgetRefs` of the chapter's widgets; close over `options.query.sql` refs and `depends_on`; map back to definitions in order.

**Step 4:** test passes; `npx tsc -b`.

**Step 5:** commit `feat(web): infer chapter filter scope from widget usage`.

---

### Task 3: Scoped strip on the app surfaces

**Files:**
- Modify: `web/src/contexts/DashboardVariablesContext.tsx` (accept a scoped variable set; load options only for it)
- Modify: `web/src/pages/DashboardPage.tsx`, `web/src/pages/PublicDashboardPage.tsx`
- Modify: `web/src/pages/DashboardPage.test.tsx`, `web/src/pages/PublicDashboardPage.test.tsx`

**Step 1: Failing tests** — with `filter_scope: "tab"` and two chapters:
- chapter A's strip lists only its variables; switching chapters changes the strip
- a muted "n filters set in other chapters" line appears when a hidden variable has a URL param
- options requests fire only for visible variables (MSW: assert the requested names)
- `filter_scope` absent → today's behavior (all variables), pinned by the existing tests

**Step 2:** targeted runs → FAIL.

**Step 3: Implement**

- `DashboardVariablesContext` gains `scope: "all" | "tab"` (default `"all"`) and computes the visible set from the same inference, or receives it as a prop from the pages (prefer: the pages compute `variablesForChapter` and pass `variables={scoped}`; the context keeps the full list for the hint). Keep `persist` touching only scoped names so hidden URL params survive.
- Strip hint: hidden = `variables` − scoped; show the line when any hidden name has a URL param present.
- Public page mirrors the viewer.

**Step 4:** targeted runs pass; `npm run test:run -- src/pages src/contexts`; `npx tsc -b`.

**Step 5:** commit `feat(web): scope the filter strip to the active chapter`.

---

### Task 4: Filter-scope toggle in the App settings panel

**Files:**
- Modify: `web/src/components/AppSettingsPanel.tsx` + test
- Modify: `internal/agent/tools_manage.go` (`update_dashboard_settings` `app` description mentions `filter_scope`)

**Step 1: Failing tests** — the panel renders a "Filters" control (`All filters everywhere` / `Only this chapter's filters`); saving writes `settings.app.filter_scope`; "All" omits the key (default); the control carries a help description.

**Step 2:** targeted run → FAIL.

**Step 3: Implement** — a `Filters` row in the App section with the two options and the inference explained in its hint (uses the shared field help, Task 16). The agent description gains `filter_scope` in the app example.

**Step 4:** targeted run + `npx tsc -b`; browser check: toggle on the showcase → the strip follows the active chapter (record a screenshot).

**Step 5:** commit `feat(web): filter-scope toggle in the app settings panel`.

---

## Phase 2 — Typed widget-config authoring

### Task 5: `internal/validate/widget_config.go` — chart/metric/table validation + conversion

**Files:**
- Create: `internal/validate/widget_config.go`
- Create: `internal/validate/widget_config_test.go`

**Step 1: Failing tests** — table-driven:
- chart: every chart type accepted; unknown type rejected; per-type requirements (`map` needs `lat_column`+`lon_column`, `sankey` needs `x_column`+`y_columns[0..1]`, `funnel` needs `category_column`+`value_column`, `histogram` needs `value_column`); `series_colors` hex-checked; `bar_mode`/`area_mode`/`layout`/`funnel_sort`/`rose_type`/`node_align` enums; `mark_lines` shape
- conversion: `chart_type` → `chartType`, `x_column` → `xAxis`, `y_columns` → `yAxis`, `group_by` → `groupBy`, `show_legend` → `showLegend`, `series_colors` → `seriesColors`, `bar_mode` → `barMode`, … (mirror the exact keys of `web/src/charts/types.ts`; the test asserts the full produced map for a representative config)
- metric: aggregation enum (`none|sum|avg|min|max|median|count|count_distinct|first`), format style enum (`number|currency|percent`), decimals 0–6, compact bool, comparison mode (`delta|percent`), sparkline columns; conversion to the stored `metric` shape
- table: columns non-empty, name required, format style enum, currency hex-free string; conversion to `table.columns`
- empty block → no-op (no keys written)

**Step 2:** `bash scripts/go-test.sh ./internal/validate/ -count=1 -timeout 3m` → FAIL.

**Step 3: Implement** — typed structs mirroring `create_chart`'s parameter names (`WidgetChartConfig`, `WidgetMetricConfig`, `WidgetTableConfig`), `Validate*` functions with actionable errors, and `ToStored*` converters. Keep the conversion table in one place with a comment pointing at `web/src/charts/types.ts` as the stored-shape source of truth.

**Step 4:** tests pass; `gofmt -l`.

**Step 5:** commit `feat(validate): typed widget config validation and conversion`.

---

### Task 6: `set_widget_config` agent tool

**Files:**
- Modify: `internal/agent/tools_manage.go` (ToolDef + handler)
- Modify: `internal/agent/tools_seed.go` (seed entry)
- Modify: `internal/agent/tools_manage_test.go`

**Step 1: Failing tests** — create a query-backed chart widget; `set_widget_config` with a chart block → the doc projection and the materialized row carry the camelCase config; metric and table blocks merge; a bad chart type → error result; the existing config's unrelated keys survive.

**Step 2:** `bash scripts/go-test.sh ./internal/agent/ -run 'TestDashboard' -count=1 -timeout 3m` → FAIL.

**Step 3: Implement** — parameters reuse the Task 5 schema (one Go constant for the chart parameter JSON, shared with Task 7); handler: load/seed doc → validate/convert blocks → merge into the widget's projected config (read-merge, one store) → audit `widget.update`.

**Step 4:** tests pass.

**Step 5:** commit `feat(agent): set_widget_config for typed chart, metric, and table configs`.

---

### Task 7: `create_dashboard_widget` typed blocks

**Files:**
- Modify: `internal/agent/tools_manage.go` + test

**Step 1: Failing test** — creating a widget with `chart`/`metric`/`table` blocks in one call produces the stored config; invalid block → error, nothing written.

**Step 2:** targeted run → FAIL.

**Step 3: Implement** — add the same optional blocks to the create schema (shared constant) and run them through Task 5 before `UpsertWidget`.

**Step 4:** tests pass.

**Step 5:** commit `feat(agent): create fully-configured widgets in one call`.

---

## Phase 3 — Cohesion engine, review tool, health panel

### Task 8: `internal/dashboardreview` heuristics

**Files:**
- Create: `internal/dashboardreview/review.go`
- Create: `internal/dashboardreview/review_test.go`

**Step 1: Failing tests** — one per rule, constructing projections:
- layout: overlap in the same chapter; out-of-bounds (row/col + size beyond `grid_cols`); dead gap above the first row of a chapter; empty chapter; widget with no tab when tabs exist; chapter balance (a chapter with >3× the median)
- content: metric without `label`; metric aggregation `none` with multiple rows? (rule: `none` is fine — flag missing `value_column`); table column without a format when the column is numeric (needs data? — without rows, flag only when a format is absent on a column whose name looks numeric? Keep it: table widget with zero `table.columns` entries); text panel with empty markdown; text panel with no heading
- surface: missing app title; theme contrast failure (reuse a Go port of the WCAG check — `internal/validate` gets `ContrastRatio` or the review package implements it); variables used by no widget while `filter_scope: "tab"`; `has_unpublished_changes` (input flag); projection warnings
- ordering: findings sorted by severity then chapter

**Step 2:** `bash scripts/go-test.sh ./internal/dashboardreview/ -count=1 -timeout 3m` → FAIL.

**Step 3: Implement** — `type Finding struct { Severity, Category, ChapterID, WidgetID, Message, FixTool string }`; `func Review(proj *dashboarddoc.Projection, publishedHash string, draftHash string) []Finding` (pure over the projection + a small options struct); rules as functions; the WCAG check lives here (Go port of `resolveAppTheme`'s contrast math).

**Step 4:** tests pass.

**Step 5:** commit `feat: dashboard cohesion review heuristics`.

---

### Task 9: `review_dashboard` agent tool

**Files:**
- Modify: `internal/agent/tools_manage.go` + seed + test

**Step 1: Failing test** — a dashboard with an empty chapter and an untitled metric returns findings naming the widget/chapter + the fix tool; the result carries `view_url`, `preview_url`, and `public_url` when sharing is on (and omits it otherwise).

**Step 2:** targeted run → FAIL.

**Step 3: Implement** — load/seed doc → `Project` → `dashboardreview.Review` → resolve URLs (settings/`public_tokens`) → JSON result `{findings, view_url, preview_url, public_url?}`.

**Step 4:** tests pass.

**Step 5:** commit `feat(agent): review_dashboard with findings and preview links`.

---

### Task 10: `GET /dashboards/{id}/review` endpoint

**Files:**
- Modify: `internal/api/dashboard_handlers.go` (or a new `dashboard_review.go`) + `internal/api/router.go` + test

**Step 1: Failing test** — editor gets the findings JSON + links; a viewer → 403; a missing dashboard → 404.

**Step 2:** `bash scripts/go-test.sh ./internal/api/ -run 'TestDashboardReview' -count=1 -timeout 3m` → FAIL.

**Step 3: Implement** — `edit`-gated handler sharing the exact assembly with Task 9 (one helper used by both).

**Step 4:** tests pass.

**Step 5:** commit `feat(api): dashboard review endpoint for the editor health panel`.

---

### Task 11: App health panel in the editor

**Files:**
- Create: `web/src/components/AppHealthPanel.tsx` + test
- Modify: `web/src/pages/DashboardEditorPage.tsx` (button + state)

**Step 1: Failing tests** — renders findings grouped by severity; clicking a finding scrolls to/focuses the widget (assert the target element gets focus/highlight); empty state reads "No issues found"; the query refetches when the panel opens.

**Step 2:** targeted run → FAIL.

**Step 3: Implement** — a drawer (existing chrome) with a "Review" button next to the App button; query `['dashboard-review', id]`, `enabled` while open; finding rows show severity, message, chapter, and a "Fix" hint naming the editor surface (e.g. "Configure chart", "Move to chapter").

**Step 4:** targeted run + `npx tsc -b`; browser check (record a screenshot).

**Step 5:** commit `feat(web): app health panel in the dashboard editor`.

---

## Phase 4 — Guide, descriptions, seed revision

### Task 12: `dashboard_guide` tool + guide content

**Files:**
- Create: `internal/agent/guide/guide.md`
- Create: `internal/agent/guide/guide.go` (`//go:embed guide.md`)
- Modify: `internal/agent/tools_manage.go` + seed + test

**Step 1: Failing tests** — the tool returns the guide (non-empty, contains the section headings); a web test asserts every `fieldHelp` key (Task 15) appears in the guide.

**Step 2:** `bash scripts/go-test.sh ./internal/agent/ -run 'TestDashboardGuide' -count=1 -timeout 3m` → FAIL.

**Step 3: Implement** — guide sections: widget types with full config examples (metric/text/table/chart), chapters + filter scoping, theming tokens + guardrails, publish/versions semantics, troubleshooting checklist, composition guidance (The Story). The tool is a plain string result (no args).

**Step 4:** tests pass.

**Step 5:** commit `feat(agent): dashboard_guide tool`.

---

### Task 13: `seed_rev` + description refresh

**Files:**
- Create: `internal/database/migrations/V131__tool_seed_rev.sql`
- Modify: `internal/agent/tools_seed.go`
- Modify: `internal/agent/tools_seed_test.go` (or the existing registry-sync test)

**Step 1: Failing test** — insert a builtin tool row with an old description + `seed_rev = 0`; run `SeedBuiltinTools`; the row's description/parameters equal the registry's and `seed_rev` equals the current revision.

**Step 2:** `bash scripts/go-test.sh ./internal/agent/ -run 'TestSeed' -count=1 -timeout 3m` → FAIL.

**Step 3: Implement** — `ALTER TABLE tools ADD COLUMN seed_rev INT NOT NULL DEFAULT 0`; `BuiltinTools` entries gain `Rev int` (constant `builtinSeedRev = 1` for this change); seeding: insert new rows with the current rev; `UPDATE … SET description=$, schema=$, config=$, seed_rev=$ WHERE org_id=$ AND name=$ AND type='builtin' AND seed_rev < $`. Document in AGENTS.md that builtin descriptions are platform-managed.

**Step 4:** tests pass.

**Step 5:** commit `feat(agent): refresh builtin tool descriptions via seed revision`.

---

### Task 14: Description refresh across the tool surface + engine prompt

**Files:**
- Modify: `internal/agent/tools_manage.go`, `internal/agent/tools_chart.go` (pointer to `set_widget_config`), `internal/agent/engine.go`
- Modify: the registry-sync test

**Step 1:** audit every dashboard tool description against the app-era model; write the new texts (create/update/get, `update_dashboard`, `update_dashboard_settings` incl. `filter_scope`, `set_dashboard_layout`, `publish_dashboard`, `create_dashboard_widget` incl. typed blocks, `update_dashboard_widget`); engine hint line mentions `dashboard_guide`, `review_dashboard`, `set_widget_config`.

**Step 2:** `bash scripts/go-test.sh ./internal/agent/ -count=1 -timeout 3m` + the seed-refresh test from Task 13 now covers the changed descriptions.

**Step 3:** commit `docs(agent): refresh dashboard tool descriptions for the app era`.

---

## Phase 5 — Human UX guidance pass

### Task 15: Field-help source + accessible HelpTip

**Files:**
- Create: `web/src/app-builder/fieldHelp.ts` + test
- Create: `web/src/components/HelpTip.tsx` + test

**Step 1: Failing tests** — `fieldHelp` covers every config field the drawer renders (metric, text, appearance, table, chart) with `{label, hint, example?}`; `HelpTip` renders a focusable button that opens a popover on click/Enter, closes on Escape/blur, wires `aria-describedby`, and works without hover.

**Step 2:** targeted run → FAIL.

**Step 3: Implement** — the structured source (typed records keyed by widget type + field path) and the component (popover positioned near the trigger, token-styled).

**Step 4:** tests pass; `npx tsc -b`.

**Step 5:** commit `feat(web): app-builder field help source and HelpTip`.

---

### Task 16: Apply guidance across the builder surfaces

**Files:**
- Modify: `web/src/pages/DashboardEditorPage.tsx` (empty state), `web/src/components/AddWidgetPicker` (or the picker inside the editor), `WidgetConfigDrawer.tsx`, `AppSettingsPanel.tsx`, `DashboardVersionsPanel.tsx`, `OrgSettingsPage.tsx`, `DashboardPage.tsx`/`PublicDashboardPage.tsx` (empty states)
- Modify their tests

**Step 1: Failing tests** — picker descriptors per type; drawer HelpTips for representative fields; app-panel hints incl. filter scope; versions empty state copy; app-view empty state copy; branding hints.

**Step 2:** targeted runs → FAIL.

**Step 3: Implement** — descriptors, hints, and micro-copy per the design's surface list; tone: quiet instrument, one line + example, progressive disclosure.

**Step 4:** targeted runs + full `npm run test:run -- src/components src/pages`; browser pass.

**Step 5:** commit `feat(web): guidance across the app-builder surfaces`.

---

### Task 17: Impeccable craft pass (bounded)

**Process (not a code task):** run the Impeccable flow over the builder surfaces — shape pass (patterns + copy), build adjustments, then **one** bounded critique round (desktop + mobile together), fix everything it shows in one batch, confirm with at most one more round. Record the decisions in `DESIGN.md` (builder guidance patterns) as part of Task 19.

**Commit:** `feat(web): impeccable pass on the app-builder surfaces` (whatever the pass changes).

---

## Phase 6 — Verification

### Task 18: E2E

**Files:** create `e2e/app-builder.spec.ts` (+ snapshots)

Scenarios:
1. **Chapter scoping**: a dashboard with two chapters and different variables per chapter + `filter_scope: "tab"` → the strip changes with the chapter; the hidden-set hint appears when another chapter's filter is set in the URL; toggling to "all" shows every filter.
2. **Health panel**: an editor with an empty chapter opens Review → the finding is listed → fixing it (adding a widget) clears it after re-open.
3. **Guidance/a11y**: the picker descriptors and a drawer HelpTip are reachable by keyboard; a themed snapshot of the App settings panel with guidance visible.

Run: `cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test app-builder.spec.ts --update-snapshots` then `--repeat-each=2`.

**Commit:** `test(e2e): chapter scoping, health panel, and builder guidance`.

---

### Task 19: Docs + full verification + PR

**Files:** `DESIGN.md` (builder guidance patterns; filter scoping), `AGENTS.md` (filter_scope, typed config tools, review, guide, seed rev), `FRONTEND.md` (health panel, HelpTip), the guide's human-readable copy under `docs/` if not already linked.

**Verification:**
```bash
task check
cd web && npx tsc -b && npm run build && npm run test:run
cd relay && npm run build
cd e2e && E2E_BASE_URL=http://localhost:8088 E2E_PG_CONNECTOR_HOST=aether-postgres npx playwright test app-builder.spec.ts dashboard-app.spec.ts dashboard-publish.spec.ts dashboard.spec.ts dashboard-filters.spec.ts
```

Then `git push -u origin feat/app-builder` + `gh pr create` (stacked on #247 until it merges; rebase onto `main` first if #247 has landed).

---

## Out of scope (later)

- Screenshots/vision for the agent.
- Independent per-chapter variable definitions.
- Org-forkable guide-as-skill.
- MCP allowlist additions for the new tools.
