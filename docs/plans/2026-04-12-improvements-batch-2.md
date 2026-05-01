# Improvements Batch 2 — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement 14 remaining improvements from IMPROVEMENTS.md: profile save feedback, presentation anchors, nav rename, chart persistence, chart UX redesign (labels + colors), filesystem recent + search, cell type history + audit filtering, dashboard widget execution + auto-refresh, "Everyone" group, and dark theme audit.

**Architecture:** Mostly frontend (`web/src/`). Backend additions: `resource_id` audit filter, cell-type history logging, dashboard execute endpoint, everyone-group seed in org creation. All routes use `net/http` ServeMux. React Query for data fetching. CSS vars for theming.

**Working directory:** `/home/jesus/Projects/hnb-claude`

**Run frontend tests:** `cd web && npm run test:run`
**Run backend tests:** `task test:api`
**Dev stack:** `docker compose -f docker-compose.dev.yml up`

**Tech Stack:** Go (backend), React + TypeScript + React Query (frontend), Vitest + Testing Library (tests), Recharts (charts)

---

### Task 1: Profile page save feedback (#20)

**Items addressed:** Profile page saving doesn't give user feedback.

**Files:**
- Modify: `web/src/pages/ProfilePage.tsx`

**Step 1: Add success/error state to the mutation**

In `ProfilePage.tsx`, the `update` mutation (line 40) has no `onError` and `onSuccess` doesn't show UI feedback. Add a `saveStatus` state and update it in mutation callbacks.

Replace the `update` mutation and add state:
```tsx
const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')

const update = useMutation({
  mutationFn: (patch: Partial<UserProfile>) => api.put('/api/v1/users/me', patch),
  onMutate: () => setSaveStatus('saving'),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['profile'] })
    setSaveStatus('saved')
    setTimeout(() => setSaveStatus('idle'), 2500)
  },
  onError: () => {
    setSaveStatus('error')
    setTimeout(() => setSaveStatus('idle'), 3000)
  },
})
```

**Step 2: Add feedback UI below the Save button**

Replace the existing save button `<div>` (lines 91-95) with:
```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 }}>
  <button
    type="button"
    style={{ ...styles.saveBtn, opacity: saveStatus === 'saving' ? 0.6 : 1 }}
    onClick={handleSave}
    disabled={saveStatus === 'saving'}
  >
    {saveStatus === 'saving' ? 'Saving…' : 'Save'}
  </button>
  {saveStatus === 'saved' && (
    <span style={{ fontSize: 13, color: 'var(--success)', fontWeight: 500 }}>Saved</span>
  )}
  {saveStatus === 'error' && (
    <span style={{ fontSize: 13, color: 'var(--error)', fontWeight: 500 }}>Save failed</span>
  )}
</div>
```

**Step 3: Run frontend tests**
```bash
cd web && npm run test:run 2>&1 | tail -20
```
Expected: all pass (no ProfilePage tests exist yet, this is purely additive).

**Step 4: Commit**
```bash
git add web/src/pages/ProfilePage.tsx
git commit -m "feat: save feedback on profile page (#20)"
```

---

### Task 2: Remove markdown anchors from presentation view (#23)

**Items addressed:** Markdown anchors (the `#` link on hover) appear in presentation view even though they were removed from notebook view.

**Context:** `PresentationPage.tsx` defines its own `markdownComponents` (lines 15-76) with anchor-link renderers for h1-h6. These should be removed — presentation view should render clean headings.

**Files:**
- Modify: `web/src/pages/PresentationPage.tsx`

**Step 1: Remove `markdownComponents` definition and usage**

Delete lines 8-76 (the `slugify` function and entire `markdownComponents` object). Then on line 131, change:
```tsx
<ReactMarkdown components={markdownComponents}>{cell.source}</ReactMarkdown>
```
to:
```tsx
<ReactMarkdown>{cell.source}</ReactMarkdown>
```

Also remove the now-unused `headerAnchor` style entry (line 206-213) and the `slugify` import if it was imported from elsewhere (it wasn't — it was defined locally, so deleting lines 8-13 is sufficient).

**Step 2: Run frontend tests**
```bash
cd web && npm run test:run 2>&1 | tail -10
```

**Step 3: Commit**
```bash
git add web/src/pages/PresentationPage.tsx
git commit -m "fix: remove markdown header anchors from presentation view (#23)"
```

---

### Task 3: Fix confusing navigation — rename "Notebooks" to "Home" (#7)

**Items addressed:** The "Notebooks" sidebar link goes to `/` which shows notebooks, dashboards, AND connectors — making it confusing since there are also separate Dashboards and Connectors links.

**Context:** `Sidebar.tsx` NAV_ITEMS has `{ to: '/', title: 'Notebooks', ... }`. The home page is a filesystem browser showing all resource types. Renaming it to "Home" or "Files" clarifies that it's the filesystem root, not a filtered notebooks-only view.

**Files:**
- Modify: `web/src/components/Sidebar.tsx`

**Step 1: Rename the nav item and change icon**

In `Sidebar.tsx`, find the NAV_ITEMS array. Change the first entry:
```tsx
{ to: '/', title: 'Home', icon: <Home size={16} /> },
```

Import `Home` from lucide-react (add to the existing import). Remove `BookOpen` if it's only used for this entry.

**Step 2: Commit**
```bash
git add web/src/components/Sidebar.tsx
git commit -m "fix: rename Notebooks nav to Home to reflect filesystem view (#7)"
```

---

### Task 4: Persist chart/table view selection per cell (#21)

**Items addressed:** When a chart is selected in a notebook, navigating away and back resets the view to table.

**Context:** `OutputRenderer.tsx` `TableOutput` component (line 115) initializes `view` as local state defaulting to `'table'`. This resets on unmount. Fix: use `localStorage` keyed by cell ID to remember the last view. The cell ID must be threaded from `NotebookPage` → `OutputRenderer` → `TableOutput`.

**Files:**
- Modify: `web/src/components/OutputRenderer.tsx`
- Modify: `web/src/pages/NotebookPage.tsx` (find where OutputRenderer is called)

**Step 1: Find how OutputRenderer is called in NotebookPage**
```bash
grep -n "OutputRenderer\|cellId\|cell\.id" web/src/pages/NotebookPage.tsx | head -20
```

**Step 2: Add optional `cellId` prop to `OutputRenderer`**

In `OutputRenderer.tsx`, update the `Props` interface:
```tsx
interface Props {
  outputs: Output[]
  fixedView?: 'table' | 'chart'
  cellId?: string
}
```

Pass it through to `OutputItem`:
```tsx
{outputs.map((out, i) => (
  <OutputItem key={i} output={out} fixedView={fixedView} cellId={cellId} />
))}
```

**Step 3: Pass `cellId` down in `OutputItem` → `TableOutput`**

Update `OutputItem` signature: `function OutputItem({ output, fixedView, cellId }: { output: Output; fixedView?: 'table' | 'chart'; cellId?: string })`

Pass to `TableOutput`:
```tsx
return <TableOutput rs={rs} fixedView={fixedView} cellId={cellId} />
```

**Step 4: Use localStorage in `TableOutput` to persist view**

In `TableOutput`, replace the `useState` initialization:
```tsx
function TableOutput({ rs, fixedView, cellId }: { rs: ResultSet; fixedView?: 'table' | 'chart'; cellId?: string }) {
  const storageKey = cellId ? `hnb_cell_view_${cellId}` : null
  const [view, setView] = useState<'table' | 'chart'>(() => {
    if (fixedView) return fixedView
    if (storageKey) {
      const saved = localStorage.getItem(storageKey)
      if (saved === 'chart' || saved === 'table') return saved
    }
    return 'table'
  })

  const handleViewChange = (v: 'table' | 'chart') => {
    setView(v)
    if (storageKey) localStorage.setItem(storageKey, v)
  }
```

Replace the two `onClick={() => setView('table')}` and `onClick={() => setView('chart')}` calls with `onClick={() => handleViewChange('table')}` and `onClick={() => handleViewChange('chart')}`.

**Step 5: Pass cellId from NotebookPage**

Find where `OutputRenderer` is used in `NotebookPage.tsx` (grep step 1 result). Pass `cellId={cell.id}`.

**Step 6: Run tests**
```bash
cd web && npm run test:run 2>&1 | tail -10
```

**Step 7: Commit**
```bash
git add web/src/components/OutputRenderer.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: persist chart/table view selection in localStorage per cell (#21)"
```

---

### Task 5: Chart config redesign — labels, colors, better UX (#6, #22)

**Items addressed:** #6 — add show-labels and custom color options. #22 — chart config panel has poor UX (raw selects).

**Context:** `ChartConfigPanel.tsx` is a minimal grid form. `ChartView.tsx` uses a fixed `COLORS` array and ignores labels. Redesign the panel to look like Grafana's approach: visual chart-type selector (icons), series color pickers, and a labels toggle.

**Files:**
- Modify: `web/src/components/ChartConfigPanel.tsx`
- Modify: `web/src/components/ChartView.tsx`

**Step 1: Extend `ChartConfig` interface in `ChartConfigPanel.tsx`**

```tsx
export interface ChartConfig {
  chartType: 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut'
  xAxis: string
  yAxis: string[]
  title?: string
  showLegend?: boolean
  showGrid?: boolean
  showLabels?: boolean
  seriesColors?: Record<string, string>  // series name → hex color
}
```

**Step 2: Redesign `ChartConfigPanel.tsx` with visual type picker, color pickers, toggles**

Replace the entire component body:

```tsx
const CHART_TYPES: { value: ChartConfig['chartType']; label: string; icon: string }[] = [
  { value: 'bar',         label: 'Bar',         icon: '▊' },
  { value: 'stacked_bar', label: 'Stacked',     icon: '▊' },
  { value: 'line',        label: 'Line',        icon: '╱' },
  { value: 'area',        label: 'Area',        icon: '◣' },
  { value: 'scatter',     label: 'Scatter',     icon: '⠿' },
  { value: 'pie',         label: 'Pie',         icon: '◕' },
  { value: 'donut',       label: 'Donut',       icon: '◎' },
]

const DEFAULT_COLORS = ['#6366f1', '#22d3ee', '#f59e0b', '#10b981', '#ef4444', '#8b5cf6', '#ec4899']

export function ChartConfigPanel({ config, columns, onChange }: ChartConfigPanelProps) {
  return (
    <div style={styles.panel}>
      {/* Chart type — visual tile picker */}
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <div style={styles.typeGrid}>
          {CHART_TYPES.map(ct => (
            <button
              key={ct.value}
              type="button"
              title={ct.label}
              onClick={() => onChange({ ...config, chartType: ct.value })}
              style={{ ...styles.typeBtn, ...(config.chartType === ct.value ? styles.typeBtnActive : {}) }}
            >
              <span style={{ fontSize: 18 }}>{ct.icon}</span>
              <span style={{ fontSize: 10, marginTop: 2 }}>{ct.label}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Axes */}
      <div style={styles.section}>
        <div style={styles.sectionLabel}>X axis</div>
        <select style={styles.select} value={config.xAxis}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>

      <div style={styles.section}>
        <div style={styles.sectionLabel}>Y axis <span style={{ fontWeight: 400 }}>(hold Ctrl to multi-select)</span></div>
        <select style={{ ...styles.select, minHeight: 64 }} multiple
          value={config.yAxis}
          onChange={e => {
            const selected = Array.from(e.target.selectedOptions).map(o => o.value)
            onChange({ ...config, yAxis: selected })
          }}>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>

      {/* Series colors */}
      {config.yAxis?.length > 0 && (
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Series colors</div>
          {config.yAxis.map((series, i) => {
            const currentColor = config.seriesColors?.[series] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]
            return (
              <div key={series} style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                <input
                  type="color"
                  value={currentColor}
                  onChange={e => onChange({
                    ...config,
                    seriesColors: { ...config.seriesColors, [series]: e.target.value }
                  })}
                  style={{ width: 28, height: 22, padding: 0, border: '1px solid var(--border)', borderRadius: 3, cursor: 'pointer', background: 'none' }}
                />
                <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{series}</span>
              </div>
            )
          })}
        </div>
      )}

      {/* Toggles */}
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Display options</div>
        {[
          { key: 'showGrid',    label: 'Grid lines',   default: true },
          { key: 'showLegend',  label: 'Legend',       default: true },
          { key: 'showLabels',  label: 'Data labels',  default: false },
        ].map(opt => (
          <label key={opt.key} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, cursor: 'pointer', marginBottom: 4 }}>
            <input
              type="checkbox"
              checked={config[opt.key as keyof ChartConfig] as boolean ?? opt.default}
              onChange={e => onChange({ ...config, [opt.key]: e.target.checked })}
              style={{ width: 14, height: 14, accentColor: 'var(--accent)', cursor: 'pointer' }}
            />
            {opt.label}
          </label>
        ))}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    borderTop: '1px solid var(--border)',
    padding: '12px 14px',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    background: 'var(--bg-secondary)',
  },
  section: { display: 'flex', flexDirection: 'column', gap: 6 },
  sectionLabel: { fontSize: 10, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.07em' },
  typeGrid: { display: 'flex', flexWrap: 'wrap', gap: 4 },
  typeBtn: {
    display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
    width: 52, height: 44, border: '1px solid var(--border)', borderRadius: 4,
    background: 'var(--bg-card)', cursor: 'pointer', color: 'var(--text-secondary)',
    padding: 4,
  },
  typeBtnActive: {
    background: 'var(--accent-light)', borderColor: 'var(--accent)', color: 'var(--accent)',
  },
  select: {
    fontSize: 12, border: '1px solid var(--border)', borderRadius: 4,
    padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)',
    width: '100%',
  },
}
```

**Step 3: Update `ChartView.tsx` to use `seriesColors` and `showLabels`**

Import `LabelList` from recharts:
```tsx
import {
  BarChart, Bar, LabelList, LineChart, Line, AreaChart, Area,
  ScatterChart, Scatter, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts'
```

Replace the `COLORS` constant and color usage. In `ChartView`, compute effective colors:
```tsx
const getColor = (seriesName: string, index: number): string => {
  return effectiveConfig.seriesColors?.[seriesName] ?? COLORS[index % COLORS.length]
}
```

Replace all `fill={COLORS[i % COLORS.length]}` and `stroke={COLORS[i % COLORS.length]}` with `fill={getColor(y, i)}` / `stroke={getColor(y, i)}`.

Add label list to bar chart series when `showLabels`:
```tsx
{effectiveYAxes.map((y, i) => (
  <Bar key={y} dataKey={y} fill={getColor(y, i)} radius={[3, 3, 0, 0]}>
    {effectiveConfig.showLabels && <LabelList dataKey={y} position="top" style={{ fontSize: 10, fill: 'var(--text-muted)' }} />}
  </Bar>
))}
```

Do the same for Line and Area series (position="top" on LabelList for lines).

Also update the "Configure" button to look more intentional — replace the plain text button with an icon button:
```tsx
import { Settings2 } from 'lucide-react'
// ...
<button
  style={styles.configBtn}
  onClick={() => setShowConfig(v => !v)}
  aria-label={showConfig ? 'Close chart config' : 'Configure chart'}
  title="Configure chart"
>
  <Settings2 size={14} style={{ marginRight: 4 }} />
  {showConfig ? 'Close' : 'Configure'}
</button>
```

**Step 4: Run tests**
```bash
cd web && npm run test:run 2>&1 | tail -10
```

**Step 5: Commit**
```bash
git add web/src/components/ChartConfigPanel.tsx web/src/components/ChartView.tsx
git commit -m "feat: redesign chart config panel — visual type picker, color pickers, data labels (#6, #22)"
```

---

### Task 6: Filesystem recent section + search (#1, #2)

**Items addressed:** #1 — "recent" section in filesystem view. #2 — search by name.

**Context:** `HomePage.tsx` shows a folder-based file browser. We need: (a) a "Recent" section at the top of the root view showing the 5 most recently updated resources across all types, (b) a search bar that filters visible items by name (client-side for now since all items are already loaded).

The recent items can come from a new backend endpoint `GET /api/v1/recent` which queries notebooks, dashboards, and connectors ordered by `updated_at DESC LIMIT 5`. Search can be purely frontend filtering on the `FolderContents` data.

**Files:**
- Create: `internal/api/recent_handlers.go`
- Modify: `internal/api/server.go` (register route)
- Modify: `web/src/pages/HomePage.tsx`
- Create: `internal/api/recent_handlers_test.go`

**Step 1: Write the failing backend test**

Create `internal/api/recent_handlers_test.go`:
```go
package api_test

import (
  "net/http"
  "testing"
)

func TestGetRecent(t *testing.T) {
  ts := setupTestServer(t)

  // Create a notebook (creates resource to appear in recent)
  nb := createNotebook(t, ts, "Recent NB")

  resp := ts.get(t, "/api/v1/recent")
  if resp.StatusCode != http.StatusOK {
    t.Fatalf("expected 200, got %d", resp.StatusCode)
  }

  var items []map[string]any
  decodeJSON(t, resp, &items)
  if len(items) == 0 {
    t.Fatal("expected at least one recent item")
  }
  // Should have type, id, name/title, updated_at fields
  first := items[0]
  if _, ok := first["type"]; !ok {
    t.Error("missing type field")
  }
  if _, ok := first["id"]; !ok {
    t.Error("missing id field")
  }
  _ = nb
}
```

**Step 2: Run test to verify it fails**
```bash
task test:api 2>&1 | grep -E "FAIL|recent" | head -5
```
Expected: FAIL (handler doesn't exist yet).

**Step 3: Create `internal/api/recent_handlers.go`**

```go
package api

import (
  "net/http"
)

type recentItem struct {
  ID        string `json:"id"`
  Type      string `json:"type"`
  Name      string `json:"name"`
  UpdatedAt string `json:"updated_at"`
}

func (s *Server) handleGetRecent(w http.ResponseWriter, r *http.Request) {
  claims := ClaimsFromContext(r.Context())
  ctx := r.Context()

  rows, err := s.db.Pool.Query(ctx, `
    SELECT id::text, 'notebook' AS type, title AS name, updated_at
    FROM notebooks WHERE org_id = $1
    UNION ALL
    SELECT id::text, 'dashboard', title, updated_at
    FROM dashboards WHERE org_id = $1
    UNION ALL
    SELECT id::text, 'connector', name, updated_at
    FROM connectors WHERE org_id = $1
    ORDER BY updated_at DESC
    LIMIT 10
  `, claims.OrgID)
  if err != nil {
    writeError(w, http.StatusInternalServerError, "query failed")
    return
  }
  defer rows.Close()

  items := []recentItem{}
  for rows.Next() {
    var item recentItem
    var updatedAt interface{}
    if err := rows.Scan(&item.ID, &item.Type, &item.Name, &updatedAt); err != nil {
      continue
    }
    if t, ok := updatedAt.(interface{ Format(string) string }); ok {
      item.UpdatedAt = t.Format("2006-01-02T15:04:05Z07:00")
    }
    items = append(items, item)
  }
  writeJSON(w, http.StatusOK, items)
}
```

**Step 4: Register the route in `server.go`**

Find where routes are registered (look for `mux.HandleFunc` patterns). Add:
```go
mux.HandleFunc("GET /api/v1/recent", auth(s.handleGetRecent))
```

**Step 5: Run backend tests**
```bash
task test:api 2>&1 | grep -E "PASS|FAIL|recent" | head -10
```
Expected: PASS.

**Step 6: Add recent section + search bar to `HomePage.tsx`**

At the top of the `HomePage` component, add:
```tsx
const [searchQuery, setSearchQuery] = useState('')

const { data: recentItems = [] } = useQuery<Array<{
  id: string; type: string; name: string; updated_at: string
}>>({
  queryKey: ['recent'],
  queryFn: () => api.get('/api/v1/recent'),
  enabled: !currentFolderId, // only fetch on root
})
```

Add a search bar in the page header area (above the file list):
```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
  <input
    style={searchStyles.input}
    value={searchQuery}
    onChange={e => setSearchQuery(e.target.value)}
    placeholder="Search files…"
  />
  {searchQuery && (
    <button style={searchStyles.clearBtn} onClick={() => setSearchQuery('')}>✕</button>
  )}
</div>
```

Add a "Recent" section that appears only at root level when not searching:
```tsx
{!currentFolderId && !searchQuery && recentItems.length > 0 && (
  <div style={{ marginBottom: 24 }}>
    <div style={sectionLabelStyle}>Recent</div>
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
      {recentItems.slice(0, 5).map(item => (
        <RecentChip key={item.id} item={item} />
      ))}
    </div>
  </div>
)}
```

`RecentChip` is a small inline component that navigates to the appropriate page on click:
```tsx
function RecentChip({ item }: { item: { id: string; type: string; name: string } }) {
  const navigate = useNavigate()
  const href = item.type === 'notebook' ? `/notebooks/${item.id}`
             : item.type === 'dashboard' ? `/dashboards/${item.id}`
             : `/connectors`
  const icon = item.type === 'notebook' ? <BookOpen size={12} />
             : item.type === 'dashboard' ? <LayoutDashboard size={12} />
             : <Database size={12} />
  return (
    <button
      style={chipStyle}
      onClick={() => navigate(href)}
      title={item.type}
    >
      {icon}
      <span style={{ marginLeft: 4 }}>{item.name}</span>
    </button>
  )
}
```

**Step 7: Filter the folder contents list by search query**

When `searchQuery` is non-empty, filter the displayed `contents` (notebooks, dashboards, connectors, folders) by name:
```tsx
const filteredContents = useMemo(() => {
  if (!searchQuery.trim() || !contents) return contents
  const q = searchQuery.toLowerCase()
  return {
    ...contents,
    folders: contents.folders?.filter(f => f.name.toLowerCase().includes(q)) ?? [],
    notebooks: contents.notebooks?.filter(n => n.title.toLowerCase().includes(q)) ?? [],
    dashboards: contents.dashboards?.filter(d => d.title.toLowerCase().includes(q)) ?? [],
    connectors: contents.connectors?.filter(c => c.name.toLowerCase().includes(q)) ?? [],
  }
}, [contents, searchQuery])
```

Use `filteredContents` instead of `contents` when rendering.

**Step 8: Run tests**
```bash
task test:api 2>&1 | grep -E "PASS|FAIL|ok" | tail -10
cd web && npm run test:run 2>&1 | tail -10
```

**Step 9: Commit**
```bash
git add internal/api/recent_handlers.go internal/api/recent_handlers_test.go internal/api/server.go web/src/pages/HomePage.tsx
git commit -m "feat: filesystem recent section and name search (#1, #2)"
```

---

### Task 7: Cell type change history + audit trail filtering (#17, #18)

**Items addressed:** #17 — cell type changes not saved in history. #18 — no easy way to track cell updates in audit trail.

**Context:** 
- `handleUpdateCell` in `cell_handlers.go` (line 106) never calls `upsertCellVersion` when `req.Type` is set. Fix: when cell type changes, log a version with a note.
- `audit.QueryParams` doesn't have a `ResourceID` field. Add it so the audit page can filter by specific cell/resource.
- The audit UI only has an action filter. Add a resource_type dropdown filter.

**Files:**
- Modify: `internal/api/cell_handlers.go`
- Modify: `internal/audit/audit.go`
- Modify: `internal/api/audit_handlers.go`
- Modify: `web/src/pages/AuditPage.tsx`

**Step 1: Log cell type change in `handleUpdateCell`**

After the `UPDATE cells SET ...` query in `handleUpdateCell`, find where `upsertCellVersion` is NOT called for type changes. Currently it's only called when `req.Source != nil`. Add a type-change log:

After the DB update executes successfully (look for the existing `if err != nil` check after the update), add:
```go
// Log cell type change to version history
if req.Type != nil {
  typeNote := fmt.Sprintf("[type changed to %s]", *req.Type)
  _ = s.upsertCellVersion(ctx, cellID, typeNote)
  s.audit.Log(ctx, audit.Entry{
    OrgID: claims.OrgID, UserID: claims.UserID,
    Action: "cell.type_change", ResourceType: "cell", ResourceID: cellID,
    Metadata: map[string]any{"new_type": *req.Type},
  })
}
```

Also add audit logging for source updates (currently missing in handleUpdateCell — only cell.create is logged):
```go
if req.Source != nil {
  s.audit.Log(ctx, audit.Entry{
    OrgID: claims.OrgID, UserID: claims.UserID,
    Action: "cell.update", ResourceType: "cell", ResourceID: cellID,
  })
}
```

**Step 2: Write failing test for cell type change history**

In `internal/api/cell_handlers_test.go` (or create if needed), add:
```go
func TestCellTypeChangeHistory(t *testing.T) {
  ts := setupTestServer(t)
  nb := createNotebook(t, ts, "Test NB")
  cell := createCell(t, ts, nb.ID, "code", "SELECT 1")

  // Change type to text
  body := map[string]any{"type": "text"}
  resp := ts.put(t, fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nb.ID, cell.ID), body)
  if resp.StatusCode != http.StatusOK {
    t.Fatalf("expected 200, got %d", resp.StatusCode)
  }

  // Check version history
  resp = ts.get(t, fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nb.ID, cell.ID))
  var versions []map[string]any
  decodeJSON(t, resp, &versions)
  found := false
  for _, v := range versions {
    if src, _ := v["source"].(string); strings.Contains(src, "type changed") {
      found = true
    }
  }
  if !found {
    t.Error("expected a version entry for type change")
  }
}
```

**Step 3: Run test to verify it fails first**
```bash
task test:api 2>&1 | grep -E "FAIL|TestCellTypeChange" | head -5
```

**Step 4: Add `ResourceID` filter to `audit.QueryParams`**

In `internal/audit/audit.go`, add to `QueryParams`:
```go
type QueryParams struct {
  OrgID        string
  UserID       string
  Action       string
  ResourceType string
  ResourceID   string  // NEW
  Limit        int
  Offset       int
}
```

In the `Query` method, after the `ResourceType` block, add:
```go
if p.ResourceID != "" {
  query += fmt.Sprintf(" AND al.resource_id = $%d", argN)
  args = append(args, p.ResourceID)
  argN++
}
```

**Step 5: Pass `resource_id` in `audit_handlers.go`**

In `handleListAuditLogs`, add to `params`:
```go
ResourceID: q.Get("resource_id"),
```

**Step 6: Add resource_type filter dropdown and action filter to audit UI**

In `AuditPage.tsx`, add a `resourceTypeFilter` state:
```tsx
const [resourceTypeFilter, setResourceTypeFilter] = useState('')
```

Add a dropdown beside the action filter input:
```tsx
<select
  style={{ ...styles.filterInput, maxWidth: 160 }}
  value={resourceTypeFilter}
  onChange={e => { setResourceTypeFilter(e.target.value); setOffset(0); setEntries([]) }}
>
  <option value="">All types</option>
  <option value="notebook">Notebook</option>
  <option value="cell">Cell</option>
  <option value="dashboard">Dashboard</option>
  <option value="connector">Connector</option>
  <option value="user">User</option>
</select>
```

Pass the filter to the API query:
```tsx
const { data: page, ... } = useQuery({
  queryKey: ['audit', offset, resourceTypeFilter],
  queryFn: () => {
    const params = new URLSearchParams({
      limit: String(PAGE_SIZE),
      offset: String(offset),
    })
    if (resourceTypeFilter) params.set('resource_type', resourceTypeFilter)
    return api.get<AuditEntry[]>(`/api/v1/audit?${params}`)
  },
})
```

Remove the client-side `actionFilter` (it filters after fetch — replace with server-side `action` param too):
```tsx
const [actionFilter, setActionFilter] = useState('')
// ... in queryKey: ['audit', offset, resourceTypeFilter, actionFilter]
// ... in queryFn: if (actionFilter) params.set('action', actionFilter)
// Remove the client-side `filtered` variable — use `entries` directly
```

**Step 7: Run backend and frontend tests**
```bash
task test:api 2>&1 | grep -E "PASS|FAIL|ok" | tail -10
cd web && npm run test:run 2>&1 | tail -10
```

**Step 8: Commit**
```bash
git add internal/api/cell_handlers.go internal/audit/audit.go internal/api/audit_handlers.go web/src/pages/AuditPage.tsx
git commit -m "feat: cell type change history, audit resource_type/action server-side filter (#17, #18)"
```

---

### Task 8: Dashboard widget auto-execution + auto-refresh (#10, #15)

**Items addressed:** #10 — widgets show "No results yet — run the notebook first." #15 — no auto-refresh on dashboard.

**Context:** 
- `QueryWidget` in `DashboardPage.tsx` (line 169) loads the notebook and renders cell outputs. Outputs are stale DB data loaded when the notebook was last run. There's no mechanism to execute the cell on dashboard load.
- The `Dashboard` model has `auto_refresh_seconds` in `DashboardSettings` but the frontend never reads it.
- The backend execute endpoint exists at `POST /api/v1/notebooks/{id}/cells/{cellId}/execute`.

**Approach:**
- For #10: Add a "Run all cells" button to the dashboard toolbar. When clicked, execute all query-type widget cells. Also, add an "Execute on open" dashboard setting to auto-run on load.
- For #15: Read `auto_refresh_seconds` from dashboard settings and use `setInterval` in the frontend to re-execute all cells at that interval. Add a UI to edit this setting in the dashboard header.

**Files:**
- Modify: `web/src/pages/DashboardPage.tsx`
- Modify: `internal/api/dashboard_handlers.go` (ensure settings update works)

**Step 1: Check if dashboard settings can be updated**
```bash
grep -n "handleUpdateDashboard\|PUT.*dashboard" internal/api/dashboard_handlers.go | head -10
```

**Step 2: Add cell execution helper to DashboardPage**

In `DashboardPage.tsx`, add a helper function and refresh state:
```tsx
const [isRefreshing, setIsRefreshing] = useState(false)

async function executeCells(widgetList: AnyWidget[]) {
  const token = localStorage.getItem('hnb_token')
  const queryWidgets = widgetList.filter(w => !INPUT_WIDGET_TYPES.has(w.type))
  setIsRefreshing(true)
  try {
    await Promise.all(
      queryWidgets.map(w =>
        w.notebook_id && w.cell_id
          ? fetch(`/api/v1/notebooks/${w.notebook_id}/cells/${w.cell_id}/execute`, {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                ...(token ? { Authorization: `Bearer ${token}` } : {}),
              },
              body: JSON.stringify({ parameters: {} }),
            })
          : Promise.resolve()
      )
    )
    // Invalidate notebook queries to reload fresh outputs
    widgets.forEach(w => {
      if (w.notebook_id) qc.invalidateQueries({ queryKey: ['notebook', w.notebook_id] })
    })
  } finally {
    setIsRefreshing(false)
  }
}
```

**Step 3: Add auto-refresh interval**

After widgets load, set up the auto-refresh:
```tsx
const refreshSeconds = dashboard?.settings?.auto_refresh_seconds ?? 0

useEffect(() => {
  if (!refreshSeconds || refreshSeconds <= 0 || !widgets.length) return
  const id = setInterval(() => executeCells(widgets), refreshSeconds * 1000)
  return () => clearInterval(id)
}, [refreshSeconds, widgets])
```

**Step 4: Add "Run all" button and refresh interval selector to the dashboard toolbar**

Find the dashboard header/toolbar in `DashboardPage.tsx`. Add:
```tsx
<button
  style={{ ...btnStyle, opacity: isRefreshing ? 0.6 : 1 }}
  disabled={isRefreshing}
  onClick={() => executeCells(widgets)}
  title="Execute all widget cells now"
>
  {isRefreshing ? 'Running…' : '▶ Run all'}
</button>

<select
  style={{ fontSize: 12, padding: '4px 8px', border: '1px solid var(--border)', borderRadius: 4,
           background: 'var(--bg-input)', color: 'var(--text-muted)', cursor: 'pointer' }}
  value={String(dashboard?.settings?.auto_refresh_seconds ?? 0)}
  onChange={async e => {
    const secs = parseInt(e.target.value)
    await api.put(`/api/v1/dashboards/${id}`, {
      settings: { ...dashboard?.settings, auto_refresh_seconds: secs }
    })
    qc.invalidateQueries({ queryKey: ['dashboard', id] })
  }}
  title="Auto-refresh interval"
>
  <option value="0">No auto-refresh</option>
  <option value="30">Every 30s</option>
  <option value="60">Every 1m</option>
  <option value="300">Every 5m</option>
  <option value="600">Every 10m</option>
</select>
```

**Step 5: Update `QueryWidget` — show a "Run" button when no outputs**

In `QueryWidget` (line 199), replace the plain "run the notebook first" message:
```tsx
if (!cell.outputs?.length) {
  return (
    <div style={queryWidgetStyles.empty}>
      No data yet.{' '}
      <button
        style={{ color: 'var(--accent)', background: 'none', border: 'none', cursor: 'pointer', fontSize: 13 }}
        onClick={() => {
          const token = localStorage.getItem('hnb_token')
          fetch(`/api/v1/notebooks/${widget.notebook_id}/cells/${widget.cell_id}/execute`, {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              ...(token ? { Authorization: `Bearer ${token}` } : {}),
            },
            body: JSON.stringify({ parameters: {} }),
          }).then(() => qc.invalidateQueries({ queryKey: ['notebook', widget.notebook_id] }))
        }}
      >
        Run cell
      </button>
    </div>
  )
}
```

**Step 6: Run frontend tests**
```bash
cd web && npm run test:run 2>&1 | tail -10
```

**Step 7: Commit**
```bash
git add web/src/pages/DashboardPage.tsx
git commit -m "feat: dashboard Run All button, per-widget Run, and auto-refresh interval (#10, #15)"
```

---

### Task 9: "Everyone" group for blanket permissions (#12)

**Items addressed:** No default group for giving permission to all org members.

**Context:** When a new org is created, insert an "Everyone" group automatically. For the ACL permission check, when a subject is the "Everyone" group, any org member qualifies. The frontend's PermissionsPanel already supports adding group ACL entries — "Everyone" will just appear in the group list.

**Files:**
- Create: `internal/database/migrations/011_everyone_group.sql`
- Modify: `internal/api/auth_handlers.go` (add everyone group on org creation)
- Modify: `internal/api/permissions.go` (special-case Everyone in group membership check)

**Step 1: Write the migration to seed Everyone for existing orgs**

Create `internal/database/migrations/011_everyone_group.sql`:
```sql
-- Seed "Everyone" group for all existing orgs
INSERT INTO groups (org_id, name)
SELECT id, 'Everyone'
FROM orgs
ON CONFLICT (org_id, name) DO NOTHING;
```

**Step 2: Create Everyone group on new org registration**

In `auth_handlers.go`, in the org-creation transaction block (after creating the home folder, around line 104), add:
```go
// Create built-in "Everyone" group
_, err = tx.Exec(ctx,
  `INSERT INTO groups (org_id, name) VALUES ($1, 'Everyone') ON CONFLICT DO NOTHING`,
  orgID,
)
if err != nil {
  writeError(w, http.StatusInternalServerError, "failed to create everyone group")
  return
}
```

**Step 3: Handle Everyone in permission checks**

In `internal/api/permissions.go`, find the `checkPermission` function (or wherever group membership is resolved). When checking if a user belongs to a group, add a special case:

```go
// "Everyone" group: any org member is implicitly a member
if groupName == "Everyone" {
  // user is already in the org (we're past org auth), so they're in Everyone
  return true, nil
}
```

To implement this, find where group membership is checked (likely a query like `SELECT 1 FROM group_members WHERE group_id=$1 AND user_id=$2`). Wrap it:

```go
// Check if this is the Everyone group
var groupName string
_ = s.db.Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, groupID).Scan(&groupName)
if groupName == "Everyone" {
  return true // any org member matches
}
// ... existing group_members query
```

**Step 4: Write a test for Everyone group permissions**
```go
func TestEveryoneGroupPermission(t *testing.T) {
  ts := setupTestServer(t)
  nb := createNotebook(t, ts, "Test NB")

  // Get the Everyone group
  resp := ts.get(t, "/api/v1/groups")
  var groups []map[string]any
  decodeJSON(t, resp, &groups)
  var everyoneID string
  for _, g := range groups {
    if g["name"] == "Everyone" {
      everyoneID = g["id"].(string)
    }
  }
  if everyoneID == "" {
    t.Fatal("Everyone group not found")
  }
  _ = nb
  // Just verify it exists — permission check integration tested elsewhere
}
```

**Step 5: Run backend tests**
```bash
task test:api 2>&1 | grep -E "PASS|FAIL|ok" | tail -10
```

**Step 6: Commit**
```bash
git add internal/database/migrations/011_everyone_group.sql internal/api/auth_handlers.go internal/api/permissions.go
git commit -m "feat: Everyone group created per org for blanket permissions (#12)"
```

---

### Task 10: Full dark theme audit — fix hardcoded colors (#9)

**Items addressed:** Dark theme uses hardcoded non-theme-aware colors in several components.

**Context:** `web/src/styles/theme.css` already defines CSS variables for both light and dark. Many components bypass these variables with hardcoded hex values. The main offenders found:
- `PresentationPage.tsx` — hardcoded `#0d0d0d`, `#1a1a1a`, `#f0f0f0`, `#888`, `#2a2a2a`, `#3a3a3a`, `#cdd6f4`
- `OutputRenderer.tsx` — hardcoded `#fdf5f5`, `#f5d0d0`, `#9a2828` for errors (should use `var(--error-light)`, `var(--error-border)`, `var(--error-text)`)
- Various inline styles using `#111`, `#fff`, `#555` directly

**Step 1: Audit all hardcoded colors in web/src**
```bash
grep -rn "#[0-9a-fA-F]\{3,6\}" web/src/components/ web/src/pages/ --include="*.tsx" | grep -v "theme.css\|\.test\." | head -50
```

**Step 2: Fix `OutputRenderer.tsx` error styles**

In the `styles` object at the bottom, replace:
```tsx
errorWrap: {
  padding: '12px 16px',
  background: 'var(--error-light)',      // was #fdf5f5
  border: '1px solid var(--error-border)', // was #f5d0d0
  ...
},
errorLabel: {
  color: 'var(--error-text)',             // was #9a2828
  ...
},
error: {
  color: 'var(--error-text)',             // was #9a2828
  ...
},
```

**Step 3: Fix `PresentationPage.tsx` to use CSS variables**

The presentation page has a dark design but uses hardcoded colors. Replace with sematics that work in both themes while keeping the presentation feel:
```tsx
page: {
  display: 'flex', flexDirection: 'column', height: '100vh',
  background: 'var(--nav-bg)',      // dark in both themes
  color: 'var(--nav-text)',
  fontFamily: 'var(--font-sans)',
  overflow: 'hidden',
},
markdownSlide: {
  fontSize: 24, lineHeight: 1.6,
  color: 'var(--nav-text)',         // was #f0f0f0
},
codeSlide: {
  background: 'var(--bg-cell-code)',
  borderRadius: 4, overflow: 'hidden',
},
codePre: {
  margin: 0, padding: '24px', fontSize: 16,
  fontFamily: 'var(--font-mono)',
  color: 'var(--text-primary)',      // was #cdd6f4
  whiteSpace: 'pre-wrap', overflowX: 'auto',
},
nav: {
  display: 'flex', alignItems: 'center', justifyContent: 'space-between',
  padding: '16px 40px',
  background: 'var(--bg-elevated)',   // was #1a1a1a
  borderTop: '1px solid var(--border)',
  flexShrink: 0,
},
navBtn: {
  padding: '10px 24px',
  background: 'var(--bg-secondary)',  // was #2a2a2a
  color: 'var(--text-primary)',        // was #f0f0f0
  border: '1px solid var(--border)',
  borderRadius: 4, fontSize: 14, fontWeight: 500, cursor: 'pointer',
},
progress: {
  fontSize: 14, color: 'var(--text-muted)', fontVariantNumeric: 'tabular-nums',
},
loading: {
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  height: '100vh', background: 'var(--bg-primary)',
  color: 'var(--text-muted)', fontSize: 16, fontFamily: 'var(--font-sans)',
},
```

**Step 4: Fix other components found in the audit (step 1)**

For each hardcoded color found:
- `#111` or `#111111` → `var(--text-primary)`
- `#fff` or `#ffffff` → `var(--bg-card)`
- `#555` → `var(--text-secondary)`
- `#888` → `var(--text-muted)`
- `#eee` or `#efefef` → `var(--border-light)`
- `#ddd` or `#e8e8e8` → `var(--border)`
- `rgba(0,0,0,...)` overlay → `var(--bg-overlay)`

**Step 5: Add a theme transition to `body` in `theme.css` for smooth toggle**

In `web/src/styles/theme.css`, after the existing body styles:
```css
html {
  transition: background-color 0.2s ease, color 0.2s ease;
}
```

**Step 6: Run frontend tests**
```bash
cd web && npm run test:run 2>&1 | tail -10
```

**Step 7: Commit**
```bash
git add web/src/ 
git commit -m "feat: comprehensive dark theme — replace hardcoded colors with CSS variables, add smooth transition (#9)"
```

---

## Execution Summary

| Task | Items | Effort |
|------|-------|--------|
| 1 | #20 Profile save feedback | ~15 min |
| 2 | #23 Presentation anchors | ~10 min |
| 3 | #7 Nav rename | ~5 min |
| 4 | #21 Chart view persistence | ~30 min |
| 5 | #6, #22 Chart config redesign | ~90 min |
| 6 | #1, #2 Filesystem recent + search | ~90 min |
| 7 | #17, #18 Cell type history + audit filter | ~60 min |
| 8 | #10, #15 Dashboard execution + auto-refresh | ~90 min |
| 9 | #12 Everyone group | ~45 min |
| 10 | #9 Dark theme audit | ~60 min |

After each task: run tests, commit. After all tasks complete, run full test suite:
```bash
task test:api && cd web && npm run test:run
```
