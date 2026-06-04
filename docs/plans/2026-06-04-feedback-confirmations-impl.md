# Implementation Plan: Group 1 — Feedback & Confirmations

> **Date:** 2026-06-04  
> **Goal:** Add missing user feedback and destructive-action confirmations across the hnb React frontend  
> **Architecture:** Frontend-only changes (no API modifications). All fixes use existing components (`ErrorBanner`, `StatusBadge`, `Modal`) and patterns (`window.confirm()`, `cellSaveState`, `navigator.clipboard`).  
> **Tech Stack:** React 18, TypeScript, Vite, lucide-react icons, inline styles (no CSS modules)

---

## Summary of Changes

| # | Issue | File(s) Changed | Complexity |
|---|-------|-----------------|------------|
| 1 | Run cell without connector → zero feedback | `NotebookPage.tsx`, `Cell.tsx` | Low |
| 2 | Test Connection button → no inline feedback | `ConnectorsPage.tsx` | Low |
| 3 | Invite link → not auto-copied, no visual emphasis | `MembersPage.tsx` | Low |
| 4 | Profile Save → feedback too subtle | `ProfilePage.tsx` | Low |
| 5 | No delete confirmation on HomePage/NotebookPage | `HomePage.tsx`, `NotebookPage.tsx` | Low |
| 6 | Connector status shows "—" | `ConnectorsPage.tsx` | Trivial |
| 7 | No "Last saved" indicator on notebook | `NotebookPage.tsx` | Low |
| 8 | Group rename/delete buttons always visible | `GroupsPage.tsx` | Medium |

---

## Task 1: Add pre-flight connector check when running a cell

**Issue:** Running a cell without a connector gives zero feedback. The API returns an unclear error.  
**Fix:** Add a pre-flight check in `saveAndRun` that validates connector presence before executing. Also visually disable the Run button when no connector is available.

### Step 1.1: Add connector pre-flight check in `saveAndRun`

**File:** `web/src/pages/NotebookPage.tsx`  
**Lines:** 406–438

**Current code (line 406–411):**
```typescript
const saveAndRun = useCallback(
    async (cellId: string) => {
      const cell = localCells.find((c) => c.id === cellId)
      if (!cell) return

      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { source: cell.source })
```

**Replace with:**
```typescript
const saveAndRun = useCallback(
    async (cellId: string) => {
      const cell = localCells.find((c) => c.id === cellId)
      if (!cell) return

      // Pre-flight: check connector is assigned
      const effectiveConnectorId = cell.connector_id || notebookConnectorId
      if (!effectiveConnectorId) {
        setLocalCells((prev) =>
          prev.map((c) =>
            c.id === cellId
              ? { ...c, outputs: [{ type: 'error', data: 'No connector selected. Assign a connector to this cell or set a default notebook connector.' }] }
              : c
          )
        )
        return
      }

      await api.put(`/api/v1/notebooks/${id}/cells/${cellId}`, { source: cell.source })
```

**Also update the dependency array (line 438):**
```typescript
    [id, localCells, paramValues, notebookConnectorId],
```

### Step 1.2: Visually indicate missing connector on Run button

**File:** `web/src/components/Cell.tsx`  
**Lines:** ~289–298 (Run button in hover toolbar)

**Current code:**
```tsx
{isCode && (
  <button
    style={styles.actionBtn}
    onClick={(e) => { e.stopPropagation(); onRun(cell.id) }}
    disabled={running}
    title="Run (Ctrl+Enter)"
  >
    {running
      ? <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />
      : <Play size={11} />
    }
  </button>
)}
```

**Replace with:**
```tsx
{isCode && (() => {
  const hasConnector = !!(cell.connector_id || connectors.length > 0)
  return (
    <button
      style={{ ...styles.actionBtn, opacity: hasConnector ? 1 : 0.4 }}
      onClick={(e) => { e.stopPropagation(); onRun(cell.id) }}
      disabled={running}
      title={hasConnector ? 'Run (Ctrl+Enter)' : 'Select a connector first'}
    >
      {running
        ? <Loader2 size={11} style={{ animation: 'spin 1s linear infinite' }} />
        : <Play size={11} />
      }
    </button>
  )
})()}
```

### Step 1.3: Commit

```bash
git add web/src/pages/NotebookPage.tsx web/src/components/Cell.tsx
git commit -m "feat: add pre-flight connector check when running cells

- Validate connector presence before executing cell
- Show clear error message when no connector is assigned
- Visually dim Run button when no connector available
- Add tooltip explaining why run is disabled"
```

---

## Task 2: Add inline feedback to Test Connection button

**Issue:** The Test Connection button shows "Testing…" text but the result only appears in the Status column, which users may not notice.  
**Fix:** Add a `Loader2` spinner to the Test button and an inline `StatusBadge` next to it showing the result.

### Step 2.1: Add `Loader2` import to ConnectorsPage

**File:** `web/src/pages/ConnectorsPage.tsx`  
**Line 7:**

**Current:**
```typescript
import { Check, X } from 'lucide-react'
```

**Replace with:**
```typescript
import { Check, X, Loader2 } from 'lucide-react'
```

### Step 2.2: Add spinner + inline StatusBadge to Test button

**File:** `web/src/pages/ConnectorsPage.tsx`  
**Lines 353–355:**

**Current code:**
```tsx
<button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)} disabled={testingId === c.id}>
  {testingId === c.id ? 'Testing…' : 'Test'}
</button>
```

**Replace with:**
```tsx
<button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)} disabled={testingId === c.id}>
  {testingId === c.id ? (
    <><Loader2 size={11} style={{ animation: 'spin 1s linear infinite', marginRight: 4 }} />Testing…</>
  ) : 'Test'}
</button>
{test && test !== undefined && (
  <StatusBadge
    status={test.ok ? 'success' : 'error'}
    label={test.ok ? 'Connected' : (test.error ?? 'Failed')}
    icon={test.ok ? <Check size={12} /> : <X size={12} />}
  />
)}
```

### Step 2.3: Commit

```bash
git add web/src/pages/ConnectorsPage.tsx
git commit -m "feat: add spinner and inline status badge to Test Connection button

- Show Loader2 spinner while test is running
- Display inline StatusBadge next to Test button with result
- Makes test results immediately visible without looking at Status column"
```

---

## Task 3: Auto-copy invite link and add visual emphasis

**Issue:** Generated invite link is displayed but not auto-copied, and there's no "Copied!" confirmation.  
**Fix:** Auto-copy to clipboard on generation, add "Copied!" feedback on the copy button, and visually highlight the link input.

### Step 3.1: Add `linkCopied` state

**File:** `web/src/pages/MembersPage.tsx`  
**After line 31** (after `const [linkLoading, setLinkLoading] = useState(false)`):

**Add:**
```typescript
const [linkCopied, setLinkCopied] = useState(false)
```

### Step 3.2: Auto-copy on successful generation

**File:** `web/src/pages/MembersPage.tsx`  
**Lines 68–73 (the `onSuccess` callback of `generateInviteLink`):**

**Current code:**
```typescript
onSuccess: (data) => {
  setGeneratedLink(data.url)
  setLinkError(null)
  setLinkLoading(false)
},
```

**Replace with:**
```typescript
onSuccess: (data) => {
  setGeneratedLink(data.url)
  setLinkError(null)
  setLinkLoading(false)
  // Auto-copy to clipboard
  navigator.clipboard.writeText(data.url).then(() => {
    setLinkCopied(true)
    setTimeout(() => setLinkCopied(false), 2000)
  }).catch(() => {})
},
```

### Step 3.3: Visually highlight the link input and add "Copied!" feedback

**File:** `web/src/pages/MembersPage.tsx`  
**Lines 163–180 (the `generatedLink` display section):**

**Current code:**
```tsx
{generatedLink && (
  <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
    <input
      style={{ ...styles.emailInput, flex: 1 }}
      type="text"
      value={generatedLink}
      readOnly
      onClick={(e) => (e.target as HTMLInputElement).select()}
    />
    <button
      type="button"
      style={{ ...styles.inviteBtn, padding: '7px 12px' }}
      onClick={() => {
        navigator.clipboard.writeText(generatedLink)
      }}
    >
      Copy
    </button>
  </div>
)}
```

**Replace with:**
```tsx
{generatedLink && (
  <>
    <ErrorBanner
      message="Link generated and copied to clipboard!"
      variant="info"
      onDismiss={() => {}}
    />
    <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 8 }}>
      <input
        style={{ ...styles.emailInput, flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12, background: 'var(--accent-light)', border: '1px solid var(--accent)' }}
        type="text"
        value={generatedLink}
        readOnly
        onClick={(e) => (e.target as HTMLInputElement).select()}
      />
      <button
        type="button"
        style={{ ...styles.inviteBtn, padding: '7px 12px' }}
        onClick={() => {
          navigator.clipboard.writeText(generatedLink)
          setLinkCopied(true)
          setTimeout(() => setLinkCopied(false), 2000)
        }}
      >
        {linkCopied ? '✓ Copied!' : 'Copy'}
      </button>
    </div>
  </>
)}
```

### Step 3.4: Commit

```bash
git add web/src/pages/MembersPage.tsx
git commit -m "feat: auto-copy invite link and add visual feedback

- Auto-copy generated link to clipboard
- Show info banner confirming link was copied
- Highlight link input with accent color
- Add 'Copied!' feedback on copy button"
```

---

## Task 4: Enhance Profile page Save feedback

**Issue:** Save feedback exists but is too subtle (small text span).  
**Fix:** Add a checkmark icon to "Saved" text, add a brief success banner, and flash the button color.

### Step 4.1: Add `Check` icon import

**File:** `web/src/pages/ProfilePage.tsx`  
**After line 4** (after `import { SectionHeader } from '../components/SectionHeader'`):

**Add:**
```typescript
import { Check } from 'lucide-react'
import { ErrorBanner } from '../components/ErrorBanner'
```

### Step 4.2: Add success banner above save button area

**File:** `web/src/pages/ProfilePage.tsx`  
**Lines ~125–135 (the save button area):**

**Current code:**
```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 }}>
  <button
    type="button"
    style={{ ...styles.saveBtn, opacity: saveStatus === 'saving' ? 0.6 : 1 }}
    onClick={handleSave}
    disabled={saveStatus === 'saving'}
  >
    {saveStatus === 'saving' ? 'Saving…' : 'Save'}
  </button>
  {saveStatus === 'saved' && (
    <span style={{ fontSize: 13, color: 'var(--success)', fontWeight: 500 }}>Saved</span>
  )}
  {saveStatus === 'error' && (
    <span style={{ fontSize: 13, color: 'var(--error)', fontWeight: 500 }}>Save failed</span>
  )}
</div>
```

**Replace with:**
```tsx
{saveStatus === 'saved' && (
  <ErrorBanner message="Profile updated successfully" variant="info" onDismiss={() => setSaveStatus('idle')} />
)}
<div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 }}>
  <button
    type="button"
    style={{
      ...styles.saveBtn,
      opacity: saveStatus === 'saving' ? 0.6 : 1,
      background: saveStatus === 'saved' ? 'var(--success)' : 'var(--accent)',
      transition: 'background 0.3s',
    }}
    onClick={handleSave}
    disabled={saveStatus === 'saving'}
  >
    {saveStatus === 'saving' ? 'Saving…' : 'Save'}
  </button>
  {saveStatus === 'saved' && (
    <span style={{ fontSize: 13, color: 'var(--success)', fontWeight: 500, display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      <Check size={14} /> Saved
    </span>
  )}
  {saveStatus === 'error' && (
    <span style={{ fontSize: 13, color: 'var(--error)', fontWeight: 500 }}>Save failed</span>
  )}
</div>
```

### Step 4.3: Commit

```bash
git add web/src/pages/ProfilePage.tsx
git commit -m "feat: enhance Profile page save feedback

- Add success banner with info variant on save
- Flash button green on successful save
- Add checkmark icon to 'Saved' text
- Smooth transition on button background color"
```

---

## Task 5: Add confirmation dialogs for destructive actions

**Issue:** `HomePage.tsx` deletes notebooks/folders without confirmation. Cell deletion in `NotebookPage.tsx` also has no confirmation.  
**Fix:** Add `window.confirm()` calls before all destructive operations.

### Step 5.1: Add confirmation to HomePage `handleDelete`

**File:** `web/src/pages/HomePage.tsx`  
**Lines 432–436:**

**Current code:**
```typescript
function handleDelete(type: ResourceType, id: string) {
  if (type === 'folder') deleteFolder.mutate(id)
  else if (type === 'notebook') deleteNotebook.mutate(id)
  // connectors / dashboards: no-op (TODO: implement via their own pages)
}
```

**Replace with:**
```typescript
function handleDelete(type: ResourceType, id: string, name?: string) {
  const label = name || type
  if (!confirm(`Delete "${label}"? This cannot be undone.`)) return
  if (type === 'folder') deleteFolder.mutate(id)
  else if (type === 'notebook') deleteNotebook.mutate(id)
  // connectors / dashboards: no-op (TODO: implement via their own pages)
}
```

**Also update the `ContextMenu` interface and usage to pass `name`:**

**File:** `web/src/pages/HomePage.tsx`

First, update the `ContextMenuProps` interface and `ContextMenu` component. Find the `onDelete` prop type (around line 42):

**Current:**
```typescript
onDelete: (type: ResourceType, id: string) => void
```

**Replace with:**
```typescript
onDelete: (type: ResourceType, id: string, name: string) => void
```

Then update the delete button in the ContextMenu (around line 78):

**Current:**
```tsx
{canDelete ? (
  <button style={{ ...ms.item, color: 'var(--error)' }} onClick={() => {
    onDelete(target.type, target.id)
    onClose()
  }}>Delete</button>
) : (
```

**Replace with:**
```tsx
{canDelete ? (
  <button style={{ ...ms.item, color: 'var(--error)' }} onClick={() => {
    onDelete(target.type, target.id, target.name)
    onClose()
  }}>Delete</button>
) : (
```

### Step 5.2: Add confirmation to cell deletion in NotebookPage

**File:** `web/src/pages/NotebookPage.tsx`  
**Line 677:**

**Current code:**
```tsx
onDelete={readOnly ? () => {} : (cid) => deleteCell.mutate(cid)}
```

**Replace with:**
```tsx
onDelete={readOnly ? () => {} : (cid) => {
  if (confirm('Delete this cell?')) deleteCell.mutate(cid)
}}
```

### Step 5.3: Commit

```bash
git add web/src/pages/HomePage.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: add confirmation dialogs for all destructive actions

- Add confirm() before notebook/folder delete on HomePage
- Add confirm() before cell delete in NotebookPage
- Pass resource name to confirmation dialog for clarity
- Consistent with existing confirm() usage in other pages"
```

---

## Task 6: Replace "—" with meaningful connector status text

**Issue:** Connector status column shows "—" when no test has been run, which is uninformative.  
**Fix:** Replace with "Unknown — click Test" to prompt user action.

### Step 6.1: Replace the "—" StatusBadge

**File:** `web/src/pages/ConnectorsPage.tsx`  
**Line 349:**

**Current code:**
```tsx
<StatusBadge status="neutral" label="—" />
```

**Replace with:**
```tsx
<span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
  Unknown — click Test
</span>
```

### Step 6.2: Commit

```bash
git add web/src/pages/ConnectorsPage.tsx
git commit -m "fix: replace uninformative '—' with 'Unknown — click Test' in connector status

- Prompt users to run a test instead of showing ambiguous dash
- Uses italic muted text to distinguish from actual status badges"
```

---

## Task 7: Add "Last saved" indicator to notebook header

**Issue:** The notebook header shows "Last updated" (metadata timestamp) but not when cell source code was last auto-saved.  
**Fix:** Derive the latest save time from `cellSaveState` and display a global save indicator.

### Step 7.1: Add `useMemo` import

**File:** `web/src/pages/NotebookPage.tsx`  
**Line 1:**

**Current:**
```typescript
import { useState, useEffect, useCallback, useRef } from 'react'
```

**Replace with:**
```typescript
import { useState, useEffect, useCallback, useRef, useMemo } from 'react'
```

### Step 7.2: Add `Check` icon import

**File:** `web/src/pages/NotebookPage.tsx`  
**Line 3:**

**Current:**
```typescript
import { ChevronsRight, ChevronLeft, Loader2, X, Bot } from 'lucide-react'
```

**Replace with:**
```typescript
import { ChevronsRight, ChevronLeft, Loader2, X, Bot, Check } from 'lucide-react'
```

### Step 7.3: Add derived save state computations

**File:** `web/src/pages/NotebookPage.tsx`  
**After line 117** (after the `cellSaveState` state declaration, before `cellRunAt`):

Find this line:
```typescript
const [cellSaveState, setCellSaveState] = useState<Record<string, { saving: boolean; savedAt: Date | null; error: string | null }>>({})
```

**Add immediately after it:**
```typescript

  // Derive global save status from per-cell save states
  const latestCellSave = useMemo(() => {
    let latest: Date | null = null
    for (const state of Object.values(cellSaveState)) {
      if (state.savedAt && (!latest || state.savedAt > latest)) {
        latest = state.savedAt
      }
    }
    return latest
  }, [cellSaveState])

  const anyCellSaving = useMemo(() =>
    Object.values(cellSaveState).some(s => s.saving),
    [cellSaveState]
  )

  const anyCellError = useMemo(() =>
    Object.values(cellSaveState).some(s => s.error),
    [cellSaveState]
  )
```

### Step 7.4: Update the header meta info section

**File:** `web/src/pages/NotebookPage.tsx`  
**Lines 521–525:**

**Current code:**
```tsx
<div style={styles.metaInfo}>
  <span style={styles.metaText}>
    Last updated {fmtTime(new Date(notebook.updated_at))}
  </span>
</div>
```

**Replace with:**
```tsx
<div style={styles.metaInfo}>
  <span style={styles.metaText}>
    Last updated {fmtTime(new Date(notebook.updated_at))}
  </span>
  {anyCellSaving && (
    <span style={{ ...styles.metaText, color: 'var(--text-muted)', display: 'inline-flex', alignItems: 'center', gap: 3 }}>
      <Loader2 size={10} style={{ animation: 'spin 1s linear infinite' }} />
      Saving…
    </span>
  )}
  {!anyCellSaving && latestCellSave && (
    <span style={{ ...styles.metaText, color: 'var(--success)', display: 'inline-flex', alignItems: 'center', gap: 3 }}>
      <Check size={11} /> All changes saved
    </span>
  )}
  {!anyCellSaving && anyCellError && (
    <span style={{ ...styles.metaText, color: 'var(--error-full)' }}>
      Save error
    </span>
  )}
</div>
```

### Step 7.5: Commit

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: add global save status indicator to notebook header

- Show 'Saving…' with spinner when any cell is saving
- Show '✓ All changes saved' in green when all saves complete
- Show 'Save error' in red when any cell has a save error
- Derives from existing cellSaveState — no new API calls"
```

---

## Task 8: Replace group action buttons with context menu

**Issue:** Rename/Delete buttons are always visible on group cards, taking up space and risking accidental clicks.  
**Fix:** Replace with a "⋯" overflow context menu that shows actions on click.

### Step 8.1: Add `groupMenuOpen` state

**File:** `web/src/pages/GroupsPage.tsx`  
**After the `selectedUserId` state** (around line 248):

Find:
```typescript
const [selectedUserId, setSelectedUserId] = useState<Record<string, string>>({})
```

**Add immediately after:**
```typescript

  // Context menu for group actions
  const [groupMenuOpen, setGroupMenuOpen] = useState<string | null>(null)
  const groupMenuRefs = useRef<Record<string, HTMLDivElement | null>>({})
```

### Step 8.2: Add click-outside handler for context menus

**File:** `web/src/pages/GroupsPage.tsx`  
**After the `useEffect` for `document.title`** (around line 253):

Find:
```typescript
useEffect(() => { document.title = "Groups — Heaven's Notebooks" }, [])
```

**Add immediately after:**
```typescript

  // Close group context menu on outside click
  useEffect(() => {
    if (!groupMenuOpen) return
    const handler = (e: MouseEvent) => {
      const ref = groupMenuRefs.current[groupMenuOpen]
      if (ref && !ref.contains(e.target as Node)) {
        setGroupMenuOpen(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [groupMenuOpen])
```

### Step 8.3: Replace the inline action buttons with a context menu

**File:** `web/src/pages/GroupsPage.tsx`  
**Lines 560–597** (the `{isAdmin && (` block with Rename/Delete buttons):

**Current code:**
```tsx
{isAdmin && (
  <div style={styles.actions}>
    {isRenaming ? (
      <>
        <button
          type="button"
          style={styles.actionBtn}
          onClick={() => handleRenameSubmit(group.id)}
          disabled={updateGroup.isPending}
        >
          Save
        </button>
        <button
          type="button"
          style={styles.actionBtn}
          onClick={() => setRenamingId(null)}
        >
          Cancel
        </button>
      </>
    ) : (
      <button
        type="button"
        style={styles.actionBtn}
        onClick={() => handleRename(group)}
      >
        Rename
      </button>
    )}
    <button
      type="button"
      style={styles.deleteBtn}
      onClick={() => handleDelete(group)}
      disabled={deleteGroup.isPending}
    >
      Delete
    </button>
  </div>
)}
```

**Replace with:**
```tsx
{isAdmin && (
  <div style={{ position: 'relative' }} ref={(el) => { groupMenuRefs.current[group.id] = el }}>
    {isRenaming ? (
      <div style={styles.actions}>
        <button
          type="button"
          style={styles.actionBtn}
          onClick={() => handleRenameSubmit(group.id)}
          disabled={updateGroup.isPending}
        >
          Save
        </button>
        <button
          type="button"
          style={styles.actionBtn}
          onClick={() => setRenamingId(null)}
        >
          Cancel
        </button>
      </div>
    ) : (
      <>
        <button
          type="button"
          style={styles.menuTrigger}
          onClick={(e) => {
            e.stopPropagation()
            setGroupMenuOpen(groupMenuOpen === group.id ? null : group.id)
          }}
          title="Group actions"
          aria-label="Group actions"
        >
          ⋯
        </button>
        {groupMenuOpen === group.id && (
          <div style={styles.contextMenu}>
            <button
              type="button"
              style={styles.contextMenuItem}
              onClick={(e) => {
                e.stopPropagation()
                handleRename(group)
                setGroupMenuOpen(null)
              }}
            >
              Rename
            </button>
            <button
              type="button"
              style={{ ...styles.contextMenuItem, color: 'var(--error)' }}
              onClick={(e) => {
                e.stopPropagation()
                handleDelete(group)
                setGroupMenuOpen(null)
              }}
              disabled={deleteGroup.isPending}
            >
              Delete
            </button>
          </div>
        )}
      </>
    )}
  </div>
)}
```

### Step 8.4: Add context menu styles

**File:** `web/src/pages/GroupsPage.tsx`  
**In the `styles` object** (at the end, before the closing `}`):

**Add these new style entries:**
```typescript
  menuTrigger: {
    padding: '4px 8px',
    fontSize: 16,
    background: 'transparent',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    lineHeight: 1,
  },
  contextMenu: {
    position: 'absolute',
    top: '100%',
    right: 0,
    marginTop: 4,
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    boxShadow: 'var(--shadow-md)',
    zIndex: 100,
    minWidth: 120,
    overflow: 'hidden',
  },
  contextMenuItem: {
    display: 'block',
    width: '100%',
    padding: '8px 14px',
    fontSize: 13,
    fontWeight: 500,
    background: 'transparent',
    border: 'none',
    cursor: 'pointer',
    textAlign: 'left' as const,
    color: 'var(--text-primary)',
  },
```

### Step 8.5: Commit

```bash
git add web/src/pages/GroupsPage.tsx
git commit -m "feat: replace group action buttons with context menu

- Replace always-visible Rename/Delete buttons with ⋯ trigger
- Context menu shows Rename and Delete options on click
- Close on outside click (same pattern as MemberDropdown)
- Keep inline Save/Cancel buttons during rename (must be visible)
- Reduces visual clutter and accidental click risk"
```

---

## Task 9: Final verification and integration test

### Step 9.1: Run the dev server and manually verify each fix

```bash
# Start infrastructure
task infra:up

# Start the dev server
task dev

# In another terminal, start the web dev server
task dev:web
```

### Step 9.2: Verification checklist

Open `http://localhost:5173` and verify:

1. **Cell run without connector:**
   - [ ] Open a notebook with no connector assigned
   - [ ] Click Run on a code cell → should show error output: "No connector selected…"
   - [ ] Run button should appear dimmed (40% opacity)
   - [ ] Hovering should show tooltip: "Select a connector first"

2. **Test Connection feedback:**
   - [ ] Go to Connectors page
   - [ ] Click "Test" on a connector → should show spinner + "Testing…"
   - [ ] After test completes → inline StatusBadge appears next to button
   - [ ] Status column also updates as before

3. **Invite link:**
   - [ ] Go to Members page → click "+ Generate invite link"
   - [ ] Click "Generate" → link appears with accent background
   - [ ] Info banner says "Link generated and copied to clipboard!"
   - [ ] Click "Copy" → button shows "✓ Copied!" for 2 seconds

4. **Profile save:**
   - [ ] Go to Profile page → change name → click Save
   - [ ] Info banner appears: "Profile updated successfully"
   - [ ] Button flashes green briefly
   - [ ] "✓ Saved" text appears with checkmark icon

5. **Delete confirmations:**
   - [ ] Go to HomePage → right-click a notebook → Delete
   - [ ] Should show confirm dialog: `Delete "notebook name"? This cannot be undone.`
   - [ ] Click Cancel → notebook should NOT be deleted
   - [ ] Open a notebook → click X on a cell → confirm dialog appears

6. **Connector status:**
   - [ ] Go to Connectors page with an untested connector
   - [ ] Status column shows italic "Unknown — click Test" instead of "—"

7. **Last saved indicator:**
   - [ ] Open a notebook → edit a cell
   - [ ] Header shows "Saving…" with spinner while auto-saving
   - [ ] After save completes → "✓ All changes saved" in green

8. **Group context menu:**
   - [ ] Go to Groups page (as admin)
   - [ ] Group row shows "⋯" button instead of Rename/Delete
   - [ ] Click "⋯" → dropdown appears with Rename and Delete
   - [ ] Click outside → menu closes
   - [ ] Click Rename → inline rename input appears (Save/Cancel visible)

### Step 9.3: Run existing tests

```bash
task test
```

All existing tests should pass (no API changes, no breaking changes).

### Step 9.4: Build verification

```bash
task build:web
```

Should complete without TypeScript errors.

### Step 9.5: Final commit

```bash
git add -A
git commit -m "chore: verify all Group 1 feedback & confirmation fixes

- All 8 issues resolved with frontend-only changes
- No API modifications required
- All existing tests pass
- Build succeeds without errors"
```

---

## Appendix: Files Modified

| File | Tasks | Lines Changed (approx) |
|------|-------|----------------------|
| `web/src/pages/NotebookPage.tsx` | 1, 5, 7 | ~40 |
| `web/src/components/Cell.tsx` | 1 | ~15 |
| `web/src/pages/ConnectorsPage.tsx` | 2, 6 | ~15 |
| `web/src/pages/MembersPage.tsx` | 3 | ~25 |
| `web/src/pages/ProfilePage.tsx` | 4 | ~20 |
| `web/src/pages/HomePage.tsx` | 5 | ~10 |
| `web/src/pages/GroupsPage.tsx` | 8 | ~60 |

**Total:** ~185 lines changed across 7 files

---

## Appendix: Dependencies & Imports Added

| File | Import Added |
|------|-------------|
| `NotebookPage.tsx` | `useMemo` from 'react', `Check` from 'lucide-react' |
| `ConnectorsPage.tsx` | `Loader2` from 'lucide-react' |
| `ProfilePage.tsx` | `Check` from 'lucide-react', `ErrorBanner` component |

No new npm packages required.
