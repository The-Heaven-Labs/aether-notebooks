# hnb Improvements — Design Spec
**Date:** 2026-03-25
**Status:** Approved
**Source:** IMPROVEMENTS.md (30 items → 4 phases)

---

## Overview

This spec covers all 30 improvements from IMPROVEMENTS.md, grouped into 4 phases ordered by dependency and complexity. Each phase delivers independently usable improvements without requiring the next phase.

---

## Phase 1 — Shell & Quick Wins

*No schema changes. All wins visible immediately.*

### 1.1 App Shell Redesign (Navigation Sidebar)

Replace the current `NavBar.tsx` with a two-part shell:

- **Slim top bar** (`TopBar.tsx`): logo, org name, profile avatar with dropdown (name, email, Sign out). In Phase 4 gains "Switch org" and "Platform Admin" links.
- **Icon-rail sidebar** (`Sidebar.tsx`): 48px collapsed, 200px expanded. Items: Notebooks, Dashboards, Connectors, Members, Audit. Collapsed state shows icons with tooltips; expanded shows icons + labels. Toggle button at the bottom of the rail. Expanded/collapsed preference persisted in `localStorage`.

The Members page no longer needs special treatment — it renders within the new shell like all other pages. The existing `NavBar.tsx` is deleted.

### 1.2 Notebook Header

`NotebookPage` gains a proper header section at the top of the page:
- Large editable title (already wired to `PUT /notebooks/:id`)
- Editable description field below the title

The Go `models.Notebook` struct and DB are missing the `description` field — add `description TEXT` (nullable) to the `notebooks` table with a migration. Add it to the `PUT /notebooks/:id` handler and response.

### 1.3 List View for Index Pages

`HomePage` (notebooks) and `DashboardsPage` default to a **row/list layout** instead of the current grid. A toggle button (grid icon / list icon) switches between modes. Layout preference persisted in `localStorage` per page.

### 1.4 Profile Menu

The top bar avatar opens a dropdown with: display name, email (read-only), "Sign out" button. Clicking outside closes it. Phase 4 extends this with org-switching and platform admin links.

### 1.5 Collaboration Cursor Names

Currently cursors appear as "Anonymous". Fix: the login and register responses already return `user.name` and `user.email` (via `authResponse`). The frontend stores these in `localStorage` at login time. When initialising `HocuspocusProvider`, the stored name/email is passed as the initial `awareness` state (`{ user: { name, email } }`). The relay passes awareness through unchanged — no relay or JWT changes needed.

### 1.6 Audit Log Names

The `GET /api/v1/audit` handler joins the `users` table to resolve `user_id → email` and performs a lookup of `resource_id → resource_name` (notebook title, dashboard title, etc.) based on `resource_type`. Response adds `user_email string` and `resource_name string` fields alongside the existing IDs. IDs are kept for disambiguation.

### 1.7 Output Type Icons

`OutputRenderer.tsx` replaces the raw type string badge with an icon + tooltip. Full type map:

| DB type(s) | Icon | Label |
|---|---|---|
| `string`, `varchar`, `text`, `char` | `Aa` | String |
| `integer`, `int`, `bigint`, `smallint` | `#` | Integer |
| `float`, `double`, `decimal`, `numeric`, `real` | `0.1` | Float |
| `boolean`, `bool` | toggle | Boolean |
| `date` | calendar | Date |
| `datetime`, `timestamp`, `timestamptz` | clock | Datetime |
| `array` | `[]` | Array |
| `json`, `jsonb` | `{}` | JSON |
| `uuid` | fingerprint | UUID |
| `null` | `∅` | Null |
| `bytes`, `bytea` | binary | Bytes |
| `unknown` | `?` | Unknown |

Array and JSON cells render an expandable preview on hover.

---

## Phase 2 — Cell Editor

*Minor schema changes: new columns on `cells`, new tables for history and snapshots.*

### 2.1 Cell Collapse / Hide Source

Two new boolean columns on `cells`:
- `source_visible BOOLEAN NOT NULL DEFAULT true` — when false, the code editor is hidden; only the output is shown.
- `cell_collapsed BOOLEAN NOT NULL DEFAULT false` — when true, the entire cell (editor + output) collapses to a single title bar.

Both states are saved via the existing `PUT /notebooks/:notebook_id/cells/:cell_id` endpoint. `CellToolbar.tsx` gains two toggle buttons. State persists across sessions and is shared with collaborators.

### 2.2 Cell Title, Description & Slug

Three new columns on `cells`:
- `title VARCHAR(255)` (nullable) — displayed above the cell editor as an inline-editable input.
- `description TEXT` (nullable) — secondary line below the title.
- `slug VARCHAR(100)` (nullable, unique within notebook) — monospace badge in the cell header. Auto-generated from title if set (slugified), otherwise `cell_<position>`. Manually editable. Used in Phase 3 for `{{slug}}` SQL template substitution.

Uniqueness enforced at the DB level with a partial unique index: `UNIQUE (notebook_id, slug) WHERE slug IS NOT NULL`.

### 2.3 Markdown Syntax Highlighting

Replace the plain `<textarea>` in `TextCell.tsx` with a CodeMirror 6 instance using `@codemirror/lang-markdown`. The `@codemirror/*` packages are already installed for `CodeCell` — no new dependencies.

### 2.4 Live Markdown Preview

A CodeMirror `ViewPlugin` decorates lines the cursor is **not** on by replacing them with rendered markdown HTML (via the existing markdown renderer). The active line remains plain text. This is a decoration-only approach — no additional library. Toggling in/out of the cell returns to full source view.

### 2.5 Inline Image Paste

In the CodeMirror markdown editor, a `domEventHandlers` extension intercepts `paste` events. If the clipboard contains image data, it converts it to a base64 data URI and inserts `![pasted image](data:image/png;base64,...)` at the cursor. No backend required in Phase 2. Phase 4 attachments will provide proper URLs that can replace the inline base64 approach.

### 2.6 Cell History & Notebook Snapshots

**Per-cell versioning:**

New table:
```sql
CREATE TABLE cell_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cell_id UUID NOT NULL REFERENCES cells(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON cell_versions (cell_id, created_at DESC);
```

Version creation logic (server-side, on each cell save):
1. Fetch the most recent version for this cell.
2. Calculate character-level edit distance between the incoming source and the last version's source.
3. **Merge** (update existing version's source in place) if: edit distance < 50 chars AND last version was created < 60 seconds ago.
4. **Create new version** if: edit distance ≥ 50 chars OR last version is older than 60 seconds OR no version exists yet.

A "History" button in `CellToolbar` opens a right-side panel showing versions by timestamp with a source diff preview. Restoring a version replaces the cell source (triggers a normal save, which may merge or create a version per the above rules).

**Notebook snapshots:**

New table:
```sql
CREATE TABLE notebook_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  cell_sources JSONB NOT NULL,  -- map of cell_id → source
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

A "Snapshot" button in the notebook toolbar prompts for a name and creates a snapshot of all current cell sources. Restoring a snapshot bulk-updates all cell sources (each triggers versioning logic). New API endpoints: `POST /api/v1/notebooks/:id/snapshots`, `GET /api/v1/notebooks/:id/snapshots`, `POST /api/v1/notebooks/:id/snapshots/:snapshot_id/restore`.

### 2.7 Keyboard Shortcuts

A `useNotebookKeyboardShortcuts` hook attached to the notebook page handles JupyterLab-style shortcuts (active when a cell is focused but not in edit mode):

| Key | Action |
|---|---|
| `Shift+Enter` | Run focused cell |
| `Ctrl+Enter` | Run cell, stay in cell |
| `Escape` | Exit edit mode |
| `B` | Add cell below |
| `A` | Add cell above |
| `D D` (double tap) | Delete cell |
| `M` | Convert to markdown cell |
| `Y` | Convert to code cell |
| `J` / `↓` | Move focus to next cell |
| `K` / `↑` | Move focus to previous cell |
| `?` | Open shortcuts cheat sheet modal |

---

## Phase 3 — Data & Connectors

*Schema changes: new columns on `notebooks` and `cells`, new executor method.*

### 3.1 Notebook-Level Default Connector

New column: `connector_id UUID REFERENCES connectors(id)` (nullable) on `notebooks`.

The notebook header gains a connector selector dropdown. All cells inherit the notebook's connector unless they have their own `connector_id` set. Cell-level override: a small connector icon in `CellToolbar` expands into a selector on click. When a cell's `connector_id` is null, it uses the notebook's connector at execution time — the server resolves the effective connector in `handleExecuteCell`.

### 3.2 Optional Database Field on Connector Creation

The `database` field in connector creation is made optional for connector types that support `SHOW DATABASES` / `SHOW SCHEMAS` (ClickHouse, MySQL, SQL Server). The backend stops requiring `database` for those types. At query time, if `database` is empty, the connector connects without a default database selected.

### 3.3 Database & Table Discovery

The `Executor` interface gains one new method:
```go
Databases(ctx context.Context) ([]string, error)
```

Implemented by each executor type (Postgres returns `SELECT datname FROM pg_database`; ClickHouse uses `SHOW DATABASES`; JS returns an empty list). The `SchemaBrowser` component, when a connector has no default database, shows a database selector first. Selecting a database calls the new `GET /api/v1/connectors/:id/databases` endpoint and then loads the existing schema for the selected database.

### 3.4 Named Query Variables (`{{slug}}` Substitution)

At `handleExecuteCell`, before sending the query to the executor, the server resolves `{{slug}}` references:

1. Parse the cell's source for all `{{...}}` tokens.
2. For each token, find the cell in the same notebook whose `slug` matches.
3. Substitute the token with `(<referenced cell's source>)` (wrapped in parens for safe subquery use).
4. Recurse for any `{{...}}` tokens in the substituted source.
5. If a cycle is detected (cell references itself directly or transitively), return a 400 error with the cycle path.

This is pure server-side string processing. The `slug` field is already added in Phase 2. Frontend: cells that are referenced by other cells display a small "referenced by N" badge below their slug.

### 3.5 Parameters at Cell Level

Extend the existing `Parameter` type. New column: `parameters JSONB` (default `[]`) on `cells`.

Cell-level parameters override notebook-level parameters of the same name at execution time. The server merges them: cell params take precedence over notebook params. In the UI, cells with their own parameters show a compact params section above the editor (collapsible). The existing `ParametersBar` at the top of the notebook continues to show and control notebook-level parameters.

---

## Phase 4 — Advanced Features

*Most complex phase. Mix of frontend-heavy and architectural work.*

### 4.1 Charts Expansion

Replace `ChartView.tsx` with a richer chart component using **Recharts** (lightweight, composable, React-native).

New chart types: Bar, Stacked Bar, Line (already exists), Area, Scatter, Pie, Donut.

Each chart type has a configuration panel: x-axis column, y-axis column(s), color scheme, title, legend toggle, grid lines. Configuration stored in `outputs[].config` (already a `Config interface{}` field in `Output` model — no schema change). The config panel is shown inline below the chart with a "Configure" toggle.

### 4.2 Dashboard Input Widgets

New `Widget.type` values: `date_picker`, `date_range`, `multi_select`, `freetext`, `number`.

A dashboard-level React context (`DashboardParamsContext`) holds current parameter values. Input widgets write to this context. Code/chart widgets that reference `{{param_name}}` in their SQL are re-executed when a controlling widget changes (debounced 300ms). Widget config specifies the parameter name it controls and its default value. Stored in `widget.config` (already a `Record<string, unknown>` — no schema change).

### 4.3 Templates & Snippets

New table:
```sql
CREATE TABLE templates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  type VARCHAR(20) NOT NULL CHECK (type IN ('notebook', 'cell')),
  content JSONB NOT NULL,
  is_builtin BOOLEAN NOT NULL DEFAULT false,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- **Notebook templates**: `content` is a full notebook definition (title, description, ordered list of cell sources/types/languages). "New notebook from template" on `HomePage`.
- **Cell snippets**: `content` is a single cell definition. "Insert snippet" button in `CellToolbar` opens a searchable snippet picker. Seeded with a few built-in snippets (e.g. "Date range filter", "Row count", "Schema inspection").
- Org admins can save any notebook or cell as a template via a "Save as template" action.

### 4.4 Attachments

New table:
```sql
CREATE TABLE attachments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  notebook_id UUID REFERENCES notebooks(id) ON DELETE SET NULL,
  filename VARCHAR(255) NOT NULL,
  mime_type VARCHAR(100) NOT NULL,
  size_bytes BIGINT NOT NULL,
  storage_path TEXT NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Phase 4 stores files on the local filesystem under `HNB_ATTACHMENT_DIR` (env var, default `./attachments`). API:
- `POST /api/v1/notebooks/:id/attachments` — multipart upload, returns attachment record
- `GET /api/v1/attachments/:id` — streams the file
- `GET /api/v1/notebooks/:id/attachments` — lists attachments for a notebook
- `DELETE /api/v1/attachments/:id` — deletes record + file

In markdown cells, attachments are inserted as `![filename](attachment://uuid)`. The frontend rewrites these references to `/api/v1/attachments/:id` before rendering. Future: swap the local filesystem backend for S3-compatible storage by changing only `storage_path` semantics and the read/write handlers — the API and data model stay the same.

### 4.5 Presentation Mode

A new route `/notebooks/:id/present` renders `PresentationPage.tsx`. Each cell is shown full-width, one at a time. Code cells show only their last output (no editor). Markdown cells render fully. Navigation: left/right arrow keys or on-screen Previous/Next buttons. A progress indicator shows current cell index / total. A "Present" button in the notebook toolbar opens it in a new tab. No backend changes needed.

### 4.6 Org / Admin Model

**New DB entities:**

```sql
-- Already exists: users table gains:
ALTER TABLE users ADD COLUMN is_platform_admin BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE org_allowed_domains (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  domain VARCHAR(255) NOT NULL,
  auto_join BOOLEAN NOT NULL DEFAULT true,  -- false = request-to-join (requires approval)
  UNIQUE (org_id, domain)
);

CREATE TABLE org_invites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  email VARCHAR(255) NOT NULL,
  role VARCHAR(50) NOT NULL,
  token VARCHAR(64) NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE org_invite_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  role VARCHAR(50) NOT NULL DEFAULT 'viewer',
  token VARCHAR(64) NOT NULL UNIQUE,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Sign-up flow refactor:**

Current `POST /api/v1/auth/register` creates a user + org atomically. Split into:
1. `POST /api/v1/auth/register` — creates account only (email, password, name). Returns a short-lived "onboarding token" (15-minute expiry).
2. `POST /api/v1/auth/org/create` — creates a new org and makes the user its admin. Called from the "Create your org" wizard step.
3. `POST /api/v1/auth/org/join` — accepts an invite token or invite link token. Adds user to org with the pre-set role.

**Domain-based join flow:**
After account creation, the server checks `org_allowed_domains` for the user's email domain. If a match with `auto_join = true` exists, the user is automatically added to that org and redirected to the app. If `auto_join = false`, a join request is queued for org admin approval (simple: org admin sees a "Pending requests" list in the Members page).

**Platform admin panel** (`/admin`, `is_platform_admin` required):
- List all orgs (name, member count, created date, status)
- Create / disable orgs
- List all users across orgs (email, name, org memberships)
- Reset a user's password

**Invite flows (org admin):**
- Email invite: `POST /api/v1/members/invite` with `{email, role}` → sends email with 7-day token link
- Invite link: `POST /api/v1/members/invite-link` → returns a shareable URL (no expiry, resettable)
- Both flows use the existing OIDC email infrastructure where available

---

## Cross-Cutting Concerns

### Database migrations
Each phase introduces migrations. All are additive (new columns with defaults, new tables) except the Phase 4 sign-up refactor, which changes existing handler behaviour without altering existing rows.

### Testing
- All new Go handlers follow the existing `internal/api/*_test.go` pattern using `setupTestServer` with a real DB.
- New executor methods (`Databases`) need unit tests per executor type.
- Frontend changes do not currently have automated tests — this is an existing gap, not introduced by this work.

### Environment variables added
| Variable | Phase | Purpose |
|---|---|---|
| `HNB_ATTACHMENT_DIR` | 4 | Local filesystem path for attachment storage |

---

## What Is Explicitly Out of Scope

- SCIM/IdP-pushed provisioning (Phase 4 covers JIT only)
- S3/object storage for attachments (Phase 4 uses local FS; S3 is a future swap)
- Custom chart code execution (deferred — needs separate security analysis)
- Logo redesign (design asset, not an engineering task)
