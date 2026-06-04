# Group 1: Feedback & Confirmations — Design Solutions

> **Author:** UX Engineering Analysis  
> **Date:** 2026-06-04  
> **Scope:** 8 issues around missing user feedback and destructive-action confirmations in the hnb React frontend

---

## Table of Contents

1. [Running a cell without a connector gives zero feedback](#1-running-a-cell-without-a-connector-gives-zero-feedback)
2. [Connector "Test Connection" button gives no feedback](#2-connector-test-connection-button-gives-no-feedback)
3. [Invite link generation shows no visible link](#3-invite-link-generation-shows-no-visible-link)
4. [Profile page Save button has no feedback](#4-profile-page-save-button-has-no-feedback)
5. [No confirmation dialog for destructive actions](#5-no-confirmation-dialog-for-destructive-actions)
6. [Connector status shows "—" instead of meaningful status](#6-connector-status-shows--instead-of-meaningful-status)
7. [No "Last saved" indicator on notebook](#7-no-last-saved-indicator-on-notebook)
8. [Group rename/delete buttons always visible](#8-group-renamedelete-buttons-always-visible)

---

## Existing Patterns Reference

| Pattern | Component | Location |
|---------|-----------|----------|
| **Error banner** | `<ErrorBanner>` | `web/src/components/ErrorBanner.tsx` — dismissible banner with `error`/`warning`/`info` variants |
| **Modal** | `<Modal>` | `web/src/components/Modal.tsx` — overlay + card with title, close button, children |
| **Status badge** | `<StatusBadge>` | `web/src/components/StatusBadge.tsx` — inline badge with `success`/`error`/`neutral` status + optional icon |
| **Confirm dialog** | `window.confirm()` | Used throughout codebase (ConnectorsPage, DashboardsPage, GroupsPage, etc.) |
| **Save status text** | Inline `<span>` | ProfilePage uses `saveStatus` state → shows "Saved" / "Save failed" text |
| **Cell save state** | `cellSaveState` map | NotebookPage tracks per-cell `{ saving, savedAt, error }` — rendered in Cell footer |
| **Form card** | `<FormCard>` | `web/src/components/FormCard.tsx` — card with title for form sections |

---

## 1. Running a cell without a connector gives zero feedback

### Current Behavior
In `NotebookPage.tsx`, `saveAndRun` (line 406) calls the execute endpoint without checking if the cell has a `connector_id`. If no connector is assigned, the API returns an error, which is caught and displayed as an error output in the cell. However, the error message from the server may be unclear (e.g., "no connector assigned"), and the user has no proactive indication of *why* it failed.

In `Cell.tsx` (line 292), the Run button calls `onRun(cell.id)` unconditionally — it doesn't check for connector presence.

### Relevant Files
- `web/src/pages/NotebookPage.tsx` — `saveAndRun` function (line 406)
- `web/src/components/Cell.tsx` — Run button (line 292), connector badge display

### Proposed Fix
**Add a pre-flight check in `saveAndRun`** that validates connector presence before executing:

```typescript
// In NotebookPage.tsx, saveAndRun:
const saveAndRun = useCallback(async (cellId: string) => {
  const cell = localCells.find((c) => c.id === cellId)
  if (!cell) return

  // Pre-flight: check connector
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

  // ... existing save + execute logic
}, [id, localCells, paramValues, notebookConnectorId])
```

**Also disable the Run button visually** in `Cell.tsx` when no connector is available:
- Add a `noConnector` prop or derive from `connectors.length === 0 && !cell.connector_id`
- Show the Play button with reduced opacity and a tooltip: "Select a connector first"

### Dependencies
- `notebookConnectorId` state is already available in `NotebookPage`
- Cell already receives `connectors` array and `cell.connector_id`

### Complexity: Low
### Risk: None — purely additive guard

---

## 2. Connector "Test Connection" button gives no feedback

### Current Behavior
In `ConnectorsPage.tsx`, the `testConnector` function (line 117) already implements proper feedback:
- Sets `testingId` to show "Testing…" in the status column
- Stores result in `testResults` map
- Renders `<StatusBadge>` with success/error

**However**, the Test button itself (line 356) only changes its text to "Testing…" while `testingId === c.id`. The button doesn't show a spinner, and after the test completes, the result only appears in the Status column — not adjacent to the button. Users may not notice the status column update.

### Relevant Files
- `web/src/pages/ConnectorsPage.tsx` — `testConnector`, `testFormConnection`, table row rendering

### Proposed Fix
**Add inline result badge next to the Test button** in the actions column:

```tsx
<td style={styles.tdActions}>
  <button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)} disabled={testingId === c.id}>
    {testingId === c.id ? (
      <><Loader2 size={11} style={{ animation: 'spin 1s linear infinite', marginRight: 4 }} />Testing…</>
    ) : 'Test'}
  </button>
  {/* Inline test result next to button */}
  {test && test !== undefined && (
    <StatusBadge
      status={test.ok ? 'success' : 'error'}
      label={test.ok ? 'Connected' : (test.error ?? 'Failed')}
      icon={test.ok ? <Check size={12} /> : <X size={12} />}
    />
  )}
  {/* ... rest of actions */}
</td>
```

Also add a **spinner animation** to the Test button using the existing `Loader2` pattern from NotebookPage.

For the **create form's "Test Connection" button** (line 220), the feedback is already adequate — it shows "Testing…" text and a `<StatusBadge>` below. No change needed there.

### Dependencies
- `Loader2` icon already imported
- `StatusBadge` component already exists
- `testResults` state already tracks per-connector results

### Complexity: Low
### Risk: None — visual enhancement only

---

## 3. Invite link generation shows no visible link

### Current Behavior
In `MembersPage.tsx`, the `generateInviteLink` mutation (line 68) sets `generatedLink` from `data.url`. The link display section (line 143) renders an `<input>` with the link and a "Copy" button — **but only when `generatedLink` is truthy**.

**The issue**: Looking at the mutation `onSuccess`, it sets `setGeneratedLink(data.url)`. The API response returns `{ token, url }`. If `data.url` is empty/undefined, the link won't display. However, examining the code more carefully, the implementation actually **does** show the link correctly when `generatedLink` is set.

**Re-assessment**: The actual issue may be that:
1. The generated link section appears **below** the Generate button, which can be missed
2. There's no auto-copy to clipboard
3. The link disappears if the user toggles the form closed and reopens it (since `generatedLink` state is preserved but the form collapses)

### Relevant Files
- `web/src/pages/MembersPage.tsx` — `generateInviteLink` mutation, link display section

### Proposed Fix
**Auto-copy to clipboard and add visual emphasis:**

```typescript
// In generateInviteLink onSuccess:
onSuccess: (data) => {
  setGeneratedLink(data.url)
  setLinkError(null)
  setLinkLoading(false)
  // Auto-copy to clipboard
  navigator.clipboard.writeText(data.url).catch(() => {})
},
```

**Add a "copied" confirmation** using the existing pattern from Cell.tsx (`copiedId` state):

```tsx
{generatedLink && (
  <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 8 }}>
    <input
      style={{ ...styles.emailInput, flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12, background: 'var(--success-light)', border: '1px solid var(--success)' }}
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
)}
```

Also add an `<ErrorBanner>` with `variant="info"` above the link saying "Link generated! It has been copied to your clipboard."

### Dependencies
- `navigator.clipboard` API (already used in Cell.tsx)
- Existing `copiedId` pattern in Cell.tsx for "Copied!" feedback

### Complexity: Low
### Risk: None

---

## 4. Profile page Save button has no feedback

### Current Behavior
In `ProfilePage.tsx`, the save mechanism (line 56) already implements feedback:
- `saveStatus` state: `'idle' | 'saving' | 'saved' | 'error'`
- Button text changes to "Saving…" during save
- Shows "Saved" (green) or "Save failed" (red) text after completion
- Auto-clears after 2.5s timeout

**Assessment**: This is actually **already implemented correctly**. The feedback exists at lines 128–134. However, the feedback is **subtle** — it's a small text span next to the button that can be missed.

### Relevant Files
- `web/src/pages/ProfilePage.tsx` — `saveStatus` state, button rendering, success/error text

### Proposed Fix
**Enhance visibility of existing feedback** — no need to add new functionality:

1. **Add a checkmark icon** to the "Saved" text:
```tsx
{saveStatus === 'saved' && (
  <span style={{ fontSize: 13, color: 'var(--success)', fontWeight: 500, display: 'inline-flex', alignItems: 'center', gap: 4 }}>
    <Check size={14} /> Saved
  </span>
)}
```

2. **Add a brief success banner** using `<ErrorBanner variant="info">` that appears for 2 seconds:
```tsx
{saveStatus === 'saved' && (
  <ErrorBanner message="Profile updated successfully" variant="info" onDismiss={() => setSaveStatus('idle')} />
)}
```

3. **Button color flash**: Temporarily change button background to success color:
```tsx
style={{
  ...styles.saveBtn,
  opacity: saveStatus === 'saving' ? 0.6 : 1,
  background: saveStatus === 'saved' ? 'var(--success)' : 'var(--accent)',
  transition: 'background 0.3s',
}}
```

### Dependencies
- `ErrorBanner` component (already exists)
- `Check` icon from lucide-react (already imported elsewhere)

### Complexity: Low
### Risk: None — enhancement to existing working feature

---

## 5. No confirmation dialog for destructive actions

### Current Behavior
**Actually, most destructive actions already use `window.confirm()`:**
- ConnectorsPage line 382: `if (confirm(\`Delete "${c.name}"?\`))`
- DashboardsPage line 114: `if (confirm(\`Delete "${d.title}"?\`))`
- GroupsPage line 408: `if (!window.confirm(\`Delete group "${group.name}"?\`))`
- MembersPage line 88: `if (!confirm(\`Remove ${member.email}?\`))`
- MCPPage, AgentsPage, ModelsPage, SkillsPage: all use `confirm()`

**The one exception** is `HomePage.tsx` line 432–435:
```typescript
function handleDelete(type: ResourceType, id: string) {
  if (type === 'folder') deleteFolder.mutate(id)
  else if (type === 'notebook') deleteNotebook.mutate(id)
}
```
This deletes notebooks and folders from the sidebar/tree **without any confirmation**.

Also, cell deletion in `NotebookPage.tsx` (line 677) has no confirmation:
```typescript
onDelete={readOnly ? () => {} : (cid) => deleteCell.mutate(cid)}
```

### Relevant Files
- `web/src/pages/HomePage.tsx` — `handleDelete` function (line 432)
- `web/src/pages/NotebookPage.tsx` — cell `onDelete` prop (line 677)
- `web/src/components/Cell.tsx` — delete button in toolbar (line 342)

### Proposed Fix

**For HomePage (notebook/folder delete):**
Add `window.confirm()`:
```typescript
function handleDelete(type: ResourceType, id: string, name?: string) {
  const label = name || type
  if (!confirm(`Delete "${label}"? This cannot be undone.`)) return
  if (type === 'folder') deleteFolder.mutate(id)
  else if (type === 'notebook') deleteNotebook.mutate(id)
}
```

**For Cell delete in NotebookPage:**
Cell deletion is frequent and low-stakes (cells can be re-created), so a full confirm dialog would be annoying. Instead, add **undo capability**:

Option A (simpler): Keep current behavior but add a brief "Undo" toast:
```typescript
// In NotebookPage, wrap deleteCell:
const deleteCellWithUndo = (cellId: string) => {
  const cell = localCells.find(c => c.id === cellId)
  deleteCell.mutate(cellId)
  // Show undo toast for 5 seconds
  setDeletedCell(cell)
  setTimeout(() => setDeletedCell(null), 5000)
}
```

Option B (simplest): Just add `confirm()` for consistency:
```typescript
onDelete={readOnly ? () => {} : (cid) => {
  if (confirm('Delete this cell?')) deleteCell.mutate(cid)
}}
```

**Recommendation:** Use Option B for cells (simplest, consistent with other pages). Add `confirm()` to HomePage deletes.

### Dependencies
- None — uses native `window.confirm()`

### Complexity: Low
### Risk: None

---

## 6. Connector status shows "—" instead of meaningful status

### Current Behavior
In `ConnectorsPage.tsx`, the Status column (line 340) shows:
- "Testing…" when `testingId === c.id`
- The test result (`Connected` / `Failed`) when `testResults[c.id]` exists
- **"—"** when no test has been run yet

The "—" is uninformative. Users see a table full of "—" and don't know if connectors are working.

### Relevant Files
- `web/src/pages/ConnectorsPage.tsx` — Status column rendering (line 340)

### Proposed Fix
**Option A: Auto-test on page load** (best UX, higher complexity)
Run a lightweight health check for each connector when the page loads:

```typescript
// After connectors are loaded, auto-test any that haven't been tested
useEffect(() => {
  connectors.forEach(c => {
    if (!testResults[c.id] && testingId !== c.id) {
      testConnector(c.id)
    }
  })
}, [connectors]) // Only run when connectors list changes
```

⚠️ This could be slow with many connectors and puts load on databases. Not recommended for production without backend support for a batch health endpoint.

**Option B: Show "Not tested" instead of "—"** (simplest, immediate improvement)
```tsx
<StatusBadge status="neutral" label="Not tested" />
```

**Option C: Show "Unknown — click Test"** (informative, prompts action)
```tsx
<span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
  Unknown — click Test
</span>
```

**Recommendation:** Option C — it's informative, prompts the user to take action, and requires zero backend changes.

### Dependencies
- None

### Complexity: Trivial
### Risk: None

---

## 7. No "Last saved" indicator on notebook

### Current Behavior
The notebook header in `NotebookPage.tsx` (line 520) shows:
```tsx
<span style={styles.metaText}>
  Last updated {fmtTime(new Date(notebook.updated_at))}
</span>
```

This shows when the notebook *metadata* was last updated (title, description, structure), but **not when cell source code was last auto-saved**. The per-cell save state (`cellSaveState`) tracks individual cell saves but isn't surfaced in the notebook header.

### Relevant Files
- `web/src/pages/NotebookPage.tsx` — header rendering (line 520), `cellSaveState` (line 117)
- `web/src/components/Cell.tsx` — per-cell footer with save state (line 355)

### Proposed Fix
**Add a global save indicator to the notebook header** that reflects the most recent cell save:

```typescript
// Derive the latest save time from cellSaveState
const latestSave = useMemo(() => {
  let latest: Date | null = null
  for (const state of Object.values(cellSaveState)) {
    if (state.savedAt && (!latest || state.savedAt > latest)) {
      latest = state.savedAt
    }
  }
  return latest
}, [cellSaveState])

// Check if any cell is currently saving
const anySaving = useMemo(() =>
  Object.values(cellSaveState).some(s => s.saving),
  [cellSaveState]
)
```

In the header, next to "Last updated":
```tsx
<div style={styles.metaInfo}>
  <span style={styles.metaText}>
    Last updated {fmtTime(new Date(notebook.updated_at))}
  </span>
  {anySaving && (
    <span style={{ ...styles.metaText, color: 'var(--text-muted)' }}>
      <Loader2 size={10} style={{ animation: 'spin 1s linear infinite', verticalAlign: 'middle', marginRight: 3 }} />
      Saving…
    </span>
  )}
  {!anySaving && latestSave && (
    <span style={{ ...styles.metaText, color: 'var(--success)' }}>
      ✓ All changes saved
    </span>
  )}
</div>
```

### Dependencies
- `cellSaveState` already tracked in NotebookPage
- `Loader2` icon already imported
- `useMemo` from React (already imported)

### Complexity: Low
### Risk: None — read-only derived state

---

## 8. Group rename/delete buttons always visible

### Current Behavior
In `GroupsPage.tsx`, the admin actions (Rename, Delete) are always visible in the group card header row (lines 540–570):

```tsx
{isAdmin && (
  <div style={styles.actions}>
    <button style={styles.actionBtn} onClick={() => handleRename(group)}>Rename</button>
    <button style={styles.deleteBtn} onClick={() => handleDelete(group)}>Delete</button>
  </div>
)}
```

These are always visible, taking up space and creating accidental-click risk.

### Relevant Files
- `web/src/pages/GroupsPage.tsx` — admin actions in group row (line 540)

### Proposed Fix
**Replace with a "⋯" context menu** (three-dot overflow menu):

```tsx
{isAdmin && (
  <div style={{ position: 'relative' }}>
    <button
      type="button"
      style={styles.menuTrigger}
      onClick={(e) => {
        e.stopPropagation()
        setGroupMenuOpen(groupMenuOpen === group.id ? null : group.id)
      }}
    >
      ⋯
    </button>
    {groupMenuOpen === group.id && (
      <div style={styles.contextMenu}>
        <button onClick={() => { handleRename(group); setGroupMenuOpen(null) }}>
          Rename
        </button>
        <button style={{ color: 'var(--error)' }} onClick={() => { handleDelete(group); setGroupMenuOpen(null) }}>
          Delete
        </button>
      </div>
    )}
  </div>
)}
```

With styles:
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
```

Add click-outside-to-close using the same pattern as `MemberDropdown` in the same file.

**State addition:**
```typescript
const [groupMenuOpen, setGroupMenuOpen] = useState<string | null>(null)
```

### Dependencies
- Click-outside pattern already exists in `MemberDropdown` (same file)
- No new components needed

### Complexity: Medium
### Risk: Low — self-contained to GroupsPage

---

## Summary Table

| # | Issue | Fix Complexity | Files Changed | Pattern Used |
|---|-------|---------------|---------------|--------------|
| 1 | No feedback running without connector | **Low** | NotebookPage.tsx, Cell.tsx | Pre-flight guard + error output |
| 2 | Test Connection no feedback | **Low** | ConnectorsPage.tsx | Loader2 spinner + inline StatusBadge |
| 3 | Invite link not visible | **Low** | MembersPage.tsx | Auto-copy + success highlight |
| 4 | Profile Save no feedback | **Low** | ProfilePage.tsx | Enhanced existing feedback with icon |
| 5 | No delete confirmation | **Low** | HomePage.tsx, NotebookPage.tsx | `window.confirm()` |
| 6 | Connector status shows "—" | **Trivial** | ConnectorsPage.tsx | Replace text with "Unknown — click Test" |
| 7 | No "Last saved" indicator | **Low** | NotebookPage.tsx | Derive from `cellSaveState` |
| 8 | Group buttons always visible | **Medium** | GroupsPage.tsx | Context menu (⋯ overflow) |

---

## Implementation Priority

1. **Issue 5** (delete confirmations) — **P0**: Prevents data loss, trivial to implement
2. **Issue 1** (run without connector) — **P0**: Core workflow broken, confusing UX
3. **Issue 6** (connector status "—") — **P1**: Trivial fix, high visibility improvement
4. **Issue 2** (test connection feedback) — **P1**: Important for connector setup flow
5. **Issue 7** (last saved indicator) — **P1**: Key for collaborative editing confidence
6. **Issue 3** (invite link visibility) — **P2**: Works but UX could be smoother
7. **Issue 4** (profile save feedback) — **P2**: Already functional, enhancement only
8. **Issue 8** (group action buttons) — **P2**: Polish item, reduces accidental clicks

---

## Testing Notes

- All fixes are frontend-only (no API changes required)
- Existing test patterns use `vi.spyOn(window, 'confirm')` for confirmation tests
- Visual changes should be verified with Playwright snapshot tests
- The `cellSaveState` derivation (Issue 7) should be unit-tested with various state combinations
