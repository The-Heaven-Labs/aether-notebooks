# Timeline Chart UX Improvements — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the timeline chart UX — events sit on the time axis, mouse wheel zoom + drag pan, staggered labels with overlap avoidance, connector lines between events, time delta in tooltip.

**Architecture:** Single-file component `web/src/charts/TimelineChart.tsx` with point-event and range/Gantt modes. Uses ECharts scatter, custom, and line series. Config panel in the same file. No new dependencies. No backend changes (config fields are stored in `metadata.chart` JSON blob).

**Tech Stack:** React, TypeScript, ECharts v6.0.0, Vitest

---

### Task 1: Update ChartConfig types with new timeline fields

**Files:**
- Modify: `web/src/charts/types.ts:10-35`

**Step 1: Add `showConnectors` and `showTimeDeltas` to `ChartConfig`**

In the timeline-specific section (around line 33):

```typescript
export interface ChartConfig {
  // ... existing fields ...
  // Timeline-specific
  maxLabelLength?: number
  showConnectors?: boolean   // NEW: draw connector lines between consecutive events
  showTimeDeltas?: boolean   // NEW: show time-from-previous in tooltip
}
```

**Step 2: Update TimelineModule.defaultConfig in TimelineChart.tsx**

In `TimelineModule`:

```typescript
defaultConfig: { chartType: 'timeline', showLegend: true, showGrid: true, showLabels: true, showConnectors: true, showTimeDeltas: true },
```

---

### Task 2: Layout change — y-axis to value, compact height

**Files:**
- Modify: `web/src/charts/TimelineChart.tsx:9-144`

**Step 1: Point-event mode — switch y-axis from category to value**

In the point-event option block (around line 93-99), replace:

```typescript
// BEFORE
yAxis: {
  type: 'category' as const,
  data: groups,
  ...getAxisStyle(),
  show: groups.length > 1,
  axisLabel: { ...getAxisStyle().axisLabel, width: 60, overflow: 'truncate' as const }
},
```

With:

```typescript
// AFTER — conditional: value axis for single group, category for multi-group
...(groups.length === 1 ? {
  yAxis: {
    type: 'value' as const,
    show: false,
    min: 0,
    max: 0.5,
    splitLine: { show: false },
  }
} : {
  yAxis: {
    type: 'category' as const,
    data: groups,
    ...getAxisStyle(),
    show: true,
    axisLabel: { ...getAxisStyle().axisLabel, width: 60, overflow: 'truncate' as const }
  }
}),
```

And update the data mapping — each event uses y=0 for single group:

```typescript
data: chartData
  .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
  .map(d => [
    new Date(String(d[timeCol])).getTime(),
    groups.length === 1 ? 0 : group,  // 0 for value axis, group name for category
    labelCol ? d[labelCol] : null,
  ]),
```

**Step 2: Reduce chart height and grid**

Single-group height: `200px` instead of `350px`.

```typescript
height: groups.length === 1 ? 200 : 350,
```

Update grid to tighten padding:

```typescript
grid: {
  top: groups.length > 1 ? 40 : 12,
  right: 16,
  bottom: 16,   // reduced from 40 (slider is now thinner)
  left: 16,
  containLabel: true,
},
```

**Step 3: Range/Gantt mode — reduce lane spacing**

In the range mode option block (line 43-74), change:

```typescript
height: Math.max(200, groups.length * 36 + 60),  // was 50 + 80
```

And tighten grid:

```typescript
grid: { top: groups.length > 1 ? 30 : 12, right: 16, bottom: 16, left: 16, containLabel: true },
```

---

### Task 3: Interaction model — mouse wheel zoom + drag pan

**Files:**
- Modify: `web/src/charts/TimelineChart.tsx:49,100`

**Step 1: Update dataZoom in point-event mode**

Replace:

```typescript
dataZoom: [{ type: 'slider' as const, xAxisIndex: 0, bottom: 0, height: 20 }],
```

With:

```typescript
dataZoom: [{
  type: 'slider' as const,
  xAxisIndex: 0,
  bottom: 0,
  height: 8,
  zoomOnMouseWheel: true,
  moveOnMouseMove: true,
}],
```

**Step 2: Same change in range/Gantt mode**

Apply the identical dataZoom change in the range mode option block.

---

### Task 4: Label stagger + auto-hide overlap

**Files:**
- Modify: `web/src/charts/TimelineChart.tsx:112-124`

**Step 1: Replace static label position with dynamic stagger**

In the point-event series `label` config:

```typescript
label: showLabels ? {
  show: true,
  position: (params: { dataIndex: number }) =>
    params.dataIndex % 2 === 0 ? 'top' as const : 'bottom' as const,
  formatter: (params: { data: unknown[] }) => {
    const d = params.data as unknown[]
    return d[2] ? truncateLabel(d[2]) : ''
  },
  fontSize: 10,
  color: colors.textMuted,
  distance: 4,   // reduced from 8
  overflow: 'truncate' as const,
  ellipsis: '…',
} : undefined,
```

**Step 2: Add labelLayout to hide overlaps**

On the scatter series (same level as `label`, `emphasis`):

```typescript
labelLayout: { hideOverlap: true },
```

---

### Task 5: Connector lines + time delta tooltip

**Files:**
- Modify: `web/src/charts/TimelineChart.tsx:9-144`

**Step 1: Extract delta computation utility**

Add inside `TimelineChartComponent` (or as a module-level helper):

```typescript
function computeTimeDeltas(
  data: { time: number; group: string }[]
): Map<string, number> {
  const deltas = new Map<string, number>()
  for (let i = 1; i < data.length; i++) {
    const key = `${data[i].group}:${i}`
    deltas.set(key, data[i].time - data[i - 1].time)
  }
  return deltas
}

function formatDuration(ms: number): string {
  const seconds = Math.floor(ms / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)
  if (hours > 0) return `${hours}h ${minutes % 60}m`
  if (minutes > 0) return `${minutes}m ${seconds % 60}s`
  return `${seconds}s`
}
```

**Step 2: Generate sorted flat data for connector + delta logic**

In the `chartData` useMemo, also produce a sorted flat array:

```typescript
const flatTimeData = useMemo(() => {
  return chartData
    .filter(d => d[timeCol] != null)
    .map((d, i) => ({
      time: new Date(String(d[timeCol])).getTime(),
      group: groupByCol ? String(d[groupByCol] ?? 'Unknown') : 'Events',
      index: i,
    }))
    .sort((a, b) => a.time - b.time)
}, [chartData, timeCol, groupByCol])
```

**Step 3: Connector line series (point-event mode only)**

In the point-event `series` array, add a connector line before the scatter series (so it renders behind dots):

```typescript
...(config.showConnectors !== false && groups.length === 1 ? [{
  name: '__connector',
  type: 'line' as const,
  data: chartData
    .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
    .map(d => [new Date(String(d[timeCol])).getTime(), 0]),
  lineStyle: {
    color: colors.textMuted,
    width: 1,
    type: 'dashed' as const,
    opacity: 0.3,
  },
  symbol: 'none',
  animation: false,
  silent: true,
  z: 1,
}] : []),
```

For grouped mode, connector lines connect consecutive events within each group. Each group's line series maps to that group's y-index.

**Step 4: Time delta in tooltip**

Update the tooltip formatter to compute and show delta:

```typescript
tooltip: {
  ...getTooltipStyle(),
  trigger: 'item' as const,
  formatter: (params: { data: unknown[]; seriesName: string; dataIndex: number }) => {
    const d = params.data as unknown[]
    const time = new Date(d[0] as number).toLocaleString()
    const label = d[2] ? `<br/><b>${d[2]}</b>` : ''
    const group = groups.length > 1 ? `<br/>Group: ${params.seriesName}` : ''

    // Compute delta from previous event in same group
    let delta = ''
    if (config.showTimeDeltas !== false) {
      const currentIdx = params.dataIndex
      const dataForGroup = chartData
        .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === params.seriesName : true)
        .sort((a, b) => new Date(String(a[timeCol])).getTime() - new Date(String(b[timeCol])).getTime())
      const eventIdx = dataForGroup.findIndex(d =>
        new Date(String(d[timeCol])).getTime() === (d[0] as number)
      )
      if (eventIdx > 0) {
        const prevTime = new Date(String(dataForGroup[eventIdx - 1][timeCol])).getTime()
        const diff = (d[0] as number) - prevTime
        if (diff > 0) {
          delta = `<br/><span style="color:#888">Δ ${formatDuration(diff)}</span>`
        }
      } else {
        delta = `<br/><span style="color:#888">Sequence start</span>`
      }
    }

    return `<b>${time}</b>${label}${group}${delta}`
  },
},
```

---

### Task 6: Config panel — two new checkboxes

**Files:**
- Modify: `web/src/charts/TimelineChart.tsx:146-241`

**Step 1: Add showConnectors and showTimeDeltas checkboxes**

In `TimelineConfigPanel`, after the existing checkbox section (after line 220):

```typescript
<div style={styles.row}>
  <label style={styles.checkbox}>
    <input
      type="checkbox"
      checked={config.showConnectors ?? true}
      onChange={e => onChange({ ...config, showConnectors: e.target.checked })}
    />
    Show connectors
  </label>
  <label style={styles.checkbox}>
    <input
      type="checkbox"
      checked={config.showTimeDeltas ?? true}
      onChange={e => onChange({ ...config, showTimeDeltas: e.target.checked })}
    />
    Show time deltas
  </label>
</div>
```

---

### Task 7: Existing tests still pass

**Files:**
- Read: `web/src/test/TimelineChart.test.tsx`

**Step 1: Run tests to verify they pass**

```bash
cd web && npx vitest run src/test/TimelineChart.test.tsx
```

Expected: 4 tests pass.

**Step 2: Add new tests for timeline UX changes**

Add to `web/src/test/TimelineChart.test.tsx`:

```typescript
test('uses value y-axis for single group (no grouping)', () => {
  // Render without groupBy — should have yAxis.type === 'value'
  // (can't easily test ECharts option directly from rendered DOM,
  // so verify chart container renders and no crash)
  render(
    <TimelineModule.Component
      data={timeData}
      config={{ chartType: 'timeline', timeColumn: 'ts', labelColumn: 'msg', showConnectors: true, showTimeDeltas: true }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders with connectors and time deltas enabled', () => {
  render(
    <TimelineModule.Component
      data={timeData}
      config={{ chartType: 'timeline', timeColumn: 'ts', labelColumn: 'msg', showConnectors: true, showTimeDeltas: true }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('config panel shows connector and time delta toggles', () => {
  const onChange = vi.fn()
  render(
    <TimelineModule.ConfigPanel
      config={{ chartType: 'timeline', showConnectors: true, showTimeDeltas: true }}
      columns={['ts', 'msg', 'level']}
      onChange={onChange}
    />
  )
  expect(screen.getByText('Show connectors')).toBeInTheDocument()
  expect(screen.getByText('Show time deltas')).toBeInTheDocument()
})
```

**Step 3: Run tests again**

```bash
cd web && npx vitest run src/test/TimelineChart.test.tsx
```

Expected: 7 tests pass.

---

### Task 8: TypeScript + lint check

**Step 1: Run TypeScript check**

```bash
cd web && npx tsc --noEmit
```

Expected: No type errors.

**Step 2: Run lint**

```bash
cd web && npm run lint
```

Expected: No lint errors (or only pre-existing ones).
