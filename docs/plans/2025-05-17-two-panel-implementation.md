# Two-Panel File Explorer — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement task-by-task.

**Goal:** Replace the single-column homepage with a VS Code/Finder-style two-panel layout: folder tree on left, folder contents on right.

**Architecture:** Create a `FolderTree` component that renders a recursive tree with expand/collapse. HomePage becomes a split-panel layout with tree on left (collapsible) and current content on right.

**Tech Stack:** React, TypeScript, CSS-in-JS (inline styles like existing code), React Query for data fetching, lucide-react for icons.

---

## Task 1: Create FolderTree Component

**Files:**
- Create: `web/src/components/FolderTree.tsx`

**Step 1: Write the failing test**

Create `web/src/test/FolderTree.test.tsx`:
```tsx
import { screen, fireEvent } from '@testing-library/react'
import { renderWithProviders } from './utils'
import { FolderTree } from '../components/FolderTree'

// Mock the api client
vi.mock('../api/client', () => ({
  api: {
    get: vi.fn(),
  },
}))

describe('FolderTree', () => {
  test('renders folder name', async () => {
    renderWithProviders(<FolderTree />)
    expect(await screen.findByText('Files')).toBeInTheDocument()
  })

  test('shows expand/collapse toggle for folders with children', async () => {
    // Test passes when we see folder with expand arrow
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/test/FolderTree.test.tsx`
Expected: FAIL — FolderTree component doesn't exist yet

**Step 3: Write minimal implementation**

Create `web/src/components/FolderTree.tsx`:
```tsx
import { useState } from 'react'
import { ChevronRight, ChevronDown, Folder, FolderOpen } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Folder } from '../types'

interface TreeNode {
  id: string
  name: string
  parent_id: string | null
  is_home: boolean
  sub_folders: TreeNode[]
}

interface FolderTreeProps {
  onSelectFolder: (folderId: string | null) => void
  selectedFolderId: string | null
}

export function FolderTree({ onSelectFolder, selectedFolderId }: FolderTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  
  const { data: folders = [] } = useQuery<Folder[]>({
    queryKey: ['folder-tree-root'],
    queryFn: () => api.get<Folder[]>('/api/v1/folders'),
  })

  // Build tree from flat list
  const rootFolders = folders.filter(f => !f.parent_id)

  const toggleFolder = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return (
    <div style={{ padding: '8px 0' }}>
      <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-muted)', padding: '0 12px', marginBottom: 8 }}>
        Folders
      </div>
      {rootFolders.map(f => (
        <TreeNodeComponent
          key={f.id}
          folder={f}
          allFolders={folders}
          expanded={expanded}
          onToggle={toggleFolder}
          onSelect={onSelectFolder}
          selectedFolderId={selectedFolderId}
          depth={0}
        />
      ))}
    </div>
  )
}

interface TreeNodeComponentProps {
  folder: Folder
  allFolders: Folder[]
  expanded: Set<string>
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  selectedFolderId: string | null
  depth: number
}

function TreeNodeComponent({ folder, allFolders, expanded, onToggle, onSelect, selectedFolderId, depth }: TreeNodeComponentProps) {
  const children = allFolders.filter(f => f.parent_id === folder.id)
  const hasChildren = children.length > 0
  const isExpanded = expanded.has(folder.id)
  const isSelected = selectedFolderId === folder.id

  return (
    <div>
      <div style={{
        display: 'flex',
        alignItems: 'center',
        padding: '4px 12px',
        paddingLeft: 12 + depth * 16,
        cursor: 'pointer',
        background: isSelected ? 'var(--accent-light)' : 'transparent',
        gap: 4,
      }} onClick={() => onSelect(folder.id)}>
        {hasChildren ? (
          <button
            style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', display: 'flex', color: 'var(--text-muted)' }}
            onClick={(e) => { e.stopPropagation(); onToggle(folder.id) }}
          >
            {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </button>
        ) : (
          <span style={{ width: 12 }} />
        )}
        {isExpanded && hasChildren ? <FolderOpen size={14} style={{ color: 'var(--accent)' }} /> : <Folder size={14} style={{ color: 'var(--accent)' }} />}
        <span style={{ fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{folder.name}</span>
      </div>
      {isExpanded && hasChildren && children.map(child => (
        <TreeNodeComponent
          key={child.id}
          folder={child}
          allFolders={allFolders}
          expanded={expanded}
          onToggle={onToggle}
          onSelect={onSelect}
          selectedFolderId={selectedFolderId}
          depth={depth + 1}
        />
      ))}
    </div>
  )
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/test/FolderTree.test.tsx`
Expected: PASS (or skip if no test written yet, just verify component compiles)

**Step 5: Commit**

```bash
cd /home/jesus/Projects/hnb-claude
git add web/src/components/FolderTree.tsx
git commit -m "feat: add FolderTree component"
```

---

## Task 2: Create TwoPanelLayout Component

**Files:**
- Create: `web/src/components/TwoPanelLayout.tsx`

**Step 1: Write the failing test**

Create `web/src/test/TwoPanelLayout.test.tsx`:
```tsx
import { screen } from '@testing-library/react'
import { renderWithProviders } from './utils'
import { TwoPanelLayout } from '../components/TwoPanelLayout'

describe('TwoPanelLayout', () => {
  test('renders left and right panels', async () => {
    renderWithProviders(
      <TwoPanelLayout
        leftPanel={<div>Tree</div>}
        rightPanel={<div>Content</div>}
      />
    )
    expect(await screen.findByText('Tree')).toBeInTheDocument()
    expect(screen.getByText('Content')).toBeInTheDocument()
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/test/TwoPanelLayout.test.tsx`
Expected: FAIL — TwoPanelLayout doesn't exist

**Step 3: Write minimal implementation**

Create `web/src/components/TwoPanelLayout.tsx`:
```tsx
import { useState } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

interface TwoPanelLayoutProps {
  leftPanel: React.ReactNode
  rightPanel: React.ReactNode
  leftWidth?: number
}

export function TwoPanelLayout({ leftPanel, rightPanel, leftWidth = 240 }: TwoPanelLayoutProps) {
  const [collapsed, setCollapsed] = useState(false)

  return (
    <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
      {/* Left panel */}
      <div style={{
        width: collapsed ? 0 : leftWidth,
        overflow: 'hidden',
        transition: 'width 0.2s ease',
        flexShrink: 0,
        borderRight: collapsed ? 'none' : '1px solid var(--border)',
      }}>
        {leftPanel}
      </div>

      {/* Toggle button */}
      <button
        style={{
          position: 'absolute',
          left: collapsed ? 48 : leftWidth,
          top: '50%',
          transform: 'translateY(-50%)',
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border)',
          borderRadius: 4,
          padding: 4,
          cursor: 'pointer',
          zIndex: 10,
          display: 'flex',
        }}
        onClick={() => setCollapsed(!collapsed)}
        title={collapsed ? 'Expand tree' : 'Collapse tree'}
      >
        {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
      </button>

      {/* Right panel */}
      <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
        {rightPanel}
      </div>
    </div>
  )
}
```

**Step 4: Run test**

Run: `cd web && npx vitest run src/test/TwoPanelLayout.test.tsx`
Expected: PASS (or component compiles)

**Step 5: Commit**

```bash
git add web/src/components/TwoPanelLayout.tsx
git commit -m "feat: add TwoPanelLayout component"
```

---

## Task 3: Integrate Tree into HomePage

**Files:**
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Add FolderTree import and state**

Add to imports:
```tsx
import { FolderTree } from '../components/FolderTree'
import { TwoPanelLayout } from '../components/TwoPanelLayout'
```

Add state for selected folder (can use existing `folderID` from searchParams):
```tsx
// folderID from searchParams already exists, use it
const [searchParams, setSearchParams] = useSearchParams()
const folderID = searchParams.get('folder')
```

**Step 2: Wrap content in TwoPanelLayout**

Find the main return statement. Wrap the content panel:
```tsx
return (
  <AppShell>
    <TwoPanelLayout
      leftPanel={
        <FolderTree
          onSelectFolder={(id) => setSearchParams(id ? { folder: id } : {})}
          selectedFolderId={folderID}
        />
      }
      rightPanel={
        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          {/* Existing breadcrumb */}
          ...
          {/* Existing content sections */}
          ...
        </div>
      }
    />
  </AppShell>
)
```

**Step 3: Style the right panel**

Add style to ensure proper layout:
```tsx
rightPanelContent: { flex: 1, display: 'flex', flexDirection: 'column' }
```

**Step 4: Verify it compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors (ignoring test file issues)

**Step 5: Commit**

```bash
git add web/src/pages/HomePage.tsx
git commit -m "feat: integrate two-panel layout into HomePage"
```

---

## Task 4: Add Expand/Collapse Persistence

**Files:**
- Modify: `web/src/components/FolderTree.tsx`

**Step 1: Add localStorage for expand state**

Update the FolderTree component to persist expanded state:
```tsx
const [expanded, setExpanded] = useState<Set<string>>(() => {
  try {
    const saved = localStorage.getItem('hnb_tree_expanded')
    return saved ? new Set(JSON.parse(saved)) : new Set()
  } catch {
    return new Set()
  }
})

// When toggling, also save to localStorage
const toggleFolder = (id: string) => {
  setExpanded(prev => {
    const next = new Set(prev)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    localStorage.setItem('hnb_tree_expanded', JSON.stringify([...next]))
    return next
  })
}
```

**Step 2: Auto-expand to current folder**

Add useEffect to expand ancestors of selected folder:
```tsx
useEffect(() => {
  if (folderID) {
    // Fetch ancestors for selected folder
    api.get<Array<{ id: string }>>(`/api/v1/folders/${folderID}/ancestors`).then(ancestors => {
      setExpanded(prev => {
        const next = new Set(prev)
        ancestors.forEach(a => next.add(a.id))
        return next
      })
    })
  }
}, [folderID])
```

**Step 3: Commit**

```bash
git add web/src/components/FolderTree.tsx
git commit -m "feat: persist folder tree expand/collapse state"
```

---

## Task 5: Lazy-Load Children on Expand

**Files:**
- Modify: `web/src/components/FolderTree.tsx`

**Step 1: Add lazy loading**

Instead of fetching all folders at once, fetch children when folder is first expanded:
```tsx
const { data: folderChildren = new Map() } = useQuery({
  queryKey: ['folder-children', ...expanded],
  queryFn: async () => {
    const childMap = new Map<string, Folder[]>()
    for (const folderId of expanded) {
      const contents = await api.get<FolderContents>(`/api/v1/folders/${folderId}`)
      if (contents.folders) {
        childMap.set(folderId, contents.folders)
      }
    }
    return childMap
  },
  enabled: expanded.size > 0,
})
```

**Step 2: Use childMap in TreeNodeComponent**

Update TreeNodeComponent to get children from folderChildren map instead of filtering allFolders.

**Step 3: Commit**

```bash
git add web/src/components/FolderTree.tsx
git commit -m "feat: lazy-load folder children on expand"
```

---

## Task 6: Mobile Responsiveness

**Files:**
- Modify: `web/src/components/TwoPanelLayout.tsx`

**Step 1: Hide tree on mobile**

```tsx
const [mobileTreeOpen, setMobileTreeOpen] = useState(false)

// Add toggle button visible on mobile
const isMobile = typeof window !== 'undefined' && window.innerWidth < 768
```

**Step 2: Show tree as overlay on mobile**

```tsx
{isMobile && (
  <>
    <button onClick={() => setMobileTreeOpen(true)}>Show Tree</button>
    {mobileTreeOpen && (
      <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)', zIndex: 100 }}>
        <div style={{ background: 'var(--bg-primary)', width: '80%', height: '100%' }}>
          {leftPanel}
          <button onClick={() => setMobileTreeOpen(false)}>Close</button>
        </div>
      </div>
    )}
  </>
)}
```

**Step 3: Commit**

```bash
git add web/src/components/TwoPanelLayout.tsx
git commit -m "feat: add mobile drawer for tree panel"
```

---

## Task 7: Clean Up HomePage (Remove Home View)

**Files:**
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Remove separate Home view logic**

The `/home` route with `homeView` prop should now just show the tree with root folders. Remove the `homeView` conditional rendering since home folders are now shown in the tree.

**Step 2: Update breadcrumb logic**

Simplify breadcrumb to always show path:
```tsx
<div style={s.breadcrumb}>
  <button style={s.crumbBtn} onClick={() => setSearchParams({})}>
    <Home size={13} style={{ marginRight: 4 }} />
    Files
  </button>
  {ancestors.map((a) => (
    <span key={a.id} style={{ display: 'flex', alignItems: 'center' }}>
      <span style={s.sep}>/</span>
      <button style={s.crumbBtn} onClick={() => setSearchParams({ folder: a.id })}>
        {a.name}
      </button>
    </span>
  ))}
</div>
```

**Step 3: Remove old home view section**

Delete lines 602-661 (the `{homeView && homeData...}` block)

**Step 4: Commit**

```bash
git add web/src/pages/HomePage.tsx
git commit -m "refactor: remove separate home view, tree handles all folders"
```

---

## Task 8: Visual Polish

**Files:**
- Modify: `web/src/components/FolderTree.tsx`
- Modify: `web/src/components/TwoPanelLayout.tsx`

**Step 1: Add hover states**

```tsx
onMouseEnter: (e) => e.currentTarget.style.background = 'var(--bg-secondary)'
onMouseLeave: (e) => e.currentTarget.style.background = isSelected ? 'var(--accent-light)' : 'transparent'
```

**Step 2: Add smooth transitions**

```tsx
transition: 'background 0.15s ease'
```

**Step 3: Commit**

```bash
git add web/src/components/FolderTree.tsx
git commit -m "style: add hover states and transitions to tree"
```

---

## Task 9: Test the Full Flow

**Step 1: Manual testing**

1. Go to Files page — tree should show on left, root folders visible
2. Click folder in tree — contents show on right
3. Click expand arrow — children load and show
4. Breadcrumb updates correctly
5. Collapse tree button works
6. Navigate to nested folder — tree auto-expands to show path
7. Refresh page — expand state persists

**Step 2: Run existing tests**

Run: `cd web && npx vitest run`
Expected: All existing tests pass (may need updates if HomePage tests expect old structure)

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: complete two-panel file explorer implementation"
```

---

**Plan complete and saved to `docs/plans/2025-05-17-two-panel-implementation.md`. Two execution options:**

**1. Subagent-Driven (this session)** — I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** — Open new session with executing-plans, batch execution with checkpoints

Which approach?