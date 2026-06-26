# Frontend Visual Reference

## Design System

### Colors

All colors are defined as CSS variables in `web/src/styles/theme.css`:

**Backgrounds**
- `--bg-primary: #f8f7f4` - Page background, main canvas
- `--bg-secondary: #f0ede7` - Secondary surfaces (toolbars, panels, card backgrounds)
- `--bg-cell-code: #ffffff` - Code cell background (unused, same as white)
- `--bg-cell-text: #fefefe` - Text cell background (unused, same as white)

**Text**
- `--text-primary: #1a1814` - Headings, primary text
- `--text-secondary: #6b6258` - Secondary text, descriptions
- `--text-muted: #9b9289` - Muted text, placeholders, timestamps

**Accent (Primary brand color)**
- `--accent: #7c6faa` - Primary actions, links, highlights (purple)
- `--accent-hover: #6a5e96` - Accent hover state (darker purple)
- `--accent-light: #ede9f8` - Accent background for nav active state (light purple)

**Borders**
- `--border: #e3ddd5` - Primary border color
- `--border-light: #ece8e1` - Lighter borders for separators

**Status**
- `--success: #5a9970` - Success states (green)
- `--error: #b85c5c` - Error states (red)
- `--warning: #b89a4a` - Warning states (amber)

**Navigation (Dark theme)**
- `--nav-bg: #1a1814` - Sidebar and topbar background
- `--nav-text: #e8e4dc` - Navigation text
- `--nav-border: #2e2a24` - Navigation borders

### Typography

**Font Stacks**
- `--font-sans: 'DM Sans', -apple-system, BlinkMacSystemFont, sans-serif` - UI text, headings, buttons
- `--font-mono: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace` - Code, tables, type badges

**Body**
- `font-size: 14px` (base)
- `line-height: 1.5`
- `font-family: var(--font-sans)`

**Code/Tables**
- `font-size: 13px` (monospace)
- `font-family: var(--font-mono)`

### Spacing & Layout

**Common Padding Values**
- `16px` - Standard card/cell padding
- `12px` - Input padding, button padding
- `8px` - Small component padding
- `4px` - Tight padding for status bars

**Common Gap Values**
- `8px` - Standard gap between elements
- `10px` - Gap between toolbar items
- `12px` - Gap between list items

**Common Border Radius**
- `--radius: 8px` - Standard radius (unused, for reference)
- `--radius-sm: 5px` - Small radius
- `10px` - Card/cell radius (most common)
- `6px` - Input/select radius
- `4px` - Badge/type icon radius
- `50%` - Avatar circle

### Shadows

- `--shadow-sm: 0 1px 3px rgba(0,0,0,0.06), 0 1px 2px rgba(0,0,0,0.04)` - Cards, cells, elevated surfaces
- `--shadow-md: 0 4px 16px rgba(0,0,0,0.08), 0 2px 6px rgba(0,0,0,0.04)` - Dropdowns, modals

## Layout Components

### AppShell

**Purpose**: Root layout wrapper for all authenticated pages

**Visual**: 
- Flexbox column layout, min-height viewport
- TopBar fixed at top (52px height)
- Sidebar + main content in flex row
- Sidebar on left, main content fills remaining space

**States**:
- Default: Sidebar expanded (200px wide)
- Collapsed: Sidebar collapsed (48px wide)
- No padding: Pass `noPadding` prop to remove main area padding (used for presentation mode)

**Styling**:
```javascript
root: { display: 'flex', flexDirection: 'column', minHeight: '100vh', background: 'var(--bg-primary)' }
body: { display: 'flex', flex: 1, overflow: 'hidden' }
main: { flex: 1, overflow: 'auto', padding: '32px' } // or padding: 0 with noPadding
```

**Component**: `web/src/components/AppShell.tsx`

### TopBar

**Purpose**: Header with logo, org name, and user dropdown

**Visual**:
- Dark background (`var(--nav-bg)`)
- Height: 52px, fixed at top
- Left: Logo mark (36x36 purple square with white "N" icon), brand divider, "Heaven's Notebooks" text
- Center: Spacer (flex: 1)
- Right: Org name, avatar circle with initials
- Admin link visible to platform admins only

**States**:
- Default: Org name + avatar visible
- Dropdown open: White dropdown (200px width) with name, email, "Sign out" button
- Avatar: Circle button, 30x30, `var(--accent-light)` background, `var(--accent)` border/text, initials centered

**Interactions**:
- Click avatar → dropdown toggle
- Click outside → dropdown close
- "Sign out" button → logout

**Styling**:
```javascript
bar: { height: 52, background: 'var(--nav-bg)', borderBottom: '1px solid var(--nav-border)', display: 'flex', alignItems: 'center', padding: '0 16px 0 8px', gap: 12 }
avatar: { width: 30, height: 30, borderRadius: '50%', background: 'var(--accent-light)', border: '1.5px solid var(--accent)', color: 'var(--accent)', fontSize: 13, fontWeight: 700 }
dropdown: { position: 'absolute', right: 0, top: 38, background: 'white', border: '1px solid var(--border)', borderRadius: 8, boxShadow: '0 4px 16px rgba(0,0,0,0.12)', minWidth: 200 }
```

**Component**: `web/src/components/TopBar.tsx`

### Sidebar

**Purpose**: Left navigation rail

**Visual**:
- Dark background (`var(--nav-bg)`)
- Width: 48px collapsed | 200px expanded
- Transition: width 0.2s ease
- Nav items: vertical stack, icons centered, text appears on expand
- Bottom: Chevron toggle button (collapse/expand)

**Nav Items** (top to bottom):
1. Files / Notebooks (BookOpen icon) — routes to `/` (file browser)
2. Dashboards (LayoutDashboard icon)
3. Connectors (Database icon)
4. Members (Users icon)
5. Groups (UsersRound icon) — shows "Admin" pill badge when user is admin + sidebar is expanded
6. Audit (ClipboardList icon)
7. Profile (User icon)

**States**:
- Collapsed: Width 48px, icons only (centered), padding: 8px 0
- Expanded: Width 200px, icons + labels, padding: 8px 12px
- Hover: Background `var(--accent-light)` (subtle)
- Active: `background: var(--accent-light)`, `color: var(--accent)`, active link highlights current page
- Toggle button: Bottom, transparent background, border top, chevron icon (direction based on state)

**Interactions**:
- Click nav item → navigate to page
- Click toggle → expand/collapse sidebar
- State persisted in localStorage (`hnb_sidebar_expanded`)

**Styling**:
```javascript
sidebar: { display: 'flex', flexDirection: 'column', background: 'var(--nav-bg)', borderRight: '1px solid var(--nav-border)', transition: 'width 0.2s ease' }
item: { display: 'flex', alignItems: 'center', gap: 10, padding: expanded ? '8px 12px' : '8px 0', fontWeight: 500, fontSize: 13, borderRadius: 6 }
toggle: { display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '12px', background: 'transparent', border: 'none', borderTop: '1px solid var(--nav-border)', color: 'var(--text-muted)' }
```

**Component**: `web/src/components/Sidebar.tsx`

## Cell Components

### CodeCell

**Purpose**: SQL editor cell with toolbar, header, code editor, output, and status bar

**Visual**:
- White card, `border-radius: 10px`, `box-shadow: var(--shadow-sm)`
- Border: `1px solid var(--border)`
- Vertical layout: Toolbar → Header → Editor → Output → Status bar

**States**:
- **Default (editing)**: Toolbar visible, header visible, editor visible, output visible (if exists), status bar visible
- **Running**: Toolbar shows spinner icon + "Running" text in run button (grayed out)
- **Collapsed**: Gray background (`var(--bg-secondary)`), shows title or "Code cell" in italics, "Expand" button on right, compact height (no editor/output visible)
- **Source hidden**: Toolbar + Header + Output visible, editor hidden (toggle with EyeOff icon)
- **Saving**: Status bar shows "Saving…" text
- **Saved**: Status bar shows "Saved Xs ago" / "Saved Xm ago" / "Saved HH:MM"
- **Save error**: Status bar shows "Save failed: {error}" in red (`var(--error)`)

**Parts**:
1. **CellToolbar**: Gray toolbar with run button, type badge, connector dropdown, action icons
2. **CellHeader**: Title + description inputs (collapsible cell shows title in collapsed state)
3. **Editor**: CodeMirror editor, monospace font, padding 14x16px, min-height 72px, light yellow background (`#fdfcfb`)
4. **Output**: Table or chart or error display
5. **Status bar**: Light background (`#faf9f7`), border top, shows Save time + Last run time

**Collaboration**:
- Uses Yjs for real-time collaboration
- Connects to Hocuspocus relay via WebSocket
- Remote cursors show user names + colored highlights

**Interactions**:
- Ctrl+Enter / Cmd+Enter → Run cell
- Ctrl+Shift+F / Cmd+Shift+F → Format SQL
- Editor auto-saves on every keystroke (debounced 1.5s)
- Click cell → Focus cell (passed up to parent via `onFocus`)

**Styling**:
```javascript
cell: { border: '1px solid var(--border)', borderRadius: 10, background: 'white', overflow: 'hidden', boxShadow: 'var(--shadow-sm)' }
cellCollapsed: { border: '1px solid var(--border)', borderRadius: 10, background: 'var(--bg-secondary)', padding: '6px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }
editor: { borderBottom: '1px solid var(--border-light)', background: '#fdfcfb' }
statusBar: { display: 'flex', justifyContent: 'space-between', padding: '4px 16px', fontSize: 11, background: '#faf9f7', borderTop: '1px solid var(--border-light)' }
```

**Component**: `web/src/components/CodeCell.tsx`

### TextCell

**Purpose**: Markdown editor cell with live preview

**Visual**:
- Same white card styling as CodeCell
- Border radius 10px, shadow-sm, border

**States**:
- **Default (editing)**: Toolbar + Editor visible, current line shows markdown syntax, other lines rendered as HTML
- **Collapsed**: Gray background, shows title or "Markdown cell" in italics, "Expand" button on right
- **Source hidden**: Toolbar visible, editor hidden (markdown rendered as output)
- Blurring editor → Save content to backend

**Live Preview Mechanism**:
- Current active line: Shows markdown source syntax
- Inactive lines: Replaced with rendered HTML widgets using ReactMarkdown
- No "preview mode" toggle needed – live preview always on except for current editing line

**Image Paste**:
- Ctrl+V with image in clipboard → Inserts `![pasted image](data:image/...;base64,...)`

**Styling**:
```javascript
cell: { border: '1px solid var(--border)', borderRadius: 10, background: 'white', overflow: 'hidden', boxShadow: 'var(--shadow-sm)' }
collapsed: { border: '1px solid var(--border)', borderRadius: 10, background: 'var(--bg-secondary)', padding: '6px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }
editor: { padding: '14px 16px', minHeight: '80px' } // CodeMirror editor container
```

**Component**: `web/src/components/TextCell.tsx`

### CellToolbar

**Purpose**: Unified toolbar for CodeCell and TextCell

**Visual**:
- Background: `var(--bg-secondary)` (light gray)
- Height: 36px
- Border bottom: `1px solid var(--border-light)`
- Two sections: Left (actions) and Right (controls)

**Left Side** (actions):
- **Run button** (code cells only): Purple accent button, white text, 12px font, 600 weight, "Run" text + Play icon (13px), or "Running" + Loader2 spinner when running
- **Type badge**: Small pill badge showing "SQL" or "MD", monospace font, 10px, 700 weight, gray background (`var(--border)`), arrow icon ")"  to switch type
- **Connector dropdown** (code cells only, if connectors exist): Select dropdown, max-width 180px, shows connector name or "— Inherit from notebook —"

**Right Side** (controls, ordered):
- **Move up**: ChevronUp icon, border button, 13px icon
- **Move down**: ChevronDown icon, border button, 13px icon
- **Toggle source visibility**: EyeOff (hidden) / Eye (shown), border button, 13px icon
- **Toggle collapse**: ChevronRight (collapsed) / ChevronDown (expanded), border button, 13px icon
- **History**: Clock icon, border button, 13px icon, opens history panel
- **Delete**: X icon, transparent background (no border), gray text, hover reveals, 13px icon

**States**:
- **Default**: All buttons enabled
- **Running**: Run button disabled, shows spinner
- **Collapsed**: Toolbar still visible, controls still functional

**Styling**:
```javascript
toolbar: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '5px 10px', background: 'var(--bg-secondary)', minHeight: 36, borderBottom: '1px solid var(--border-light)' }
runBtn: { padding: '4px 12px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 5, fontSize: 12, fontWeight: 600 }
iconBtn: { padding: '3px 7px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }
deleteBtn: { padding: '3px 7px', background: 'transparent', border: '1px solid transparent', borderRadius: 4, fontSize: 12, color: 'var(--text-muted)', cursor: 'pointer' }
```

**Component**: `web/src/components/CellToolbar.tsx`

### CellHeader

**Purpose**: Optional title and description fields for cells

**Visual**:
- Inline editable inputs
- Title: Large text, bold, 14-16px
- Description: Smaller text, muted, 12-13px

**States**:
- **Default**: Text visible, click to edit
- **Editing**: Input field focused, border visible

**Component**: `web/src/components/CellHeader.tsx`

### OutputRenderer

**Purpose**: Display cell execution results (table, chart, error, or text)

**Visual**:
- Container inside cell, border top divider
- No padding container, children provide their own

**Output Types**:

1. **Table** (type: 'table'):
   - **Header bar**: Row count + column count, "X rows · Y columns", monospace font, 11px, gray text
   - **View toggle**: Two buttons "Table" and "Chart" in segmented control (2px gap, gray background, white active)
   - **Table view**:
     - Sticky header (`position: sticky; top: 0`)
     - White header background, `border-bottom: 1px solid var(--border)`
     - Column names: Bold, 12px, monospace
     - Type badges: Small pill, 10px, background `var(--bg-primary)`, border `var(--border-light)`, shows type icon (e.g., "#", "Aa", calendar icon)
     - Rows: Alternating white and `#faf9f7` background
     - Max height: 340px, scroll horizontally and vertically
     - Cells: Monospace 13px, null values italicized and gray, JSON objects stringified
   - **Chart view**: Opens ChartView component (see below)

2. **Error** (type: 'error'):
   - **Visual**: Light red background (`#fff5f5`), red text
   - **Label**: Bold "ERROR" uppercase, 11px, red
   - **Message**: Monospace, 13px, pre-wrap, preserves newlines
   - No table rows or charts

3. **Text** (type: 'text'):
   - **Visual**: Gray background (`var(--bg-secondary)`), monospace
   - **Styling**: `pre` tag, monospace 13px, padding 12x16px, border top

4. **Empty**:
   - **Visual**: "No results returned" text, gray, 13px, padding 12x16px

**Styling**:
```javascript
table: { width: '100%', borderCollapse: 'collapse', fontSize: 13, fontFamily: 'var(--font-mono)' }
th: { padding: '9px 16px', textAlign: 'left', background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border)', position: 'sticky', top: 0 }
td: { padding: '7px 16px', borderBottom: '1px solid var(--border-light)', fontSize: 13 }
rowAlt: { background: '#faf9f7' }
null: { color: 'var(--text-muted)', fontStyle: 'italic' }
badge: { fontSize: 10, background: 'var(--bg-primary)', border: '1px solid var(--border-light)', borderRadius: 4, padding: '1px 5px' }
```

**Component**: `web/src/components/OutputRenderer.tsx`

**Type Icons** (column type badges):
- **String** (string/varchar/text/char): "Aa"
- **Integer** (int/int2/int4/int8/bigint/smallint): "#"
- **Float** (float/float4/float8/double/decimal/numeric/real): "0.1"
- **Boolean** (bool/boolean): ToggleLeft icon
- **Date** (date): Calendar icon
- **Datetime** (datetime/timestamp/timestamptz): Clock icon
- **Time** (time): Timer icon
- **Interval** (interval): Sigma icon
- **Array** (array): "[]"
- **JSON** (json/jsonb): "{}"
- **UUID** (uuid): Fingerprint icon
- **Null** (null): Ban icon
- **Bytes** (bytes/bytea): Binary icon
- **Unknown**: "?"

### ChartView (ECharts)

**Purpose**: ECharts visualization wrapper for table data. Each chart type lives in `web/src/charts/{Name}Chart.tsx` and exports a `ChartModule`.

**Positioning Convention (critical)**:
All ECharts-based charts must follow this layout pattern to prevent title/legend/grid overlap:

```
title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: ... } : undefined,
legend: config.showLegend !== false ? { top: config.title ? 32 : 0, ... } : undefined,
grid:   { top: config.title ? 56 : (showLegend ? 30 : 8), ... }
```

For non-grid charts (pie, sankey, map geo), adjust series center/top:
```
pie center:    [x, config.title ? '58%' : '50%']
tree top:      config.title ? '16%' : '8%'
```

This matches all existing charts — see the JSDoc on `ChartModule` in `web/src/charts/types.ts`.

**Chart Types**:
- **Bar/Stacked Bar**: Vertical bars, X-axis bottom, Y-axis left, grid, tooltip
- **Line**: Lines with dots, smooth/connectNulls optional, dataZoom
- **Area**: Filled areas under lines
- **Scatter**: Dots, optional color/size columns for 3rd/4th dimensions, dataZoom always on
- **Pie/Donut**: Circular slices, roseType, startAngle, padAngle. Uses `labelColumn || xAxis` for names
- **Timeline**: Gantt-style (range mode) or event dots (point mode), swim lanes via groupBy
- **Sankey**: Flow diagram, auto-layout, nodeAlign/nodeWidth/nodeGap
- **Hierarchy Tree**: Parent-child tree, configurable layout/top-down
- **Big Number**: Plain HTML (no ECharts), valueColumn + label + prefix/suffix
- **Map**: Geo scatter (world map) or fallback axis scatter

**Colors** (7-color palette):
- `#6366f1` (indigo), `#22d3ee` (cyan), `#f59e0b` (amber), `#10b981` (emerald)
- `#ef4444` (red), `#8b5cf6` (violet), `#ec4899` (pink)

**Component**: `web/src/charts/index.tsx` (`ChartView`)

## UI Controls

### ConnectorSelector

**Purpose**: Dropdown input to select a database connector

**Visual**:
- Native `<select>` element
- Monospace font, 11px, font-weight 600
- Background: white, border: 1px solid var(--border), border-radius: 6px
- Padding: 2px 6px
- Max-width: 180px

**Options**:
- First option: "— Inherit from notebook —" (empty value, gray text)
- Connector options: Shows connector name

**States**:
- **Default**: Gray text if no connector selected (`var(--text-muted)`)
- **Selected**: Primary text color (`var(--text-primary)`)

**Component**: `web/src/components/ConnectorSelector.tsx`

### ParametersBar

**Purpose**: Display and manage notebook-level SQL parameters

**Visual**:
- Background: `var(--bg-secondary)` (light gray)
- Border bottom: `1px solid var(--border-light)`
- Padding: 6px 40px
- Height: min 40px
- Horizontal layout, items wrap on narrow screens

**States**:

1. **Viewing mode** (default, parameters exist):
   - Info icon (?): Shows tooltip on hover about parameter syntax
   - Parameter inputs: Label (bold monospace) + input field, side-by-side
   - Settings gear icon button: Click to enter edit mode

2. **Edit mode** (managing parameters):
   - "Parameters" title, info icon, then parameter rows
   - Each row: Name input + Type dropdown + Default value input + X remove button
   - "Add" button (dashed border)
   - "Cancel" + "Save" buttons on right

**Parameter Types** (dropdown):
- string
- number
- boolean
- date
- daterange

**Styling**:
```javascript
bar: { background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border-light)', padding: '6px 40px', display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 8, minHeight: 40 }
paramField: { display: 'flex', alignItems: 'center', gap: 5, fontSize: 12 }
paramName: { fontFamily: 'var(--font-mono)', fontWeight: 700, color: 'var(--text-secondary)', fontSize: 11 }
paramInput: { padding: '2px 7px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, fontFamily: 'var(--font-mono)', background: 'white', width: 110 }
manageBtn: { padding: '3px 8px', fontSize: 11, fontWeight: 600, background: 'var(--border)', border: '1px solid transparent', borderRadius: 4, cursor: 'pointer', color: 'var(--text-muted)' }
```

**Component**: `web/src/components/ParametersBar.tsx`

### SchemaBrowser

**Purpose**: Explore database tables, columns, and types

**Visual**:
- Right sidebar panel
- Tree structure with expansion arrows
- Tables > columns > types

**Interactions**:
- Click table → expand/collapse
- Click column → copy name to clipboard (?)

**Component**: `web/src/components/SchemaBrowser.tsx`

## Page Components

### HomePage (File Browser)

**Purpose**: Filesystem browser — navigate folders and all resource types (notebooks, connectors, dashboards)

**Navigation**: Controlled by `?folder=<uuid>` query param. Absent = root level.

**Visual**:
- AppShell wrapper, max-width 1280px centered
- **Breadcrumb** (top): "Files" root crumb (with Home icon) + clickable ancestor segments separated by "/"
- **Toolbar**: "+ New Folder", "+ New Notebook", "+ New Dashboard" buttons
- **Inline create form**: Input + Create/Cancel buttons, Enter to submit, Escape to cancel
- **Folders section**: Auto-fill grid (`minmax(180px, 1fr)`), white cards with folder icon + name + optional "home" badge
- **Notebooks / Connectors / Dashboards sections**: Vertical lists, white rows with type icon + name + link
- **Empty state**: Folder icon, "This folder is empty", "New Notebook" CTA

**Context menu (`⋯` button on every item)**:
- Opens a fixed-position dropdown (z-index 1000)
- Items: Rename (folders + notebooks only), Move to…, Permissions, Delete
- Closes on outside click
- Menu clamps to viewport bottom edge

**Inline rename** (appears after clicking Rename):
- Input pre-filled with current name, replaces the name display
- Confirm on Enter or blur, cancel on Escape
- Calls `PUT /api/v1/folders/:id { name }` or `PUT /api/v1/notebooks/:id { title }`

**Move to… modal**:
- Centered overlay with semi-transparent backdrop
- Navigable folder tree with breadcrumb; "Move here" confirm button
- Calls appropriate `PUT` endpoint with `parent_id` or `folder_id` (null = move to root)

**Permissions**:
- Clicking "Permissions" in the `⋯` menu opens `PermissionsPanel` slide-over
- `permissionsTarget` state `{ type, id, name }` controls which panel is open

**Component**: `web/src/pages/HomePage.tsx`

### NotebookPage

**Purpose**: Main editing view for a single notebook

**Layout**:
- AppShell wrapper
- Top area: Back button (<), notebook title, connector selector (dropdown)
- Left sidebar (optional): Schema browser (toggle to show/hide)
- Right sidebar (optional): Parameters, schedules, history panels (toggle to show/hide)
- Center: Vertical stack of cells (CodeCell/TextCell components)
- Floating actions: + Add SQL cell / + Add Markdown cell buttons

**Sidebars**:
- **Schema Browser**: Right-side panel, shows tables/columns
- **Parameters Bar**: Top bar below notebook title, shows parameter inputs
- **Schedules Panel**: Right-side panel, manage scheduled runs
- **History Panel**: Right-side panel, version history for selected cell

**Cell Management**:
- Cells stack vertically, 16px gap between cells
- Add cell buttons: Bottom of page or floating
- Drag to reorder (?) (not implemented yet)
- Keyboard shortcuts: Ctrl+Enter run, Ctrl+Shift+F format

**Visual**:
- Background: `var(--bg-primary)` (off-white)
- Cells: White cards, 10px radius, shadow
- Side panels: Light gray backgrounds, border left/right

**Component**: `web/src/pages/NotebookPage.tsx`

### DashboardsPage

**Purpose**: List all dashboards + create new

**Visual**:
- Similar to HomePage layout
- Grid/list toggle (same pattern)
- Dashboard cards show title, edit date, widget count (?)

**Component**: `web/src/pages/DashboardsPage.tsx`

### DashboardPage

**Purpose**: Edit/view a single dashboard

**Layout**:
- Grid of widgets (react-grid-layout)
- Each widget: Output from a cell (table or chart)
- Edit mode: Add/remove widgets, resize, drag

**Styling**:
- Not implemented yet (placeholder)

**Component**: `web/src/pages/DashboardPage.tsx`

### ConnectorsPage

**Purpose**: Manage database connections

**Visual**:
- List of connectors, each showing: Name, Type (Postgres/ClickHouse/JS), database name, status
- "New Connector" button
- Edit/Delete actions per connector

**Form**:
- Name input
- Type dropdown (Postgres, ClickHouse, JavaScript)
- Connection details (host, port, database, credentials)
- Test connection button

**Component**: `web/src/pages/ConnectorsPage.tsx`

### MembersPage

**Purpose**: Manage organization members

**Visual**:
- List of members (name, email, role)
- Invite button
- Role change dropdown (viewer/editor/admin)
- Remove member button

**Special Note**:
- Hides top bar menu when viewing (different layout than other pages)
- Shows only name and back button

**Component**: `web/src/pages/MembersPage.tsx`

### AdminPage

**Purpose**: Platform admin controls (visible only to platform admins)

**Visual**:
- List of organizations
- User management across orgs
- Platform-wide audit log

**Component**: `web/src/pages/AdminPage.tsx`

### GroupsPage

**Purpose**: Manage org groups — view all groups, expand to see members, admin-only create/rename/delete

**Visual**:
- AppShell wrapper
- "+ New Group" button (admin only), inline create form with name input
- Group rows: accordion-style, clicking the row header expands/collapses
  - Collapsed: Group name + member count + (admin) Rename / Delete buttons
  - Expanded: Member list with × remove per member + Add member `<select>` + Add button
- Rename: inline edit input (same Enter/Escape pattern as HomePage)
- Delete: `window.confirm` before API call
- ErrorBanner on any mutation failure

**Auth gating**:
- Create / Rename / Delete group buttons hidden for non-admin users
- Add member / Remove member hidden for non-admin users
- Members query (`GET /api/v1/members`) only fires when user is admin (`enabled: isAdmin`)

**Component**: `web/src/pages/GroupsPage.tsx`

### ProfilePage

**Purpose**: User profile — name, status, theme toggle

**Visual**:
- Edit name and status inline
- Light/dark theme toggle (persisted to localStorage)

**Component**: `web/src/pages/ProfilePage.tsx`

## Overlay Components

### PermissionsPanel

**Purpose**: Slide-over drawer for managing per-resource ACL entries

**Trigger**: "Permissions" option in the `⋯` context menu on any file-browser item

**Visual**:
- Fixed-position right drawer, width 420px, height 100vh, z-index 1501
- Semi-transparent backdrop (rgba(0,0,0,0.3)), z-index 1500, clicking closes panel
- **Header**: Resource name (bold) + resource-type badge (color-coded) + × close button
- **Inheritance note**: "Inheriting N permissions from parent folder" or "No inherited permissions"
- **ACL entries list**: Avatar circle (initials / `#` for groups) + name + per-action checkboxes + × remove
- **Draft mode**: Checkboxes and removes update local draft; Save/Discard buttons appear when there are unsaved changes
- **Add entry row**: `<select>` with Users/Groups optgroups + action checkboxes + Add button

**Actions per resource type**:
- `folder`: view, create, edit, manage, delete
- `notebook`: view, run, edit, share, delete
- `connector`: view, use, edit, share, delete
- `dashboard`: view, edit, share, delete

**API calls**:
- `GET /api/v1/acl/:resource_type/:resource_id` — load entries
- `PUT /api/v1/acl/:resource_type/:resource_id` — replace full ACL (requires `manage`/`share` permission)
- `GET /api/v1/members`, `GET /api/v1/groups` — populate subject dropdown

**Props**:
```tsx
interface PermissionsPanelProps {
  resourceType: 'folder' | 'notebook' | 'connector' | 'dashboard'
  resourceId: string
  resourceName: string
  parentFolderId?: string
  onClose: () => void
}
```

**Component**: `web/src/components/PermissionsPanel.tsx`

## Common Visual States

### Loading

**When**: Data fetching, API calls, creating items

**Visual**:
- Spinner: `Loader2` icon from lucide-react, 13px, animated rotation
- Color: Inherit from parent (usually `var(--text-muted)` or white)
- Button text: "Running" or "Saving…" next to spinner

**Examples**:
- Run button: `<Loader2 size={13} /> Running`
- Create notebook: Loading state while mutating

**Styling**:
```javascript
// Animation is automatic from lucide-react icon
// Just use: <Loader2 size={13} className="animate-spin" />
// Or in React inline: style={{ animation: 'spin 1s linear infinite' }}
```

### Empty

**When**: No notebooks, no connectors, no results, first-time user

**Visual**:
- Center-aligned column
- Icon circle: 56x56px, gray background (`var(--bg-secondary)`), border-radius 14px
- Title: Bold, 18px, primary text
- Description: 14px, secondary text
- CTA button: Primary action button

**Examples**:
- No notebooks: BookOpen icon, "No notebooks yet", "Create your first notebook" button
- No results: Empty output section

**Styling**:
```javascript
empty: { textAlign: 'center', padding: '80px 0', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12 }
emptyIcon: { width: 56, height: 56, background: 'var(--bg-secondary)', borderRadius: 14, display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: 4 }
emptyTitle: { fontSize: 18, fontWeight: 700, color: 'var(--text-primary)' }
emptyText: { fontSize: 14, color: 'var(--text-secondary)', marginBottom: 8 }
```

### Error

**When**: API error, validation error, query execution error

**Visual**:
- **Inline error**: Red text (`var(--error)`), monospace font, 13px
- **Card error**: Light red background (`#fff5f5`), border top divider, "ERROR" label in bold uppercase red 11px
- **Toast/Alert**: Not implemented yet (no toast system)

**Examples**:
- Cell output error: Red card with "Error" label + stack trace in monospace
- Create notebook error: `<p style={{ color: '#c0392b', fontSize: 12 }}>` (note: should use `var(--error)`)
- Save error: Status bar shows "Save failed: {error}" in red

**Styling**:
```javascript
errorWrap: { padding: '12px 16px', background: '#fff5f5', borderTop: '1px solid var(--border-light)', display: 'flex', flexDirection: 'column', gap: 6 }
errorLabel: { fontSize: 11, fontWeight: 700, color: 'var(--error)', textTransform: 'uppercase', letterSpacing: '0.06em' }
error: { color: 'var(--error)', fontSize: 13, fontFamily: 'var(--font-mono)', whiteSpace: 'pre-wrap', margin: 0 }
```

### Active/Selected

**When**: Active nav item, selected cell, focused input

**Visual**:
- **Nav item active**: Background `var(--accent-light)`, text `var(--accent)`
- **Cell focused**: Border highlight (not implemented, no special state)
- **Input focused**: Browser default focus ring (no custom outline)

**Nav Active State**:
```javascript
// In NavLink style function
background: isActive ? 'var(--accent-light)' : 'transparent'
color: isActive ? 'var(--accent)' : 'var(--text-muted)'
```

### Hover

**When**: Button hover, card hover, link hover

**Visual**:
- **Button hover**: Slight opacity change or border color shift
- **Card hover**: Box-shadow increase (not implemented)
- **Link hover**: Text underline

**Examples**:
- Nav item hover: Subtle background change (current: same as active)
- Avatar hover: ? (not defined)
- Delete button hover: ? (not defined)

**Note**: Most buttons don't have explicit hover states defined (only active/selected)

### Collapsed/Expanded

**When**: Sidebar toggle, cell collapse, panel collapse

**Visual**:
- **Sidebar collapsed**: Width 48px, icon-only, centered, no labels
- **Sidebar expanded**: Width 200px, icon + label, left-aligned
- **Cell collapsed**: Gray background (`var(--bg-secondary)`), compact height (26px), title + "Expand" button
- **Cell expanded**: White card, full height, all parts visible

**Interactions**:
- Sidebar: Click chevron at bottom to toggle
- Cell: Click chevron in toolbar (ChevronDown icon = expanded, ChevronRight icon = collapsed)

### Disabled

**When**: Button disabled, input disabled, action not available

**Visual**:
- **Button disabled**: Opacity lowered, cursor not-allowed, no hover effect
- **Input disabled**: Gray background, text muted, cursor not-allowed

**Examples**:
- Run button while running: `disabled={running}`, shows spinner
- Create button: `disabled={!newTitle.trim()}`

**Styling**:
```javascript
// Browser default disabled styles (opacity, cursor)
// No custom disabled styles defined
```

## UI Patterns

### Cards

**Purpose**: Container for notebook cards, dashboard cards, cells

**Pattern**:
```javascript
card: {
  background: 'white',
  borderRadius: 10,
  border: '1px solid var(--border)',
  overflow: 'hidden',
  boxShadow: 'var(--shadow-sm)',
}
```

**Variations**:
- **Notebook card**: Hover shadow increase (not implemented)
- **Cell card**: Same as above
- **Form card**: Purple left border accent for emphasis (``border: '1.5px solid var(--accent-light)'``)

### Buttons

**Primary Button**:
```javascript
{
  padding: '8px 18px',
  background: 'var(--accent)',
  color: 'white',
  border: 'none',
  borderRadius: 7,
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
}
```

**Secondary Button**:
```javascript
{
  padding: '8px 16px',
  border: '1px solid var(--border)',
  borderRadius: 6,
  background: 'none',
  fontSize: 13,
  cursor: 'pointer',
  color: 'var(--text-secondary)',
}
```

**Icon Button**:
```javascript
{
  padding: '3px 7px',
  background: 'transparent',
  border: '1px solid var(--border)',
  borderRadius: 4,
  fontSize: 12,
  cursor: 'pointer',
  color: 'var(--text-secondary)',
}
```

**Run Button** (accent):
```javascript
{
  padding: '4px 12px',
  background: 'var(--accent)',
  color: 'white',
  border: 'none',
  borderRadius: 5,
  fontSize: 12,
  fontWeight: 600,
}
```

### Forms

**Input Field**:
```javascript
{
  flex: 1,
  padding: '8px 12px',
  border: '1px solid var(--border)',
  borderRadius: 6,
  fontSize: 14,
  outline: 'none',
  background: 'var(--bg-primary)',
}
```

**Select/Dropdown**:
```javascript
{
  fontSize: 11,
  fontFamily: 'var(--font-mono)',
  fontWeight: 600,
  padding: '2px 6px',
  background: 'var(--bg-primary)',
  border: '1px solid var(--border)',
  borderRadius: 4,
  outline: 'none',
  maxWidth: 180,
}
```

**Focus State**:
- Browser default outline (no custom focus ring)
- Missing explicit focus styles

**Label**:
```javascript
{
  display: 'flex',
  alignItems: 'center',
  gap: 5,
  fontSize: 12,
}
```

### Typography

**Page Title**:
```javascript
{ fontSize: 22, fontWeight: 700, letterSpacing: '-0.3px', color: 'var(--text-primary)' }
```

**Section Title**:
```javascript
{ fontSize: 18, fontWeight: 700, letterSpacing: '-0.2px', color: 'var(--text-primary)' }
```

**Card Title**:
```javascript
{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }
```

**Body Text**:
```javascript
{ fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.5 }
```

**Secondary Text**:
```javascript
{ fontSize: 13, color: 'var(--text-secondary)' }
```

**Muted Text**:
```javascript
{ fontSize: 12, color: 'var(--text-muted)' }
```

**Monospace**:
```javascript
{ fontFamily: 'var(--font-mono)', fontSize: 13 }
```

**Label/Badge**:
```javascript
{ fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em', fontFamily: 'var(--font-mono)' }
```

## Accessibility

### Keyboard Navigation

- **Tab**: Navigate between interactive elements (buttons, inputs, selects)
- **Enter/Space**: Activate buttons, submit forms
- **Ctrl+Enter / Cmd+Enter**: Run cell (code cells)
- **Ctrl+Shift+F / Cmd+Shift+F**: Format SQL
- **Escape**: Cancel inline forms (create notebook)
- **Arrow keys**: Not implemented for cell reordering

### Focus States

- **Current**: Browser default outline (no custom focus ring)
- **Missing**: Explicit `:focus-visible` styles not defined
- **Recommendation**: Add custom focus rings (2px outline, 2px offset, `var(--accent)` color)

### ARIA Labels

- **Avatar button**: `aria-label="Profile menu"`
- **Missing**: Most buttons, inputs, and icons lack aria-labels
- **Recommendation**: Add aria-labels to icon-only buttons, input fields, and icon decorative elements

### Color Contrast

- **Accent on white**: AA pass (4.5:1+ contrast)
- **Text primary on bg-primary**: AA pass
- **Text muted on white**: May fail AA (needs review)
- **Recommendation**: Audit all text/background combinations for WCAG AA compliance

### Screen Readers

- **Missing**: Live region announcements for errors, loading states
- **Missing**: Descriptive labels for icons (using `aria-label` or visually hidden text)
- **Recommendation**: Add `aria-live="polite"` regions for dynamic content (errors, save status)

## Visual Regression Testing

### Running Tests

```bash
# Run all E2E tests including visual tests
npx playwright test

# Run specific test file
npx playwright test e2e/dashboard.spec.ts

# Run tests and update snapshots
npx playwright test --update-snapshots

# Run tests in UI mode (for debugging)
npx playwright test --ui
```

### When to Add Visual Tests

**Required**:
- New page-level component (add to `e2e/[page].spec.ts`)
- New major UI state (empty state, error state, loading state)
- Layout changes (grid to list, sidebar toggle)
- Critical user flows (create notebook, run cell, toggle view)

**Optional**:
- Minor component changes (button style, color tweak)
- Edge cases (long text overflow, single item vs multiple)

### Test Naming Convention

```typescript
// Use 'visual:' prefix for visual tests
test('visual: dashboard page', async ({ page }) => {
  await page.goto('/dashboards')
  await expect(page).toHaveScreenshot('dashboards-page.png')
})

// For different states
test('visual: dashboard empty state', async ({ page }) => {
  // Empty state setup
  await expect(page).toHaveScreenshot('dashboards-empty.png')
})

test('visual: dashboard with data', async ({ page }) => {
  // Seed data and setup
  await expect(page).toHaveScreenshot('dashboards-with-data.png')
})
```

### Snapshot Locations

- Snapshots: `e2e/snapshots/[test-name].png`
- Test results: `e2e/test-results/` (diff images on failure)

### Common Test Scenarios

**1. Empty State**:
```typescript
// No notebooks exist yet
test('visual: notebooks empty state', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveScreenshot('notebooks-empty.png')
})
```

**2. Populated List**:
```typescript
// Multiple notebooks visible
test('visual: notebooks list view', async ({ page }) => {
  // Seed data via API
  await page.goto('/')
  await expect(page).toHaveScreenshot('notebooks-list.png')
})
```

**3. Grid vs List Layout**:
```typescript
test('visual: notebooks grid view', async ({ page }) => {
  await page.goto('/')
  await page.click('button[title="Switch to grid"]')
  await expect(page).toHaveScreenshot('notebooks-grid.png')
})
```

**4. Create Form**:
```typescript
test('visual: create notebook form', async ({ page }) => {
  await page.goto('/')
  await page.click('button:has-text("New Notebook")')
  await expect(page).toHaveScreenshot('create-notebook-form.png')
})
```

**5. Sidebar States**:
```typescript
test('visual: sidebar collapsed', async ({ page }) => {
  await page.goto('/')
  // Collapse sidebar (if expanded by default)
  await page.click('button[aria-label="Collapse sidebar"]')
  await expect(page).toHaveScreenshot('sidebar-collapsed.png')
})
```

**6. Cell States**:
```typescript
test('visual: code cell running', async ({ page }) => {
  // Setup cell and trigger run
  await expect(page).toHaveScreenshot('cell-running.png')
})
```

**7. Error State**:
```typescript
test('visual: cell error output', async ({ page }) => {
  // Trigger error in cell execution
  await expect(page).toHaveScreenshot('cell-error.png')
})
```

### Updating Snapshots

When visual changes are intentional:
1. Run tests with `--update-snapshots` flag
2. Review diff images in `e2e/test-results/`
3. Commit updated snapshots to git

### Debugging Failed Tests

1. Check diff image in `e2e/test-results/` for visual differences
2. Run in UI mode: `npx playwright test --ui` to step through
3. Check console errors: Logs appear in test output
4. Verify test setup: Ensure data/API is mocked correctly

## Development Workflow for AI Assistance

When working with an AI that cannot see images:

### Describing UI Changes

**Good Request**:
> "Update the Sidebar component. Current: 48px collapsed width, 200px expanded, dark background with nav items. Change: Increase collapsed width to 56px for better touch targets, keep expanded at 200px. Add hover highlight (lighter background) on nav items."

**Bad Request**:
> "Make the sidebar look better" (too vague)

### Component Description Template

When asking AI to implement a component:

```markdown
## Component: MyComponent

**Purpose**: One-line description

**Visual**:
- Shape: (card, row, panel, dropdown, etc.)
- Size: (width, height, padding)
- Background: (color or gradient)
- Border: (width, style, color, radius)
- Shadow: (shadow-sm, shadow-md, none)
- Typography: (font, size, weight, color)

**States**:
- **Default**: ...
- **Hover**: ...
- **Active**: ...
- **Disabled**: ...

**Interactions**:
- Click X → triggers Y
- Hover X → shows Y

**Layout**:
- Parent container: AppShell / Page
- Children: Sub-components
- Position: (fixed, absolute, relative, sticky)

**Examples**:
- Similar to: [existing component name]
- Differences: ...
```

### Testing Visual Changes

After making changes:
1. Run visual tests locally: `npx playwright test`
2. If tests fail, check diff images
3. If changes are intentional, update snapshots
4. Review in browser: `npx playwright test --ui`

### Common Pitfalls

- **Font**: Always use `var(--font-sans)` or `var(--font-mono)`, never hardcode font families
- **Colors**: Use CSS variables (`var(--accent)`), never hardcoded hex codes
- **Borders**: Use `var(--border)` / `var(--border-light)`, keep consistent width/style
- **Shadows**: Use `var(--shadow-sm)` / `var(--shadow-md)`, avoid custom shadow definitions
- **Radius**: Use 10px for cards/cells, 6px for inputs/buttons, 4px for small elements (badges, icons)

### When to Ask for Clarification

Ask the user to describe:
- Exact color values (if not in theme)
- Spacing/padding values (in pixels)
- Border radius values
- Font sizes (in pixels)
- Hover/active states (if not obvious)
- Layout changes (flexbox, grid, positioning)