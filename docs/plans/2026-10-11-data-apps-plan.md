# Data Apps — Wave 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship the App view and the widget kit that makes a dashboard read like a product: `settings.app` in the backend, real `metric` + `text` widgets, per-widget appearance, table column formatting, and the full-bleed chrome-free App surface (The Story) with the editor affordances to author it.

**Architecture:** Approach A from `docs/plans/2026-10-11-data-apps-design.md`, wave 1 only. No new tables: `dashboards.settings.app` is additive JSONB, widget configs extend existing JSONB, and metric/text render client-side over the existing `POST /dashboards/{id}/execute` results. The App surface is a bare `AppShell` variant plus a masthead composition; the public page reuses the same renderers.

**Tech Stack:** Go (`net/http` ServeMux, pgx), React 19 + TypeScript + React Query + ECharts + react-grid-layout, Vitest, Playwright.

**Preconditions:**
- Branch `feat/data-apps-design` is checked out (design doc committed at `76d096c0`). Implementation continues here; open a PR at the end.
- Start infra before Go tests: `task infra:up`
- Full dev stack before E2E/browser validation: `docker compose -f docker-compose.dev.yml up -d`
- All Go test commands include `-timeout 3m` (repo rule).
- Waves 2–4 (tabs, publish, theming, escape hatches, distribution, agent tooling) are separate plans; do not implement them here.

---

## Phase 1 — Backend: app settings

### Task 1: `DashboardAppSettings` model + validation

**Files:**
- Modify: `internal/models/dashboard.go`
- Modify: `internal/api/dashboard_handlers.go` (validation in `handleUpdateDashboard`, before the settings marshal at ~line 152)
- Modify: `internal/api/dashboard_handlers_test.go` (append test)

**Step 1: Write the failing test**

Append to `internal/api/dashboard_handlers_test.go` (follow `TestDashboardCRUD`'s helper usage):

```go
func TestDashboardAppSettings(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("dash-app-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Dash App Org")

	body, _ := json.Marshal(map[string]interface{}{"title": "App Settings"})
	req := httptest.NewRequest("POST", "/api/v1/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var dashResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&dashResp)
	dashID := dashResp["id"].(string)

	put := func(settings map[string]interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]interface{}{"settings": settings})
		r := httptest.NewRequest("PUT", "/api/v1/dashboards/"+dashID, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-AETHER-Admin-Mode", "true")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w
	}

	// Valid app settings round-trip.
	rec = put(map[string]interface{}{
		"app": map[string]interface{}{
			"title":             "Q3 Revenue Review",
			"subtitle":          "Sales ops · updated hourly",
			"width":             "full",
			"show_header":       true,
			"show_refresh":      false,
			"show_last_updated": true,
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/v1/dashboards/"+dashID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var got struct {
		Settings struct {
			App struct {
				Title       string `json:"title"`
				Subtitle    string `json:"subtitle"`
				Width       string `json:"width"`
				ShowRefresh *bool  `json:"show_refresh"`
			} `json:"app"`
		} `json:"settings"`
	}
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Settings.App.Title != "Q3 Revenue Review" || got.Settings.App.Width != "full" {
		t.Fatalf("app settings did not round-trip: %+v", got.Settings.App)
	}
	if got.Settings.App.ShowRefresh == nil || *got.Settings.App.ShowRefresh {
		t.Fatalf("expected show_refresh=false to survive, got %+v", got.Settings.App.ShowRefresh)
	}

	// Invalid width → 400.
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"width": "wide"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid width: expected 400, got %d", rec.Code)
	}

	// Overlong title → 400.
	long := strings.Repeat("x", 200)
	if rec = put(map[string]interface{}{"app": map[string]interface{}{"title": long}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("overlong title: expected 400, got %d", rec.Code)
	}
}
```

Add `"strings"` to the test file's imports if missing.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestDashboardAppSettings -timeout 3m -v`
Expected: FAIL — `settings.app` does not round-trip (the struct has no field) and invalid width returns 200.

**Step 3: Add the model**

In `internal/models/dashboard.go`, extend `DashboardSettings` and add the new type:

```go
type DashboardSettings struct {
	AutoRefreshSeconds int                   `json:"auto_refresh_seconds,omitempty"`
	GridCols           int                   `json:"grid_cols,omitempty"`
	QueryCacheSeconds  *int                  `json:"query_cache_seconds,omitempty"`
	PublicLive         bool                  `json:"public_live,omitempty"`
	Variables          []DashboardVariable   `json:"variables,omitempty"`
	App                *DashboardAppSettings `json:"app,omitempty"`
}

// DashboardAppSettings configures the dashboard's App surface (the
// full-bleed, chrome-free viewer). All fields are optional; absent values
// keep today's viewer behavior (fixed width, header/refresh/freshness shown).
type DashboardAppSettings struct {
	Title            string `json:"title,omitempty"`
	Subtitle         string `json:"subtitle,omitempty"`
	LogoAttachmentID string `json:"logo_attachment_id,omitempty"`
	Width            string `json:"width,omitempty"`  // "fixed" | "full"; empty = fixed
	Layout           string `json:"layout,omitempty"` // "grid" | "custom"; empty = grid (wave 4)
	ShowHeader       *bool  `json:"show_header,omitempty"`
	ShowRefresh      *bool  `json:"show_refresh,omitempty"`
	ShowLastUpdated  *bool  `json:"show_last_updated,omitempty"`
}
```

The booleans are pointers so "unset" (render by default) is distinguishable from an explicit `false`.

**Step 4: Add validation**

In `internal/api/dashboard_handlers.go`, in `handleUpdateDashboard`, before `settingsJSON, _ := json.Marshal(req.Settings)`:

```go
	if req.Settings != nil {
		if err := validateDashboardAppSettings(req.Settings.App); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
```

And add near the request types:

```go
const (
	maxAppTitleRunes    = 120
	maxAppSubtitleRunes = 200
)

// validateDashboardAppSettings bounds the app surface configuration. Unknown
// JSON keys are already dropped by the struct round-trip; this only rejects
// values that would render incorrectly.
func validateDashboardAppSettings(app *models.DashboardAppSettings) error {
	if app == nil {
		return nil
	}
	if len([]rune(app.Title)) > maxAppTitleRunes {
		return fmt.Errorf("app title must be at most %d characters", maxAppTitleRunes)
	}
	if len([]rune(app.Subtitle)) > maxAppSubtitleRunes {
		return fmt.Errorf("app subtitle must be at most %d characters", maxAppSubtitleRunes)
	}
	if app.Width != "" && app.Width != "fixed" && app.Width != "full" {
		return fmt.Errorf(`app width must be "fixed" or "full"`)
	}
	if app.Layout != "" && app.Layout != "grid" && app.Layout != "custom" {
		return fmt.Errorf(`app layout must be "grid" or "custom"`)
	}
	return nil
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestDashboard' -timeout 3m`
Expected: PASS (new test + existing dashboard tests).

**Step 6: Commit**

```bash
git add internal/models/dashboard.go internal/api/dashboard_handlers.go internal/api/dashboard_handlers_test.go
git commit -m "feat(api): dashboard app settings model and validation"
```

---

## Phase 2 — Frontend: widget kit

### Task 2: Shared value formatting + metric computation utils

**Files:**
- Create: `web/src/utils/valueFormat.ts`
- Create: `web/src/utils/metric.ts`
- Create: `web/src/utils/metric.test.ts`
- Modify: `web/src/types/index.ts` (add `DashboardAppSettings`, extend `Dashboard.settings`)

**Step 1: Write the failing tests**

`web/src/utils/metric.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { computeMetric, formatValue } from './metric'

const result = {
  columns: [{ name: 'revenue' }, { name: 'day' }, { name: 'prev' }],
  rows: [
    [120, '2026-10-01', 100],
    [80, '2026-10-02', 90],
  ],
}

describe('computeMetric', () => {
  it('sums the value column', () => {
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'sum' })).toBe(200)
  })
  it('returns the first value for aggregation none', () => {
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'none' })).toBe(120)
  })
  it('averages, counts, and counts distinct', () => {
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'avg' })).toBe(100)
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'count' })).toBe(2)
    expect(computeMetric(result as never, { value_column: 'day', aggregation: 'count_distinct' })).toBe(2)
  })
  it('returns null for an unknown column or empty rows', () => {
    expect(computeMetric(result as never, { value_column: 'nope', aggregation: 'sum' })).toBeNull()
    expect(computeMetric({ columns: [], rows: [] } as never, { value_column: 'x', aggregation: 'sum' })).toBeNull()
  })
})

describe('formatValue', () => {
  it('formats plain numbers with decimals', () => {
    expect(formatValue(1234.5, { style: 'number', decimals: 1 })).toBe('1,234.5')
  })
  it('formats currency', () => {
    expect(formatValue(200, { style: 'currency', currency: 'USD', decimals: 0 })).toBe('$200')
  })
  it('formats percent as-is with a suffix (no 100x multiplication)', () => {
    expect(formatValue(12.5, { style: 'percent', decimals: 1 })).toBe('12.5%')
  })
  it('formats compact numbers', () => {
    expect(formatValue(1_250_000, { style: 'number', compact: true })).toBe('1.3M')
  })
  it('renders null as an em dash', () => {
    expect(formatValue(null)).toBe('—')
  })
})
```

**Step 2: Run tests to verify they fail**

Run: `cd web && npm run test:run -- src/utils/metric.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement `valueFormat.ts`**

```ts
export interface ValueFormat {
  style?: 'number' | 'currency' | 'percent'
  currency?: string
  decimals?: number
  compact?: boolean
}

export function formatValue(value: number | string | null | undefined, format?: ValueFormat): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'string') return value
  const decimals = format?.decimals
  const opts: Intl.NumberFormatOptions = {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals ?? 2,
  }
  if (format?.style === 'currency') {
    opts.style = 'currency'
    opts.currency = format.currency || 'USD'
  } else if (format?.compact) {
    opts.notation = 'compact'
    opts.maximumFractionDigits = decimals ?? 1
  }
  const formatted = new Intl.NumberFormat('en-US', opts).format(value)
  // Percent is rendered as-is (12.5 → "12.5%"), unlike Intl's style:'percent'
  // which multiplies by 100 — widget authors write their SQL values directly.
  return format?.style === 'percent' ? `${formatted}%` : formatted
}
```

**Step 4: Implement `metric.ts`**

```ts
import { formatValue, type ValueFormat } from './valueFormat'
import type { ResultSet } from '../types'

export type MetricAggregation =
  | 'none' | 'sum' | 'avg' | 'min' | 'max' | 'median'
  | 'count' | 'count_distinct' | 'first'

export interface MetricConfig {
  value_column?: string
  aggregation?: MetricAggregation
  label?: string
  format?: ValueFormat
  comparison?: { column?: string; mode?: 'delta' | 'percent' }
  sparkline?: { x_column?: string; y_column?: string }
}

export function metricConfigFrom(config: Record<string, unknown> | undefined): MetricConfig {
  const m = config?.metric
  return (m && typeof m === 'object' ? m : {}) as MetricConfig
}

function columnValues(result: ResultSet, column: string): unknown[] {
  const idx = result.columns.findIndex(c => c.name === column)
  if (idx < 0) return []
  return result.rows.map(row => row[idx])
}

export function computeMetric(result: ResultSet, cfg: MetricConfig): number | null {
  const column = cfg.value_column
  if (!column || !result.rows.length) return null
  const idx = result.columns.findIndex(c => c.name === column)
  if (idx < 0) return null
  const raw = result.rows.map(row => row[idx])
  const nums = raw.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const agg = cfg.aggregation ?? 'none'
  switch (agg) {
    case 'none':
    case 'first':
      return typeof raw[0] === 'number' ? raw[0] : null
    case 'count':
      return raw.length
    case 'count_distinct':
      return new Set(raw.map(v => String(v))).size
    default:
      if (!nums.length) return null
      switch (agg) {
        case 'sum': return nums.reduce((a, b) => a + b, 0)
        case 'avg': return nums.reduce((a, b) => a + b, 0) / nums.length
        case 'min': return Math.min(...nums)
        case 'max': return Math.max(...nums)
        case 'median': {
          const sorted = [...nums].sort((a, b) => a - b)
          const mid = Math.floor(sorted.length / 2)
          return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2
        }
        default: return null
      }
  }
}

export function computeComparison(result: ResultSet, cfg: MetricConfig): { delta: number; percent: number } | null {
  const column = cfg.comparison?.column
  const main = computeMetric(result, cfg)
  if (!column || main === null) return null
  const prev = columnValues(result, column).find((v): v is number => typeof v === 'number')
  if (prev === undefined) return null
  const delta = main - prev
  return { delta, percent: prev === 0 ? 0 : (delta / prev) * 100 }
}

export function sparklinePoints(result: ResultSet, cfg: MetricConfig): Array<[number, number]> | null {
  const { x_column, y_column } = cfg.sparkline ?? {}
  if (!x_column || !y_column || result.rows.length < 2) return null
  const xi = result.columns.findIndex(c => c.name === x_column)
  const yi = result.columns.findIndex(c => c.name === y_column)
  if (xi < 0 || yi < 0) return null
  const rows = result.rows.filter(row => typeof row[yi] === 'number')
  if (rows.length < 2) return null
  // x is only used for ordering: numeric values sort numerically, date-like
  // strings sort by parsed time, anything else keeps the row order.
  const xs = rows.map(row => row[xi])
  const numeric = xs.every(v => typeof v === 'number')
  const dateLike = !numeric && xs.every(v => !Number.isNaN(new Date(String(v)).getTime()))
  if (numeric) rows.sort((a, b) => (a[xi] as number) - (b[xi] as number))
  else if (dateLike) rows.sort((a, b) => new Date(String(a[xi])).getTime() - new Date(String(b[xi])).getTime())
  return rows.map((row, i) => [i, row[yi] as number])
}

export { formatValue }
```

`formatValue` is re-exported so the metric widget and the table share one formatter.

**Step 5: Extend the TypeScript types**

In `web/src/types/index.ts`:

```ts
export interface DashboardAppSettings {
  title?: string
  subtitle?: string
  logo_attachment_id?: string
  width?: 'fixed' | 'full'
  layout?: 'grid' | 'custom'
  show_header?: boolean
  show_refresh?: boolean
  show_last_updated?: boolean
}
```

and add `app?: DashboardAppSettings` to `Dashboard['settings']`.

**Step 6: Run tests to verify they pass**

Run: `cd web && npm run test:run -- src/utils/metric.test.ts && npx tsc --noEmit`
Expected: PASS.

**Step 7: Commit**

```bash
git add web/src/utils/valueFormat.ts web/src/utils/metric.ts web/src/utils/metric.test.ts web/src/types/index.ts
git commit -m "feat(web): metric computation and value formatting utils"
```

---

### Task 3: `MetricWidget` component (The Story's reading)

**Files:**
- Create: `web/src/components/MetricWidget.tsx`
- Create: `web/src/components/MetricWidget.test.tsx`

**Step 1: Write the failing tests**

```tsx
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MetricWidget } from './MetricWidget'
import type { ResultSet } from '../types'

const result = {
  columns: [{ name: 'revenue' }, { name: 'day' }, { name: 'prev' }],
  rows: [
    [120, '2026-10-01', 100],
    [80, '2026-10-02', 90],
  ],
} as unknown as ResultSet

describe('MetricWidget', () => {
  it('renders the label and the formatted aggregate', () => {
    render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'sum', label: 'Total revenue', format: { style: 'currency', currency: 'USD', decimals: 0 } }} />)
    expect(screen.getByText('Total revenue')).toBeInTheDocument()
    expect(screen.getByText('$200')).toBeInTheDocument()
  })

  it('shows the comparison as a signed percent', () => {
    render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'sum', comparison: { column: 'prev', mode: 'percent' } }} />)
    expect(screen.getByText('+100.0%')).toBeInTheDocument()
  })

  it('renders a sparkline polyline when configured', () => {
    const { container } = render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'none', sparkline: { x_column: 'day', y_column: 'revenue' } }} />)
    expect(container.querySelector('svg polyline')).toBeTruthy()
  })

  it('renders an em dash for a missing column', () => {
    render(<MetricWidget result={result} config={{ value_column: 'nope', aggregation: 'sum' }} />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/MetricWidget.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement**

`web/src/components/MetricWidget.tsx` — pure presentation; no fetching:

- Props: `{ result: ResultSet; config: MetricConfig }`.
- Value: `computeMetric` + `formatValue(config.format)`.
- Comparison: `computeComparison`; percent mode → `+25.0%` (one decimal, signed); delta mode → signed `formatValue(delta, format)`. Positive → `var(--success)`, negative → `var(--danger)`, zero → `var(--text-muted)`; icon `TrendingUp`/`TrendingDown` (lucide, 12px).
- Sparkline: inline SVG (`width="100%"` `height={28}` `viewBox="0 0 120 28"` `preserveAspectRatio="none"`), one `<polyline>` with `stroke="var(--accent)"`, `fill="none"`, `strokeWidth={1.5}`; normalize points between min/max with 2px vertical padding.
- Reading type role (approved recommendation, open decision #1 in the design): value `fontFamily: 'var(--font-mono)'`, `fontVariantNumeric: 'tabular-nums'`, `fontSize: 32`, `fontWeight: 700`, `letterSpacing: '-0.5px'`, `lineHeight: 1.1`. Label: instrument label — 10px, 700, uppercase, `letterSpacing: '0.08em'`, `var(--font-mono)`, `var(--text-muted)`.
- Layout: flex column, `justifyContent: 'center'`, `gap: 4`, `padding: 16` (WidgetFrame may override padding in Task 8), left-aligned; sparkline under the value.
- Missing value → `—` in `var(--text-muted)`; missing `value_column` → muted hint "Pick a value column in the widget settings." (so an unconfigured metric teaches instead of crashing).
- Accessibility: `role="group"` with `aria-label` = `"{label}: {formatted}{comparison}"`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/MetricWidget.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/MetricWidget.tsx web/src/components/MetricWidget.test.tsx
git commit -m "feat(web): metric widget with comparison and sparkline"
```

---

### Task 4: `QueryDataWidget` renders metric by type

**Files:**
- Modify: `web/src/components/QueryDataWidget.tsx` (renderer switch at ~line 296)
- Modify: `web/src/components/QueryDataWidget.test.tsx`

**Step 1: Write the failing test**

Extend `QueryDataWidget.test.tsx` (follow the existing mock pattern in that file): add a case where the mocked execute response contains one row and the widget type is `metric` with `config.metric = { value_column: 'v', aggregation: 'none' }`; expect the formatted value to render (e.g. `42`).

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/QueryDataWidget.test.tsx`
Expected: FAIL — metric renders through `OutputRenderer` as a table.

**Step 3: Implement the renderer switch**

Replace the success-path render (after the zero-state block) with:

```tsx
  const metric = metricConfigFrom(widget.config)

  return (
    <div style={{ ...styles.root, opacity: isFetching ? 0.55 : 1 }} aria-busy={isFetching}>
      {widget.type === 'metric' && resultSet ? (
        <MetricWidget result={resultSet} config={metric} />
      ) : (
        <OutputRenderer
          outputs={outputs}
          fixedView={widget.type === 'chart' ? 'chart' : 'table'}
          chartConfig={normalizeChartConfig(widget.config)}
          footerExtra={footerExtra}
        />
      )}
    </div>
  )
```

Notes:
- Keep the existing zero-state logic (`filtersExclude || noRows`) untouched — a metric with zero rows still shows `NoDataState`.
- Metric widgets keep the footer (`footerExtra`) so freshness and refresh stay available; render it under the metric inside the same root (wrap in a footer row). Simplest: pass `footerExtra` through a small flex column: metric on top, footer below, as `OutputRenderer` already does internally for its own footer. Follow `OutputRenderer`'s footer markup conventions.
- Import `MetricWidget` and `metricConfigFrom`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/QueryDataWidget.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/QueryDataWidget.tsx web/src/components/QueryDataWidget.test.tsx
git commit -m "feat(web): render metric widgets in query widgets"
```

---

### Task 5: Text interpolation util

**Files:**
- Create: `web/src/utils/interpolateText.ts`
- Create: `web/src/utils/interpolateText.test.ts`

**Step 1: Write the failing tests**

```ts
import { describe, expect, it } from 'vitest'
import { interpolateText } from './interpolateText'
import type { DashboardVariable } from '../types'

const vars: DashboardVariable[] = [
  { name: 'region', label: 'Region', type: 'text' },
  { name: 'regions', label: 'Regions', type: 'multi_select' },
  { name: 'window', label: 'Window', type: 'date_range' },
]

describe('interpolateText', () => {
  it('substitutes declared variables', () => {
    expect(interpolateText('## {{region}} overview', { region: 'EMEA' }, vars)).toBe('## EMEA overview')
  })
  it('joins multi-select values and formats date ranges', () => {
    expect(interpolateText('{{regions}}', { regions: ['EMEA', 'AMER'] }, vars)).toBe('EMEA, AMER')
    expect(interpolateText('{{window}}', { window: ['2026-10-01', '2026-10-07'] }, vars)).toBe('2026-10-01 → 2026-10-07')
  })
  it('escapes HTML-significant characters in values', () => {
    expect(interpolateText('{{region}}', { region: '<b>&</b>' }, vars)).toBe('&lt;b&gt;&amp;&lt;/b&gt;')
  })
  it('leaves unknown tokens untouched', () => {
    expect(interpolateText('{{mystery}}', { mystery: 'x' }, vars)).toBe('{{mystery}}')
  })
  it('renders missing values as empty', () => {
    expect(interpolateText('a{{region}}b', {}, vars)).toBe('ab')
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/utils/interpolateText.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement**

```ts
import type { DashboardVariable } from '../types'

function escapeHtml(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function displayValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) {
    // date_range round-trips as [start, end]; everything else joins.
    if (value.length === 2 && typeof value[0] === 'string' && /^\d{4}-\d{2}-\d{2}/.test(value[0])) {
      return `${value[0]} → ${value[1]}`
    }
    return value.map(v => String(v)).join(', ')
  }
  return String(value)
}

// Display-only interpolation for text widgets: declared variables are
// substituted as escaped plain text. Unknown tokens stay visible so authors
// can spot typos; this never becomes a rendering surface because the text
// renderer never enables raw HTML.
export function interpolateText(
  template: string,
  values: Record<string, unknown>,
  variables: DashboardVariable[],
): string {
  const declared = new Set(variables.map(v => v.name))
  return template.replace(/\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}/g, (match, name: string) => {
    if (!declared.has(name)) return match
    return escapeHtml(displayValue(values[name]))
  })
}
```

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/utils/interpolateText.test.ts`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/utils/interpolateText.ts web/src/utils/interpolateText.test.ts
git commit -m "feat(web): text template interpolation for text widgets"
```

---

### Task 6: `TextWidget` component

**Files:**
- Create: `web/src/components/TextWidget.tsx`
- Create: `web/src/components/TextWidget.test.tsx`

**Step 1: Write the failing tests**

```tsx
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TextWidget } from './TextWidget'

describe('TextWidget', () => {
  it('renders markdown headings', () => {
    render(<TextWidget markdown={'## What changed\n\nRevenue is up.'} />)
    expect(screen.getByRole('heading', { name: 'What changed' })).toBeInTheDocument()
  })
  it('renders raw HTML as text, never as markup', () => {
    render(<TextWidget markdown={'<img src=x onerror=alert(1)>'} />)
    expect(screen.queryByRole('img')).toBeNull()
  })
})
```

Wrap renders in the variables provider if `TextWidget` reads context; see Step 3 — to keep the component testable, accept `values`/`variables` via the existing context but allow an override prop. Simplest testable shape: `TextWidget({ markdown, values?, variables? })` defaulting to context.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/TextWidget.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement**

- `textConfigFrom(config: Record<string, unknown> | undefined): { markdown: string }` — reads `config.text.markdown`, defaults `''`.
- Component: `{ markdown: string; values?: Record<string, unknown>; variables?: DashboardVariable[] }`; defaults come from `useDashboardVariables()` when props are absent.
- Render `ReactMarkdown remarkPlugins={[remarkGfm]}` over `interpolateText(markdown, values, variables)`. **No `rehypeRaw`** — raw HTML must render as literal text (wave 1 contract).
- Styling: body type (DM Sans 14px/1.6) via the existing markdown styles used by dashboard text cells (reuse the same class or styles as `DashboardPage`'s text-cell rendering); headings use the existing type scale; `wordBreak: 'break-word'`.
- Empty markdown → muted "Empty text panel — edit it in the dashboard editor."

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/TextWidget.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/TextWidget.tsx web/src/components/TextWidget.test.tsx
git commit -m "feat(web): text widget with markdown and variable interpolation"
```

---

### Task 7: Add-widget picker supports text and metric

**Files:**
- Create: `web/src/utils/addWidgetPayload.ts`
- Create: `web/src/utils/addWidgetPayload.test.ts`
- Modify: `web/src/pages/DashboardEditorPage.tsx` (picker state + payload + UI, ~lines 448–554 and the `addWidget` mutation ~222)

**Step 1: Write the failing tests**

```ts
import { describe, expect, it } from 'vitest'
import { buildAddWidgetPayload } from './addWidgetPayload'

const layout = { row: 0, col: 0, width: 6, height: 8 }

describe('buildAddWidgetPayload', () => {
  it('builds a source-less text widget', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'text', markdown: '## Hi', layout })).toEqual({
      type: 'text',
      layout,
      config: { text: { markdown: '## Hi' } },
    })
  })
  it('builds a query metric widget', () => {
    expect(buildAddWidgetPayload({ source: 'query', type: 'metric', connectorId: 'c1', query: 'SELECT 1', layout })).toEqual({
      type: 'metric',
      layout,
      config: {},
      connector_id: 'c1',
      query: 'SELECT 1',
      language: 'sql',
    })
  })
  it('builds a cell widget as today', () => {
    expect(buildAddWidgetPayload({ source: 'cell', type: 'table', notebookId: 'n1', cellId: 'k1', layout })).toEqual({
      type: 'table',
      layout,
      config: {},
      notebook_id: 'n1',
      cell_id: 'k1',
    })
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/utils/addWidgetPayload.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement the helper**

```ts
export type AddWidgetSource = 'cell' | 'query' | 'none'

export interface AddWidgetInput {
  source: AddWidgetSource
  type: 'table' | 'chart' | 'metric' | 'text'
  layout: { row: number; col: number; width: number; height: number }
  notebookId?: string
  cellId?: string
  connectorId?: string | null
  query?: string
  markdown?: string
}

export function buildAddWidgetPayload(input: AddWidgetInput): Record<string, unknown> {
  const base = { type: input.type, layout: input.layout, config: {} as Record<string, unknown> }
  if (input.source === 'none') {
    return { ...base, type: 'text', config: { text: { markdown: input.markdown ?? '' } } }
  }
  if (input.source === 'query') {
    return { ...base, connector_id: input.connectorId, query: input.query, language: 'sql' }
  }
  return { ...base, notebook_id: input.notebookId, cell_id: input.cellId }
}
```

**Step 4: Wire the editor**

In `DashboardEditorPage.tsx`:
- `pickerSource` type becomes `'cell' | 'query' | 'none'`; add `<option value="none">None — text panel</option>`.
- Widget Type select gains `metric` and `text` options. When `pickerSource === 'none'`, force `pickerType` to `text` (hide the type select or show it disabled) and render a markdown `<textarea>` (`pickerMarkdown` state, `minHeight: 120`).
- `metric` is only valid for the query source; if the type is `metric` and the source is `cell`, treat it as a query-only option in the UI (disable the option or auto-switch — keep it simple: the type select shows all four options, and the Add button stays disabled unless the source/type combination is valid: `none`→`text`; `cell`→`table|chart`; `query`→`table|chart|metric`).
- `addWidget` mutation calls `buildAddWidgetPayload({...})`.
- Reset `pickerMarkdown` on close/success alongside the existing resets.

**Step 5: Run tests + typecheck**

Run: `cd web && npm run test:run -- src/utils/addWidgetPayload.test.ts && npx tsc --noEmit`
Expected: PASS.

**Step 6: Commit**

```bash
git add web/src/utils/addWidgetPayload.ts web/src/utils/addWidgetPayload.test.ts web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): add-widget picker supports text and metric widgets"
```

---

### Task 8: Widget appearance util + `WidgetFrame`

**Files:**
- Create: `web/src/utils/widgetAppearance.ts`
- Create: `web/src/utils/widgetAppearance.test.ts`
- Create: `web/src/components/WidgetFrame.tsx`
- Create: `web/src/components/WidgetFrame.test.tsx`

**Step 1: Write the failing tests**

```ts
// widgetAppearance.test.ts
import { describe, expect, it } from 'vitest'
import { appearanceFrom } from './widgetAppearance'

describe('appearanceFrom', () => {
  it('defaults to today’s card look', () => {
    expect(appearanceFrom(undefined)).toEqual({ background: 'card', border: true, padding: 'none', align: 'left' })
  })
  it('reads explicit values', () => {
    expect(appearanceFrom({ appearance: { background: 'transparent', border: false, padding: 'none', align: 'center' } }))
      .toEqual({ background: 'transparent', border: false, padding: 'none', align: 'center' })
  })
  it('ignores invalid values', () => {
    expect(appearanceFrom({ appearance: { background: 'neon', padding: 'xl' } }).background).toBe('card')
  })
})
```

```tsx
// WidgetFrame.test.tsx
import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { WidgetFrame } from './WidgetFrame'

describe('WidgetFrame', () => {
  it('renders a bordered card by default', () => {
    const { container } = render(<WidgetFrame appearance={{ background: 'card', border: true, padding: 'md', align: 'left' }}>x</WidgetFrame>)
    const el = container.firstElementChild as HTMLElement
    expect(el.style.background).toBe('var(--bg-card)')
    expect(el.style.border).not.toBe('none')
  })
  it('transparent removes background and border', () => {
    const { container } = render(<WidgetFrame appearance={{ background: 'transparent', border: true, padding: 'none', align: 'left' }}>x</WidgetFrame>)
    const el = container.firstElementChild as HTMLElement
    expect(el.style.background).toBe('transparent')
    expect(el.style.border).toBe('none')
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/utils/widgetAppearance.test.ts src/components/WidgetFrame.test.tsx`
Expected: FAIL — modules not found.

**Step 3: Implement**

```ts
// widgetAppearance.ts
export interface WidgetAppearance {
  background: 'card' | 'transparent'
  border: boolean
  padding: 'none' | 'sm' | 'md'
  align: 'left' | 'center' | 'right'
}

const DEFAULTS: WidgetAppearance = { background: 'card', border: true, padding: 'none', align: 'left' }

export function appearanceFrom(config: Record<string, unknown> | undefined): WidgetAppearance {
  const raw = config?.appearance
  if (!raw || typeof raw !== 'object') return DEFAULTS
  const a = raw as Record<string, unknown>
  const pick = <T extends string>(v: unknown, allowed: readonly T[], fallback: T): T =>
    typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
  return {
    background: pick(a.background, ['card', 'transparent'] as const, DEFAULTS.background),
    border: typeof a.border === 'boolean' ? a.border : DEFAULTS.border,
    padding: pick(a.padding, ['none', 'sm', 'md'] as const, DEFAULTS.padding),
    align: pick(a.align, ['left', 'center', 'right'] as const, DEFAULTS.align),
  }
}
```

`WidgetFrame.tsx`: a div applying the appearance — `background: card ? 'var(--bg-card)' : 'transparent'`; `border: border && background === 'card' ? '1px solid var(--border-light)' : 'none'`; padding `none→0, sm→8px, md→16px`; `textAlign: align`; plus `height: '100%'`, `display: 'flex'`, `flexDirection: 'column'`, `overflow: 'hidden'`, `borderRadius: 4`. Children fill the frame.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/utils/widgetAppearance.test.ts src/components/WidgetFrame.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/utils/widgetAppearance.ts web/src/utils/widgetAppearance.test.ts web/src/components/WidgetFrame.tsx web/src/components/WidgetFrame.test.tsx
git commit -m "feat(web): widget appearance model and frame"
```

---

### Task 9: Apply `WidgetFrame` in the viewer and editor

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx` (widget card, ~line 591)
- Modify: `web/src/pages/DashboardEditorPage.tsx` (widget cards, desktop ~625 and mobile ~574)

**Step 1: Apply in the viewer**

In `DashboardPage.tsx`, replace the `styles.widgetCard` wrapper for both query and cell widgets with:

```tsx
<WidgetFrame appearance={appearanceFrom(widget.config)}>
  {/* existing QueryDataWidget / WidgetCard */}
</WidgetFrame>
```

The frame's defaults (`--bg-card`, `--border-light`, 4px radius, no inner padding) match today's card exactly, so wiring the frame changes nothing until an author opts into appearance settings.

**Step 2: Apply in the editor**

Wrap the widget content inside the editor's `dash-widget-card` with the same `WidgetFrame`, so the editor preview shows the authored appearance while drag chrome stays outside the frame.

**Step 2b: Render source-less text widgets**

Both pages currently render every non-query widget as a cell widget, so a source-less `text` widget (created by the Task 7 picker) shows "Cell not found". In both pages, when `widget.type === 'text' && !widget.notebook_id && !widget.connector_id`, render `<TextWidget markdown={textConfigFrom(widget.config).markdown} />` (import `textConfigFrom` from `../utils/textConfig`) inside the frame instead of the cell-output path. Cell-linked text cells keep their existing rendering.

**Step 3: Verify**

Run: `cd web && npm run test:run -- src/components/WidgetFrame.test.tsx src/components/QueryDataWidget.test.tsx src/components/TextWidget.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 4: Commit**

```bash
git add web/src/pages/DashboardPage.tsx web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): apply widget appearance in viewer and editor"
```

---

### Task 10: Config drawer — metric, text, and appearance sections

**Files:**
- Modify: `web/src/components/WidgetConfigDrawer.tsx`
- Modify: `web/src/components/WidgetConfigDrawer.test.tsx`

**Step 1: Write the failing tests**

Extend `WidgetConfigDrawer.test.tsx` (follow its existing API-mocking pattern):
- Selecting the metric type and filling `Value column` + `Aggregation` saves `config.metric` via PUT.
- Editing the text markdown saves `config.text.markdown` via PUT.
- Changing appearance `background` saves `config.appearance.background` via PUT.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/WidgetConfigDrawer.test.tsx`
Expected: FAIL — no such fields.

**Step 3: Implement**

Add a shared `saveWidgetConfig(patch: Record<string, unknown>)` that PUTs `{ config: { ...widget.config, ...patch } }` (query widgets store raw config; cell widgets keep the existing `withWidgetOverride` chart path untouched).

- **Metric section** (`widgetType === 'metric'`): value column (text input with a `<datalist>` populated from `runResult`'s columns when a run has happened), aggregation select, label input, format (style select, currency input shown when currency, decimals number, compact checkbox), comparison (column input, mode select `percent|delta`), sparkline (x column, y column inputs). Each field saves `{ metric: { ...metricConfigFrom(widget.config), <field> } }` on change/blur (debounce text inputs 600ms like the SQL editor).
- **Text section** (`widgetType === 'text'`): markdown `<textarea>` (`minHeight: 160`) saving `{ text: { markdown } }` debounced; a note that `{{variable}}` tokens are interpolated for display.
- **Appearance section** (all types): background select (`Card | Transparent`), border checkbox, padding select (`None | Small | Medium`), align select (`Left | Center | Right`), saving `{ appearance: {...} }` immediately.
- Keep the Visualization select and the existing Source/SQL/Run/preview behavior unchanged.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/WidgetConfigDrawer.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/WidgetConfigDrawer.tsx web/src/components/WidgetConfigDrawer.test.tsx
git commit -m "feat(web): widget config drawer — metric, text, appearance"
```

---

### Task 11: Table column formatting

**Files:**
- Create: `web/src/utils/tableConfig.ts` (+ `tableConfig.test.ts`)
- Modify: `web/src/components/OutputRenderer.tsx` (table cell rendering)
- Modify: `web/src/test/OutputRenderer.test.tsx`
- Modify: `web/src/components/QueryDataWidget.tsx` (pass the formats down)

**Step 1: Write the failing tests**

```ts
// tableConfig.test.ts
import { describe, expect, it } from 'vitest'
import { tableConfigFrom } from './tableConfig'

describe('tableConfigFrom', () => {
  it('reads per-column formats', () => {
    const cfg = tableConfigFrom({ table: { columns: [{ name: 'revenue', format: { style: 'currency', currency: 'USD' } }] } })
    expect(cfg.columns.revenue).toEqual({ style: 'currency', currency: 'USD' })
  })
  it('returns an empty map for missing config', () => {
    expect(tableConfigFrom(undefined).columns).toEqual({})
  })
})
```

Extend `web/src/test/OutputRenderer.test.tsx`: render a table output with `columnFormats={{ revenue: { style: 'currency', currency: 'USD', decimals: 0 } }}` and expect `$1,200` in the cell (numeric value `1200`).

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/utils/tableConfig.test.ts src/test/OutputRenderer.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `tableConfigFrom(config)` → `{ columns: Record<string, ValueFormat> }` (from `config.table.columns[]`, keyed by name).
- `OutputRenderer` gains an optional `columnFormats?: Record<string, ValueFormat>` prop; in `TableOutput`'s cell rendering, when a format exists for the column and the value is a number, render `formatValue(value, format)`; otherwise keep the existing rendering.
- `QueryDataWidget` passes `columnFormats={tableConfigFrom(widget.config).columns}` (harmless for chart/metric renders).

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/utils/tableConfig.test.ts src/test/OutputRenderer.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/utils/tableConfig.ts web/src/utils/tableConfig.test.ts web/src/components/OutputRenderer.tsx web/src/test/OutputRenderer.test.tsx web/src/components/QueryDataWidget.tsx
git commit -m "feat(web): table column formatting"
```

---

## Phase 3 — The App surface (The Story)

### Task 12: `AppHeader` masthead component

**Files:**
- Create: `web/src/components/AppHeader.tsx`
- Create: `web/src/components/AppHeader.test.tsx`

**Step 1: Write the failing tests**

- Renders title and subtitle.
- Hides the subtitle when absent.
- Fires `onRefresh` when the refresh button is clicked; shows a spinner while `refreshing`.
- Shows `Updated {time}` when `lastUpdated` is set and `showLastUpdated` is not false.
- Renders `children` in the actions slot.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/AppHeader.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement**

Props:

```ts
{
  title: string
  subtitle?: string
  width: 'fixed' | 'full'
  showRefresh: boolean
  showLastUpdated: boolean
  lastUpdated: Date | null
  refreshing: boolean
  onRefresh: () => void
  backTo?: string
  children?: React.ReactNode   // right-side actions (Edit, Share, connector, etc.)
}
```

Composition (The Story, first viewport):
- A quiet masthead strip: back link (small icon button, `aria-label="Back to dashboards"`, only when `backTo`), then a title block — title 22px/700 DM Sans (`-0.3px` tracking), subtitle 13px `var(--text-secondary)` — then the right side: freshness (`Updated {HH:MM}` via `formatExecutedAt`, muted 11px mono), refresh button when `showRefresh`, then `children`.
- `width: 'fixed'` → content constrained (`maxWidth: 1200, margin: '0 auto'`); `'full'` → full width. Padding `16px 32px`; bottom hairline `1px solid var(--border-light)`.
- Not sticky (calm instrument, no floating chrome).
- Freshness is `role="status" aria-live="polite"`.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/AppHeader.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AppHeader.tsx web/src/components/AppHeader.test.tsx
git commit -m "feat(web): app header masthead"
```

---

### Task 13: App settings panel in the editor

**Files:**
- Create: `web/src/components/AppSettingsPanel.tsx`
- Create: `web/src/components/AppSettingsPanel.test.tsx`
- Modify: `web/src/pages/DashboardEditorPage.tsx` (header button + panel state)

**Step 1: Write the failing tests**

- Renders current `settings.app` values (title, subtitle, width, toggles).
- Saving PUTs `/api/v1/dashboards/{id}` with `settings.app.title` etc.

**Step 2: Run to verify failure**

Run: `cd web && npm run test:run -- src/components/AppSettingsPanel.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- A right-side drawer (reuse `WidgetConfigDrawer`'s drawer chrome: backdrop, focus trap, Escape close) with fields: `App title` (placeholder: the dashboard title), `Subtitle`, `Width` (`Fixed | Full width`), checkboxes `Show header`, `Show refresh`, `Show last updated`.
- Save: `api.put('/api/v1/dashboards/{id}', { settings: { ...dashboard.settings, app: { ...dashboard.settings.app, ...draft } } })`, then invalidate `['dashboard', id]`.
- Note in the panel: "Logo and fonts arrive with org branding (next wave)."
- Editor header: add an "App" button (icon `LayoutTemplate` or `Monitor`) next to the Variables button, opening the panel. Hidden on mobile like Permissions.

**Step 4: Run to verify pass**

Run: `cd web && npm run test:run -- src/components/AppSettingsPanel.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AppSettingsPanel.tsx web/src/components/AppSettingsPanel.test.tsx web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(web): app settings panel in the dashboard editor"
```

---

### Task 14: Bare shell + the App surface composition

**Files:**
- Modify: `web/src/components/AppShell.tsx` (add `bare?: boolean`)
- Modify: `web/src/pages/DashboardPage.tsx` (composition)
- Modify: `web/src/components/QueryDataWidget.tsx` (add `onFetched?: () => void`)
- Modify: `web/src/components/DashboardVariableBar.tsx` (quiet strip styling hook, if needed)

**Step 1: Add the bare shell**

In `AppShell.tsx`, add `bare?: boolean` to `Props`. When `bare` is true, return `<>{children}</>` before any shell chrome (no TopBar, no Sidebar, no MOTD/agent panel). Keep all hooks above the early return to satisfy the rules of hooks — the cleanest spot is after the existing hook declarations and before the shell render; verify no hook sits between (the current render is a single return at the end).

**Step 2: Compose the App surface in `DashboardPage.tsx`**

- Wrap the page in `<AppShell noPadding bare>`.
- Replace the dark sub-header with `<AppHeader>`:
  - `title={settings.app?.title || dashboard.title}`; `subtitle={settings.app?.subtitle}`.
  - `width={settings.app?.width ?? 'fixed'}`.
  - `showRefresh={settings.app?.show_refresh !== false}`; `showLastUpdated={settings.app?.show_last_updated !== false}`.
  - `showHeader === false` → render only a minimal bar with the back link and actions (no title block).
  - `lastUpdated` — new `lastFetchedAt` state; pass `onFetched={() => setLastFetchedAt(new Date())}` to `QueryDataWidget` (call it in the existing `useEffect` that sets `fetchedAt`) and set it after cell-widget executions in `executeAllWidgets`.
  - `refreshing={refreshingCount > 0}`; `onRefresh={() => executeAllWidgets(widgets)}`.
  - `backTo="/dashboards"`.
  - Actions slot (children): connector selector (viewers), Edit link, Share button, and for editors the auto-refresh select (moved from the old header). Keep the "Refreshing n/m / Updated" status in the actions slot or the freshness line — one signal, not two.
- Filter strip: `{variables.length > 0 && <div className="app-filter-strip"><DashboardVariableBar /></div>}` — a subtle strip (`background: var(--bg-panel)` if available, else `transparent`; bottom hairline; `padding: 8px 32px`), constrained to the same width as the masthead.
- Body: `padding: '24px 32px'`, `gap: 24`, constrained to `maxWidth: 1200` when width is fixed; widgets wrapped in `WidgetFrame` (Task 9).
- Empty state: keep the existing `EmptyState` but update copy: "This app has no widgets yet" + "Add widgets in the dashboard editor."

**Step 3: Verify**

Run: `cd web && npx tsc --noEmit && npm run test:run -- src/components`
Expected: PASS (no regressions in component tests).

**Step 4: Real-browser validation (mandatory)**

With the dev stack up (`docker compose -f docker-compose.dev.yml up -d`), open `http://localhost:5173`, sign in as `nova@heaven-labs.com` / `nova123`, open a dashboard with query widgets, and check `/dashboards/:id/view`: bare shell (no rail), masthead, filter strip, refresh, freshness updates after a refresh; light and dark themes.

**Step 5: Commit**

```bash
git add web/src/components/AppShell.tsx web/src/pages/DashboardPage.tsx web/src/components/QueryDataWidget.tsx web/src/components/DashboardVariableBar.tsx
git commit -m "feat(web): full-bleed app surface (The Story)"
```

---

### Task 15: Public page parity

**Files:**
- Modify: `web/src/pages/PublicDashboardPage.tsx`

**Step 1: Implement**

- Header: use `settings.app?.title || dashboard.title` and `settings.app?.subtitle` in the public masthead (keep the `Live`/`Read-only` badge and the "Powered by Aether" footer).
- `visibleWidgets`: keep query widgets gated on `public_live` and cell widgets gated on stored outputs; additionally include source-less text widgets (`!widget.connector_id && !widget.notebook_id`) rendered through `TextWidget` with `textConfigFrom(widget.config).markdown` (public viewers get the interpolated markdown with the public variable values).
- Metric rendering is inherited from `QueryDataWidget` (Task 4); nothing extra to do.
- Width: honor `settings.app?.width` in the public layout container.

**Step 2: Verify**

Run: `cd web && npx tsc --noEmit`
Expected: PASS.

**Step 3: Commit**

```bash
git add web/src/pages/PublicDashboardPage.tsx
git commit -m "feat(web): app header, text widgets, and width on public dashboards"
```

---

### Task 16: E2E + visual snapshot

**Files:**
- Create: `e2e/dashboard-app.spec.ts`

**Step 1: Write the spec**

Follow `e2e/dashboard-filters.spec.ts`'s helpers (it creates a Postgres connector and a dashboard with a query widget). Scenario:

1. Create a dashboard; add a query widget of type **metric** with `SELECT 42 AS revenue, 40 AS prev` and config `metric: { value_column: 'revenue', aggregation: 'none', comparison: { column: 'prev', mode: 'percent' } }` (via API or the picker).
2. Add a **text** widget with markdown `## What changed\n\nRevenue is up.` via the picker (source "None").
3. Open the App settings panel and set title `Executive Brief`, subtitle `Sales ops`.
4. Go to `/dashboards/{id}/view` and assert: no sidebar (`page.locator('nav')` hidden / rail absent), masthead title + subtitle visible, metric `42` and `+5.0%` visible, the text heading visible, refresh button visible.
5. Capture `await expect(page).toHaveScreenshot('dashboard-app.png')`.

**Step 2: Run**

```bash
npx playwright test --config=e2e/playwright.config.ts e2e/dashboard-app.spec.ts --update-snapshots
npx playwright test --config=e2e/playwright.config.ts e2e/dashboard-app.spec.ts
```
Expected: PASS; snapshot committed.

**Step 3: Commit**

```bash
git add e2e/dashboard-app.spec.ts e2e/snapshots
git commit -m "test(e2e): app surface with metric and text widgets"
```

---

### Task 17: Docs + full verification

**Files:**
- Modify: `FRONTEND.md` (dashboard section: app surface, metric/text/appearance)
- Modify: `DESIGN.md` (durable additions: the reading type role — JetBrains Mono tabular for metric values; the widget appearance vocabulary)
- Modify: `AGENTS.md` (dashboard paragraph: app surface + `settings.app` + widget kit)

**Step 1: Update docs**

Keep each edit tight; follow the existing section styles. In DESIGN.md, add the reading role under Typography and the appearance vocabulary under Components, both explicitly marked as approved 2026-10-11.

**Step 2: Full verification**

```bash
task check
cd web && npx tsc --noEmit && npm run build && npm run test:run
cd relay && npm run build   # untouched, sanity only
npx playwright test --config=e2e/playwright.config.ts
```

**Step 3: Commit**

```bash
git add FRONTEND.md DESIGN.md AGENTS.md
git commit -m "docs: app surface, reading role, and widget appearance"
```

---

## Out of scope (later plans)

- Wave 2: tabs, `dashboard_versions` + publish, org/dashboard theming.
- Wave 3: sanitized HTML/CSS panel (`html` widget, DOMPurify, sandboxed frame).
- Wave 4: sandboxed custom components/pages, distribution (embed/presentation/print), agent tooling.
