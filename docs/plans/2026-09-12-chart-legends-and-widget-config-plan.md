# Chart Legends + Widget Config Ownership — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship top-down scrollable legends on every legend-capable chart, fix the dead `showLegend` toggles, add funnel/sankey legends, and make `widgets.config` hold only explicit per-widget overrides with a dashboard override flag + reset.

**Architecture:** One `buildLegend(config, colors)` helper in `charts/common.tsx` replaces seven hand-built legends (right-docked ECharts `type:'scroll'` paging). A shared `mergeWidgetChartConfig` helper governs dashboard/public rendering: unflagged widgets render cell config only; flagged widgets render cell config with widget overrides. `create_dashboard_widget` stops snapshotting cell config.

**Tech Stack:** React 19 + TypeScript, ECharts 6 (core imports), Vitest + Testing Library, Go 1.25 stdlib, Flyway-style migrations not needed (no schema change).

**Design:** `docs/plans/2026-09-12-chart-legends-and-widget-config-design.md`

---

## Preconditions

- Branch `feat/chart-legends-and-widget-config` (design doc committed there).
- Dev stack up for browser checks: `docker compose -f docker-compose.dev.yml up -d` (web assets are rebuilt into `web/dist` by `web-builder`).
- Frontend checks: `cd web && npm run test:run`, `npx tsc --noEmit`, `npm run build`, `npm run lint`.
- Go checks: `AETHER_RATE_LIMIT_REGISTER=500 task check`.

---

### Task 1: `buildLegend` helper + convention doc + unit tests

**Files:**
- Modify: `web/src/charts/common.tsx` (add helper near `getTooltipStyle`/`getAxisStyle`)
- Modify: `web/src/charts/types.ts:108-134` (rewrite layout convention comment)
- Test: `web/src/test/legend.test.ts` (new)

**Step 1: Write the failing tests**

Create `web/src/test/legend.test.ts`:

```ts
import { test, expect } from 'vitest'
import { buildLegend } from '../charts/common'
import { getChartColors } from '../charts/common'

const colors = getChartColors()

test('hidden legend returns show:false', () => {
  expect(buildLegend({ showLegend: false }, colors)).toEqual({ show: false })
})

test('legend is a vertical scroll pager docked right', () => {
  const legend = buildLegend({}, colors) as Record<string, unknown>
  expect(legend.type).toBe('scroll')
  expect(legend.orient).toBe('vertical')
  expect(legend.right).toBe(10)
  expect(legend.top).toBe(8)
  expect(legend.bottom).toBe(8)
  expect(legend.height).toBe('80%')
  expect((legend.textStyle as Record<string, unknown>).color).toBe(colors.textMuted)
})

test('legend drops below a title band', () => {
  const legend = buildLegend({ title: 'Orders' }, colors) as Record<string, unknown>
  expect(legend.top).toBe(40)
})

test('scrolling legend is themed', () => {
  const legend = buildLegend({}, colors) as Record<string, unknown>
  expect(legend.pageIconColor).toBe(colors.textMuted)
  expect((legend.pageTextStyle as Record<string, unknown>).color).toBe(colors.textMuted)
})
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/test/legend.test.ts`
Expected: FAIL — `buildLegend` is not exported.

**Step 3: Implement**

Add to `web/src/charts/common.tsx` (after `getAxisStyle`):

```ts
export function buildLegend(
  config: Pick<import('./types').ChartConfig, 'title' | 'showLegend'>,
  colors: ReturnType<typeof getChartColors>,
): Record<string, unknown> {
  if (config.showLegend === false) return { show: false }
  return {
    type: 'scroll',
    orient: 'vertical' as const,
    right: 10,
    top: config.title ? 40 : 8,
    bottom: 8,
    height: '80%',
    align: 'auto',
    itemWidth: 14,
    itemHeight: 10,
    textStyle: { fontSize: 11, color: colors.textMuted },
    pageIconSize: 10,
    pageIconColor: colors.textMuted,
    pageIconInactiveColor: colors.border,
    pageTextStyle: { color: colors.textMuted, fontSize: 10 },
  }
}
```

Prefer importing the type at the top: add `ChartConfig` to the existing `import type { MarkLineConfig } from './types'` line. Update the `types.ts` convention comment block to describe the new pattern:

```
 *   legend: config.showLegend !== false ? {
 *     type: 'scroll', orient: 'vertical', right: 10,
 *     top: config.title ? 40 : 8, ...   // use buildLegend()
 *   } : { show: false },
 *
 *   grid: {
 *     top: config.title ? 56 : 8,
 *     right: config.showLegend !== false ? 150 : 16,   // legend column
 *     bottom: dataZoom ? 32 : 8, left: 16, containLabel: true,
 *   }
 *
 * Grid-less charts (pie/donut) keep the legend right-docked and shift
 * center/top instead; axis-less legends never sit above the plot.
```

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run --project=default src/test/legend.test.ts`
Expected: PASS (4 tests).

**Step 5: Commit**

```bash
git add web/src/charts/common.tsx web/src/charts/types.ts web/src/test/legend.test.ts
git commit -m "feat(charts): add scrollable vertical legend builder"
```

---

### Task 2: Axis charts, scatter, pie use the scroll legend

**Files:**
- Modify: `web/src/charts/BarChart.tsx` (legend line 59, grid lines 49-51)
- Modify: `web/src/charts/LineChart.tsx` (legend 51, grid 52)
- Modify: `web/src/charts/AreaChart.tsx` (legend 63, grid 64)
- Modify: `web/src/charts/ScatterChart.tsx` (legend 75, grid 76)
- Modify: `web/src/charts/PieChart.tsx` (legend 61; keep center shift 65)

**Changes**

Bar (both horizontal and vertical grid variants):

```ts
const legendShown = config.showLegend !== false
...
legend: legendShown ? buildLegend(config, colors) : { show: false },   // via helper in all cases
const grid = isHorizontal
  ? { top: config.title ? 56 : 8, right: legendShown ? 150 : 16, bottom: config.dataZoom ? 42 : 8, left: 16, containLabel: true }
  : { top: config.title ? 56 : 8, right: legendShown ? 150 : 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true }
```

`legend: buildLegend(config, colors)` is sufficient (the helper handles `show:false`); the extra variable is only for grid math.

Line/Area:

```ts
legend: buildLegend(config, colors),
grid: { top: config.title ? 56 : 8, right: config.showLegend !== false ? 150 : 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true },
```

Scatter (keep the existing "legend is meaningful" condition):

```ts
const legendShown = config.showLegend !== false && (hasGroupBy ? series.length > 1 : yAxes.length > 1)
...
legend: legendShown ? buildLegend(config, colors) : { show: false },
grid: { top: config.title ? 56 : 8, right: legendShown ? 150 : 16, bottom: 32, left: 16, containLabel: true },
```

Pie:

```ts
legend: buildLegend(config, colors),
center: config.showLegend !== false ? ['40%', config.title ? '58%' : '50%'] as [string, string] : ['50%', '50%'] as [string, string],
```

Add `buildLegend` to each file's import from `./common`. Deps arrays already include `config.showLegend`, `config.title`, `colors`; no new deps.

**Verify:** `cd web && npx tsc --noEmit && npx vitest run --project=default src/test/ChartView.test.tsx`

**Commit:**

```bash
git commit -am "feat(charts): top-down scrollable legends for axis, scatter, pie"
```

---

### Task 3: Timeline + map — working toggles and scroll legends

**Files:**
- Modify: `web/src/charts/TimelineChart.tsx` (range branch 56-57, point branch 115-122/181, deps)
- Modify: `web/src/charts/MapChart.tsx` (legend 302, geo 197-218, defaultConfig ~553)

**Timeline**

Add `const legendShown = groups.length > 1 && config.showLegend !== false` next to `groups`.
Range branch:

```ts
legend: legendShown ? buildLegend(config, colors) : undefined,
grid: { top: config.title ? 46 : 12, right: legendShown ? 150 : 16, bottom: 16, left: 16, containLabel: true },
```

Point branch: keep the `singleGroup` label-layout tops as-is, but for grouped layouts:

```ts
const gridTop = singleGroup
  ? (config.title ? 76 : 50)
  : (config.title ? 46 : 12)
const gridConfig = singleGroup
  ? { top: gridTop, right: 16, bottom: 60, left: 16 }
  : { top: gridTop, right: legendShown ? 150 : 16, bottom: 16, left: 16, containLabel: true }
...
legend: legendShown ? buildLegend(config, colors) : undefined,
```

Add `config.showLegend`, `legendShown` to the `useMemo` deps (range branch deps around line 243; point branch where `gridTop` is computed).

**Map**

```ts
const legendShown = hasGroupBy && config.showLegend !== false
...
legend: legendShown ? buildLegend(config, colors) : { show: false },
geo: { ...geo, right: legendShown ? 150 : undefined },
```

`geo.right: undefined` must not be spread when absent — build the geo object conditionally (`...(legendShown ? { right: 150 } : {})`) or set `right` only in the legend case. Add `legendShown`/`config.showLegend` to deps. Flip map `defaultConfig` to `showLegend: true`.

**Verify:** `cd web && npx tsc --noEmit && npx vitest run --project=default src/test/TimelineChart.test.tsx src/test/ChartView.test.tsx`

**Commit:**

```bash
git commit -am "fix(charts): honor showLegend on timeline and map with scroll legends"
```

---

### Task 4: Funnel + sankey legends, toggles, no-legend comments

**Files:**
- Modify: `web/src/charts/FunnelChart.tsx` (series 36-41, panel 137-152, defaultConfig 197-208)
- Modify: `web/src/charts/SankeyChart.tsx` (series 89-104, panel, defaultConfig 274-283)
- Modify: `web/src/charts/{Histogram,HierarchyTreeChart,HeatmapChart,BigNumber}.tsx` (comment only)

**Funnel**

```ts
const legendShown = config.showLegend !== false
...
legend: buildLegend(config, colors),
series: [{
  ...,
  top: config.title ? 56 : 20,
  left: '10%',
  right: legendShown ? 160 : '10%',
  ...
}]
```

Panel: Legend checkbox copied from `AxisConfigPanel.tsx:347-354` (`aria-label="Legend"`, `checked={config.showLegend ?? true}`). `defaultConfig` gains `showLegend: true`.

**Sankey**

```ts
const legendShown = config.showLegend !== false
...
legend: buildLegend(config, colors),
series: [{ ..., right: legendShown ? 160 : 16, top: config.title ? 40 : 8, bottom: 8, left: 8, ... }]
```

ECharts sankey accepts series `left/top/right/bottom` layout props. Panel: same Legend checkbox. `defaultConfig` flips `showLegend: false` → `true`.

**No-legend comments** (one line above the option builder in each):

- Histogram: `// No legend by design: a histogram is a single derived series.`
- HierarchyTree: `// No legend by design: nodes are labeled directly on the tree.`
- Heatmap: `// No legend by design: intensity is conveyed by visualMap.`
- BigNumber: `// No legend by design: a single scalar has no series.`

**Verify:** `cd web && npx tsc --noEmit && npx vitest run --project=default`

**Commit:**

```bash
git commit -am "feat(charts): add scroll legends and toggles to funnel and sankey"
```

---

### Task 5: `create_dashboard_widget` stops snapshotting chart config

**Files:**
- Modify: `internal/agent/tools_manage.go:327-333`
- Test: `internal/api/mcp_tools_test.go` or new `internal/api/dashboard_widget_test.go`

**Step 1: Failing test**

In package `api_test`, add a test that:
1. `setupTestServer`, register user/org, create notebook + cell via helpers, set the cell's `metadata.chart` to a non-empty JSON (e.g. via `PUT /api/v1/notebooks/{id}/cells/{cell_id}` with `{"metadata":{"chart":{"chartType":"pie","showLegend":false}}}`).
2. Create a dashboard (`POST /api/v1/dashboards` with `{"name":"D"}`).
3. Call the MCP tool: `doMCPRequest(t, srv, pat, "tools/call", {"name":"create_dashboard_widget","arguments":{"dashboard_id":..., "notebook_id":..., "cell_id":..., "type":"chart","row":0,"col":0,"width":6,"height":4}})` (create a PAT or use `testJWT.Issue` token; the MCP route accepts JWTs).
4. Query `SELECT config FROM widgets WHERE id = $1` and assert it is exactly `{}` (not the cell chart config).

Run it; expect FAIL with the snapshot JSON.

**Step 2: Implement**

Replace the copy block in `makeCreateDashboardWidgetHandler` with:

```go
// Chart widgets render cells.metadata.chart; widgets.config holds only
// explicit per-widget overrides (config_edited_from_dashboard). Never copy
// the cell config here — a creation-time snapshot would shadow notebook edits.
widgetConfig := json.RawMessage(`{}`)
```

Delete the now-unused `cellChart` query. Run `go build ./...`.

**Step 3: Verify + commit**

Run: `AETHER_RATE_LIMIT_REGISTER=500 go test ./internal/api/ -run 'TestCreateDashboardWidget' -p 1 -count=1 -v`
Expected: PASS.

```bash
git commit -am "fix(dashboards): stop snapshotting cell chart config into widgets"
```

---

### Task 6: Shared normalizer + merge helper + tests

**Files:**
- Create: `web/src/charts/normalizeChartConfig.ts` (move from `Cell.tsx:21-89`)
- Create: `web/src/charts/widgetChartConfig.ts`
- Modify: `web/src/components/Cell.tsx` (import the shared function; delete the local copy)
- Test: `web/src/test/widgetChartConfig.test.ts` (new)

**normalizeChartConfig.ts** — move the function verbatim (export it). Cell.tsx keeps behavior via `import { normalizeChartConfig } from '../charts/normalizeChartConfig'`.

**widgetChartConfig.ts:**

```ts
import type { ChartConfig } from './types'
import { normalizeChartConfig } from './normalizeChartConfig'

export const WIDGET_OVERRIDE_FLAG = 'config_edited_from_dashboard'

export function hasWidgetOverride(widgetConfig: unknown): boolean {
  return !!widgetConfig && typeof widgetConfig === 'object' &&
    (widgetConfig as Record<string, unknown>)[WIDGET_OVERRIDE_FLAG] === true
}

/**
 * Chart appearance for a dashboard widget.
 * Unflagged widgets render the notebook cell config only (legacy creation-time
 * snapshots are ignored). Flagged widgets render the cell config plus explicit
 * per-widget overrides; the marker key is never passed to charts.
 */
export function mergeWidgetChartConfig(cellChart: unknown, widgetConfig: unknown): ChartConfig {
  const cell = normalizeChartConfig(cellChart) ?? ({} as ChartConfig)
  if (!hasWidgetOverride(widgetConfig)) return cell
  const { [WIDGET_OVERRIDE_FLAG]: _ignored, ...overrides } =
    widgetConfig as Record<string, unknown>
  return normalizeChartConfig({ ...cell, ...overrides }) ?? ({} as ChartConfig)
}
```

**Tests** (pure):

1. unflagged widget config is ignored: `merge({chartType:'pie',showLegend:true}, {chartType:'bar'})` → `chartType: 'pie'`.
2. flagged widget wins: `merge({chartType:'pie',showLegend:true}, {config_edited_from_dashboard:true, chartType:'bar'})` → `chartType: 'bar'`, no `config_edited_from_dashboard` key.
3. legacy snapshot key not carried: `merge` result never contains the flag.
4. normalization runs: legacy `{type:'stacked_bar'}` cell → `chartType: 'bar', barMode: 'stacked'`.
5. empty/null inputs → `{}`.

**Verify:** `cd web && npx vitest run --project=default src/test/widgetChartConfig.test.ts`

**Commit:**

```bash
git commit -am "feat(dashboards): shared chart config normalizer and widget merge helper"
```

---

### Task 7: Wire merge + override flag + reset into dashboard surfaces

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx:180-185, 263`
- Modify: `web/src/pages/DashboardEditorPage.tsx:70, 216-221`
- Modify: `web/src/pages/PublicDashboardPage.tsx:92`
- Modify: `web/src/charts/index.tsx` (ChartView props)
- Modify: `web/src/charts/ChartConfigModal.tsx` (optional reset button)
- Modify: `web/src/components/OutputRenderer.tsx:34-46, 58, 75, 293-295, 719`

**Rendering**

DashboardPage:
```ts
const chartConfig = mergeWidgetChartConfig((cell as any).metadata?.chart, widget.config)
const chartOverridden = hasWidgetOverride(widget.config)
```
pass `chartConfigOverridden={chartOverridden}` and `onChartConfigReset={() => resetWidgetConfig(widget.id)}`.

DashboardEditorPage: same in `WidgetContent`.
PublicDashboardPage: `mergeWidgetChartConfig(cellData.metadata?.chart, widget.config)` (no reset props).

**Saving** — DashboardPage `handleChartConfigChange` and DashboardEditorPage `saveWidgetConfig` PUT:

```ts
api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, {
  config: { ...config, [WIDGET_OVERRIDE_FLAG]: true },
})
```

**Reset handler** (both pages): PUT `{ config: {} }` and invalidate the dashboard query.

**Plumbing**

- `OutputRenderer` Props add `chartConfigOverridden?: boolean`, `onChartConfigReset?: () => void`; thread through `OutputItem` → `TableOutput` → `<ChartView ... chartConfigOverridden={...} onChartConfigReset={...} />`.
- `ChartView` props add the same two; pass to both `ChartConfigModal` render sites.
- `ChartConfigModal` props add `onResetToNotebook?: () => void`; when provided, render a text button in the header actions:

```tsx
{onResetToNotebook && (
  <button style={styles.resetBtn} onClick={() => { onResetToNotebook(); onClose() }}
    title="Use the notebook's chart config again">
    Reset to notebook
  </button>
)}
```

Style it like `cancelBtn` but auto width (`fontSize: 11, padding: '4px 10px'`).

**Verify:** `cd web && npx tsc --noEmit && npm run lint && npx vitest run --project=default`

**Commit:**

```bash
git commit -am "feat(dashboards): override flag, merge helper, and reset-to-notebook"
```

---

### Task 8: Full verification + browser sweep + PR

**Step 1: Automated checks**

```bash
cd web && npx tsc --noEmit && npm run test:run && npm run build && npm run lint
cd .. && AETHER_RATE_LIMIT_REGISTER=500 task check
```

**Step 2: Browser sweep (agent-browser, dev stack at :8088)**

Wait for `web-builder` to finish (the API serves `web/dist`). Then:

1. Login (`nova@heaven-labs.com` / `nova123`).
2. Create/open a notebook; run a query with 40+ groups from the ClickHouse seed data (e.g. `SELECT ... GROUP BY ... LIMIT 50`), make an area chart, then a bar chart: screenshot and confirm the legend is a right-docked top-down column with page arrows, the plot is not covered, and toggling Legend off widens the plot.
3. Switch to pie/donut with 30+ slices: all items reachable via paging (screenshot before/after one page click).
4. Funnel and sankey on a grouped/flow query: legend renders on the right.
5. Timeline and map with `groupBy`: the existing Legend checkbox now hides/shows the legend.
6. Dashboard: add a chart widget, open Configure from the dashboard, save a change (e.g. title) → verify it persists under override even after changing the notebook config; click Reset to notebook → notebook value returns.
7. Light/dark theme screenshot for pager visibility.
8. Use the ImageAnalyzer agent on screenshots to confirm layout claims.

**Step 3: Fix anything found; rerun the affected checks; commit.**

**Step 4: Push + PR**

```bash
git push -u origin feat/chart-legends-and-widget-config
gh pr create --base main --title "feat(charts): scrollable top-down legends + dashboard widget config ownership" --body "..."
```

Include design/plan links, the testing evidence (automated + browser), and note D9 (static export unchanged) and D7 (legacy snapshots inert, no destructive migration).
