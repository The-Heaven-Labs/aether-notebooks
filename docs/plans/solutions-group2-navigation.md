# Group 2: Navigation & Information Architecture — Solution Design Doc

## Overview

This document proposes fixes for 9 navigation and information architecture issues in the hnb React frontend. Each solution prioritizes minimal code changes and maximum UX impact.

---

## Issue 1: Duplicate "+ New Notebook" Buttons on Home Page

### Current State
- **Toolbar** (line ~547): Shows three buttons: `+ New Folder`, `+ New Notebook`, `+ New Dashboard`
- **EmptyState** (line ~588): When folder is empty, shows `action={{ label: '+ New Notebook', onClick: () => setCreating('notebook') }}`
- Both trigger the same `setCreating('notebook')` inline form

### Problem
When a folder is empty, users see two "+ New Notebook" buttons — one in the toolbar and one in the empty state. This creates visual redundancy and decision fatigue.

### Proposed Fix
**Remove the "+ New Notebook" action from the EmptyState component on HomePage.** The toolbar buttons are always visible and provide a consistent creation surface. The EmptyState should focus on orientation ("this folder is empty") rather than duplicating toolbar actions.

Alternatively, if we want the empty state to be actionable, **remove the "+ New Notebook" button from the toolbar** and keep only the EmptyState action when empty. But this is worse — the toolbar should be consistent.

**Recommended: Remove the `action` prop from the EmptyState call in HomePage.**

```tsx
// Before:
<EmptyState
  icon={<FolderIcon size={32} />}
  title="This folder is empty"
  text="Create a folder or notebook to get started."
  action={{ label: '+ New Notebook', onClick: () => setCreating('notebook') }}
/>

// After:
<EmptyState
  icon={<FolderIcon size={32} />}
  title="This folder is empty"
  text="Use the buttons above to create a folder, notebook, or dashboard."
/>
```

### Files Changed
- `web/src/pages/HomePage.tsx` — Remove `action` prop from EmptyState, update text

### Dependencies
None. Purely presentational change.

---

## Issue 2: Duplicate "+ New Dashboard" Buttons on Dashboards Page

### Current State
- **SectionHeader** (line ~62): Contains `<button ...>+ New Dashboard</button>` in the header actions
- **EmptyState** (line ~79): When `dashboards.length === 0`, shows `action={{ label: '+ New Dashboard', onClick: () => setCreating(true) }}`

### Problem
Same pattern as Issue 1. When no dashboards exist, two "+ New Dashboard" buttons appear.

### Proposed Fix
**Remove the `action` prop from the EmptyState in DashboardsPage.** The SectionHeader button is the canonical creation point. Update the empty state text to guide users.

```tsx
// Before:
<EmptyState
  title="No dashboards yet"
  text="Create a dashboard to display notebook cell outputs in a shared view."
  action={{ label: '+ New Dashboard', onClick: () => setCreating(true) }}
/>

// After:
<EmptyState
  title="No dashboards yet"
  text="Create a dashboard to display notebook cell outputs in a shared view. Use the button above to get started."
/>
```

### Files Changed
- `web/src/pages/DashboardsPage.tsx` — Remove `action` prop from EmptyState, update text

### Dependencies
None.

---

## Issue 3: No Way to Navigate Back from Dashboard Editor

### Current State
The DashboardEditorPage **already has** a back link (lines ~182-186):
```tsx
<Link to="/dashboards" style={styles.backLink}>
  <ArrowLeft size={14} style={{ flexShrink: 0 }} />
  <span>Dashboards</span>
</Link>
```

### Problem
The back link exists but is minimal — it's just a text link with an arrow. Users may not notice it, especially since:
1. It's styled in muted color (`var(--text-muted)`)
2. There's no breadcrumb trail showing hierarchy
3. The sub-header has `position: sticky` with negative margins that may cause it to scroll out of view in some edge cases

### Proposed Fix
**Enhance the existing back navigation with a more prominent breadcrumb pattern.** Add the dashboard name as a clickable element and make the back link more visually distinct:

```tsx
<header style={styles.subHeader}>
  <div style={styles.headerLeft}>
    <Link to="/dashboards" style={styles.backLink}>
      <ArrowLeft size={14} style={{ flexShrink: 0 }} />
      <span>Dashboards</span>
    </Link>
    <span style={styles.breadcrumbSep}>/</span>
    {/* Dashboard title (already exists, editable inline) */}
    {editingTitle ? ( ... ) : ( ... )}
  </div>
  ...
</header>
```

The current implementation is actually functional. The fix is to:
1. Make the back link slightly more prominent (use `var(--text-secondary)` instead of `var(--text-muted)`)
2. Add `title="Back to all dashboards"` tooltip
3. Ensure the sub-header is always visible (verify sticky positioning works correctly)

```tsx
// Style change:
backLink: {
  ...
  color: 'var(--text-secondary)',  // was: var(--text-muted) — more visible
  ...
}
```

### Files Changed
- `web/src/pages/DashboardEditorPage.tsx` — Enhance back link visibility, add title attribute

### Dependencies
None.

---

## Issue 4: Notebook Breadcrumb Shows "Notebooks" but Goes to Files

### Current State
In `NotebookPage.tsx` (line ~374):
```tsx
<Link to="/" style={styles.backBtn} title="Back to notebooks">
  <ChevronLeft size={14} style={{ flexShrink: 0 }} />
  <span>Notebooks</span>
</Link>
```

The route `/` is the **Files** page (HomePage), not a "Notebooks" page. The sidebar labels it "Files". The breadcrumb says "Notebooks".

### Problem
The label "Notebooks" is misleading — clicking it takes users to the Files page (which shows folders, notebooks, connectors, and dashboards). Users expecting a notebooks-only list will be confused.

### Proposed Fix
**Change the breadcrumb label from "Notebooks" to "Files" to match the actual destination.** Also update the title attribute.

```tsx
// Before:
<Link to="/" style={styles.backBtn} title="Back to notebooks">
  <ChevronLeft size={14} style={{ flexShrink: 0 }} />
  <span>Notebooks</span>
</Link>

// After:
<Link to="/" style={styles.backBtn} title="Back to Files">
  <ChevronLeft size={14} style={{ flexShrink: 0 }} />
  <span>Files</span>
</Link>
```

**Alternative (better long-term):** If the notebook belongs to a folder, show the folder breadcrumb path: `Files / Folder Name / Notebook Title`. This would use the notebook's `folder_id` to fetch ancestors. But this is a larger change — the simple label fix resolves the immediate confusion.

### Files Changed
- `web/src/pages/NotebookPage.tsx` — Change breadcrumb text and title attribute

### Dependencies
None.

---

## Issue 5: No Loading Skeleton on Page Transitions

### Current State
- **HomePage**: Shows `"Loading…"` text (line ~568)
- **DashboardsPage**: Shows `<LoadingSpinner />` centered (line ~73)
- **DashboardEditorPage**: Shows a minimal loading dot (line ~170)
- **NotebookPage**: Shows `<LoadingPage />` (full-page spinner, line ~352)
- **theme.css**: Already has a `skeleton-pulse` keyframe animation defined (line 239) but no skeleton components use it

### Problem
Users see a flash of empty content or a generic spinner during page transitions. This feels slow and unpolished, especially on fast connections where the spinner flashes for <200ms.

### Proposed Fix
**Create a reusable `Skeleton` component and use it for page-specific loading states.**

#### New component: `web/src/components/Skeleton.tsx`

```tsx
interface SkeletonProps {
  width?: string | number
  height?: string | number
  borderRadius?: number
  style?: React.CSSProperties
  count?: number
}

export function Skeleton({ width = '100%', height = 16, borderRadius = 4, style, count = 1 }: SkeletonProps) {
  const base: React.CSSProperties = {
    width, height, borderRadius,
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

#### Page-specific skeleton layouts:

**HomePage skeleton:**
```tsx
// Replace: {isLoading && <div>Loading…</div>}
// With:
{isLoading && (
  <div>
    <Skeleton width={320} height={32} style={{ marginBottom: 20 }} />
    <Skeleton width={200} height={12} style={{ marginBottom: 12 }} />
    <Skeleton count={3} height={40} />
  </div>
)}
```

**DashboardsPage skeleton:**
```tsx
// Replace the LoadingSpinner block with:
{isLoading && (
  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
    <Skeleton count={4} height={48} />
  </div>
)}
```

**NotebookPage skeleton:**
```tsx
// Replace LoadingPage with a notebook-shaped skeleton:
{isLoading && (
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
)}
```

### Files Changed
- `web/src/components/Skeleton.tsx` — **New file**
- `web/src/pages/HomePage.tsx` — Replace loading text with skeleton
- `web/src/pages/DashboardsPage.tsx` — Replace spinner with skeleton
- `web/src/pages/NotebookPage.tsx` — Replace LoadingPage with skeleton
- `web/src/pages/DashboardEditorPage.tsx` — Replace loading dot with skeleton

### Dependencies
The `skeleton-pulse` CSS animation already exists in `theme.css`. No new CSS needed.

---

## Issue 6: Folder Tree Collapse Button Doesn't Visually Change

### Current State
In `FolderTree.tsx`, the expand/collapse toggle **does** visually change — it switches between `<ChevronRight>` (collapsed) and `<ChevronDown>` (expanded) icons (line ~213-214). The folder icon also changes between `<FolderIcon>` and `<FolderOpen>`.

### Problem
The issue may refer to the **section headers** ("Home" and "Folders") which are static labels with no collapse affordance. Users may expect to collapse these sections but there's no button to do so.

Alternatively, the issue could be about the expand/collapse animation — the chevron changes instantly with no transition, making it easy to miss.

### Proposed Fix
**Add collapsible section headers to the FolderTree.** This gives users control over the two major sections:

```tsx
// In FolderTree component, add state for section collapse:
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

Update section headers to be clickable:
```tsx
<div 
  style={{ ...sectionHeaderStyle, cursor: 'pointer' }}
  onClick={() => toggleSection('home')}
>
  {sectionsCollapsed.has('home') 
    ? <ChevronRight size={10} style={{ marginRight: 4 }} />
    : <ChevronDown size={10} style={{ marginRight: 4 }} />
  }
  Home
</div>
{!sectionsCollapsed.has('home') && homeFolders.map(f => ( ... ))}
```

### Files Changed
- `web/src/components/FolderTree.tsx` — Add collapsible section headers with chevron indicators

### Dependencies
None.

---

## Issue 7: Context Menu Doesn't Close on Outside Click

### Current State

**HomePage ContextMenu** (lines 63-70): Has a proper click-outside handler:
```tsx
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
✅ This works correctly.

**FolderTree context menu** (lines 35-40): Uses a different pattern:
```tsx
useEffect(() => {
  if (!openMenuId) return
  const close = () => setOpenMenuId(null)
  setTimeout(() => document.addEventListener('click', close), 0)
  return () => document.removeEventListener('click', close)
}, [openMenuId])
```

### Problem
The FolderTree menu uses `'click'` instead of `'mousedown'`. The `setTimeout(..., 0)` is a workaround for the opening click event immediately triggering the close handler. However, this pattern has a race condition:
- If the user clicks quickly on another menu trigger, the `click` event from the first menu's opening may fire before the listener is added (due to setTimeout), leaving the old menu open.
- The `'click'` event fires after `'mousedown'`, so there's a perceptible delay.

### Proposed Fix
**Standardize on the HomePage's pattern** — use `'mousedown'` with a ref-based containment check. Refactor the FolderTree to use the same approach:

```tsx
const menuRef = useRef<HTMLDivElement>(null)

useEffect(() => {
  if (!openMenuId) return
  function handleClick(e: MouseEvent) {
    if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
      setOpenMenuId(null)
    }
  }
  document.addEventListener('mousedown', handleClick)
  return () => document.removeEventListener('mousedown', handleClick)
}, [openMenuId])
```

Also add Escape key support to both context menus:
```tsx
useEffect(() => {
  if (!openMenuId) return
  function handleKey(e: KeyboardEvent) {
    if (e.key === 'Escape') setOpenMenuId(null)
  }
  document.addEventListener('keydown', handleKey)
  return () => document.removeEventListener('keydown', handleKey)
}, [openMenuId])
```

### Files Changed
- `web/src/components/FolderTree.tsx` — Replace click-outside pattern with mousedown + ref check, add Escape handler
- `web/src/pages/HomePage.tsx` — Add Escape key handler to ContextMenu (already has click-outside)

### Dependencies
None.

---

## Issue 8: No Keyboard Shortcut Documentation

### Current State
- **ShortcutsModal** exists at `web/src/components/ShortcutsModal.tsx` with a table of shortcuts
- **useNotebookKeyboardShortcuts** hook handles `?` key to open the modal
- The modal is **only accessible from NotebookPage** — no other page has shortcut documentation
- The `?` shortcut only works when not focused in an input/textarea

### Problem
Users have no way to discover keyboard shortcuts except by accident. The shortcuts modal exists but is hidden behind an undocumented keypress, and only on the notebook page.

### Proposed Fix
**Two-part solution:**

#### Part A: Add a "?" indicator in the TopBar or Sidebar
Add a small keyboard icon button to the TopBar that opens a global shortcuts modal:

```tsx
// In TopBar.tsx, add next to the admin link:
<button 
  style={styles.shortcutsBtn}
  onClick={() => setShowShortcuts(true)}
  title="Keyboard shortcuts (?)"
>
  <Keyboard size={14} />
</button>
```

#### Part B: Create a global ShortcutsModal with context-aware sections
Extend the existing ShortcutsModal to accept additional shortcut entries, or create a global version:

```tsx
// Global shortcuts available everywhere:
const GLOBAL_SHORTCUTS = [
  { key: '?', action: 'Show keyboard shortcuts' },
  { key: 'G then F', action: 'Go to Files' },
  { key: 'G then D', action: 'Go to Dashboards' },
  { key: 'G then C', action: 'Go to Connectors' },
]

// Notebook-specific (already in ShortcutsModal):
// ... existing entries
```

#### Part C: Register global "?" handler in AppShell
```tsx
// In AppShell.tsx:
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
```

### Files Changed
- `web/src/components/AppShell.tsx` — Add global "?" keyboard listener, shortcuts modal state
- `web/src/components/TopBar.tsx` — Add keyboard shortcuts button
- `web/src/components/ShortcutsModal.tsx` — Accept additional shortcut entries as props, add global shortcuts section

### Dependencies
The existing `ShortcutsModal` and `Modal` components can be reused. The `Keyboard` icon is available in `lucide-react`.

---

## Issue 9: Search Box Doesn't Clear with Escape

### Current State
In `HomePage.tsx` (line ~535):
```tsx
<input
  style={s.searchInput}
  type="search"
  placeholder="Search by name…"
  value={searchQuery}
  onChange={(e) => setSearchQuery(e.target.value)}
  aria-label="Search files"
/>
```

### Problem
Pressing Escape while focused on the search input does nothing. Expected behavior (matching browser convention for `type="search"`) is to clear the search query. Note: browsers natively clear `type="search"` inputs on Escape in some cases, but React's controlled input (`value={searchQuery}`) overrides this native behavior.

### Proposed Fix
**Add an onKeyDown handler to clear the search on Escape:**

```tsx
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

The `blur()` call after clearing provides clear feedback that the search is dismissed.

### Files Changed
- `web/src/pages/HomePage.tsx` — Add `onKeyDown` handler to search input

### Dependencies
None.

---

## Implementation Priority

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| 🔴 P0 | #4 Breadcrumb label mismatch | 2 min | High — fixes active confusion |
| 🔴 P0 | #9 Search Escape to clear | 2 min | High — broken expected behavior |
| 🟡 P1 | #1 Duplicate New Notebook button | 5 min | Medium — visual clutter |
| 🟡 P1 | #2 Duplicate New Dashboard button | 5 min | Medium — visual clutter |
| 🟡 P1 | #7 Context menu outside click | 15 min | Medium — interaction bug |
| 🟡 P1 | #3 Dashboard editor back navigation | 10 min | Medium — discoverability |
| 🟢 P2 | #5 Loading skeletons | 45 min | Medium — polish |
| 🟢 P2 | #6 Folder tree section collapse | 30 min | Low-Medium — nice to have |
| 🟢 P2 | #8 Keyboard shortcut documentation | 30 min | Medium — discoverability |

## Summary of All File Changes

| File | Issues |
|------|--------|
| `web/src/pages/HomePage.tsx` | #1, #7, #9 |
| `web/src/pages/DashboardsPage.tsx` | #2 |
| `web/src/pages/DashboardEditorPage.tsx` | #3, #5 |
| `web/src/pages/NotebookPage.tsx` | #4, #5 |
| `web/src/components/FolderTree.tsx` | #6, #7 |
| `web/src/components/AppShell.tsx` | #8 |
| `web/src/components/TopBar.tsx` | #8 |
| `web/src/components/ShortcutsModal.tsx` | #8 |
| `web/src/components/Skeleton.tsx` | #5 (new file) |
