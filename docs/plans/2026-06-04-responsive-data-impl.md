# Implementation Plan: Group 6 — Responsive/Mobile & Data Management

## Goal
Fix 8 issues across responsive layout (mobile/tablet) and data management (pagination, bulk actions, exports, copy feedback) to make hnb usable on all screen sizes and improve data workflow UX.

## Architecture
- **Frontend**: React + TypeScript + Vite, inline styles (no CSS framework), `react-grid-layout` for dashboards
- **Backend**: Go (net/http ServeMux), Postgres via pgx, audit logs in `audit_logs` table
- **State**: React Query (`@tanstack/react-query`) for server state, `useState` for local UI state
- **Layout**: `AppShell` → `TopBar` + `Sidebar` + `<main>`. `TwoPanelLayout` wraps folder tree + content on HomePage.

## Tech Stack
- React 18, TypeScript, Vite
- `react-grid-layout` ^2.2.3 (has built-in responsive support)
- `lucide-react` for icons
- Go 1.22+, pgx v5, Postgres 15
- `task` runner

---

## Task 1: Create `useMediaQuery` shared hook

**Files:**
- `web/src/hooks/useMediaQuery.ts` — **NEW**

**Why:** Issues #1, #2, #3, #4 all need viewport detection. A single reusable hook avoids duplication.

### Step 1.1: Create the hook file

Create `web/src/hooks/useMediaQuery.ts`:

```typescript
import { useState, useEffect } from 'react'

/**
 * Returns true when viewport width is below `maxWidth` px.
 * Uses `window.matchMedia` for efficient, event-driven updates.
 */
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

### Step 1.2: Verify it compiles

```bash
cd web && npx tsc --noEmit src/hooks/useMediaQuery.ts
```

**Expected:** No errors.

### Step 1.3: Commit

```bash
git add web/src/hooks/useMediaQuery.ts
git commit -m "feat: add useMediaQuery shared hook for responsive breakpoints"
```

---

## Task 2: Issue #1 — Mobile viewport auto-collapses sidebar

**Problem:** At < 768px the sidebar stays expanded (200px), consuming half the viewport.

**Files:**
- `web/src/components/Sidebar.tsx` — Modify (lines 1–120)
- `web/src/components/TopBar.tsx` — Modify (add hamburger button)
- `web/src/components/AppShell.tsx` — Modify (wire mobile sidebar state)

### Step 2.1: Add viewport detection to Sidebar

In `web/src/components/Sidebar.tsx`, add imports and state after line 1 (`import { useState } from 'react'`):

**Change line 1 from:**
```tsx
import { useState } from 'react'
```

**To:**
```tsx
import { useState, useEffect, useCallback } from 'react'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { Menu } from 'lucide-react'
```

### Step 2.2: Add mobile state and auto-collapse logic

After the existing `expanded` state (around line 27), add:

```tsx
  const isMobile = useMediaQuery(768)
  const isTablet = useMediaQuery(1024)
  const [drawerOpen, setDrawerOpen] = useState(false)

  // Auto-collapse on tablet, hide on mobile
  useEffect(() => {
    if (isMobile) {
      // On mobile, force collapsed and close drawer
      if (expanded) {
        setExpanded(false)
      }
    } else if (isTablet) {
      // On tablet, force collapsed (icon rail)
      if (expanded) {
        setExpanded(false)
      }
    }
  }, [isMobile, isTablet]) // eslint-disable-line react-hooks/exhaustive-deps
```

### Step 2.3: Modify sidebar rendering for mobile drawer mode

Replace the `return` block (starting at the current `return (` around line 43) with:

```tsx
  // On mobile, render as a drawer overlay
  if (isMobile) {
    if (!drawerOpen) return null
    return (
      <>
        <div
          style={styles.overlay}
          onClick={() => setDrawerOpen(false)}
        />
        <nav style={{ ...styles.sidebar, ...styles.drawer }}>
          <div style={styles.drawerHeader}>
            <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' }}>Navigation</span>
            <button
              style={styles.drawerCloseBtn}
              onClick={() => setDrawerOpen(false)}
              aria-label="Close navigation"
            >
              ×
            </button>
          </div>
          <div style={styles.items}>
            {renderNavItems()}
          </div>
        </nav>
      </>
    )
  }

  // Desktop/tablet: normal flex child
  const width = expanded ? 200 : 48

  return (
    <nav style={{ ...styles.sidebar, width }}>
      <div style={styles.items}>
        {renderNavItems()}
      </div>
      {!isTablet && (
        <button style={styles.toggle} onClick={toggle} title={expanded ? 'Collapse sidebar' : 'Expand sidebar'}>
          {expanded ? <ChevronLeft size={14} /> : <ChevronRight size={14} />}
        </button>
      )}
    </nav>
  )
```

### Step 2.4: Extract nav items into a helper function

Add this function inside the `Sidebar` component, before the return statements:

```tsx
  const renderNavItems = () => (
    <>
      {NAV_ITEMS.map(({ to, title, icon }) => (
        <NavLink
          key={to}
          to={to}
          end={to === '/'}
          title={title}
          onClick={() => isMobile && setDrawerOpen(false)}
          style={({ isActive }) => itemStyle(isActive)}
        >
          <span style={styles.icon}>{icon}</span>
          {(expanded || isMobile) && (
            <span style={styles.label}>{title}</span>
          )}
        </NavLink>
      ))}
      <div style={styles.sectionDivider} />
      {(expanded || isMobile) ? (
        <div style={styles.sectionHeader}>
          <span style={styles.sectionTitle}>AI Agents</span>
        </div>
      ) : (
        <div style={{ ...styles.sectionHeader, justifyContent: 'center' }}>
          <Bot size={14} style={{ color: 'var(--text-muted)' }} />
        </div>
      )}
      {AGENT_NAV_ITEMS.map(({ to, title, icon }) => (
        <NavLink
          key={to}
          to={to}
          title={title}
          onClick={() => isMobile && setDrawerOpen(false)}
          style={({ isActive }) => itemStyle(isActive)}
        >
          <span style={styles.icon}>{icon}</span>
          {(expanded || isMobile) && (
            <span style={styles.label}>{title}</span>
          )}
        </NavLink>
      ))}
    </>
  )
```

**Note:** Remove the old inline JSX that was between `<div style={styles.items}>` and the toggle button. The `renderNavItems()` call replaces it.

### Step 2.5: Add `toggleDrawer` export and new styles

Add this method inside the component (before the return):

```tsx
  const openDrawer = useCallback(() => setDrawerOpen(true), [])
```

Expose it via a ref or context. The simplest approach: export a function that TopBar can call. Since React components can't easily export functions, use a **custom event** pattern:

Add at the top of the file (after imports):

```tsx
// Custom event to toggle the mobile sidebar drawer
export const SIDEBAR_DRAWER_EVENT = 'hnb-sidebar-drawer'
export function openMobileSidebar() {
  window.dispatchEvent(new CustomEvent(SIDEBAR_DRAWER_EVENT))
}
```

Inside the Sidebar component, add a listener:

```tsx
  useEffect(() => {
    const handler = () => setDrawerOpen(true)
    window.addEventListener(SIDEBAR_DRAWER_EVENT, handler)
    return () => window.removeEventListener(SIDEBAR_DRAWER_EVENT, handler)
  }, [])
```

### Step 2.6: Add new styles to the styles object

Add these entries to the `styles` object at the bottom of the file:

```tsx
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'rgba(0,0,0,0.5)',
    zIndex: 999,
  },
  drawer: {
    position: 'fixed',
    left: 0,
    top: 52,  // below TopBar
    bottom: 0,
    width: 260,
    zIndex: 1000,
    boxShadow: 'var(--shadow-md)',
  },
  drawerHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '12px 16px',
    borderBottom: '1px solid var(--nav-border)',
  },
  drawerCloseBtn: {
    background: 'none',
    border: 'none',
    fontSize: 22,
    cursor: 'pointer',
    color: 'var(--text-muted)',
    padding: '0 4px',
    lineHeight: 1,
  },
```

### Step 2.7: Add hamburger button to TopBar

In `web/src/components/TopBar.tsx`, add import at line 3:

```tsx
import { Menu } from 'lucide-react'
import { openMobileSidebar } from './Sidebar'
```

Inside the `TopBar` component's return, add a hamburger button right after the `<Link to="/" style={styles.brand}>` block (after the brand `</Link>` closing tag, before `<div style={styles.spacer} />`):

```tsx
      <button
        style={styles.hamburger}
        onClick={() => openMobileSidebar()}
        aria-label="Open navigation"
        className="hnb-hamburger"
      >
        <Menu size={18} />
      </button>
```

Add to the `styles` object:

```tsx
  hamburger: {
    display: 'none',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--nav-text)',
    padding: '6px',
    alignItems: 'center',
    justifyContent: 'center',
  },
```

**Important:** The hamburger should only show on mobile. Since we use inline styles, add a `<style>` tag in the TopBar component or use a CSS class. The cleanest approach: add to `web/src/index.css` (or the main CSS file):

```css
@media (max-width: 767px) {
  .hnb-hamburger {
    display: flex !important;
  }
}
```

Find the main CSS file:

```bash
find web/src -name "*.css" -not -path "*/node_modules/*" | head -10
```

Add the media query to the appropriate CSS file (likely `web/src/index.css` or `web/src/theme.css`).

### Step 2.8: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "feat: auto-collapse sidebar on mobile viewports (<768px)

- Add useMediaQuery hook for responsive breakpoints
- Sidebar renders as drawer overlay on mobile
- Auto-collapse to icon rail on tablet (768-1024px)
- Add hamburger menu button in TopBar (mobile only)
- Close drawer on nav item click"
```

---

## Task 3: Issue #2 — Sidebar overlaps content on tablet widths

**Problem:** At 768–1024px, expanded sidebar (200px) + folder tree (240px) leaves too little content space.

**Files:**
- `web/src/components/Sidebar.tsx` — Already handled in Task 2 (auto-collapse at < 1024px)
- `web/src/components/TwoPanelLayout.tsx` — Modify for dynamic leftWidth

### Step 3.1: Make TwoPanelLayout responsive

In `web/src/components/TwoPanelLayout.tsx`, replace the existing `useEffect` for `isMobile` (lines 18–23) with:

```tsx
  const isMobile = useMediaQuery(768)
  const isTablet = useMediaQuery(1024)

  // Effective left width: narrower on tablet
  const effectiveLeftWidth = isTablet && !isMobile ? Math.min(leftWidth, 200) : leftWidth
```

Add import at the top:

```tsx
import { useMediaQuery } from '../hooks/useMediaQuery'
```

Remove the old `useState`/`useEffect` for `isMobile` (lines 16–23):

```tsx
// DELETE these lines:
  const [isMobile, setIsMobile] = useState(false)
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => {
    const check = () => setIsMobile(window.innerWidth < 768)
    check()
    window.addEventListener('resize', check)
    return () => window.removeEventListener('resize', check)
  }, [])
```

Replace with:

```tsx
  const isMobile = useMediaQuery(768)
  const isTablet = useMediaQuery(1024)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const effectiveLeftWidth = isTablet && !isMobile ? Math.min(leftWidth, 200) : leftWidth
```

### Step 3.2: Update left panel width reference

In the left panel `<div>` style, change `leftWidth` to `effectiveLeftWidth`:

**Find (around line 40):**
```tsx
      <div style={{
        width: isMobile ? 0 : (collapsed ? 0 : leftWidth),
```

**Replace with:**
```tsx
      <div style={{
        width: isMobile ? 0 : (collapsed ? 0 : effectiveLeftWidth),
```

### Step 3.3: Fix toggle button position

**Find (around line 50):**
```tsx
          left: 240,
```

**Replace with:**
```tsx
          left: effectiveLeftWidth,
```

### Step 3.4: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "feat: responsive TwoPanelLayout — narrower folder tree on tablet

- Reduce left panel to 200px on tablet (768-1024px)
- Fix toggle button position to track dynamic width
- Use useMediaQuery hook instead of manual resize listener"
```

---

## Task 4: Issue #3 — Connector creation form overflows on small viewports

**Problem:** The 2-column form grid squeezes inputs on viewports < 600px. The table overflows horizontally.

**Files:**
- `web/src/pages/ConnectorsPage.tsx` — Modify styles (lines 298–313)

### Step 4.1: Make form grid responsive

In `web/src/pages/ConnectorsPage.tsx`, find the `styles` object at the bottom of the file.

**Change `body` style (line ~299):**
```tsx
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
```

**To:**
```tsx
  body: { maxWidth: 1100, margin: '0 auto', padding: 'clamp(16px, 4vw, 32px) clamp(16px, 4vw, 40px)', width: '100%', boxSizing: 'border-box' as const },
```

**Change `formGrid` style (line ~300):**
```tsx
  formGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 },
```

**To:**
```tsx
  formGrid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12, marginBottom: 16 },
```

This uses CSS `auto-fit` with `minmax(220px, 1fr)` — it will show 2 columns when the container is ≥ 460px, and collapse to 1 column below that.

### Step 4.2: Wrap connector table in scrollable container

Find the `<StyledTable>` usage (around line 229). Wrap it in a scrollable div:

**Find:**
```tsx
        <StyledTable headers={['Name', 'Type', 'Host', 'Database', 'Status', '']}>
```

**Replace with:**
```tsx
        <div style={styles.tableWrap}>
        <StyledTable headers={['Name', 'Type', 'Host', 'Database', 'Status', '']}>
```

**Find the closing `</StyledTable>` tag (around line 290) and add after it:**
```tsx
        </div>
```

Add the `tableWrap` style to the styles object:

```tsx
  tableWrap: { overflowX: 'auto', WebkitOverflowScrolling: 'touch' as const },
```

### Step 4.3: Make form actions wrap on mobile

**Find:**
```tsx
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end' },
```

**Replace with:**
```tsx
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end', flexWrap: 'wrap' as const },
```

### Step 4.4: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "fix: responsive connector form — auto-fit grid, scrollable table

- Form grid uses repeat(auto-fit, minmax(220px, 1fr)) for responsive columns
- Body padding uses clamp() for mobile-friendly spacing
- Connector table wrapped in horizontally scrollable container
- Form actions wrap on narrow viewports"
```

---

## Task 5: Issue #4 — Dashboard editor is unusable on mobile

**Problem:** `react-grid-layout` with fixed 12-column grid makes widgets impossibly small on mobile. Sub-header overflows.

**Files:**
- `web/src/pages/DashboardEditorPage.tsx` — Modify (lines 1–440)

### Step 5.1: Add mobile detection

Add import at the top of the file:

```tsx
import { useMediaQuery } from '../hooks/useMediaQuery'
import { Plus } from 'lucide-react'
```

Inside the `DashboardEditorPage` component, after the existing `containerWidth` state (around line 102), add:

```tsx
  const isMobileLayout = containerWidth < 600
```

### Step 5.2: Make sub-header responsive

Find the sub-header `<header>` element (around line 207). Replace the `headerRight` section:

**Find:**
```tsx
        <div style={styles.headerRight}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Cols</span>
            {[6, 8, 12, 16, 24].map(c => (
              <button
                key={c}
                type="button"
                style={{
                  padding: '3px 8px',
                  fontSize: 12,
                  fontWeight: 600,
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  cursor: 'pointer',
                  background: gridCols === c ? 'var(--accent)' : 'var(--bg-input)',
                  color: gridCols === c ? '#fff' : 'var(--text-secondary)',
                }}
                onClick={async () => {
                  setGridCols(c)
                  await api.put(`/api/v1/dashboards/${id}`, {
                    settings: { ...dashboard?.settings, grid_cols: c },
                  })
                  qc.invalidateQueries({ queryKey: ['dashboard', id] })
                }}
              >
                {c}
              </button>
            ))}
          </div>
          <button
            type="button"
            style={styles.addWidgetBtn}
            onClick={() => setShowPicker(true)}
          >
            + Add Widget
          </button>
        </div>
```

**Replace with:**
```tsx
        <div style={styles.headerRight}>
          {!isMobileLayout && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Cols</span>
              {[6, 8, 12, 16, 24].map(c => (
                <button
                  key={c}
                  type="button"
                  style={{
                    padding: '3px 8px',
                    fontSize: 12,
                    fontWeight: 600,
                    border: '1px solid var(--border)',
                    borderRadius: 4,
                    cursor: 'pointer',
                    background: gridCols === c ? 'var(--accent)' : 'var(--bg-input)',
                    color: gridCols === c ? '#fff' : 'var(--text-secondary)',
                  }}
                  onClick={async () => {
                    setGridCols(c)
                    await api.put(`/api/v1/dashboards/${id}`, {
                      settings: { ...dashboard?.settings, grid_cols: c },
                    })
                    qc.invalidateQueries({ queryKey: ['dashboard', id] })
                  }}
                >
                  {c}
                </button>
              ))}
            </div>
          )}
          <button
            type="button"
            style={{
              ...styles.addWidgetBtn,
              ...(isMobileLayout ? { padding: '6px 10px', fontSize: 12 } : {}),
            }}
            onClick={() => setShowPicker(true)}
          >
            {isMobileLayout ? <Plus size={16} /> : '+ Add Widget'}
          </button>
        </div>
```

### Step 5.3: Update sub-header style for mobile wrapping

**Find in styles:**
```tsx
  subHeader: {
    background: 'var(--bg-primary)',
    borderBottom: '1px solid var(--border)',
    height: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '0 24px',
    flexShrink: 0,
    position: 'sticky',
    top: -32,
    zIndex: 99,
    margin: '-32px -32px 0',
  },
```

**Replace with:**
```tsx
  subHeader: {
    background: 'var(--bg-primary)',
    borderBottom: '1px solid var(--border)',
    minHeight: 52,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px clamp(12px, 3vw, 24px)',
    flexShrink: 0,
    position: 'sticky',
    top: -32,
    zIndex: 99,
    margin: '-32px -32px 0',
    flexWrap: 'wrap',
    gap: 8,
  },
```

### Step 5.4: Add mobile stacked layout for widgets

Find the main body section where widgets render (around line 357). The current code is:

```tsx
          <div ref={gridRef}>
            <GridLayout
              layout={dashboard.widgets?.map(toGridItem) ?? []}
              width={containerWidth}
              gridConfig={{ cols: gridCols, rowHeight: 120 }}
              dragConfig={{ enabled: true, handle: '.widget-drag-handle' }}
              resizeConfig={{ enabled: true }}
              onLayoutChange={handleLayoutChange}
              style={{ minHeight: 240 }}
            >
              {dashboard.widgets?.map((widget: Widget) => (
                <div key={widget.id} style={{ position: 'relative' }}>
                  <div
                    className="widget-drag-handle"
                    style={{
                      position: 'absolute',
                      top: 0, left: 0, right: 0,
                      height: 28,
                      cursor: 'grab',
                      zIndex: 1,
                      borderRadius: '4px 4px 0 0',
                    }}
                    title="Drag to move"
                  />
                  <div style={styles.widgetCard}>
                    <button
                      type="button"
                      style={{ ...styles.deleteWidgetBtn, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                      title="Remove widget"
                      onClick={() => {
                        if (confirm('Remove this widget?')) {
                          deleteWidget.mutate(widget.id)
                        }
                      }}
                    >
                      <X size={12} />
                    </button>
                    <WidgetContent widget={widget} />
                  </div>
                </div>
              ))}
            </GridLayout>
          </div>
```

**Replace with:**
```tsx
          <div ref={gridRef}>
            {isMobileLayout ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                {dashboard.widgets?.map((widget: Widget) => (
                  <div key={widget.id} style={{ position: 'relative' }}>
                    <div style={styles.widgetCard}>
                      <button
                        type="button"
                        style={{ ...styles.deleteWidgetBtn, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                        title="Remove widget"
                        onClick={() => {
                          if (confirm('Remove this widget?')) {
                            deleteWidget.mutate(widget.id)
                          }
                        }}
                      >
                        <X size={12} />
                      </button>
                      <WidgetContent widget={widget} />
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <GridLayout
                layout={dashboard.widgets?.map(toGridItem) ?? []}
                width={containerWidth}
                gridConfig={{ cols: gridCols, rowHeight: 120 }}
                dragConfig={{ enabled: true, handle: '.widget-drag-handle' }}
                resizeConfig={{ enabled: true }}
                onLayoutChange={handleLayoutChange}
                style={{ minHeight: 240 }}
              >
                {dashboard.widgets?.map((widget: Widget) => (
                  <div key={widget.id} style={{ position: 'relative' }}>
                    <div
                      className="widget-drag-handle"
                      style={{
                        position: 'absolute',
                        top: 0, left: 0, right: 0,
                        height: 28,
                        cursor: 'grab',
                        zIndex: 1,
                        borderRadius: '4px 4px 0 0',
                      }}
                      title="Drag to move"
                    />
                    <div style={styles.widgetCard}>
                      <button
                        type="button"
                        style={{ ...styles.deleteWidgetBtn, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                        title="Remove widget"
                        onClick={() => {
                          if (confirm('Remove this widget?')) {
                            deleteWidget.mutate(widget.id)
                          }
                        }}
                      >
                        <X size={12} />
                      </button>
                      <WidgetContent widget={widget} />
                    </div>
                  </div>
                ))}
              </GridLayout>
            )}
          </div>
```

### Step 5.5: Update body padding for mobile

**Find:**
```tsx
  body: {
    flex: 1,
    maxWidth: 1280,
    margin: '0 auto',
    padding: '40px 40px',
    width: '100%',
  },
```

**Replace with:**
```tsx
  body: {
    flex: 1,
    maxWidth: 1280,
    margin: '0 auto',
    padding: 'clamp(16px, 4vw, 40px)',
    width: '100%',
    boxSizing: 'border-box' as const,
  },
```

### Step 5.6: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "feat: responsive dashboard editor — stacked widgets on mobile

- Widgets render as vertical stack when container < 600px (no drag/resize)
- Hide column count selector on mobile, collapse Add Widget to icon
- Sub-header wraps with responsive padding
- Body padding uses clamp() for mobile"
```

---

## Task 6: Issue #8 — Audit log resource IDs: copy feedback + visible icon

**Problem:** Clicking a truncated ID copies it but gives no visual feedback. The `Copy` icon is imported but never rendered.

**Files:**
- `web/src/pages/AuditPage.tsx` — Modify `ResourceCell` component (lines 107–138)

### Step 6.1: Add `Check` icon import

**Change line 3 from:**
```tsx
import { ChevronDown, ChevronUp, Copy } from 'lucide-react'
```

**To:**
```tsx
import { ChevronDown, ChevronUp, Copy, Check } from 'lucide-react'
```

### Step 6.2: Add `copiedId` state to AuditPage

Add after the existing state declarations (after line 22, `const [sortDir, setSortDir] = ...`):

```tsx
  const [copiedId, setCopiedId] = useState<string | null>(null)
```

### Step 6.3: Pass `copiedId` and handler to ResourceCell

First, add a `handleCopy` function inside `AuditPage`:

```tsx
  const handleCopyId = (id: string) => {
    navigator.clipboard.writeText(id).then(() => {
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    })
  }
```

Then update the `AuditRow` usage to pass the copy handler:

**Find (around line 117):**
```tsx
              {sorted.map((entry) => (
                <AuditRow key={entry.id} entry={entry} />
              ))}
```

**Replace with:**
```tsx
              {sorted.map((entry) => (
                <AuditRow key={entry.id} entry={entry} copiedId={copiedId} onCopy={handleCopyId} />
              ))}
```

### Step 6.4: Update `AuditRow` to accept and pass copy props

**Find the `AuditRow` function signature (around line 148):**
```tsx
function AuditRow({ entry }: { entry: AuditEntry }) {
```

**Replace with:**
```tsx
function AuditRow({ entry, copiedId, onCopy }: { entry: AuditEntry; copiedId: string | null; onCopy: (id: string) => void }) {
```

**Find the ResourceCell usage inside AuditRow (around line 163):**
```tsx
        <ResourceCell entry={entry} />
```

**Replace with:**
```tsx
        <ResourceCell entry={entry} copiedId={copiedId} onCopy={onCopy} />
```

### Step 6.5: Update `ResourceCell` with copy icon and feedback

**Find the `ResourceCell` function (around line 107):**
```tsx
function ResourceCell({ entry }: { entry: AuditEntry }) {
  const { resource_type, resource_id, resource_name, resource_parent_name } = entry

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (resource_id) navigator.clipboard.writeText(resource_id)
  }

  if (resource_type === 'cell') {
    const parent = resource_parent_name || null
    const id = resource_id ? truncateId(resource_id) : null
    if (parent && id) {
      return <span>{parent} <span style={styles.resourceSub} title={resource_id} onClick={handleCopy} className="cursor-pointer">› {id}</span></span>
    }
    if (parent) return <span>{parent}</span>
    return <span style={styles.mono}>{id || '—'}</span>
  }

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      {resource_name && <span>{resource_name}</span>}
      {resource_id && (
        <span
          style={{ ...(resource_name ? styles.resourceSub : styles.mono), cursor: 'pointer' }}
          title={`Click to copy: ${resource_id}`}
          onClick={handleCopy}
        >
          {truncateId(resource_id)}
        </span>
      )}
    </span>
  )
}
```

**Replace the entire function with:**
```tsx
function ResourceCell({ entry, copiedId, onCopy }: { entry: AuditEntry; copiedId: string | null; onCopy: (id: string) => void }) {
  const { resource_type, resource_id, resource_name, resource_parent_name } = entry
  const isCopied = copiedId === resource_id

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    if (resource_id) onCopy(resource_id)
  }

  const copyIcon = resource_id ? (
    <span
      style={styles.copyIcon}
      onClick={handleCopy}
      title={isCopied ? 'Copied!' : `Copy: ${resource_id}`}
    >
      {isCopied ? (
        <Check size={10} style={{ color: 'var(--success, #10b981)' }} />
      ) : (
        <Copy size={10} style={{ opacity: 0.5 }} />
      )}
    </span>
  ) : null

  if (resource_type === 'cell') {
    const parent = resource_parent_name || null
    const id = resource_id ? truncateId(resource_id) : null
    if (parent && id) {
      return (
        <span>
          {parent}{' '}
          <span style={styles.resourceSub} onClick={handleCopy} className="cursor-pointer" title={isCopied ? 'Copied!' : resource_id}>
            › {id}
          </span>
          {copyIcon}
        </span>
      )
    }
    if (parent) return <span>{parent}</span>
    return (
      <span style={styles.mono}>
        {id || '—'}
        {copyIcon}
      </span>
    )
  }

  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      {resource_name && <span>{resource_name}</span>}
      {resource_id && (
        <span
          style={{ ...(resource_name ? styles.resourceSub : styles.mono), cursor: 'pointer' }}
          title={isCopied ? 'Copied!' : `Click to copy: ${resource_id}`}
          onClick={handleCopy}
        >
          {truncateId(resource_id)}
        </span>
      )}
      {copyIcon}
    </span>
  )
}
```

### Step 6.6: Add copyIcon style

Add to the `styles` object at the bottom of AuditPage.tsx:

```tsx
  copyIcon: {
    display: 'inline-flex',
    alignItems: 'center',
    cursor: 'pointer',
    marginLeft: 4,
    verticalAlign: 'middle',
  },
```

### Step 6.7: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "fix: audit log copy feedback — visible icon + checkmark

- Add Check icon import (was unused)
- Show persistent Copy icon next to resource IDs
- Display green Check for 2s after copy
- Update title tooltip to show 'Copied!' state
- Pass copiedId state through AuditRow to ResourceCell"
```

---

## Task 7: Issue #7 — CSV/JSON export for query results

**Problem:** No way to export query results from `OutputRenderer`.

**Files:**
- `web/src/components/OutputRenderer.tsx` — Modify `TableOutput` component

### Step 7.1: Add export functions

Add these two functions before the `TableOutput` function (around line 140, after the `sortRows` function):

```tsx
function exportCSV(rs: ResultSet, filename?: string) {
  const header = rs.columns.map(c => `"${c.name.replace(/"/g, '""')}"`).join(',')
  const rows = rs.rows.map(row =>
    (row as unknown[]).map(cell => {
      if (cell === null || cell === undefined) return ''
      const str = typeof cell === 'object' ? JSON.stringify(cell) : String(cell)
      return `"${str.replace(/"/g, '""')}"`
    }).join(',')
  )
  const csv = [header, ...rows].join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename || `query-results-${new Date().toISOString().slice(0, 10)}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

function exportJSON(rs: ResultSet, filename?: string) {
  const data = rs.rows.map(row => {
    const obj: Record<string, unknown> = {}
    rs.columns.forEach((col, i) => {
      obj[col.name] = (row as unknown[])[i]
    })
    return obj
  })
  const json = JSON.stringify(data, null, 2)
  const blob = new Blob([json], { type: 'application/json;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename || `query-results-${new Date().toISOString().slice(0, 10)}.json`
  a.click()
  URL.revokeObjectURL(url)
}
```

### Step 7.2: Add Download icon import

**Change the import (line 5) from:**
```tsx
import { ToggleLeft, Calendar, Clock, Fingerprint, Ban, Binary, Table, BarChart2, Timer, Sigma, ChevronUp, ChevronDown, ChevronLeft, ChevronRight, X, Copy, Check } from 'lucide-react'
```

**To:**
```tsx
import { ToggleLeft, Calendar, Clock, Fingerprint, Ban, Binary, Table, BarChart2, Timer, Sigma, ChevronUp, ChevronDown, ChevronLeft, ChevronRight, X, Copy, Check, Download } from 'lucide-react'
```

### Step 7.3: Add export buttons to the output bar

Find the `outputBar` section inside `TableOutput` (around line 243):

```tsx
      <div style={styles.outputBar}>
        <span style={styles.rowCount}>
          {rs.rows.length} row{rs.rows.length !== 1 ? 's' : ''} · {rs.columns.length} columns
        </span>
        {!fixedView && (
          <div style={styles.viewToggle}>
```

**Replace with:**
```tsx
      <div style={styles.outputBar}>
        <span style={styles.rowCount}>
          {rs.rows.length} row{rs.rows.length !== 1 ? 's' : ''} · {rs.columns.length} columns
        </span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <div style={styles.exportGroup}>
            <button
              style={styles.exportBtn}
              onClick={() => exportCSV(rs)}
              title="Download as CSV"
            >
              <Download size={12} /> CSV
            </button>
            <button
              style={styles.exportBtn}
              onClick={() => exportJSON(rs)}
              title="Download as JSON"
            >
              <Download size={12} /> JSON
            </button>
          </div>
          {!fixedView && (
            <div style={styles.viewToggle}>
```

Now find the closing of the viewToggle div and outputBar. The current structure is:

```tsx
          </div>
        )}
      </div>
```

**Replace with:**
```tsx
            </div>
          )}
        </div>
      </div>
```

### Step 7.4: Add export button styles

Add to the `styles` object:

```tsx
  exportGroup: {
    display: 'flex',
    gap: 4,
  },
  exportBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '3px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'none',
    fontSize: 11,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
```

### Step 7.5: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "feat: CSV and JSON export buttons for query results

- Add exportCSV() — proper quoting for commas, quotes, newlines
- Add exportJSON() — array of objects with column names as keys
- Buttons appear in output bar between row count and view toggle
- Download filename includes date stamp"
```

---

## Task 8: Issue #5 — Pagination on audit log (backend)

**Problem:** Audit log uses "Load more" infinite scroll. No total count, no page navigation.

**Files:**
- `internal/audit/audit.go` — Add `Count` method
- `internal/api/audit_handlers.go` — Return `{ entries, total }` response
- `internal/api/audit_handlers_test.go` — Update tests

### Step 8.1: Add `Count` method to audit Logger

In `internal/audit/audit.go`, add this method after the `Query` method (after line 117):

```go
// Count returns the total number of audit entries matching the given filters
// (ignoring Limit/Offset).
func (l *Logger) Count(ctx context.Context, p QueryParams) (int, error) {
	query := `SELECT COUNT(*) FROM audit_logs WHERE org_id = $1`
	args := []any{p.OrgID}
	argN := 2

	if p.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argN)
		args = append(args, p.UserID)
		argN++
	}
	if p.Action != "" {
		query += fmt.Sprintf(" AND action ILIKE $%d", argN)
		args = append(args, "%"+p.Action+"%")
		argN++
	}
	if p.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argN)
		args = append(args, p.ResourceType)
		argN++
	}
	if p.ResourceID != "" {
		query += fmt.Sprintf(" AND resource_id = $%d", argN)
		args = append(args, p.ResourceID)
		argN++
	}

	var count int
	err := l.db.Pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return count, nil
}
```

### Step 8.2: Update audit handler to return `{ entries, total }`

In `internal/api/audit_handlers.go`, replace the entire file contents with:

```go
package api

import (
	"net/http"
	"strconv"

	"github.com/heavenlabs/hnb/internal/audit"
)

type auditListResponse struct {
	Entries []audit.Entry `json:"entries"`
	Total   int           `json:"total"`
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	q := r.URL.Query()

	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	params := audit.QueryParams{
		OrgID:        claims.OrgID,
		Limit:        limit,
		Offset:       offset,
		Action:       q.Get("action"),
		UserID:       q.Get("user_id"),
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
	}

	entries, err := s.audit.Query(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}

	total, err := s.audit.Count(ctx, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "count failed")
		return
	}

	writeJSON(w, http.StatusOK, auditListResponse{
		Entries: entries,
		Total:   total,
	})
}
```

### Step 8.3: Update existing tests

In `internal/api/audit_handlers_test.go`, the tests decode the response as `[]map[string]any`. Update them to decode the new response shape.

**Find (line ~23):**
```go
	var entries []map[string]any
	json.NewDecoder(rec.Body).Decode(&entries)
	if len(entries) == 0 {
		t.Fatal("expected audit entries")
	}
	// The notebook.create entry should have resource_name = "My Notebook"
	for _, e := range entries {
		if e["action"] == "notebook.create" {
			if e["resource_name"] != "My Notebook" {
				t.Fatalf("expected resource_name 'My Notebook', got %v", e["resource_name"])
			}
			if e["user_email"] == "" || e["user_email"] == nil {
				t.Fatalf("expected user_email, got %v", e["user_email"])
			}
			return
		}
	}
	t.Fatal("notebook.create entry not found")
```

**Replace with:**
```go
	var resp struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Entries) == 0 {
		t.Fatal("expected audit entries")
	}
	if resp.Total == 0 {
		t.Fatal("expected total > 0")
	}
	// The notebook.create entry should have resource_name = "My Notebook"
	for _, e := range resp.Entries {
		if e["action"] == "notebook.create" {
			if e["resource_name"] != "My Notebook" {
				t.Fatalf("expected resource_name 'My Notebook', got %v", e["resource_name"])
			}
			if e["user_email"] == "" || e["user_email"] == nil {
				t.Fatalf("expected user_email, got %v", e["user_email"])
			}
			return
		}
	}
	t.Fatal("notebook.create entry not found")
```

### Step 8.4: Run backend tests

```bash
task test:api 2>&1 | tail -20
```

**Expected:** All tests pass. The existing audit tests should pass with the updated response shape.

### Step 8.5: Commit

```bash
git add -A
git commit -m "feat: audit log returns {entries, total} for pagination

- Add Count() method to audit.Logger (same filters, no limit/offset)
- Handler returns auditListResponse{Entries, Total} instead of bare array
- Update existing tests to decode new response shape"
```

---

## Task 9: Issue #5 — Pagination on audit log (frontend)

**Problem:** Frontend uses "Load more" pattern. Need page-number pagination.

**Files:**
- `web/src/components/Pagination.tsx` — **NEW**
- `web/src/pages/AuditPage.tsx` — Major rewrite

### Step 9.1: Create reusable Pagination component

Create `web/src/components/Pagination.tsx`:

```tsx
import { ChevronLeft, ChevronRight } from 'lucide-react'

interface Props {
  page: number        // 0-indexed current page
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}

export function Pagination({ page, pageSize, total, onPageChange }: Props) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const startItem = total === 0 ? 0 : page * pageSize + 1
  const endItem = Math.min((page + 1) * pageSize, total)

  // Build page number array with ellipsis
  const pages = buildPageNumbers(page, totalPages)

  return (
    <div style={styles.container}>
      <span style={styles.info}>
        Showing {startItem}–{endItem} of {total.toLocaleString()} entries
      </span>
      <div style={styles.controls}>
        <button
          style={{ ...styles.btn, opacity: page === 0 ? 0.3 : 1 }}
          onClick={() => onPageChange(page - 1)}
          disabled={page === 0}
          aria-label="Previous page"
        >
          <ChevronLeft size={14} />
        </button>
        {pages.map((p, i) =>
          p === '…' ? (
            <span key={`ellipsis-${i}`} style={styles.ellipsis}>…</span>
          ) : (
            <button
              key={p}
              style={{
                ...styles.pageBtn,
                ...(p === page ? styles.pageBtnActive : {}),
              }}
              onClick={() => onPageChange(p as number)}
            >
              {(p as number) + 1}
            </button>
          )
        )}
        <button
          style={{ ...styles.btn, opacity: page >= totalPages - 1 ? 0.3 : 1 }}
          onClick={() => onPageChange(page + 1)}
          disabled={page >= totalPages - 1}
          aria-label="Next page"
        >
          <ChevronRight size={14} />
        </button>
      </div>
    </div>
  )
}

/** Build array of page numbers (0-indexed) with '…' for gaps */
function buildPageNumbers(current: number, total: number): (number | '…')[] {
  if (total <= 7) {
    return Array.from({ length: total }, (_, i) => i)
  }

  const pages: (number | '…')[] = [0]

  if (current > 2) pages.push('…')

  const start = Math.max(1, current - 1)
  const end = Math.min(total - 2, current + 1)

  for (let i = start; i <= end; i++) {
    pages.push(i)
  }

  if (current < total - 3) pages.push('…')

  pages.push(total - 1)

  return pages
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '16px 0',
    gap: 16,
    flexWrap: 'wrap',
  },
  info: {
    fontSize: 12,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  controls: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  btn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '4px 8px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  pageBtn: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    minWidth: 32,
    height: 32,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    fontFamily: 'var(--font-mono)',
  },
  pageBtnActive: {
    background: 'var(--accent)',
    color: '#fff',
    borderColor: 'var(--accent)',
  },
  ellipsis: {
    padding: '0 4px',
    fontSize: 14,
    color: 'var(--text-muted)',
    userSelect: 'none',
  },
}
```

### Step 9.2: Rewrite AuditPage to use page-number pagination

Replace the state and query logic in `web/src/pages/AuditPage.tsx`.

**Replace the state declarations (lines 14–22) with:**

```tsx
  const [page, setPage] = useState(0)
  const [total, setTotal] = useState(0)
  const [actionFilter, setActionFilter] = useState('')
  const [resourceTypeFilter, setResourceTypeFilter] = useState('')
  const [sortCol, setSortCol] = useState<'created_at' | 'action' | 'resource_type' | ''>('')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
```

(Remove `offset`, `entries`, `hasMore` state variables.)

**Replace the filter reset effect (line 26) with:**

```tsx
  useEffect(() => { setPage(0) }, [resourceTypeFilter, actionFilter])
```

**Replace the useQuery block (lines 28–42) with:**

```tsx
  const { data, isFetching, isLoading, error } = useQuery({
    queryKey: ['audit', page, resourceTypeFilter, actionFilter],
    queryFn: () => {
      const params = new URLSearchParams({
        limit: String(PAGE_SIZE),
        offset: String(page * PAGE_SIZE),
      })
      if (resourceTypeFilter) params.set('resource_type', resourceTypeFilter)
      if (actionFilter.trim()) params.set('action', actionFilter.trim())
      return api.get<{ entries: AuditEntry[]; total: number }>(`/api/v1/audit?${params}`)
    },
  })

  const entries = data?.entries ?? []
  const totalCount = data?.total ?? 0
```

**Remove the `useEffect` that processes `page` data (lines 44–52)** — no longer needed since we get entries directly.

**Remove the `sorted` variable and client-side sort (lines 54–61)** — sorting is now server-side via the backend's `ORDER BY al.created_at DESC`. For the initial implementation, keep client-side sort since the backend only sorts by `created_at DESC`. The sort buttons will sort the current page's entries client-side.

Actually, keep the sort logic but rename `sorted` to `displayEntries`:

```tsx
  const displayEntries = [...entries].sort((a, b) => {
    if (!sortCol) return 0
    let cmp = 0
    if (sortCol === 'created_at') cmp = new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    else if (sortCol === 'action') cmp = a.action.localeCompare(b.action)
    else if (sortCol === 'resource_type') cmp = (a.resource_type || '').localeCompare(b.resource_type || '')
    return sortDir === 'asc' ? cmp : -cmp
  })
```

**Remove the `handleLoadMore` function (lines 63–65).**

### Step 9.3: Update the JSX rendering

**Find the section header subtitle (around line 75):**
```tsx
<SectionHeader title="Audit Log" subtitle={`${filtered.length} entr${filtered.length !== 1 ? 'ies' : 'y'} loaded`}>
```

**Replace with:**
```tsx
<SectionHeader title="Audit Log" subtitle={`${totalCount.toLocaleString()} entr${totalCount !== 1 ? 'ies' : 'y'} total`}>
```

**Find the table rendering (around line 99):**
```tsx
              {sorted.map((entry) => (
                <AuditRow key={entry.id} entry={entry} copiedId={copiedId} onCopy={handleCopyId} />
              ))}
```

**Replace with:**
```tsx
              {displayEntries.map((entry) => (
                <AuditRow key={entry.id} entry={entry} copiedId={copiedId} onCopy={handleCopyId} />
              ))}
```

**Find the "Load more" section (around line 107–117):**
```tsx
          {!isLoading && hasMore && !actionFilter && !resourceTypeFilter && (
            <div style={styles.loadMoreWrap}>
              <button
                type="button"
                style={styles.loadMoreBtn}
                onClick={handleLoadMore}
                disabled={isFetching}
              >
                {isFetching ? 'Loading…' : 'Load more'}
              </button>
            </div>
          )}
```

**Replace with:**
```tsx
          {!isLoading && totalCount > PAGE_SIZE && (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={totalCount}
              onPageChange={setPage}
            />
          )}
```

### Step 9.4: Add Pagination import

Add at the top of AuditPage.tsx:

```tsx
import { Pagination } from '../components/Pagination'
```

### Step 9.5: Remove unused styles

Remove `loadMoreWrap` and `loadMoreBtn` from the styles object since they're no longer used.

### Step 9.6: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
task test:api 2>&1 | tail -5
```

**Expected:** All tests pass.

```bash
git add -A
git commit -m "feat: audit log page-number pagination

- Create reusable Pagination component with ellipsis for large page counts
- Backend returns {entries, total} — frontend uses page/offset math
- Replace 'Load more' button with page navigation
- Show 'Showing X–Y of Z entries' summary
- Reset to page 0 on filter change"
```

---

## Task 10: Issue #6 — Bulk actions on file list

**Problem:** Users can't multi-select files for batch move/delete.

**Files:**
- `web/src/pages/HomePage.tsx` — Major additions

### Step 10.1: Add selection state

After the existing state declarations in `HomePage` (around line 209, after `const [permissionsTarget, ...]`), add:

```tsx
  // Bulk selection
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [selectionMode, setSelectionMode] = useState(false)
```

### Step 10.2: Add selection helper functions

Add these functions inside the `HomePage` component, near the handlers section:

```tsx
  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
    if (!selectionMode) setSelectionMode(true)
  }

  const selectAll = () => {
    const allIds = [
      ...(data?.folders ?? []).map(f => `folder:${f.id}`),
      ...(data?.notebooks ?? []).map(nb => `notebook:${nb.id}`),
      ...(data?.connectors ?? []).map(c => `connector:${c.id}`),
      ...(data?.dashboards ?? []).map(d => `dashboard:${d.id}`),
    ]
    setSelected(new Set(allIds))
    setSelectionMode(true)
  }

  const clearSelection = () => {
    setSelected(new Set())
    setSelectionMode(false)
  }

  const isSelected = (type: string, id: string) => selected.has(`${type}:${id}`)

  const selectedItems = Array.from(selected).map(key => {
    const [type, id] = key.split(':') as [ResourceType, string]
    return { type, id }
  })
```

### Step 10.3: Add bulk mutations

```tsx
  const bulkDelete = useMutation({
    mutationFn: async () => {
      const promises = selectedItems.map(({ type, id }) => {
        if (type === 'folder') return api.delete(`/api/v1/folders/${id}?force=true`)
        if (type === 'notebook') return api.delete(`/api/v1/notebooks/${id}`)
        return Promise.resolve()
      })
      await Promise.all(promises)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
      clearSelection()
    },
    onError: (e: Error) => setError(e.message),
  })

  const bulkMove = useMutation({
    mutationFn: async (destFolderID: string | null) => {
      const promises = selectedItems.map(({ type, id }) => {
        if (type === 'folder') return api.put(`/api/v1/folders/${id}`, { parent_id: destFolderID })
        if (type === 'notebook') return api.put(`/api/v1/notebooks/${id}`, { folder_id: destFolderID })
        if (type === 'connector') return api.put(`/api/v1/connectors/${id}`, { folder_id: destFolderID })
        if (type === 'dashboard') return api.put(`/api/v1/dashboards/${id}`, { folder_id: destFolderID })
        return Promise.resolve()
      })
      await Promise.all(promises)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['folder-contents'] })
      qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
      qc.invalidateQueries({ queryKey: ['folder-home'] })
      clearSelection()
    },
    onError: (e: Error) => setError(e.message),
  })
```

### Step 10.4: Add bulk action toolbar

Add this state for the bulk move modal:

```tsx
  const [bulkMoving, setBulkMoving] = useState(false)
```

Add the bulk toolbar JSX right after the search bar `<div>` (after the search input section, before the breadcrumb):

```tsx
            {/* Bulk selection toolbar */}
            {selectionMode && (
              <div style={s.bulkToolbar}>
                <label style={s.bulkCheckLabel}>
                  <input
                    type="checkbox"
                    checked={selected.size > 0 && selected.size === (
                      (data?.folders?.length ?? 0) +
                      (data?.notebooks?.length ?? 0) +
                      (data?.connectors?.length ?? 0) +
                      (data?.dashboards?.length ?? 0)
                    )}
                    onChange={(e) => e.target.checked ? selectAll() : clearSelection()}
                  />
                </label>
                <span style={s.bulkCount}>
                  {selected.size} item{selected.size !== 1 ? 's' : ''} selected
                </span>
                <div style={{ flex: 1 }} />
                <button
                  style={s.bulkBtn}
                  onClick={() => setBulkMoving(true)}
                  disabled={selected.size === 0}
                >
                  Move to…
                </button>
                <button
                  style={{ ...s.bulkBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                  onClick={() => {
                    if (confirm(`Delete ${selected.size} item(s)? This cannot be undone.`)) {
                      bulkDelete.mutate()
                    }
                  }}
                  disabled={selected.size === 0 || bulkDelete.isPending}
                >
                  {bulkDelete.isPending ? 'Deleting…' : 'Delete'}
                </button>
                <button style={s.bulkCancelBtn} onClick={clearSelection}>
                  Cancel
                </button>
              </div>
            )}

            {!selectionMode && (
              <button style={s.selectBtn} onClick={() => setSelectionMode(true)}>
                Select
              </button>
            )}
```

### Step 10.5: Add checkboxes to items

For each item type (folders, notebooks, connectors, dashboards), add a checkbox when `selectionMode` is true.

**For folders** — In the folder card rendering, add a checkbox at the start of the `folderCard` div. Find:

```tsx
                    <div key={f.id} style={s.folderCard} className="card-hover">
```

**Replace with:**
```tsx
                    <div
                      key={f.id}
                      style={{
                        ...s.folderCard,
                        ...(isSelected('folder', f.id) ? { borderColor: 'var(--accent)', background: 'var(--accent-light)' } : {}),
                      }}
                      className="card-hover"
                    >
                      {selectionMode && (
                        <input
                          type="checkbox"
                          checked={isSelected('folder', f.id)}
                          onChange={() => toggleSelect(`folder:${f.id}`)}
                          style={s.itemCheckbox}
                          onClick={(e) => e.stopPropagation()}
                        />
                      )}
```

**For notebooks** — Similarly, find the notebook item div and add:

```tsx
                    <div
                      key={nb.id}
                      style={{
                        ...s.item,
                        ...(isSelected('notebook', nb.id) ? { borderColor: 'var(--accent)', background: 'var(--accent-light)' } : {}),
                      }}
                    >
                      {selectionMode && (
                        <input
                          type="checkbox"
                          checked={isSelected('notebook', nb.id)}
                          onChange={() => toggleSelect(`notebook:${nb.id}`)}
                          style={s.itemCheckbox}
                          onClick={(e) => e.stopPropagation()}
                        />
                      )}
```

Apply the same pattern for connectors and dashboards.

### Step 10.6: Add bulk move modal handler

Add a simplified bulk move modal. After the existing `{moving && <MoveModal ... />}` block:

```tsx
            {/* Bulk move modal */}
            {bulkMoving && (
              <BulkMoveModal
                count={selected.size}
                onConfirm={(destFolderID) => {
                  bulkMove.mutate(destFolderID)
                  setBulkMoving(false)
                }}
                onClose={() => setBulkMoving(false)}
              />
            )}
```

Create a `BulkMoveModal` component inside the file (before `HomePage`):

```tsx
function BulkMoveModal({ count, onConfirm, onClose }: { count: number; onConfirm: (destFolderID: string | null) => void; onClose: () => void }) {
  const [pickerFolderID, setPickerFolderID] = useState<string | null>(null)
  const [pickerAncestors, setPickerAncestors] = useState<Array<{ id: string; name: string }>>([])

  const { data, isLoading } = useQuery<FolderContents>({
    queryKey: ['bulk-move-picker', pickerFolderID ?? 'root'],
    queryFn: () => pickerFolderID
      ? api.get<FolderContents>(`/api/v1/folders/${pickerFolderID}`)
      : api.get<FolderContents>('/api/v1/folders'),
  })

  function navigateTo(folder: { id: string; name: string }) {
    setPickerAncestors(prev => [...prev, folder])
    setPickerFolderID(folder.id)
  }

  function navigateToAncestor(idx: number) {
    if (idx < 0) {
      setPickerAncestors([])
      setPickerFolderID(null)
    } else {
      const ancestor = pickerAncestors[idx]
      setPickerAncestors(prev => prev.slice(0, idx + 1))
      setPickerFolderID(ancestor.id)
    }
  }

  return (
    <div style={ms.backdrop} onClick={onClose}>
      <div style={ms.modal} onClick={(e) => e.stopPropagation()}>
        <div style={ms.modalHeader}>
          <span style={ms.modalTitle}>Move {count} item(s) to folder</span>
          <button style={ms.closeBtn} onClick={onClose}>×</button>
        </div>
        <div style={ms.pickerCrumb}>
          <button style={ms.crumbLink} onClick={() => navigateToAncestor(-1)}>Root</button>
          {pickerAncestors.map((a, idx) => (
            <span key={a.id} style={{ display: 'flex', alignItems: 'center' }}>
              <span style={{ color: 'var(--text-muted)', margin: '0 4px' }}>/</span>
              <button style={ms.crumbLink} onClick={() => navigateToAncestor(idx)}>{a.name}</button>
            </span>
          ))}
        </div>
        <div style={ms.folderList}>
          {isLoading && <div style={ms.loadingText}>Loading…</div>}
          {!isLoading && data && data.folders.length === 0 && (
            <div style={ms.emptyText}>No subfolders here.</div>
          )}
          {data?.folders.map((f) => (
            <button key={f.id} style={ms.folderRow} onClick={() => navigateTo({ id: f.id, name: f.name })}>
              <FolderIcon size={14} style={{ color: 'var(--accent)', flexShrink: 0, marginRight: 8 }} />
              <span style={{ flex: 1, textAlign: 'left', fontSize: 13 }}>{f.name}</span>
              <span style={ms.drillArrow}>›</span>
            </button>
          ))}
        </div>
        <div style={ms.modalFooter}>
          <button style={ms.moveHereBtn} onClick={() => onConfirm(pickerFolderID)}>
            Move here
          </button>
          <button style={s.cancelBtn} onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  )
}
```

### Step 10.7: Add Escape key handler for selection mode

Add an effect to exit selection mode on Escape:

```tsx
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && selectionMode) {
        clearSelection()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [selectionMode]) // eslint-disable-line react-hooks/exhaustive-deps
```

### Step 10.8: Add bulk action styles

Add to the `s` styles object:

```tsx
  bulkToolbar: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    padding: '10px 16px',
    background: 'var(--accent-light)',
    border: '1px solid var(--accent)',
    borderRadius: 4,
    marginBottom: 16,
  },
  bulkCheckLabel: {
    display: 'flex',
    alignItems: 'center',
    cursor: 'pointer',
  },
  bulkCount: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  bulkBtn: {
    padding: '5px 14px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    color: 'var(--text-primary)',
  },
  bulkCancelBtn: {
    padding: '5px 14px',
    border: 'none',
    borderRadius: 4,
    background: 'none',
    fontSize: 12,
    cursor: 'pointer',
    color: 'var(--text-muted)',
  },
  selectBtn: {
    padding: '5px 12px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'none',
    fontSize: 12,
    fontWeight: 500,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    marginBottom: 12,
  },
  itemCheckbox: {
    marginRight: 8,
    cursor: 'pointer',
    flexShrink: 0,
    accentColor: 'var(--accent)',
  },
```

### Step 10.9: Verify and commit

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

```bash
git add -A
git commit -m "feat: bulk select + move/delete on file list

- Add selection mode with checkbox multi-select
- Bulk toolbar shows count + Move/Delete/Clear actions
- Bulk move uses existing per-item API via Promise.all
- Bulk delete uses existing per-item API via Promise.all
- Escape key exits selection mode
- BulkMoveModal reuses folder picker pattern
- Visual highlight on selected items (accent border + background)"
```

---

## Task 11: Final integration testing

### Step 11.1: Run all frontend type checks

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

### Step 11.2: Run all backend tests

```bash
task test 2>&1 | tail -20
```

**Expected:** All tests pass.

### Step 11.3: Build frontend

```bash
task build:web 2>&1 | tail -10
```

**Expected:** Successful build, no errors.

### Step 11.4: Manual responsive testing checklist

Open the app in a browser and test at these breakpoints:

| Width | Test | Expected |
|-------|------|----------|
| 375px | Sidebar hidden, hamburger visible | Tap hamburger → drawer opens with nav items |
| 375px | Connector form | Single column, inputs full width |
| 375px | Dashboard editor | Widgets stacked vertically, no column selector |
| 768px | Sidebar | Forced to icon rail (48px) |
| 768px | TwoPanelLayout folder tree | 200px width |
| 1024px | Sidebar | Normal expanded/collapsed per user pref |
| Any | Audit log | Page numbers visible, "Showing X–Y of Z" |
| Any | Audit copy icon | Click → green check for 2s |
| Any | Output export | CSV and JSON buttons download files |
| Any | File list "Select" | Checkboxes appear, bulk toolbar shows |

### Step 11.5: Final commit

```bash
git add -A
git commit --allow-empty -m "chore: group 6 responsive + data management — complete

All 8 issues addressed:
- #1: Mobile sidebar auto-collapse + drawer
- #2: Tablet sidebar overlap resolved
- #3: Connector form responsive grid
- #4: Dashboard editor mobile stacked layout
- #5: Audit log page-number pagination (backend + frontend)
- #6: Bulk select/move/delete on file list
- #7: CSV + JSON export for query results
- #8: Audit copy feedback with visible icon"
```

---

## Summary of all files changed

| File | Action | Issues |
|------|--------|--------|
| `web/src/hooks/useMediaQuery.ts` | **NEW** | #1, #2 |
| `web/src/components/Sidebar.tsx` | MODIFY | #1, #2 |
| `web/src/components/TopBar.tsx` | MODIFY | #1 |
| `web/src/components/AppShell.tsx` | No changes needed | — |
| `web/src/components/TwoPanelLayout.tsx` | MODIFY | #2 |
| `web/src/pages/ConnectorsPage.tsx` | MODIFY | #3 |
| `web/src/pages/DashboardEditorPage.tsx` | MODIFY | #4 |
| `web/src/pages/AuditPage.tsx` | MODIFY | #5, #8 |
| `web/src/components/Pagination.tsx` | **NEW** | #5 |
| `internal/audit/audit.go` | MODIFY | #5 |
| `internal/api/audit_handlers.go` | MODIFY | #5 |
| `internal/api/audit_handlers_test.go` | MODIFY | #5 |
| `web/src/pages/HomePage.tsx` | MODIFY | #6 |
| `web/src/components/OutputRenderer.tsx` | MODIFY | #7 |
| `web/src/index.css` (or theme.css) | MODIFY | #1 |

## Breakpoint Reference

| Width | Sidebar | Folder Tree | Form Grid | Dashboard |
|-------|---------|-------------|-----------|-----------|
| ≥ 1024px | Expanded (200px) or collapsed (48px) per user | 240px | 2 columns | GridLayout (drag/resize) |
| 768–1023px | Forced collapsed (48px) | 200px | 2 columns | GridLayout (drag/resize) |
| 480–767px | Hidden (drawer) | Drawer | auto-fit (1-2 cols) | Stacked (no drag) |
| < 480px | Hidden (drawer) | Drawer | 1 column | Stacked (no drag) |
