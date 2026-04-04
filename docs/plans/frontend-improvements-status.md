# Frontend Improvements Plan - Status

> **Last Updated**: After Task 10 completion
> **Status**: Phase 1-2 complete, Phase 3 in progress (1/5 tasks done)

---

## ✅ Phase 1: Visual Quality (COMPLETE - 4/4 tasks)

### Task 1: Replace Hardcoded Colors with CSS Variables ✅

**Status**: ✅ Complete

**What was done**:
- Added `--success-full`, `--success-light`, `--error-full`, `--error-light`, `--error-border` to theme.css
- Replaced all 54 instances of hardcoded error/success colors across 18 files
- Updated StatusBadge, SchedulesPanel, ScheduleItem, SchemaBrowser, LoginPage, AuditPage, MembersPage, DashboardEditorPage, NotebookPage, HomePage, OrgOnboardingPage, ConnectorsPage

**Commit**: `refactor: replace all remaining hardcoded colors with CSS variables`

---

### Task 2: Add Focus States for Accessibility ✅

**Status**: ✅ Complete

**What was done**:
- Added focus-visible styles to theme.css for keyboard navigation
- Added focus styles for buttons, inputs, selects, textareas, links

**Commit**: `feat: add focus-visible states for accessibility`

---

### Task 3: Standardize Border Radius ✅

**Status**: ✅ Complete

**What was done**:
- Added CSS variables: `--radius-xs`, `--radius-sm`, `--radius`, `--radius-md`, `--radius-lg`
- Replaced all non-standard values (3, 5, 7, 9, 14) with standard scale (4, 6, 8, 10, 12)
- Updated 51 files across components and pages

**Commit**: `feat: add comprehensive border-radius scale to theme.css` and `style: standardize border-radius values across all components`

---

### Task 4: Add Consistent Hover States ✅

**Status**: ✅ Complete

**What was done**:
- Added `.card-hover` class for cards with shadow transition
- Added `.icon-btn` class for icon buttons
- Added `.delete-btn` class for delete buttons
- Applied to HomePage and DashboardsPage

**Commit**: `feat: add hover states for cards and interactive elements`

---

## ✅ Phase 2: UX Improvements (COMPLETE - 5/5 tasks)

### Task 5: Fix Collaboration Cursor Names ✅

**Status**: ✅ Complete

**What was done**:
- Added `GET /api/v1/users/me` backend endpoint in `internal/api/auth_handlers.go`
- Updated `LoginPage.tsx` OIDC callback to fetch user info after getting token
- Fixed IMPROVEMENTS.md #27

**Commit**: `fix: fetch user info after OIDC login to show real names in collaboration`

---

### Task 6: Notebook Title/Description Header ✅

**Status**: ✅ Complete

**What was done**:
- Created fixed header section at top of notebook page
- Added back button linking to home
- Made title and description editable inline
- Added "Last updated" metadata
- Moved title out of scrollable area for better UX

**Commit**: `feat: add prominent notebook header with back button and metadata`

---

### Task 7: Create ErrorBanner Component ✅

**Status**: ✅ Complete

**What was done**:
- Created `web/src/components/ErrorBanner.tsx` with error/warning/info variants
- Added dismiss button support
- Replaced inline error displays in:
  - HomePage.tsx
  - NotebookPage.tsx
  - MembersPage.tsx
  - DashboardEditorPage.tsx
  - LoginPage.tsx
  - OrgOnboardingPage.tsx
  - AuditPage.tsx
- Removed unused error styles from MembersPage

**Commit**: `Progress: Task 7 complete - ErrorBanner component created and applied`

---

### Task 8: Improve Empty States Consistency ✅

**Status**: ✅ Complete

**What was done**:
- Verified EmptyState component usage across all pages
- Converted dashboard empty states to use EmptyState component
- Removed duplicated empty state styles from DashboardEditorPage, DashboardPage, PublicDashboardPage
- Table inline empty states left as-is (appropriate for table context)

**Commit**: `Progress: Task 8 complete - consolidate empty state styling using EmptyState component`

---

### Task 9: Add Loading Skeletons ✅

**Status**: ✅ Complete

**What was done**:
- Created `web/src/components/LoadingSpinner.tsx` - Simple loading dot component
- Created `web/src/components/LoadingPage.tsx` - Full-page loading component with optional message
- Created `web/src/components/Skeleton.tsx` - Skeleton loading components (Skeleton, SkeletonRow, SkeletonCard)
- Added skeleton pulse animation to theme.css
- Updated NotebookPage to use LoadingPage component
- Updated DashboardsPage to use LoadingSpinner component
- Removed duplicate loading styles from pages

**Effort**: 2 hours

---

## 🟡 Phase 3: Features (IN PROGRESS - 1/5 tasks)

### Task 10: Markdown Anchor Links (#5) ✅

**Status**: ✅ Complete

**What was done**:
- Added `slugify` function to generate anchor IDs from heading text
- Created `markdownComponents` object with custom h1-h6 components
- Each heading now gets an auto-generated ID and shows `#` anchor link on hover
- Updated files:
  - `web/src/components/Cell.tsx` - Main markdown cell component
  - `web/src/components/CellVariantWireframe.tsx` - Storybook wireframe component
  - `web/src/pages/PresentationPage.tsx` - Presentation mode
  - `web/src/styles/theme.css` - Added hover CSS rule for header anchors
- Anchor links are hidden by default, appear on header hover

**Effort**: 2 hours

---

### Task 11: Named Query Variables (#13)

**Status**: 🟡 Not started

**Effort**: 4-6 hours (needs backend)

---

### Task 12: Image Customization (#17)

**Status**: 🟡 Not started

**Effort**: 3 hours

---

### Task 13: Audit Log UX - Show Names (#26)

**Status**: 🟡 Not started

**Effort**: 2 hours (needs backend join)

---

### Task 14: Profile Menu Enhancement (#28)

**Status**: 🟡 Not started

**Effort**: 3 hours

---

## 🟢 Phase 4: Components (NOT STARTED - 0/3 tasks)

### Task 15: Tooltip Component
**File**: Create `web/src/components/Tooltip.tsx`
**Effort**: 1 hour

---

### Task 16: Dashboard Editor Polish
**Effort**: 2 hours

---

### Task 17: Loading State Improvements
**Effort**: 1 hour

---

## 📊 Summary

| Phase | Task | Priority | Effort | Status |
|-------|------|----------|--------|--------|
| 1 | 1. Replace Colors | 🔴 High | 1h | ✅ Complete |
| 1 | 2. Focus States | 🔴 High | 30m | ✅ Complete |
| 1 | 3. Border Radius | 🔴 High | 1-2h | ✅ Complete |
| 1 | 4. Hover States | 🔴 High | 1-2h | ✅ Complete |
| 2 | 5. Collab Cursors | 🟡 Medium | 30m | ✅ Complete |
| 2 | 6. Notebook Header | 🟡 Medium | 2-3h | ✅ Complete |
| 2 | 7. ErrorBanner | 🟡 Medium | 1h | ✅ Complete |
| 2 | 8. Empty States | 🟡 Medium | 30m | ✅ Complete |
| 2 | 9. Skeletons | 🟡 Medium | 2h | ✅ Complete |
| 3 | 10. Anchor Links | 🟡 Medium | 2h | ✅ Complete |
| 3 | 11. Named Variables | 🟢 Low | 4-6h | 🟡 Not started |
| 3 | 12. Image Custom | 🟢 Low | 3h | 🟡 Not started |
| 3 | 13. Audit Names | 🟢 Low | 2h | 🟡 Not started |
| 3 | 14. Profile Menu | 🟢 Low | 3h | 🟡 Not started |
| 4 | 15. Tooltip | 🟢 Low | 1h | 🟢 Not started |
| 4 | 16. Dashboard Polish | 🟢 Low | 2h | 🟢 Not started |
| 4 | 17. Loading States | 🟢 Low | 1h | 🟢 Not started |

---

## Next Actions

**Phase 1 ✅ COMPLETE** - Visual Quality
**Phase 2 ✅ COMPLETE** - UX Improvements
**Phase 3 🟡 IN PROGRESS** - Features

**Next up (Phase 3)**:

1. **Task 11**: Named Query Variables (#13) - 4-6 hours
   - Requires backend support for named parameters

2. **Task 12**: Image Customization (#17) - 3 hours
   - Allow users to customize image sizing/alignment

3. **Task 13**: Audit Log UX - Show Names (#26) - 2 hours
   - Requires backend join to show user names

4. **Task 14**: Profile Menu Enhancement (#28) - 3 hours
   - Add user preferences menu