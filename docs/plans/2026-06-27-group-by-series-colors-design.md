# group_by + series_colors for Axis-Based Charts

## Problem

When a bar/line/area/scatter chart has data grouped by a column (e.g., `service` with values `api-gateway`, `user-service`, etc.), the user cannot color individual group series because:

1. **Backend validation** (`filterChartConfig` in `tools_chart.go`) rejects `series_colors` keys not in `y_columns` — returns warning "Use chart_type='timeline' for group_by-based colors"
2. **group_by storage** is gated to `chartType == "timeline"` only — silently dropped for bar/line/area/scatter
3. **read_cell stale filtering** silently deletes `seriesColors` keys not in `y_Axis` on read
4. **Frontend rendering** creates one ECharts series per y_column — no series splitting by group value

The only workaround is to pivot the SQL query so each group becomes its own column, which is fragile and unintuitive.

## Design

### Backend changes

1. **`chartAllowedFields` map**: Add `"groupBy": true` to bar, stacked_bar, line, area, scatter entries
2. **`create_chart` / `update_chart`**: Remove the `chartType == "timeline"` gate — store `groupBy` for all chart types
3. **`filterChartConfig()`**: Remove y_columns-based `seriesColors` filtering entirely — allow any key through (frontend already handles unknown keys with fallback palette)
4. **`read_cell` stale filtering**: Remove `seriesColors` filtering for axis-based charts (lines 376-432)
5. **`read_cell` chart_summary**: Report `group_by` for all chart types, not just timeline — so `data_series` gets populated from the group column's unique values
6. **Tool descriptions**: Update `update_chart` description and `series_colors` parameter to reflect new capabilities

### Frontend changes

When `config.groupBy` is set and the data contains that column, each axis-based chart component creates series from the **cross-product of group values × y_columns**:

| y_columns | group_by | ECharts series names |
|---|---|---|
| `["cumulative_count"]` | `"service"` | `api-gateway`, `user-service`, ... |
| `["revenue", "cost"]` | `"service"` | `api-gateway (revenue)`, `api-gateway (cost)`, `user-service (revenue)`, ... |

**Color key**: Always the group value (e.g., `"api-gateway"`). All y_columns for the same group share the same color. Fallback palette index cycles per group.

**Series name convention**:
- No groupBy: y_column name (current behavior)
- Single y_column + groupBy: group value (e.g., `"api-gateway"`)
- Multiple y_columns + groupBy: `"group (y_column)"` (e.g., `"api-gateway (revenue)"`)

**Rendering algorithm** (shared `useGroupBySeries` hook in `common.tsx`):
```
1. Build Map<xVal, Map<groupVal, row>> from chartData (O(n))
2. uniqueX = ordered keys of outer map
3. groups = ordered unique group values
4. ECharts series = groups.flatMap((group, gi) =>
     yAxes.map((y) => ({
       name: yAxes.length > 1 ? `${group} (${y})` : group,
       data: uniqueX.map(x => map.get(x)?.get(group)?.[y] ?? null),
       itemStyle: { color: seriesColors?.[group] ?? CHART_COLORS[gi % N] },
     }))
   )
5. xAxis.data = uniqueX (instead of chartData.map(d => d[xAxis]))
```

When `groupBy` is NOT set, each component behaves exactly as before (zero risk to existing charts).

**Config panel**: When `groupBy` is set, hide y_columns color pickers and show hint text explaining that colors apply per group value. The agent can still set `series_colors` via the tool.

### Affected files

| File | Change type |
|---|---|
| `internal/agent/tools_chart.go` | Backend — remove gates, remove validation, update descriptions |
| `internal/agent/tools_notebook.go` | Backend — remove stale filtering, update summary |
| `internal/agent/engine.go` | Backend — update agent prompt descriptions |
| `web/src/charts/common.tsx` | Frontend — add `useGroupBySeries` hook |
| `web/src/charts/BarChart.tsx` | Frontend — use hook when groupBy set |
| `web/src/charts/LineChart.tsx` | Frontend — same |
| `web/src/charts/AreaChart.tsx` | Frontend — same |
| `web/src/charts/ScatterChart.tsx` | Frontend — same |
| `web/src/charts/AxisConfigPanel.tsx` | Frontend — hide color pickers when groupBy set |

### Not affected

- Pie/donut, timeline, hierarchy_tree, sankey, map, big_number — already have no validation and work correctly
- Database schema — no migration needed, `groupBy` is already in the JSONB metadata
- Backend chart config model — no new fields needed, `groupBy` and `seriesColors` already exist

### Example

Previously (agent had to pivot SQL):
```
UPDATE cells SET source = 'SELECT timestamp,
  countIf(service=''api-gateway'') ... AS "api-gateway", ...' WHERE id = '...'
UPDATE cells SET metadata = jsonb_set(..., '{chart,yAxis}', '["api-gateway","user-service",...]')
UPDATE cells SET metadata = jsonb_set(..., '{chart,seriesColors}', '{"api-gateway":"#B3D9FF",...}')
```

Now (agent describes intent):
```
UPDATE cells SET metadata = jsonb_set(..., '{chart,groupBy}', '"service"')
UPDATE cells SET metadata = jsonb_set(..., '{chart,seriesColors}', '{"api-gateway":"#B3D9FF","user-service":"#FFB3BA",...}')
```

No SQL rewrite needed. The frontend splits the data into group-based series automatically.
