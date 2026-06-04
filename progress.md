# Progress

## Status
In Progress

## Tasks
- [x] Group 1: Feedback & Confirmations — Design solutions for 8 issues (completed 2026-06-04)
- [x] Group 3: Notebook & Cell Experience — Design solutions for 10 issues (completed 2026-06-04)
- [x] Group 4+8: Empty States, Onboarding & Polish — Design solutions for 6 issues (completed 2026-06-04)
- [x] Group 2: Navigation & Information Architecture — Design solutions for 9 issues (completed 2026-06-04)
- [x] Group 5: Accessibility & Focus Management — Design solutions for 7 issues (completed 2026-06-04)

- [x] Group 6+7: Responsive/Mobile & Data Management — Design solutions for 8 issues (completed 2026-06-04)

## Files Changed
- group1-feedback.md — Design doc for all 8 feedback/confirmation fixes
- docs/plans/solutions-group1-feedback.md — Same (canonical path)
- docs/plans/solutions-group3-notebook.md — Design doc for all 10 notebook/cell fixes
- group3-notebook.md — Same (alternate path)
- docs/plans/solutions-group4-empty-states.md — Design doc for all 6 fixes
- group2-navigation.md — Design doc for all 9 navigation/IA fixes
- docs/plans/solutions-group5-accessibility.md — Design doc for all 7 accessibility fixes
- group6-responsive-data.md — Design doc for all 8 responsive + data management fixes
- docs/plans/solutions-group6-responsive-data.md — Same (canonical path)

## Notes
### Group 1 Notes (2026-06-04)
- Output at group1-feedback.md and docs/plans/solutions-group1-feedback.md
- 8 issues analyzed: run-without-connector feedback, test connection feedback, invite link visibility, profile save feedback, delete confirmations, connector status "—", last-saved indicator, group action buttons
- Key findings:
  - Issue 5 (delete confirmations): HomePage.tsx handleDelete has NO confirm() — only gap; all other pages already use window.confirm()
  - Issue 4 (profile save): Already implemented with saveStatus state — just needs visual enhancement
  - Issue 3 (invite link): Link IS displayed but needs auto-copy + visual emphasis
  - Issue 2 (test connection): Status column shows result but button area lacks inline feedback
- P0 fixes (2): delete confirmations on HomePage, run-without-connector guard
- P1 fixes (3): connector status text, test connection inline badge, last-saved indicator
- P2 fixes (3): invite link auto-copy, profile save enhancement, group context menu
- All 8 fixes are frontend-only; no backend changes required
- Total estimated effort: ~2 hours

### Group 3 Notes (2026-06-04)
- Output at group3-notebook.md and docs/plans/solutions-group3-notebook.md
- 10 issues analyzed: schema auto-detect, cell title placeholder, code/text visual distinction, slide break tooltip, parameters description, cron helper, drag-and-drop reorder, markdown preview, connector selector consistency, description markdown
- 3 High priority: schema auto-detect (trivial 1-line fix), code/text visual distinction (left border accent), drag-and-drop reorder (needs @dnd-kit + backend endpoint)
- 5 Medium priority: cell title smart placeholder, slide break tooltip, parameters description, cron helper, markdown preview
- 2 Low priority: connector selector text consistency, description markdown rendering
- Quick wins (< 30 min each): Issues 1, 3, 4, 9
- 9 of 10 fixes are frontend-only; Issue 7 (drag-and-drop) requires a new backend endpoint

- Group 4+8 analysis complete. Output at docs/plans/solutions-group4-empty-states.md
- 6 issues analyzed: empty state illustrations, chart guidance, OIDC test button, status char counter, default connector badge, column button tooltips
- Total estimated effort: ~1.5 hours
- 5 of 6 fixes are frontend-only; Issue 3 (OIDC test) requires a new backend endpoint

### Group 5 Notes (2026-06-04)
- Output at docs/plans/solutions-group5-accessibility.md and group5-accessibility.md
- 7 issues analyzed: sidebar aria-current, CodeMirror label, cell toolbar grouping, dashboard column button names, color-only status, modal focus trap, skip-to-content
- 4 trivial fixes (< 10 min each): StatusBadge role="status", column button aria-labels, toolbar role, CodeMirror contentAttributes
- 2 small fixes (~15 min each): sidebar aria-current via render prop, skip-to-content link
- 1 medium fix (~30 min): modal focus trap with focus restoration
- All 7 fixes are frontend-only; no backend changes required

### Group 6+7 Notes (2026-06-04)
- Output at group6-responsive-data.md and docs/plans/solutions-group6-responsive-data.md
- 8 issues analyzed: 4 responsive (sidebar auto-collapse, tablet overlap, connector form overflow, dashboard mobile) + 4 data management (audit pagination, bulk file actions, CSV export, audit copy feedback)
- P0 fixes: sidebar auto-collapse on mobile, connector form responsive grid
- P1 fixes: audit copy feedback (trivial), CSV export (client-side), tablet sidebar
- P2 fixes: dashboard mobile layout, audit pagination (needs backend total count)
- P3: bulk file actions (largest effort)
- New shared infrastructure: useMediaQuery hook, Pagination component
- 6 of 8 fixes are frontend-only; audit pagination needs a backend total count endpoint

### Group 2 Notes (2026-06-04)
- Output at group2-navigation.md
- 9 issues analyzed: duplicate buttons (×2), dashboard back nav, breadcrumb label, loading skeletons, folder tree collapse, context menu close, keyboard shortcuts, search Escape
- 3 P0 fixes (< 5 min each): breadcrumb label, search Escape, duplicate buttons
- 3 P1 fixes (~30 min total): context menu, dashboard back nav, duplicate dashboard button
- 3 P2 fixes (~2 hrs total): loading skeletons, folder tree sections, keyboard shortcut docs
- All fixes are frontend-only; no backend changes required
- New file needed: web/src/components/Skeleton.tsx
