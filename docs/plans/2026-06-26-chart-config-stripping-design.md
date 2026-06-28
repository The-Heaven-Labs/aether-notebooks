# Chart Config Field Filtering

**Date:** 2026-06-26  
**Status:** ✅ Implemented

## Problem

The agent tools `create_chart` and `update_chart` accept 38 configuration fields, but each chart type only uses a subset (8–12). The agent can set irrelevant fields (e.g., `groupBy` on a `line` chart) that are silently ignored by the rendering engine. This causes two problems:

1. Stale/irrelevant fields accumulate in `cells.metadata.chart`, confusing the agent when it reads back the config via `read_cell`.
2. The agent expects its settings to work, but they don't — leading to repeated error-and-retry cycles.

## Solution

Define per-chart-type allowed field sets. On every save (`create_chart`, `update_chart`), silently strip fields not in the allowed set. On read (`read_cell`'s `chart_summary`), only report fields the current chart type actually uses.

### Allowed Fields per Chart Type

```go
var chartAllowedFields = map[string]map[string]bool{
    "line": {
        "title": true, "showLegend": true, "showGrid": true,
        "dataZoom": true, "showLabels": true, "smooth": true,
        "connectNulls": true, "seriesColors": true, "xAxis": true, "yAxis": true,
    },
    "area": {
        "title": true, "showLegend": true, "showGrid": true,
        "dataZoom": true, "showLabels": true, "smooth": true,
        "connectNulls": true, "seriesColors": true, "xAxis": true, "yAxis": true,
    },
    "bar": {
        "title": true, "showLegend": true, "showGrid": true,
        "dataZoom": true, "showLabels": true, "seriesColors": true,
        "xAxis": true, "yAxis": true, "barWidth": true, "barCategoryGap": true,
    },
    "stacked_bar": same as bar,
    "scatter": {
        "title": true, "showLegend": true, "showGrid": true,
        "seriesColors": true, "colorColumn": true, "sizeColumn": true,
        "xAxis": true, "yAxis": true,
    },
    "pie": {
        "title": true, "showLegend": true, "showLabels": true,
        "labelColumn": true, "seriesColors": true, "xAxis": true, "yAxis": true,
        "roseType": true, "startAngle": true, "padAngle": true,
    },
    "donut": same as pie,
    "timeline": {
        "title": true, "showLegend": true, "showGrid": true,
        "showLabels": true, "showConnectors": true, "showTimeDeltas": true,
        "maxLabelLength": true, "timeColumn": true, "endTimeColumn": true,
        "labelColumn": true, "groupBy": true, "seriesColors": true,
    },
    "hierarchy_tree": {
        "title": true, "seriesColors": true, "idColumn": true,
        "parentIdColumn": true, "labelColumn": true, "metricColumns": true,
        "layout": true,
    },
    "sankey": {
        "title": true, "seriesColors": true, "xAxis": true, "yAxis": true,
        "nodeAlign": true, "nodeWidth": true, "nodeGap": true,
    },
    "map": {
        "title": true, "showLabels": true, "seriesColors": true,
        "labelColumn": true, "xAxis": true, "yAxis": true,
    },
    "big_number": {
        "valueColumn": true, "label": true, "prefix": true, "suffix": true,
        "decimalPlaces": true, "skipEmpty": true, "seriesColors": true,
    },
}
```

### Fields Always Kept

`chartType` and `created_at` are always kept regardless of chart type.

### Implementation

**`internal/agent/tools_chart.go`**:
- Add the `chartAllowedFields` map.
- In `create_chart`: after building `chartConfig`, iterate and delete keys not in the allowed set for `req.ChartType`.
- In `update_chart`: after processing all request fields into `existingConfig`, iterate and delete keys not in the allowed set for the final `chartType`.

**`internal/agent/tools_notebook.go`**:
- In `read_cell`'s chart_summary builder: use the same map to only report fields the current chart type actually uses.

### Existing Data

Existing charts with stale fields will be cleaned up automatically on the first `update_chart` call. No migration needed.

### Out of Scope

- The `AxisConfigPanel` UI already constrains what users can set via the color picker, dropdowns, etc. This design targets the agent-only tools.
- No new chart types or config fields are introduced.
