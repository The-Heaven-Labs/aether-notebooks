# Chart Config Field Filtering — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Strip irrelevant chart config fields on save and only report relevant ones on read.

**Architecture:** Add a per-chart-type allowed-fields map. In `create_chart`/`update_chart`, filter `existingConfig`/`chartConfig` against it before saving. In `read_cell`, use the same map to limit `chart_summary` fields.

**Tech Stack:** Go (tools_chart.go, tools_notebook.go)

**Design doc:** `docs/plans/2026-06-26-chart-config-stripping-design.md`

---

### Task 1: Add allowed fields map and filter in create_chart/update_chart

**Files:**
- Modify: `internal/agent/tools_chart.go`

**Step 1: Add the allowed fields map**

Add at the top of `tools_chart.go` (or just before `makeCreateChartHandler`):

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
	"stacked_bar": {
		"title": true, "showLegend": true, "showGrid": true,
		"dataZoom": true, "showLabels": true, "seriesColors": true,
		"xAxis": true, "yAxis": true, "barWidth": true, "barCategoryGap": true,
	},
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
	"donut": {
		"title": true, "showLegend": true, "showLabels": true,
		"labelColumn": true, "seriesColors": true, "xAxis": true, "yAxis": true,
		"roseType": true, "startAngle": true, "padAngle": true,
	},
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

**Step 2: Add a helper function**

```go
func filterChartConfig(config map[string]any, chartType string) {
	allowed, ok := chartAllowedFields[chartType]
	if !ok {
		return
	}
	for k := range config {
		if k == "chartType" || k == "created_at" {
			continue
		}
		if !allowed[k] {
			delete(config, k)
		}
	}
}
```

**Step 3: Apply in create_chart**

After `chartConfig` is fully built (after all the `if req.Xxx != nil` blocks and before `configJSON, _ = json.Marshal(chartConfig)`), add:
```go
filterChartConfig(chartConfig, req.ChartType)
```

**Step 4: Apply in update_chart**

After all request fields are processed into `existingConfig` (after `if req.SizeColumn != ""` block), add:
```go
ct, _ := existingConfig["chartType"].(string)
if ct != "" {
	filterChartConfig(existingConfig, ct)
}
```

Also, remove the now-unnecessary `groupBy` cleanup and `series_colors` validation code since `filterChartConfig` handles that.

**Step 5: Build**

Run: `cd /home/jesus/Projects/hnb-claude && rtk go build ./internal/agent/...`
Expected: Success

**Step 6: Test**

Run: `cd /home/jesus/Projects/hnb-claude && rtk go test ./internal/agent/... -count=1 -short`
Expected: 13 passed

**Step 7: Commit**

```bash
git add internal/agent/tools_chart.go
git commit -m "feat: strip irrelevant chart config fields on chart_type save"
```

---

### Task 2: Filter chart_summary in read_cell

**Files:**
- Modify: `internal/agent/tools_notebook.go`

**Step 1: Import the map**

The `chartAllowedFields` map is in `tools_chart.go`. Since both files are in the same package (`agent`), no import needed — the map is accessible directly.

**Step 2: Filter chart_summary**

In the `chart_summary` builder in `read_cell` (around line 306-375), after extracting the chart config from metadata, only report fields that are in the allowed set for the current chart type.

Replace the current summary-building code with logic that:
1. Extracts `chartType` from metadata first
2. Gets the allowed fields set
3. Only adds summary entries for keys present in the allowed set

```go
summary := map[string]any{}
chartType := ""
if t, ok := meta.Chart["chartType"].(string); ok {
    chartType = t
}
summary["chart_type"] = chartType

allowed := chartAllowedFields[chartType]
for k, v := range meta.Chart {
    if k == "chartType" || k == "created_at" {
        continue
    }
    if allowed != nil && !allowed[k] {
        continue
    }
    // Add to summary, converting types as needed
    switch k {
    case "yAxis":
        if yAxis, ok := v.([]any); ok {
            var series []string
            for _, y := range yAxis {
                if s, ok := y.(string); ok {
                    series = append(series, s)
                }
            }
            summary["configured_series"] = series
        }
    case "title":
        summary["chart_title"], _ = v.(string)
    case "xAxis":
        summary["x_axis"], _ = v.(string)
    case "groupBy":
        summary["group_by"], _ = v.(string)
    case "timeColumn":
        summary["time_column"], _ = v.(string)
    // ...add other relevant summary keys
    }
}
```

Actually, this is complex. A simpler approach: keep the existing summary code but wrap each field in a check against the allowed set. For example:

```go
if allowed["yAxis"] || allowed == nil {
    // existing yAxis extraction code
}
if allowed["groupBy"] || allowed == nil {
    // existing groupBy extraction code
}
```

But wait — `chart_summary` is a high-level summary, not a full config dump. It currently reports: `chart_type`, `chart_title`, `configured_series`, `x_axis`, `group_by`, `time_column`, `data_columns`, `data_rows`, `sample_row`, `data_series`, `configured_series`. Most of these are derived from actual data, not just config fields.

The simplest approach: only filter the `chart_summary` fields that come directly from the chart config: `group_by`. If `groupBy` is not in the allowed set (i.e., chart type is not timeline), don't include it in the chart_summary.

This is already done from the previous change. So Task 2 might not need any changes.

**Step 2: Verify no changes needed**

Check if `group_by` is already conditionally excluded:
- Read around line 328-331 of `tools_notebook.go` to confirm `group_by` is only shown for timeline charts.

If already handled, this task is no-op.

**Step 3: Build and test**

Run: `cd /home/jesus/Projects/hnb-claude && rtk go build ./internal/agent/... && rtk go test ./internal/agent/... -count=1 -short`
Expected: Success

**Step 4: Commit**

```bash
git add internal/agent/tools_notebook.go
git commit -m "feat: filter chart_summary by allowed fields per chart type"
```
