# All Improvements Design Document

**Date:** 2026-06-08
**Status:** Designed (pending implementation)
**Total items:** 43 across 9 groups

---

## Overview

This document contains designs for all 43 improvement items from `IMPROVEMENTS.md`, grouped into 9 thematic groups. Each design was validated through collaborative brainstorming with the product owner.

---

## Group 1: UI/UX Polish & Dark Mode

### Item 2: Keyboard shortcuts modal dark mode

**File:** `web/src/components/ShortcutsModal.tsx`

Replace hardcoded light theme colors in the `kbd` element with theme-aware CSS variables:

```tsx
// Before:
kbd: { fontFamily: 'var(--font-mono)', fontSize: 11, background: '#f5f5f5', border: '1px solid #e8e8e8', borderRadius: 3, padding: '2px 6px' },

// After:
kbd: { fontFamily: 'var(--font-mono)', fontSize: 11, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 3, padding: '2px 6px' },
```

---

### Item 3: Folder tree collapse button visibility and behavior

**File:** `web/src/components/TwoPanelLayout.tsx`

**Issues:**
1. Button position hardcoded at `left: 240` — floats away after panel collapses
2. Chevron always points left — doesn't indicate collapsed state

**Design:**
- Dynamic `left` position: `collapsed ? 8 : 240`
- Chevron icon toggles: `isCollapsed ? ChevronRight : ChevronLeft`
- Tooltip: `isCollapsed ? "Expand folder tree" : "Collapse folder tree"`
- Folder tree panel: `width: isCollapsed ? 0 : 240` with `overflow: hidden` transition

---

### Item 6: Custom confirmation dialogs

**File:** `web/src/components/ConfirmDialog.tsx` (new)

Replace all `window.confirm()` calls with a themed modal dialog component.

**Component interface:**
```tsx
interface ConfirmDialogProps {
  open: boolean
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean  // red confirm button
  onConfirm: () => void
  onCancel: () => void
}
```

**Usage pattern:**
```tsx
const [deleteConfirm, setDeleteConfirm] = useState<{id: string} | null>(null)

<button onClick={() => setDeleteConfirm({ id: item.id })}>Delete</button>
<ConfirmDialog
  open={deleteConfirm !== null}
  title="Delete item?"
  message="This cannot be undone."
  destructive
  onConfirm={() => handleDelete(deleteConfirm!.id)}
  onCancel={() => setDeleteConfirm(null)}
/>
```

**Migration:** Replace each `window.confirm()` with the new component over time. 14 locations identified across the codebase.

---

### Item 10: Widget play button with loading state

**File:** `web/src/pages/DashboardPage.tsx`

**Changes:**
1. Each `WidgetCard` gets its own `isLoading` state
2. Play button (▶/↻) added to widget card header, top-right
3. Button shows `<Loader />` when loading, `<Play />` when idle
4. Clicking triggers `POST /api/v1/notebooks/:notebookId/cells/:cellId/execute`
5. Also fix hardcoded `color: '#fff'` on "Run all" button → use `var(--text-inverse)`

---

### Item 37: Typing slow with many rows displayed

**Files:** `web/src/pages/NotebookPage.tsx`, `web/src/components/Cell.tsx`

**Root cause:** No memoization — when ANY cell's state changes, all cells re-render, causing CodeMirror reconciliation lag.

**Design:**
- Wrap `Cell` component in `React.memo` to prevent unnecessary re-renders
- Use `useMemo` on the cells list rendering
- Add `useCallback` for cell event handlers

```tsx
import { memo } from 'react'

const MemoizedCell = memo(Cell)

{localCells.map((cell, i) => (
  <MemoizedCell
    key={cell.id}
    cell={cell}
    notebookId={id}
    connectors={connectors}
    index={i}
    // ... all props
  />
))}
```

---

### Item 38: Drag-and-drop reordering of cells

**Files:** `web/src/pages/NotebookPage.tsx`

**Design:**
- Install `@dnd-kit/core` and `@dnd-kit/sortable`
- Wrap cells area with `DndContext` and `SortableContext`
- Each cell gets `useSortable` hook with `id={cell.id}`
- Drag handle: `GripVertical` icon on left edge, visible on hover
- On drag end: call existing `moveCell` API via `PUT /api/v1/notebooks/:id/cells/:id/move`

**Visual:**
```
[ ⋮⋮ ] 1 SQL [connector] [Run ▶] [MD] [↑][↓] [👁] [>] [🕐]
```

---

### Item 39: Text cell editor doesn't show markdown preview

**File:** `web/src/components/MarkdownCell.tsx`

**Design:**
- Add `splitMode: boolean` state (default `false`)
- When `splitMode=true`: show textarea AND preview side-by-side (flex row, ~50% each)
- Toolbar gets "Split" toggle button
- In split mode, preview updates on every keystroke

**Layout in split mode:**
```
┌─────────────────────────────────────────────────────────┐
│ [Image] [B] [I] [H] [Code] [Link] [Split ⬚]             │
├────────────────────────┬────────────────────────────────┤
│ textarea (50%)         │ rendered markdown (50%)        │
└────────────────────────┴────────────────────────────────┘
```

---

## Group 2: Dashboard & Widgets

### Item 4: Dashboard edit mode and permissions

**Files:** `web/src/pages/DashboardPage.tsx`, `web/src/App.tsx`

**Design:**
1. Single route `/dashboards/:id` — no more `/view` suffix
2. State: `editMode: boolean` (default based on permissions)
3. URL includes `?edit=true` query param for shareable edit links
4. Header shows:
   - View mode: "Edit" button (if user has editor/admin role)
   - Edit mode: "Done" button + existing editor controls
5. In edit mode: show drag handles, delete buttons, add widget
6. In view mode: hide drag handles/delete, show play buttons

**Permission check:**
- Use existing auth context to get user role
- If role is `viewer` → `editMode = false` always, no Edit button
- If role is `editor` or `admin` → `editMode = false` by default

**Also:** Add edit button (pencil icon) on each widget in edit mode to open the picker pre-filled with current values.

---

### Item 5: "Run all" button (Already implemented)

The "Run all" button already exists in `DashboardPage.tsx` (line 374).

---

### Item 8: Edit already placed widgets

**File:** `web/src/pages/DashboardEditorPage.tsx`

Each widget card gets a pencil/edit icon next to the X delete button. Clicking opens the picker modal pre-filled with:
- Current notebook
- Current cell
- Current type (table/chart)

---

### Item 10: Per-widget play button (See Group 1, Item 10)

Already covered above.

---

## Group 3: Agent & Skills System

### Item 13: Cell execution metrics

**Database:** New table `cell_execution_logs`:
```sql
CREATE TABLE cell_execution_logs (
  id UUID PRIMARY KEY,
  cell_id UUID NOT NULL REFERENCES cells(id),
  connector_id UUID,
  connect_time_ms INT,
  query_time_ms INT,
  render_time_ms INT,
  queue_time_ms INT,
  total_time_ms INT,
  executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)
```

**Backend changes (`internal/api/execute_handlers.go`):**
- Wrap execution with timing: start → connect → execute → render → end
- Store `ExecutionLog` record after each run
- Include metrics in agent's `run_cell` tool response

**Frontend (`Cell.tsx`):**
- In meta bar near run button: show `⏱ 1.2s`
- On hover: tooltip with breakdown:
  ```
  Total: 1.2s
  Queue: 45ms
  Connect: 89ms
  Query: 1.0s
  Render: 66ms
  ```

---

### Item 16: Skills not usable — agent can't discover available skills

**Design:**
1. Add `list_skills` tool:
   ```go
   // Tool: list_skills → returns all org skills
   { id, name, description, capabilities }
   ```

2. Enhance skill `system_prompt` format for agent readability:
   ```markdown
   # Skill: data-analyst
   ## Description: Analyzes datasets, generates insights
   ## Capabilities:
   - run_sql(query, connector_id?) - Execute SQL
   - read_notebook(id) - Read notebook cells
   ## When to use: Ask me to analyze data, explore tables
   ```

3. Auto-inject skills in system prompt (already partially working) — ensure agent knows what each skill can do.

---

### Item 17: Trigger skills via `/skill:<name>`

**File:** `internal/agent/engine.go`

**Normalization:**
```go
func normalizeSkillName(name string) string {
    return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
}
```

**Message processing:**
- User sends: `/skill:data-analyst analyze sales by region`
- Engine detects `/skill:` prefix, fetches skill's `system_prompt`
- Injects skill prompt as additional system message for this turn ONLY
- Next message reverts to base agent prompt

**Error handling:**
- If skill not found → return error: "skill 'xyz' not found"
- If multiple skills match → pick best match or return disambiguation

---

### Item 18: Maximum tool turns configurable, default 90

**Files:** `internal/models/agent.go`, `internal/api/agent_handlers.go`, `internal/agent/engine.go`

**Changes:**
1. Add `max_turns *int` field to `Agent` model
2. Update `handleCreateSession` to fetch agent's `max_turns` or fall back to 90
3. Fix `engine.go` — remove hardcoded `const maxTurns = 15`, use session value

```go
// engine.go - before:
const maxTurns = 15

// engine.go - after:
maxTurns := session.MaxTurns
if maxTurns == 0 { maxTurns = 90 }
```

**Frontend (`AgentsPage.tsx`):**
- Add `max_turns` input field when creating/editing agents
- Default value: 90

---

### Item 27: Trigger agent modal from outside notebooks

**File:** `web/src/components/AgentPanel.tsx`

**Design:**
1. Global keyboard shortcut: `Ctrl+K` → opens global agent modal
2. "AI Assistant" button in app header as alternative trigger
3. Opens `AgentPanel` in floating modal overlay (centered, backdrop)
4. Session created without `notebookId` (global context)
5. `Escape` or clicking backdrop closes

**Contextual launch:**
- From folder page: pass folder context
- From search: pass search context

---

### Item 30: Subagent spawning not working

**Files:** `internal/agent/subagent.go`, `internal/agent/tools_agent.go`

**Root cause:** `spawn_subagents` tool handler creates task records but never calls `SpawnSubagents()`.

**Fix:**
1. Refactor `SpawnSubagents` to accept dependencies (pool, LLM, masterKey) directly
2. Tool handler launches goroutine running `SpawnSubagents`
3. Progress emitted via `tasks_updated` events through WebSocket

**Flow after fix:**
```
Agent calls spawn_subagents tool
  → Handler creates DB records (status: 'queued')
  → Handler launches goroutine running SpawnSubagents
  → Handler returns immediately with task_ids
  → Subagents run in parallel, emitting tasks_updated via WebSocket
  → Frontend displays progress
```

---

### Item 31: Tool to give full notebook content to agent

**Files:** `internal/agent/tools_notebook.go`

**New tool:** `get_notebook_context`
- Input: `{ notebook_id: string, max_cells?: number, include_outputs?: boolean }`
- Returns: Full notebook structure as formatted text

**Auto-inject summary** (when session starts with notebook):
```
Notebook: "Sales Analysis Q1"
Cells: 12 total (SQL: 8, MD: 4)
Structure:
- Cell 1: SQL (users table, limit 100)
...
(4 cells omitted - call get_notebook_context for full content)
```

**Safeguards:**
- `max_cells`: limit to first 50 (default)
- `include_outputs`: truncate to first 10 rows if true
- If >50 cells: summarize with representative sample
- If total size >100KB: truncate to first 20 cells

---

### Item 34: MCP config test button

**Files:** `internal/api/mcp_server_handlers.go`, `web/src/pages/MCPPage.tsx`

**Design:**
1. New `POST /api/v1/mcp-servers/{id}/test` endpoint:
   - For HTTP MCPs: call `GET {command}/tools/list` with auth headers
   - For OAuth: use logged-in user's token
   - Returns: `{ success: bool, error?: string, tools?: string[] }`

2. Add OAuth support to `MCPServerOrg` model:
   ```go
   AuthType   string   `json:"auth_type"`   // "none", "oauth", "api_key"
   AuthConfig JSONMap  `json:"auth_config"` // encrypted config
   ```

3. Frontend: "Test Connection" button next to Edit/Delete
   - Spinner while testing
   - Success: green checkmark + "Connected! Found N tools"
   - Failure: red error message

---

## Group 4: Permissions & Access Control

### Item 7: Remove predefined permission profiles

**File:** `web/src/components/PermissionsPanel.tsx`

**Changes:**
1. Remove `PRESETS` object (lines 16-21)
2. Remove `applyPreset` function (lines 217-229)
3. Remove preset buttons (none/viewer/editor/admin) from ACL entry row
4. Keep individual action checkboxes for manual permission management

---

### Item 11: Dashboard permission system

**Files:** `internal/api/dashboard_handlers.go`, `internal/api/router.go`, `web/src/pages/DashboardEditorPage.tsx`

**Changes:**
1. Add ACL permission checks to dashboard handlers:
   - `handleGetDashboard` → `requirePermission("dashboard", id, "view")`
   - `handlePutDashboard` (update) → `requirePermission("dashboard", id, "edit")`
   - `handleListDashboards` → filter by ACL

2. Folder inheritance: if no explicit dashboard ACL, fall back to parent folder permissions

3. Add PermissionsPanel to dashboard editor:
   - "Permissions" button in header → opens modal
   - Same component as notebooks/connectors

4. Permission actions for dashboards: `view`, `edit`, `delete`, `share`

---

### Item 22: Personal access tokens

**Database:**
```sql
CREATE TABLE api_tokens (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  org_id UUID NOT NULL,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL,  -- bcrypt hash
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,     -- NULL = never
  created_at TIMESTAMPTZ NOT NULL
)
```

**Token format:** `hnb_tok_{random_32_chars}`

**API handlers:**
- `POST /api/v1/tokens` — create (returns raw token ONCE)
- `GET /api/v1/tokens` — list user's tokens (metadata only)
- `DELETE /api/v1/tokens/{id}` — revoke

**Auth middleware:** `Authorization: Bearer hnb_tok_xxx` → validate hash, set user context

**UI:** `web/src/pages/ProfilePage.tsx` — "Personal Access Tokens" section:
- "Create token" button → modal with name input
- Shows token list: name, created date, last used, revoke
- Warning: "Copy the token now — you won't see it again"

---

### Item 33: Scalable skill/MCP selector UI

**File:** `web/src/pages/AgentsPage.tsx`

**Design:**
```
┌─────────────────────────────────────────────────────────┐
│ 🔍 Search skills and agents...                        │
├─────────────────────────────────────────────────────────┤
│ ☐ data-analyst        ☐ code-reviewer      ☐ sql-expert│
│ ☑ sql-helper           ☐ markdown-writer    ☐ api-designer│
│ ... (virtualized - only visible items rendered)         │
├─────────────────────────────────────────────────────────┤
│ Selected: [sql-helper ×] [data-analyst ×]               │
└─────────────────────────────────────────────────────────┘
```

**Implementation:**
- Plain `<input>` with search text
- `@tanstack/react-virtual` for checkbox list virtualization
- Selected items as removable chips below input
- "Select all visible" / "Clear all" buttons

---

## Group 5: Audit & File Management

### Item 9: Audit page cell execution logging

**Files:** `internal/api/execute_handlers.go`, `web/src/pages/AuditPage.tsx`

**Backend changes:**
- When logging `cell.execute`, include full metadata:
  ```go
  metadata: {
    "notebook_id": "xxx",
    "cell_id": "xxx",
    "connector_id": "xxx",
    "connector_name": "postgres",
    "query": "SELECT * FROM users LIMIT 100",  // full query stored
    "row_count": 45,
    "duration_ms": 234,
  }
  ```

**Frontend:**
- When displaying `cell.execute`, show:
  - Notebook name (linked)
  - Cell position
  - Connector used
  - Query preview (first 200 chars, expandable)
  - Row count / duration

---

### Item 24: Custom confirmation dialogs

Already covered in Item 6 (Group 1).

---

### Item 28: Member/Group sidebar icon collision

**File:** `web/src/components/Sidebar.tsx`

**Change:**
- Members: `UserCircle` (single user silhouette)
- Groups: `Users` (three people silhouette)

Simple swap — resolves visual collision.

---

### Item 29: Audit page filter improvements

**File:** `web/src/pages/AuditPage.tsx`

**Changes:**
1. Add searchable user dropdown (virtualized, scalable for thousands of users)
2. Add date range filter (`from` and `to` date inputs)
3. Fix action filter behavior when "All types" selected

**Updated filter bar:**
```
[🔍 Search user... ▼] [Resource Type ▼] [Action ___________] [From __] [To __]
```

**URL sync:** All filters sync to URL query params for shareable links.

---

### Item 32: Import/export notebooks with .ipynb support

**Files:** `internal/api/notebook_handlers.go`, `web/src/pages/HomePage.tsx`, `web/src/pages/NotebookPage.tsx`

**Export (`GET /api/v1/notebooks/:id/export`):**
- Notebook cells → Jupyter cell format
- Code cell → `code` cell with `source`, `execution_count`, `outputs`
- Markdown cell → `markdown` cell with `source`
- Outputs stored as JSON (exact round-trip not guaranteed)
- Downloads as `{notebook-title}.ipynb`

**Import (`POST /api/v1/notebooks/import`):**
- Accepts `.ipynb` file upload (multipart/form-data)
- Parse JSON, validate structure
- Convert jupyter cells → hnb cells
- Options:
  - Create new notebook (default)
  - Import into existing notebook (append OR replace)

---

### Item 35: Multi-cell selection keyboard navigation issue

**File:** `web/src/pages/NotebookPage.tsx`

**Fix:** Enforce single-cell selection only.
- When `setFocusedCellId` is called, replace (not append to) current selection
- Ignore Shift/Ctrl modifiers on cell click
- Arrow keys work on single `focusedCellId` (already works)

---

### Item 36: New cell created near bottom visually cut off

**File:** `web/src/pages/NotebookPage.tsx`

**Fix:**
1. After `createCell.mutate` succeeds, scroll new cell into view:
   ```tsx
   onSuccess: (data) => {
     setTimeout(() => {
       const el = document.getElementById('cell-' + data.id)
       if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' })
     }, 50)
   }
   ```

2. Ensure bottom padding on cells area — buttons never flush with viewport bottom.

---

### Item 40: Bulk actions on file list

**File:** `web/src/pages/HomePage.tsx`

**Changes:**
1. Hide checkboxes by default — visible on hover (Google Drive style)
2. On hover: show checkbox on left side of each item
3. Click checkbox: toggle selection
4. Click item: normal navigation
5. Bulk toolbar appears automatically when `selected.size > 0`

**Remove:**
- "Select" button and `selectionMode` state

**Keep:**
- `selected` Set, `selectAll()`/`clearSelection()`, `bulkDelete`/`bulkMove` mutations

**Add:**
- "Permissions" button in bulk toolbar → opens PermissionsPanel for selected items

---

## Group 6: Markdown & Rich Content

### Item 19: Full-screen image viewer with zoom

**File:** `web/src/components/MarkdownCell.tsx`

**Design:**
1. Center overlay button on each image: "⛶" expand icon
2. Click → opens full-screen viewer modal

**Viewer features:**
- Dark backdrop
- Centered image with zoom controls
- **ESC key** closes
- Click backdrop closes
- Close button (X) in corner

**Zoom controls:**
- +/- buttons, scroll wheel, pinch
- Zoom range: 25% to 400%
- Current zoom percentage display
- Fit-to-screen button
- Reset button (to 100%)

---

### Item 25: Collapse/Show all buttons

**File:** `web/src/pages/NotebookPage.tsx`

**Design:**
- Single toggle: "Collapse all" / "Show all"
- "Collapse all" → sets `source_visible: false` AND `outputs_collapsed: true` for all cells
- "Show all" → sets `source_visible: true` AND `outputs_collapsed: false` for all cells

**UI in toolbar:**
```
[ Collapse all ]   or   [ Show all ]
```

**Individual cell expand/collapse still works independently.**

---

### Item 43: Cell title markdown support + remove description field

**Files:** `web/src/components/Cell.tsx`, database migration

**Changes:**

1. **Cell title renders markdown:**
   - When displaying cell title (not editing), wrap in `ReactMarkdown`
   - When editing, keep plain `<input>` (can't edit markdown source)
   - Use `rehypeRaw` plugin for inline HTML

2. **Remove `description` field entirely:**
   - Database: remove `description` column from `cells` table
   - Backend: remove from all cell structs, handlers, queries
   - Frontend: remove from `Cell` type and `onUpdateCellMeta` callback

---

## Group 7: Admin & Configuration

### Item 20: Image fullscreen and zoom

Already covered in Item 19 (Group 6).

---

### Item 23: Admin MOTD configuration

**Database:**
```sql
CREATE TABLE motd_messages (
  id UUID PRIMARY KEY,
  org_id UUID NOT NULL,
  title TEXT,
  content TEXT NOT NULL,       -- markdown
  priority INT DEFAULT 0,      -- higher = shown first
  visibility TEXT NOT NULL,    -- 'all' | 'specific'
  pages TEXT[],                -- if specific, which pages
  show_on_login BOOLEAN DEFAULT false,
  created_by UUID,
  created_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ        -- NULL = never
)
```

**API endpoints:**
- `GET /api/v1/motd` — returns active MOTDs for current user/org
- `POST /api/v1/admin/motd` — create MOTD
- `PUT /api/v1/admin/motd/:id` — update MOTD
- `DELETE /api/v1/admin/motd/:id` — delete MOTD

**Admin UI (`AdminPage`):**
- "MOTD" section in admin settings
- List of MOTDs with edit/delete
- "Add MOTD" form: title, content (markdown), priority, visibility, show_on_login, expiration

**Frontend banner:**
- Fetch active MOTDs on page load
- Render as dismissable banner at top of AppShell
- Dismiss (X) → stored in localStorage per MOTD (expires after 24h)
- Multiple MOTDs stacked by priority

---

### Item 42: OIDC provider form test/validate button

**Files:** `internal/api/org_handlers.go`, `web/src/pages/OrgSettingsPage.tsx`

**New API endpoint:** `POST /api/v1/sso/providers/test`
- Input: `{ discovery_url, client_id, client_secret }`
- Validates discovery URL, attempts to fetch OIDC config
- Returns: `{ success: bool, error?: string, provider_info?: {...} }`

**Frontend:**
- "Test Connection" button before Save/Add button
- Spinner while testing
- Success: green message "✓ Connected! Provider: {name}"
- Failure: red error message

---

## Group 8: Documentation

### Item 21: OpenAPI documentation with swagger

**Implementation:**
1. Add `swaggo/swag` to `tools.go`
2. Annotate all public handlers (`/api/v1/*`) with swagger docs:
   ```go
   // @Summary List notebooks
   // @Description Returns all notebooks for the authenticated org
   // @Tags notebooks
   // @Accept json
   // @Produce json
   // @Security BearerAuth
   // @Success 200 {array} Notebook
   // @Failure 401 {object} Error
   // @Router /api/v1/notebooks [get]
   ```
3. Generate spec: `swag init -g cmd/hnb-server/main.go -o internal/api/docs`
4. Add to build: `swag init ...` in Makefile/Taskfile
5. Serve at `GET /docs` (Swagger UI) and `GET /swagger.json`

---

## Group 9: Bugs

### Item 1: Invite link not working — toast "Not Found"

**Files:** `web/src/pages/MembersPage.tsx`, `web/src/App.tsx`, `internal/api/org_handlers.go`

**Two bugs found:**

1. **Wrong URL in frontend** (MembersPage.tsx line 71):
   ```tsx
   // Before (wrong):
   api.post('/api/v1/organizations/invite-link', { role: linkRole })
   
   // After (correct):
   api.post('/api/v1/members/invite-link', { role: linkRole })
   ```

2. **No `/join` route in frontend:**
   - Add route: `GET /join` → `JoinPage` component
   - `JoinPage` reads `?token=` from URL
   - Calls `POST /api/v1/auth/org/join` with `{ invite_link_token: token }`
   - On success: redirect to `/` (logged in)
   - On error: show error message

3. **Backend returns relative URL:**
   ```go
   // Line 356 in org_handlers.go - change:
   "url": fmt.Sprintf("/join?token=%s", token),
   // to:
   "url": fmt.Sprintf("%s/join?token=%s", s.frontendURL, token),
   ```

---

### Item 26: Cell selection causes multi-output keyboard issue

Already covered in Item 35 (Group 5) — same design, single cell selection enforcement.

---

## Implementation Order Recommendation

### Phase 1: Quick Wins (Low effort, high impact)
- Item 2: Keyboard shortcuts modal dark mode (1 line fix)
- Item 3: Folder tree collapse button (CSS + positioning)
- Item 28: Icon swap (1 line each)
- Item 41: Already done

### Phase 2: Critical Bugs
- Item 1: Invite link fix (2 bugs, affects user onboarding)
- Item 35/26: Multi-cell selection fix

### Phase 3: High-Value Features
- Item 6: Custom confirmation dialogs (reusable component)
- Item 22: Personal access tokens (security feature)
- Item 33: Scalable skill selector (enables growth)
- Item 21: OpenAPI docs (developer experience)

### Phase 4: Core Features
- Item 4: Dashboard edit mode unification
- Item 13: Cell execution metrics
- Item 30: Subagent spawning fix
- Item 32: Import/Export

### Phase 5: Polish
- Items 37, 38, 39: Editor improvements
- Item 25: Collapse all
- Item 23: MOTD system

---

## Files to Create/Modify Summary

### New Files
- `web/src/components/ConfirmDialog.tsx`
- `web/src/components/ImageViewer.tsx` (full-screen viewer)
- `web/src/components/SearchableMultiSelect.tsx`
- `web/src/pages/JoinPage.tsx`
- `internal/api/docs/` (swagger output)
- `docs/plans/2026-06-08-all-improvements-design.md` (this file)

### Files to Modify
- `web/src/components/ShortcutsModal.tsx` (Item 2)
- `web/src/components/TwoPanelLayout.tsx` (Item 3)
- `web/src/pages/DashboardPage.tsx` (Items 4, 10)
- `web/src/pages/DashboardEditorPage.tsx` (Items 4, 8)
- `web/src/App.tsx` (Items 4, 1)
- `web/src/pages/NotebookPage.tsx` (Items 25, 35, 36, 37, 38, 39)
- `web/src/components/Cell.tsx` (Items 37, 38, 43)
- `web/src/components/MarkdownCell.tsx` (Items 19, 39)
- `web/src/pages/MembersPage.tsx` (Item 1)
- `web/src/pages/HomePage.tsx` (Item 40)
- `web/src/pages/AuditPage.tsx` (Item 9, 29)
- `web/src/components/Sidebar.tsx` (Item 28)
- `web/src/pages/AgentsPage.tsx` (Item 33)
- `web/src/pages/ProfilePage.tsx` (Item 22)
- `web/src/pages/OrgSettingsPage.tsx` (Item 42)
- `web/src/pages/AdminPage.tsx` (Item 23)
- `internal/agent/engine.go` (Items 17, 18, 30)
- `internal/agent/subagent.go` (Item 30)
- `internal/agent/tools_agent.go` (Items 16, 30)
- `internal/agent/tools_notebook.go` (Item 31)
- `internal/api/org_handlers.go` (Items 1, 42)
- `internal/api/mcp_server_handlers.go` (Item 34)
- `internal/api/router.go` (Item 11)
- `internal/api/dashboard_handlers.go` (Item 11)
- `internal/api/execute_handlers.go` (Item 9, 13)
- `internal/models/agent.go` (Item 18)
- `internal/audit/audit.go` (Item 9)
- Database migrations for: `cell_execution_logs`, `api_tokens`, `motd_messages`, remove `cells.description`