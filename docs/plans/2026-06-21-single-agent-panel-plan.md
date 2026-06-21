# Single Agent Panel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the notebook-scoped AgentPanel mount with the global one, moving cell callbacks into AgentPanel internally.

**Architecture:** A single `AgentPanel` renders from `AppShell` only. `AgentPanel` uses `useQueryClient` from `@tanstack/react-query` to handle `cell_output`/`cell_created` WS messages when `notebookId` is set, plus an internal `scrollToCell` helper. `NotebookPage` removes its AgentPanel mount, AI button, and associated state.

**Tech Stack:** React, TypeScript, @tanstack/react-query, WebSocket

---

### Task 1: Add cell callbacks to AgentPanel

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`

**Step 1: Import useQueryClient**

Add to the existing `@tanstack/react-query` import at the top of AgentPanel.tsx:

```tsx
import { useQueryClient } from '@tanstack/react-query'
```

**Step 2: Add scrollToCell helper inside the component**

Add this helper function inside the `AgentPanel` component body (after the refs, before the `useEffect` blocks):

```tsx
const scrollToCell = useCallback((cellId: string) => {
  let attempts = 0
  const interval = setInterval(() => {
    const el = document.getElementById('cell-' + cellId)
    if (el) {
      clearInterval(interval)
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('cell-flash')
      setTimeout(() => el.classList.remove('cell-flash'), 3000)
    } else if (++attempts >= 50) {
      clearInterval(interval)
    }
  }, 100)
}, [])
```

**Step 3: Add queryClient ref**

Inside the component, after the `pageContext` sends the page context to the backend:

```tsx
const queryClient = useQueryClient()
```

**Step 4: Replace external callbacks with internal handling**

In `connectWebSocket`, the `onmessage` handler has these cases. Replace the callback calls:

For `cell_created` (currently lines 309-312):
```tsx
case 'cell_created':
  onCellCreated?.(msg.cell_id, msg.position)
  onCellScrollTo?.(msg.cell_id)
  break
```
Replace with:
```tsx
case 'cell_created':
  if (notebookId) {
    queryClient.invalidateQueries({ queryKey: ['notebook', notebookId] })
  }
  scrollToCell(msg.cell_id)
  break
```

For `cell_output` (currently lines 313-316):
```tsx
case 'cell_output':
  onCellOutput?.(msg.cell_id, msg.outputs)
  onCellScrollTo?.(msg.cell_id)
  break
```
Replace with:
```tsx
case 'cell_output':
  if (notebookId) {
    queryClient.setQueryData(['notebook', notebookId], (old: any) => {
      if (!old) return old
      return {
        ...old,
        cells: old.cells.map((c: any) =>
          c.id === msg.cell_id ? { ...c, outputs: msg.outputs as any[] } : c
        )
      }
    })
  }
  scrollToCell(msg.cell_id)
  break
```

For `cell_updated` (currently lines 317-319):
```tsx
case 'cell_updated':
  onCellScrollTo?.(msg.cell_id)
  break
```
Replace with:
```tsx
case 'cell_updated':
  scrollToCell(msg.cell_id)
  break
```

**Step 5: Clean up unused props**

Replace the original `onCellCreated`, `onCellOutput`, `onCellScrollTo` props with the single internal handler. These are no longer needed from parents:

In `AgentPanelProps` interface, remove:
- `onCellCreated?: (cellId: string, position: number) => void`
- `onCellOutput?: (cellId: string, outputs: Array<{ type: string; data: unknown }>) => void`
- `onCellScrollTo?: (cellId: string) => void`

Update the `dependencies` array in `useCallback` for `connectWebSocket` — remove `onCellCreated`, `onCellScrollTo`, add `notebookId`, `queryClient`, `scrollToCell`.

**Step 6: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors.

**Step 7: Commit**

```bash
git add web/src/components/AgentPanel.tsx
git commit -m "feat: move cell callbacks into AgentPanel using useQueryClient"
```

---

### Task 2: Derive notebookId in AppShell

**Files:**
- Modify: `web/src/components/AppShell.tsx`

**Step 1: Add currentNotebookId memo**

After `currentPageContext` (line 81-94), add:

```tsx
const currentNotebookId = useMemo(() => {
  const nbMatch = location.pathname.match(/^\/notebooks\/([a-f0-9-]+)/)
  return nbMatch ? nbMatch[1] : ''
}, [location.pathname])
```

**Step 2: Pass notebookId to AgentPanel**

In both AgentPanel mounts (docked at line 153, floating at line 211), change:
```tsx
notebookId=""
```
to:
```tsx
notebookId={currentNotebookId}
```

**Step 3: Remove hideGlobalFab prop**

Remove `hideGlobalFab` from the `Props` interface, its usage on line 168 (`!hideGlobalFab &&`), and its passing from `NotebookPage` (handled in Task 3).

Change line 168 from:
```tsx
{!hideGlobalFab && (
```
to:
```tsx
{(
```

Also remove the prop from the `interface Props` declaration (line 15).

**Step 4: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors.

**Step 5: Commit**

```bash
git add web/src/components/AppShell.tsx
git commit -m "feat: derive notebookId from route in AppShell"
```

---

### Task 3: Remove AgentPanel mount and AI button from NotebookPage

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Remove showAgent and agentPanelWidth state**

Remove these state declarations (lines 218-227):
```tsx
const [showAgent, setShowAgent] = useState(() => {
  try { return localStorage.getItem(`hnb:agentPanel:${id}`) === 'true' } catch { return false }
})
const [agentPanelWidth, setAgentPanelWidth] = useState(() => {
  try {
    const saved = localStorage.getItem(`hnb:agentPanelWidth:${id}`)
    if (saved) return Math.max(280, Math.min(960, parseInt(saved, 10)))
  } catch { /* ignore */ }
  return Math.max(280, Min(960, Math.round(window.innerWidth / 3)))
})
```

**Step 2: Remove localStorage persistence effects**

Remove these two `useEffect` hooks (lines 457-463):
```tsx
useEffect(() => {
  localStorage.setItem(`hnb:agentPanel:${id}`, String(showAgent))
}, [showAgent, id])

useEffect(() => {
  localStorage.setItem(`hnb:agentPanelWidth:${id}`, String(agentPanelWidth))
}, [agentPanelWidth, id])
```

**Step 3: Remove AI button from toolbar**

Remove the AI button JSX (lines 1252-1259).

**Step 4: Remove hideGlobalFab from AppShell**

Change:
```tsx
<AppShell noPadding hideGlobalFab={showAgent}>
```
to:
```tsx
<AppShell noPadding>
```

**Step 5: Remove AgentPanel mount and imports**

Remove the `AgentPanel` import (line 26):
```tsx
import { AgentPanel } from '../components/AgentPanel'
```

Remove the `<AgentPanel>` JSX block (lines 1451-1487).

**Step 6: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors.

Run: `cd web && npx vite build`
Expected: Build succeeds.

**Step 7: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: remove notebook-scoped AgentPanel and AI button"
```

---

### Task 4: Final cleanup and verification

**Files:**
- Verify: all modified files

**Step 1: Verify AgentPanelProps no longer references removed props**

Run: `rg "onCellCreated|onCellOutput|onCellScrollTo" web/src/`
The only matches should be the `queryClient` calls inside AgentPanel.tsx and in the `WSMessage` type definition.

**Step 2: Verify hideGlobalFab is gone**

Run: `rg "hideGlobalFab" web/src/`
Expected: No results.

**Step 3: Full build check**

Run: `cd web && npx tsc --noEmit && npx vite build`
Expected: Both pass.

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: cleanup unused AgentPanel props and exports"
```
