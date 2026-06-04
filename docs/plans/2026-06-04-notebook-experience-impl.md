# Group 3: Notebook & Cell Experience — Implementation Plan

> **Date:** 2026-06-04  
> **Goal:** Implement 10 UX improvements to the notebook editing, cell management, and related panel experiences in the hnb React frontend.  
> **Architecture:** All changes are primarily in the React frontend (`web/src/`). Issue #7 (drag-and-drop) also requires a new Go backend endpoint.  
> **Tech Stack:** React + TypeScript, inline styles (no Tailwind), Go `net/http` backend, PostgreSQL, `@dnd-kit` for drag-and-drop (new dependency).

---

## Implementation Order

| Phase | Issues | Rationale |
|-------|--------|-----------|
| **Phase 1** — Quick Wins | #1, #3, #4, #9 | Trivial/small changes, no backend, high confidence |
| **Phase 2** — Medium Effort | #2, #5, #6, #8, #10 | Frontend-only, moderate complexity |
| **Phase 3** — Largest | #7 | New dependency + backend endpoint + frontend integration |

---

# Phase 1: Quick Wins

---

## Task 1.1 — Issue #1: Schema Browser auto-detect notebook connector

**Goal:** When the notebook has a connector selected in the toolbar, the Schema Browser should use it as a fallback instead of showing "Select a connector."

**File:** `web/src/pages/NotebookPage.tsx`  
**Line:** ~497 (the `schemaConnectorId` computation)

### Current code (line ~497):
```typescript
const schemaConnectorId = localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id ?? null
```

### Change:
Replace the single line with a fallback chain that includes the notebook-level connector:

```typescript
const schemaConnectorId =
  localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id
  ?? (notebookConnectorId || null)
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Open a notebook, set a connector in the toolbar, open Schema Browser — it should load the schema without requiring any cell to have a connector assigned.

### Commit:
```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "fix: schema browser falls back to notebook-level connector"
```

---

## Task 1.2 — Issue #9: Connector selector text consistency

**Goal:** Standardize empty-option text across all connector selectors.

### File 1: `web/src/components/ConnectorSelector.tsx` (line ~38)

**Current code:**
```tsx
<option value="">
  {allowClear && value ? '— None —' : placeholder}
</option>
```

**Change to:**
```tsx
<option value="" disabled={!allowClear || !value}>
  {allowClear && value ? 'Clear selection' : placeholder}
</option>
```

### File 2: `web/src/components/Cell.tsx` (line ~266)

**Current code:**
```tsx
<option value="">— inherit from notebook —</option>
```

**Change to:**
```tsx
<option value="">Inherit from notebook</option>
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Open a notebook → cell connector dropdown should show "Inherit from notebook" (no em-dashes)
3. Toolbar connector selector should show "Clear selection" when a value is selected

### Commit:
```bash
git add web/src/components/ConnectorSelector.tsx web/src/components/Cell.tsx
git commit -m "fix: standardize connector selector empty-option text"
```

---

## Task 1.3 — Issue #3: Visual distinction between code and text cells

**Goal:** Add a colored left border accent to differentiate code cells (blue/accent) from text cells (green/success).

**File:** `web/src/components/Cell.tsx`

### Step 1: Add border-left to the cell container

**Current code (line ~247, the `<div id={'cell-' + cell.id} style={styles.cell} ...>`):**
```tsx
<div
  id={'cell-' + cell.id}
  style={styles.cell}
  onMouseEnter={() => setHovered(true)}
  onMouseLeave={() => setHovered(false)}
  onClick={() => onFocus?.(cell.id)}
>
```

**Change to:**
```tsx
<div
  id={'cell-' + cell.id}
  style={{
    ...styles.cell,
    borderLeft: isCode
      ? '3px solid var(--accent)'
      : '3px solid var(--success)',
  }}
  onMouseEnter={() => setHovered(true)}
  onMouseLeave={() => setHovered(false)}
  onClick={() => onFocus?.(cell.id)}
>
```

### Step 2: Differentiate meta bar background

**Current styles object (line ~407, `metaBar`):**
```typescript
metaBar: {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '6px 16px',
  gap: 8,
  minHeight: 32,
},
```

**Change to — remove the static `metaBar` style and compute it inline.** Find the meta bar `<div style={styles.metaBar}>` (line ~255) and change it to:

```tsx
<div style={{
  ...styles.metaBar,
  background: isCode ? 'var(--bg-cell-code)' : 'var(--bg-cell-text)',
}}>
```

### Step 3: Also update collapsed cell view

**Current collapsed view (line ~234):**
```tsx
<div
  id={'cell-' + cell.id}
  style={styles.collapsed}>
```

**Change to:**
```tsx
<div
  id={'cell-' + cell.id}
  style={{
    ...styles.collapsed,
    borderLeft: isCode
      ? '3px solid var(--accent)'
      : '3px solid var(--success)',
  }}>
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Open a notebook with both code and text cells — code cells should have a purple/blue left border, text cells should have a green left border
3. Check both light and dark themes
4. Collapsed cells should also show the colored border

### Commit:
```bash
git add web/src/components/Cell.tsx
git commit -m "feat: add colored left border to distinguish code vs text cells"
```

---

## Task 1.4 — Issue #4: Slide break button tooltip improvement

**Goal:** Improve the slide break button with a clearer tooltip and a visual indicator.

**File:** `web/src/components/Cell.tsx`

### Step 1: Enhance the tooltip text

**Current code (line ~334):**
```tsx
<button
  type="button"
  title={cell.slide_break ? 'Separate into own slide' : 'Join with previous slide'}
  style={{ ...styles.actionBtn, color: cell.slide_break ? 'var(--accent)' : 'var(--text-muted)' }}
  onClick={() => onUpdateCellMeta?.({ slide_break: !cell.slide_break })}
>
```

**Change to:**
```tsx
<button
  type="button"
  title={cell.slide_break
    ? 'Slide break: This cell starts a new slide in Present mode.\nClick to merge with the previous slide.'
    : 'No slide break: This cell continues the previous slide.\nClick to start a new slide here.'}
  aria-label={cell.slide_break ? 'Remove slide break' : 'Add slide break'}
  style={{ ...styles.actionBtn, color: cell.slide_break ? 'var(--accent)' : 'var(--text-muted)' }}
  onClick={() => onUpdateCellMeta?.({ slide_break: !cell.slide_break })}
>
```

### Step 2: Add slide break indicator above cells with `slide_break: true`

Find the normal (non-collapsed) cell render. **Before** the meta bar `<div>` (line ~255), add:

```tsx
{cell.slide_break && index !== undefined && index > 0 && (
  <div style={styles.slideBreakIndicator}>
    <span style={styles.slideBreakLabel}>— Slide break —</span>
  </div>
)}
```

### Step 3: Add the new styles

In the `styles` object at the bottom of `Cell.tsx`, add:

```typescript
slideBreakIndicator: {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  padding: '4px 0',
  margin: '-8px 0 -4px',
},
slideBreakLabel: {
  fontSize: 9,
  fontFamily: 'var(--font-mono)',
  color: 'var(--accent)',
  letterSpacing: '0.1em',
  textTransform: 'uppercase' as const,
  opacity: 0.7,
},
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Toggle slide break on a cell — should see "— Slide break —" indicator above it
3. Hover the button — should see the enhanced multi-line tooltip

### Commit:
```bash
git add web/src/components/Cell.tsx
git commit -m "feat: improve slide break button tooltip and add visual indicator"
```

---

# Phase 2: Medium Effort

---

## Task 2.1 — Issue #2: Smart cell title placeholder

**Goal:** Generate contextual placeholder text for the cell title input based on cell content.

**File:** `web/src/components/Cell.tsx`

### Step 1: Add the helper function

Add this function **before** the `Cell` component (around line ~130, after the `fmtTime` helper):

```typescript
function generateTitlePlaceholder(source: string, isCode: boolean): string {
  if (!source?.trim()) {
    return isCode ? 'e.g., Monthly active users' : 'e.g., Analysis summary'
  }
  const firstLine = source.trim().split('\n')[0].trim()
  if (!firstLine) {
    return isCode ? 'e.g., Monthly active users' : 'e.g., Analysis summary'
  }
  if (isCode) {
    if (firstLine.length <= 40) return firstLine
    return firstLine.slice(0, 37) + '…'
  } else {
    // Strip markdown heading markers
    const cleaned = firstLine.replace(/^#+\s*/, '').trim()
    if (cleaned.length > 0) return cleaned.slice(0, 40)
    return 'e.g., Analysis summary'
  }
}
```

### Step 2: Use the function in the title input

**Current code (line ~283):**
```tsx
<input
  style={styles.titleInput}
  value={cell.title ?? ''}
  onChange={(e) => onUpdateCellMeta?.({ title: e.target.value })}
  onClick={(e) => e.stopPropagation()}
  placeholder="Untitled"
/>
```

**Change to:**
```tsx
<input
  style={styles.titleInput}
  value={cell.title ?? ''}
  onChange={(e) => onUpdateCellMeta?.({ title: e.target.value })}
  onClick={(e) => e.stopPropagation()}
  placeholder={generateTitlePlaceholder(cell.source, isCode)}
/>
```

### Step 3: Update collapsed view fallback text

**Current code (line ~239):**
```tsx
<span style={styles.collapsedTitle}>
  {cell.title || (isCode ? 'Untitled query' : 'Untitled note')}
</span>
```

**Change to:**
```tsx
<span style={styles.collapsedTitle}>
  {cell.title || generateTitlePlaceholder(cell.source, isCode)}
</span>
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Create a code cell with `SELECT * FROM users` — placeholder should show `SELECT * FROM users`
3. Create a text cell with `## Summary` — placeholder should show `Summary`
4. Empty cells should show `e.g., Monthly active users` / `e.g., Analysis summary`

### Commit:
```bash
git add web/src/components/Cell.tsx
git commit -m "feat: smart cell title placeholder based on content"
```

---

## Task 2.2 — Issue #5: Parameters panel inline description

**Goal:** Add an always-visible description in the parameters manage panel explaining what parameters are.

**File:** `web/src/components/ParametersBar.tsx`

### Step 1: Add description in manage panel

**Current code (line ~47-53):**
```tsx
{managing && (
  <div style={styles.managePanel}>
    <span style={styles.manageTitle}>Parameters</span>
    <span
      style={styles.infoIcon}
      title={'Reference parameters in SQL using {{param_name}}\nExample: WHERE date >= {{start_date}}'}
    >
      <Info size={13} />
    </span>
    {draftParams.map((p, i) => (
```

**Change to:**
```tsx
{managing && (
  <div style={styles.managePanel}>
    <span style={styles.manageTitle}>Parameters</span>
    <span
      style={styles.infoIcon}
      title={'Reference parameters in SQL using {{param_name}}\nExample: WHERE date >= {{start_date}}'}
    >
      <Info size={13} />
    </span>
    <p style={styles.manageDescription}>
      Define variables referenced in SQL as <code style={{ fontFamily: 'var(--font-mono)', fontSize: 11, background: 'var(--bg-input)', padding: '1px 4px', borderRadius: 2 }}>{'{{param_name}}'}</code>.
      Useful for dates, filters, and thresholds you want to change without editing queries.
    </p>
    {draftParams.map((p, i) => (
```

### Step 2: Add empty-state hint in non-managing view

**Current code (line ~18-19, inside `!managing`):**
```tsx
{!managing && (
  <div style={styles.paramsList}>
    <span style={styles.infoIcon} ...>
      <Info size={13} />
    </span>
    {parameters.map((p) => (
```

**Change to — add an empty-state hint after the parameters list:**
```tsx
{!managing && (
  <div style={styles.paramsList}>
    <span style={styles.infoIcon} ...>
      <Info size={13} />
    </span>
    {parameters.length === 0 && (
      <span style={styles.emptyHint}>
        No parameters defined. Click ⚙ to add variables for your queries.
      </span>
    )}
    {parameters.map((p) => (
```

### Step 3: Add the new styles

In the `styles` object at the bottom of `ParametersBar.tsx`, add:

```typescript
manageDescription: {
  width: '100%',
  fontSize: 11,
  color: 'var(--text-muted)',
  margin: '0 0 4px',
  lineHeight: 1.5,
  fontFamily: 'var(--font-sans)',
},
emptyHint: {
  fontSize: 11,
  color: 'var(--text-muted)',
  fontStyle: 'italic',
},
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Open Parameters panel with no parameters → should see "No parameters defined. Click ⚙ to add variables for your queries."
3. Click ⚙ to manage → should see the description paragraph below "PARAMETERS" title

### Commit:
```bash
git add web/src/components/ParametersBar.tsx
git commit -m "feat: add inline description to parameters manage panel"
```

---

## Task 2.3 — Issue #6: Cron input helper with presets

**Goal:** Add a live cron description and quick presets below the cron input.

**File:** `web/src/components/SchedulesPanel.tsx`

### Step 1: Add the cron helper constants and function

Add these **before** the `SchedulesPanel` component (around line ~10):

```typescript
const CRON_PRESETS = [
  { label: 'Every hour', value: '0 * * * *' },
  { label: 'Daily 9am', value: '0 9 * * *' },
  { label: 'Weekdays 9am', value: '0 9 * * 1-5' },
  { label: 'Weekly Monday', value: '0 9 * * 1' },
  { label: 'Monthly 1st', value: '0 9 1 * *' },
]

function describeCron(expr: string): string {
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return '⚠ Invalid: expected 5 fields (min hour day month weekday)'
  const [min, hour, day, month, weekday] = parts
  if (min === '0' && hour === '9' && day === '*' && month === '*' && weekday === '1-5')
    return '✓ Runs at 9:00 AM on weekdays'
  if (min === '0' && hour === '9' && day === '*' && month === '*' && weekday === '1')
    return '✓ Runs at 9:00 AM every Monday'
  if (min === '0' && hour === '9' && day === '1' && month === '*')
    return '✓ Runs at 9:00 AM on the 1st of every month'
  if (min === '0' && hour === '*' && day === '*')
    return '✓ Runs at the top of every hour'
  if (min === '0' && hour !== '*' && day === '*' && month === '*' && weekday === '*')
    return `✓ Runs at ${hour.padStart(2, '0')}:${min.padStart(2, '0')} every day`
  return `✓ min=${min} hour=${hour} day=${day} month=${month} weekday=${weekday}`
}
```

### Step 2: Add the helper UI below the cron input

**Current code (line ~108-122):**
```tsx
<div style={styles.createForm}>
  <input
    style={styles.cronInput}
    type="text"
    placeholder="Cron expression (e.g. 0 9 * * 1)"
    value={cronDraft}
    onChange={(e) => {
      setCronDraft(e.target.value)
      setCreateError(null)
    }}
    onKeyDown={(e) => {
      if (e.key === 'Enter') handleCreate()
    }}
  />
  <button
    type="button"
    style={styles.createBtn}
    onClick={handleCreate}
    disabled={createSchedule.isPending}
  >
    {createSchedule.isPending ? 'Creating…' : 'Create'}
  </button>
</div>
{createError && <div style={styles.errorText}>{createError}</div>}
```

**Change to:**
```tsx
<div style={styles.createForm}>
  <input
    style={styles.cronInput}
    type="text"
    placeholder="Cron expression (e.g. 0 9 * * 1)"
    value={cronDraft}
    onChange={(e) => {
      setCronDraft(e.target.value)
      setCreateError(null)
    }}
    onKeyDown={(e) => {
      if (e.key === 'Enter') handleCreate()
    }}
  />
  <button
    type="button"
    style={styles.createBtn}
    onClick={handleCreate}
    disabled={createSchedule.isPending}
  >
    {createSchedule.isPending ? 'Creating…' : 'Create'}
  </button>
</div>
{cronDraft.trim() && (
  <div style={styles.cronHelper}>
    <span style={styles.cronPreviewText}>{describeCron(cronDraft.trim())}</span>
  </div>
)}
<div style={styles.cronPresets}>
  <span style={styles.presetLabel}>Quick:</span>
  {CRON_PRESETS.map((p) => (
    <button
      key={p.value}
      style={styles.presetBtn}
      onClick={() => setCronDraft(p.value)}
    >
      {p.label}
    </button>
  ))}
</div>
{createError && <div style={styles.errorText}>{createError}</div>}
```

### Step 3: Add the new styles

In the `styles` object at the bottom of `SchedulesPanel.tsx`, add:

```typescript
cronHelper: {
  padding: '4px 0',
},
cronPreviewText: {
  fontSize: 12,
  fontFamily: 'var(--font-mono)',
  color: 'var(--success)',
},
cronPresets: {
  display: 'flex',
  alignItems: 'center',
  gap: 6,
  flexWrap: 'wrap' as const,
},
presetLabel: {
  fontSize: 11,
  color: 'var(--text-muted)',
  fontFamily: 'var(--font-sans)',
},
presetBtn: {
  padding: '2px 8px',
  fontSize: 11,
  fontFamily: 'var(--font-mono)',
  color: 'var(--text-secondary)',
  background: 'var(--bg-primary)',
  border: '1px solid var(--border)',
  borderRadius: 3,
  cursor: 'pointer',
},
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Open Schedules panel → type `0 9 * * 1` → should see "✓ Runs at 9:00 AM every Monday"
3. Quick preset buttons should populate the input when clicked
4. Invalid input like `abc` should show "⚠ Invalid: expected 5 fields..."

### Commit:
```bash
git add web/src/components/SchedulesPanel.tsx
git commit -m "feat: add cron expression helper and quick presets to schedules panel"
```

---

## Task 2.4 — Issue #8: Live markdown preview toggle

**Goal:** Add a "Preview" toggle button to the markdown cell toolbar that renders the full cell source as markdown.

**File:** `web/src/components/MarkdownCell.tsx`

### Step 1: Add imports

At the top of the file, add `Eye` and `EyeOff` to the imports. Currently there are no lucide imports, so add:

```typescript
import { Eye, EyeOff } from 'lucide-react'
```

### Step 2: Add `showPreview` state to `MarkdownView`

Inside the `MarkdownView` component (line ~225), add after the existing state declarations:

```typescript
const [showPreview, setShowPreview] = useState(false)
```

### Step 3: Add the preview toggle button to the toolbar

**Current toolbar code (line ~293-308):**
```tsx
{focusedIdx !== null && (
  <div style={styles.mdToolbar}>
    <button
      style={styles.mdToolbarBtn}
      disabled={uploading}
      onMouseDown={e => e.preventDefault()}
      onClick={() => {
        setSelectedBlockIdx(focusedIdx)
        fileInputRef.current?.click()
      }}
      title="Upload image"
    >
      {uploading ? '...' : (
        <svg ...>
          ...
        </svg>
      )}
    </button>
  </div>
)}
```

**Change to:**
```tsx
{focusedIdx !== null && (
  <div style={styles.mdToolbar}>
    <button
      style={styles.mdToolbarBtn}
      disabled={uploading}
      onMouseDown={e => e.preventDefault()}
      onClick={() => {
        setSelectedBlockIdx(focusedIdx)
        fileInputRef.current?.click()
      }}
      title="Upload image"
    >
      {uploading ? '...' : (
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
          <circle cx="8.5" cy="8.5" r="1.5"/>
          <polyline points="21 15 16 10 5 21"/>
        </svg>
      )}
    </button>
    <button
      style={{
        ...styles.mdToolbarBtn,
        ...(showPreview ? styles.mdToolbarBtnActive : {}),
      }}
      onClick={() => setShowPreview(v => !v)}
      title="Toggle full preview"
    >
      {showPreview ? <EyeOff size={13} /> : <Eye size={13} />}
      {showPreview ? 'Edit' : 'Preview'}
    </button>
  </div>
)}
```

### Step 4: Add the preview panel

**After the toolbar block** and **before** the hidden file input, add:

```tsx
{showPreview && focusedIdx !== null && (
  <div style={styles.mdLivePreview}>
    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={markdownComponents}>
      {cell.source}
    </ReactMarkdown>
  </div>
)}
```

### Step 5: Add new styles

In the `styles` object at the bottom of `MarkdownCell.tsx`, add:

```typescript
mdToolbarBtnActive: {
  background: 'var(--accent-light)',
  borderColor: 'var(--accent)',
  color: 'var(--accent)',
},
mdLivePreview: {
  borderTop: '1px solid var(--border-light)',
  padding: '14px 20px',
  fontSize: 14,
  lineHeight: 1.75,
  color: 'var(--text-primary)',
  fontFamily: 'var(--font-sans)',
  background: 'var(--bg-cell-text)',
  maxHeight: 400,
  overflowY: 'auto' as const,
},
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Edit a markdown cell → toolbar should show "Preview" button
3. Click "Preview" → full rendered markdown appears below the toolbar
4. Click "Edit" (button label changes) → preview hides

### Commit:
```bash
git add web/src/components/MarkdownCell.tsx
git commit -m "feat: add live markdown preview toggle to markdown cell toolbar"
```

---

## Task 2.5 — Issue #10: Notebook description markdown support

**Goal:** Render the notebook description as markdown when not being edited; keep an input for editing.

**File:** `web/src/pages/NotebookPage.tsx`

### Step 1: Add imports

At the top of the file, add:

```typescript
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeRaw from 'rehype-raw'
```

### Step 2: Add `editingDesc` state

After the existing `descDraft` state (line ~88), add:

```typescript
const [editingDesc, setEditingDesc] = useState(false)
```

### Step 3: Replace the description input with click-to-edit pattern

**Current code (line ~540-551):**
```tsx
<input
  style={styles.descInput}
  value={descDraft}
  onChange={(e) => setDescDraft(e.target.value)}
  onBlur={() => {
    if (descDraft !== (notebook?.description ?? '')) {
      updateNotebook.mutate({ description: descDraft })
    }
  }}
  placeholder="Add a description for this notebook…"
/>
```

**Change to:**
```tsx
{editingDesc ? (
  <input
    style={styles.descInput}
    value={descDraft}
    onChange={(e) => setDescDraft(e.target.value)}
    onBlur={() => {
      setEditingDesc(false)
      if (descDraft !== (notebook?.description ?? '')) {
        updateNotebook.mutate({ description: descDraft })
      }
    }}
    onKeyDown={(e) => {
      if (e.key === 'Enter' || e.key === 'Escape') (e.target as HTMLInputElement).blur()
    }}
    autoFocus
  />
) : (
  <div
    style={styles.descRendered}
    onClick={() => { setDescDraft(notebook.description ?? ''); setEditingDesc(true) }}
    title="Click to edit description"
  >
    {notebook.description ? (
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
        {notebook.description}
      </ReactMarkdown>
    ) : (
      <span style={styles.descPlaceholder}>Add a description for this notebook…</span>
    )}
  </div>
)}
```

### Step 4: Add new styles

In the `styles` object at the bottom of `NotebookPage.tsx`, add:

```typescript
descRendered: {
  fontSize: 14,
  color: 'var(--text-muted)',
  fontFamily: 'var(--font-sans)',
  cursor: 'pointer',
  lineHeight: 1.6,
  padding: '2px 0',
  minHeight: 24,
},
descPlaceholder: {
  color: 'var(--text-muted)',
  opacity: 0.6,
  fontStyle: 'italic',
},
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. Set a notebook description with markdown: `**Bold** and [link](https://example.com)`
3. On blur, it should render as formatted markdown
4. Click to edit → returns to input mode
5. Empty description shows italic placeholder text

### Commit:
```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: render notebook description as markdown with click-to-edit"
```

---

# Phase 3: Drag-and-Drop Cell Reordering

---

## Task 3.1 — Backend: Add cell reorder endpoint

**Goal:** Create a `PUT /api/v1/notebooks/:id/cells/reorder` endpoint that accepts an ordered list of cell IDs and updates their `position` values.

### Step 1: Add the request type and handler

**File:** `internal/api/cell_handlers.go`

Add the request struct at the top of the file (after `updateCellRequest`):

```go
type reorderCellsRequest struct {
	CellIDs []string `json:"cell_ids"`
}
```

Add the handler function at the end of the file (before the `nilIfEmptyStr` helper):

```go
func (s *Server) handleReorderCells(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	if allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "edit"); err != nil || !allowed {
		writeError(w, http.StatusForbidden, "no permission to edit cells in this notebook")
		return
	}

	var req reorderCellsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.CellIDs) == 0 {
		writeError(w, http.StatusBadRequest, "cell_ids must not be empty")
		return
	}

	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	// Verify all cell IDs belong to this notebook
	var count int
	err := s.db.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM cells WHERE notebook_id=$1 AND id = ANY($2)",
		nbID, req.CellIDs,
	).Scan(&count)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if count != len(req.CellIDs) {
		writeError(w, http.StatusBadRequest, "some cell IDs are invalid or do not belong to this notebook")
		return
	}

	// Update positions in a transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(ctx)

	for i, cellID := range req.CellIDs {
		_, err := tx.Exec(ctx,
			"UPDATE cells SET position=$1, updated_at=NOW() WHERE id=$2 AND notebook_id=$3",
			i, cellID, nbID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update cell order")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "cell.reorder", ResourceType: "notebook", ResourceID: nbID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

### Step 2: Register the route

**File:** `internal/api/router.go` (line ~136, after the duplicate cell route)

Add this line after the `POST .../cells/{cell_id}/duplicate` route:

```go
s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/reorder", authMW(RequireRole("editor")(http.HandlerFunc(s.handleReorderCells))))
```

**IMPORTANT:** This route must be registered **before** the `PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}` route, otherwise Go's router will match `reorder` as a `{cell_id}` parameter. Check the current order:

```
Line 134: PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}
```

Move the reorder route **above** line 134:

```go
// Cell routes — reorder must come before {cell_id} to avoid route conflict
s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/reorder", authMW(RequireRole("editor")(http.HandlerFunc(s.handleReorderCells))))
s.mux.Handle("PUT /api/v1/notebooks/{notebook_id}/cells/{cell_id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateCell))))
```

### Step 3: Write the test

**File:** `internal/api/cell_handlers_test.go`

Add this test at the end of the file:

```go
func TestReorderCells(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("reorder-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Reorder Org")
	nbID := createNotebook(t, srv, token, "Reorder NB")

	// Create 3 cells
	cell1 := createCell(t, srv, token, nbID, "sql", "SELECT 1", "")
	cell2 := createCell(t, srv, token, nbID, "sql", "SELECT 2", "")
	cell3 := createCell(t, srv, token, nbID, "sql", "SELECT 3", "")

	// Reorder: [cell3, cell1, cell2]
	body, _ := json.Marshal(map[string]interface{}{
		"cell_ids": []string{cell3, cell1, cell2},
	})
	req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/reorder", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify positions via DB
	rows, err := srv.DB().Pool.Query(context.Background(),
		"SELECT id, position FROM cells WHERE notebook_id=$1 ORDER BY position", nbID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}

	if len(ids) != 3 {
		t.Fatalf("expected 3 cells, got %d", len(ids))
	}
	if ids[0] != cell3 || ids[1] != cell1 || ids[2] != cell2 {
		t.Fatalf("expected order [cell3,cell1,cell2], got %v", ids)
	}
}
```

### Step 4: Run the test

```bash
cd /tmp/pi-worktree-d9bd8b3d-2
task test:api -- -run TestReorderCells -v
```

**Expected output:**
```
=== RUN   TestReorderCells
--- PASS: TestReorderCells (X.XXs)
PASS
```

### Commit:
```bash
git add internal/api/cell_handlers.go internal/api/router.go internal/api/cell_handlers_test.go
git commit -m "feat: add PUT /cells/reorder endpoint for drag-and-drop cell ordering"
```

---

## Task 3.2 — Frontend: Install @dnd-kit and create SortableCell wrapper

**Goal:** Install the drag-and-drop library and create the sortable wrapper component.

### Step 1: Install dependencies

```bash
cd web && npm install @dnd-kit/core @dnd-kit/sortable @dnd-kit/utilities
```

### Step 2: Create the SortableCell component

**New file:** `web/src/components/SortableCell.tsx`

```typescript
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical } from 'lucide-react'
import type { ReactNode } from 'react'

interface SortableCellProps {
  id: string
  children: ReactNode
  dragHandleProps?: Record<string, unknown>
}

export function SortableCell({ id, children }: SortableCellProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id })

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    zIndex: isDragging ? 100 : undefined,
    position: 'relative',
  }

  return (
    <div ref={setNodeRef} style={style} {...attributes}>
      {children}
    </div>
  )
}

export function DragHandle({ listeners }: { listeners?: Record<string, unknown> }) {
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        cursor: 'grab',
        color: 'var(--text-muted)',
        opacity: 0.5,
        padding: '0 2px',
        flexShrink: 0,
      }}
      title="Drag to reorder"
      {...listeners}
    >
      <GripVertical size={12} />
    </span>
  )
}
```

### Step 3: Add GripVertical import to Cell.tsx

**File:** `web/src/components/Cell.tsx` (line ~1)

**Current import:**
```typescript
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff, ChevronRight, Clock, X, SeparatorHorizontal, Copy, Link, Check, LayoutDashboard } from 'lucide-react'
```

**Change to:**
```typescript
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff, ChevronRight, Clock, X, SeparatorHorizontal, Copy, Link, Check, LayoutDashboard, GripVertical } from 'lucide-react'
```

### Step 4: Add drag handle prop to Cell component

In the `Props` interface of `Cell.tsx` (around line ~95), add:

```typescript
dragHandleProps?: Record<string, unknown>
```

In the `Cell` function parameters (around line ~115), add `dragHandleProps` to the destructured props.

In the meta bar's `metaLeft` section (line ~258, after the cell number), add the drag handle:

```tsx
{dragHandleProps && (
  <span
    style={styles.dragHandle}
    title="Drag to reorder"
    {...dragHandleProps}
  >
    <GripVertical size={12} />
  </span>
)}
```

Add the style to the styles object:

```typescript
dragHandle: {
  display: 'inline-flex',
  alignItems: 'center',
  cursor: 'grab',
  color: 'var(--text-muted)',
  opacity: 0.4,
  padding: '0 2px',
  flexShrink: 0,
  userSelect: 'none',
},
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. The drag handle should appear as a grip icon at the start of each cell's meta bar

### Commit:
```bash
git add web/src/components/SortableCell.tsx web/src/components/Cell.tsx web/package.json web/package-lock.json
git commit -m "feat: add SortableCell wrapper and drag handle in Cell meta bar"
```

---

## Task 3.3 — Frontend: Wire up DnD context in NotebookPage

**Goal:** Wrap the cells area in DndContext + SortableContext and persist order on drop.

**File:** `web/src/pages/NotebookPage.tsx`

### Step 1: Add imports

At the top of the file, add:

```typescript
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import type { DragEndEvent } from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { SortableCell } from '../components/SortableCell'
```

### Step 2: Add the reorder mutation

Inside the `NotebookPage` component, add this mutation (after the `moveCell` callback, around line ~494):

```typescript
const reorderCells = useMutation({
  mutationFn: (cellIds: string[]) =>
    api.put(`/api/v1/notebooks/${id}/cells/reorder`, { cell_ids: cellIds }),
  onError: (err: Error) => setMutationError(err.message),
})
```

### Step 3: Add the drag end handler

After the `reorderCells` mutation:

```typescript
const sensors = useSensors(
  useSensor(PointerSensor, { activationConstraint: { distance: 8 } })
)

function handleDragEnd(event: DragEndEvent) {
  const { active, over } = event
  if (!over || active.id === over.id) return

  setLocalCells((prev) => {
    const oldIndex = prev.findIndex((c) => c.id === active.id)
    const newIndex = prev.findIndex((c) => c.id === over.id)
    if (oldIndex < 0 || newIndex < 0) return prev

    const next = [...prev]
    const [moved] = next.splice(oldIndex, 1)
    next.splice(newIndex, 0, moved)

    // Persist the new order
    reorderCells.mutate(next.map((c) => c.id))

    return next
  })
}
```

### Step 4: Update `moveCell` to persist order

**Current code (line ~484):**
```typescript
const moveCell = useCallback((cellId: string, dir: -1 | 1) => {
  setLocalCells((prev) => {
    const idx = prev.findIndex((c) => c.id === cellId)
    if (idx < 0) return prev
    const next = [...prev]
    const swap = idx + dir
    if (swap < 0 || swap >= next.length) return prev
    ;[next[idx], next[swap]] = [next[swap], next[idx]]
    return next
  })
}, [])
```

**Change to:**
```typescript
const moveCell = useCallback((cellId: string, dir: -1 | 1) => {
  setLocalCells((prev) => {
    const idx = prev.findIndex((c) => c.id === cellId)
    if (idx < 0) return prev
    const next = [...prev]
    const swap = idx + dir
    if (swap < 0 || swap >= next.length) return prev
    ;[next[idx], next[swap]] = [next[swap], next[idx]]
    // Persist the new order
    reorderCells.mutate(next.map((c) => c.id))
    return next
  })
}, [reorderCells])
```

### Step 5: Wrap cells in DndContext + SortableContext

**Current code (line ~660-703, the cells rendering area):**
```tsx
<div style={styles.cells}>
  {localCells.map((cell, i) => (
    <div key={cell.id}>
      {/* ... cell params ... */}
      <NotebookCell
        cell={cell}
        ...
      />
      <AddCellBar ... />
    </div>
  ))}
  {/* ... add row ... */}
</div>
```

**Change to:**
```tsx
<DndContext
  sensors={sensors}
  collisionDetection={closestCenter}
  onDragEnd={handleDragEnd}
>
  <SortableContext
    items={localCells.map((c) => c.id)}
    strategy={verticalListSortingStrategy}
  >
    <div style={styles.cells}>
      {localCells.map((cell, i) => (
        <SortableCell key={cell.id} id={cell.id}>
          <div>
            {/* cell params (unchanged) */}
            {cell.type === 'code' && cell.parameters && cell.parameters.length > 0 && (
              <div style={styles.cellParams}>
                {/* ... existing params code ... */}
              </div>
            )}
            <NotebookCell
              cell={cell}
              connectors={connectors}
              notebookId={id!}
              onRun={permissions?.can_run ? saveAndRun : () => {}}
              onDelete={readOnly ? () => {} : (cid) => deleteCell.mutate(cid)}
              onSourceChange={readOnly ? () => {} : updateSource}
              onSave={readOnly ? undefined : saveCellSource}
              onAssignConnector={readOnly ? () => {} : assignConnector}
              onClearConnector={readOnly ? undefined : clearCellConnector}
              onMoveUp={readOnly || i === 0 ? undefined : () => moveCell(cell.id, -1)}
              onMoveDown={readOnly || i === localCells.length - 1 ? undefined : () => moveCell(cell.id, 1)}
              onSwitchType={readOnly ? undefined : () => switchCellType(cell.id)}
              onDuplicate={readOnly ? undefined : () => duplicateCell.mutate(cell.id)}
              running={runningCells.has(cell.id)}
              saveState={cellSaveState[cell.id]}
              runAt={cellRunAt[cell.id]}
              onUpdateCellMeta={readOnly ? undefined : (updates) => updateCellMeta(cell.id, updates)}
              onShowHistory={readOnly ? undefined : () => fetchHistory(cell.id)}
              onFocus={(cid) => setFocusedCellId(cid)}
              onAddToDashboard={readOnly ? undefined : (cid) => setAddToDashboardCellId(cid)}
              index={i}
            />
            <AddCellBar
              onAddCode={readOnly ? () => {} : () => createCell.mutate({ type: 'code', position: cell.position + 1 })}
              onAddText={readOnly ? () => {} : () => createCell.mutate({ type: 'text', position: cell.position + 1 })}
            />
          </div>
        </SortableCell>
      ))}

      <div style={styles.addRow}>
        <button type="button" style={styles.addBtn} onClick={readOnly ? () => {} : () => createCell.mutate({ type: 'code' })} disabled={readOnly}>
          + Code Cell
        </button>
        <button type="button" style={styles.addBtn} onClick={readOnly ? () => {} : () => createCell.mutate({ type: 'text' })} disabled={readOnly}>
          + Text Cell
        </button>
      </div>
      <div ref={cellsEndRef} />
    </div>
  </SortableContext>
</DndContext>
```

### Step 6: Pass drag handle listeners to Cell

The `SortableCell` component uses `useSortable` which provides `listeners`. We need to pass these down to the Cell's drag handle. Update `SortableCell.tsx` to pass `listeners` as `dragHandleProps`:

**Updated `SortableCell.tsx`:**
```typescript
import { useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import type { ReactNode } from 'react'

interface SortableCellProps {
  id: string
  children: (dragHandleProps: Record<string, unknown>) => ReactNode
}

export function SortableCell({ id, children }: SortableCellProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id })

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    zIndex: isDragging ? 100 : undefined,
    position: 'relative',
  }

  return (
    <div ref={setNodeRef} style={style} {...attributes}>
      {children(listeners ?? {})}
    </div>
  )
}
```

Then in `NotebookPage.tsx`, update the usage:

```tsx
<SortableCell key={cell.id} id={cell.id}>
  {(dragHandleProps) => (
    <div>
      {/* ... cell params ... */}
      <NotebookCell
        cell={cell}
        connectors={connectors}
        notebookId={id!}
        dragHandleProps={dragHandleProps}
        // ... rest of props unchanged ...
      />
      <AddCellBar ... />
    </div>
  )}
</SortableCell>
```

### Verification:
1. `cd web && npx tsc --noEmit` — must pass
2. `task test:api -- -run TestReorderCells -v` — must pass
3. Start the dev server, open a notebook with multiple cells
4. Drag a cell by its grip handle — it should reorder with animation
5. Refresh the page — the new order should be persisted
6. Up/down chevron buttons should also persist order now

### Commit:
```bash
git add web/src/pages/NotebookPage.tsx web/src/components/SortableCell.tsx
git commit -m "feat: drag-and-drop cell reordering with @dnd-kit"
```

---

## Task 3.4 — Final integration test and cleanup

### Step 1: Run full type check

```bash
cd web && npx tsc --noEmit
```

**Expected:** No errors.

### Step 2: Run full Go test suite

```bash
task test:api
```

**Expected:** All tests pass.

### Step 3: Visual smoke test

Open a notebook and verify all 10 improvements:

1. ✅ Schema Browser uses notebook connector when no cell has one
2. ✅ Cell title placeholder shows content-derived text
3. ✅ Code cells have accent left border, text cells have green left border
4. ✅ Slide break button has descriptive tooltip + visual indicator
5. ✅ Parameters panel shows description in manage mode
6. ✅ Cron input shows live description + quick presets
7. ✅ Cells can be drag-and-dropped to reorder
8. ✅ Markdown cells have a Preview toggle button
9. ✅ Connector selectors use consistent text (no em-dashes)
10. ✅ Notebook description renders as markdown

### Step 4: Final commit

```bash
git add -A
git commit -m "chore: group 3 notebook experience improvements - complete"
```

---

## Summary of All File Changes

| File | Issues | Changes |
|------|--------|---------|
| `web/src/pages/NotebookPage.tsx` | #1, #10, #7 | Schema connector fallback, markdown description, DnD context |
| `web/src/components/Cell.tsx` | #2, #3, #4, #7, #9 | Smart placeholder, left border, slide indicator, drag handle, connector text |
| `web/src/components/ConnectorSelector.tsx` | #9 | "Clear selection" text |
| `web/src/components/ParametersBar.tsx` | #5 | Inline description + empty hint |
| `web/src/components/SchedulesPanel.tsx` | #6 | Cron helper + presets |
| `web/src/components/MarkdownCell.tsx` | #8 | Preview toggle button + panel |
| `web/src/components/SortableCell.tsx` | #7 | **NEW** — DnD sortable wrapper |
| `internal/api/cell_handlers.go` | #7 | `handleReorderCells` handler |
| `internal/api/router.go` | #7 | New route registration |
| `internal/api/cell_handlers_test.go` | #7 | `TestReorderCells` test |

## New Dependencies

| Package | Size (gzipped) | Purpose |
|---------|----------------|---------|
| `@dnd-kit/core` | ~8KB | Drag-and-drop engine |
| `@dnd-kit/sortable` | ~5KB | Sortable list utilities |
| `@dnd-kit/utilities` | ~2KB | CSS transform helpers |
