# Phase 1 — Shell & Quick Wins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the horizontal NavBar with a sidebar+topbar shell, add notebook descriptions, list-view toggle, profile menu, fix collaboration cursor names, enrich audit log with names, and replace raw type badges with icons — all with no schema changes except adding `notebooks.description`.

**Architecture:** New `AppShell` wrapper component provides a `<TopBar>` + `<Sidebar>` layout; all authenticated pages render inside it. `NavBar.tsx` is deleted. The testing infrastructure (Vitest + RTL + Playwright) is bootstrapped in Task 1 so tests can be written throughout.

**Tech Stack:** Go 1.25, React 19, Vite 8, CodeMirror 6, Vitest, @testing-library/react, MSW v2, Playwright

---

## File Map

**Create:**
- `web/src/components/TopBar.tsx` — slim top bar (logo, org name, profile dropdown)
- `web/src/components/Sidebar.tsx` — collapsible icon-rail nav
- `web/src/components/AppShell.tsx` — layout wrapper used by all authenticated pages
- `web/src/test/setup.ts` — Vitest global setup (jest-dom)
- `web/src/test/handlers.ts` — MSW request handlers for component tests
- `web/src/test/server.ts` — MSW server setup
- `web/src/test/Sidebar.test.tsx`
- `web/src/test/OutputRenderer.test.tsx`
- `e2e/playwright.config.ts`
- `e2e/navigation.spec.ts`
- `internal/database/migrations/002_notebook_description.sql`

**Modify:**
- `web/vite.config.ts` — add `test` block
- `web/package.json` — add test scripts + deps
- `Taskfile.yml` — add `test:web`, `test:web:watch`, `test:e2e`, `test:e2e:ui` tasks
- `internal/models/notebook.go` — add `Description` field
- `internal/api/notebook_handlers.go` — include description in all queries
- `internal/audit/audit.go` — add `UserEmail`, `ResourceName` to `Entry`; enrich `Query`
- `web/src/hooks/useAuth.ts` — store `name`+`email` in localStorage on login/register
- `web/src/api/auth.ts` — return full auth response including user name/email
- `web/src/components/CellToolbar.tsx` — no change needed in Phase 1
- `web/src/components/CodeCell.tsx` — pass awareness with user identity
- `web/src/components/OutputRenderer.tsx` — replace type string with icon+tooltip
- `web/src/pages/HomePage.tsx` — use AppShell, add list/grid toggle, show description
- `web/src/pages/DashboardsPage.tsx` — use AppShell, add list/grid toggle
- `web/src/pages/NotebookPage.tsx` — use AppShell, add description field
- `web/src/pages/ConnectorsPage.tsx` — use AppShell
- `web/src/pages/AuditPage.tsx` — use AppShell, show user_email + resource_name
- `web/src/pages/MembersPage.tsx` — use AppShell
- `web/src/App.tsx` — remove AppShell import from pages (AppShell wraps inside each page)
- `web/src/types/index.ts` — add `description` to Notebook, `user_email`+`resource_name` to AuditEntry

**Delete:**
- `web/src/components/NavBar.tsx`

---

## Task 1: Testing Infrastructure

**Files:**
- Modify: `web/package.json`
- Modify: `web/vite.config.ts`
- Create: `web/src/test/setup.ts`
- Create: `web/src/test/handlers.ts`
- Create: `web/src/test/server.ts`
- Modify: `Taskfile.yml`
- Create: `e2e/playwright.config.ts`

- [ ] **Step 1: Install frontend test dependencies**

```bash
cd web && npm install --save-dev vitest @vitest/coverage-v8 @testing-library/react @testing-library/user-event @testing-library/jest-dom jsdom msw
```

Expected: packages added to `web/node_modules/`, `package-lock.json` updated.

- [ ] **Step 2: Add test script to `web/package.json`**

Replace the `"scripts"` block:
```json
"scripts": {
  "dev": "vite",
  "build": "tsc -b && vite build",
  "lint": "eslint .",
  "preview": "vite preview",
  "test": "vitest",
  "test:run": "vitest run",
  "test:coverage": "vitest run --coverage"
},
```

- [ ] **Step 3: Add Vitest config to `web/vite.config.ts`**

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const apiTarget = process.env.API_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    proxy: {
      '/api': apiTarget,
      '/internal': apiTarget,
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
})
```

- [ ] **Step 4: Create `web/src/test/setup.ts`**

```typescript
import '@testing-library/jest-dom'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import { server } from './server'

beforeAll(() => server.listen({ onUnhandledRequest: 'warn' }))
afterEach(() => {
  cleanup()
  server.resetHandlers()
})
afterAll(() => server.close())
```

- [ ] **Step 5: Create `web/src/test/handlers.ts`**

```typescript
import { http, HttpResponse } from 'msw'

export const handlers = [
  http.get('/api/v1/notebooks', () =>
    HttpResponse.json([
      { id: 'nb-1', org_id: 'org-1', title: 'Test Notebook', description: '', parameters: [], created_by: 'u1', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
    ])
  ),
  http.get('/api/v1/connectors', () => HttpResponse.json([])),
  http.get('/api/v1/members', () => HttpResponse.json([])),
  http.get('/api/v1/audit', () => HttpResponse.json([])),
  http.get('/api/v1/dashboards', () => HttpResponse.json([])),
]
```

- [ ] **Step 6: Create `web/src/test/server.ts`**

```typescript
import { setupServer } from 'msw/node'
import { handlers } from './handlers'

export const server = setupServer(...handlers)
```

- [ ] **Step 7: Install Playwright**

```bash
cd /home/jesus/Projects/hnb-claude && npm init -y --prefix e2e 2>/dev/null; cd e2e && npm install --save-dev @playwright/test && npx playwright install chromium
```

Expected: Playwright installed, Chromium downloaded.

- [ ] **Step 8: Create `e2e/playwright.config.ts`**

```typescript
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  retries: 0,
  timeout: 30_000,
  use: {
    baseURL: 'http://localhost:5173',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  snapshotDir: './snapshots',
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
```

- [ ] **Step 9: Add tasks to `Taskfile.yml`**

Add after the existing `test:e2e` task:
```yaml
  test:web:
    desc: Run frontend component tests (Vitest)
    dir: web
    cmds:
      - npm run test:run

  test:web:watch:
    desc: Run frontend component tests in watch mode
    dir: web
    cmds:
      - npm run test

  test:e2e:pw:
    desc: Run Playwright E2E tests (requires running dev stack on :5173)
    cmds:
      - npx playwright test --config=e2e/playwright.config.ts

  test:e2e:pw:ui:
    desc: Open Playwright UI mode
    cmds:
      - npx playwright test --ui --config=e2e/playwright.config.ts
```

- [ ] **Step 10: Verify Vitest runs (no tests yet = passes vacuously)**

```bash
cd web && npm run test:run
```

Expected: `No test files found` or 0 tests passing — no errors.

- [ ] **Step 11: Commit**

```bash
git add web/package.json web/package-lock.json web/vite.config.ts web/src/test/ e2e/playwright.config.ts Taskfile.yml
git commit -m "feat: add Vitest + RTL + MSW + Playwright testing infrastructure"
```

---

## Task 2: Migration — `notebooks.description`

**Files:**
- Create: `internal/database/migrations/002_notebook_description.sql`
- Modify: `internal/models/notebook.go`

- [ ] **Step 1: Write the migration**

Create `internal/database/migrations/002_notebook_description.sql`:
```sql
ALTER TABLE notebooks ADD COLUMN IF NOT EXISTS description TEXT;
```

- [ ] **Step 2: Add `Description` to the Go model**

In `internal/models/notebook.go`, add `Description` after `Title`:
```go
type Notebook struct {
	ID          string      `json:"id"`
	OrgID       string      `json:"org_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Parameters  []Parameter `json:"parameters"`
	CreatedBy   string      `json:"created_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}
```

- [ ] **Step 3: Write a failing test for notebook description**

Add to `internal/api/notebook_handlers_test.go` (create the file if it doesn't exist — if the package is `api_test`, use that):
```go
func TestNotebookDescription(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "desc@example.com", "DescOrg")

	// Create notebook with description
	body, _ := json.Marshal(map[string]string{"title": "My NB", "description": "A test notebook"})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create notebook: %d %s", rec.Code, rec.Body.String())
	}

	var nb map[string]any
	json.NewDecoder(rec.Body).Decode(&nb)
	if nb["description"] != "A test notebook" {
		t.Fatalf("expected description 'A test notebook', got %v", nb["description"])
	}
}
```

- [ ] **Step 4: Run the test — expect it to fail**

```bash
task test:api 2>&1 | grep -A3 "TestNotebookDescription"
```

Expected: FAIL — description field missing or empty.

- [ ] **Step 5: Update `handleCreateNotebook` in `internal/api/notebook_handlers.go`**

```go
type createNotebookRequest struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Parameters  []models.Parameter `json:"parameters"`
}

func (s *Server) handleCreateNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	params, _ := json.Marshal(req.Parameters)
	if req.Parameters == nil {
		params = []byte("[]")
	}
	ctx := r.Context()
	var nb models.Notebook
	var paramsOut []byte
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, description, parameters, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, org_id, title, COALESCE(description,''), parameters, created_by, created_at, updated_at`,
		claims.OrgID, req.Title, req.Description, params, claims.UserID,
	).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &paramsOut, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notebook")
		return
	}
	json.Unmarshal(paramsOut, &nb.Parameters)
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "notebook.create", ResourceType: "notebook", ResourceID: nb.ID,
	})
	writeJSON(w, http.StatusCreated, nb)
}
```

- [ ] **Step 6: Update `handleListNotebooks` to include description**

Replace the SELECT in `handleListNotebooks`:
```go
rows, err := s.db.Pool.Query(ctx,
    `SELECT id, org_id, title, COALESCE(description,''), parameters, created_by, created_at, updated_at
     FROM notebooks WHERE org_id = $1 ORDER BY updated_at DESC`,
    claims.OrgID,
)
```
And scan: `rows.Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)`

- [ ] **Step 7: Update `handleGetNotebook` to include description**

Replace the SELECT in `handleGetNotebook`:
```go
err := s.db.Pool.QueryRow(ctx,
    `SELECT id, org_id, title, COALESCE(description,''), parameters, created_by, created_at, updated_at
     FROM notebooks WHERE id = $1 AND org_id = $2`,
    nbID, claims.OrgID,
).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &params, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
```

- [ ] **Step 8: Update `handleUpdateNotebook` to support description**

In `updateNotebookRequest` add `Description *string \`json:"description,omitempty"\``.

In `handleUpdateNotebook`, after the existing `req.Title` check:
```go
if req.Description != nil {
    query += fmt.Sprintf(", description = $%d", argN)
    args = append(args, *req.Description)
    argN++
}
```

Update RETURNING clause and Scan to include `COALESCE(description,'')` and `&nb.Description`.

- [ ] **Step 9: Update the `Notebook` type in `web/src/types/index.ts`**

```typescript
export interface Notebook {
  id: string
  org_id: string
  title: string
  description: string
  created_by: string
  created_at: string
  updated_at: string
  parameters?: Parameter[]
}
```

- [ ] **Step 10: Run tests and apply migration**

```bash
task test:api 2>&1 | grep -E "PASS|FAIL|TestNotebook"
```

Expected: `TestNotebookDescription --- PASS`

- [ ] **Step 11: Commit**

```bash
git add internal/database/migrations/002_notebook_description.sql internal/models/notebook.go internal/api/notebook_handlers.go web/src/types/index.ts
git commit -m "feat: add description field to notebooks"
```

---

## Task 3: Audit Log Enrichment

**Files:**
- Modify: `internal/audit/audit.go`
- Modify: `web/src/types/index.ts`
- Modify: `web/src/pages/AuditPage.tsx`

- [ ] **Step 1: Write a failing test for enriched audit entries**

Add to `internal/api/audit_handlers_test.go`:
```go
func TestAuditLogEnrichment(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "audit@example.com", "AuditOrg")
	nbID := createNotebook(t, srv, token, "My Notebook")

	// Fetch audit logs
	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body.String())
	}

	var entries []map[string]any
	json.NewDecoder(rec.Body).Decode(&entries)
	if len(entries) == 0 {
		t.Fatal("expected audit entries")
	}
	// The notebook.create entry should have resource_name = "My Notebook"
	for _, e := range entries {
		if e["action"] == "notebook.create" {
			if e["resource_name"] != "My Notebook" {
				t.Fatalf("expected resource_name 'My Notebook', got %v", e["resource_name"])
			}
			if e["user_email"] == "" || e["user_email"] == nil {
				t.Fatalf("expected user_email, got %v", e["user_email"])
			}
			return
		}
	}
	t.Fatal("notebook.create entry not found")
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
task test:api 2>&1 | grep -A3 "TestAuditLogEnrichment"
```

Expected: FAIL — `resource_name` and `user_email` fields missing.

- [ ] **Step 3: Update `audit.Entry` in `internal/audit/audit.go`**

```go
type Entry struct {
	ID           int64                  `json:"id"`
	OrgID        string                 `json:"org_id"`
	UserID       string                 `json:"user_id,omitempty"`
	UserEmail    string                 `json:"user_email,omitempty"`
	Action       string                 `json:"action"`
	ResourceType string                 `json:"resource_type"`
	ResourceID   string                 `json:"resource_id,omitempty"`
	ResourceName string                 `json:"resource_name,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}
```

- [ ] **Step 4: Update `audit.Query` to join user email and resource name**

Replace the `Query` method body:
```go
func (l *Logger) Query(ctx context.Context, p QueryParams) ([]Entry, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}

	query := `
		SELECT
			al.id, al.org_id,
			COALESCE(al.user_id::text, ''),
			COALESCE(u.email, ''),
			al.action, al.resource_type,
			COALESCE(al.resource_id::text, ''),
			COALESCE(
				CASE al.resource_type
					WHEN 'notebook'  THEN (SELECT title FROM notebooks WHERE id = al.resource_id)
					WHEN 'dashboard' THEN (SELECT title FROM dashboards WHERE id = al.resource_id)
					WHEN 'connector' THEN (SELECT name  FROM connectors WHERE id = al.resource_id)
					WHEN 'user'      THEN (SELECT name  FROM users     WHERE id = al.resource_id)
					ELSE ''
				END, ''
			),
			al.metadata, al.created_at
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.user_id
		WHERE al.org_id = $1`
	args := []interface{}{p.OrgID}
	argN := 2

	if p.UserID != "" {
		query += fmt.Sprintf(" AND al.user_id = $%d", argN)
		args = append(args, p.UserID)
		argN++
	}
	if p.Action != "" {
		query += fmt.Sprintf(" AND al.action = $%d", argN)
		args = append(args, p.Action)
		argN++
	}
	if p.ResourceType != "" {
		query += fmt.Sprintf(" AND al.resource_type = $%d", argN)
		args = append(args, p.ResourceType)
		argN++
	}

	query += fmt.Sprintf(" ORDER BY al.created_at DESC LIMIT $%d OFFSET $%d", argN, argN+1)
	args = append(args, p.Limit, p.Offset)

	rows, err := l.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var metaJSON []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.UserID, &e.UserEmail, &e.Action,
			&e.ResourceType, &e.ResourceID, &e.ResourceName, &metaJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &e.Metadata)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
```

- [ ] **Step 5: Run test — expect PASS**

```bash
task test:api 2>&1 | grep -A3 "TestAuditLogEnrichment"
```

Expected: `PASS`

- [ ] **Step 6: Update `AuditEntry` in `web/src/types/index.ts`**

```typescript
export interface AuditEntry {
  id: string
  org_id: string
  user_id: string
  user_email: string
  action: string
  resource_type: string
  resource_id: string
  resource_name: string
  created_at: string
}
```

- [ ] **Step 7: Update `web/src/pages/AuditPage.tsx` to display names**

In the audit table, replace the `user_id` and `resource_id` cells:
```tsx
<td style={styles.td} title={entry.user_id}>{entry.user_email || entry.user_id}</td>
<td style={styles.td} title={entry.resource_id}>{entry.resource_name || entry.resource_id}</td>
```

- [ ] **Step 8: Commit**

```bash
git add internal/audit/audit.go web/src/types/index.ts web/src/pages/AuditPage.tsx internal/api/audit_handlers_test.go
git commit -m "feat: enrich audit log entries with user email and resource name"
```

---

## Task 4: TopBar + Sidebar (Replace NavBar)

**Files:**
- Create: `web/src/components/TopBar.tsx`
- Create: `web/src/components/Sidebar.tsx`
- Create: `web/src/components/AppShell.tsx`
- Create: `web/src/test/Sidebar.test.tsx`
- Delete: `web/src/components/NavBar.tsx`
- Modify: all 6 page files

- [ ] **Step 1: Write a failing Sidebar test**

Create `web/src/test/Sidebar.test.tsx`:
```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from '../components/Sidebar'

beforeEach(() => {
  localStorage.clear()
})

describe('Sidebar', () => {
  it('renders all 5 navigation items', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )
    expect(screen.getByTitle('Notebooks')).toBeDefined()
    expect(screen.getByTitle('Dashboards')).toBeDefined()
    expect(screen.getByTitle('Connectors')).toBeDefined()
    expect(screen.getByTitle('Members')).toBeDefined()
    expect(screen.getByTitle('Audit')).toBeDefined()
  })

  it('persists expanded state to localStorage on toggle', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )
    const toggle = screen.getByTitle('Expand sidebar')
    fireEvent.click(toggle)
    expect(localStorage.getItem('hnb_sidebar_expanded')).toBe('true')
    fireEvent.click(toggle)
    expect(localStorage.getItem('hnb_sidebar_expanded')).toBe('false')
  })
})
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd web && npm run test:run 2>&1 | tail -20
```

Expected: FAIL — `Sidebar` component not found.

- [ ] **Step 3: Create `web/src/components/Sidebar.tsx`**

```tsx
import { useState } from 'react'
import { NavLink } from 'react-router-dom'

const NAV_ITEMS = [
  { to: '/',           title: 'Notebooks',   icon: '▦' },
  { to: '/dashboards', title: 'Dashboards',  icon: '⊞' },
  { to: '/connectors', title: 'Connectors',  icon: '⚡' },
  { to: '/members',    title: 'Members',     icon: '👥' },
  { to: '/audit',      title: 'Audit',       icon: '📋' },
]

export function Sidebar() {
  const [expanded, setExpanded] = useState(() => {
    return localStorage.getItem('hnb_sidebar_expanded') === 'true'
  })

  const toggle = () => {
    const next = !expanded
    setExpanded(next)
    localStorage.setItem('hnb_sidebar_expanded', String(next))
  }

  const width = expanded ? 200 : 48

  return (
    <nav style={{ ...styles.sidebar, width }}>
      <div style={styles.items}>
        {NAV_ITEMS.map(({ to, title, icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            title={title}
            style={({ isActive }) => ({
              ...styles.item,
              background: isActive ? 'var(--accent-light)' : 'transparent',
              color: isActive ? 'var(--accent)' : 'var(--text-muted)',
            })}
          >
            <span style={styles.icon}>{icon}</span>
            {expanded && <span style={styles.label}>{title}</span>}
          </NavLink>
        ))}
      </div>
      <button style={styles.toggle} onClick={toggle} title={expanded ? 'Collapse sidebar' : 'Expand sidebar'}>
        {expanded ? '◀' : '▶'}
      </button>
    </nav>
  )
}

const styles: Record<string, React.CSSProperties> = {
  sidebar: {
    display: 'flex',
    flexDirection: 'column',
    background: 'var(--nav-bg)',
    borderRight: '1px solid var(--nav-border)',
    flexShrink: 0,
    transition: 'width 0.2s ease',
    overflow: 'hidden',
  },
  items: {
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    padding: '8px 0',
    gap: 2,
  },
  item: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '8px 12px',
    textDecoration: 'none',
    borderRadius: 6,
    margin: '0 4px',
    fontSize: 13,
    fontWeight: 500,
    whiteSpace: 'nowrap',
    transition: 'background 0.15s, color 0.15s',
  },
  icon: {
    fontSize: 16,
    flexShrink: 0,
    width: 24,
    textAlign: 'center',
  },
  label: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
  },
  toggle: {
    background: 'transparent',
    border: 'none',
    padding: '12px',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 11,
    borderTop: '1px solid var(--nav-border)',
  },
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd web && npm run test:run 2>&1 | grep -E "PASS|FAIL|Sidebar"
```

Expected: `PASS`

- [ ] **Step 5: Create `web/src/components/TopBar.tsx`**

```tsx
import { useState, useRef, useEffect } from 'react'
import { useAuth } from '../hooks/useAuth'

export function TopBar() {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const name = localStorage.getItem('hnb_user_name') ?? ''
  const email = localStorage.getItem('hnb_user_email') ?? ''
  const orgName = localStorage.getItem('hnb_org_name') ?? ''
  const initials = name ? name[0].toUpperCase() : email ? email[0].toUpperCase() : '?'

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  if (!user) return null

  return (
    <header style={styles.bar}>
      <div style={styles.brand}>
        <div style={styles.logo}>▦</div>
        <span style={styles.appName}>hnb</span>
      </div>
      <span style={styles.orgName}>{orgName}</span>
      <div style={styles.right} ref={ref}>
        <button style={styles.avatar} onClick={() => setOpen(o => !o)} aria-label="Profile menu">
          {initials}
        </button>
        {open && (
          <div style={styles.dropdown}>
            <div style={styles.dropdownHeader}>
              <div style={styles.dropdownName}>{name}</div>
              <div style={styles.dropdownEmail}>{email}</div>
            </div>
            <button style={styles.signOut} onClick={() => { logout(); setOpen(false) }}>
              Sign out
            </button>
          </div>
        )}
      </div>
    </header>
  )
}

const styles: Record<string, React.CSSProperties> = {
  bar: {
    height: 44,
    background: 'var(--nav-bg)',
    borderBottom: '1px solid var(--nav-border)',
    display: 'flex',
    alignItems: 'center',
    padding: '0 16px',
    gap: 12,
    flexShrink: 0,
    zIndex: 10,
  },
  brand: { display: 'flex', alignItems: 'center', gap: 8 },
  logo: {
    width: 26, height: 26,
    background: 'var(--accent)',
    borderRadius: 6,
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    fontSize: 14, color: 'white',
  },
  appName: { fontSize: 14, fontWeight: 700, color: 'var(--nav-text)' },
  orgName: { flex: 1, fontSize: 12, color: 'var(--text-muted)', fontWeight: 500 },
  right: { position: 'relative' },
  avatar: {
    width: 30, height: 30,
    borderRadius: '50%',
    background: 'var(--accent-light)',
    border: '1.5px solid var(--accent)',
    color: 'var(--accent)',
    fontSize: 13, fontWeight: 700,
    cursor: 'pointer',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
  },
  dropdown: {
    position: 'absolute', right: 0, top: 38,
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: '0 4px 16px rgba(0,0,0,0.12)',
    minWidth: 200,
    zIndex: 100,
    overflow: 'hidden',
  },
  dropdownHeader: { padding: '12px 14px', borderBottom: '1px solid var(--border-light)' },
  dropdownName: { fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  dropdownEmail: { fontSize: 12, color: 'var(--text-muted)', marginTop: 2 },
  signOut: {
    width: '100%', padding: '10px 14px',
    background: 'transparent', border: 'none',
    fontSize: 13, color: 'var(--error)',
    cursor: 'pointer', textAlign: 'left',
    fontWeight: 500,
  },
}
```

- [ ] **Step 6: Create `web/src/components/AppShell.tsx`**

```tsx
import { TopBar } from './TopBar'
import { Sidebar } from './Sidebar'

interface Props {
  children: React.ReactNode
}

export function AppShell({ children }: Props) {
  return (
    <div style={styles.root}>
      <TopBar />
      <div style={styles.body}>
        <Sidebar />
        <main style={styles.main}>{children}</main>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  root: { display: 'flex', flexDirection: 'column', minHeight: '100vh', background: 'var(--bg-primary)' },
  body: { display: 'flex', flex: 1, overflow: 'hidden' },
  main: { flex: 1, overflow: 'auto', padding: '32px' },
}
```

- [ ] **Step 7: Update `useAuth.ts` to store name/email in localStorage on login**

Replace the `login` and `register` callbacks in `web/src/hooks/useAuth.ts`:
```typescript
const login = useCallback(async (email: string, password: string) => {
  const resp = await apiLogin(email, password)
  localStorage.setItem('hnb_user_name', resp.user.name)
  localStorage.setItem('hnb_user_email', resp.user.email)
  localStorage.setItem('hnb_org_name', resp.org.name)
  const claims = parseJwt(resp.token)
  if (claims) {
    setUser({ user_id: claims.sub as string, org_id: claims.org_id as string, role: claims.role as string })
  }
}, [])

const register = useCallback(async (email: string, password: string, name: string, orgName: string) => {
  const resp = await apiRegister(email, password, name, orgName)
  localStorage.setItem('hnb_user_name', resp.user.name)
  localStorage.setItem('hnb_user_email', resp.user.email)
  localStorage.setItem('hnb_org_name', resp.org.name)
  const claims = parseJwt(resp.token)
  if (claims) {
    setUser({ user_id: claims.sub as string, org_id: claims.org_id as string, role: claims.role as string })
  }
}, [])

const logout = useCallback(() => {
  apiLogout()
  localStorage.removeItem('hnb_user_name')
  localStorage.removeItem('hnb_user_email')
  localStorage.removeItem('hnb_org_name')
  setUser(null)
}, [])
```

- [ ] **Step 8: Update `web/src/api/auth.ts` to return the full auth response**

In `web/src/api/auth.ts`, update login and register to return the full response object instead of just the token. First read the file:

The existing `apiLogin` and `apiRegister` functions should return the full `authResponse`. Check the current implementation; if they only return `token`, update them to return the full object and update `useAuth.ts` accordingly. The key change is:

```typescript
// auth.ts — login
export async function login(email: string, password: string): Promise<{ token: string; user: { name: string; email: string }; org: { name: string } }> {
  const resp = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ message: 'Login failed' }))
    throw new Error(err.message ?? 'Login failed')
  }
  const data = await resp.json()
  localStorage.setItem('hnb_token', data.token)
  return data
}
```

Apply the same pattern to `register`.

- [ ] **Step 9: Update all pages to use AppShell**

For each page (`HomePage`, `DashboardsPage`, `ConnectorsPage`, `AuditPage`, `MembersPage`, `NotebookPage`):
1. Remove `import { NavBar } from '../components/NavBar'`
2. Add `import { AppShell } from '../components/AppShell'`
3. Replace `<NavBar activePage="..." />` with nothing (AppShell wraps the content)
4. Replace the outermost `<div style={styles.page}>` + `<NavBar>` pattern with `<AppShell><div style={styles.content}>...</div></AppShell>`

Example for `HomePage`:
```tsx
// Before
return (
  <div style={styles.page}>
    <NavBar activePage="notebooks" />
    <main style={styles.main}>
      <div style={styles.content}>...</div>
    </main>
  </div>
)

// After
return (
  <AppShell>
    <div style={styles.content}>...</div>
  </AppShell>
)
```

Remove `page`, `main` style entries from each page's `styles` object.

- [ ] **Step 10: Delete `web/src/components/NavBar.tsx`**

```bash
rm web/src/components/NavBar.tsx
```

- [ ] **Step 11: Verify build compiles**

```bash
cd web && npm run build 2>&1 | tail -10
```

Expected: `built in X.XXs` with no TypeScript errors.

- [ ] **Step 12: Commit**

```bash
git add web/src/components/TopBar.tsx web/src/components/Sidebar.tsx web/src/components/AppShell.tsx web/src/test/Sidebar.test.tsx web/src/hooks/useAuth.ts web/src/api/ web/src/pages/
git rm web/src/components/NavBar.tsx
git commit -m "feat: replace NavBar with AppShell (TopBar + collapsible Sidebar)"
```

---

## Task 5: List/Grid Toggle for Index Pages

**Files:**
- Modify: `web/src/pages/HomePage.tsx`
- Modify: `web/src/pages/DashboardsPage.tsx`

- [ ] **Step 1: Update `HomePage` to support list/grid toggle**

Add state and toggle button. The key changes:

```tsx
const [layout, setLayout] = useState<'grid' | 'list'>(() =>
  (localStorage.getItem('hnb_notebooks_layout') as 'grid' | 'list') ?? 'list'
)
const toggleLayout = () => {
  const next = layout === 'list' ? 'grid' : 'list'
  setLayout(next)
  localStorage.setItem('hnb_notebooks_layout', next)
}
```

Add toggle button next to "+ New Notebook":
```tsx
<button type="button" style={styles.layoutBtn} onClick={toggleLayout} title={layout === 'list' ? 'Switch to grid' : 'Switch to list'}>
  {layout === 'list' ? '⊞' : '≡'}
</button>
```

Replace `<div style={styles.grid}>` with:
```tsx
<div style={layout === 'grid' ? styles.grid : styles.list}>
  {notebooks.map((nb) =>
    layout === 'grid'
      ? <NotebookCard key={nb.id} notebook={nb} onDelete={() => deleteNotebook.mutate(nb.id)} />
      : <NotebookRow key={nb.id} notebook={nb} onDelete={() => deleteNotebook.mutate(nb.id)} />
  )}
</div>
```

Add `NotebookRow` component (below `NotebookCard`):
```tsx
function NotebookRow({ notebook, onDelete }: { notebook: Notebook; onDelete: () => void }) {
  const updated = new Date(notebook.updated_at)
  const dateStr = updated.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })

  return (
    <div style={rowStyles.row}>
      <Link to={`/notebooks/${notebook.id}`} style={rowStyles.link}>
        <span style={rowStyles.icon}>▦</span>
        <div style={rowStyles.info}>
          <span style={rowStyles.title}>{notebook.title}</span>
          {notebook.description && <span style={rowStyles.desc}>{notebook.description}</span>}
        </div>
        <span style={rowStyles.date}>{dateStr}</span>
      </Link>
      <button type="button" style={rowStyles.del} onClick={(e) => { e.preventDefault(); onDelete() }}>Delete</button>
    </div>
  )
}

const rowStyles: Record<string, React.CSSProperties> = {
  row: { display: 'flex', alignItems: 'center', background: 'white', borderRadius: 8, border: '1px solid var(--border)', padding: '10px 16px', gap: 12 },
  link: { flex: 1, display: 'flex', alignItems: 'center', gap: 12, textDecoration: 'none' },
  icon: { fontSize: 18, color: 'var(--accent)', flexShrink: 0 },
  info: { flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  desc: { fontSize: 12, color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
  date: { fontSize: 12, color: 'var(--text-muted)', flexShrink: 0 },
  del: { padding: '3px 8px', border: 'none', background: 'transparent', color: 'var(--error)', fontSize: 12, cursor: 'pointer', flexShrink: 0 },
}
```

Add `layoutBtn` and `list` to `styles`:
```tsx
layoutBtn: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 6, background: 'none', cursor: 'pointer', fontSize: 14 },
list: { display: 'flex', flexDirection: 'column', gap: 8 },
```

- [ ] **Step 2: Apply the same pattern to `DashboardsPage.tsx`**

Same changes with key `'hnb_dashboards_layout'` and `DashboardRow` component instead of `NotebookRow`.

- [ ] **Step 3: Verify build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/HomePage.tsx web/src/pages/DashboardsPage.tsx
git commit -m "feat: list/grid layout toggle for notebooks and dashboards pages"
```

---

## Task 6: Collaboration Cursor Names

**Files:**
- Modify: `web/src/components/CodeCell.tsx`

- [ ] **Step 1: Pass user awareness to HocuspocusProvider**

In `CodeCell.tsx`, update `getOrCreateCollab` to set initial awareness after creating the provider:

```typescript
function getOrCreateCollab(notebookId: string): NotebookCollab {
  const existing = collabCache.get(notebookId)
  if (existing) {
    existing.refCount++
    return existing
  }

  const doc = new Y.Doc()
  const token = localStorage.getItem('hnb_token') ?? ''
  const userName = localStorage.getItem('hnb_user_name') ?? ''
  const userEmail = localStorage.getItem('hnb_user_email') ?? ''

  const provider = new HocuspocusProvider({
    url: RELAY_URL,
    name: notebookId,
    document: doc,
    token,
    onAuthenticationFailed: () => console.warn('[yjs] Relay auth failed — collaborative editing disabled'),
  })

  // Set user identity so remote cursors show the real name
  provider.awareness.setLocalStateField('user', {
    name: userName || userEmail || 'Anonymous',
    email: userEmail,
    color: `hsl(${Math.abs(hashStr(userEmail || userName)) % 360}, 70%, 55%)`,
  })

  const entry: NotebookCollab = { doc, provider, refCount: 1 }
  collabCache.set(notebookId, entry)
  return entry
}

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  return h
}
```

- [ ] **Step 2: Verify build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: no TypeScript errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/CodeCell.tsx
git commit -m "fix: show real user name on collaboration cursors via Hocuspocus awareness"
```

---

## Task 7: Output Type Icons

**Files:**
- Create: `web/src/test/OutputRenderer.test.tsx`
- Modify: `web/src/components/OutputRenderer.tsx`

- [ ] **Step 1: Write failing test for type icons**

Create `web/src/test/OutputRenderer.test.tsx`:
```tsx
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { OutputRenderer } from '../components/OutputRenderer'
import type { Output } from '../types'

const makeTableOutput = (colType: string): Output => ({
  type: 'table',
  data: {
    columns: [{ name: 'val', type: colType }],
    rows: [['test']],
  },
})

describe('OutputRenderer type icons', () => {
  it('shows # icon for integer type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('integer')]} />)
    expect(screen.getByTitle('Integer')).toBeDefined()
  })

  it('shows 0.1 icon for float type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('float')]} />)
    expect(screen.getByTitle('Float')).toBeDefined()
  })

  it('shows calendar icon for date type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('date')]} />)
    expect(screen.getByTitle('Date')).toBeDefined()
  })

  it('shows ? icon for unknown type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('super_weird_type')]} />)
    expect(screen.getByTitle('Unknown')).toBeDefined()
  })

  it('shows {} icon for jsonb type', () => {
    render(<OutputRenderer outputs={[makeTableOutput('jsonb')]} />)
    expect(screen.getByTitle('JSON')).toBeDefined()
  })
})
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd web && npm run test:run 2>&1 | grep -E "PASS|FAIL|OutputRenderer"
```

Expected: FAIL

- [ ] **Step 3: Update `web/src/components/OutputRenderer.tsx` — add type icon map and replace `colType` span**

Add the type map and `TypeIcon` component:
```tsx
const TYPE_MAP: Record<string, { icon: string; label: string }> = {
  string: { icon: 'Aa', label: 'String' },
  varchar: { icon: 'Aa', label: 'String' },
  text: { icon: 'Aa', label: 'String' },
  char: { icon: 'Aa', label: 'String' },
  integer: { icon: '#', label: 'Integer' },
  int: { icon: '#', label: 'Integer' },
  int2: { icon: '#', label: 'Integer' },
  int4: { icon: '#', label: 'Integer' },
  int8: { icon: '#', label: 'Integer' },
  bigint: { icon: '#', label: 'Integer' },
  smallint: { icon: '#', label: 'Integer' },
  float: { icon: '0.1', label: 'Float' },
  float4: { icon: '0.1', label: 'Float' },
  float8: { icon: '0.1', label: 'Float' },
  double: { icon: '0.1', label: 'Float' },
  decimal: { icon: '0.1', label: 'Float' },
  numeric: { icon: '0.1', label: 'Float' },
  real: { icon: '0.1', label: 'Float' },
  boolean: { icon: '⊙', label: 'Boolean' },
  bool: { icon: '⊙', label: 'Boolean' },
  date: { icon: '📅', label: 'Date' },
  datetime: { icon: '🕐', label: 'Datetime' },
  timestamp: { icon: '🕐', label: 'Datetime' },
  timestamptz: { icon: '🕐', label: 'Datetime' },
  'timestamp with time zone': { icon: '🕐', label: 'Datetime' },
  array: { icon: '[]', label: 'Array' },
  json: { icon: '{}', label: 'JSON' },
  jsonb: { icon: '{}', label: 'JSON' },
  uuid: { icon: '⌗', label: 'UUID' },
  null: { icon: '∅', label: 'Null' },
  bytes: { icon: '⬡', label: 'Bytes' },
  bytea: { icon: '⬡', label: 'Bytes' },
  unknown: { icon: '?', label: 'Unknown' },
}

function TypeIcon({ type }: { type: string }) {
  const normalized = type.toLowerCase()
  const info = TYPE_MAP[normalized] ?? { icon: '?', label: 'Unknown' }
  return (
    <span
      title={info.label}
      style={typeIconStyles.badge}
    >
      {info.icon}
    </span>
  )
}

const typeIconStyles: Record<string, React.CSSProperties> = {
  badge: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    color: 'var(--text-muted)',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border-light)',
    borderRadius: 4,
    padding: '1px 5px',
    marginLeft: 6,
    cursor: 'default',
    userSelect: 'none',
  },
}
```

In `TableOutput`, replace:
```tsx
<span style={styles.colType}>{col.type}</span>
```
with:
```tsx
<TypeIcon type={col.type} />
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd web && npm run test:run 2>&1 | grep -E "PASS|FAIL|OutputRenderer"
```

Expected: `PASS`

- [ ] **Step 5: Commit**

```bash
git add web/src/components/OutputRenderer.tsx web/src/test/OutputRenderer.test.tsx
git commit -m "feat: replace raw type strings with icons+tooltips in output table headers"
```

---

## Task 8: Notebook Header with Description

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

- [ ] **Step 1: Add description editing to NotebookPage**

In `NotebookPage.tsx`, add description draft state alongside the existing title draft:
```tsx
const [descDraft, setDescDraft] = useState('')

useEffect(() => {
  if (notebook) {
    setTitleDraft(notebook.title)
    setDescDraft(notebook.description ?? '')
  }
}, [notebook])
```

Below the existing title editing UI, add a description field:
```tsx
{/* Description */}
<div style={styles.notebookDesc}>
  <input
    style={styles.descInput}
    value={descDraft}
    onChange={(e) => setDescDraft(e.target.value)}
    onBlur={() => {
      if (descDraft !== (notebook?.description ?? '')) {
        updateNotebook.mutate({ description: descDraft })
      }
    }}
    placeholder="Add a description…"
  />
</div>
```

Update the `updateNotebook` mutation body to include `description` when set:
```tsx
mutationFn: (data: { title?: string; description?: string }) =>
  api.put<Notebook>(`/api/v1/notebooks/${id}`, data),
```

Add styles:
```tsx
notebookDesc: { marginBottom: 8 },
descInput: {
  width: '100%',
  border: 'none',
  outline: 'none',
  fontSize: 14,
  color: 'var(--text-secondary)',
  background: 'transparent',
  fontFamily: 'var(--font-sans)',
  padding: '2px 0',
},
```

- [ ] **Step 2: Verify build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: editable description field in notebook header"
```

---

## Task 9: Playwright Navigation Spec + Visual Snapshots

**Files:**
- Create: `e2e/navigation.spec.ts`

- [ ] **Step 1: Create `e2e/navigation.spec.ts`**

```typescript
import { test, expect } from '@playwright/test'

// Helper: register + login to get a session
async function loginAsNewUser(page: import('@playwright/test').Page) {
  const ts = Date.now()
  await page.goto('/login')
  await page.getByRole('link', { name: /register/i }).click()
  await page.getByPlaceholder(/email/i).fill(`nav-test-${ts}@example.com`)
  await page.getByPlaceholder(/password/i).fill('testpass123')
  await page.getByPlaceholder(/name/i).fill('Nav Tester')
  await page.getByPlaceholder(/org/i).fill(`NavOrg-${ts}`)
  await page.getByRole('button', { name: /register/i }).click()
  await page.waitForURL('/')
}

test.describe('Navigation', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsNewUser(page)
  })

  test('sidebar renders and collapses', async ({ page }) => {
    await expect(page.getByTitle('Notebooks')).toBeVisible()
    await expect(page.getByTitle('Dashboards')).toBeVisible()
    await expect(page.getByTitle('Connectors')).toBeVisible()
    await expect(page.getByTitle('Members')).toBeVisible()
    await expect(page.getByTitle('Audit')).toBeVisible()

    // Collapse
    await page.getByTitle('Expand sidebar').click()
    await expect(page.getByTitle('Collapse sidebar')).toBeVisible()

    // Visual snapshot — collapsed
    await expect(page).toHaveScreenshot('sidebar-expanded.png', { maxDiffPixelRatio: 0.01 })

    // Collapse back
    await page.getByTitle('Collapse sidebar').click()
    await expect(page).toHaveScreenshot('sidebar-collapsed.png', { maxDiffPixelRatio: 0.01 })
  })

  test('navigates to Dashboards page', async ({ page }) => {
    await page.getByTitle('Dashboards').click()
    await expect(page).toHaveURL('/dashboards')
  })

  test('profile dropdown shows name and sign-out', async ({ page }) => {
    await page.getByLabel('Profile menu').click()
    await expect(page.getByText('Nav Tester')).toBeVisible()
    await expect(page.getByRole('button', { name: /sign out/i })).toBeVisible()
  })

  test('sign out redirects to login', async ({ page }) => {
    await page.getByLabel('Profile menu').click()
    await page.getByRole('button', { name: /sign out/i }).click()
    await expect(page).toHaveURL('/login')
  })
})
```

- [ ] **Step 2: Commit**

```bash
git add e2e/navigation.spec.ts
git commit -m "test: Playwright navigation spec with visual snapshots"
```

---

## Phase 1 Complete

Run the full test suite to verify:

```bash
task test        # Go tests
task test:web    # Vitest component tests
```

Expected: all tests green. Then run the per-phase visual validation checklist from the spec before merging.
