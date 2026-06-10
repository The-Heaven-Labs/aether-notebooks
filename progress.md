# Progress

## Status
In Progress — Wave 3 complete (Items 34, 42)

## Branch
`feat/all-improvements-2026-06-09`

## Completed Items

### Wave 1 (Quick Wins)
- Item 2: Keyboard shortcuts modal dark mode
- Item 3: Folder tree collapse button visibility/behavior
- Item 28: Sidebar icon swap (Members/Groups)
- Item 1: Invite link fix (API URL + /join route)
- Item 25: Collapse/show all cells toggle
- Item 36: New cell scroll into view
- Item 6: ConfirmDialog component
- Item 18: Agent max_turns configurable (default 90)

### Wave 2 (Backend Agent Features)
- Item 16: Skills discovery — list_skills tool + enhanced system prompts
- Item 17: Trigger skills via /skill:<name>
- Item 30: Subagent spawning fix (launches background execution)
- Item 31: get_notebook_context tool with safeguards

### Wave 2b (Frontend)
- Item 4: Dashboard edit mode toggle (Edit/View links)
- Item 7: Remove predefined permission profiles
- Item 8: Edit already placed widgets (pencil icon)
- Item 10: Per-widget play button with loading state

### Wave 3 (Test Connection Buttons)
- Item 34: MCP config test button (backend + frontend)
- Item 42: OIDC provider form test/validate button (backend + frontend)

## Remaining Items
- Item 9: Audit page cell execution logging
- Item 11: Dashboard permission system
- Item 13: Cell execution metrics
- Item 19: Full-screen image viewer with zoom
- Item 21: OpenAPI documentation with swagger
- Item 22: Personal access tokens
- Item 23: Admin MOTD configuration
- Item 27: Trigger agent modal from outside notebooks
- Item 29: Audit page filter improvements
- Item 32: Import/export notebooks with .ipynb support
- Item 33: Scalable skill/MCP selector UI
- Item 35: Multi-cell selection keyboard navigation fix
- Item 37: Typing slow with many rows (memoization)
- Item 38: Drag-and-drop reordering of cells
- Item 39: Text cell editor markdown split preview
- Item 40: Bulk actions on file list
- Item 43: Cell title markdown support + remove description field

## Recent Commits
- `4c2f6a8` feat: MCP test connection button and OIDC provider validation (items 34, 42)
- `fd436ec` fix: subagent spawning now launches background execution (item 30)
- `7d012d6` feat: add get_notebook_context tool with safeguards (item 31)
- `332cef7` feat: remove permission presets, widget play/edit buttons, dashboard edit mode toggle (items 4, 7, 8, 10)
- `4d5be1a` feat: configurable max tool turns for agents, default 90 (item 18)
