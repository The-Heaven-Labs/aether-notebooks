# Frontend Improvements Plan - Updated Status

> **Last Updated**: Current state after component extraction
> **Status**: Partially complete - components extracted, visual polish needed

## ✅ Already Completed

### Components Extracted
These components have been successfully extracted:

- **EmptyState** (`web/src/components/EmptyState.tsx`) ✅
- **Modal** (`web/src/components/Modal.tsx`) ✅
- **FormCard** (`web/src/components/FormCard.tsx`) ✅
- **PanelHeader** (`web/src/components/PanelHeader.tsx`) ✅
- **SectionHeader** (`web/src/components/SectionHeader.tsx`) ✅
- **StatusBadge** (`web/src/components/StatusBadge.tsx`) ✅ (needs CSS var fix)
- **StyledTable** (`web/src/components/StyledTable.tsx`) ✅
- **TreeItem** (`web/src/components/TreeItem.tsx`) ✅
- **ScheduleItem** (`web/src/components/ScheduleItem.tsx`) ✅ (needs CSS var fix)

### Pages Using New Components
- **AuditPage.tsx**: Uses `EmptyState`, `StyledTable`
- **DashboardsPage.tsx**: Uses `EmptyState`
- **MembersPage.tsx**: Uses `StyledTable`, `FormCard`
- **HomePage.tsx**: Uses `EmptyState`
- **ConnectorsPage.tsx**: Uses `StyledTable`, `FormCard`

---

## 🔴 High Priority - Still Needed

### Task 1: Replace Remaining Hardcoded Colors

**Status**: 🔴 In Progress (18instances remaining, down from 54)

**Files still needing updates**:

1. **StatusBadge.tsx** (lines 12-13) - High priority, affects all usages
   ```typescript
   // Current
   success: '#2d7d46',
   error: '#c0392b',
   
   // Should be
   success: 'var(--success-full)',
   error: 'var(--error-full)',
   ```

2. **SchedulesPanel.tsx** (line 204)
3. **ScheduleItem.tsx** (lines 115, 135)
4. **SchemaBrowser.tsx** (line 164)
5. **LoginPage.tsx** (line 301)
6. **AuditPage.tsx** (line 58)
7. **MembersPage.tsx** (lines 200, 247)
8. **DashboardEditorPage.tsx** (lines 407, 411, 420)
9. **NotebookPage.tsx** (lines 376, 378)
10. **HomePage.tsx** (line 86)
11. **OrgOnboardingPage.tsx** (line 227)
12. **ConnectorsPage.tsx** (line 252)

**New CSS variables to add**:
```css
/* Status colors */
--success-full: #27ae60;
--success-light: #e8f5e9;
--error-full: #c0392b;
--error-light: #fdf3f3;
--error-border: #fcd0d0;
```

**Effort**: 1 hour | **Impact**: High

---

### Task 2: Add Focus States for Accessibility

**Status**: 🔴 Not Started

**Files**: `web/src/styles/theme.css`

**Implementation**:
```css
/* Focus visible for accessibility */
*:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

*:focus:not(:focus-visible) {
  outline: none;
}

button:focus-visible,
input:focus-visible,
select:focus-visible,
textarea:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-color: var(--accent);
}

a:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
  border-radius: 2px;
}
```

**Effort**: 30 minutes | **Impact**: High (accessibility)

---

### Task 3: Standardize Border Radius

**Status**: 🔴 Not Started

**Current**: Mixed values (3px, 4px, 5px, 6px, 7px, 8px, 10px, 12px, 14px)

**Target**: 4 standard values
- **XS (4px)**: Badges, tags, type icons
- **SM (6px)**: Inputs, buttons
- **MD (10px)**: Cards, cells
- **LG (12px)**: Large panels

**Files to update**: ~20 component files

**Effort**: 1-2 hours | **Impact**: Medium (consistency)

---

### Task 4: Add Consistent Hover States

**Status**: 🔴 Not Started

**Problem**: Many interactive elements lack hover feedback

**Approach**: Add CSS classes for common hover patterns

**Implementation**:
```css
/* Card hover */
.card-hover:hover {
  box-shadow: var(--shadow-md);
  transition: box-shadow 0.15s ease;
}

/* Button hover */
button:hover:not(:disabled) {
  opacity: 0.9;
}

/* Icon button hover */
.icon-btn:hover:not(:disabled) {
  background: var(--bg-secondary);
}
```

**Effort**: 1-2 hours | **Impact**: Medium (UX polish)

---

## 🟡 Medium Priority

### Task 5: Fix Collaboration Cursor Names

**Status**: 🟡 Implemented but possibly buggy

**File**: `web/src/components/CodeCell.tsx` (lines 41-54)

**Action**: Test and verify localStorage values are set correctly

**Effort**: 30 minutes

---

### Task 6: Notebook Title/Description Header

**Status**: 🟡 Not Started (IMPROVEMENTS.md #1)

**File**: `web/src/pages/NotebookPage.tsx`

**Implementation**: Add prominent title/description area at top of notebook page

**Effort**: 2-3 hours

---

### Task 7: Create ErrorBanner Component

**Status**: 🟡 Not Started

**File**: Create `web/src/components/ErrorBanner.tsx`

**Why needed**: Standardize error displays across all pages

**Usage**: Replace inline error styles in all pages

**Effort**: 1 hour

---

### Task 8: Add Loading Skeletons

**Status**: 🟡 Not Started

**File**: Create `web/src/components/Skeleton.tsx`

**Why needed**: Better perceived performance during loading

**Effort**: 2 hours

---

### Task 9: Create Tooltip Component

**Status**: 🟡 Not Started

**File**: Create `web/src/components/Tooltip.tsx`

**Why needed**: Add tooltips to icon-only buttons

**Effort**: 1 hour

---

## 🟢 Lower Priority (From IMPROVEMENTS.md)

### Task 10: Markdown Anchor Links (#5)
**Effort**: 2 hours

### Task 11: Named Query Variables (#13)
**Effort**: 4-6 hours (needs backend)

### Task 12: Image Customization (#17)
**Effort**: 3 hours

### Task 13: Audit Log UX - Show Names (#26)
**Effort**: 2 hours (needs backend join)

### Task 14: Profile Menu Enhancement (#28)
**Effort**: 3 hours

---

## 📊 Summary

| Task | Priority | Effort | Status |
|------|----------|--------|--------|
| 1. Replace Colors | 🔴 High | 1h | 18 instances left |
| 2. Focus States | 🔴 High | 30m | Not started |
| 3. Border Radius | 🔴 High | 1-2h | Not started |
| 4. Hover States | 🔴 High | 1-2h | Not started |
| 5. Collab Cursors | 🟡 Medium | 30m | Needs testing |
| 6. Notebook Header | 🟡 Medium | 2-3h | Not started |
| 7. ErrorBanner | 🟡 Medium | 1h | Not started |
| 8. Skeletons | 🟡 Medium | 2h | Not started |
| 9. Tooltip | 🟡 Medium | 1h | Not started |
| 10-14. Features | 🟢 Low | Varies | Not started |

---

## 🚀 Recommended Execution Order

**Week 1: Visual Quality (Quick Wins)**
1. Replace remaining hardcoded colors (1h)
2. Add focus states (30m)
3. Standardize border radius (1-2h)

**Week 2: UX Polish**
4. Add hover states (1-2h)
5. Create ErrorBanner component (1h)
6. Test collaboration cursors (30m)

**Week 3: Features**
7. Notebook title/description header (2-3h)
8. Add loading skeletons (2h)
9. Create Tooltip component (1h)

**As Needed:**
10. IMPROVEMENTS.md items (#5, #13, #17, #26, #28)

---

## Next Actions

1. **Start with Task 1**: Replace remaining 18 hardcoded colors
   - Most impactful
   - Quick win
   - Improves StatusBadge immediately

2. **Then Task 2**: Add focus states
   - Accessibility requirement
   - 30 minutes
   - High impact

3. **Then Task 3**: Standardize border radius
   - Visual consistency
   - 1-2 hours
   - Low effort, medium impact