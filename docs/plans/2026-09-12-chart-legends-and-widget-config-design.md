# Scrollable Top-Down Legends + Dashboard Widget Config Ownership — Design

**Date:** 2026-09-12
**Status:** Approved

Sources:
- `~/Downloads/chart-legend-vertical-scroll.md` (legend overflow: top-down, scrollable)
- `~/Downloads/dashboard-chart-shadowing-and-legend-toggle.md` (widget config shadowing + dead `showLegend` toggles + funnel/sankey legends)
- `~/Downloads/chart-config-gaps.md` §6 (chart-config change checklist)

All line references were re-verified against the current tree (base `80f7bf45`), not the docs' v0.39.0.

## Problem

1. **Legend overflow.** Axis charts use a wrapping top legend that consumes the plot once
   series are numerous (`BarChart.tsx:59`, `LineChart.tsx:51`, `AreaChart.tsx:63`,
   `ScatterChart.tsx:75`, `TimelineChart.tsx:56,181`, `MapChart.tsx:302`). Pie/donut stack
   items top-down on the right (`PieChart.tsx:61`) but clip silently past the canvas height.
   ECharts' native paging legend (`type: 'scroll'`) solves both.
2. **Dead legend toggles.** Timeline and map render `legend` unconditionally when grouped;
   their config-panel `showLegend` checkboxes do nothing. Funnel/sankey have no legend (and
   sankey's `defaultConfig.showLegend: false` is dead).
3. **Dashboard config shadowing.** `create_dashboard_widget` copies the cell's whole chart
   config into `widgets.config` at creation (`tools_manage.go:327-333`); every dashboard
   surface merges cell config first and widget config second, so the creation-time snapshot
   permanently shadows later notebook edits (labels, legend, title, axes). Dashboard-side
   edits also write full configs into `widgets.config`, re-cementing the shadow. The
   dashboard/public paths additionally skip `Cell.tsx`'s `normalizeChartConfig`
   (legacy chartType migration + undefined-stripping).

## Decisions

- **D1 — Native paging legend, right-docked.** One `buildLegend(config, colors)` helper in
  `charts/common.tsx` produces `type:'scroll'`, `orient:'vertical'`, `right:10`,
  `top: title?40:8`, `bottom:8`, `height:'80%'`, themed page icons/text. No custom DOM
  legend, no horizontal top bands. When `showLegend === false` it returns `{ show: false }`.
- **D2 — Axis charts reserve a right column.** `grid.right: showLegend ? 150 : 16` and
  `grid.top: title ? 56 : 8` for bar/line/area; scatter uses the same but only when its
  existing "legend shown" condition holds. DataZoom/grid bottom math is unchanged.
- **D3 — Pie keeps its center shift** (`['40%', title?'58%':'50%']` when legend shown) and
  gains the scroll cap, fixing clipping.
- **D4 — Timeline and map honor `showLegend`** through `buildLegend`, and their geometry
  reserves the right column only when the legend is actually rendered. Map's
  `defaultConfig.showLegend` flips to `true` (consistent with every other legend-capable
  chart); its geo/series box shrinks to leave room.
- **D5 — Funnel and sankey get legends** using the same right-column scroll helper, a
  config-panel Legend checkbox, and `showLegend: true` defaults. Their plot boxes shrink to
  reserve the column.
- **D6 — Charts without legends stay that way**, each with a one-line "no legend by design"
  comment: histogram (single series), hierarchy_tree (nodes), heatmap (visualMap), big_number.
- **D7 — `cells.metadata.chart` is the source of truth for chart appearance.**
  `widgets.config` holds only explicit per-widget overrides for chart widgets.
  - Creation stops snapshotting (`config = '{}'`).
  - Unflagged widgets (`config_edited_from_dashboard !== true`) render the cell config only;
    legacy creation-time snapshots become inert — no destructive cleanup migration needed.
  - Dashboard chart-config saves write the full config plus
    `config_edited_from_dashboard: true`; the merge helper strips the flag before render.
    The flag is a `widgets.config` concern, never a `ChartConfig` field.
  - A **Reset to notebook config** action (PUT `config: {}`) clears overrides.
- **D8 — Shared normalization.** `normalizeChartConfig` moves from `Cell.tsx` to a shared
  module and is applied by the merge helper, closing the legacy-config gap on dashboard and
  public paths. Embed already reads cell metadata directly.
- **D9 — Static export is out of scope.** `notebookExport.ts` keeps its current legends:
  paging arrows cannot work in a static image, and exporting only page 1 would hide series.
- **D10 — Existing `showLegend` config field only.** No new `ChartConfig` keys, no
  DB schema change beyond the backend behavior fix; `Cell.tsx`'s allow-list already carries
  `showLegend`.

## Changes

| Area | Files |
|---|---|
| Legend helper + convention | `web/src/charts/common.tsx`, `web/src/charts/types.ts` |
| Axis + scatter + pie | `web/src/charts/{Bar,Line,Area,Scatter,Pie}Chart.tsx` |
| Timeline + map | `web/src/charts/{Timeline,Map}Chart.tsx` |
| Funnel + sankey | `web/src/charts/{Funnel,Sankey}Chart.tsx` |
| No-legend comments | `web/src/charts/{Histogram,HierarchyTreeChart,HeatmapChart,BigNumber}.tsx` |
| Widget creation | `internal/agent/tools_manage.go` (+ test) |
| Widget merge + flag | new `web/src/charts/widgetChartConfig.ts`, `web/src/pages/{Dashboard,DashboardEditor,PublicDashboard}Page.tsx` |
| Normalization extraction | `web/src/components/Cell.tsx` → shared module |
| Reset affordance | `web/src/charts/{index.tsx,ChartConfigModal.tsx}`, `web/src/components/OutputRenderer.tsx` |

## Testing

- Unit: `buildLegend` (scroll/vertical/show:false/title offset), merge helper (unflagged
  ignores widget, flagged wins, flag stripped, normalization applied), funnel/sankey panel
  toggles, backend "create widget does not snapshot cell config".
- Browser (agent-browser): 40+ series area/bar/pie (page arrows appear, plot respected, no
  clipped items), legend-off widens the plot, funnel/sankey legends, timeline/map toggles,
  dashboard edit → flag → notebook edit stays overridden → reset converges, light/dark.
- `npm run test:run`, `npx tsc --noEmit`, `npm run build`, `npm run lint`, `task check`.

## Out of scope

- Dashboard-only chart editing without notebook edit permission (use the reset/preview
  behavior; overrides still require dashboard edit).
- Migrating old widget snapshots with SQL (ignored by D7).
- `notebookExport.ts` legend parity (D9) and its duplicate normalizer.
