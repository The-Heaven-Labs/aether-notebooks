# UX Audit Findings - Grouped for Solution Design

## Group 1: Feedback & Confirmations (8 items)
Issues where the app doesn't tell users what happened or doesn't confirm destructive actions.

1. **Running a cell without a connector gives zero feedback** — No error when clicking Run without a connector selected
2. **Connector "Test Connection" button gives no feedback** — No loading/success/error indication
3. **Invite link generation shows no visible link** — Generated link is never displayed to user
4. **Profile page Save button has no feedback** — No confirmation after saving
5. **No confirmation dialog for destructive actions** — Delete on connectors/notebooks/dashboards/groups has no confirmation
6. **Connector status shows "—" instead of meaningful status** — Status only updates after manual Test click
7. **No "Last saved" indicator on notebook** — Auto-save status not visible in notebook header
8. **Group rename/delete buttons always visible** — Should be in context menu to prevent accidents

## Group 2: Navigation & Information Architecture (9 items)
Issues with how users move through the app and find their way back.

1. **Duplicate "+ New Notebook" buttons on Home page** — Redundant buttons in folder tree and main content
2. **Duplicate "+ New Dashboard" buttons on Dashboards page** — Same issue
3. **No way to navigate back from dashboard editor** — No breadcrumb or back link
4. **Notebook breadcrumb shows "Notebooks" but goes to Files** — Misleading label
5. **No loading skeleton on page transitions** — Brief flash of empty content
6. **Folder tree collapse button doesn't visually change** — Button text doesn't update
7. **Context menu doesn't close on outside click** — Requires Escape key
8. **No keyboard shortcut documentation** — No "?" shortcut or shortcuts modal
9. **Search box doesn't clear with Escape** — Expected behavior missing

## Group 3: Notebook & Cell Experience (10 items)
Issues specific to the core notebook editing experience.

1. **Schema Browser doesn't auto-detect notebook connector** — Forces re-selection
2. **Cell title "Untitled" not auto-populated** — No prompt to name cells
3. **No visual distinction between code and text cells** — Look too similar when not focused
4. **"Join with previous slide" button has no explanation** — No tooltip
5. **Parameters panel has no description** — Doesn't explain what parameters are
6. **Schedule panel cron input has no helper** — No cron reference or human-readable preview
7. **No drag-and-drop reordering of cells** — Common expectation for notebook UX
8. **Text cell editor has no live markdown preview** — Must click away to see rendered output
9. **Connector selector shows "— None —" inconsistently** — Different from placeholder text
10. **Notebook description doesn't support markdown** — Users might expect formatting

## Group 4: Empty States & Onboarding (3 items)
Issues with first-time user experience and empty page guidance.

1. **No empty state illustrations** — Plain text only on Connectors/Agents/Models/Skills/MCPs
2. **Chart view shows empty state with no guidance** — No "Configure chart" prompt
3. **OIDC provider form has no "Test" button** — Can't validate before saving

## Group 5: Accessibility & Focus Management (7 items)
Issues that affect screen reader users and keyboard-only users.

1. **Sidebar has no aria-current="page"** — Active link not announced
2. **Code editor (CodeMirror) is not labeled** — No accessible name
3. **Cell action buttons lack grouping** — Not wrapped in toolbar role
4. **Dashboard column width buttons have no accessible names** — Just numbers
5. **Color-only status indicators** — No text alternatives
6. **Focus trap not implemented in modals** — Tab moves outside dialog
7. **No "skip to content" link on inner pages** — Only on login page

## Group 6: Responsive & Mobile (4 items)
Issues with the app on smaller screens.

1. **Mobile viewport doesn't auto-collapse sidebar** — Takes up most of screen at 375px
2. **Sidebar overlaps content on tablet widths** — Content becomes too narrow
3. **Connector creation form overflows on small viewports** — No scroll indication
4. **Dashboard editor is unusable on mobile** — Grid layout doesn't adapt

## Group 7: Data Management & Export (4 items)
Issues with managing and exporting data.

1. **No pagination on audit log** — All entries in single page
2. **No bulk actions on file list** — Can't multi-select for move/delete
3. **No export options for notebook or query results** — No CSV/Excel/PDF export
4. **Audit log resource IDs truncated with no easy copy** — Clicking copies but no visual indication

## Group 8: Polish & Minor UX (3 items)
Small quality-of-life improvements.

1. **Profile status field has no character limit indicator** — No max length shown
2. **No visual indicator of default connector** — "Set default" button but no badge/star
3. **Dashboard widget column buttons have no labels** — Numbers without tooltips
