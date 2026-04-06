# Improvements Batch 1 — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement six improvements from IMPROVEMENTS.md: sidebar/profile refactor, filesystem metadata, filesystem filter, connector editing, duplicate cell, and permissions presets.

**Architecture:** Mostly frontend changes in `web/src/`. Two backend additions (PUT connector, POST duplicate cell) follow the existing handler pattern in `internal/api/`. All routes use `net/http` ServeMux, no framework. Frontend uses React Query for data fetching.

**Working directory:** `.worktrees/improvements/`

**Run frontend tests:** `cd web && npm run test:run`
**Run backend tests:** `task test:api` (requires infra — see CLAUDE.md)

**Tech Stack:** Go (backend), React + TypeScript + React Query (frontend), Vitest + Testing Library (tests)

---

### Task 1: Sidebar/Profile refactor

**Items addressed:** #4 (Profile on avatar), #5 (remove Admin badge from Groups sidebar)

**Context:** `Sidebar.tsx` has a Profile nav item and an Admin badge on the Groups link. The user wants: Profile removed from sidebar, accessible via the avatar dropdown in `TopBar.tsx`. The Admin badge on Groups should be removed. `ProfilePage.tsx` should show the user's group memberships. The `handleListGroups` handler needs a `?member=me` filter so the profile can fetch only the user's groups.

**Files:**
- Modify: `web/src/components/Sidebar.tsx`
- Modify: `web/src/components/TopBar.tsx`
- Modify: `web/src/pages/ProfilePage.tsx`
- Modify: `internal/api/group_handlers.go`
- Modify: `web/src/test/Sidebar.test.tsx`

**Step 1: Update `handleListGroups` in `internal/api/group_handlers.go` to support `?member=me`**

Add after the existing `rows, err := s.db.Pool.Query(...)` block — check for `r.URL.Query().Get("member") == "me"` and use a JOIN query instead:

```go
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var rows pgx.Rows
	var err error

	if r.URL.Query().Get("member") == "me" {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT g.id, g.org_id, g.name, g.created_at, COUNT(gm2.user_id) AS member_count
			 FROM groups g
			 JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $2
			 LEFT JOIN group_members gm2 ON gm2.group_id = g.id
			 WHERE g.org_id = $1
			 GROUP BY g.id
			 ORDER BY g.name`,
			claims.OrgID, claims.UserID,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT g.id, g.org_id, g.name, g.created_at, COUNT(gm.user_id) AS member_count
			 FROM groups g
			 LEFT JOIN group_members gm ON gm.group_id = g.id
			 WHERE g.org_id = $1
			 GROUP BY g.id
			 ORDER BY g.name`,
			claims.OrgID,
		)
	}
	// ... rest unchanged
```

You need to add `"github.com/jackc/pgx/v5"` to imports if `pgx.Rows` is used (check if it's already imported — in this file it's `rows, err` from Pool.Query which returns `pgx.Rows` implicitly; just declare `var rows pgx.Rows` at the top or use `:=` in each branch).

**Step 2: Run backend tests**

```bash
cd /path/to/worktree && task test:api 2>&1 | grep -E "PASS|FAIL|ok|---"
```

Expected: all pass.

**Step 3: Remove Profile from sidebar and Admin badge from Groups in `web/src/components/Sidebar.tsx`**

Replace `NAV_ITEMS`:
```tsx
const NAV_ITEMS = [
  { to: '/',           title: 'Notebooks',   icon: <BookOpen size={16} /> },
  { to: '/dashboards', title: 'Dashboards',  icon: <LayoutDashboard size={16} /> },
  { to: '/connectors', title: 'Connectors',  icon: <Database size={16} /> },
  { to: '/members',    title: 'Members',     icon: <Users size={16} /> },
  { to: '/groups',     title: 'Groups',      icon: <UsersRound size={16} /> },
  { to: '/audit',      title: 'Audit',       icon: <ClipboardList size={16} /> },
]
```

Also remove `User` from the lucide-react import (no longer needed) and remove the `adminBadge` handling in the NavLink render — the `{adminBadge && isAdmin && ...}` span and the `adminBadge` field.

**Step 4: Add "Profile" link to the TopBar avatar dropdown in `web/src/components/TopBar.tsx`**

Add `import { Link } from 'react-router-dom'` if not already imported (it is). In the dropdown, add a Profile link before Sign out:

```tsx
{open && (
  <div style={styles.dropdown}>
    <div style={styles.dropdownHeader}>
      <div style={styles.dropdownName}>{name}</div>
      <div style={styles.dropdownEmail}>{email}</div>
    </div>
    <Link
      to="/profile"
      style={styles.dropdownLink}
      onClick={() => setOpen(false)}
    >
      Profile settings
    </Link>
    <button style={styles.signOut} onClick={() => { logout(); setOpen(false) }}>
      Sign out
    </button>
  </div>
)}
```

Add to styles:
```tsx
dropdownLink: {
  display: 'block',
  padding: '10px 14px',
  fontSize: 13,
  color: 'var(--text-primary)',
  textDecoration: 'none',
  borderBottom: '1px solid var(--border-light)',
},
```

**Step 5: Add "My Groups" section to `web/src/pages/ProfilePage.tsx`**

Add a groups query and display after the existing form fields:

```tsx
const { data: myGroups = [] } = useQuery({
  queryKey: ['groups', 'mine'],
  queryFn: () => api.get<{ id: string; name: string; member_count: number }[]>('/api/v1/groups?member=me'),
})
```

Then in the JSX, after the Save button section:
```tsx
<div style={{ marginTop: 32, borderTop: '1px solid var(--border-light)', paddingTop: 24 }}>
  <div style={{ fontSize: 12, fontWeight: 600, color: '#555', marginBottom: 12 }}>My Groups</div>
  {myGroups.length === 0 ? (
    <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>Not a member of any group.</div>
  ) : (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
      {myGroups.map(g => (
        <span key={g.id} style={styles.groupTag}>{g.name}</span>
      ))}
    </div>
  )}
</div>
```

Add to styles:
```tsx
groupTag: {
  padding: '3px 10px',
  background: 'var(--accent-light)',
  color: 'var(--accent)',
  borderRadius: 12,
  fontSize: 12,
  fontWeight: 500,
},
```

**Step 6: Fix `web/src/test/Sidebar.test.tsx`**

Remove the test `'shows Admin badge on Groups link for admin users'` (badge is gone) and update the nav items test to not expect Profile (it's no longer in the sidebar):

```tsx
it('renders all nav items (no Profile in sidebar)', () => {
  renderWithProviders(<Sidebar />)
  expect(screen.getByTitle('Notebooks')).toBeDefined()
  expect(screen.getByTitle('Dashboards')).toBeDefined()
  expect(screen.getByTitle('Connectors')).toBeDefined()
  expect(screen.getByTitle('Members')).toBeDefined()
  expect(screen.getByTitle('Groups')).toBeDefined()
  expect(screen.getByTitle('Audit')).toBeDefined()
  expect(screen.queryByTitle('Profile')).toBeNull()
})

it('does not show Admin badge on Groups link', () => {
  localStorage.setItem('hnb_sidebar_expanded', 'true')
  renderWithProviders(<Sidebar />)
  expect(screen.queryByText('Admin')).toBeNull()
})
```

Remove the `'shows Admin badge on Groups link for admin users'` and `'editor does not show Admin badge'` tests entirely — they tested the removed feature.

**Step 7: Run frontend tests**

```bash
cd web && npm run test:run 2>&1 | grep -E "Test Files|Tests "
```

Expected: all pass (count may drop by 1-2 tests for the removed badge tests).

**Step 8: Commit**

```bash
git add web/src/components/Sidebar.tsx web/src/components/TopBar.tsx \
        web/src/pages/ProfilePage.tsx web/src/test/Sidebar.test.tsx \
        internal/api/group_handlers.go
git commit -m "feat: profile in avatar dropdown, groups in profile, remove sidebar Admin badge"
```

---

### Task 2: Filesystem card metadata

**Item addressed:** #11 (show creator + date on filesystem cards)

**Context:** `HomePage.tsx` renders folder/notebook/connector/dashboard cards. Each has `created_by` (a user_id string) and `created_at` (ISO timestamp). The Members list (`GET /api/v1/members`) has `user_id` → `name` mapping. Fetch members and build a lookup map to resolve names. Show "by Name · date" below each card title.

**Files:**
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Add members query in `HomePage.tsx`**

Near the top of the `HomePage` component, after the existing queries, add:

```tsx
const { data: members = [] } = useQuery({
  queryKey: ['members'],
  queryFn: () => api.get<Array<{ user_id: string; name: string }>>('/api/v1/members'),
})
const memberName = (userId: string) =>
  members.find(m => m.user_id === userId)?.name ?? userId.slice(0, 8)
```

**Step 2: Add a `MetaLine` helper component**

Near the top of the file (after the existing helper components, before `HomePage`):

```tsx
function MetaLine({ createdBy, createdAt }: { createdBy: string; createdAt: string }) {
  const date = new Date(createdAt)
  const formatted = date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
  return (
    <span style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2, display: 'block', lineHeight: 1.4 }}>
      {createdBy} · {formatted}
    </span>
  )
}
```

In the component, call it as: `<MetaLine createdBy={memberName(item.created_by)} createdAt={item.created_at} />`

**Step 3: Add `<MetaLine>` to each card**

Find where each resource type renders its card name/title and add the MetaLine below. Look for the section that renders `folder.name`, `notebook.title`, etc. in the grid cards. For each one, add the MetaLine after the title span.

For example, for folders:
```tsx
<span style={s.cardTitle}>{folder.name}</span>
<MetaLine createdBy={memberName(folder.created_by)} createdAt={folder.created_at} />
```

Do the same for notebooks, connectors, and dashboards. Connectors in `FolderContents` don't have `created_by`/`created_at` in the partial type — skip MetaLine for connectors in the filesystem view, or add those fields to the `FolderContents.connectors` type if the API already returns them.

**Step 4: Run frontend tests**

```bash
cd web && npm run test:run -- src/test/HomePage.test.tsx 2>&1 | tail -6
```

Expected: all 21 tests still pass (MetaLine doesn't break existing assertions).

**Step 5: Commit**

```bash
git add web/src/pages/HomePage.tsx
git commit -m "feat: show creator name and date on filesystem cards"
```

---

### Task 3: Filesystem "Created by me" filter

**Item addressed:** #3 (filter in filesystem view)

**Context:** `HomePage.tsx` currently shows all items. Add a filter bar above the grid with two pills: "All" and "Mine" (created by current user). The `useAuth()` hook provides `user.user_id`. Filter is client-side only.

**Files:**
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Add filter state**

In the `HomePage` component, add:
```tsx
const { user } = useAuth()
const [filter, setFilter] = useState<'all' | 'mine'>('all')

const filterItem = <T extends { created_by: string }>(items: T[]): T[] =>
  filter === 'mine' ? items.filter(i => i.created_by === user?.user_id) : items
```

**Step 2: Add filter bar JSX**

Add this above the breadcrumb/toolbar section:
```tsx
<div style={{ display: 'flex', gap: 6, marginBottom: 12 }}>
  {(['all', 'mine'] as const).map(f => (
    <button
      key={f}
      onClick={() => setFilter(f)}
      style={{
        padding: '4px 12px',
        borderRadius: 20,
        fontSize: 12,
        fontWeight: 500,
        cursor: 'pointer',
        border: 'none',
        background: filter === f ? 'var(--accent)' : 'var(--accent-light)',
        color: filter === f ? '#fff' : 'var(--accent)',
      }}
    >
      {f === 'all' ? 'All' : 'Created by me'}
    </button>
  ))}
</div>
```

**Step 3: Apply filter to all resource lists**

Wherever `contents?.folders`, `contents?.notebooks`, `contents?.connectors`, `contents?.dashboards` are mapped in JSX, wrap them with `filterItem(...)`:

```tsx
{filterItem(contents?.folders ?? []).map(folder => ...)}
{filterItem(contents?.notebooks ?? []).map(notebook => ...)}
// connectors type may lack created_by — cast or skip
{filterItem(contents?.dashboards ?? []).map(dash => ...)}
```

Connectors in FolderContents don't have `created_by` in the partial type. Either skip filtering connectors or add `created_by?: string` to the FolderContents connector partial type in `web/src/types/index.ts`.

**Step 4: Run tests**

```bash
cd web && npm run test:run -- src/test/HomePage.test.tsx 2>&1 | tail -6
```

All 21 tests should still pass ("Files" breadcrumb and item display aren't broken by adding a filter bar).

**Step 5: Commit**

```bash
git add web/src/pages/HomePage.tsx web/src/types/index.ts
git commit -m "feat: 'Created by me' filter in filesystem view"
```

---

### Task 4: Edit connector — backend + frontend

**Items addressed:** #14 (can't edit connector via UI), #19 (clicking connector in filesystem navigates properly)

**Context:** There is no `PUT /api/v1/connectors/{id}` route. `ConnectorsPage.tsx` only has a create form. When a user clicks a connector in the filesystem, it currently navigates to `/connectors` with no way to edit.

Plan:
1. Add `handleUpdateConnector` to `connector_handlers.go`
2. Register `PUT /api/v1/connectors/{id}` in `router.go`
3. Add edit mode to `ConnectorsPage.tsx` — reads `?edit={id}` URL param and opens the form pre-filled
4. In `HomePage.tsx`, make the "open" action for connectors navigate to `/connectors?edit={id}`

**Files:**
- Modify: `internal/api/connector_handlers.go`
- Modify: `internal/api/router.go`
- Modify: `web/src/pages/ConnectorsPage.tsx`
- Modify: `web/src/pages/HomePage.tsx`
- Modify: `web/src/test/ConnectorsPage.test.tsx`

**Step 1: Add `handleUpdateConnector` to `internal/api/connector_handlers.go`**

Add after `handleListConnectors`:

```go
type updateConnectorRequest struct {
	Name      *string                 `json:"name,omitempty"`
	Config    *models.ConnectorConfig `json:"config,omitempty"`
	IsDefault *bool                   `json:"is_default,omitempty"`
}

func (s *Server) handleUpdateConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	ctx := r.Context()

	var req updateConnectorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Verify ownership
	var orgID string
	err := s.db.Pool.QueryRow(ctx, `SELECT org_id FROM connectors WHERE id=$1`, id).Scan(&orgID)
	if err != nil || orgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	if req.Name != nil {
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET name=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
			*req.Name, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if req.Config != nil {
		configJSON, err := json.Marshal(req.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid config")
			return
		}
		encrypted, err := crypto.Encrypt(configJSON, s.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt config")
			return
		}
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET config_encrypted=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
			encrypted, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if req.IsDefault != nil && *req.IsDefault {
		tx, err := s.db.Pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)
		tx.Exec(ctx, `UPDATE connectors SET is_default=false WHERE org_id=$1`, claims.OrgID)
		tx.Exec(ctx, `UPDATE connectors SET is_default=true WHERE id=$1`, id)
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	// Return updated connector (re-fetch)
	var c models.Connector
	var encryptedConfig []byte
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id
		 FROM connectors WHERE id=$1`,
		id,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if plain, err := crypto.Decrypt(encryptedConfig, s.masterKey); err == nil {
		json.Unmarshal(plain, &c.Config)
		c.Config.Password = "***"
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.update", ResourceType: "connector", ResourceID: id,
	})

	writeJSON(w, http.StatusOK, c)
}
```

**Step 2: Register route in `internal/api/router.go`**

After the existing connector routes, add:
```go
s.mux.Handle("PUT /api/v1/connectors/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateConnector))))
```

**Step 3: Write a backend test in `internal/api/connector_handlers_test.go`**

Read the existing test file to understand the pattern, then add:

```go
func TestUpdateConnector(t *testing.T) {
    ts := setupTestServer(t)
    // Create a connector first
    body := `{"name":"TestConn","type":"postgres","config":{"host":"localhost","port":5432,"database":"db","user":"u","password":"p"}}`
    res := ts.Do(t, "POST", "/api/v1/connectors", body, ts.AdminToken)
    require.Equal(t, http.StatusCreated, res.StatusCode)
    var created map[string]interface{}
    json.NewDecoder(res.Body).Decode(&created)
    id := created["id"].(string)

    // Update name
    res = ts.Do(t, "PUT", "/api/v1/connectors/"+id, `{"name":"UpdatedConn"}`, ts.AdminToken)
    require.Equal(t, http.StatusOK, res.StatusCode)
    var updated map[string]interface{}
    json.NewDecoder(res.Body).Decode(&updated)
    require.Equal(t, "UpdatedConn", updated["name"])
}
```

**Step 4: Run backend tests**

```bash
task test:api 2>&1 | grep -E "PASS|FAIL|ok |---"
```

**Step 5: Add edit mode to `ConnectorsPage.tsx`**

Add `useSearchParams` and `useEffect` to open the edit form when `?edit={id}` is in the URL:

```tsx
import { useSearchParams } from 'react-router-dom'
// ...
const [searchParams, setSearchParams] = useSearchParams()
const [editing, setEditing] = useState<string | null>(null)
const [editForm, setEditForm] = useState<ConnectorForm>(defaultForm())

// Open edit mode from URL param
useEffect(() => {
  const editId = searchParams.get('edit')
  if (editId && connectors.length > 0) {
    const c = connectors.find(x => x.id === editId)
    if (c) {
      setEditing(c.id)
      setEditForm({
        name: c.name,
        type: c.type as ConnectorType,
        host: c.config?.host ?? '',
        port: String(c.config?.port ?? 5432),
        database: c.config?.database ?? '',
        user: c.config?.user ?? '',
        password: '',  // never pre-fill password
        ssl_mode: c.config?.ssl_mode ?? 'disable',
        is_default: c.is_default ?? false,
      })
      setSearchParams({})  // clear ?edit= from URL
    }
  }
}, [searchParams, connectors])
```

Add `updateConnector` mutation:
```tsx
const updateConnector = useMutation({
  mutationFn: (id: string) => api.put<Connector>(`/api/v1/connectors/${id}`, {
    name: editForm.name,
    config: {
      host: editForm.host,
      port: parseInt(editForm.port),
      database: editForm.database,
      user: editForm.user,
      ...(editForm.password !== '' ? { password: editForm.password } : {}),
      ssl_mode: editForm.ssl_mode,
    },
    ...(editForm.is_default ? { is_default: true } : {}),
  }),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['connectors'] })
    setEditing(null)
    setEditForm(defaultForm())
  },
  onError: (e: Error) => setCreateError(e.message),
})
```

In the connectors table, add an "Edit" button per row that calls `setEditing(c.id)` and pre-fills `editForm`.

When `editing !== null`, show an edit form (same layout as the create form) with a "Save" button calling `updateConnector.mutate(editing)` and a "Cancel" button calling `setEditing(null)`.

**Step 6: Fix connector navigation from filesystem in `web/src/pages/HomePage.tsx`**

In the `ContextMenu` component, the "Delete" button for connectors and dashboards has a `// TODO` comment. Change the connector click behaviour so clicking the connector card title navigates to `/connectors?edit={id}`:

Find where connector cards are rendered (look for `connector.name` in the cards grid). Replace the card title with:

```tsx
<Link to={`/connectors?edit=${connector.id}`} style={{ textDecoration: 'none', color: 'inherit' }}>
  {connector.name}
</Link>
```

Also update the ContextMenu's Delete action for connectors to navigate:
```tsx
onClick={() => {
  navigate(`/connectors?edit=${target.id}`)
  onClose()
}}
```

Actually simpler: in ContextMenu, when `target.type === 'connector'`, replace the "Permissions" action with "Edit" that navigates to `/connectors?edit={id}`.

**Step 7: Run frontend tests**

```bash
cd web && npm run test:run 2>&1 | grep -E "Test Files|Tests "
```

**Step 8: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/router.go \
        internal/api/connector_handlers_test.go \
        web/src/pages/ConnectorsPage.tsx web/src/pages/HomePage.tsx
git commit -m "feat: edit connector via UI — PUT /api/v1/connectors/:id + edit form + filesystem navigate"
```

---

### Task 5: Duplicate cell

**Item addressed:** #16 (option to duplicate a cell in notebooks)

**Context:** The cell toolbar in `Cell.tsx` has actions (run, toggle source, etc.). We need to add a "Duplicate" button that calls a new backend endpoint which inserts a copy of the cell at `position + 1` (shifting others down) and returns the new cell.

**Files:**
- Modify: `internal/api/cell_handlers.go`
- Modify: `internal/api/router.go`
- Modify: `web/src/components/Cell.tsx`
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Add `handleDuplicateCell` in `internal/api/cell_handlers.go`**

```go
func (s *Server) handleDuplicateCell(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	ctx := r.Context()

	// Fetch source cell
	var src models.Cell
	var outputs, params []byte
	var lang, connID, title, desc, slug *string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, notebook_id, position, type, language, connector_id, source, outputs,
		        source_visible, cell_collapsed, slide_break, parameters,
		        COALESCE(title,''), COALESCE(description,''), COALESCE(slug,'')
		 FROM cells WHERE id=$1 AND notebook_id=$2`,
		cellID, nbID,
	).Scan(&src.ID, &src.NotebookID, &src.Position, &src.Type,
		&lang, &connID, &src.Source, &outputs,
		&src.SourceVisible, &src.CellCollapsed, &src.SlideBreak, &params,
		&title, &desc, &slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	// Verify notebook belongs to org
	var orgID string
	s.db.Pool.QueryRow(ctx, `SELECT org_id FROM notebooks WHERE id=$1`, nbID).Scan(&orgID)
	if orgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	// Shift cells after source position
	insertPos := src.Position + 1
	if _, err := s.db.Pool.Exec(ctx,
		`UPDATE cells SET position=position+1 WHERE notebook_id=$1 AND position>=$2`,
		nbID, insertPos,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// Insert duplicate (empty outputs, new id)
	var newCell models.Cell
	var newOutputs, newParams []byte
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO cells (notebook_id, position, type, language, connector_id, source, outputs,
		                    source_visible, cell_collapsed, slide_break, parameters, title, description, slug)
		 VALUES ($1,$2,$3,$4,$5,$6,'[]',$7,$8,$9,$10,$11,$12,$13)
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
		           source_visible, cell_collapsed, slide_break, parameters,
		           COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		           created_at, updated_at`,
		nbID, insertPos, src.Type, lang, connID, src.Source,
		src.SourceVisible, src.CellCollapsed, src.SlideBreak, params, title, desc, slug,
	).Scan(&newCell.ID, &newCell.NotebookID, &newCell.Position, &newCell.Type,
		&lang, &connID, &newCell.Source, &newOutputs,
		&newCell.SourceVisible, &newCell.CellCollapsed, &newCell.SlideBreak, &newParams,
		&newCell.Title, &newCell.Description, &newCell.Slug,
		&newCell.CreatedAt, &newCell.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to duplicate cell")
		return
	}
	if lang != nil { newCell.Language = *lang }
	if connID != nil { newCell.ConnectorID = connID }
	json.Unmarshal(newOutputs, &newCell.Outputs)
	json.Unmarshal(newParams, &newCell.Parameters)
	if newCell.Outputs == nil { newCell.Outputs = []models.Output{} }

	writeJSON(w, http.StatusCreated, newCell)
}
```

**Step 2: Register route in `internal/api/router.go`**

After the existing cell routes:
```go
s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/duplicate", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDuplicateCell))))
```

**Step 3: Write backend test in `internal/api/cell_handlers_test.go`**

Read the existing test patterns in that file, then add a test that:
1. Creates a notebook
2. Creates a cell
3. POSTs to `/api/v1/notebooks/{nb_id}/cells/{cell_id}/duplicate`
4. Asserts 201, `source` matches original, `position` is `original + 1`

**Step 4: Run backend tests**

```bash
task test:api 2>&1 | grep -E "PASS|FAIL|ok |---"
```

**Step 5: Add Duplicate button to `web/src/components/Cell.tsx`**

Read `Cell.tsx` to find the toolbar/actions section. Add a "Duplicate" button (use the `Copy` icon from lucide-react) that calls an `onDuplicate` prop:

```tsx
// In Cell component props:
onDuplicate?: () => void

// In toolbar JSX, near other action buttons:
{onDuplicate && (
  <button style={toolbarBtn} onClick={onDuplicate} title="Duplicate cell">
    <Copy size={14} />
  </button>
)}
```

**Step 6: Wire up `onDuplicate` in `NotebookPage.tsx`**

In `NotebookPage.tsx`, find where `<NotebookCell>` is rendered and add:
```tsx
onDuplicate={() => duplicateCell.mutate(cell.id)}
```

Add the mutation near the other mutations:
```tsx
const duplicateCell = useMutation({
  mutationFn: (cellId: string) =>
    api.post<Cell>(`/api/v1/notebooks/${id}/cells/${cellId}/duplicate`, {}),
  onSuccess: (newCell) => {
    setLocalCells(prev => {
      const idx = prev.findIndex(c => c.position >= newCell.position && c.id !== newCell.id)
      const shifted = prev.map(c =>
        c.position >= newCell.position ? { ...c, position: c.position + 1 } : c
      )
      return [...shifted, newCell].sort((a, b) => a.position - b.position)
    })
  },
})
```

**Step 7: Run frontend tests**

```bash
cd web && npm run test:run 2>&1 | grep -E "Test Files|Tests "
```

**Step 8: Commit**

```bash
git add internal/api/cell_handlers.go internal/api/router.go \
        internal/api/cell_handlers_test.go \
        web/src/components/Cell.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: duplicate cell — POST /api/v1/.../duplicate + Copy button in cell toolbar"
```

---

### Task 6: Permissions presets

**Item addressed:** #13 (preset configs for None/Viewer/Editor/Admin in permissions panel)

**Context:** `PermissionsPanel.tsx` shows a row of checkboxes per ACL entry. The user wants quick "preset" buttons that tick/untick a predefined set of actions. Presets should respect the resource type (e.g. notebooks have `run`, dashboards don't).

Presets (actions to grant):
- **None** — `[]`
- **Viewer** — `['view']`
- **Editor** — `['view', 'run', 'edit']` (notebook); `['view', 'edit']` (dashboard/connector); `['view', 'create', 'edit']` (folder)
- **Admin** — all actions for that resource type

**Files:**
- Modify: `web/src/components/PermissionsPanel.tsx`

**Step 1: Define preset map in `PermissionsPanel.tsx`**

After the `ACTION_LABELS` constant:

```tsx
const PRESETS: Record<ResourceType, Record<string, string[]>> = {
  folder:    { none: [], viewer: ['view'], editor: ['view', 'create', 'edit'], admin: ['view', 'create', 'edit', 'manage', 'delete'] },
  notebook:  { none: [], viewer: ['view'], editor: ['view', 'run', 'edit'], admin: ['view', 'run', 'edit', 'share', 'delete'] },
  connector: { none: [], viewer: ['view'], editor: ['view', 'use', 'edit'], admin: ['view', 'use', 'edit', 'share', 'delete'] },
  dashboard: { none: [], viewer: ['view'], editor: ['view', 'edit'], admin: ['view', 'edit', 'share', 'delete'] },
}
```

**Step 2: Add preset buttons to the entry row UI**

In the section that renders existing ACL entries (where checkboxes are shown per entry), add a row of preset buttons below the checkboxes for each entry. This updates the entry's actions in the draft.

Find the render for existing entries — it iterates `draft` and renders checkboxes. After the checkboxes row, add:

```tsx
<div style={{ display: 'flex', gap: 4, marginTop: 4 }}>
  {(['none', 'viewer', 'editor', 'admin'] as const).map(preset => (
    <button
      key={preset}
      onClick={() => applyPreset(entry.id, preset)}
      style={{
        padding: '2px 8px',
        fontSize: 10,
        fontWeight: 600,
        borderRadius: 3,
        border: '1px solid var(--border)',
        background: 'transparent',
        color: 'var(--text-muted)',
        cursor: 'pointer',
        textTransform: 'capitalize',
      }}
    >
      {preset}
    </button>
  ))}
</div>
```

**Step 3: Add `applyPreset` handler in the component**

```tsx
function applyPreset(entryId: string, preset: 'none' | 'viewer' | 'editor' | 'admin') {
  const actions = PRESETS[resourceType][preset]
  setDraft(prev =>
    prev.map(e => e.id === entryId ? { ...e, actions } : e)
  )
  setDirty(true)
}
```

**Step 4: Run frontend tests**

```bash
cd web && npm run test:run -- src/test/PermissionsPanel.test.tsx 2>&1 | tail -6
```

**Step 5: Commit**

```bash
git add web/src/components/PermissionsPanel.tsx
git commit -m "feat: permission presets (None/Viewer/Editor/Admin) in PermissionsPanel"
```

---

### Task 7: Full suite green + merge

**Step 1: Run complete frontend test suite**

```bash
cd web && npm run test:run 2>&1 | tail -6
```

Expected: all files pass.

**Step 2: Run backend tests (requires infra)**

```bash
task test:api 2>&1 | grep -E "PASS|FAIL|ok |---"
```

**Step 3: If anything fails, investigate and fix**

**Step 4: Merge to main**

```bash
git checkout main
git merge feature/improvements --no-ff -m "feat: improvements batch 1 — profile/sidebar, filesystem metadata+filter, connector edit, duplicate cell, permissions presets"
git worktree remove .worktrees/improvements
git branch -d feature/improvements
```
