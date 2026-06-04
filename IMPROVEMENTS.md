## Implemented

- [x] If I'm a viewer and access one notebook, I can start editing it but when leaving I receive insufficient permissions. If the user has no permission, cells should be read-only. Same goes for the permissions menu, I can start editing and even save permissions for a notebook I have no permission, but it should be blocked from even starting
- [x] In the groups page, there is no feedback if I do not fill the new group name and press the create button. Same trying to add someone to a group but not selecting a person
- [x] On the settings page, pressing the Save button has no feedback
- [x] In the connectors page, if I click test it will update the status, but clicking again shows no feedback and nothing changes
- [x] When logging-in, if the user does not authenticate with SSO, the password field should have focus
- [x] The settings page flashes quickly on load then turns all white
- [x] Contrast of the left menu items on the light theme with the dark lateral menu background color
- [x] Contrast of the cursor in code cells in the dark theme is not good. Cursor is dark on the dark background.
- [x] Code cells should have a default LIMIT 1000 config that is applied to queries. It should be visible in the UI and have a Unlimited option
- [x] Contrast for color highlighting of code cells in the dark theme are not great, I think it misses saturation for the colors, look too much like white
- [x] Add a default No Access role to go along admin/editor/viewer
- [x] In the audit view, actions made to notebooks show the notebook name and the first part of the id. While a tooltip appears showing the full id, there is no way to copy the full id.
- [x] Adding an user to a group does not create an audit action same for removing
- [x] Removing an user from a group does not ask for confirmation on the UI
- [x] When creating an account, I can either create a new org or paste an invite token. But it seems there is no way to generate such token?
- [x] The navigation for items created should have a "home" and from this home, every user home folder should be in it, and thus it should be possible for users to (given they have access) navigate other users' folders
- [x] Currently there is no way to change permissions on who can access/use/etc on a connector.
- [x] In the agent chat, there is no way to access history for past sessions
- [x] Pressing "/" in the chat should open a picker for the possible slash commands
- [x] Clicking on the AI button should open the chat with a default agent. This default should be configurable
- [x] When the LLM is working, I can't right away send a message to be queued, it is blocked
- [x] When one message is sent and the chat input is blocked, the form goes out of focus, and I need to click it again to type the next message
- [x] There should be native tool calls to list notebooks, connectors, folders and everything the user has access in the platform
- [x] When agent panel is opened, the input form should be focused automatically
- [x] There should be a native tool call for data schema exploration
- [x] There should be native tools for task tracking
- [x] When the agent creates a new cell, it should be highlighted and scrolled immediatly, not only when that part of the session/command ends
- [x] Instead of MCPs being configured at agent level, they should be configured at application level, and then could be shared between multiple agents
- [x] There should be a way to copy the whole agent conversation as markdown
- [x] The agent panel should be resizable. When opening it, the things behind (cells) should move to the left, instead of being behind it
- [x] There is a bug with the queue of next messages, it is sent again and again repeating itself, never ending.
- [x] Slash commands (/new, /skills, /agents, /summarize) now work. /new starts a fresh session with the same agent. /summarize generates a summary, creates a new session with it as context, and shows a loading indicator during processing.
- [x] UI/UX improvements (2026-06-03):
  - **Landing Page**: "Continue" button renamed to "Sign In", added "Skip to form" link for keyboard users, password field now has show/hide toggle and strength indicator (Weak/Fair/Strong)
  - **Create Account**: Added password visibility toggle and strength indicator
  - **Org Setup**: Placeholder changed to "e.g., Acme Analytics", back button simplified to "Back" (removed unicode arrow)
  - **Files/Home**: Added `aria-label="More options"` to all context menu buttons for screen readers
  - **Notebook View**: Description placeholder clarified to "Add a description for this notebook...", connector placeholder changed to "Select a connector", removed leading space from "Run All" button
  - **Cell**: "no connector" button renamed to "Select connector", added "Row limit:" label before LIMIT dropdown, MD/SQL toggle buttons now have descriptive tooltips ("Convert to Markdown cell" / "Convert to SQL cell")
  - **Groups**: Replaced "▶" character with ChevronRight icon, added `aria-expanded` to expand button, added ability to select initial members during group creation
  - **Members**: "No_access" role now displays as "No Access", "YOU" badge changed to "You" (proper casing), added tooltip explaining why self-remove is disabled
  - **Permissions Panel**: Added `<h2>` heading with resource name + "permissions", added `role="dialog"` and `aria-labelledby`, close button has `aria-label="Close permissions dialog"`, checkbox labels now show full descriptions (e.g., "See notebook content and cell outputs")
  - **Dashboards**: Layout toggle button text clarified to "Switch to grid view" / "Switch to list view"
  - **Audit**: Added sortable column headers (Timestamp, Action, Resource Type), added timezone indicator to timestamps (e.g., "BRT"), simplified ID display (clickable to copy, removed redundant copy button), replaced hardcoded "white" with `var(--bg-card)`
  - **Agents**: Empty state now includes explanation: "Agents are AI assistants that can run queries, analyze data, and build notebooks for you"
  - **Global**: Fixed settings page flash by moving theme initialization to inline `<script>` in `<head>`, added `aria-expanded` to folder tree toggle buttons

## Not Yet Implemented

### UX Audit — 2026-06-03 (Comprehensive Manual Testing)

The following issues were identified by systematically navigating every page, clicking every button, filling every form, and testing every feature in the application.

---

#### 🔴 Critical / High Impact

- [ ] **Running a cell without a connector gives zero feedback** — When clicking "Run" on a code cell that has no connector selected (neither at notebook level nor cell level), nothing happens. No error message, no toast, no visual indication. The user has no idea why the query didn't execute. Should show an inline error or toast: "Please select a connector to run queries."

- [ ] **Schema Browser doesn't auto-detect notebook's selected connector** — When opening the Schema Browser panel in a notebook that already has "Dev Postgres" selected as its connector, the panel says "Select a connector to browse its schema". It should automatically use the notebook-level connector.

- [ ] **Invite link generation shows no visible link or copy button** — After clicking "Generate" in the Invite Link section on the Members page, a "Dismiss" button appears (presumably a toast), but the generated link is never displayed to the user. There's no way to see or copy the invite token. The link/token should appear in an input field with a copy button.

- [ ] **Connector "Test Connection" button in creation form gives no feedback** — When creating a new connector and clicking "Test Connection", there is no loading indicator, no success message, and no error message. The button appears to do nothing. Should show a loading spinner, then success/failure result.

- [ ] **Duplicate "+ New Notebook" buttons on Home page** — The home page shows two identical "+ New Notebook" buttons: one in the folder tree toolbar and one in the main content area. This is confusing and redundant. The folder tree toolbar buttons (+ New Folder, + New Notebook, + New Dashboard) duplicate the main content area actions.

- [ ] **Duplicate "+ New Dashboard" buttons on Dashboards page** — Same issue as above: two "+ New Dashboard" buttons appear, one in the sidebar area and one in the main content header.

---

#### 🟡 Medium Impact

- [ ] **Mobile viewport doesn't auto-collapse sidebar** — At 375px width (iPhone), the sidebar remains fully expanded, taking up most of the screen real estate. The sidebar should auto-collapse or become a hamburger menu on small viewports.

- [ ] **Folder tree collapse button doesn't visually collapse the tree** — Clicking "Collapse folder tree" button doesn't appear to change the visual state of the folder tree panel. The button text remains "Collapse folder tree" (doesn't change to "Expand folder tree"). Need to verify if this actually works or is broken.

- [ ] **Profile page Save button has no feedback** — After changing the name or status on the Profile page and clicking "Save", there is no success confirmation (toast, inline message, or button state change). The user doesn't know if the save succeeded.

- [ ] **Chart view shows empty state with no guidance** — When switching to "Chart" view on a cell with only 1 column, it shows "Need at least 2 columns to chart" which is good. But when switching to Chart on a cell with data but no chart configuration, it shows a blank area with no prompt to configure the chart (no "Configure chart" button or instructions).

- [ ] **No keyboard shortcut documentation accessible** — There's no visible way to access keyboard shortcuts (no "?" shortcut, no shortcuts modal accessible from the UI). The code cell shows "Run (Ctrl+Enter)" in the button tooltip, but there's no comprehensive shortcut reference.

- [ ] **Cell title "Untitled" is not auto-populated** — New code cells default to "Untitled" and new text cells also default to "Untitled". There's no prompt or placeholder suggesting the user name their cells. For notebooks with many cells, this makes navigation difficult.

- [ ] **No confirmation dialog for destructive actions on connectors** — Clicking "Delete" on a connector immediately deletes it without asking for confirmation. Same for "Delete" on notebooks, dashboards, and groups. These destructive actions should require confirmation.

- [ ] **Audit log resource IDs are truncated with no easy way to copy full ID** — The audit log shows truncated resource IDs (e.g., "1316071a…"). While clicking copies the ID, there's no visual indication that clicking will copy, and the full ID is never shown even in a tooltip.

- [ ] **No pagination or infinite scroll on audit log** — The audit log shows all entries in a single page. For organizations with many actions, this will become unusable. Should have pagination or virtualized scrolling.

- [ ] **No empty state illustration or guidance on empty pages** — Connectors, Agents, Models, Skills, MCPs pages all show plain text empty states ("No X yet."). They lack visual illustrations, examples, or guided onboarding steps that would help new users understand what these features do and how to get started.

- [ ] **Connector status shows "—" instead of a meaningful status** — After creating a connector, the status column shows "—" instead of "Connected" or a status badge. The status only updates after clicking "Test". Should auto-test on creation or show a pending state.

- [ ] **No way to navigate back from dashboard editor to dashboard list** — The dashboard editor page has no breadcrumb or "Back to Dashboards" link. The only way back is to click "Dashboards" in the sidebar. A breadcrumb like "Dashboards / Test Dashboard" would improve navigation.

- [ ] **Notebook breadcrumb shows "Notebooks" but it's not a clickable path** — The notebook page shows a "Notebooks" link that navigates back to the home/files page, but it's labeled "Notebooks" which implies it would go to a notebooks list. It should say "Files" or "Home" to match the actual destination.

---

#### 🟢 Low Impact / Polish

- [ ] **Search box doesn't clear with Escape key** — When typing in the "Search files" search box, pressing Escape doesn't clear the search. Users expect Escape to clear the search input.

- [ ] **No loading skeleton/spinner on page transitions** — When navigating between pages (e.g., Files → Connectors → Members), there's a brief flash of empty content before data loads. Should show skeleton loaders or a page-level loading indicator.

- [ ] **Context menu doesn't close on outside click in some cases** — The "More options" context menu on file items sometimes requires pressing Escape rather than clicking outside to close.

- [ ] **Dashboard widget column buttons (6, 8, 12, 16, 24) have no labels** — The buttons showing numbers like "6", "8", "12", "16", "24" in the dashboard editor have no tooltip or label explaining what they do (they control the column width of the next widget). Should have tooltips like "Set widget width to 6 columns".

- [ ] **No visual distinction between code and text cells at a glance** — Code cells and text cells look very similar when not focused. The only difference is the cell type toggle button. Should have a more distinct visual indicator (e.g., different border color, icon, or background).

- [ ] **"Join with previous slide" button has no explanation** — The "Join with previous slide" button in the cell toolbar has no tooltip explaining what it does or when to use it. It's related to presentation mode but this isn't obvious.

- [ ] **Parameters panel "+ Add" button has no description** — The Parameters panel shows an "+ Add" button but doesn't explain what parameters are or how they're used in queries (e.g., `{{param_name}}` syntax).

- [ ] **Schedule panel cron input has no helper or examples** — The Schedules panel has a cron expression input with placeholder "e.g. 0 9 * * 1" but no link to a cron reference, no human-readable preview of what the schedule means, and no timezone indicator.

- [ ] **No drag-and-drop reordering of cells** — Cells cannot be reordered by drag-and-drop. The only way to move cells is likely through keyboard shortcuts or cut/paste. Drag handles would improve the editing experience.

- [ ] **Text cell editor doesn't show markdown preview while editing** — Text cells use a plain editor without live markdown preview. Users have to click away to see the rendered output. A split-pane or inline preview would improve the experience.

- [ ] **No "Last saved" indicator on notebook** — The notebook page shows "Last updated Just now" in the sidebar but not prominently in the notebook header. Users editing notebooks want to see auto-save status.

- [ ] **Connector selector dropdown shows "— None —" as first option** — The notebook-level connector dropdown shows "— None —" as the first option with an em-dash. This is inconsistent with the placeholder "Select a connector" shown when no connector is selected. Should be consistent.

- [ ] **Group rename and delete buttons are always visible** — On the Groups page, the "Rename" and "Delete" buttons for a group are always visible when the group is expanded. They should be in a context menu or at least styled as secondary/danger actions to prevent accidental clicks.

- [ ] **No bulk actions on file list** — The home/files page doesn't support multi-select or bulk actions (move, delete, change permissions). For users with many notebooks, this makes management tedious.

- [ ] **Profile status field has no character limit indicator** — The "Status (optional)" field on the Profile page has no indication of maximum length or current character count.

- [ ] **OIDC provider form has no "Test" or "Validate" button** — When adding an OIDC provider in Settings, there's no way to test the configuration before saving. Users have to save and then try logging in to verify it works.

- [ ] **No visual indicator of which connector is the default** — The connector list shows a "Set default" button but doesn't visually indicate which connector is currently the default. Should show a badge or star icon.

- [ ] **Notebook description field doesn't support markdown** — The notebook description is a plain text input. Users might expect to format it with markdown since the rest of the app supports it.

- [ ] **No export options for notebook or query results** — There's no way to export a notebook as PDF/HTML or export query results as CSV/Excel from the UI. These are common features in notebook platforms.

---

#### ♿ Accessibility

- [ ] **Sidebar navigation has no current-page indicator for screen readers** — The active sidebar link is visually highlighted but has no `aria-current="page"` attribute for screen readers.

- [ ] **Code editor (CodeMirror) is not labeled** — The code editor textbox in cells has no accessible name or label. Screen readers would announce it as just "textbox" with no context.

- [ ] **Cell action buttons lack grouping** — The cell toolbar buttons (Run, Convert, Duplicate, Copy link, Hide, Collapse, History, Add to dashboard, Join, Delete) are not wrapped in a toolbar role or group. Screen readers announce them as individual buttons with no context that they belong to a cell.

- [ ] **Dashboard editor column width buttons have no accessible names** — The buttons "6", "8", "12", "16", "24" in the dashboard editor are announced as just numbers with no context.

- [ ] **Color-only status indicators** — Connector status uses color (green for connected, red for error) without text alternatives in some views. Should always include text status.

- [ ] **Focus trap not implemented in modals** — The create notebook, create folder, create dashboard dialogs don't appear to trap focus. Tab key moves focus outside the dialog.

- [ ] **No "skip to content" link on inner pages** — The login page has "Skip to form" but the main app pages (after login) have no skip-to-content link to bypass the sidebar navigation.

---

#### 📱 Responsive / Cross-browser

- [ ] **Sidebar overlaps content on tablet widths (768px-1024px)** — At tablet widths, the sidebar doesn't collapse but the content area becomes very narrow. Should switch to an overlay sidebar at these widths.

- [ ] **Connector creation form overflows on small viewports** — The connector creation form has many fields (Name, Type, Host, Port, Database, User, Password, SSL Mode) that overflow vertically on small screens without scroll indication.

- [ ] **Dashboard editor is unusable on mobile** — The grid-based dashboard editor with column width buttons makes no sense on mobile viewports. Should switch to a vertical stack layout.

---

#### 🐛 Post-Merge Bug Fixes

- [x] **Schema browser colors don't match dark theme** — The schema browser panel had hardcoded light theme colors (`#fff`, `#e8e8e8`, `#ddd`) that didn't adapt to dark mode. Fixed by replacing with CSS variables (`--bg-primary`, `--border`, `--text-muted`).

