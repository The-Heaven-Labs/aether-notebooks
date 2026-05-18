# PermissionsPanel UI/UX Redesign

**Date:** 2026-05-18
**Status:** Approved

## 1. Overview

Redesign the PermissionsPanel (slide-out drawer) to fix layout/overflow issues, improve contrast, and reduce visual noise. Functionality remains unchanged.

## 2. Changes

### Remove filter pills from HomePage
- Delete the "All / Created by me" filter pills UI (lines 486–505 in `HomePage.tsx`)
- Delete the unused `filter` state and `filterItems` helper

### PermissionsPanel Layout

**Row structure — two visual states:**
- **Compact (default):** Avatar + Name + subject type badge — minimal visual footprint
- **Expanded (hover/focus):** Preset buttons (none/viewer/editor/admin) + individual action toggles shown side-by-side horizontally below the name line; remove button

**Spacing/contrast fixes:**
- Drawer width: `420px → 480px`
- `entryInfo` min-width/max-width: `80/100px → 120/160px` (accommodate longer names without truncation)
- `actionLabel` color: `var(--text-secondary) → var(--text-primary)`
- `entryType` color stays `var(--text-muted)`
- Type badge text color: low-contrast badges use `var(--text-primary)`
- Preset buttons: selected preset uses accent background with white text (not just border)

**Preset button styling:**
- Default: transparent bg, border `var(--border)`, color `var(--text-muted)`
- Selected: bg `var(--accent)`, text `#fff`
- Hover: subtle opacity change via existing `button:not(:disabled):hover` rule

**Remove button:** appears on row hover, positioned to the right of the entry row, `×` character.

**Add entry row:**
- User/group select dropdown
- Action toggles shown horizontally (side-by-side, not stacked)
- "Add" button

## 3. Files to Modify

- `web/src/pages/HomePage.tsx` — remove filter pills, `filter` state, `filterItems` helper
- `web/src/components/PermissionsPanel.tsx` — complete style and layout overhaul