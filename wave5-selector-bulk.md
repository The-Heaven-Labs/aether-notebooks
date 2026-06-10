# Wave 5: Scalable Selector + Bulk Actions

**Date:** 2026-06-09
**Items:** 33, 40
**Commit:** `feat: scalable skill selector and bulk file actions (items 33, 40)`

## Item 33: Scalable Skill/MCP Selector UI

**File:** `web/src/pages/AgentsPage.tsx`

### Changes
Replaced the pill-button toggle UI for skills and MCP servers with a searchable checkbox grid + selected chips pattern.

**Before:** Flat row of toggle buttons — doesn't scale past ~10 items.

**After:**
- Search input at top filters by name/description (case-insensitive)
- Checkbox grid (`grid-template-columns: repeat(auto-fill, minmax(180px, 1fr))`) with `maxHeight: 200` and scroll
- Selected items shown as removable chips below the grid
- "No skills match" empty state when search yields nothing
- MCP servers show type badge `(stdio)` / `(http)` next to name

**Implementation details:**
- Added `skillSearch` and `mcpSearch` local state in `AgentFormFields`
- `filteredSkills` / `filteredMCPs` computed from search text
- Existing `toggleSkill` / `toggleMCPServer` callbacks unchanged
- New styles: `searchInput`, `selectorGrid`, `checkboxLabel`, `checkboxText`, `chipsRow`, `chip`, `chipRemove`

## Item 40: Bulk Actions on File List

**File:** `web/src/pages/HomePage.tsx`

### Changes

1. **Removed `selectionMode` state** — selection is now implicit (active when `selected.size > 0`)
2. **Removed "Select" button** — no longer needed
3. **Hover-visible checkboxes** — checkboxes always in DOM but hidden via CSS (`opacity: 0`), shown on hover or when checked:
   ```css
   .file-list-item .file-checkbox { opacity: 0; transition: opacity 0.15s; }
   .file-list-item:hover .file-checkbox { opacity: 1; }
   .file-list-item .file-checkbox.checked { opacity: 1; }
   ```
4. **Added "Permissions" button** to bulk toolbar — opens `BulkPermissionsModal`
5. **Added `BulkPermissionsModal` component** — lets user pick viewer/editor/admin role and applies to all selected items via `PUT /api/v1/acl/{type}/{id}`
6. **Added `file-list-item` CSS class** to all 4 item types (folders, notebooks, connectors, dashboards)
7. **Replaced `itemCheckbox` style** with `hoverCheckbox` wrapper style
8. **Escape key** clears selection when items are selected

**BulkPermissionsModal details:**
- Shows count of selected items
- Dropdown for permission level: Viewer (view), Editor (view+edit), Admin (full)
- Applies via ACL API for each selected item
- Shows error if any API call fails
- Clears selection on success

## Validation
- `npx tsc --noEmit` — clean
- `go build ./...` — clean (no Go changes)
- No remaining references to `selectionMode`
