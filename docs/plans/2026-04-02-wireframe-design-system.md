# Wireframe Design System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate every component and page in the web app from the current purple-accented, shadowed design to the flat wireframe style established by `Cell.tsx`.

**Architecture:** All changes are CSS-only (inline style objects). No new abstractions, no new files except where noted. Each batch touches a logical group of components, compiles cleanly, and ships as one commit. The global CSS variables in `theme.css` are updated first so downstream components inherit the changes automatically.

**Tech Stack:** React + TypeScript, inline `React.CSSProperties` style objects, `var(--*)` CSS variables defined in `web/src/styles/theme.css`.

---

## Wireframe Style Reference

Every component must match these values. Commit this list to memory.

| Property | Old value | New value |
|---|---|---|
| Card/panel background | `var(--bg-secondary)` | `#fff` |
| All borders | `var(--border)` / `var(--border-light)` | `1px solid #e8e8e8` |
| Border-radius | `6–12px` | `4px` (panels, cards, inputs) |
| Box shadow | `var(--shadow-sm/md)` | **none** (modals only: `0 2px 16px rgba(0,0,0,0.12)`) |
| Primary action button | `background: var(--accent)` | `background: #111, color: #fff` |
| Secondary button | varied | `background: none, border: 1px solid #ddd, color: #555` |
| Accent color usage | structural chrome | only: focused inputs, active tabs, links |
| Text primary | `var(--text-primary)` | `#111` / `#222` |
| Text secondary | `var(--text-secondary)` | `#555` |
| Text muted | `var(--text-muted)` | `#aaa` / `#bbb` |
| Table header bg | `var(--bg-secondary)` | `#fff` |
| Table row alt bg | `#faf9f7` | **none** (all rows same) |
| Accent-light bg | `var(--accent-light)` | `#f5f5f5` |

---

## Task 1: Update CSS Variables (theme.css)

Updating the variables means many downstream components pick up the new values for free, reducing the per-component work.

**Files:**
- Modify: `web/src/styles/theme.css`

**Step 1: Edit theme.css**

Replace the `:root` block with:

```css
:root {
  --bg-primary: #f5f5f5;
  --bg-secondary: #f9f9f9;
  --bg-cell-code: #f7f7f7;
  --bg-cell-text: #ffffff;
  --border: #e8e8e8;
  --border-light: #efefef;
  --text-primary: #111;
  --text-secondary: #555;
  --text-muted: #aaa;
  --accent: #7c6faa;
  --accent-hover: #6a5e96;
  --accent-light: #f5f5f5;
  --success: #2e7d32;
  --success-full: #27ae60;
  --success-light: #f0f9f0;
  --error: #b85c5c;
  --error-full: #c0392b;
  --error-light: #fdf5f5;
  --error-border: #f5d0d0;
  --warning: #b89a4a;
  --nav-bg: #1a1814;
  --nav-text: #e8e4dc;
  --nav-border: #2e2a24;
  --font-sans: 'DM Sans', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-mono: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
  --radius-xs: 3px;
  --radius-sm: 4px;
  --radius: 4px;
  --radius-md: 4px;
  --radius-lg: 4px;
  --shadow-sm: none;
  --shadow-md: 0 2px 16px rgba(0,0,0,0.10);
}
```

Key changes:
- `--bg-primary`: `#f8f7f4` → `#f5f5f5` (cooler, more neutral)
- `--accent-light`: `#ede9f8` → `#f5f5f5` (neutral, not purple)
- `--shadow-sm`: shadow → `none` (flat)
- `--shadow-md`: heavy → subtle (only for modals/dropdowns)
- All `--radius-*`: collapsed to `4px`

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```
Expected: clean compilation.

**Step 3: Commit**
```bash
git add web/src/styles/theme.css
git commit -m "style: flatten CSS variables — no shadows, neutral borders, 4px radius"
```

---

## Task 2: Batch A — Foundation Components

**Files:**
- Modify: `web/src/components/Modal.tsx`
- Modify: `web/src/components/FormCard.tsx`
- Modify: `web/src/components/PanelHeader.tsx`
- Modify: `web/src/components/SectionHeader.tsx`
- Modify: `web/src/components/ErrorBanner.tsx`

These are primitives used everywhere. Getting them right first means composed components look correct automatically.

### 2a. Modal.tsx

Find the `modal` style entry. Apply:
```ts
modal: {
  background: '#fff',
  borderRadius: 4,
  border: '1px solid #e8e8e8',
  boxShadow: '0 2px 16px rgba(0,0,0,0.12)',
  // keep width/maxHeight/overflow as-is
}
```

Also update `header` borderBottom and `footer` borderTop to `1px solid #e8e8e8`.

### 2b. FormCard.tsx

```ts
card: {
  background: '#fff',
  border: '1px solid #e8e8e8',
  borderRadius: 4,
  // remove boxShadow entirely
}
```

Remove `boxShadow` from the style object.

### 2c. PanelHeader.tsx

Ensure `header` has `borderBottom: '1px solid #e8e8e8'` and `background: '#fff'`. Remove any `var(--bg-secondary)`.

### 2d. SectionHeader.tsx

Audit only — this component is already aligned. Verify no `var(--accent)` appears in structural styles; it should only appear in the title if used as a colored heading. No changes expected.

### 2e. ErrorBanner.tsx

Change `borderRadius` from `6` to `4` in all variant styles. Verify colored backgrounds (error red, warning yellow) are kept — those are intentional for status communication.

**Step 1: Make all changes above**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Verify visually in Storybook**
```bash
cd web && npm run storybook
```
Open: Modal, FormCard stories. Confirm flat appearance.

**Step 4: Commit**
```bash
git add web/src/components/Modal.tsx web/src/components/FormCard.tsx \
        web/src/components/PanelHeader.tsx web/src/components/SectionHeader.tsx \
        web/src/components/ErrorBanner.tsx
git commit -m "style: flatten Modal, FormCard, PanelHeader, ErrorBanner to wireframe"
```

---

## Task 3: Batch B — Panels and Sidebars

**Files:**
- Modify: `web/src/components/SchemaBrowser.tsx`
- Modify: `web/src/components/HistoryPanel.tsx`
- Modify: `web/src/components/SchedulesPanel.tsx`

### 3a. SchemaBrowser.tsx

```ts
// sidebar
background: '#fff',
borderRight: '1px solid #e8e8e8',
// remove any box-shadow

// loadingDot (if present): background → '#ddd'
// searchInput: border → '1px solid #ddd', borderRadius → 4
// treeItem hover: background → '#f5f5f5'
```

### 3b. HistoryPanel.tsx

```ts
// panel container
background: '#fff',
borderLeft: '1px solid #e8e8e8',
// remove box-shadow if present

// currentBadge
background: '#f5f5f5',
color: '#666',
border: '1px solid #e8e8e8',

// restoreBtn
background: 'none',
border: '1px solid #ddd',
color: '#555',
borderRadius: 4,

// versionItem hover: background → '#f9f9f9'
```

### 3c. SchedulesPanel.tsx

```ts
// panel
background: '#fff',
borderTop: '1px solid #e8e8e8',

// createBtn / saveBtn: use primary dark style
background: '#111',
color: '#fff',
border: 'none',
borderRadius: 4,

// cancelBtn
background: 'none',
border: '1px solid #ddd',
color: '#555',
borderRadius: 4,

// cronInput / select fields
border: '1px solid #ddd',
borderRadius: 4,

// loadingDot: background → '#ddd'
```

**Step 1: Apply all changes**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Commit**
```bash
git add web/src/components/SchemaBrowser.tsx \
        web/src/components/HistoryPanel.tsx \
        web/src/components/SchedulesPanel.tsx
git commit -m "style: flatten SchemaBrowser, HistoryPanel, SchedulesPanel panels"
```

---

## Task 4: Batch C — Tables and Data Display

**Files:**
- Modify: `web/src/components/StyledTable.tsx`
- Modify: `web/src/components/OutputRenderer.tsx`
- Modify: `web/src/components/StatusBadge.tsx`

### 4a. StyledTable.tsx

```ts
// tableWrapStyle
border: '1px solid #e8e8e8',
borderRadius: 4,
// remove boxShadow

// thBase
background: '#fff',
borderBottom: '1px solid #e8e8e8',
color: '#888',
fontSize: 11,
fontWeight: 600,
fontFamily: 'var(--font-mono)',

// rowStyle
borderBottom: '1px solid #e8e8e8',
// No alternating row color — all rows same background

// tdBase
color: '#333',
```

### 4b. OutputRenderer.tsx

```ts
// Remove rowAlt style entirely — delete the ternary that applies it

// viewBtnActive
background: '#f5f5f5',
borderRadius: 4,
border: '1px solid #ddd',
// remove boxShadow

// viewBtn (inactive)
background: 'none',
border: '1px solid transparent',

// th
background: '#fff',
borderBottom: '1px solid #e8e8e8',

// td
borderBottom: '1px solid #f0f0f0',

// error output box
background: '#fdf5f5',
border: '1px solid #f5d0d0',
borderRadius: 4,

// rowCount / colCount footer text
color: '#bbb',
fontFamily: 'var(--font-mono)',
fontSize: 10,
```

### 4c. StatusBadge.tsx

Audit only. Verify no structural changes needed. Keep colored status text as-is (it communicates state). No changes expected.

**Step 1: Apply all changes**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Verify in Storybook**

Open OutputRenderer and StyledTable stories. Confirm: no alternating rows, flat table header, no shadow on table wrapper.

**Step 4: Commit**
```bash
git add web/src/components/StyledTable.tsx \
        web/src/components/OutputRenderer.tsx \
        web/src/components/StatusBadge.tsx
git commit -m "style: flatten tables — no alternating rows, hairline borders, no shadow"
```

---

## Task 5: Batch D — Misc Components

**Files:**
- Modify: `web/src/components/EmptyState.tsx`
- Modify: `web/src/components/ShortcutsModal.tsx`
- Modify: `web/src/components/ConnectorSelector.tsx`
- Modify: `web/src/components/ParametersBar.tsx`
- Modify: `web/src/components/TopBar.tsx` (verify/minimal)

### 5a. EmptyState.tsx

```ts
// iconTile
background: '#f5f5f5',
border: '1px solid #e8e8e8',
// remove var(--accent-light) background

// actionBtn (primary action)
background: '#111',
color: '#fff',
border: 'none',
borderRadius: 4,
padding: '8px 18px',
fontSize: 13,
```

### 5b. ShortcutsModal.tsx

```ts
// kbd element
background: '#f5f5f5',
border: '1px solid #e8e8e8',
borderRadius: 3,
fontFamily: 'var(--font-mono)',
fontSize: 11,
```

### 5c. ConnectorSelector.tsx

This is a native `<select>` wrapper or custom component. Add consistent border styling:

```ts
// The select element itself or its wrapper
border: '1px solid #ddd',
borderRadius: 4,
padding: '4px 8px',
fontSize: 12,
fontFamily: 'var(--font-mono)',
background: '#fff',
color: '#333',
outline: 'none',
```

### 5d. ParametersBar.tsx

```ts
// saveBtn / primary action
background: '#111',
color: '#fff',
border: 'none',
borderRadius: 4,

// addParamBtn (secondary)
background: 'none',
border: '1px solid #ddd',
borderRadius: 4,
color: '#555',
// change from dashed to solid

// cancelBtn
background: 'none',
border: '1px solid #ddd',
borderRadius: 4,
color: '#555',

// param input fields
border: '1px solid #ddd',
borderRadius: 4,
```

### 5e. TopBar.tsx

Audit only. The dark `var(--nav-bg)` navigation bar is intentionally dark — keep as-is. Check:
- Dropdown/menu popover: must have `border: '1px solid #e8e8e8'`, `borderRadius: 4`, shadow at `0 2px 16px rgba(0,0,0,0.12)`
- Active nav item highlight: keep `var(--accent)` or use `rgba(255,255,255,0.12)` on dark bg
- No changes to nav bg itself

**Step 1: Apply all changes**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Commit**
```bash
git add web/src/components/EmptyState.tsx web/src/components/ShortcutsModal.tsx \
        web/src/components/ConnectorSelector.tsx web/src/components/ParametersBar.tsx \
        web/src/components/TopBar.tsx
git commit -m "style: flatten EmptyState, Shortcuts, ConnectorSelector, ParametersBar"
```

---

## Task 6: Batch E — Pages (Simple)

**Files:**
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/pages/AdminPage.tsx`
- Modify: `web/src/pages/MembersPage.tsx`

### 6a. LoginPage.tsx

```ts
// form container / card
border: '1px solid #e8e8e8',
borderRadius: 4,
boxShadow: 'none',  // remove shadow

// input fields
border: '1px solid #ddd',
borderRadius: 4,
// change from 1.5px to 1px

// submit button
background: '#111',
color: '#fff',
border: 'none',
borderRadius: 4,

// SSO buttons
border: '1px solid #ddd',
borderRadius: 4,
background: '#fff',
color: '#333',
// remove 1.5px border width
```

### 6b. AdminPage.tsx

```ts
// tabs borderBottom
borderBottom: '1px solid #e8e8e8',

// active tab indicator: keep var(--accent) for active underline — this is interactive feedback
// th
borderBottom: '1px solid #e8e8e8',
// td
borderBottom: '1px solid #e8e8e8',

// All var(--border) → '1px solid #e8e8e8'
// All var(--border-light) → '1px solid #efefef'
```

### 6c. MembersPage.tsx

```ts
// inviteBtn (primary action)
background: '#111',
color: '#fff',
border: 'none',
borderRadius: 4,

// selfBadge
background: '#f5f5f5',
color: '#666',
border: '1px solid #e8e8e8',
borderRadius: 3,
// remove var(--accent-light) background and var(--accent) color

// removeBtn: keep as-is (ghost style is correct)

// roleSelectInline
border: '1px solid #ddd',
borderRadius: 4,
```

**Step 1: Apply all changes**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Commit**
```bash
git add web/src/pages/LoginPage.tsx web/src/pages/AdminPage.tsx \
        web/src/pages/MembersPage.tsx
git commit -m "style: flatten Login, Admin, Members pages to wireframe"
```

---

## Task 7: Batch F — List Pages

**Files:**
- Modify: `web/src/pages/HomePage.tsx`
- Modify: `web/src/pages/DashboardsPage.tsx`
- Modify: `web/src/pages/ConnectorsPage.tsx`

These all follow the same "list of cards + create form" pattern. Apply the same treatment to each.

### Per-page changes

**Card/item container:**
```ts
background: '#fff',
border: '1px solid #e8e8e8',
borderRadius: 4,
// remove boxShadow
// hover: change border to '1px solid #ccc' instead of shadow lift
```

**Create/new form container:**
```ts
background: '#fff',
border: '1px solid #e8e8e8',
borderRadius: 4,
// remove boxShadow and remove any accent-colored border
```

**Primary action buttons (Create, Invite, Save):**
```ts
background: '#111',
color: '#fff',
border: 'none',
borderRadius: 4,
padding: '7px 16px',
fontSize: 13,
```

**Secondary/cancel buttons:**
```ts
background: 'none',
border: '1px solid #ddd',
borderRadius: 4,
color: '#555',
```

**ConnectorsPage specifically:**
```ts
// testBtn (test connection)
background: 'none',
border: '1px solid #ddd',
color: '#555',
// not accent-bordered

// type badges
background: '#f5f5f5',
color: '#666',
borderRadius: 3,
```

**CardFooter / item footer separator:**
```ts
borderTop: '1px solid #e8e8e8',
```

**Step 1: Apply all changes to all three pages**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Start dev server and spot-check each page**
```bash
# Terminal 1
task dev

# Terminal 2
task dev:web
```
Open http://localhost:5173 — Home, Dashboards, Connectors. Verify: no shadows on cards, dark primary buttons, hairline separators.

**Step 4: Commit**
```bash
git add web/src/pages/HomePage.tsx web/src/pages/DashboardsPage.tsx \
        web/src/pages/ConnectorsPage.tsx
git commit -m "style: flatten Home, Dashboards, Connectors list pages to wireframe"
```

---

## Task 8: Batch G — Dashboard Editor (Most Complex)

**Files:**
- Modify: `web/src/pages/DashboardEditorPage.tsx`
- Modify: `web/src/components/ChartView.tsx`

The Dashboard Editor has compound UI: widget cards, a picker panel/overlay, and the chart/table display.

### 8a. DashboardEditorPage.tsx

**Picker panel (slide-in overlay):**
```ts
pickerPanel: {
  background: '#fff',
  borderLeft: '1px solid #e8e8e8',
  boxShadow: 'none',  // remove heavy shadow — border defines edge
  borderRadius: 0,    // full-height panel, no radius
}

pickerHeader: {
  borderBottom: '1px solid #e8e8e8',
  background: '#fff',
}
```

**Widget cards on the canvas:**
```ts
widgetCard: {
  background: '#fff',
  border: '1px solid #e8e8e8',
  borderRadius: 4,
  // remove boxShadow
}

widgetCardSelected: {
  border: '1px solid #7c6faa',  // keep accent for selected state — it's interactive
}
```

**Add widget button:**
```ts
addWidgetBtn: {
  background: '#111',
  color: '#fff',
  border: 'none',
  borderRadius: 4,
}
```

**Cell/widget picker items:**
```ts
pickerItem: {
  borderBottom: '1px solid #f0f0f0',
  background: '#fff',
}
pickerItemHover: {
  background: '#f9f9f9',
}
```

### 8b. ChartView.tsx

```ts
// chartContainer / wrapper
border: '1px solid #e8e8e8',
borderRadius: 4,
background: '#fff',
// remove shadow

// configBtn (settings gear)
background: 'none',
border: '1px solid #ddd',
color: '#aaa',
borderRadius: 4,
```

**Step 1: Apply all changes**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Spot-check in browser**

Navigate to a dashboard editor. Confirm: widget cards flat, picker panel edge defined by hairline, no heavy shadows.

**Step 4: Commit**
```bash
git add web/src/pages/DashboardEditorPage.tsx \
        web/src/components/ChartView.tsx
git commit -m "style: flatten DashboardEditor and ChartView to wireframe"
```

---

## Task 9: Batch H — Notebook Page Cleanup

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

The notebook page was partially migrated when `Cell.tsx` was introduced. This task finishes the job.

**Toolbar buttons:**
```ts
schemaBtn: {
  background: 'none',
  border: '1px solid #ddd',
  color: '#555',
  borderRadius: 4,
  fontSize: 12,
  padding: '5px 12px',
}

schemaBtnActive: {
  background: '#f5f5f5',
  border: '1px solid #ccc',
  color: '#111',
  // do NOT use var(--accent) color here — keep neutral
}

runAllBtn: {
  background: '#111',  // dark primary, not purple
  color: '#fff',
  border: 'none',
  borderRadius: 4,
}
```

**Notebook title / description:**
```ts
notebookTitle: {
  // remove letterSpacing if it looks off
  cursor: 'pointer',
  color: '#111',
}

titleInput: {
  // borderBottom should be '1px solid #ccc' not accent
  borderBottom: '1px solid #ccc',
}

descInput: {
  color: '#aaa',
  fontSize: 14,
}
```

**Verify cells container is correct** (from previous work):
```ts
cells: {
  background: '#fff',
  border: '1px solid #e8e8e8',
  borderRadius: 4,
  overflow: 'hidden',
}
```

**Step 1: Apply all changes**

**Step 2: Type-check**
```bash
cd web && npx tsc --noEmit
```

**Step 3: Spot-check**

Open a notebook in the browser. Verify toolbar buttons are flat/neutral, run-all is dark, schema panel slides in cleanly.

**Step 4: Commit**
```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "style: finish wireframe migration of NotebookPage toolbar and title"
```

---

## Task 10: Global Sweep and Polish

A final pass to catch anything missed, and to clean up deprecated components.

**Step 1: Grep for remaining design system leaks**

Run these greps and fix any results that appear in active (non-deprecated) components:

```bash
# Box shadows that shouldn't exist
grep -rn "shadow-sm\|shadow-md\|box-shadow" web/src/components web/src/pages \
  --include="*.tsx" | grep -v "stories\|CellVariant\|TopBar\|Modal"

# Large border-radius values
grep -rn "borderRadius: [89]\|borderRadius: 1[0-9]\|borderRadius: '1[0-9]" \
  web/src/components web/src/pages --include="*.tsx" | grep -v "stories\|CellVariant"

# Accent color on backgrounds (not interactive)
grep -rn "background.*accent\|accent.*background" \
  web/src/components web/src/pages --include="*.tsx" | grep -v "stories\|CellVariant"
```

Fix each result by applying the wireframe values from the reference table at the top of this document.

**Step 2: Remove deprecated cell components**

`CodeCell.tsx`, `TextCell.tsx`, `CellHeader.tsx`, and `CellToolbar.tsx` are superseded by `Cell.tsx`. Delete them and their story files:

```bash
rm web/src/components/CodeCell.tsx
rm web/src/components/TextCell.tsx
rm web/src/components/CellHeader.tsx
rm web/src/components/CellHeader.stories.tsx
rm web/src/components/CellToolbar.tsx
rm web/src/components/CellToolbar.stories.tsx
```

**Step 3: Type-check to confirm nothing imports the deleted files**
```bash
cd web && npx tsc --noEmit
```

Expected: clean. If any import errors appear, fix them by pointing to `Cell.tsx`.

**Step 4: Full browser walkthrough**

Visit each page in sequence and verify visual consistency:
1. `/` — Home (notebook list)
2. `/notebooks/:id` — Notebook editor (cells, schema browser, schedules)
3. `/dashboards` — Dashboards list
4. `/dashboards/:id/edit` — Dashboard editor
5. `/connectors` — Connectors list
6. `/admin` — Admin panel

**Step 5: Commit**
```bash
git add -A
git commit -m "style: final sweep — remove deprecated cell components, fix remaining style leaks"
```

---

## Notes for the Implementer

- **Do not** create new helper components, abstractions, or CSS utility classes. Edit styles inline where they already live.
- **Do not** change any logic, props, or API calls — this is purely visual.
- **Keep** `var(--accent)` on: focused input outlines, active tab underlines, links, cursor/caret in editors, selected widget borders.
- **The Storybook** (`npm run storybook` in `web/`) is the fastest way to spot-check components without running the full app.
- **AppShell.tsx** and **Sidebar.tsx** are not in this plan — they were reviewed and are already aligned (dark nav = intentional, light content area = already correct).
- **PresentationPage.tsx** and **PublicDashboardPage.tsx** are read-only views — they can receive the same treatment as DashboardPage in a follow-up; not included here to keep scope tight.
