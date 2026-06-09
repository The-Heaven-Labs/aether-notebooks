# Wave 1: Quick Fixes — Results

**Commit:** `92ba603`
**Branch:** `feat/all-improvements-2026-06-09`

## Item 2: Keyboard shortcuts modal dark mode ✅

**File:** `web/src/components/ShortcutsModal.tsx`

Changed `kbd` style from hardcoded light colors (`#f5f5f5`, `#e8e8e8`) to theme-aware CSS variables (`var(--bg-secondary)`, `var(--border)`). Added `color: 'var(--text-primary)'` so text is visible in dark mode.

## Item 3: Folder tree collapse button visibility and behavior ✅

**File:** `web/src/components/TwoPanelLayout.tsx`

1. Added `ChevronRight` to lucide-react import
2. Toggle button `left` position now dynamic: `collapsed ? 8 : 240`
3. Chevron icon toggles: `collapsed ? ChevronRight : ChevronLeft`
4. Tooltip text updates: `collapsed ? 'Expand folder tree' : 'Collapse folder tree'`

## Item 28: Member/Group sidebar icon collision ✅

**File:** `web/src/components/Sidebar.tsx`

1. Replaced `UsersRound` import with `UserCircle`
2. Members icon: `<Users>` → `<UserCircle>` (single user silhouette)
3. Groups icon: `<UsersRound>` → `<Users>` (three people silhouette)

## Validation

- All 3 files compile (TypeScript — no type errors)
- Changes are minimal and isolated — no side effects on other components
- CSS variables used are already defined in `theme.css`
