# Collaborator Follow Feature — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Google Docs-style "follow a collaborator" feature to notebooks. Users can click a collaborator's avatar to follow their viewport (cell focus + scroll position) and automatically scroll to cells they execute. When not following anyone, remote changes never cause unwanted scrolling.

**Architecture:** Hybrid approach using Yjs awareness for real-time viewport tracking and WebSocket cell events (with added `user_email`) for execution-triggered scrolling. Both gated by a local "following" state. No backend changes needed for awareness (Hocuspocus relay handles it). Minimal backend changes: add `user_email` to existing WebSocket broadcast events.

**Tech Stack:** Yjs awareness (via Hocuspocus relay), Go API backend (WebSocket hub), React/TypeScript frontend

---

### Task 1: Add `userEmail` helper to Go Server

**Files:**
- Modify: `internal/api/ws.go` — add helper method on Server

**Step 1: Read the file**

Read `internal/api/ws.go` to see the Server struct definition (likely in another file).

**Step 2: Add `userEmail` helper**

Add a helper on `Server` that looks up a user's email from their ID:

```go
func (s *Server) userEmail(ctx context.Context, userID string) string {
    var email string
    err := s.db.Pool.QueryRow(ctx, "SELECT email FROM users WHERE id = $1", userID).Scan(&email)
    if err != nil {
        return userID // fallback: use ID
    }
    return email
}
```

(If `s.db` is not directly accessible, use `s.db.Pool`.)

**Step 3: Commit**

```bash
git add internal/api/ws.go
git commit -m "feat: add userEmail helper for broadcast identity"
```

---

### Task 2: Add `user_email` to cell_output broadcasts in execute_handlers.go

**Files:**
- Modify: `internal/api/execute_handlers.go:220,232`

**Step 1: Read the file**

Read `internal/api/execute_handlers.go` around lines 210-240 to confirm the exact broadcast calls.

**Step 2: Add user_email to error broadcast (line 220)**

Current:
```go
s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": []models.Output{errOutput}})
```

New:
```go
s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": []models.Output{errOutput}, "user_email": s.userEmail(ctx, claims.UserID)})
```

**Step 3: Add user_email to success broadcast (line 232)**

Current:
```go
s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": cellOutputs})
```

New:
```go
s.hub.Broadcast(nbID, map[string]any{"type": "cell_output", "cell_id": cellID, "outputs": cellOutputs, "user_email": s.userEmail(ctx, claims.UserID)})
```

**Step 4: Build to verify**

Run: `task build`
Expected: Successful build

**Step 5: Commit**

```bash
git add internal/api/execute_handlers.go
git commit -m "feat: add user_email to cell_output broadcasts"
```

---

### Task 3: Add `user_email` to cell_created, cell_updated, cell_metadata_changed, cell_deleted broadcasts

**Files:**
- Modify: `internal/api/cell_handlers.go:152,361,365,424`

**Step 1: Read the file**

Read `internal/api/cell_handlers.go` around lines 150-430.

**Step 2: Add user_email to cell_created broadcast**

At the broadcast call in `handleCreateCell` (around line 152), `claims` is already available. Add `"user_email": s.userEmail(ctx, claims.UserID)`.

**Step 3: Add user_email to cell_updated broadcast**

In `handleUpdateCell`, the `updateMsg` map is built incrementally (lines 330-360), then broadcast at line 361. Add `"user_email": s.userEmail(ctx, claims.UserID)` to the `updateMsg` map (after line 360, before line 361).

**Step 4: Add user_email to cell_metadata_changed broadcast**

At line 365, add `"user_email": s.userEmail(ctx, claims.UserID)` to the broadcast map.

**Step 5: Add user_email to cell_deleted broadcast**

At line 424, add `"user_email": s.userEmail(ctx, claims.UserID)` to the broadcast map.

**Step 6: Build to verify**

Run: `task build`
Expected: Successful build

**Step 7: Commit**

```bash
git add internal/api/cell_handlers.go
git commit -m "feat: add user_email to cell lifecycle broadcasts"
```

---

### Task 4: Add `user_email: "agent@hnb"` to agent broadcasts

**Files:**
- Modify: `internal/agent/tools_notebook.go:303`
- Modify: `internal/agent/tools_chart.go:102`

**Step 1: Read the files**

Read both files to confirm the broadcast call sites. The `ToolContext` has a `UserID` field but not email. Since agent actions are synthetic (not from a real user), hardcode `"agent@hnb"`.

**Step 2: Modify tools_notebook.go**

Current (line 303):
```go
ctx.BroadcastFunc(notebookID, map[string]any{
    "type":    "cell_updated",
    "cell_id": req.CellID,
    "source":  req.Source,
})
```

New:
```go
ctx.BroadcastFunc(notebookID, map[string]any{
    "type":    "cell_updated",
    "cell_id": req.CellID,
    "source":  req.Source,
    "user_email": "agent@hnb",
})
```

**Step 3: Modify tools_chart.go**

Current (line 102):
```go
ctx.BroadcastFunc(notebookID, map[string]any{
    "type":     "cell_metadata_changed",
    "cell_id":  req.CellID,
    "metadata": metadataMap,
})
```

New:
```go
ctx.BroadcastFunc(notebookID, map[string]any{
    "type":     "cell_metadata_changed",
    "cell_id":  req.CellID,
    "metadata": metadataMap,
    "user_email": "agent@hnb",
})
```

**Step 4: Build to verify**

Run: `task build`
Expected: Successful build

**Step 5: Commit**

```bash
git add internal/agent/tools_notebook.go internal/agent/tools_chart.go
git commit -m "feat: add user_email agent@hnb to agent broadcasts"
```

---

### Task 5: Update useNotebookWs hook to pass user_email through callbacks

**Files:**
- Modify: `web/src/hooks/useNotebookWs.ts`

**Step 1: Read the file**

Read `web/src/hooks/useNotebookWs.ts` (97 lines total).

**Step 2: Update callback type signatures**

Change `onCellOutput` to accept an additional `user_email?: string` parameter (e.g., change to `onCellOutput?: (cellId: string, outputs: Output[], userEmail?: string) => void`). Similarly for `onCellUpdated`, `onCellMetadataChanged`, `onCellCreated`.

Alternatively, to minimize changes, add the user_email to each callback's parameter list.

**Step 3: Pass user_email from WebSocket messages to callbacks**

In each handler, extract `msg.user_email` and pass it:
```typescript
onCellOutputRef.current(msg.cell_id, msg.outputs, msg.user_email)
onCellUpdatedRef.current(msg.cell_id, updates, msg.user_email)
// etc.
```

**Step 4: Commit**

```bash
git add web/src/hooks/useNotebookWs.ts
git commit -m "feat: pass user_email through WebSocket callbacks"
```

---

### Task 6: Gate flashCell on followed user in NotebookPage

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Read the file**

Read the relevant sections: the `flashCell` function (line 236), the WebSocket callbacks (lines 251-303), and the component state declarations.

**Step 2: Add `following` state**

```typescript
const [following, setFollowing] = useState<{ email: string; name: string } | null>(null)
```

**Step 3: Update useNotebookWs callbacks to accept user_email**

Update the callback signatures to accept an optional `userEmail: string | undefined` parameter.

**Step 4: Gate flashCell on following match**

Modify the four flashCell calls to check against the followed user:

```typescript
if (!pendingExecRef.current.has(cellId) && (!following || userEmail === following.email)) {
  flashCell(cellId)
}
```

Wait — the logic should be: **only** scroll if `userEmail === following.email`. If `following` is null (not following anyone), never scroll. If `following` is set but userEmail doesn't match, don't scroll. If `following` is set and userEmail matches, scroll.

```typescript
if (!pendingExecRef.current.has(cellId) && following && userEmail === following.email) {
  flashCell(cellId)
}
```

But we need to keep the existing behavior for agent events — they should always be followed by the agent invoker. The agent panel handles its own scrolling via `onCellScrollTo`, so we don't need to gate agent events in flashCell. Actually, we do need to handle it: if the user is following the agent, `user_email === "agent@hnb"` would match. If they're not following the agent, no scroll.

The existing AgentPanel's `onCellScrollTo` still fires independently — it handles scrolling for the user who opened the agent panel. The WebSocket `flashCell` is a separate mechanism that fires for ALL viewers. So:
- Agent invoker: scrolls via AgentPanel's `onCellScrollTo` (always) + would also get WebSocket scroll if following agent
- Non-invoker: only scrolls if following agent

This is correct behavior. The double-scroll for the invoker is harmless (already at the right cell).

**Step 5: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: gate flashCell on followed user"
```

---

### Task 7: Add focus/scroll awareness fields and listening

**Files:**
- Modify: `web/src/components/Cell.tsx` — awareness setup + export helper
- Modify: `web/src/pages/NotebookPage.tsx` — awareness listener integration

**Step 1: Add focus awareness update in Cell.tsx**

In `getOrCreateCollab`, add a new awareness field for focus tracking. Export a helper function `updateCellFocus(cellId: string | null)` that updates the awareness state.

```typescript
export function updateCellFocus(notebookId: string, cellId: string | null) {
  const collab = collabCache.get(notebookId)
  if (!collab?.provider.awareness) return
  collab.provider.awareness.setLocalStateField('focus', {
    cellId,
    scrollTop: null,
    updatedAt: Date.now(),
  })
}
```

**Step 2: Add scroll tracking**

Add a throttled scroll listener in NotebookPage that updates awareness with `scrollTop`:

```typescript
const scrollContainerRef = useRef<HTMLDivElement>(null)
useEffect(() => {
  const el = scrollContainerRef.current
  if (!el) return
  let ticking = false
  const handler = () => {
    if (!ticking) {
      requestAnimationFrame(() => {
        updateCellScroll(id, el.scrollTop)
        ticking = false
      })
      ticking = true
    }
  }
  el.addEventListener('scroll', handler, { passive: true })
  return () => el.removeEventListener('scroll', handler)
}, [id])
```

Where `updateCellScroll` is:
```typescript
export function updateCellScroll(notebookId: string, scrollTop: number | null) {
  const collab = collabCache.get(notebookId)
  if (!collab?.provider.awareness) return
  collab.provider.awareness.setLocalStateField('focus', {
    cellId: collab.provider.awareness.getLocalState()?.focus?.cellId ?? null,
    scrollTop,
    updatedAt: Date.now(),
  })
}
```

**Step 3: Add awareness change listener in NotebookPage**

Set up an awareness listener that reacts when the followed user changes focus:

```typescript
useEffect(() => {
  const collab = getOrCreateCollab(id)
  const awareness = collab.provider.awareness
  if (!awareness) return

  const handler = () => {
    if (!following) return
    const states = awareness.getStates()
    for (const [, state] of states) {
      if (state.user?.email === following.email && state.focus?.cellId) {
        const el = document.getElementById('cell-' + state.focus.cellId)
        if (el) {
          el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        }
        break
      }
    }
  }
  awareness.on('change', handler)
  return () => awareness.off('change', handler)
}, [id, following])
```

Note: Don't forget to import `getOrCreateCollab` at the top of NotebookPage.tsx (it's already imported as part of `import { Cell as NotebookCell, focusCellEditorEnd } from '../components/Cell'` — add `getOrCreateCollab` to this import).

**Step 4: Update awareness following field when follow state changes**

When the user follows/unfollows, update the awareness state:

```typescript
useEffect(() => {
  const collab = collabCache.get(id)
  if (!collab?.provider.awareness) return
  collab.provider.awareness.setLocalStateField('following', {
    email: following?.email ?? null,
  })
}, [id, following])
```

**Step 5: Add cell focus/blur handlers**

In the cell components (CodeCell, MarkdownCell, TextCell), call `updateCellFocus(notebookId, cellId)` on focus and `updateCellFocus(notebookId, null)` on blur.

**Step 6: Commit**

```bash
git add web/src/components/Cell.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: add awareness focus/scroll tracking and following listener"
```

---

### Task 8: Create CollaboratorAvatars component

**Files:**
- Create: `web/src/components/CollaboratorAvatars.tsx`

**Step 1: Create component**

The component reads awareness states from the Yjs document, renders colored avatars, and handles follow/unfollow:

```typescript
import { useEffect, useState } from 'react'
import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'

interface Collaborator {
  email: string
  name: string
  color: string
  focus: { cellId: string | null; scrollTop: number | null } | null
  following: { email: string | null } | null
}

interface CollaboratorAvatarsProps {
  provider: HocuspocusProvider
  currentUserEmail: string
  following: { email: string; name: string } | null
  onFollow: (collab: { email: string; name: string }) => void
  onUnfollow: () => void
  showAgent: boolean
  onFollowAgent: () => void
}

const MAX_VISIBLE = 4
```

**Props:**
- `provider` — HocuspocusProvider (to access awareness)
- `currentUserEmail` — to exclude self from clickable avatars
- `following` — current follow state (null if not following)
- `onFollow` — callback when user clicks an unfollowed collaborator
- `onUnfollow` — callback for unfollow
- `showAgent` — whether to show the AI avatar
- `onFollowAgent` — callback to follow/unfollow the agent

**Key behaviors:**
- Listen to awareness changes, build list of unique collaborators (exclude self)
- Sort: followed user first, then by name
- Max 4 visible, show `+N` overflow button
- Click unfollowed user → `onFollow(email, name)`
- Click followed user → `onUnfollow()`
- Press Escape → `onUnfollow()`
- Highlighted border when following
- Small dot indicator when someone is following you (scan awareness for `following.email === currentUserEmail`)

**Visual style (inline styles matching the codebase pattern):**
- Avatar: 28px circle, colored background, white initials text, 11px font
- Following: 2px solid var(--accent) border
- Followed-by-others: small dot (6px) at bottom-right of avatar
- Overflow button: `+N` label, same size/shape, neutral background
- Tooltip on hover showing name

**Step 2: Add Escape key listener for unfollow**

In the component (or in NotebookPage), listen for Escape keydown and call `onUnfollow` if `following` is not null.

**Step 3: Commit**

```bash
git add web/src/components/CollaboratorAvatars.tsx
git commit -m "feat: create CollaboratorAvatars component"
```

---

### Task 9: Reorganize toolbar with dropdown menus

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx` — toolbar JSX + styles

**Step 1: Design the dropdown components**

Create simple dropdown menus inline (or as subcomponents) for "View" and "Share":

**View dropdown** — contains:
- Parameters (toggle, shows active state)
- Schema (toggle)
- Schedules (toggle)
- Separator
- Collapse All / Show All
- Hide Code / Show Code
- Hide Outputs / Show Outputs

**Share dropdown** — contains:
- Export (.ipynb)
- Present mode
- Separator (if permissions present)
- Permissions (conditional on `can_edit`)

**Dropdown implementation pattern:**
```typescript
const [viewOpen, setViewOpen] = useState(false)
const [shareOpen, setShareOpen] = useState(false)

// Dropdown button with popover
<button onClick={() => setViewOpen(!viewOpen)} style={styles.schemaBtn}>
  View <ChevronDown size={12} />
</button>
{viewOpen && (
  <div style={styles.dropdown}>
    <button onClick={...}>Parameters ✓</button>
    <button onClick={...}>Schema</button>
    ...
  </div>
)}
```

**Dropdown styles:**
```typescript
dropdown: {
  position: 'absolute',
  top: '100%',
  right: 0,
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
  zIndex: 100,
  minWidth: 180,
  padding: '4px 0',
},
dropdownItem: {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  width: '100%',
  padding: '6px 12px',
  border: 'none',
  background: 'none',
  cursor: 'pointer',
  fontSize: 13,
  color: 'var(--text-primary)',
  textAlign: 'left',
},
dropdownItemActive: {
  background: 'var(--bg-secondary)',
  color: 'var(--accent)',
},
dropdownSeparator: {
  height: 1,
  background: 'var(--border-light)',
  margin: '4px 0',
},
```

**Step 2: Update toolbar layout**

Replace the flat list of buttons on the right with:

```tsx
<div style={styles.toolbarRight}>
  {/* View dropdown */}
  <div style={{ position: 'relative' }}>
    <button ... onClick={() => setViewOpen(!viewOpen)}>View ▾</button>
    {viewOpen && <ViewDropdownContent />}
  </div>

  {/* Run All — standalone */}
  <button type="button" style={styles.runAllBtn} onClick={runAll} disabled={runningCount > 0}>
    <ChevronsRight size={13} /> Run All
  </button>

  {/* AI — standalone */}
  <button ... onClick={() => setShowAgent(v => !v)}>
    <Bot size={13} /> AI
  </button>

  {/* Share dropdown */}
  <div style={{ position: 'relative' }}>
    <button ... onClick={() => setShareOpen(!shareOpen)}>Share ▾</button>
    {shareOpen && <ShareDropdownContent />}
  </div>
</div>
```

**Step 3: Wire dropdown menu items to their actions**

Each dropdown item calls the existing toggle/setter functions (same as current buttons).

**Step 4: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: reorganize toolbar with View and Share dropdown menus"
```

---

### Task 10: Integrate CollaboratorAvatars into toolbar

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Import and place the component**

In the toolbar left side, after the ConnectorSelector and running badge:

```tsx
<div style={styles.toolbarLeft}>
  <ConnectorSelector ... />
  {runningCount > 0 && <RunningBadge ... />}
  <CollaboratorAvatars
    provider={collab?.provider}
    currentUserEmail={userEmail}
    following={following}
    onFollow={(collab) => setFollowing({ email: collab.email, name: collab.name })}
    onUnfollow={() => setFollowing(null)}
    showAgent={showAgent}
    onFollowAgent={() => {
      if (following?.email === 'agent@hnb') {
        setFollowing(null)
      } else {
        setFollowing({ email: 'agent@hnb', name: 'AI Agent' })
      }
    }}
  />
</div>
```

**Step 2: Get userEmail**

Read from localStorage (same as Cell.tsx):
```typescript
const userEmail = localStorage.getItem('hnb_user_email') ?? ''
```

**Step 3: Get the collab provider**

The `collabCache` is in Cell.tsx. Import `getOrCreateCollab` and call it:
```typescript
const collab = useMemo(() => id ? getOrCreateCollab(id) : null, [id])
```

Wait — but `getOrCreateCollab` returns a `NotebookCollab` with `doc`, `provider`, `refCount`, `synced`. If we call it here, it increments `refCount`. But `getOrCreateCollab` is already called inside each `CodeCell` component when the cell mounts. Calling it in NotebookPage would add an extra reference that doesn't get released.

Better approach: export the `collabCache` from Cell.tsx so NotebookPage can access it:

```typescript
export { collabCache }
```

Then in NotebookPage:
```typescript
const collab = useMemo(() => collabCache.get(id ?? ''), [id])
```

**Step 4: Handle keyboard shortcut (Escape to unfollow)**

Add to `useNotebookKeyboardShortcuts` or add a direct keydown listener:

```typescript
useEffect(() => {
  const handler = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && following) {
      setFollowing(null)
    }
  }
  window.addEventListener('keydown', handler)
  return () => window.removeEventListener('keydown', handler)
}, [following])
```

**Step 5: Update toolbarLeft styles to accommodate avatars**

No major changes needed — just adjust `gap` if needed or add a vertical divider between the connector/running-badge group and the avatars.

**Step 6: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: integrate CollaboratorAvatars into notebook toolbar"
```

---

### Task 11: Add cell focus tracking in cell components

**Files:**
- Modify: `web/src/components/Cell.tsx` — add focus tracking in CodeEditorView
- Modify: `web/src/components/MarkdownCell.tsx` — add focus tracking

**Step 1: Track CodeEditorView focus**

In the CodeEditorView component (inside Cell.tsx), add focus/blur handlers to the EditorView:

```typescript
view.dispatch({
  effects: compartment.reconfigure([
    EditorView.updateListener.of((update) => {
      if (update.focusChanged) {
        updateCellFocus(notebookId, update.view.hasFocus ? cellId : null)
      }
    }),
    // ... existing extensions
  ]),
})
```

Wait — the compartment is reconfigured multiple times. Better to add the updateListener directly when creating the EditorView. Let me check how the EditorView is created in Cell.tsx.

Actually, looking at Cell.tsx, the CodeEditorView is created in a `useEffect` or `useMemo`. The `yCollab`, `ySyncFacet`, and other extensions are configured via a `Compartment`.

The cleanest approach: add the updateListener alongside existing extensions. Find where the EditorView is created and add:

```typescript
EditorView.updateListener.of((update: ViewUpdate) => {
  if (update.focusChanged) {
    updateCellFocus(notebookId, update.view.hasFocus ? cellId : null)
  }
})
```

**Step 2: Track MarkdownCell focus**

In `MarkdownCell.tsx`, find where the textarea/input gets focus/blur and call `updateCellFocus`:

```typescript
const handleFocus = () => updateCellFocus(notebookId, cellId)
const handleBlur = () => updateCellFocus(notebookId, null)
```

**Step 3: Commit**

```bash
git add web/src/components/Cell.tsx web/src/components/MarkdownCell.tsx
git commit -m "feat: track cell focus in awareness via focus/blur handlers"
```

---

### Task 12: Add scroll-aware container ref

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Find the scrollable cell area**

Find the container that wraps the cell list (the scrollable div). It's likely the `main` or a specific div with overflow-y: auto.

**Step 2: Add ref and scroll listener**

Add a ref to the scroll container and a throttled scroll listener that updates awareness:

```typescript
const cellsContainerRef = useRef<HTMLDivElement>(null)

useEffect(() => {
  const el = cellsContainerRef.current
  if (!el || !id) return
  let ticking = false
  const handler = () => {
    if (!ticking) {
      requestAnimationFrame(() => {
        updateCellScroll(id, el.scrollTop)
        ticking = false
      })
      ticking = true
    }
  }
  el.addEventListener('scroll', handler, { passive: true })
  return () => el.removeEventListener('scroll', handler)
}, [id])
```

**Step 3: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: add scroll tracking awareness via throttled scroll listener"
```

---

### Task 13: Handle agent panel scroll-to with follow gating

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx` — agent scroll-to logic
- Modify: `web/src/components/AgentPanel.tsx` — ensure agent scroll uses correct logic

**Step 1: Check agent scroll behavior**

The `AgentPanel` calls `onCellScrollTo(cellId)` for `cell_created`, `cell_output`, `cell_updated`. In NotebookPage, the `onCellScrollTo` callback currently always scrolls. Since the user confirmed "agent actions should always be followed" (for the invoker), we should keep this behavior as-is for the agent invoker.

**Step 2: Ensure agent events don't double-trigger**

When the agent broadcasts a `cell_updated` (with `user_email: "agent@hnb"`), the WebSocket handler in NotebookPage also receives it. If the user is following the agent, `flashCell` would also fire. This is fine — the cell is already in view from `onCellScrollTo`, so the second scroll is a no-op.

No changes needed.

**Step 3: Commit (if any changes made)**

```bash
git commit -m "feat: verify agent scroll behavior with follow gating"
```

---

### Task 14: Verify with `task build` and `task test`

**Step 1: Build the Go backend**

Run: `task build`
Expected: Both Go binaries compile successfully under `./bin/`

**Step 2: Build the web frontend**

Run: `task build:web`
Expected: Vite builds successfully, output in `web/dist/`

**Step 3: Run Go tests**

Run: `task test`
Expected: All tests pass (check for any test failures related to WebSocket message formats — the new `user_email` field shouldn't break tests since they don't typically assert on message payload fields)

**Step 4: Commit any fixes**

```bash
git commit -m "fix: address build/test issues after follow feature"
```
