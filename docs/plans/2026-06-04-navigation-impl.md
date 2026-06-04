# Implementation Plan: Navigation & Information Architecture Fixes (Group 2)

## Goal
Fix 9 navigation and information architecture issues in the hnb React frontend to improve UX consistency, discoverability, and polish.

## Architecture
All changes are frontend-only in the React/Vite/TypeScript codebase under `web/src/`. No backend changes. The app uses React Router v6, React Query, inline styles (no CSS modules), and `lucide-react` for icons.

## Tech Stack
- React 18 + TypeScript
- React Router v6 (`react-router-dom`)
- `@tanstack/react-query` for data fetching
- `lucide-react` for icons
- Inline styles (CSSProperties objects)
- Vite dev server (port 5173)

---

## Task 1: Fix Notebook Breadcrumb Label (Issue #4) — P0

**Problem:** NotebookPage breadcrumb says "Notebooks" but links to `/` (Files page). Misleading.

**File:** `web/src/pages/NotebookPage.tsx`

### Step 1.1: Change breadcrumb text and title attribute

At **line 374**, change:

```tsx
// BEFORE (line 374):
<Link to="/" style={styles.backBtn} title="Back to notebooks">
  <ChevronLeft size={14} style={{ flexShrink: 0 }} />
  <span>Notebooks</span>
</Link>
```

```tsx
// AFTER:
<Link to="/" style={styles.backBtn} title="Back to Files">
  <ChevronLeft size={14} style={{ flexShrink: 0 }} />
  <span>Files</span>
</Link>
```

### Step 1.2: Commit

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "fix: change notebook breadcrumb from 'Notebooks' to 'Files' to match destination"
```

---

## Task 2: Add Escape-to-Clear on Search Box (Issue #9) — P0

**Problem:** Pressing Escape in the HomePage search input does nothing. Expected: clear the query.

**File:** `web/src/pages/HomePage.tsx`

### Step 2.1: Add onKeyDown handler to search input

At **line 535**, the search input currently is:

```tsx
// BEFORE (lines 535-542):
<input
  style={s.searchInput}
  type="search"
  placeholder="Search by name…"
  value={searchQuery}
  onChange={(e) => setSearchQuery(e.target.value)}
  aria-label="Search files"
/>
```

Replace with:

```tsx
// AFTER:
<input
  style={s.searchInput}
  type="search"
  placeholder="Search by name…"
  value={searchQuery}
  onChange={(e) => setSearchQuery(e.target.value)}
  onKeyDown={(e) => {
    if (e.key === 'Escape') {
      setSearchQuery('')
      e.currentTarget.blur()
    }
  }}
  aria-label="Search files"
/>
```

### Step 2.2: Commit

```bash
git add web/src/pages/HomePage.tsx
git commit -m "fix: clear search input on Escape key press"
```

---

## Task 3: Remove Duplicate "+ New Notebook" Button (Issue #1) — P1

**Problem:** When a folder is empty, two "+ New Notebook" buttons appear — one in the toolbar, one in the EmptyState.

**File:** `web/src/pages/HomePage.tsx`

### Step 3.1: Remove action prop from EmptyState and update text

At **lines 588-592**, change:

```tsx
// BEFORE (lines 588-592):
<EmptyState
  icon={<FolderIcon size={32} />}
  title="This folder is empty"
  text="Create a folder or notebook to get started."
  action={{ label: '+ New Notebook', onClick: () => setCreating('notebook') }}
/>
```

```tsx
// AFTER:
<EmptyState
  icon={<FolderIcon size={32} />}
  title="This folder is empty"
  text="Use the buttons above to create a folder, notebook, or dashboard."
/>
```

### Step 3.2: Commit

```bash
git add web/src/pages/HomePage.tsx
git commit -m "fix: remove duplicate '+ New Notebook' button from empty state"
```

---

## Task 4: Remove Duplicate "+ New Dashboard" Button (Issue #2) — P1

**Problem:** When no dashboards exist, two "+ New Dashboard" buttons appear — one in the SectionHeader, one in the EmptyState.

**File:** `web/src/pages/DashboardsPage.tsx`

### Step 4.1: Remove action prop from EmptyState and update text

At **lines 79-83**, change:

```tsx
// BEFORE (lines 79-83):
<EmptyState
  title="No dashboards yet"
  text="Create a dashboard to display notebook cell outputs in a shared view."
  action={{ label: '+ New Dashboard', onClick: () => setCreating(true) }}
/>
```

```tsx
// AFTER:
<EmptyState
  title="No dashboards yet"
  text="Create a dashboard to display notebook cell outputs in a shared view. Use the button above to get started."
/>
```

### Step 4.2: Commit

```bash
git add web/src/pages/DashboardsPage.tsx
git commit -m "fix: remove duplicate '+ New Dashboard' button from empty state"
```

---

## Task 5: Enhance Dashboard Editor Back Navigation (Issue #3) — P1

**Problem:** The back link in DashboardEditorPage exists but is too subtle — muted color, no tooltip.

**File:** `web/src/pages/DashboardEditorPage.tsx`

### Step 5.1: Make back link more visible and add tooltip

At **lines 182-185**, change the back link:

```tsx
// BEFORE (lines 182-185):
<Link to="/dashboards" style={styles.backLink}>
  <ArrowLeft size={14} style={{ flexShrink: 0 }} />
  <span>Dashboards</span>
</Link>
```

```tsx
// AFTER:
<Link to="/dashboards" style={styles.backLink} title="Back to all dashboards">
  <ArrowLeft size={14} style={{ flexShrink: 0 }} />
  <span>Dashboards</span>
</Link>
```

### Step 5.2: Update backLink style color for better visibility

In the `styles` object at **line ~388**, change the `backLink` style:

```tsx
// BEFORE:
backLink: {
  display: 'flex',
  alignItems: 'center',
  gap: 5,
  color: 'var(--text-muted)',
  textDecoration: 'none',
  fontSize: 13,
  fontWeight: 500,
  flexShrink: 0,
},
```

```tsx
// AFTER:
backLink: {
  display: 'flex',
  alignItems: 'center',
  gap: 5,
  color: 'var(--text-secondary)',
  textDecoration: 'none',
  fontSize: 13,
  fontWeight: 500,
  flexShrink: 0,
},
```

### Step 5.3: Commit

```bash
git add web/src/pages/DashboardEditorPage.tsx
git commit -m "fix: improve dashboard editor back link visibility and add tooltip"
```

---

## Task 6: Fix Context Menu Outside Click (Issue #7) — P1

**Problem:** FolderTree context menu uses `'click'` event with `setTimeout` workaround instead of the more reliable `'mousedown'` pattern. Also lacks Escape key support.

**File:** `web/src/components/FolderTree.tsx`

### Step 6.1: Add useRef import

At **line 1**, the import currently is:

```tsx
// BEFORE (line 1):
import { useState, useEffect } from 'react'
```

```tsx
// AFTER:
import { useState, useEffect, useRef } from 'react'
```

### Step 6.2: Replace the click-outside handler in FolderTree component

At **lines 35-40**, the current handler is:

```tsx
// BEFORE (lines 35-40):
useEffect(() => {
  if (!openMenuId) return
  const close = () => setOpenMenuId(null)
  setTimeout(() => document.addEventListener('click', close), 0)
  return () => document.removeEventListener('click', close)
}, [openMenuId])
```

Replace with a `mousedown`-based handler plus Escape key support:

```tsx
// AFTER:
useEffect(() => {
  if (!openMenuId) return
  function handleClick(e: MouseEvent) {
    // Check if click is inside the menu portal (fixed-positioned menu)
    const menus = document.querySelectorAll('[data-folder-menu]')
    for (const menu of menus) {
      if (menu.contains(e.target as Node)) return
    }
    setOpenMenuId(null)
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === 'Escape') setOpenMenuId(null)
  }
  document.addEventListener('mousedown', handleClick)
  document.addEventListener('keydown', handleKey)
  return () => {
    document.removeEventListener('mousedown', handleClick)
    document.removeEventListener('keydown', handleKey)
  }
}, [openMenuId])
```

### Step 6.3: Add data attribute to the menu div for containment checking

At **line ~243**, the menu div currently is:

```tsx
// BEFORE:
<div
  style={{
    position: 'fixed',
    top: menuPos.top,
    left: menuPos.left,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    boxShadow: 'var(--shadow-md)',
    minWidth: 140,
    padding: '4px 0',
    zIndex: 1000,
  }}
>
```

Add `data-folder-menu` attribute:

```tsx
// AFTER:
<div
  data-folder-menu=""
  style={{
    position: 'fixed',
    top: menuPos.top,
    left: menuPos.left,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    boxShadow: 'var(--shadow-md)',
    minWidth: 140,
    padding: '4px 0',
    zIndex: 1000,
  }}
>
```

### Step 6.4: Add Escape key handler to HomePage ContextMenu

**File:** `web/src/pages/HomePage.tsx`

In the `ContextMenu` component, at **lines 63-70**, the current handler is:

```tsx
// BEFORE (lines 63-70):
useEffect(() => {
  function handleClick(e: MouseEvent) {
    if (ref.current && !ref.current.contains(e.target as Node)) {
      onClose()
    }
  }
  document.addEventListener('mousedown', handleClick)
  return () => document.removeEventListener('mousedown', handleClick)
}, [onClose])
```

Add Escape key handler after it:

```tsx
// AFTER (add right after the existing useEffect, before the return statement):
useEffect(() => {
  function handleClick(e: MouseEvent) {
    if (ref.current && !ref.current.contains(e.target as Node)) {
      onClose()
    }
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
  document.addEventListener('mousedown', handleClick)
  document.addEventListener('keydown', handleKey)
  return () => {
    document.removeEventListener('mousedown', handleClick)
    document.removeEventListener('keydown', handleKey)
  }
}, [onClose])
```

### Step 6.5: Commit

```bash
git add web/src/components/FolderTree.tsx web/src/pages/HomePage.tsx
git commit -m "fix: use mousedown for context menu outside click, add Escape key support"
```

---

## Task 7: Create Skeleton Component and Add Loading Skeletons (Issue #5) — P2

**Problem:** Pages show generic spinners or "Loading…" text during transitions instead of content-shaped skeletons.

### Step 7.1: Create the Skeleton component

**New file:** `web/src/components/Skeleton.tsx`

```tsx
import type React from 'react'

interface SkeletonProps {
  width?: string | number
  height?: string | number
  borderRadius?: number
  style?: React.CSSProperties
  count?: number
}

export function Skeleton({ width = '100%', height = 16, borderRadius = 4, style, count = 1 }: SkeletonProps) {
  const base: React.CSSProperties = {
    width,
    height,
    borderRadius,
    background: 'var(--border-light)',
    animation: 'skeleton-pulse 1.5s ease-in-out infinite',
    ...style,
  }
  if (count === 1) return <div style={base} />
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {Array.from({ length: count }, (_, i) => <div key={i} style={base} />)}
    </div>
  )
}
```

### Step 7.2: Replace loading state in HomePage

**File:** `web/src/pages/HomePage.tsx`

Add import at the top (after existing imports, around **line 13**):

```tsx
import { Skeleton } from '../components/Skeleton'
```

At **line 568**, replace:

```tsx
// BEFORE (line 568):
{isLoading && <div style={{ padding: 32, color: 'var(--text-muted)', fontSize: 14 }}>Loading…</div>}
```

```tsx
// AFTER:
{isLoading && (
  <div style={{ padding: '8px 0' }}>
    <Skeleton width={200} height={12} style={{ marginBottom: 16 }} />
    <Skeleton count={4} height={40} />
  </div>
)}
```

### Step 7.3: Replace loading state in DashboardsPage

**File:** `web/src/pages/DashboardsPage.tsx`

Add import at the top (after existing imports, around **line 10**):

```tsx
import { Skeleton } from '../components/Skeleton'
```

At **lines 73-75**, replace:

```tsx
// BEFORE (lines 73-75):
<div style={{ display: 'flex', justifyContent: 'center', padding: '80px 0' }}>
  <LoadingSpinner />
</div>
```

```tsx
// AFTER:
<div style={{ padding: '8px 0' }}>
  <Skeleton count={4} height={48} />
</div>
```

Also remove the unused `LoadingSpinner` import at **line 9**:

```tsx
// BEFORE (line 9):
import { LoadingSpinner } from '../components/LoadingSpinner'
```

Remove this line entirely.

### Step 7.4: Replace loading state in DashboardEditorPage

**File:** `web/src/pages/DashboardEditorPage.tsx`

Add import at the top (after existing imports, around **line 14**):

```tsx
import { Skeleton } from '../components/Skeleton'
```

At **lines 168-173**, replace:

```tsx
// BEFORE (lines 168-173):
if (isLoading) {
  return (
    <div style={styles.loadingPage}>
      <div style={styles.loadingDot} />
    </div>
  )
}
```

```tsx
// AFTER:
if (isLoading) {
  return (
    <AppShell>
      <div style={{ padding: '40px' }}>
        <Skeleton width={120} height={14} style={{ marginBottom: 16 }} />
        <Skeleton width={300} height={28} style={{ marginBottom: 24 }} />
        <Skeleton height={200} style={{ marginBottom: 16 }} />
        <Skeleton height={120} />
      </div>
    </AppShell>
  )
}
```

### Step 7.5: Replace loading state in NotebookPage

**File:** `web/src/pages/NotebookPage.tsx`

Add import at the top (after existing imports, around **line 19**):

```tsx
import { Skeleton } from '../components/Skeleton'
```

At **lines 350-354**, replace:

```tsx
// BEFORE (lines 350-354):
if (isLoading) return (
  <AppShell noPadding>
    <LoadingPage />
  </AppShell>
)
```

```tsx
// AFTER:
if (isLoading) return (
  <AppShell noPadding>
    <div style={{ padding: '32px 40px' }}>
      <Skeleton width={120} height={14} style={{ marginBottom: 16 }} />
      <Skeleton width={400} height={32} style={{ marginBottom: 8 }} />
      <Skeleton width={300} height={16} style={{ marginBottom: 32 }} />
      <Skeleton height={120} style={{ marginBottom: 12 }} />
      <Skeleton height={120} style={{ marginBottom: 12 }} />
      <Skeleton height={80} />
    </div>
  </AppShell>
)
```

Also remove the unused `LoadingPage` import at **line 5**:

```tsx
// BEFORE (line 5):
import { LoadingPage } from '../components/LoadingPage'
```

Remove this line entirely.

### Step 7.6: Commit

```bash
git add web/src/components/Skeleton.tsx web/src/pages/HomePage.tsx web/src/pages/DashboardsPage.tsx web/src/pages/DashboardEditorPage.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: add Skeleton component and replace loading spinners with content-shaped skeletons"
```

---

## Task 8: Add Collapsible Section Headers to FolderTree (Issue #6) — P2

**Problem:** The "Home" and "Folders" section headers in FolderTree are static labels. Users can't collapse these sections.

**File:** `web/src/components/FolderTree.tsx`

### Step 8.1: Add state for collapsed sections

In the `FolderTree` function component, after the `expanded` state declaration (**around line 22**), add:

```tsx
const [sectionsCollapsed, setSectionsCollapsed] = useState<Set<string>>(() => {
  try {
    const saved = localStorage.getItem('hnb_tree_sections')
    return saved ? new Set(JSON.parse(saved)) : new Set()
  } catch { return new Set() }
})

const toggleSection = (name: string) => {
  setSectionsCollapsed(prev => {
    const next = new Set(prev)
    if (next.has(name)) next.delete(name)
    else next.add(name)
    localStorage.setItem('hnb_tree_sections', JSON.stringify([...next]))
    return next
  })
}
```

### Step 8.2: Make the "Home" section header clickable

At **lines ~122-124**, the Home section header currently is:

```tsx
// BEFORE:
<div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8 }}>
  Home
</div>
```

Replace with:

```tsx
// AFTER:
<div
  style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 4, userSelect: 'none' }}
  onClick={() => toggleSection('home')}
>
  {sectionsCollapsed.has('home')
    ? <ChevronRight size={10} style={{ flexShrink: 0 }} />
    : <ChevronDown size={10} style={{ flexShrink: 0 }} />
  }
  Home
</div>
```

### Step 8.3: Conditionally render Home section content

Wrap the home folders rendering. At **lines ~125-139**, wrap with a conditional:

```tsx
// BEFORE:
{homeFolders.map(f => (
  <TreeNodeComponent
    key={f.id}
    ...
  />
))}
```

```tsx
// AFTER:
{!sectionsCollapsed.has('home') && homeFolders.map(f => (
  <TreeNodeComponent
    key={f.id}
    ...
  />
))}
```

### Step 8.4: Make the "Folders" section header clickable

At **lines ~143-145**, the Folders section header currently is:

```tsx
// BEFORE:
<div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8 }}>
  Folders
</div>
```

Replace with:

```tsx
// AFTER:
<div
  style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 4, userSelect: 'none' }}
  onClick={() => toggleSection('folders')}
>
  {sectionsCollapsed.has('folders')
    ? <ChevronRight size={10} style={{ flexShrink: 0 }} />
    : <ChevronDown size={10} style={{ flexShrink: 0 }} />
  }
  Folders
</div>
```

### Step 8.5: Conditionally render Folders section content

Wrap the org folders rendering. At **lines ~146-160**, wrap with a conditional:

```tsx
// BEFORE:
{orgFolders.map(f => (
  <TreeNodeComponent
    key={f.id}
    ...
  />
))}
```

```tsx
// AFTER:
{!sectionsCollapsed.has('folders') && orgFolders.map(f => (
  <TreeNodeComponent
    key={f.id}
    ...
  />
))}
```

### Step 8.6: Commit

```bash
git add web/src/components/FolderTree.tsx
git commit -m "feat: add collapsible section headers to folder tree sidebar"
```

---

## Task 9: Add Global Keyboard Shortcut Documentation (Issue #8) — P2

**Problem:** The shortcuts modal exists but is only accessible from NotebookPage via `?`. Users on other pages have no way to discover shortcuts.

### Step 9.1: Extend ShortcutsModal to accept additional shortcut entries

**File:** `web/src/components/ShortcutsModal.tsx`

Replace the entire file content:

```tsx
// BEFORE (entire file):
import type React from 'react'
import { Modal } from './Modal'

interface Props { onClose: () => void }

const SHORTCUTS = [
  { key: 'Shift+Enter', action: 'Run focused cell' },
  { key: 'B', action: 'Add code cell' },
  { key: 'A', action: 'Add code cell' },
  { key: 'D D', action: 'Delete cell' },
  { key: 'J / ↓', action: 'Move focus down' },
  { key: 'K / ↑', action: 'Move focus up' },
  { key: 'M', action: 'Convert to markdown' },
  { key: 'Y', action: 'Convert to code' },
  { key: '?', action: 'Show this modal' },
  { key: 'Ctrl+Enter (in editor)', action: 'Run cell' },
  { key: 'Ctrl+Shift+F (in SQL editor)', action: 'Format SQL' },
  { key: 'Escape (in editor)', action: 'Exit cell edit mode' },
]

export function ShortcutsModal({ onClose }: Props) {
  return (
    <Modal title="Keyboard Shortcuts" onClose={onClose}>
      <table style={styles.table}>
        <tbody>
          {SHORTCUTS.map(({ key, action }) => (
            <tr key={key}>
              <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
              <td style={styles.action}>{action}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Modal>
  )
}

const styles: Record<string, React.CSSProperties> = {
  table: { width: '100%', borderCollapse: 'collapse', padding: '8px 20px' },
  key: { padding: '8px 20px 8px', width: 160 },
  kbd: { fontFamily: 'var(--font-mono)', fontSize: 11, background: '#f5f5f5', border: '1px solid #e8e8e8', borderRadius: 3, padding: '2px 6px' },
  action: { padding: '8px 20px 8px 0', fontSize: 13, color: 'var(--text-secondary)' },
}
```

```tsx
// AFTER (entire file):
import type React from 'react'
import { Modal } from './Modal'

interface ShortcutEntry {
  key: string
  action: string
}

interface Props {
  onClose: () => void
  extraShortcuts?: ShortcutEntry[]
}

const NOTEBOOK_SHORTCUTS: ShortcutEntry[] = [
  { key: 'Shift+Enter', action: 'Run focused cell' },
  { key: 'B', action: 'Add code cell below' },
  { key: 'A', action: 'Add code cell above' },
  { key: 'D D', action: 'Delete cell' },
  { key: 'J / ↓', action: 'Move focus down' },
  { key: 'K / ↑', action: 'Move focus up' },
  { key: 'M', action: 'Convert to markdown' },
  { key: 'Y', action: 'Convert to code' },
  { key: 'Ctrl+Enter (in editor)', action: 'Run cell' },
  { key: 'Ctrl+Shift+F (in SQL editor)', action: 'Format SQL' },
  { key: 'Escape (in editor)', action: 'Exit cell edit mode' },
]

const GLOBAL_SHORTCUTS: ShortcutEntry[] = [
  { key: '?', action: 'Show keyboard shortcuts' },
]

export function ShortcutsModal({ onClose, extraShortcuts }: Props) {
  return (
    <Modal title="Keyboard Shortcuts" onClose={onClose}>
      <div style={styles.body}>
        <div style={styles.section}>
          <div style={styles.sectionTitle}>Global</div>
          <table style={styles.table}>
            <tbody>
              {GLOBAL_SHORTCUTS.map(({ key, action }) => (
                <tr key={key}>
                  <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                  <td style={styles.action}>{action}</td>
                </tr>
              ))}
              {extraShortcuts?.map(({ key, action }) => (
                <tr key={key}>
                  <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                  <td style={styles.action}>{action}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionTitle}>Notebook Editor</div>
          <table style={styles.table}>
            <tbody>
              {NOTEBOOK_SHORTCUTS.map(({ key, action }) => (
                <tr key={key}>
                  <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                  <td style={styles.action}>{action}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </Modal>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { padding: '8px 0' },
  section: { marginBottom: 16 },
  sectionTitle: { fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '8px 20px 4px' },
  table: { width: '100%', borderCollapse: 'collapse' },
  key: { padding: '6px 20px 6px', width: 200 },
  kbd: { fontFamily: 'var(--font-mono)', fontSize: 11, background: '#f5f5f5', border: '1px solid #e8e8e8', borderRadius: 3, padding: '2px 6px' },
  action: { padding: '6px 20px 6px 0', fontSize: 13, color: 'var(--text-secondary)' },
}
```

### Step 9.2: Add global "?" handler and shortcuts button to AppShell

**File:** `web/src/components/AppShell.tsx`

Replace the entire file:

```tsx
// BEFORE (entire file):
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'

interface Props {
  children: React.ReactNode
  noPadding?: boolean
}

export function AppShell({ children, noPadding }: Props) {
  return (
    <div style={styles.root}>
      <TopBar />
      <div style={styles.body}>
        <Sidebar />
        <main style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' } : {}) }}>{children}</main>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', height: '100vh', maxHeight: '100vh', overflow: 'hidden', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0 },
  main: { flex: 1, overflow: 'auto', padding: '32px', minHeight: 0 },
}
```

```tsx
// AFTER (entire file):
import { useState, useEffect } from 'react'
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'
import { ShortcutsModal } from './ShortcutsModal'

interface Props {
  children: React.ReactNode
  noPadding?: boolean
}

export function AppShell({ children, noPadding }: Props) {
  const [showShortcuts, setShowShortcuts] = useState(false)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement).tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return
      if (e.key === '?') {
        e.preventDefault()
        setShowShortcuts(true)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  return (
    <div style={styles.root}>
      <TopBar onShowShortcuts={() => setShowShortcuts(true)} />
      <div style={styles.body}>
        <Sidebar />
        <main style={{ ...styles.main, background: 'var(--bg-primary)', ...(noPadding ? { padding: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' } : {}) }}>{children}</main>
      </div>
      {showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', height: '100vh', maxHeight: '100vh', overflow: 'hidden', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden', minHeight: 0 },
  main: { flex: 1, overflow: 'auto', padding: '32px', minHeight: 0 },
}
```

### Step 9.3: Add shortcuts button to TopBar

**File:** `web/src/components/TopBar.tsx`

Add `Keyboard` import from lucide-react. At **line 3**, change:

```tsx
// BEFORE (line 3):
import { useState, useRef, useEffect } from 'react'
```

No change to React imports. Add a new import after **line 4** (`import { Link } from 'react-router-dom'`):

Actually, looking at the file, there's no lucide-react import yet. Add after **line 4**:

```tsx
import { Keyboard } from 'lucide-react'
```

Update the TopBar interface to accept the `onShowShortcuts` prop. At **line 22**, change:

```tsx
// BEFORE (line 22):
export function TopBar() {
```

```tsx
// AFTER:
interface TopBarProps {
  onShowShortcuts?: () => void
}

export function TopBar({ onShowShortcuts }: TopBarProps) {
```

Add the shortcuts button in the header, before the admin link. At **line 59** (before `{isPlatformAdmin && (`):

```tsx
// BEFORE:
{isPlatformAdmin && (
  <Link to="/admin" style={styles.adminLink}>Admin</Link>
)}
```

```tsx
// AFTER:
{onShowShortcuts && (
  <button
    style={styles.shortcutsBtn}
    onClick={onShowShortcuts}
    title="Keyboard shortcuts (?)"
    aria-label="Keyboard shortcuts"
  >
    <Keyboard size={14} />
  </button>
)}
{isPlatformAdmin && (
  <Link to="/admin" style={styles.adminLink}>Admin</Link>
)}
```

Add the `shortcutsBtn` style to the styles object. After the `adminLink` style (around **line 107**), add:

```tsx
shortcutsBtn: {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: 'none',
  border: '1px solid var(--nav-border)',
  borderRadius: 4,
  padding: '5px 7px',
  cursor: 'pointer',
  color: 'var(--nav-text-muted)',
},
```

### Step 9.4: Remove duplicate "?" handler from NotebookPage

Since `?` is now handled globally in AppShell, remove the duplicate from NotebookPage's `useNotebookKeyboardShortcuts` hook.

**File:** `web/src/hooks/useNotebookKeyboardShortcuts.ts`

At **line 37**, remove the `?` handler:

```tsx
// BEFORE (line 37):
if (e.key === '?') { actions.openShortcutsModal(); return }
```

Remove this line entirely.

Also remove `openShortcutsModal` from the `ShortcutActions` interface at **line 11**:

```tsx
// BEFORE (line 11):
openShortcutsModal: () => void
```

Remove this line.

**File:** `web/src/pages/NotebookPage.tsx`

Remove the `openShortcutsModal` callback from the hook call. At **line ~318**:

```tsx
// BEFORE:
openShortcutsModal: () => setShowShortcuts(true),
```

Remove this line.

The `showShortcuts` state and `ShortcutsModal` rendering in NotebookPage can remain — they serve as a notebook-specific modal. But since the global AppShell modal now handles `?`, we can simplify. However, to avoid breaking the notebook-specific shortcuts display, keep the existing `showShortcuts` state and modal in NotebookPage but remove the `?` trigger (it's now global). The notebook page can still show its modal via other means if needed.

Actually, since the global AppShell modal now covers all shortcuts (including notebook ones), we should remove the notebook-specific shortcuts modal entirely to avoid confusion.

Remove from **NotebookPage.tsx**:
- Line ~108: `const [showShortcuts, setShowShortcuts] = useState(false)` — remove this line
- Line ~318: `openShortcutsModal: () => setShowShortcuts(true),` — remove this line
- Line ~473: `{showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}` — remove this line
- Line ~18: `import { ShortcutsModal } from '../components/ShortcutsModal'` — remove this line

### Step 9.5: Commit

```bash
git add web/src/components/ShortcutsModal.tsx web/src/components/AppShell.tsx web/src/components/TopBar.tsx web/src/hooks/useNotebookKeyboardShortcuts.ts web/src/pages/NotebookPage.tsx
git commit -m "feat: add global keyboard shortcut documentation accessible from all pages"
```

---

## Task 10: Final Verification

### Step 10.1: Run the TypeScript compiler to check for errors

```bash
cd web && npx tsc --noEmit
```

Expected: No errors. If there are type errors, fix them.

### Step 10.2: Start the dev server and manually verify

```bash
task dev:web
```

Then open `http://localhost:5173` and verify:

1. **Issue #1**: Navigate to an empty folder on HomePage — only toolbar buttons visible, no duplicate "+ New Notebook" in empty state
2. **Issue #2**: Navigate to Dashboards page with no dashboards — only header button visible, no duplicate in empty state
3. **Issue #3**: Open a dashboard editor — back link is visible with tooltip "Back to all dashboards"
4. **Issue #4**: Open a notebook — breadcrumb says "Files" not "Notebooks"
5. **Issue #5**: Navigate between pages — see skeleton loading instead of spinners
6. **Issue #6**: In the sidebar folder tree, click "Home" and "Folders" section headers to collapse/expand them
7. **Issue #7**: Right-click a folder in the tree to open context menu, click outside — menu closes. Press Escape — menu closes.
8. **Issue #8**: Press `?` on any page — shortcuts modal appears. Click keyboard icon in TopBar — same modal.
9. **Issue #9**: Type in the search box on HomePage, press Escape — search clears and input loses focus.

### Step 10.3: Run existing tests

```bash
task test
```

Expected: All existing tests pass.

### Step 10.4: Final commit (if any fixes needed)

```bash
git add -A
git commit -m "fix: address any remaining issues from verification"
```

---

## Summary of All File Changes

| File | Issues | Changes |
|------|--------|---------|
| `web/src/pages/NotebookPage.tsx` | #4, #5, #8 | Breadcrumb label fix, skeleton loading, remove shortcuts modal |
| `web/src/pages/HomePage.tsx` | #1, #7, #9, #5 | Remove duplicate button, Escape on context menu + search, skeleton loading |
| `web/src/pages/DashboardsPage.tsx` | #2, #5 | Remove duplicate button, skeleton loading |
| `web/src/pages/DashboardEditorPage.tsx` | #3, #5 | Enhance back link, skeleton loading |
| `web/src/components/FolderTree.tsx` | #6, #7 | Collapsible sections, fix context menu outside click |
| `web/src/components/AppShell.tsx` | #8 | Global `?` handler, shortcuts modal state |
| `web/src/components/TopBar.tsx` | #8 | Keyboard shortcuts button |
| `web/src/components/ShortcutsModal.tsx` | #8 | Accept extra shortcuts, sectioned layout |
| `web/src/components/Skeleton.tsx` | #5 | **New file** — reusable skeleton component |
| `web/src/hooks/useNotebookKeyboardShortcuts.ts` | #8 | Remove `?` handler (now global) |

## Commit History (Expected)

```
1. fix: change notebook breadcrumb from 'Notebooks' to 'Files' to match destination
2. fix: clear search input on Escape key press
3. fix: remove duplicate '+ New Notebook' button from empty state
4. fix: remove duplicate '+ New Dashboard' button from empty state
5. fix: improve dashboard editor back link visibility and add tooltip
6. fix: use mousedown for context menu outside click, add Escape key support
7. feat: add Skeleton component and replace loading spinners with content-shaped skeletons
8. feat: add collapsible section headers to folder tree sidebar
9. feat: add global keyboard shortcut documentation accessible from all pages
```
