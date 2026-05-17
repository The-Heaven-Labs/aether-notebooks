# Two-Panel File Explorer — Design

## Overview

Replace the current single-column homepage with a VS Code/Finder-style two-panel layout:

- **Left panel (Tree):** Collapsible folder tree showing the full hierarchy
- **Right panel (Content):** Contents of the selected folder

## Layout

```
┌─────────────────────────────────────────────────────────────┐
│  Sidebar │  Tree Panel (240px)  │  Content Panel           │
│          │                       │                          │
│  Files   │  ▼ Demon Home/        │  Breadcrumb              │
│  Home    │    ▼ ML Research      │  Files / Demon Home /    │
│  ...     │      Model Training   │    ML Research           │
│          │      Personal Notes   │                          │
│          │  ▶ Shared Projects/   │  [Recent chips]          │
│          │  ▶ Analytics/         │                          │
│          │  ▶ Engineering/      │  Folders | Notebooks |   │
│          │                       │  Dashboards | Connectors │
│          │                       │                          │
│          │                       │  (grid or list)          │
└─────────────────────────────────────────────────────────────┘
```

## Tree Panel (Left)

### Behavior
- **Width:** 240px, collapsible to 0 (icon-only at 48px)
- **Always visible** when on Files pages
- **Shows full folder tree** rooted at the current org
- Folders with children show `▼` (expanded) or `▶` (collapsed) toggle
- Clicking a folder name selects it (highlights it, opens in content panel)
- Clicking the toggle arrow expands/collapses children without selecting
- Current folder path is auto-expanded and highlighted with accent background
- Tree auto-scrolls to show current selection

### Visual
- Folder icons: `Folder` (closed) or `FolderOpen` (open) from lucide
- Indent: 16px per level
- Active folder: `var(--accent-light)` background
- Hover: subtle highlight `var(--bg-secondary)`
- Section labels: "Folders" header at top of tree

### Data Source
- Single API call to `/api/v1/folders` (root contents)
- Each folder includes `sub_folders[]` recursively if possible, OR
- Lazy-load children on expand via `/api/v1/folders/{parent_id}`

### Persistence
- Expand/collapse state saved to localStorage per folder ID
- Last selected folder remembered

## Content Panel (Right)

### Breadcrumb
- Full path from root: `Files / Demon Home / ML Research`
- Clickable segments to navigate to any ancestor
- Same style as current breadcrumb but cleaner presentation

### Content Sections
- **No "Home" section** — home folders are entry points in the tree
- Sections: Recent (if at root), Folders, Notebooks, Dashboards, Connectors
- Same grid/list layout as current, just cleaner presentation

### Empty State
- When folder has no contents

## Navigation Flow

1. User clicks folder in tree → content panel updates
2. URL updates to `/?folder={id}` (shareable)
3. Breadcrumb in content panel updates
4. Tree auto-expands to show current path
5. "Files" in sidebar → root view (`/?folder=`) with full tree visible

## Interactions

| Action | Result |
|--------|--------|
| Click folder name | Select folder, show contents |
| Click expand arrow | Toggle children visibility |
| Double-click folder | Open in new tab (maybe) |
| Click breadcrumb segment | Navigate to that folder |
| Hover folder | Highlight |
| Click toggle button on tree panel edge | Collapse/expand tree panel |

## Mobile Consideration

- On narrow screens (<768px): tree panel hidden by default
- Toggle button to show tree as overlay/drawer
- Content panel fills full width when tree hidden

## Implementation Notes

### API Impact
- Current `/api/v1/folders` returns flat list — need recursive OR fetch children per folder
- Consider adding `?recursive=true` or `?depth=2` param
- Or add new endpoint `/api/v1/folders/tree`

### Performance
- Tree shouldn't fetch all folders recursively on load
- Lazy-load children on expand (first expand fetches immediate children)
- Cache folder data in React Query

### States
- Loading: skeleton or spinner in content panel
- Empty folder: EmptyState component
- Error: ErrorBanner

## Success Criteria
- User can navigate entire folder hierarchy from tree
- Current folder always visible in tree
- Clear visual hierarchy showing depth
- Responsive behavior on mobile
- Fast (no perceptible lag on expand/collapse)