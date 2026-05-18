# PermissionsPanel UI/UX Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove broken "All/Created by me" filter from HomePage, and redesign PermissionsPanel to fix layout/overflow issues and improve contrast.

**Architecture:** Two separate changes: (1) HomePage cleanup removing filter state and UI, (2) PermissionsPanel complete style overhaul with two-state rows (compact default, expanded on hover) and improved contrast.

**Tech Stack:** React (inline styles, no new dependencies), CSS variables from theme.css

---

## Task 1: Remove filter from HomePage

**Files:**
- Modify: `web/src/pages/HomePage.tsx:244-248` (state + helper)
- Modify: `web/src/pages/HomePage.tsx:486-505` (filter pills UI)
- Modify: `web/src/pages/HomePage.tsx:413-416,621,625,672,676,722,726,765,769` (filterItems usages)

**Step 1: Remove filter state and filterItems helper**

Modify lines 244-248 in HomePage.tsx — delete:
```typescript
const [filter, setFilter] = useState<'all' | 'mine'>('all')

const filterItems = <T extends { created_by: string }>(items: T[]): T[] =>
  filter === 'mine' ? items.filter(i => i.created_by === user?.user_id) : items
```

**Step 2: Remove filterItems usages**

In all locations where `filterItems(...)` is called (searchFolders, searchNotebooks, searchConnectors, searchDashboards), replace `filterItems(searchX)` with just `searchX`.

**Step 3: Remove filter pills UI**

Delete lines 486-505 (the filter pills div with both 'all' and 'mine' buttons).

**Step 4: Verify**

Run: `grep -n "filterItems\|setFilter\|filter.*mine" web/src/pages/HomePage.tsx` — should return no matches.

---

## Task 2: Redesign PermissionsPanel — Layout & Row Structure

**Files:**
- Modify: `web/src/components/PermissionsPanel.tsx`

**Step 1: Update drawer and entry row dimensions**

```typescript
drawer: {
  ...
  width: 480,  // was 420
},
entryInfo: {
  ...
  minWidth: 120,  // was 80
  maxWidth: 160,  // was 100
},
entryName: {
  ...
  maxWidth: 150,  // was 90
},
```

**Step 2: Add expanded state tracking per row**

Add a new state to track which rows are expanded:
```typescript
const [expandedRows, setExpandedRows] = useState<Set<number>>(new Set())
```

Add toggle function:
```typescript
function toggleExpand(idx: number) {
  setExpandedRows(prev => {
    const next = new Set(prev)
    if (next.has(idx)) next.delete(idx)
    else next.add(idx)
    return next
  })
}
```

**Step 3: Update directEntries rows to use two-state layout**

Each directEntries row should:
- Always show: Avatar + Name + subject type (entryInfo)
- On hover/focus: presetRow + checkboxGroup + removeBtn appear below entryInfo, side-by-side

Current row (lines 334-376) should become:
```tsx
<div
  key={entry.id || `direct-${idx}`}
  style={styles.entryRow}
  onMouseEnter={() => toggleExpand(idx)}
  onMouseLeave={() => toggleExpand(idx)}
  onFocus={() => toggleExpand(idx)}
  tabIndex={0}
>
  <Avatar name={subjectName(entry)} type={entry.subject_type} />
  <div style={styles.entryInfo}>
    <span style={styles.entryName}>{subjectName(entry)}</span>
    <span style={styles.entryType}>{entry.subject_type}</span>
  </div>
  {expandedRows.has(idx) && (
    <div style={styles.expandedRow}>
      <div style={styles.presetRow}>
        {(['none', 'viewer', 'editor', 'admin'] as const).map((preset) => (
          <button
            key={preset}
            onClick={() => canEdit && applyPreset(idx, preset)}
            disabled={!canEdit}
            style={{
              ...styles.presetBtn,
              ...(entry.actions.join(',') === PRESETS[resourceType][preset].join(',') ||
                 (entry.actions.length === 0 && preset === 'none')
                ? styles.presetBtnSelected : {}),
            }}
          >
            {preset}
          </button>
        ))}
      </div>
      <div style={styles.checkboxGroup}>
        {actions.map((action) => (
          <label key={action} style={styles.checkLabel} title={ACTION_DESCRIPTIONS[resourceType][action]}>
            <input
              type="checkbox"
              checked={entry.actions.includes(action)}
              onChange={() => canEdit && handleToggleAction(idx, action)}
              disabled={!canEdit}
              style={{ marginRight: 3 }}
            />
            <span style={styles.actionLabel}>{action}</span>
          </label>
        ))}
      </div>
      <button
        style={styles.removeBtn}
        title="Remove"
        disabled={!canEdit}
        onClick={() => canEdit && handleRemoveEntry(idx)}
      >
        ×
      </button>
    </div>
  )}
</div>
```

**Step 4: Add expanded row styles**

```typescript
expandedRow: {
  display: 'flex',
  alignItems: 'center',
  gap: 12,
  width: '100%',
  marginTop: 4,
},
presetBtnSelected: {
  background: 'var(--accent)',
  color: '#fff',
  border: '1px solid var(--accent)',
},
```

**Step 5: Verify row expansion works**

Manual test: hover over a permission row — preset buttons and checkboxes should appear below the name.

---

## Task 3: Fix contrast issues in PermissionsPanel

**Step 1: Update actionLabel color to text-primary**

In styles:
```typescript
actionLabel: {
  fontSize: 11,
  color: 'var(--text-primary)',  // was var(--text-secondary)
},
```

**Step 2: Ensure inherited entries show checkboxes (read-only) with same contrast**

The inherited section already uses `actionLabel` from the same styles object, so it inherits the contrast fix. Verify inherited rows display correctly with full-contrast text.

**Step 3: Verify type badge contrast**

Type badge at line 261-263 uses `typeBadgeColors[resourceType]` for background and `var(--text-secondary)` for text. The connector badge (#e8fff0) is light, so text should be dark. Consider using `var(--text-primary)` for all type badges to be safe. Update line 516:
```typescript
textTransform: 'uppercase' as const,
color: 'var(--text-primary)',  // was var(--text-secondary)
```

**Step 4: Run visual check**

Start dev server and inspect permissions modal for any contrast issues.

---

## Task 4: Style preset buttons — selected state with accent

**Step 1: Confirm presetBtnSelected style**

Already added in Task 2 Step 4. Verify that selected preset has accent background and white text.

**Step 2: Run tests**

```bash
cd web && npm run build 2>&1 | head -50
```

Should build without errors.

---

## Task 5: Final verification

**Step 1: Run full build**

```bash
task build:web
```

**Step 2: Run tests**

```bash
task test
```

All tests should pass.