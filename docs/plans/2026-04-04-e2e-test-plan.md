# End-to-End Test Plan — Full Feature Coverage

**Date:** 2026-04-04  
**Scope:** All shipped features as of this date  
**Tool:** agent-browser (navigate, click, fill forms, assert UI state)  
**Base URL:** http://localhost:5173 (Vite dev server)

---

## Prerequisites

```bash
docker compose -f docker-compose.dev.yml up -d   # start infra
task dev                                           # Go API on :8080
task dev:web                                       # Vite on :5173
task dev:relay                                     # Hocuspocus on :3001
```

Have two browser sessions ready (or use incognito) to test multi-user scenarios.

---

## Test Suite 1 — Authentication

### T1.1 — Registration
1. Navigate to `http://localhost:5173`
2. Should redirect to `/login`
3. Click "Create account" (or navigate to `/register`)
4. Fill: name = "Alice Admin", email = `alice@test.com`, password = `password123`
5. Submit
6. **Assert**: Redirected to `/` (file browser)
7. **Assert**: Sidebar visible with nav items

### T1.2 — Login / Logout
1. Click user avatar in TopBar
2. Click "Sign out"
3. **Assert**: Redirected to `/login`
4. Log back in with the same credentials
5. **Assert**: Back on `/`

### T1.3 — Second user registration (editor)
1. Open incognito / second browser
2. Register: name = "Bob Editor", email = `bob@test.com`, password = `password123`
3. **Assert**: Bob lands on `/`
4. **Assert**: Bob has his own home folder visible in the file browser

---

## Test Suite 2 — File Browser (HomePage)

### T2.1 — Root view
1. Log in as Alice
2. Navigate to `http://localhost:5173/`
3. **Assert**: Breadcrumb shows "Files" (no sub-segments)
4. **Assert**: Alice's Home folder is visible in the Folders grid
5. **Assert**: Toolbar shows "+ New Folder", "+ New Notebook", "+ New Dashboard"

### T2.2 — Create folder
1. Click "+ New Folder"
2. **Assert**: Inline create form appears with input
3. Type "Engineering", press Enter
4. **Assert**: "Engineering" folder appears in Folders grid

### T2.3 — Navigate into folder
1. Click the "Engineering" folder card
2. **Assert**: URL changes to `?folder=<uuid>`
3. **Assert**: Breadcrumb shows "Files / Engineering"
4. **Assert**: Folder contents area is empty (empty state shown)

### T2.4 — Create notebook inside folder
1. While inside "Engineering" folder, click "+ New Notebook"
2. Type "Q1 Report", press Enter
3. **Assert**: Navigated to `/notebooks/<id>` (notebook opens automatically)
4. Go back to `http://localhost:5173/?folder=<engineering-id>`
5. **Assert**: "Q1 Report" appears in Notebooks section

### T2.5 — Breadcrumb navigation
1. Click "Files" in breadcrumb
2. **Assert**: URL becomes `http://localhost:5173/` (no folder param)
3. **Assert**: Root contents shown

### T2.6 — Context menu — Rename folder
1. In root, hover over "Engineering" folder
2. Click the `⋯` button on the Engineering card
3. **Assert**: Dropdown appears with: Rename, Move to…, Permissions, Delete
4. Click "Rename"
5. **Assert**: Input appears pre-filled with "Engineering"
6. Clear and type "Backend", press Enter
7. **Assert**: Folder card now shows "Backend"

### T2.7 — Context menu — Move to…
1. Create a second folder "Frontend" at root
2. Navigate into "Backend" folder
3. Click `⋯` on "Q1 Report" notebook
4. Click "Move to…"
5. **Assert**: Modal opens with folder tree showing root folders
6. Click "Frontend" folder in picker
7. **Assert**: "Frontend" row is highlighted / drilled into
8. Click "Move here"
9. **Assert**: Modal closes, "Q1 Report" is no longer in Backend
10. Navigate to Frontend folder, **Assert**: "Q1 Report" is there

### T2.8 — Delete folder (force)
1. Click `⋯` on "Frontend" folder at root
2. Click "Delete"
3. **Assert**: Folder disappears from the grid

### T2.9 — Create dashboard from file browser
1. Click "+ New Dashboard"
2. Type "Sales Overview", press Enter
3. **Assert**: Navigated to `/dashboards/<id>`

### T2.10 — Home folder is private
1. Log in as Bob in second browser
2. Navigate to root
3. **Assert**: Bob can see his own "Bob's Home" folder
4. **Assert**: Bob cannot see "Alice's Home" in the root listing (private by default)

---

## Test Suite 3 — Notebooks

### T3.1 — Create and auto-open
1. From file browser, click "+ New Notebook"
2. Type "My Notebook", press Enter
3. **Assert**: Immediately navigated to `/notebooks/<id>`

### T3.2 — Default connector auto-assign
1. Go to `/connectors`
2. Create a connector named "Local DB" (type: Postgres, any valid creds), mark as default
3. Create a new notebook
4. **Assert**: Notebook opens with "Local DB" pre-selected as connector

### T3.3 — Cell execution and output
1. Open a notebook
2. Click "+ Add SQL cell"
3. Type `SELECT 1 AS val`
4. Press Ctrl+Enter (or click Run)
5. **Assert**: Output table appears with column "val", value "1"

### T3.4 — Multi-cell slide (presentation mode)
1. In a notebook, add 3 code cells
2. On cell 2, click the `⋯` or settings → enable "Slide break"
3. Enter presentation mode
4. **Assert**: Slide 1 contains cell 1 only
5. **Assert**: Slide 2 contains cells 2 and 3

### T3.5 — Notebook title/description UX
1. Open a notebook
2. **Assert**: Back button, title, and description are in a clear layout (not crammed next to each other)
3. Click title, edit it, press Enter
4. **Assert**: Title updates

---

## Test Suite 4 — Connectors

### T4.1 — Create connector with default flag
1. Go to `/connectors`
2. Click "New Connector"
3. Fill name "Prod DB", type "Postgres", fill connection details
4. Check "Set as default"
5. Submit
6. **Assert**: Connector appears in list with "default" badge

### T4.2 — Only one default at a time
1. Create a second connector "Staging DB", set as default
2. **Assert**: "Prod DB" no longer has the default badge
3. **Assert**: "Staging DB" now has the default badge

### T4.3 — connector folder_id
1. In file browser, create a "Data" folder
2. Navigate into it
3. Create a notebook (to confirm folder_id wiring)
4. Go to Connectors page, click `⋯` on a connector → Move to… → select "Data"
5. Navigate to "Data" folder in file browser
6. **Assert**: Connector appears in Connectors section of that folder

---

## Test Suite 5 — Dashboards

### T5.1 — Create dashboard and add widget
1. Navigate to `/dashboards`
2. Create "Sales Dashboard"
3. Open it
4. Click "Add Widget"
5. Select a notebook cell that has output
6. **Assert**: Widget appears on the grid

### T5.2 — Resize widget (grid system)
1. In a dashboard with a widget
2. Drag the resize handle of the widget
3. **Assert**: Widget resizes on the grid

### T5.3 — Move widget via drag
1. Drag a widget to a different grid position
2. **Assert**: Widget snaps to new grid position
3. Reload the page
4. **Assert**: Widget position persisted

---

## Test Suite 6 — Profile

### T6.1 — Edit name and status
1. Navigate to `/profile`
2. Edit name field, save
3. **Assert**: TopBar avatar initials update
4. Edit status field, save
5. **Assert**: Status visible on profile

### T6.2 — Theme toggle
1. On profile page, toggle theme from Light to Dark
2. **Assert**: UI switches to dark theme (background, nav colors)
3. Refresh page
4. **Assert**: Dark theme persisted

---

## Test Suite 7 — Permissions

### T7.1 — Permissions panel opens
1. Log in as Alice
2. In file browser, click `⋯` on a notebook
3. Click "Permissions"
4. **Assert**: Slide-over drawer opens from right
5. **Assert**: Header shows notebook name and type badge
6. **Assert**: "No inherited permissions" or inheritance count shown

### T7.2 — Grant view access to Bob
1. With Permissions panel open on a notebook
2. In "Add entry" row, select Bob from the Users dropdown
3. Check "view" action
4. Click "Add"
5. Click "Save"
6. **Assert**: Bob appears in entries list with "view" checked
7. Log in as Bob, navigate to that notebook URL
8. **Assert**: Bob can view the notebook

### T7.3 — Deny access via folder ACL
1. As Alice, open Permissions panel on "Backend" folder
2. Add ACL entry: org_role "viewer" → no actions (or explicitly deny by not granting)
3. Save
4. Log in as Bob (role: editor by default)
5. Navigate to root file browser
6. **Assert**: "Backend" folder is not visible to Bob (or 403 when navigating to it directly)

### T7.4 — Folder permission inheritance
1. As Alice, on "Backend" folder, grant Bob "view" and "create"
2. Navigate into Backend, open Permissions on a notebook inside
3. **Assert**: Inheritance note shows "Inheriting 1 permission from parent folder"
4. As Bob, navigate to the Backend folder
5. **Assert**: Bob can see folder contents

### T7.5 — Manage permission required for ACL changes
1. Log in as Bob
2. Try to navigate directly to a notebook owned by Alice (where Bob has view only)
3. Open Permissions panel on it
4. **Assert**: "Manage" / "Share" permission required — PUT returns 403, or Save button is disabled

### T7.6 — ACL overrides org role
1. Alice sets Backend folder ACL: only Alice has access (no org_role entry)
2. Bob has editor org role
3. As Bob, try to navigate to Backend folder URL directly
4. **Assert**: 403 / not visible (ACL suppresses org-role fallback)

---

## Test Suite 8 — Groups

### T8.1 — View groups page (all members)
1. Log in as Bob (non-admin)
2. Navigate to `/groups`
3. **Assert**: Groups page loads, shows existing groups
4. **Assert**: No "+ New Group" button visible

### T8.2 — Create group (admin only)
1. Log in as Alice (admin)
2. Navigate to `/groups`
3. **Assert**: "+ New Group" button visible
4. Click it, type "Data Team", press Enter
5. **Assert**: "Data Team" appears in groups list with member count 0

### T8.3 — Add member to group
1. Click "Data Team" row to expand
2. **Assert**: Member list shown (empty)
3. Select Bob from "Add member" dropdown, click Add
4. **Assert**: Bob appears in member list
5. **Assert**: Member count updates to 1

### T8.4 — Remove member from group
1. With "Data Team" expanded, click × next to Bob
2. **Assert**: Bob removed from list
3. **Assert**: Member count updates to 0

### T8.5 — Rename group
1. Click "Rename" on "Data Team"
2. Clear and type "Analytics Team", press Enter
3. **Assert**: Group row shows "Analytics Team"

### T8.6 — Delete group
1. Click "Delete" on "Analytics Team"
2. Confirm the dialog
3. **Assert**: Group removed from list

### T8.7 — Use group in permissions
1. Create "Eng Team" group, add Bob as member
2. Open Permissions panel on a notebook
3. In "Add entry", select "Eng Team" from Groups optgroup
4. Grant "view" + "run", Save
5. As Bob, verify access to the notebook

### T8.8 — Sidebar admin badge
1. Log in as Alice (admin)
2. Expand sidebar
3. **Assert**: "Groups" link has an "Admin" badge pill
4. Log in as Bob (non-admin), expand sidebar
5. **Assert**: "Groups" link has NO admin badge

---

## Test Suite 9 — Audit Log

### T9.1 — Actions are logged
1. As Alice, create a notebook, edit it, delete it
2. Navigate to `/audit`
3. **Assert**: Recent actions (notebook.create, notebook.update, notebook.delete) are listed
4. **Assert**: Correct user and timestamp shown

---

## Test Suite 10 — Regression (existing features)

### T10.1 — Real-time collaboration
1. Alice and Bob both open the same notebook
2. Alice types in a code cell
3. **Assert**: Bob sees Alice's typing in real-time (Yjs sync)

### T10.2 — Scheduled notebook run
1. Create a notebook with a code cell
2. Add a schedule (every minute cron)
3. Wait for next run
4. **Assert**: "Last run" timestamp updates

### T10.3 — Notebook parameters
1. Create a notebook with a parameter (name: `limit`, default: `10`)
2. Add cell: `SELECT * FROM foo LIMIT {{ limit }}`
3. In ParametersBar, change limit to 5
4. Run the cell
5. **Assert**: Query uses the overridden parameter value

### T10.4 — Cell version history
1. Edit a cell source multiple times
2. Open History panel
3. **Assert**: Multiple versions listed
4. Click a version to restore
5. **Assert**: Cell source reverts

---

## Notes

- All permission tests require at minimum one admin and one non-admin user
- Tests T7.x and T8.x depend on T1.3 (Bob registered) and T8.2 (admin creates groups)
- Dashboard drag/resize (T5.2, T5.3) requires react-grid-layout to be wired correctly — if widgets don't snap, check widget layout save endpoint
- The `⋯` menu on connectors and dashboards shows Delete but it is a no-op (manage from their dedicated pages)
