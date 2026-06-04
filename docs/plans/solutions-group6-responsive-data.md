# Group 6+7: Responsive/Mobile & Data Management — Design Solutions

## Overview

This document covers 8 issues across two categories:
- **Responsive/Mobile** (Issues 1–4): Layout adaptation for small viewports
- **Data Management** (Issues 5–8): Pagination, bulk actions, exports, and UX polish

---

## RESPONSIVE ISSUES

---

### Issue 1: Mobile viewport doesn't auto-collapse sidebar

**Problem:** At 375px (iPhone SE), the sidebar renders at its full expanded width (200px), consuming over half the viewport. The user must manually tap the collapse chevron.

**Current Implementation:**
- `web/src/components/Sidebar.tsx` — Uses `useState` initialized from `localStorage('hnb_sidebar_expanded')`, defaulting to `true` (expanded = 200px). No media query or viewport detection.
- `web/src/components/AppShell.tsx` — Renders `<Sidebar />` as a flex child with no responsive logic. The body is `display: flex` with `overflow: hidden`.
- `web/src/components/TwoPanelLayout.tsx` — Already has a `useEffect` with `window.innerWidth < 768` check and a mobile drawer pattern. This is the only responsive code in the app.

**Proposed Fix: Auto-collapse sidebar on narrow viewports**

Add a `useEffect` in `Sidebar.tsx` that listens for viewport width changes and forces collapsed state below a breakpoint (768px). On mobile (< 480px), hide the sidebar entirely and expose a hamburger toggle in the TopBar.

**Implementation:**

1. **`Sidebar.tsx`** — Add viewport listener:
```tsx
const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768)

useEffect(() => {
  const check = () => {
    const mobile = window.innerWidth < 768
    setIsMobile(mobile)
    if (mobile && expanded) {
      setExpanded(false) // Force collapse
    }
  }
  check()
  window.addEventListener('resize', check)
  return () => window.removeEventListener('resize', check)
}, [expanded])
```

2. **`Sidebar.tsx`** — On very small screens (< 480px), render as a fixed overlay drawer (similar to TwoPanelLayout's mobile pattern) instead of a flex child. Add a hamburger button visible only on mobile.

3. **`TopBar.tsx`** — Add a hamburger menu button (visible only `@media max-width: 767px`) that toggles a mobile sidebar drawer.

**Breakpoints:**
| Width | Behavior |
|-------|----------|
| ≥ 768px | Normal sidebar (expanded/collapsed per user pref) |
| 480–767px | Sidebar forced to collapsed (48px icon rail) |
| < 480px | Sidebar hidden; accessible via hamburger → drawer overlay |

**Dependencies:** None. Pure CSS-in-JS changes. Could extract a shared `useMediaQuery` hook for reuse.

**Files to modify:**
- `web/src/components/Sidebar.tsx` — Add viewport detection, drawer mode
- `web/src/components/TopBar.tsx` — Add hamburger button for mobile
- `web/src/components/AppShell.tsx` — Pass mobile state or use context

**New shared hook:**
```tsx
// web/src/hooks/useMediaQuery.ts
export function useMediaQuery(maxWidth: number): boolean {
  const [matches, setMatches] = useState(() => window.innerWidth < maxWidth)
  useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${maxWidth}px)`)
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches)
    setMatches(mql.matches)
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [maxWidth])
  return matches
}
```

---

### Issue 2: Sidebar overlaps content on tablet widths

**Problem:** At 768–1024px (iPad portrait), the expanded sidebar (200px) plus the TwoPanelLayout left panel (240px) leaves only ~328px for content on a 768px viewport. The sidebar doesn't adapt.

**Current Implementation:**
- `AppShell.tsx` — `body` is `display: flex; flex: 1; overflow: hidden`. Sidebar is a flex child with `flexShrink: 0` and fixed width (200 or 48).
- `TwoPanelLayout.tsx` — Left panel is 240px fixed width. Toggle button position is hardcoded at `left: 240`.

**Proposed Fix: Auto-collapse sidebar at tablet widths + reduce folder tree width**

1. **`Sidebar.tsx`** — At viewport < 1024px, auto-collapse to 48px icon rail (same mechanism as Issue 1 but at a higher breakpoint).

2. **`TwoPanelLayout.tsx`** — Reduce `leftWidth` to 200px when viewport < 1024px. The toggle button `left` position should be computed dynamically based on sidebar state + left panel width, not hardcoded to 240.

3. **`AppShell.tsx`** — No changes needed; flex layout handles it once sidebar width changes.

**Breakpoints (refined):**
| Width | Sidebar | Folder Tree | Content Available |
|-------|---------|-------------|-------------------|
| ≥ 1280px | Expanded (200px) | 240px | Flexible |
| 1024–1279px | Collapsed (48px) | 240px | ~736px min |
| 768–1023px | Collapsed (48px) | 200px | ~520px min |
| < 768px | Hidden (drawer) | Drawer | Full width |

**Dependencies:** Issue 1's `useMediaQuery` hook.

**Files to modify:**
- `web/src/components/Sidebar.tsx` — Add 1024px breakpoint
- `web/src/components/TwoPanelLayout.tsx` — Dynamic leftWidth, fix toggle position

---

### Issue 3: Connector creation form overflows on small viewports

**Problem:** The connector form uses `gridTemplateColumns: '1fr 1fr'` (2-column grid). On viewports < 600px, this squeezes inputs to ~140px each, making them unusable. The form has no horizontal scroll indication.

**Current Implementation:**
- `web/src/pages/ConnectorsPage.tsx`:
  - `styles.body`: `maxWidth: 1100, margin: '0 auto', padding: '32px 40px'` — 40px horizontal padding is excessive on mobile.
  - `styles.formGrid`: `gridTemplateColumns: '1fr 1fr', gap: 12` — Always 2 columns, never collapses.
  - The table of connectors also overflows horizontally with 6 columns (Name, Type, Host, Database, Status, Actions).

**Proposed Fix: Responsive form grid + scrollable table wrapper**

1. **Form grid** — Use CSS `auto-fit` with a minimum column width:
```tsx
formGrid: {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
  gap: 12,
  marginBottom: 16,
}
```
This naturally collapses to 1 column when the container is < ~420px.

2. **Body padding** — Reduce on mobile:
```tsx
body: {
  maxWidth: 1100,
  margin: '0 auto',
  padding: 'clamp(16px, 4vw, 32px) clamp(16px, 4vw, 40px)',
  width: '100%',
}
```

3. **Connector table** — Wrap in a horizontally scrollable container with a visual fade indicator:
```tsx
<div style={{ overflowX: 'auto', WebkitOverflowScrolling: 'touch', position: 'relative' }}>
  <StyledTable ... />
</div>
```
Add a CSS `::after` pseudo-element fade on the right edge to indicate more content.

4. **Action buttons** — On narrow viewports, convert the action button row to a dropdown menu (⋯) pattern, consistent with the context menu already used in HomePage.

**Dependencies:** None.

**Files to modify:**
- `web/src/pages/ConnectorsPage.tsx` — formGrid, body padding, table wrapper

---

### Issue 4: Dashboard editor is unusable on mobile

**Problem:** The dashboard uses `react-grid-layout`'s `GridLayout` with a fixed `containerWidth` measured via `ResizeObserver`. On mobile, widgets with `minW: 2` in a 12-column grid become impossibly small. The sub-header with column count buttons and "Add Widget" also overflows.

**Current Implementation:**
- `web/src/pages/DashboardEditorPage.tsx`:
  - Uses `GridLayout` from `react-grid-layout` with `cols: gridCols` (default 12), `rowHeight: 120`.
  - `containerWidth` measured via `ResizeObserver` on `gridRef`.
  - Sub-header has `padding: '0 24px'`, column selector buttons `[6, 8, 12, 16, 24]`, and "Add Widget" button — all in a flex row that overflows.
  - Widget min width is `minW: 2` (in grid units).

**Proposed Fix: Stack widgets vertically on mobile + simplify header**

1. **Mobile layout mode** — When `containerWidth < 600`, switch to a single-column stack layout (disable drag/resize):
```tsx
const isMobileLayout = containerWidth < 600

// In render:
{isMobileLayout ? (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
    {widgets.map(widget => (
      <div key={widget.id} style={styles.widgetCard}>
        <WidgetContent widget={widget} />
      </div>
    ))}
  </div>
) : (
  <GridLayout ...>...</GridLayout>
)}
```

2. **Sub-header** — On mobile, hide the column count selector and collapse "Add Widget" to an icon button:
```tsx
<header style={{
  ...styles.subHeader,
  flexWrap: 'wrap',
  height: 'auto',
  minHeight: 52,
  padding: '8px 16px',
  gap: 8,
}}>
  <div style={styles.headerLeft}>...</div>
  {!isMobileLayout && <div>...column buttons...</div>}
  <button style={{ ...styles.addWidgetBtn, padding: '6px 10px' }}>
    {isMobileLayout ? <Plus size={16} /> : '+ Add Widget'}
  </button>
</header>
```

3. **GridLayout responsive** — Alternatively, use `react-grid-layout`'s built-in `Responsive` or `WidthProvider` with breakpoints:
```tsx
const breakpoints = { lg: 1200, md: 996, sm: 768, xs: 480, xxs: 0 }
const cols = { lg: 12, md: 10, sm: 6, xs: 4, xxs: 1 }
```
With `xxs: 1`, all widgets span the full width on mobile. Set `minW: 1` for mobile.

**Dependencies:** `react-grid-layout` already supports responsive mode. May need to store per-breakpoint layouts if we want persistence.

**Files to modify:**
- `web/src/pages/DashboardEditorPage.tsx` — Responsive grid config, simplified header

---

## DATA MANAGEMENT ISSUES

---

### Issue 5: No pagination on audit log

**Problem:** The audit log uses a "Load more" infinite scroll pattern. While functional, there's no way to jump to a specific page, see total count, or navigate efficiently through thousands of entries.

**Current Implementation:**
- `web/src/pages/AuditPage.tsx`:
  - `PAGE_SIZE = 50`, starts at `offset: 0`.
  - Uses "Load more" button that appends next page to `entries` state.
  - Backend (`internal/api/audit_handlers.go`) already supports `limit` and `offset` query params (up to 500 per page).
  - No total count returned from backend.
  - Client-side sorting only (sorts accumulated entries in memory).

**Proposed Fix: Add proper pagination with page numbers + backend total count**

1. **Backend** — Add a `total` count to the audit response. Modify `handleListAuditLogs` to return `{ entries: [...], total: N }` instead of a bare array:

```go
// audit_handlers.go
type auditListResponse struct {
    Entries []audit.Entry `json:"entries"`
    Total   int           `json:"total"`
}
```

This requires adding a `COUNT(*)` query to the audit store. CheckHouse audit store needs a `Count` method.

2. **Frontend** — Replace "Load more" with page-number pagination:

```tsx
const PAGE_SIZE = 50
const [page, setPage] = useState(0) // 0-indexed
const [total, setTotal] = useState(0)

// Query uses offset = page * PAGE_SIZE
// Response gives us { entries, total }
// Render page numbers: [1] [2] [3] ... [N]
// Show "Showing 1–50 of 1,234 entries"
```

3. **Pagination component** — Create a reusable `<Pagination>` component:
```tsx
interface PaginationProps {
  page: number        // 0-indexed current page
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}
```
Renders: `‹ 1 2 3 … 25 ›` with ellipsis for large page counts.

4. **Remove client-side sort** — Since we're now paginating server-side, sorting should also be server-side. Add `sort` and `order` params to the backend.

**Backend changes needed:**
- `internal/audit/clickhouse.go` (or wherever audit store lives) — Add `Count()` method
- `internal/api/audit_handlers.go` — Return `{ entries, total }`, accept `sort`/`order` params

**Dependencies:** Backend API change (breaking). Frontend can be built to handle both old (array) and new (object) response formats during transition.

**Files to modify:**
- `internal/api/audit_handlers.go` — New response shape, sort params
- `internal/audit/*.go` — Add Count method
- `web/src/pages/AuditPage.tsx` — Page-number pagination, server-side sort
- `web/src/components/Pagination.tsx` — **New file**

---

### Issue 6: No bulk actions on file list

**Problem:** Users can't multi-select files/folders/notebooks for batch move or delete. Each item requires individual right-click → action.

**Current Implementation:**
- `web/src/pages/HomePage.tsx`:
  - Items are rendered in sections (Folders grid, Notebooks/Connectors/Dashboards lists).
  - Each item has a context menu (⋯) with individual actions: Rename, Move, Permissions, Delete.
  - No selection state exists.
  - Backend supports individual delete/move operations.

**Proposed Fix: Checkbox multi-select with bulk action toolbar**

1. **Selection state** — Add a `Set<string>` of selected IDs:
```tsx
const [selected, setSelected] = useState<Set<string>>(new Set())
const [selectionMode, setSelectionMode] = useState(false)
```

2. **Selection UI** — When `selected.size > 0` or `selectionMode` is true:
   - Show a checkbox on each item (left side).
   - Show a sticky bulk action toolbar at the top of the content area:
   ```
   [✓] 3 items selected  |  [Move to…] [Delete]  [Clear]
   ```

3. **Toggle selection mode:**
   - Enter: Click a "Select" button in the toolbar area, or Shift+click an item.
   - Exit: Click "Clear" or press Escape.
   - Ctrl/Cmd+click toggles individual items.
   - "Select All" checkbox in the toolbar.

4. **Bulk actions:**
   - **Move** — Opens the existing `MoveModal` but with multiple IDs. Requires a new batch move API endpoint: `PUT /api/v1/folders/batch-move` accepting `{ ids: string[], types: string[], dest_folder_id: string }`.
   - **Delete** — Confirmation dialog listing items. Requires batch delete endpoint.

5. **Backend endpoints** (new):
```
POST /api/v1/resources/batch-move    { ids: [{type, id}], dest_folder_id }
POST /api/v1/resources/batch-delete  { ids: [{type, id}] }
```

**Alternative (simpler, no backend changes):**
- Only implement bulk **move** using the existing per-item `moveItem` mutation in a `Promise.all()`.
- Bulk delete similarly with `Promise.all()` of individual delete calls.
- This is less efficient but requires zero backend changes.

**Recommended:** Start with the simpler approach (client-side batch using existing endpoints). Add batch endpoints later if performance is an issue.

**Dependencies:** None for simple approach. Batch endpoints for optimized approach.

**Files to modify:**
- `web/src/pages/HomePage.tsx` — Selection state, checkboxes, bulk toolbar
- (Optional) `internal/api/resource_handlers.go` — **New file** for batch endpoints

---

### Issue 7: No export options for notebook or query results

**Problem:** Users can't export query results to CSV/Excel or notebooks to PDF. The `OutputRenderer` shows table data but has no download/export button.

**Current Implementation:**
- `web/src/components/OutputRenderer.tsx`:
  - `TableOutput` renders a `<table>` with `ResultSet` data (columns + rows).
  - Has a row count display and table/chart toggle.
  - No export functionality.
- No backend export endpoints exist (confirmed by grep).

**Proposed Fix: Client-side CSV export + backend notebook export**

**A. CSV Export (client-side, immediate value):**

Add a "Download CSV" button to the `OutputRenderer`'s output bar:

```tsx
// In TableOutput, add to the outputBar:
<button style={styles.exportBtn} onClick={() => exportCSV(rs)} title="Download CSV">
  <Download size={12} /> CSV
</button>
```

Implementation:
```tsx
function exportCSV(rs: ResultSet) {
  const header = rs.columns.map(c => `"${c.name.replace(/"/g, '""')}"`).join(',')
  const rows = rs.rows.map(row =>
    (row as unknown[]).map(cell => {
      if (cell === null) return ''
      const str = typeof cell === 'object' ? JSON.stringify(cell) : String(cell)
      return `"${str.replace(/"/g, '""')}"`
    }).join(',')
  )
  const csv = [header, ...rows].join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `query-results-${new Date().toISOString().slice(0,10)}.csv`
  a.click()
  URL.revokeObjectURL(url)
}
```

**B. JSON Export (client-side):**
Same pattern, `JSON.stringify(rs.rows, null, 2)`.

**C. Notebook Export (backend, future):**
Add a `GET /api/v1/notebooks/:id/export?format=pdf|html|ipynb` endpoint. This is a larger feature requiring server-side rendering.

**Recommended scope for this fix:** CSV + JSON export from `OutputRenderer`. This covers the most common use case (exporting query results) with zero backend changes.

**UI placement:** Add to the existing output bar (between row count and view toggle):
```
[ 42 rows · 5 columns ]  [CSV ↓] [JSON ↓]  [Table | Chart]
```

**Dependencies:** None. Pure client-side implementation.

**Files to modify:**
- `web/src/components/OutputRenderer.tsx` — Add export buttons + `exportCSV`/`exportJSON` functions
- (Future) `internal/api/notebook_handlers.go` — Notebook export endpoint

---

### Issue 8: Audit log resource IDs truncated with no easy copy

**Problem:** Resource IDs are truncated to 8 chars (`truncateId`). Clicking copies the full ID to clipboard, but there's no visual feedback (no "Copied!" toast, no icon change). Users don't know the click worked.

**Current Implementation:**
- `web/src/pages/AuditPage.tsx`:
  - `truncateId(id)` returns `id.slice(0, 8) + '…'` for IDs > 8 chars.
  - `ResourceCell` has `onClick={handleCopy}` which calls `navigator.clipboard.writeText(resource_id)`.
  - The clickable span has `title={`Click to copy: ${resource_id}`}` and `className="cursor-pointer"`.
  - No visual feedback after copy — no state change, no toast.
  - The `Copy` icon is imported from `lucide-react` but **never used** in the component.

**Proposed Fix: Add inline copy feedback + visible copy icon**

1. **Add copy feedback state** — Track which ID was just copied with a timeout:
```tsx
const [copiedId, setCopiedId] = useState<string | null>(null)

const handleCopy = (id: string) => {
  navigator.clipboard.writeText(id)
  setCopiedId(id)
  setTimeout(() => setCopiedId(null), 2000)
}
```

2. **Visual feedback** — Show a checkmark + "Copied!" text inline:
```tsx
<span style={styles.resourceSub} onClick={() => handleCopy(resource_id)}>
  {truncateId(resource_id)}
  {copiedId === resource_id ? (
    <Check size={10} style={{ color: 'var(--success)', marginLeft: 4 }} />
  ) : (
    <Copy size={10} style={{ marginLeft: 4, opacity: 0.4 }} />
  )}
</span>
```

3. **Hover state** — Show the copy icon more prominently on hover:
```css
/* In the resourceSub span */
&:hover .copy-icon { opacity: 1; }
```
Since we're using inline styles, use a wrapper component with `onMouseEnter/Leave` state or a CSS class.

4. **Full ID tooltip** — Already present via `title` attribute. Additionally, when `copiedId === resource_id`, change the title to "Copied!" temporarily.

5. **Optional: Show full ID on click** — Instead of just copying, toggle between truncated and full display:
```tsx
const [expandedId, setExpandedId] = useState<string | null>(null)
// Clicking shows full ID; a second click or copy button copies it
```

**Recommended:** Approach 1–3 (inline checkmark feedback + persistent copy icon). Simple, clear, no UX ambiguity.

**Dependencies:** None. The `Copy` and `Check` icons are already imported.

**Files to modify:**
- `web/src/pages/AuditPage.tsx` — Add `copiedId` state, render copy icon + feedback in `ResourceCell`

---

## Implementation Priority

| Priority | Issue | Effort | Impact | Backend Change? |
|----------|-------|--------|--------|-----------------|
| P0 | #1 Sidebar auto-collapse | Small | High (mobile UX) | No |
| P0 | #3 Connector form overflow | Small | High (usable forms) | No |
| P1 | #8 Audit copy feedback | Tiny | Medium (UX polish) | No |
| P1 | #7 CSV export | Small | High (user request) | No |
| P1 | #2 Tablet sidebar overlap | Small | Medium | No |
| P2 | #4 Dashboard mobile | Medium | Medium | No |
| P2 | #5 Audit pagination | Medium | Medium | Yes (total count) |
| P3 | #6 Bulk file actions | Large | Medium | Maybe (batch endpoints) |

---

## Shared Infrastructure

### New Hook: `useMediaQuery`
```tsx
// web/src/hooks/useMediaQuery.ts
import { useState, useEffect } from 'react'

export function useMediaQuery(maxWidth: number): boolean {
  const [matches, setMatches] = useState(() => 
    typeof window !== 'undefined' ? window.innerWidth < maxWidth : false
  )
  useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${maxWidth}px)`)
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches)
    setMatches(mql.matches)
    mql.addEventListener('change', handler)
    return () => mql.removeEventListener('change', handler)
  }, [maxWidth])
  return matches
}
```

Used by: Issues #1, #2, #3, #4.

### New Component: `Pagination`
```tsx
// web/src/components/Pagination.tsx
interface Props {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}
```

Used by: Issue #5.

---

## Testing Considerations

- **Responsive issues (#1–4):** Test at 375px (iPhone SE), 768px (iPad portrait), 1024px (iPad landscape), 1440px (desktop). Verify sidebar behavior, form layout, dashboard grid.
- **Audit pagination (#5):** Test with 0, 50, 500, 10,000 entries. Verify page navigation, sort persistence, filter reset.
- **Bulk actions (#6):** Test selecting all, partial selection, cross-type selection, move/delete confirmation.
- **CSV export (#7):** Test with special characters (commas, quotes, newlines in data), null values, large result sets (10K rows).
- **Copy feedback (#8):** Verify 2-second timeout, rapid click behavior, clipboard permission denied fallback.
