# Group 3: Notebook & Cell Experience — UX Design Solutions

> **Author:** Senior UX Engineer  
> **Date:** 2026-06-04  
> **Scope:** 10 issues affecting the notebook editing, cell management, and related panel experiences in the hnb React frontend.

---

## Table of Contents

1. [Schema Browser doesn't auto-detect notebook connector](#1-schema-browser-doesnt-auto-detect-notebook-connector)
2. [Cell title "Untitled" not auto-populated](#2-cell-title-untitled-not-auto-populated)
3. [No visual distinction between code and text cells](#3-no-visual-distinction-between-code-and-text-cells)
4. ["Join with previous slide" button has no explanation](#4-join-with-previous-slide-button-has-no-explanation)
5. [Parameters panel has no description](#5-parameters-panel-has-no-description)
6. [Schedule panel cron input has no helper](#6-schedule-panel-cron-input-has-no-helper)
7. [No drag-and-drop reordering of cells](#7-no-drag-and-drop-reordering-of-cells)
8. [Text cell editor has no live markdown preview](#8-text-cell-editor-has-no-live-markdown-preview)
9. [Connector selector shows "— None —" inconsistently](#9-connector-selector-shows--none--inconsistently)
10. [Notebook description doesn't support markdown](#10-notebook-description-doesnt-support-markdown)

---

## 1. Schema Browser doesn't auto-detect notebook connector

### Current Implementation

**File:** `web/src/pages/NotebookPage.tsx` (line ~497)  
**File:** `web/src/components/SchemaBrowser.tsx`

The schema connector ID is derived from the **first code cell** that has a connector assigned:

```typescript
const schemaConnectorId = localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id ?? null
```

This is then passed to `<SchemaBrowser connectorId={schemaConnectorId} />`.

**Problem:** The schema browser ignores the **notebook-level connector** (`notebookConnectorId`). If no individual cell has a connector assigned yet (common in new notebooks), the schema browser shows "Select a connector to browse its schema" even though the notebook already has a connector selected in the toolbar.

### Proposed Fix

**Priority:** High | **Effort:** Trivial (1-line change)

Change the fallback logic in `NotebookPage.tsx` to prefer the notebook-level connector:

```typescript
const schemaConnectorId = 
  // First: any cell-level connector
  localCells.find((c) => c.type === 'code' && c.connector_id)?.connector_id 
  // Fallback: notebook-level connector
  ?? notebookConnectorId 
  ?? null
```

This is a single-line change. The `notebookConnectorId` state is already available in scope.

### Dependencies
- None. `notebookConnectorId` is already tracked in `NotebookPage` state.

---

## 2. Cell title "Untitled" not auto-populated

### Current Implementation

**File:** `web/src/components/Cell.tsx` (lines ~280-287)

The title input shows a static `placeholder="Untitled"` and the value is `cell.title ?? ''`. There is no prompt or nudge to name cells. The collapsed view shows `"Untitled query"` / `"Untitled note"` as fallback text.

**Problem:** Users rarely name their cells, making navigation in large notebooks difficult. The placeholder is generic and doesn't guide behavior.

### Proposed Fix

**Priority:** Medium | **Effort:** Small

**Approach A — Smart placeholder (simplest):**  
Generate a contextual placeholder based on cell content:
- For code cells: extract the first SQL keyword + table name (e.g., `"SELECT from users…"`) or show `"e.g., Monthly active users"`
- For text cells: show `"e.g., ## Analysis Summary"` or the first few words of source

```typescript
function generateTitlePlaceholder(cell: Cell, isCode: boolean): string {
  if (cell.source?.trim()) {
    if (isCode) {
      // Extract first meaningful line
      const firstLine = cell.source.trim().split('\n')[0].trim()
      if (firstLine.length > 0 && firstLine.length < 40) return firstLine
      return firstLine.slice(0, 37) + '…'
    } else {
      // First line of markdown, strip heading markers
      const firstLine = cell.source.trim().split('\n')[0].replace(/^#+\s*/, '').trim()
      if (firstLine.length > 0) return firstLine.slice(0, 40)
    }
  }
  return isCode ? 'e.g., Monthly active users' : 'e.g., Analysis summary'
}
```

**Approach B — Auto-name on first run (optional enhancement):**  
After a cell runs successfully for the first time and has no title, auto-populate with a generated name (e.g., from the SQL query). This could be done server-side or client-side.

**Recommendation:** Start with Approach A (smart placeholder). It's zero-backend-effort and immediately useful.

### Dependencies
- None. Purely client-side change in `Cell.tsx`.

---

## 3. No visual distinction between code and text cells

### Current Implementation

**File:** `web/src/components/Cell.tsx` (styles object, lines ~400+)

Both code and text cells use the same `styles.cell` container:
```typescript
cell: {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 4,
  overflow: 'hidden',
}
```

The only distinction is the `SQL` / `MD` tag in the meta bar (9px, muted color). The cell container, border, and background are identical.

**Problem:** In notebooks with many cells, it's hard to visually scan and distinguish code from text cells at a glance.

### Proposed Fix

**Priority:** High | **Effort:** Small

Add a **left border accent** that differs by cell type — a common pattern in notebook UIs (Jupyter, Observable):

```typescript
// In the Cell component, compute border style:
const cellBorderStyle: React.CSSProperties = isCode
  ? { borderLeft: '3px solid var(--accent)' }      // Blue left border for code
  : { borderLeft: '3px solid var(--success)' }      // Green left border for text

// Apply to cell container:
<div style={{ ...styles.cell, ...cellBorderStyle }}>
```

Additionally, differentiate the meta bar background subtly:
```typescript
metaBar: {
  // ... existing styles
  background: isCode ? 'var(--bg-cell-code)' : 'var(--bg-cell-text)',
}
```

**Alternative:** Use a top border instead of left border if left-border conflicts with the collapsed/expand visual language.

### Dependencies
- CSS variables `--accent` and `--success` must exist in the theme (they already do, used elsewhere).
- `--bg-cell-code` and `--bg-cell-text` are already referenced in `MarkdownCell.tsx`.

---

## 4. "Join with previous slide" button has no explanation

### Current Implementation

**File:** `web/src/components/Cell.tsx` (lines ~330-338)

```tsx
<button
  type="button"
  title={cell.slide_break ? 'Separate into own slide' : 'Join with previous slide'}
  style={{ ...styles.actionBtn, color: cell.slide_break ? 'var(--accent)' : 'var(--text-muted)' }}
  onClick={() => onUpdateCellMeta?.({ slide_break: !cell.slide_break })}
>
  {cell.slide_break ? <Link size={13} /> : <SeparatorHorizontal size={13} />}
</button>
```

The button has a `title` attribute, but:
1. The concept of "slides" is never explained to the user
2. The icons (`Link` / `SeparatorHorizontal`) are abstract
3. No tooltip with context about what presentation mode is

**Problem:** Users don't understand what slides are, how they relate to "Present" mode, or what this toggle does.

### Proposed Fix

**Priority:** Medium | **Effort:** Small

**Enhance the tooltip** with a multi-line explanation:

```tsx
title={cell.slide_break 
  ? 'Slide break: This cell starts a new slide in Present mode.\nClick to merge with the previous slide.' 
  : 'No slide break: This cell continues the previous slide.\nClick to start a new slide here.'}
```

**Add a visual indicator** — when a cell has `slide_break: true`, show a subtle horizontal rule or label above the cell:

```tsx
{cell.slide_break && (
  <div style={styles.slideBreakIndicator}>
    <span style={styles.slideBreakLabel}>— Slide break —</span>
  </div>
)}
```

```typescript
slideBreakIndicator: {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  padding: '4px 0',
  margin: '-6px 0 0',
},
slideBreakLabel: {
  fontSize: 9,
  fontFamily: 'var(--font-mono)',
  color: 'var(--accent)',
  letterSpacing: '0.1em',
  textTransform: 'uppercase',
  opacity: 0.7,
}
```

### Dependencies
- None. Purely presentational.

---

## 5. Parameters panel has no description

### Current Implementation

**File:** `web/src/components/ParametersBar.tsx`

The parameters bar shows an `Info` icon with a tooltip:
```tsx
<span style={styles.infoIcon} title={'Reference parameters in SQL using {{param_name}}\nExample: WHERE date >= {{start_date}}'}>
  <Info size={13} />
</span>
```

**Problem:** 
1. The info tooltip only appears on hover of a small icon — easily missed
2. When the panel is in "manage" mode (editing parameter definitions), there's no explanation of what parameters are for
3. New users have no context for why they'd create parameters

### Proposed Fix

**Priority:** Medium | **Effort:** Small

**Add an inline description** in the manage panel that's always visible:

```tsx
{managing && (
  <div style={styles.managePanel}>
    <span style={styles.manageTitle}>Parameters</span>
    <span style={styles.infoIcon} title={...}>
      <Info size={13} />
    </span>
    {/* NEW: Inline description */}
    <p style={styles.manageDescription}>
      Define variables that can be referenced in any SQL cell using <code>{'{{param_name}}'}</code>. 
      Useful for dates, filters, and thresholds you want to change without editing queries.
    </p>
    {/* ... rest of manage panel */}
  </div>
)}
```

```typescript
manageDescription: {
  width: '100%',
  fontSize: 11,
  color: 'var(--text-muted)',
  margin: '0 0 4px',
  lineHeight: 1.5,
  fontFamily: 'var(--font-sans)',
},
```

Also improve the non-managing view — make the Info icon tooltip more prominent by adding a short text label:

```tsx
{!managing && parameters.length === 0 && (
  <span style={styles.emptyHint}>
    No parameters defined. Click ⚙ to add variables for your queries.
  </span>
)}
```

### Dependencies
- None. Purely presentational.

---

## 6. Schedule panel cron input has no helper

### Current Implementation

**File:** `web/src/components/SchedulesPanel.tsx` (lines ~105-120)

The cron input is a plain text field:
```tsx
<input
  style={styles.cronInput}
  type="text"
  placeholder="Cron expression (e.g. 0 9 * * 1)"
  value={cronDraft}
  onChange={(e) => { setCronDraft(e.target.value); setCreateError(null) }}
  onKeyDown={(e) => { if (e.key === 'Enter') handleCreate() }}
/>
```

**Problem:** Users unfamiliar with cron syntax have no reference, no validation feedback, and no preview of what the schedule means in human-readable terms.

### Proposed Fix

**Priority:** Medium | **Effort:** Medium

**Add a live cron description** below the input that parses the expression in real-time:

```tsx
<div style={styles.createForm}>
  <input ... />
  <button ...>Create</button>
</div>
{/* NEW: Cron helper */}
{cronDraft.trim() && (
  <div style={styles.cronHelper}>
    <span style={styles.cronPreview}>
      {describeCron(cronDraft.trim())}
    </span>
  </div>
)}
{/* NEW: Quick presets */}
<div style={styles.cronPresets}>
  <span style={styles.presetLabel}>Quick:</span>
  {CRON_PRESETS.map(p => (
    <button key={p.value} style={styles.presetBtn} onClick={() => setCronDraft(p.value)}>
      {p.label}
    </button>
  ))}
</div>
```

**Cron description function** (client-side, no dependency needed):

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
  // Simple human-readable conversion for common patterns
  if (min === '0' && hour === '9' && day === '*' && month === '*' && weekday === '1-5')
    return '✓ Runs at 9:00 AM on weekdays'
  if (min === '0' && hour === '9' && day === '*' && month === '*' && weekday === '1')
    return '✓ Runs at 9:00 AM every Monday'
  if (min === '0' && hour === '9' && day === '1' && month === '*')
    return '✓ Runs at 9:00 AM on the 1st of every month'
  if (min === '0' && hour === '*' && day === '*')
    return '✓ Runs at the top of every hour'
  // Generic fallback
  return `✓ min=${min} hour=${hour} day=${day} month=${month} weekday=${weekday}`
}
```

**Optional enhancement:** Add a link to a cron reference: `<a href="https://crontab.guru" target="_blank" rel="noopener">Cron reference ↗</a>`

### Dependencies
- None. Purely client-side. Could optionally add `cronstrue` npm package for robust parsing, but the simple function above covers 90% of use cases without a dependency.

---

## 7. No drag-and-drop reordering of cells

### Current Implementation

**File:** `web/src/components/Cell.tsx` (toolbar buttons)  
**File:** `web/src/pages/NotebookPage.tsx` (moveCell function)

Cells can currently only be reordered via up/down chevron buttons in the hover toolbar:
```tsx
{onMoveUp && <button style={styles.actionBtn} onClick={onMoveUp}><ChevronUp size={11} /></button>}
{onMoveDown && <button style={styles.actionBtn} onClick={onMoveDown}><ChevronDown size={11} /></button>}
```

The `moveCell` function in `NotebookPage` does a local array swap but **does not persist** the new order to the backend.

**Problem:** Drag-and-drop is the expected interaction pattern for reordering items in a list. Click-based up/down is slow and cumbersome for large notebooks.

### Proposed Fix

**Priority:** High | **Effort:** Medium

**Recommended library:** `@dnd-kit/core` + `@dnd-kit/sortable` — lightweight (~15KB gzipped), accessible, and React-native.

**Implementation plan:**

1. **Wrap cells area in `DndContext` + `SortableContext`:**
```tsx
// NotebookPage.tsx
import { DndContext, closestCenter, PointerSensor, useSensor, useSensors } from '@dnd-kit/core'
import { SortableContext, verticalListSortingStrategy, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'

function SortableCellWrapper({ cell, children }: { cell: Cell; children: React.ReactNode }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: cell.id })
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
    zIndex: isDragging ? 100 : undefined,
  }
  return (
    <div ref={setNodeRef} style={style} {...attributes} {...listeners}>
      {children}
    </div>
  )
}
```

2. **Add drag handle** — rather than making the whole cell draggable (which conflicts with text selection and CodeMirror), add a dedicated drag handle in the meta bar:

```tsx
// In Cell.tsx meta bar, add at the start of metaLeft:
<span 
  style={styles.dragHandle} 
  title="Drag to reorder"
  {...dragHandleProps}
>
  <GripVertical size={12} />
</span>
```

3. **Persist order on drop** — call `PUT /api/v1/notebooks/:id/cells/reorder` with the new cell ID order. This requires a new backend endpoint.

4. **Add a backend endpoint** for persisting cell order:
```
PUT /api/v1/notebooks/:id/cells/reorder
Body: { cell_ids: ["id1", "id2", "id3", ...] }
```

### Dependencies
- **New npm package:** `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities`
- **New backend endpoint:** `PUT /api/v1/notebooks/:id/cells/reorder` (or update existing cell PUT to accept `position`)
- **Existing `moveCell` function** needs to be extended to persist order

---

## 8. Text cell editor has no live markdown preview

### Current Implementation

**File:** `web/src/components/MarkdownCell.tsx`

The current implementation uses a **block-based WYSIWYG** approach:
- Content is split into blocks (separated by blank lines)
- Each block toggles between a `<textarea>` (edit mode) and rendered `<ReactMarkdown>` (preview mode)
- Clicking a block enters edit mode; blurring renders it

**Problem:** While the block-based approach is clever, users cannot see the full rendered document while editing. They must blur each block individually to see the rendered output. There's no "split view" or "full preview" mode.

### Proposed Fix

**Priority:** Medium | **Effort:** Medium

**Add a preview toggle** in the markdown cell toolbar that shows a side-by-side or full preview:

```tsx
// Add to MarkdownView's toolbar area:
{focusedIdx !== null && (
  <div style={styles.mdToolbar}>
    {/* Existing image upload button */}
    <button ...>📷</button>
    
    {/* NEW: Preview toggle */}
    <button
      style={{ ...styles.mdToolbarBtn, ...(showPreview ? styles.mdToolbarBtnActive : {}) }}
      onClick={() => setShowPreview(v => !v)}
      title="Toggle full preview"
    >
      {showPreview ? <Eye size={13} /> : <EyeOff size={13} />}
      Preview
    </button>
  </div>
)}
```

When `showPreview` is true, render the full cell source as markdown below the focused textarea:

```tsx
{showPreview && focusedIdx !== null && (
  <div style={styles.mdLivePreview}>
    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={markdownComponents}>
      {cell.source}
    </ReactMarkdown>
  </div>
)}
```

**Alternative (simpler):** Since the block-based approach already renders non-focused blocks as markdown, the issue is mainly that the *focused* block shows raw markdown. A simpler fix would be to add a subtle "preview strip" — render the current block's markdown in a small floating preview above or below the textarea.

**Recommendation:** Add the full-preview toggle in the toolbar. It's the simplest approach that gives users what they expect.

### Dependencies
- None. `ReactMarkdown` is already imported and configured.

---

## 9. Connector selector shows "— None —" inconsistently

### Current Implementation

**File:** `web/src/components/ConnectorSelector.tsx` (lines ~38-44)

```tsx
<option value="">
  {allowClear && value ? '— None —' : placeholder}
</option>
```

The logic is:
- If `allowClear=true` AND a value is currently selected → show "— None —" (to allow clearing)
- Otherwise → show the `placeholder` prop (e.g., "Select a connector")

**File:** `web/src/components/Cell.tsx` (lines ~264-270)

The cell-level connector dropdown uses different text:
```tsx
<option value="">— inherit from notebook —</option>
```

**Problem:** 
1. "— None —" vs placeholder text is visually inconsistent — em-dashes with a value feel different from placeholder text without a value
2. The cell-level selector says "— inherit from notebook —" which is a different pattern entirely
3. Users may be confused about the difference between "None", "inherit", and "no selection"

### Proposed Fix

**Priority:** Low | **Effort:** Trivial

**Standardize the empty-option text across all connector selectors:**

In `ConnectorSelector.tsx`:
```tsx
<option value="" disabled={!allowClear || !value}>
  {allowClear && value ? 'Clear selection' : placeholder}
</option>
```

Changes:
- Replace "— None —" with "Clear selection" (action-oriented, clearer intent)
- Add `disabled` when there's nothing to clear (prevents selecting the placeholder as a value)

In `Cell.tsx` connector dropdown:
```tsx
<option value="">Inherit from notebook</option>
```

Changes:
- Remove em-dashes (visual noise)
- Use sentence case for consistency

**Also consider:** Add a visual separator or different styling for the empty option to distinguish it from real connector options.

### Dependencies
- None. Purely presentational text changes.

---

## 10. Notebook description doesn't support markdown

### Current Implementation

**File:** `web/src/pages/NotebookPage.tsx` (lines ~540-550)

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

The description is a plain `<input>` field. It does not support any formatting.

**Problem:** Users writing notebook descriptions may want to include links, emphasis, or lists. A plain text field feels limiting for documentation that accompanies analytical work.

### Proposed Fix

**Priority:** Low | **Effort:** Small-Medium

**Approach A — Render markdown on blur (simplest):**  
Keep the `<input>` for editing, but render the description as markdown when not being edited:

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

```typescript
descRendered: {
  fontSize: 14,
  color: 'var(--text-muted)',
  fontFamily: 'var(--font-sans)',
  cursor: 'pointer',
  lineHeight: 1.6,
  padding: '2px 0',
  // Limit rendered markdown size in description context
  '& p': { margin: 0 },
  '& a': { color: 'var(--accent)' },
},
descPlaceholder: {
  color: 'var(--text-muted)',
  opacity: 0.6,
  fontStyle: 'italic',
}
```

**Approach B — Textarea with markdown support:**  
Replace `<input>` with a small `<textarea>` that supports multi-line markdown editing, with a live preview toggle. This is more complex but offers a better editing experience.

**Recommendation:** Start with Approach A. It requires minimal changes and gives users markdown rendering for free. The editing experience remains simple (single-line input), but the rendered output supports formatting.

### Dependencies
- `ReactMarkdown`, `remarkGfm`, `rehypeRaw` — already imported in `MarkdownCell.tsx`, just need to import in `NotebookPage.tsx`.
- No backend changes needed (description is already stored as a string).

---

## Summary Table

| # | Issue | Effort | Priority | Backend Change? | Key File(s) |
|---|-------|--------|----------|-----------------|-------------|
| 1 | Schema Browser auto-detect connector | Trivial | **High** | No | `NotebookPage.tsx` |
| 2 | Cell title auto-populate | Small | Medium | No | `Cell.tsx` |
| 3 | Code vs text visual distinction | Small | **High** | No | `Cell.tsx` |
| 4 | Slide break button tooltip | Small | Medium | No | `Cell.tsx` |
| 5 | Parameters panel description | Small | Medium | No | `ParametersBar.tsx` |
| 6 | Cron input helper | Medium | Medium | No | `SchedulesPanel.tsx` |
| 7 | Drag-and-drop cell reorder | Medium | **High** | **Yes** | `NotebookPage.tsx`, `Cell.tsx` |
| 8 | Live markdown preview | Medium | Medium | No | `MarkdownCell.tsx` |
| 9 | Connector selector consistency | Trivial | Low | No | `ConnectorSelector.tsx`, `Cell.tsx` |
| 10 | Description markdown support | Small-Med | Low | No | `NotebookPage.tsx` |

## Implementation Order Recommendation

1. **Quick wins (no backend, <30 min each):** Issues 1, 3, 4, 9
2. **Medium effort, high value:** Issues 2, 5, 6, 8, 10
3. **Largest effort:** Issue 7 (drag-and-drop — requires new dependency + backend endpoint)

---

## Cross-Cutting Concerns

### Accessibility
- Issue 3 (visual distinction): Ensure the border color is not the *only* differentiator — the existing SQL/MD text tag provides redundant encoding
- Issue 7 (drag-and-drop): `@dnd-kit` has built-in keyboard support for drag operations
- Issue 4 (tooltips): Use `aria-label` in addition to `title` for screen readers

### Performance
- Issue 7 (drag-and-drop): Use `position` field for ordering rather than re-indexing all cells on every drop
- Issue 8 (markdown preview): Debounce the preview rendering to avoid re-parsing markdown on every keystroke

### Testing
- Issues 1, 9: Simple unit tests for the conditional logic
- Issue 6: Snapshot test the cron description output
- Issue 7: E2E test for drag-and-drop with Playwright
