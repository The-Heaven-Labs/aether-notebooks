# Single Agent Panel

**Date:** 2026-06-21
**Status:** ✅ Partially Implemented

## Problem

The AI agent panel has two mount points — one in `AppShell` ("global") and one in `NotebookPage` ("notebook") — that render the same `AgentPanel` component with different props. The global panel lacks notebook-specific callbacks (`onCellOutput`, `onCellScrollTo`, `onCellCreated`), creating a feature gap. Maintaining two entry points adds complexity and risks behavioral drift.

## Solution

Eliminate the notebook-specific mount point. Render `AgentPanel` only from `AppShell`, and move the cell-output/cell-created/scroll-to behavior into `AgentPanel` itself, driven by a `notebookId` derived from the current route.

## Detailed Changes

### 1. AgentPanel: internal cell callbacks

Import `useQueryClient` from `@tanstack/react-query`. Handle three WebSocket messages internally when `notebookId` is set:

| WS Message | Action |
|---|---|
| `cell_output` | `queryClient.setQueryData(['notebook', notebookId], ...)` to update the cell's outputs |
| `cell_created` | `queryClient.invalidateQueries({ queryKey: ['notebook', notebookId] })` |
| `cell_updated` | (no cache update needed — frontend Yjs/collab handles it) |

Add a `scrollToCell(cellId)` helper that polls for the DOM element and calls `scrollIntoView` + flash animation. Call it for all three message types.

### 2. AppShell: derive notebookId from route

Add a `currentNotebookId` memo derived from `location.pathname` (same regex used for `currentPageContext`). Pass it as `notebookId` prop to `AgentPanel`.

Remove `hideGlobalFab` prop — no longer needed since NotebookPage no longer mounts its own AgentPanel.

### 3. NotebookPage: remove AgentPanel mount

Remove:
- `showAgent` state and its localStorage persistence
- `agentPanelWidth` state and its localStorage persistence
- AI toggle button from toolbar
- `<AgentPanel>` JSX element
- `hideGlobalFab={showAgent}` from `<AppShell>`

### 4. Empty state text

Already handled by existing `notebookId ? "Ask me about this notebook..." : "Ask me anything..."` branching in AgentPanel (line 972-975).

## Edge Cases

- **Navigating away from notebook:** `notebookId` becomes `""`. Chat history under `__global__` key is preserved. Agent loses notebook context but conversation persists.
- **Multiple notebooks:** Panel is scoped to whichever notebook the user is currently viewing. `notebookId` changes reactively.
- **Page refresh during stream:** Handled by existing `reconnect_sync` WS mechanism — no changes needed.
- **Tool confirm / cell diff / auto-approve:** Entirely internal to AgentPanel — unaffected.

## Removed State

From `NotebookPage`: `showAgent` (boolean), `agentPanelWidth` (number), 2 `useEffect` hooks for localStorage persistence.
