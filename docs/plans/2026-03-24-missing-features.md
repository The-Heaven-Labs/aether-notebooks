# Missing Features Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement all features described in the design doc that are missing or incomplete in the frontend and backend.

**Architecture:** The backend is a Go monolith (`internal/api/`). The frontend is React + Vite + TanStack Query. Features are grouped by dependency order — backend gaps first, then frontend pages that wire to them. Real-time collaboration and OIDC are last as they are the most complex.

**Tech Stack:** Go 1.22, pgx v5, React 18, TypeScript, TanStack Query, React Router v6, CodeMirror 6, ECharts, react-markdown, Yjs/Hocuspocus

---

## Group 1: Backend API Gaps

These APIs are used by multiple frontend features below. Build them first.

---

### Task 1: Notebook update endpoint (rename + parameters)

**Files:**
- Modify: `internal/api/notebook_handlers.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/notebook_handlers_test.go` (create if not exists)

**Step 1: Add route to router.go**

In `internal/api/router.go`, after the existing notebook routes, add:
```go
s.mux.Handle("PUT /api/v1/notebooks/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateNotebook))))
```

**Step 2: Add handler to notebook_handlers.go**

```go
type updateNotebookRequest struct {
	Title       *string          `json:"title,omitempty"`
	Description *string          `json:"description,omitempty"`
	Parameters  []models.Parameter `json:"parameters,omitempty"`
}

func (s *Server) handleUpdateNotebook(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	var req updateNotebookRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	query := "UPDATE notebooks SET updated_at = NOW()"
	args := []interface{}{}
	argN := 1

	if req.Title != nil {
		query += fmt.Sprintf(", title = $%d", argN)
		args = append(args, *req.Title)
		argN++
	}
	if req.Parameters != nil {
		paramsJSON, _ := json.Marshal(req.Parameters)
		query += fmt.Sprintf(", parameters = $%d", argN)
		args = append(args, paramsJSON)
		argN++
	}

	query += fmt.Sprintf(" WHERE id = $%d AND org_id = $%d", argN, argN+1)
	args = append(args, nbID, claims.OrgID)
	query += " RETURNING id, org_id, title, parameters, created_by, created_at, updated_at"

	var nb models.Notebook
	var paramsOut []byte
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&nb.ID, &nb.OrgID, &nb.Title, &paramsOut, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	json.Unmarshal(paramsOut, &nb.Parameters)
	writeJSON(w, http.StatusOK, nb)
}
```

**Step 3: Build and verify**
```bash
cd /home/jesus/Projects/hnb-claude && go build ./...
```
Expected: no errors

**Step 4: Commit**
```bash
git add internal/api/notebook_handlers.go internal/api/router.go
git commit -m "feat: add PUT /api/v1/notebooks/{id} for rename and parameter updates"
```

---

### Task 2: Member management API

**Files:**
- Create: `internal/api/member_handlers.go`
- Modify: `internal/api/router.go`

**Step 1: Add routes to router.go**
```go
s.mux.Handle("GET /api/v1/members", authMW(http.HandlerFunc(s.handleListMembers)))
s.mux.Handle("POST /api/v1/members", authMW(RequireRole("admin")(http.HandlerFunc(s.handleInviteMember))))
s.mux.Handle("PUT /api/v1/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateMemberRole))))
s.mux.Handle("DELETE /api/v1/members/{user_id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleRemoveMember))))
```

**Step 2: Create member_handlers.go**
```go
package api

import (
	"encoding/json"
	"net/http"
)

type memberResponse struct {
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	JoinedAt  string `json:"joined_at"`
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name, om.role, om.created_at
		 FROM org_members om
		 JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1
		 ORDER BY om.created_at ASC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var members []memberResponse
	for rows.Next() {
		var m memberResponse
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		members = append(members, m)
	}
	if members == nil {
		members = []memberResponse{}
	}
	writeJSON(w, http.StatusOK, members)
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	targetUserID := r.PathValue("user_id")

	var req updateRoleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	validRoles := map[string]bool{"admin": true, "editor": true, "viewer": true}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "role must be admin, editor, or viewer")
		return
	}
	if targetUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot change your own role")
		return
	}

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE org_members SET role = $1 WHERE org_id = $2 AND user_id = $3`,
		req.Role, claims.OrgID, targetUserID,
	)
	if err != nil || result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	targetUserID := r.PathValue("user_id")

	if targetUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot remove yourself")
		return
	}

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		claims.OrgID, targetUserID,
	)
	if err != nil || result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInviteMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	validRoles := map[string]bool{"admin": true, "editor": true, "viewer": true}
	if !validRoles[req.Role] {
		req.Role = "viewer"
	}

	ctx := r.Context()
	// Find user by email
	var userID string
	err := s.db.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, req.Email).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found — they must register first")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		claims.OrgID, userID, req.Role,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	_ = json.NewEncoder(w) // silence import
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 3: Build**
```bash
go build ./...
```

**Step 4: Commit**
```bash
git add internal/api/member_handlers.go internal/api/router.go
git commit -m "feat: member management API (list, invite, update role, remove)"
```

---

### Task 3: Connector test + schema endpoints

**Files:**
- Modify: `internal/api/connector_handlers.go`
- Modify: `internal/api/router.go`
- Modify: `internal/executor/postgres.go`
- Modify: `internal/executor/clickhouse.go`

**Step 1: Add routes**
```go
s.mux.Handle("POST /api/v1/connectors/{id}/test", authMW(http.HandlerFunc(s.handleTestConnector)))
s.mux.Handle("GET /api/v1/connectors/{id}/schema", authMW(http.HandlerFunc(s.handleConnectorSchema)))
```

**Step 2: Add TestConnection to executors**

In `internal/executor/executor.go`, add to the Executor interface:
```go
type Executor interface {
    Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error)
    TestConnection(ctx context.Context) error
    Schema(ctx context.Context) ([]SchemaTable, error)
}

type SchemaTable struct {
    Schema  string         `json:"schema"`
    Name    string         `json:"name"`
    Columns []SchemaColumn `json:"columns"`
}

type SchemaColumn struct {
    Name     string `json:"name"`
    DataType string `json:"data_type"`
}
```

In `internal/executor/postgres.go`:
```go
func (p *PostgresExecutor) TestConnection(ctx context.Context) error {
    return p.pool.Ping(ctx)
}

func (p *PostgresExecutor) Schema(ctx context.Context) ([]SchemaTable, error) {
    rows, err := p.pool.Query(ctx, `
        SELECT table_schema, table_name, column_name, data_type
        FROM information_schema.columns
        WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
        ORDER BY table_schema, table_name, ordinal_position
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    tableMap := map[string]*SchemaTable{}
    var order []string
    for rows.Next() {
        var schema, table, col, dtype string
        rows.Scan(&schema, &table, &col, &dtype)
        key := schema + "." + table
        if _, ok := tableMap[key]; !ok {
            tableMap[key] = &SchemaTable{Schema: schema, Name: table}
            order = append(order, key)
        }
        tableMap[key].Columns = append(tableMap[key].Columns, SchemaColumn{Name: col, DataType: dtype})
    }

    result := make([]SchemaTable, 0, len(order))
    for _, k := range order {
        result = append(result, *tableMap[k])
    }
    return result, nil
}
```

**Step 3: Add handlers to connector_handlers.go**
```go
func (s *Server) handleTestConnector(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    connID := r.PathValue("id")
    ctx := r.Context()

    cfg, err := s.loadConnectorConfig(ctx, connID, claims.OrgID)
    if err != nil {
        writeError(w, http.StatusNotFound, "connector not found")
        return
    }

    exec, err := executor.Build(cfg)
    if err != nil {
        writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
        return
    }

    if err := exec.TestConnection(ctx); err != nil {
        writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleConnectorSchema(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    connID := r.PathValue("id")
    ctx := r.Context()

    cfg, err := s.loadConnectorConfig(ctx, connID, claims.OrgID)
    if err != nil {
        writeError(w, http.StatusNotFound, "connector not found")
        return
    }

    exec, err := executor.Build(cfg)
    if err != nil {
        writeError(w, http.StatusBadGateway, "cannot connect: "+err.Error())
        return
    }

    tables, err := exec.Schema(ctx)
    if err != nil {
        writeError(w, http.StatusBadGateway, "schema fetch failed: "+err.Error())
        return
    }
    writeJSON(w, http.StatusOK, tables)
}
```

Add helper `loadConnectorConfig` to connector_handlers.go (extracts the decrypt+build logic shared with execute_handlers):
```go
func (s *Server) loadConnectorConfig(ctx context.Context, connID, orgID string) (models.ConnectorConfig, error) {
    var configEnc []byte
    var connType string
    err := s.db.Pool.QueryRow(ctx,
        `SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
        connID, orgID,
    ).Scan(&connType, &configEnc)
    if err != nil {
        return models.ConnectorConfig{}, err
    }
    plain, err := crypto.Decrypt(s.masterKey, configEnc)
    if err != nil {
        return models.ConnectorConfig{}, err
    }
    var cfg models.ConnectorConfig
    json.Unmarshal(plain, &cfg)
    cfg.Type = connType
    return cfg, nil
}
```

**Step 4: Build**
```bash
go build ./...
```

**Step 5: Commit**
```bash
git add internal/api/connector_handlers.go internal/api/router.go internal/executor/
git commit -m "feat: connector test + schema endpoints"
```

---

### Task 4: Schedule enable/disable + update endpoint

**Files:**
- Modify: `internal/api/schedule_handlers.go`
- Modify: `internal/api/router.go`

**Step 1: Add route**
```go
s.mux.Handle("PUT /api/v1/schedules/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUpdateSchedule))))
```

**Step 2: Add handler**
```go
type updateScheduleRequest struct {
	Enabled            *bool             `json:"enabled,omitempty"`
	CronExpression     *string           `json:"cron_expression,omitempty"`
	ParameterOverrides map[string]string `json:"parameter_overrides,omitempty"`
}

func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	schedID := r.PathValue("id")

	var req updateScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	query := "UPDATE schedules SET updated_at = NOW()"
	args := []interface{}{}
	argN := 1

	if req.Enabled != nil {
		query += fmt.Sprintf(", enabled = $%d", argN)
		args = append(args, *req.Enabled)
		argN++
	}
	if req.CronExpression != nil {
		next, err := scheduler.NextRun(*req.CronExpression)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cron expression")
			return
		}
		query += fmt.Sprintf(", cron_expression = $%d, next_run_at = $%d", argN, argN+1)
		args = append(args, *req.CronExpression, next)
		argN += 2
	}
	if req.ParameterOverrides != nil {
		overridesJSON, _ := json.Marshal(req.ParameterOverrides)
		query += fmt.Sprintf(", parameter_overrides = $%d", argN)
		args = append(args, overridesJSON)
		argN++
	}

	query += fmt.Sprintf(` WHERE id = $%d AND notebook_id IN (SELECT id FROM notebooks WHERE org_id = $%d)`, argN, argN+1)
	args = append(args, schedID, claims.OrgID)
	query += " RETURNING id, notebook_id, cron_expression, parameter_overrides, enabled, last_run_at, next_run_at, created_at, updated_at"

	var sched models.Schedule
	var overridesOut []byte
	err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
		&sched.ID, &sched.NotebookID, &sched.CronExpression, &overridesOut,
		&sched.Enabled, &sched.LastRunAt, &sched.NextRunAt, &sched.CreatedAt, &sched.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	json.Unmarshal(overridesOut, &sched.ParameterOverrides)
	writeJSON(w, http.StatusOK, sched)
}
```

**Step 3: Build and commit**
```bash
go build ./...
git add internal/api/schedule_handlers.go internal/api/router.go
git commit -m "feat: PUT /api/v1/schedules/{id} for enable/disable and cron updates"
```

---

### Task 5: Audit log API endpoint

**Files:**
- Create: `internal/api/audit_handlers.go`
- Modify: `internal/api/router.go`

**Step 1: Add route (admin only)**
```go
s.mux.Handle("GET /api/v1/audit", authMW(RequireRole("admin")(http.HandlerFunc(s.handleListAuditLogs))))
```

**Step 2: Create audit_handlers.go**
```go
package api

import (
	"net/http"
	"strconv"
)

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	q := r.URL.Query()

	limit := 100
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	args := []interface{}{claims.OrgID, limit, offset}
	filter := ""
	argN := 4

	if action := q.Get("action"); action != "" {
		filter += fmt.Sprintf(" AND action = $%d", argN)
		args = append(args, action)
		argN++
	}
	if userID := q.Get("user_id"); userID != "" {
		filter += fmt.Sprintf(" AND user_id = $%d", argN)
		args = append(args, userID)
		argN++
	}
	if resourceType := q.Get("resource_type"); resourceType != "" {
		filter += fmt.Sprintf(" AND resource_type = $%d", argN)
		args = append(args, resourceType)
		argN++
	}

	rows, err := s.db.Pool.Query(ctx,
		fmt.Sprintf(`SELECT id, org_id, user_id, action, resource_type, resource_id, created_at
		 FROM audit_logs WHERE org_id = $1`+filter+`
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type entry struct {
		ID           string `json:"id"`
		OrgID        string `json:"org_id"`
		UserID       string `json:"user_id"`
		Action       string `json:"action"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		CreatedAt    string `json:"created_at"`
	}
	var entries []entry
	for rows.Next() {
		var e entry
		rows.Scan(&e.ID, &e.OrgID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID, &e.CreatedAt)
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []entry{}
	}
	writeJSON(w, http.StatusOK, entries)
}
```

**Step 3: Build and commit**
```bash
go build ./...
git add internal/api/audit_handlers.go internal/api/router.go
git commit -m "feat: GET /api/v1/audit for admin audit log browsing"
```

---

## Group 2: Frontend — Core Notebook UX

---

### Task 6: Notebook inline rename

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Add rename state and handler**

In `NotebookPage.tsx`, add state for editing the title and a `renameNotebook` mutation:

```tsx
const [editingTitle, setEditingTitle] = useState(false)
const [titleDraft, setTitleDraft] = useState('')

const renameNotebook = useMutation({
  mutationFn: (title: string) =>
    api.put(`/api/v1/notebooks/${id}`, { title }),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['notebook', id] }),
})
```

**Step 2: Replace the static title in the header**

Replace:
```tsx
<span style={styles.notebookTitle}>{notebook.title}</span>
```
With:
```tsx
{editingTitle ? (
  <input
    style={styles.titleInput}
    value={titleDraft}
    onChange={(e) => setTitleDraft(e.target.value)}
    onBlur={() => {
      setEditingTitle(false)
      if (titleDraft.trim() && titleDraft !== notebook.title) {
        renameNotebook.mutate(titleDraft.trim())
      }
    }}
    onKeyDown={(e) => {
      if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
      if (e.key === 'Escape') { setEditingTitle(false) }
    }}
    autoFocus
  />
) : (
  <span
    style={styles.notebookTitle}
    onClick={() => { setTitleDraft(notebook.title); setEditingTitle(true) }}
    title="Click to rename"
  >
    {notebook.title}
  </span>
)}
```

**Step 3: Add style**
```tsx
titleInput: {
  fontSize: 14,
  fontWeight: 600,
  color: 'var(--nav-text)',
  background: 'transparent',
  border: 'none',
  borderBottom: '1px solid var(--accent)',
  outline: 'none',
  maxWidth: 400,
  fontFamily: 'var(--font-sans)',
},
```

**Step 4: Commit**
```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: inline notebook title rename"
```

---

### Task 7: Notebook parameters UI

**Files:**
- Create: `web/src/components/ParametersBar.tsx`
- Modify: `web/src/pages/NotebookPage.tsx`
- Modify: `web/src/types/index.ts`

**Step 1: Add Parameter type to types/index.ts**
```ts
export interface Parameter {
  name: string
  type: 'string' | 'number' | 'date' | 'daterange'
  default: string
}
```
Add `parameters?: Parameter[]` to the `Notebook` interface.

**Step 2: Create ParametersBar.tsx**

This component renders current parameter values and lets users edit them at run-time. It also has a "Manage" toggle to add/remove/edit parameter definitions.

```tsx
import { useState } from 'react'
import type { Parameter } from '../types'

interface Props {
  parameters: Parameter[]
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
  onSaveDefinitions: (params: Parameter[]) => void
  isAdmin: boolean
}

export function ParametersBar({ parameters, values, onChange, onSaveDefinitions, isAdmin }: Props) {
  const [managing, setManaging] = useState(false)
  const [draftParams, setDraftParams] = useState<Parameter[]>(parameters)

  if (parameters.length === 0 && !isAdmin) return null

  return (
    <div style={styles.bar}>
      <div style={styles.paramsList}>
        {parameters.map((p) => (
          <label key={p.name} style={styles.paramField}>
            <span style={styles.paramName}>{p.name}</span>
            <input
              style={styles.paramInput}
              value={values[p.name] ?? p.default}
              onChange={(e) => onChange({ ...values, [p.name]: e.target.value })}
              placeholder={p.default}
            />
          </label>
        ))}
        {parameters.length === 0 && (
          <span style={styles.noParams}>No parameters defined</span>
        )}
      </div>
      {isAdmin && (
        <button style={styles.manageBtn} onClick={() => setManaging(!managing)}>
          {managing ? 'Done' : '⚙ Parameters'}
        </button>
      )}
      {managing && (
        <div style={styles.managPanel}>
          {draftParams.map((p, i) => (
            <div key={i} style={styles.draftRow}>
              <input
                style={styles.draftInput}
                placeholder="name"
                value={p.name}
                onChange={(e) => {
                  const next = [...draftParams]
                  next[i] = { ...next[i], name: e.target.value }
                  setDraftParams(next)
                }}
              />
              <select
                style={styles.draftInput}
                value={p.type}
                onChange={(e) => {
                  const next = [...draftParams]
                  next[i] = { ...next[i], type: e.target.value as Parameter['type'] }
                  setDraftParams(next)
                }}
              >
                <option value="string">string</option>
                <option value="number">number</option>
                <option value="date">date</option>
                <option value="daterange">daterange</option>
              </select>
              <input
                style={styles.draftInput}
                placeholder="default"
                value={p.default}
                onChange={(e) => {
                  const next = [...draftParams]
                  next[i] = { ...next[i], default: e.target.value }
                  setDraftParams(next)
                }}
              />
              <button onClick={() => setDraftParams(draftParams.filter((_, j) => j !== i))}>✕</button>
            </div>
          ))}
          <button onClick={() => setDraftParams([...draftParams, { name: '', type: 'string', default: '' }])}>
            + Add parameter
          </button>
          <button style={styles.saveBtn} onClick={() => { onSaveDefinitions(draftParams); setManaging(false) }}>
            Save
          </button>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  bar: { background: 'var(--bg-secondary)', borderBottom: '1px solid var(--border-light)', padding: '8px 40px', display: 'flex', alignItems: 'flex-start', gap: 12, flexWrap: 'wrap' },
  paramsList: { display: 'flex', gap: 12, flexWrap: 'wrap', alignItems: 'center', flex: 1 },
  paramField: { display: 'flex', alignItems: 'center', gap: 6, fontSize: 12 },
  paramName: { fontFamily: 'var(--font-mono)', fontWeight: 600, color: 'var(--text-secondary)' },
  paramInput: { padding: '3px 8px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, fontFamily: 'var(--font-mono)', background: 'white', width: 120 },
  noParams: { fontSize: 12, color: 'var(--text-muted)', fontStyle: 'italic' },
  manageBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, background: 'var(--border)', border: '1px solid transparent', borderRadius: 4, cursor: 'pointer', color: 'var(--text-secondary)', whiteSpace: 'nowrap' },
  managPanel: { width: '100%', marginTop: 8, display: 'flex', flexDirection: 'column', gap: 8 },
  draftRow: { display: 'flex', gap: 8, alignItems: 'center' },
  draftInput: { padding: '4px 8px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, fontFamily: 'var(--font-mono)' },
  saveBtn: { alignSelf: 'flex-start', padding: '5px 14px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 5, fontSize: 12, fontWeight: 600, cursor: 'pointer' },
}
```

**Step 3: Wire into NotebookPage.tsx**

Add state for run-time parameter values:
```tsx
const [paramValues, setParamValues] = useState<Record<string, string>>({})
```

Pass params when running:
```tsx
const result = await api.post<{ outputs: Output[] }>(
  `/api/v1/notebooks/${id}/cells/${cellId}/execute`,
  { parameters: paramValues },
)
```

Add `saveParameters` mutation:
```tsx
const saveParameters = useMutation({
  mutationFn: (params: Parameter[]) =>
    api.put(`/api/v1/notebooks/${id}`, { parameters: params }),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['notebook', id] }),
})
```

Render `ParametersBar` between the header and body:
```tsx
<ParametersBar
  parameters={notebook.parameters ?? []}
  values={paramValues}
  onChange={setParamValues}
  onSaveDefinitions={(params) => saveParameters.mutate(params)}
  isAdmin={true} // wire from auth claims later
/>
```

**Step 4: Commit**
```bash
git add web/src/components/ParametersBar.tsx web/src/pages/NotebookPage.tsx web/src/types/index.ts
git commit -m "feat: notebook parameters bar — define params and pass at run-time"
```

---

## Group 3: Frontend — Connector Management Page

---

### Task 8: Connector management admin page

**Files:**
- Create: `web/src/pages/ConnectorsPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/HomePage.tsx` (add nav link)

**Step 1: Create ConnectorsPage.tsx**

This page lists all connectors, allows admins to create new ones (Postgres/ClickHouse), test them, and delete them.

```tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'

type ConnectorType = 'postgres' | 'clickhouse'

interface ConnectorForm {
  name: string
  type: ConnectorType
  host: string
  port: string
  database: string
  user: string
  password: string
  ssl_mode: string
}

const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable',
})

export function ConnectorsPage() {
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState<ConnectorForm>(defaultForm())
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; error?: string }>>({})

  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const createConnector = useMutation({
    mutationFn: () => api.post<Connector>('/api/v1/connectors', {
      name: form.name,
      type: form.type,
      config: {
        host: form.host,
        port: parseInt(form.port),
        database: form.database,
        user: form.user,
        password: form.password,
        ssl_mode: form.ssl_mode,
      },
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      setCreating(false)
      setForm(defaultForm())
    },
  })

  const deleteConnector = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/connectors/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['connectors'] }),
  })

  const testConnector = async (id: string) => {
    const result = await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
    setTestResults((prev) => ({ ...prev, [id]: result }))
  }

  return (
    <div style={styles.page}>
      <header style={styles.header}>
        <div style={styles.headerLeft}>
          <Link to="/" style={styles.backLink}>← Home</Link>
          <span style={styles.sep}>/</span>
          <span style={styles.title}>Connectors</span>
        </div>
        <button style={styles.addBtn} onClick={() => setCreating(true)}>+ New Connector</button>
      </header>

      <div style={styles.body}>
        {creating && (
          <div style={styles.formCard}>
            <h3 style={styles.formTitle}>New Connector</h3>
            <div style={styles.formGrid}>
              {/* Name */}
              <label style={styles.label}>Name
                <input style={styles.input} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="My Postgres" />
              </label>
              {/* Type */}
              <label style={styles.label}>Type
                <select style={styles.input} value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value as ConnectorType, port: e.target.value === 'clickhouse' ? '9000' : '5432' })}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                </select>
              </label>
              {/* Host */}
              <label style={styles.label}>Host
                <input style={styles.input} value={form.host} onChange={(e) => setForm({ ...form, host: e.target.value })} />
              </label>
              {/* Port */}
              <label style={styles.label}>Port
                <input style={styles.input} value={form.port} onChange={(e) => setForm({ ...form, port: e.target.value })} />
              </label>
              {/* Database */}
              <label style={styles.label}>Database
                <input style={styles.input} value={form.database} onChange={(e) => setForm({ ...form, database: e.target.value })} />
              </label>
              {/* User */}
              <label style={styles.label}>User
                <input style={styles.input} value={form.user} onChange={(e) => setForm({ ...form, user: e.target.value })} />
              </label>
              {/* Password */}
              <label style={styles.label}>Password
                <input style={styles.input} type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
              </label>
              {/* SSL */}
              <label style={styles.label}>SSL Mode
                <select style={styles.input} value={form.ssl_mode} onChange={(e) => setForm({ ...form, ssl_mode: e.target.value })}>
                  <option value="disable">disable</option>
                  <option value="require">require</option>
                  <option value="verify-full">verify-full</option>
                </select>
              </label>
            </div>
            <div style={styles.formActions}>
              <button style={styles.cancelBtn} onClick={() => setCreating(false)}>Cancel</button>
              <button style={styles.saveBtn} onClick={() => createConnector.mutate()} disabled={!form.name || !form.host || !form.database}>
                Create
              </button>
            </div>
          </div>
        )}

        <table style={styles.table}>
          <thead>
            <tr>
              {['Name', 'Type', 'Host', 'Database', 'Status', ''].map((h) => (
                <th key={h} style={styles.th}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {connectors.map((c) => {
              const test = testResults[c.id]
              return (
                <tr key={c.id} style={styles.tr}>
                  <td style={styles.td}><strong>{c.name}</strong></td>
                  <td style={styles.td}><code style={styles.badge}>{c.type}</code></td>
                  <td style={styles.td}>{(c as any).config?.host ?? '—'}</td>
                  <td style={styles.td}>{(c as any).config?.database ?? '—'}</td>
                  <td style={styles.td}>
                    {test ? (
                      <span style={{ color: test.ok ? 'green' : 'red', fontSize: 12 }}>
                        {test.ok ? '✓ Connected' : `✗ ${test.error}`}
                      </span>
                    ) : '—'}
                  </td>
                  <td style={styles.tdActions}>
                    <button style={styles.actionBtn} onClick={() => testConnector(c.id)}>Test</button>
                    <button style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${c.name}"?`)) deleteConnector.mutate(c.id) }}>Delete</button>
                  </td>
                </tr>
              )
            })}
            {connectors.length === 0 && (
              <tr><td colSpan={6} style={{ ...styles.td, textAlign: 'center', color: 'var(--text-muted)', padding: 32 }}>No connectors yet</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: { minHeight: '100vh', background: 'var(--bg-primary)', display: 'flex', flexDirection: 'column' },
  header: { background: 'var(--nav-bg)', borderBottom: '1px solid var(--nav-border)', height: 52, display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0 32px', position: 'sticky', top: 0, zIndex: 100 },
  headerLeft: { display: 'flex', alignItems: 'center', gap: 10 },
  backLink: { color: '#6a6260', textDecoration: 'none', fontSize: 13, fontWeight: 500 },
  sep: { color: '#3a3630', fontSize: 14 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--nav-text)' },
  addBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  formCard: { background: 'white', border: '1px solid var(--border)', borderRadius: 10, padding: 24, marginBottom: 24, boxShadow: 'var(--shadow-sm)' },
  formTitle: { margin: '0 0 16px', fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' },
  formGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'white' },
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end' },
  cancelBtn: { padding: '6px 16px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 5, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  table: { width: '100%', borderCollapse: 'collapse', background: 'white', borderRadius: 10, overflow: 'hidden', border: '1px solid var(--border)', boxShadow: 'var(--shadow-sm)' },
  th: { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', letterSpacing: '0.06em', borderBottom: '1px solid var(--border-light)', background: 'var(--bg-secondary)' },
  tr: { borderBottom: '1px solid var(--border-light)' },
  td: { padding: '12px 16px', fontSize: 13, color: 'var(--text-primary)' },
  tdActions: { padding: '8px 16px', display: 'flex', gap: 6, justifyContent: 'flex-end' },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--bg-secondary)', padding: '2px 6px', borderRadius: 3 },
  actionBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'transparent', cursor: 'pointer', color: 'var(--text-secondary)' },
  deleteBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid transparent', borderRadius: 4, background: 'transparent', cursor: 'pointer', color: '#c0392b' },
}
```

**Step 2: Register route in App.tsx**

Add import and route:
```tsx
import { ConnectorsPage } from './pages/ConnectorsPage'
// In AppRoutes:
<Route path="/connectors" element={<ProtectedRoute><ConnectorsPage /></ProtectedRoute>} />
```

**Step 3: Add nav link in HomePage.tsx header**

Add a "Connectors" link next to the sign-out button (visible to admins — for now show to all, role-gating is Task 13).

**Step 4: Commit**
```bash
git add web/src/pages/ConnectorsPage.tsx web/src/App.tsx web/src/pages/HomePage.tsx
git commit -m "feat: connector management page"
```

---

### Task 9: Schema browser sidebar in NotebookPage

**Files:**
- Create: `web/src/components/SchemaBrowser.tsx`
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Create SchemaBrowser.tsx**

```tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

interface SchemaColumn { name: string; data_type: string }
interface SchemaTable { schema: string; name: string; columns: SchemaColumn[] }

interface Props {
  connectorId: string | null
  onInsert: (text: string) => void
}

export function SchemaBrowser({ connectorId, onInsert }: Props) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const { data: tables = [], isLoading } = useQuery({
    queryKey: ['schema', connectorId],
    queryFn: () => api.get<SchemaTable[]>(`/api/v1/connectors/${connectorId}/schema`),
    enabled: !!connectorId,
    staleTime: 5 * 60 * 1000,
  })

  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      next.has(key) ? next.delete(key) : next.add(key)
      return next
    })

  if (!connectorId) return (
    <div style={styles.empty}>Select a connector to browse schema</div>
  )

  if (isLoading) return <div style={styles.empty}>Loading schema…</div>

  const grouped = tables.reduce<Record<string, SchemaTable[]>>((acc, t) => {
    if (!acc[t.schema]) acc[t.schema] = []
    acc[t.schema].push(t)
    return acc
  }, {})

  return (
    <div style={styles.browser}>
      {Object.entries(grouped).map(([schema, schemaTables]) => (
        <div key={schema}>
          <div style={styles.schemaHeader}>{schema}</div>
          {schemaTables.map((t) => {
            const key = `${t.schema}.${t.name}`
            const open = expanded.has(key)
            return (
              <div key={key}>
                <div style={styles.tableRow} onClick={() => toggle(key)}>
                  <span style={styles.tableToggle}>{open ? '▾' : '▸'}</span>
                  <span
                    style={styles.tableName}
                    onDoubleClick={() => onInsert(t.schema === 'public' ? t.name : `${t.schema}.${t.name}`)}
                    title="Double-click to insert"
                  >
                    {t.name}
                  </span>
                </div>
                {open && t.columns.map((c) => (
                  <div
                    key={c.name}
                    style={styles.colRow}
                    onDoubleClick={() => onInsert(c.name)}
                    title={`${c.data_type} — double-click to insert`}
                  >
                    <span style={styles.colName}>{c.name}</span>
                    <span style={styles.colType}>{c.data_type}</span>
                  </div>
                ))}
              </div>
            )
          })}
        </div>
      ))}
      {tables.length === 0 && <div style={styles.empty}>No tables found</div>}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  browser: { fontSize: 12, fontFamily: 'var(--font-mono)', overflowY: 'auto', height: '100%' },
  empty: { padding: 16, color: 'var(--text-muted)', fontSize: 12, fontStyle: 'italic' },
  schemaHeader: { padding: '8px 12px 4px', fontSize: 10, fontWeight: 700, letterSpacing: '0.08em', color: 'var(--text-muted)', textTransform: 'uppercase' },
  tableRow: { display: 'flex', alignItems: 'center', gap: 4, padding: '3px 12px', cursor: 'pointer', userSelect: 'none' },
  tableToggle: { fontSize: 10, color: 'var(--text-muted)', width: 10 },
  tableName: { color: 'var(--text-primary)', fontWeight: 500 },
  colRow: { display: 'flex', justifyContent: 'space-between', padding: '2px 12px 2px 28px', cursor: 'pointer' },
  colName: { color: 'var(--text-secondary)' },
  colType: { color: 'var(--text-muted)', fontSize: 10 },
}
```

**Step 2: Add sidebar layout to NotebookPage.tsx**

Add state `const [schemaOpen, setSchemaOpen] = useState(false)` and a toggle button in the header.

Wrap the body in a two-column layout when schema is open:
```tsx
<div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
  <div style={{ flex: 1, overflow: 'auto', padding: '32px 0 64px' }}>
    <div style={styles.bodyInner}>
      {/* cells */}
    </div>
  </div>
  {schemaOpen && (
    <aside style={styles.sidebar}>
      <SchemaBrowser
        connectorId={localCells.find(c => c.type === 'code')?.connector_id ?? null}
        onInsert={(text) => { /* future: insert at cursor */ }}
      />
    </aside>
  )}
</div>
```

Add sidebar style:
```tsx
sidebar: {
  width: 240,
  borderLeft: '1px solid var(--border-light)',
  background: 'var(--bg-secondary)',
  flexShrink: 0,
  overflowY: 'auto',
},
```

**Step 3: Commit**
```bash
git add web/src/components/SchemaBrowser.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: schema browser sidebar for connector catalog"
```

---

## Group 4: Frontend — Schedules UI

---

### Task 10: Schedule management panel in NotebookPage

**Files:**
- Create: `web/src/components/SchedulesPanel.tsx`
- Modify: `web/src/pages/NotebookPage.tsx`
- Modify: `web/src/types/index.ts`

**Step 1: Add Schedule type to types/index.ts**
```ts
export interface Schedule {
  id: string
  notebook_id: string
  cron_expression: string
  parameter_overrides: Record<string, string>
  enabled: boolean
  last_run_at: string | null
  next_run_at: string | null
  created_at: string
  updated_at: string
}
```

**Step 2: Create SchedulesPanel.tsx**

A slide-out panel triggered by a "Schedules" button in the notebook header:

```tsx
import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Schedule } from '../types'

interface Props { notebookId: string; onClose: () => void }

export function SchedulesPanel({ notebookId, onClose }: Props) {
  const qc = useQueryClient()
  const [newCron, setNewCron] = useState('0 9 * * 1-5')
  const [creating, setCreating] = useState(false)

  const { data: schedules = [] } = useQuery({
    queryKey: ['schedules', notebookId],
    queryFn: () => api.get<Schedule[]>(`/api/v1/notebooks/${notebookId}/schedules`),
  })

  const createSchedule = useMutation({
    mutationFn: () => api.post<Schedule>(`/api/v1/notebooks/${notebookId}/schedules`, { cron_expression: newCron }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['schedules', notebookId] }); setCreating(false); setNewCron('0 9 * * 1-5') },
  })

  const deleteSchedule = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/schedules/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['schedules', notebookId] }),
  })

  const toggleSchedule = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.put(`/api/v1/schedules/${id}`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['schedules', notebookId] }),
  })

  const fmt = (d: string | null) => d ? new Date(d).toLocaleString() : 'Never'

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.panel} onClick={(e) => e.stopPropagation()}>
        <div style={styles.panelHeader}>
          <span style={styles.panelTitle}>Schedules</span>
          <button style={styles.closeBtn} onClick={onClose}>✕</button>
        </div>

        {schedules.map((s) => (
          <div key={s.id} style={styles.scheduleRow}>
            <div style={styles.scheduleMain}>
              <code style={styles.cron}>{s.cron_expression}</code>
              <span style={styles.schedMeta}>Next: {fmt(s.next_run_at)} · Last: {fmt(s.last_run_at)}</span>
            </div>
            <div style={styles.schedActions}>
              <button
                style={{ ...styles.toggleBtn, background: s.enabled ? '#d4edda' : 'var(--border)' }}
                onClick={() => toggleSchedule.mutate({ id: s.id, enabled: !s.enabled })}
              >
                {s.enabled ? 'On' : 'Off'}
              </button>
              <button style={styles.delBtn} onClick={() => deleteSchedule.mutate(s.id)}>✕</button>
            </div>
          </div>
        ))}

        {schedules.length === 0 && !creating && (
          <p style={styles.empty}>No schedules. Run this notebook automatically on a cron schedule.</p>
        )}

        {creating ? (
          <div style={styles.createRow}>
            <input
              style={styles.cronInput}
              value={newCron}
              onChange={(e) => setNewCron(e.target.value)}
              placeholder="0 9 * * 1-5"
            />
            <button style={styles.addBtn} onClick={() => createSchedule.mutate()}>Add</button>
            <button style={styles.cancelBtn} onClick={() => setCreating(false)}>Cancel</button>
          </div>
        ) : (
          <button style={styles.newBtn} onClick={() => setCreating(true)}>+ New Schedule</button>
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 200, display: 'flex', justifyContent: 'flex-end' },
  panel: { width: 380, background: 'white', boxShadow: '-4px 0 20px rgba(0,0,0,0.1)', display: 'flex', flexDirection: 'column', padding: 24, gap: 12, overflowY: 'auto' },
  panelHeader: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 },
  panelTitle: { fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  closeBtn: { background: 'none', border: 'none', fontSize: 16, cursor: 'pointer', color: 'var(--text-muted)' },
  scheduleRow: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '10px 0', borderBottom: '1px solid var(--border-light)' },
  scheduleMain: { display: 'flex', flexDirection: 'column', gap: 3 },
  cron: { fontFamily: 'var(--font-mono)', fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  schedMeta: { fontSize: 11, color: 'var(--text-muted)' },
  schedActions: { display: 'flex', gap: 6, alignItems: 'center' },
  toggleBtn: { padding: '3px 10px', border: 'none', borderRadius: 4, fontSize: 11, fontWeight: 700, cursor: 'pointer' },
  delBtn: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 13 },
  empty: { color: 'var(--text-muted)', fontSize: 13, fontStyle: 'italic', margin: '8px 0' },
  createRow: { display: 'flex', gap: 8, alignItems: 'center' },
  cronInput: { flex: 1, padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 5, fontFamily: 'var(--font-mono)', fontSize: 13 },
  addBtn: { padding: '6px 14px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 5, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  cancelBtn: { padding: '6px 10px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, cursor: 'pointer' },
  newBtn: { alignSelf: 'flex-start', padding: '6px 14px', background: 'transparent', border: '1.5px dashed var(--border)', borderRadius: 6, fontSize: 13, color: 'var(--text-muted)', cursor: 'pointer' },
}
```

**Step 3: Wire into NotebookPage.tsx**

Add state `const [schedulesOpen, setSchedulesOpen] = useState(false)` and a "Schedules" button in the header right section. Render `<SchedulesPanel>` when open.

**Step 4: Commit**
```bash
git add web/src/components/SchedulesPanel.tsx web/src/pages/NotebookPage.tsx web/src/types/index.ts
git commit -m "feat: schedules management panel in notebook page"
```

---

## Group 5: Frontend — Dashboards

---

### Task 11: Dashboard list page

**Files:**
- Create: `web/src/pages/DashboardsPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/HomePage.tsx`
- Modify: `web/src/types/index.ts`

**Step 1: Add Dashboard type to types/index.ts** (already partially defined, verify Widget is complete)
```ts
export interface Dashboard {
  id: string
  org_id: string
  title: string
  settings: { refresh_interval?: number }
  public_token?: string
  created_at: string
  updated_at: string
  widgets?: Widget[]
}

export interface Widget {
  id: string
  dashboard_id: string
  notebook_id: string
  cell_id: string
  type: 'chart' | 'table' | 'text' | 'metric'
  layout: { row: number; col: number; width: number; height: number }
  config: Record<string, unknown>
  created_at: string
}
```

**Step 2: Create DashboardsPage.tsx** (list + create, similar pattern to HomePage notebook list)

```tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard } from '../types'

export function DashboardsPage() {
  const qc = useQueryClient()
  const [newTitle, setNewTitle] = useState('')
  const [creating, setCreating] = useState(false)

  const { data: dashboards = [], isLoading } = useQuery({
    queryKey: ['dashboards'],
    queryFn: () => api.get<Dashboard[]>('/api/v1/dashboards'),
  })

  const createDashboard = useMutation({
    mutationFn: () => api.post<Dashboard>('/api/v1/dashboards', { title: newTitle, settings: {} }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['dashboards'] }); setNewTitle(''); setCreating(false) },
  })

  const deleteDashboard = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/dashboards/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboards'] }),
  })

  return (
    <div style={styles.page}>
      {/* Header with nav and + New button — same pattern as HomePage */}
      {/* Grid of dashboard cards with title, widget count, link to editor */}
      {/* Create form inline similar to HomePage */}
    </div>
  )
}
// Styles follow the same pattern as HomePage
```

**Step 3: Register route and add nav link**

In App.tsx:
```tsx
import { DashboardsPage } from './pages/DashboardsPage'
<Route path="/dashboards" element={<ProtectedRoute><DashboardsPage /></ProtectedRoute>} />
```

In HomePage nav, add:
```tsx
<Link to="/dashboards" style={styles.navLink}>Dashboards</Link>
<Link to="/connectors" style={styles.navLink}>Connectors</Link>
```

**Step 4: Commit**
```bash
git add web/src/pages/DashboardsPage.tsx web/src/App.tsx web/src/pages/HomePage.tsx web/src/types/index.ts
git commit -m "feat: dashboards list page"
```

---

### Task 12: Dashboard editor

**Files:**
- Create: `web/src/pages/DashboardEditorPage.tsx`
- Modify: `web/src/App.tsx`

This is the most complex frontend task. The editor shows a grid of widgets. Widgets pull their data from notebook cells.

**Step 1: Register route**
```tsx
<Route path="/dashboards/:id" element={<ProtectedRoute><DashboardEditorPage /></ProtectedRoute>} />
```

**Step 2: Create DashboardEditorPage.tsx**

Key sections:
1. **Header**: title (inline rename via PUT /api/v1/dashboards/{id}), "Share" button to generate public token, "Add Widget" button
2. **Grid**: CSS grid, each widget rendered in its `layout` position
3. **Widget component**: renders based on widget type — calls `GET /api/v1/notebooks/{notebookId}` to get cell data, then renders table/chart/text/metric
4. **Add Widget modal**: select notebook → select cell → select widget type → position (row, col, width, height) → POST /api/v1/dashboards/{id}/widgets

Backend note: `GET /api/v1/notebooks/{id}` already returns cells with outputs, so widgets can use that data without a separate API call.

```tsx
import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Dashboard, Widget, Notebook, Cell } from '../types'
import { OutputRenderer } from '../components/OutputRenderer'

interface DashboardWithWidgets extends Dashboard { widgets: Widget[] }

export function DashboardEditorPage() {
  const { id } = useParams<{ id: string }>()
  const qc = useQueryClient()
  const [addingWidget, setAddingWidget] = useState(false)
  const [shareUrl, setShareUrl] = useState<string | null>(null)

  const { data: dashboard } = useQuery({
    queryKey: ['dashboard', id],
    queryFn: () => api.get<DashboardWithWidgets>(`/api/v1/dashboards/${id}`),
    enabled: !!id,
  })

  const { data: notebooks = [] } = useQuery({
    queryKey: ['notebooks'],
    queryFn: () => api.get<Notebook[]>('/api/v1/notebooks'),
    enabled: addingWidget,
  })

  const deleteWidget = useMutation({
    mutationFn: (wid: string) => api.delete(`/api/v1/dashboards/${id}/widgets/${wid}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['dashboard', id] }),
  })

  const shareDashboard = useMutation({
    mutationFn: () => api.post<{ token: string }>(`/api/v1/dashboards/${id}/share`, {}),
    onSuccess: (data) => setShareUrl(`${window.location.origin}/public/dashboards/${data.token}`),
  })

  const widgets = dashboard?.widgets ?? []

  return (
    <div style={styles.page}>
      <header style={styles.header}>
        <div style={styles.headerLeft}>
          <Link to="/dashboards" style={styles.backLink}>← Dashboards</Link>
          <span>/</span>
          <span style={styles.title}>{dashboard?.title ?? 'Loading…'}</span>
        </div>
        <div style={styles.headerRight}>
          {shareUrl && <input style={styles.shareInput} readOnly value={shareUrl} onClick={(e) => (e.target as HTMLInputElement).select()} />}
          <button style={styles.shareBtn} onClick={() => shareDashboard.mutate()}>Share</button>
          <button style={styles.addBtn} onClick={() => setAddingWidget(true)}>+ Widget</button>
        </div>
      </header>

      {/* Widget grid */}
      <div style={styles.grid}>
        {widgets.map((w) => (
          <DashboardWidget key={w.id} widget={w} onDelete={() => deleteWidget.mutate(w.id)} />
        ))}
        {widgets.length === 0 && (
          <div style={styles.emptyGrid}>
            <p>No widgets yet. Add a widget to display notebook cell outputs here.</p>
            <button style={styles.addBtn} onClick={() => setAddingWidget(true)}>+ Add Widget</button>
          </div>
        )}
      </div>

      {addingWidget && (
        <AddWidgetModal
          dashboardId={id!}
          notebooks={notebooks}
          onClose={() => setAddingWidget(false)}
          onAdded={() => { qc.invalidateQueries({ queryKey: ['dashboard', id] }); setAddingWidget(false) }}
        />
      )}
    </div>
  )
}

function DashboardWidget({ widget, onDelete }: { widget: Widget; onDelete: () => void }) {
  const { data: notebook } = useQuery({
    queryKey: ['notebook', widget.notebook_id],
    queryFn: () => api.get<{ cells: Cell[] }>(`/api/v1/notebooks/${widget.notebook_id}`),
  })
  const cell = notebook?.cells?.find((c) => c.id === widget.cell_id)

  return (
    <div style={{
      ...styles.widget,
      gridColumn: `${widget.layout.col} / span ${widget.layout.width}`,
      gridRow: `${widget.layout.row} / span ${widget.layout.height}`,
    }}>
      <div style={styles.widgetHeader}>
        <span style={styles.widgetLabel}>{widget.type}</span>
        <button style={styles.delWidgetBtn} onClick={onDelete}>✕</button>
      </div>
      {cell ? <OutputRenderer outputs={cell.outputs} /> : <div style={styles.widgetEmpty}>Loading…</div>}
    </div>
  )
}

function AddWidgetModal({ dashboardId, notebooks, onClose, onAdded }: {
  dashboardId: string; notebooks: Notebook[]; onClose: () => void; onAdded: () => void
}) {
  const [notebookId, setNotebookId] = useState('')
  const [cellId, setCellId] = useState('')
  const [type, setType] = useState<Widget['type']>('table')
  const [row, setRow] = useState(1); const [col, setCol] = useState(1)
  const [width, setWidth] = useState(6); const [height, setHeight] = useState(4)

  const { data: notebook } = useQuery({
    queryKey: ['notebook', notebookId],
    queryFn: () => api.get<{ cells: Cell[] }>(`/api/v1/notebooks/${notebookId}`),
    enabled: !!notebookId,
  })

  const addWidget = useMutation({
    mutationFn: () => api.post(`/api/v1/dashboards/${dashboardId}/widgets`, {
      notebook_id: notebookId, cell_id: cellId, type,
      layout: { row, col, width, height }, config: {},
    }),
    onSuccess: onAdded,
  })

  return (
    <div style={styles.modalOverlay} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <h3 style={styles.modalTitle}>Add Widget</h3>
        <label style={styles.modalLabel}>Notebook
          <select style={styles.modalInput} value={notebookId} onChange={(e) => { setNotebookId(e.target.value); setCellId('') }}>
            <option value="">Select notebook…</option>
            {notebooks.map((n) => <option key={n.id} value={n.id}>{n.title}</option>)}
          </select>
        </label>
        {notebookId && (
          <label style={styles.modalLabel}>Cell
            <select style={styles.modalInput} value={cellId} onChange={(e) => setCellId(e.target.value)}>
              <option value="">Select cell…</option>
              {notebook?.cells?.filter(c => c.type === 'code').map((c) => (
                <option key={c.id} value={c.id}>{c.source.slice(0, 60) || 'Empty cell'}</option>
              ))}
            </select>
          </label>
        )}
        <label style={styles.modalLabel}>Widget Type
          <select style={styles.modalInput} value={type} onChange={(e) => setType(e.target.value as Widget['type'])}>
            <option value="table">Table</option>
            <option value="chart">Chart</option>
            <option value="metric">Metric</option>
            <option value="text">Text</option>
          </select>
        </label>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: 8 }}>
          {[['Row', row, setRow], ['Col', col, setCol], ['Width', width, setWidth], ['Height', height, setHeight]].map(([label, val, setter]) => (
            <label key={String(label)} style={styles.modalLabel}>{String(label)}
              <input style={styles.modalInput} type="number" min={1} value={Number(val)} onChange={(e) => (setter as (v: number) => void)(parseInt(e.target.value) || 1)} />
            </label>
          ))}
        </div>
        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
          <button style={styles.cancelBtn} onClick={onClose}>Cancel</button>
          <button style={styles.saveBtn} onClick={() => addWidget.mutate()} disabled={!cellId}>Add Widget</button>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: { minHeight: '100vh', background: 'var(--bg-primary)', display: 'flex', flexDirection: 'column' },
  header: { background: 'var(--nav-bg)', borderBottom: '1px solid var(--nav-border)', height: 52, display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0 24px', position: 'sticky', top: 0, zIndex: 100 },
  headerLeft: { display: 'flex', alignItems: 'center', gap: 10 },
  backLink: { color: '#6a6260', textDecoration: 'none', fontSize: 13, fontWeight: 500 },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--nav-text)' },
  headerRight: { display: 'flex', alignItems: 'center', gap: 10 },
  shareInput: { fontSize: 12, padding: '4px 10px', border: '1px solid var(--border)', borderRadius: 4, width: 320, fontFamily: 'var(--font-mono)' },
  shareBtn: { padding: '5px 14px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 5, fontSize: 12, cursor: 'pointer', color: 'var(--nav-text)' },
  addBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  grid: { flex: 1, display: 'grid', gridTemplateColumns: 'repeat(12, 1fr)', gap: 16, padding: 24, alignContent: 'start' },
  emptyGrid: { gridColumn: '1 / -1', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 16, minHeight: 300, color: 'var(--text-muted)', fontSize: 14 },
  widget: { background: 'white', border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden', boxShadow: 'var(--shadow-sm)', display: 'flex', flexDirection: 'column' },
  widgetHeader: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '6px 12px', borderBottom: '1px solid var(--border-light)', background: 'var(--bg-secondary)' },
  widgetLabel: { fontSize: 10, fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--text-muted)' },
  delWidgetBtn: { background: 'none', border: 'none', cursor: 'pointer', fontSize: 12, color: 'var(--text-muted)' },
  widgetEmpty: { flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: 13 },
  modalOverlay: { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', zIndex: 300, display: 'flex', alignItems: 'center', justifyContent: 'center' },
  modal: { background: 'white', borderRadius: 12, padding: 24, width: 480, display: 'flex', flexDirection: 'column', gap: 12, boxShadow: '0 20px 60px rgba(0,0,0,0.15)' },
  modalTitle: { margin: 0, fontSize: 16, fontWeight: 700 },
  modalLabel: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  modalInput: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, fontFamily: 'var(--font-mono)' },
  cancelBtn: { padding: '7px 18px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, cursor: 'pointer' },
  saveBtn: { padding: '7px 18px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 5, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
}
```

**Step 3: Commit**
```bash
git add web/src/pages/DashboardEditorPage.tsx web/src/App.tsx
git commit -m "feat: dashboard editor with widget management and public sharing"
```

---

### Task 13: Public dashboard viewer (no auth)

**Files:**
- Create: `web/src/pages/PublicDashboardPage.tsx`
- Modify: `web/src/App.tsx`

**Step 1: Create PublicDashboardPage.tsx**

Similar to DashboardEditorPage but:
- No auth required
- No add/delete widget buttons
- Fetches from `GET /api/v1/public/dashboards/{token}`
- Uses a separate non-authenticated API call

```tsx
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

export function PublicDashboardPage() {
  const { token } = useParams<{ token: string }>()

  const { data: dashboard, isLoading } = useQuery({
    queryKey: ['public-dashboard', token],
    queryFn: async () => {
      const res = await fetch(`/api/v1/public/dashboards/${token}`)
      if (!res.ok) throw new Error('Dashboard not found')
      return res.json()
    },
    enabled: !!token,
  })

  if (isLoading) return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>Loading…</div>
  if (!dashboard) return <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh' }}>Dashboard not found</div>

  // Render read-only grid of widgets using OutputRenderer
  // No edit controls
}
```

**Step 2: Register route (outside ProtectedRoute)**
```tsx
<Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
```

**Step 3: Commit**
```bash
git add web/src/pages/PublicDashboardPage.tsx web/src/App.tsx
git commit -m "feat: public dashboard viewer (no auth)"
```

---

## Group 6: Frontend — Member & Org Management

---

### Task 14: Member management page

**Files:**
- Create: `web/src/pages/MembersPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Create MembersPage.tsx**

Admin-only page. Lists current members with their roles. Allows role changes and removal. Has an invite form (by email).

Key API calls:
- `GET /api/v1/members` → list
- `POST /api/v1/members` → invite by email + role
- `PUT /api/v1/members/{user_id}` → change role
- `DELETE /api/v1/members/{user_id}` → remove

The component follows the same table pattern as ConnectorsPage.

**Step 2: Register route**
```tsx
<Route path="/members" element={<ProtectedRoute><MembersPage /></ProtectedRoute>} />
```

**Step 3: Add nav link in HomePage**

**Step 4: Commit**
```bash
git add web/src/pages/MembersPage.tsx web/src/App.tsx web/src/pages/HomePage.tsx
git commit -m "feat: member management page"
```

---

### Task 15: Audit log viewer

**Files:**
- Create: `web/src/pages/AuditPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/pages/HomePage.tsx`

**Step 1: Create AuditPage.tsx**

Admin-only page. Paginated table of audit log entries. Filters:
- Action (text input)
- Resource type (select: notebook, cell, connector, schedule, dashboard)
- User ID (text input)

Key API call:
- `GET /api/v1/audit?limit=100&offset=0&action=...&resource_type=...`

Columns: Timestamp, User, Action, Resource Type, Resource ID

**Step 2: Register route and add nav link**

**Step 3: Commit**
```bash
git add web/src/pages/AuditPage.tsx web/src/App.tsx web/src/pages/HomePage.tsx
git commit -m "feat: audit log viewer (admin)"
```

---

## Group 7: Global Navigation Refactor

---

### Task 16: Navigation sidebar / top nav with role-aware links

**Files:**
- Create: `web/src/components/NavBar.tsx`
- Modify: `web/src/pages/HomePage.tsx`
- Modify: `web/src/hooks/useAuth.ts`

Currently, the `useAuth` hook stores the token but may not expose the user's role. The nav should show admin-only items (Connectors, Members, Audit) only to admins.

**Step 1: Check useAuth exposes role**

Read `web/src/hooks/useAuth.ts`. If the JWT payload or login response includes the role, decode it and expose it from the hook. Store `role` in localStorage alongside the token.

**Step 2: Update login flow to store role**

In `web/src/api/auth.ts`, after login, save `role` to localStorage.

**Step 3: Create NavBar component**

A shared top navigation bar used by all authenticated pages. Shows:
- Logo/name (links to `/`)
- Notebooks, Dashboards (all roles)
- Connectors, Members, Audit (admin only)
- User email + Sign out (right side)

**Step 4: Replace per-page headers with NavBar in all pages**

Refactor HomePage, ConnectorsPage, MembersPage, AuditPage, DashboardsPage to use NavBar for the top portion instead of duplicating header code.

**Step 5: Commit**
```bash
git add web/src/components/NavBar.tsx web/src/hooks/useAuth.ts web/src/api/auth.ts web/src/pages/
git commit -m "feat: shared navbar with role-aware links"
```

---

## Group 8: Real-Time Collaboration

---

### Task 17: Yjs WebSocket integration in NotebookPage

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`
- Modify: `web/src/components/CodeCell.tsx`
- Modify: `web/src/components/TextCell.tsx`

This replaces the current REST-based cell state with Yjs CRDT editing. The Yjs relay (Hocuspocus) is already running and the backend has internal endpoints for persistence.

**Step 1: Understand what's already installed**

Check `web/package.json` for `yjs`, `y-codemirror.next`, `@hocuspocus/provider`. These may already be listed.

**Step 2: Install missing packages if needed**
```bash
cd web && npm install yjs @hocuspocus/provider y-codemirror.next
```

**Step 3: Create Yjs provider hook**

Create `web/src/hooks/useNotebookYjs.ts`:
```ts
import { useEffect, useRef } from 'react'
import * as Y from 'yjs'
import { HocuspocusProvider } from '@hocuspocus/provider'
import { getToken } from '../api/auth'

export function useNotebookYjs(notebookId: string) {
  const docRef = useRef(new Y.Doc())
  const providerRef = useRef<HocuspocusProvider | null>(null)

  useEffect(() => {
    const token = getToken()
    const provider = new HocuspocusProvider({
      url: `ws://${window.location.host}/api/v1/ws/notebooks/${notebookId}`,
      name: notebookId,
      document: docRef.current,
      token: token ?? '',
    })
    providerRef.current = provider
    return () => provider.destroy()
  }, [notebookId])

  return { doc: docRef.current, provider: providerRef.current }
}
```

**Step 4: Bind CodeMirror to Y.Text**

In `CodeCell.tsx`, use `yCollab` from `y-codemirror.next` to bind the editor to a `Y.Text` for the cell:

```ts
import { yCollab } from 'y-codemirror.next'
// In the editor setup, add:
yCollab(yText, provider.awareness)
```

Where `yText` comes from `doc.getText(cell.id)`.

**Step 5: Awareness (cursors)**

The Hocuspocus provider gives awareness out of the box. Display collaborator cursors using the `yCollab` cursor rendering built into `y-codemirror.next`.

**Step 6: Cell order sync**

Use `Y.Array` on the doc for the cell order. Cell additions/deletions update the Y.Array. This is the hardest part — plan to keep REST as fallback for create/delete and sync order via Y.Array.

**Step 7: Commit**
```bash
git add web/src/hooks/useNotebookYjs.ts web/src/components/CodeCell.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: Yjs real-time collaboration in notebook editor"
```

---

## Group 9: Auth — OIDC/SSO

---

### Task 18: OIDC login button in LoginPage

**Files:**
- Modify: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/api/auth.ts`
- Modify: `internal/api/auth_handlers.go` (OIDC callback if not complete)
- Modify: `internal/api/router.go`

**Step 1: Check if OIDC backend routes exist**

Look in `router.go` for `/auth/oidc` routes. Add if missing:
```go
s.mux.Handle("GET /api/v1/auth/oidc/authorize", http.HandlerFunc(s.handleOIDCAuthorize))
s.mux.Handle("GET /api/v1/auth/oidc/callback", http.HandlerFunc(s.handleOIDCCallback))
```

**Step 2: Add OIDC config to org settings**

If not already done, orgs need OIDC config (issuer, client_id, client_secret). Add GET/PUT `/api/v1/org/settings` API.

**Step 3: Add SSO button to LoginPage.tsx**

Below the email/password form, add:
```tsx
<div style={styles.divider}><span>or</span></div>
<button style={styles.ssoBtn} onClick={() => window.location.href = '/api/v1/auth/oidc/authorize'}>
  Continue with SSO
</button>
```

The OIDC flow is: user clicks SSO → redirect to identity provider → callback to `/api/v1/auth/oidc/callback` → redirect to frontend with JWT in query param → frontend stores token and redirects to `/`.

**Step 4: Handle OIDC callback in frontend**

Add route in App.tsx (or handle in the LoginPage if token is in URL):
```tsx
<Route path="/auth/callback" element={<OIDCCallbackPage />} />
```

`OIDCCallbackPage` reads `?token=...` from URL, calls `setToken(token)`, then navigates to `/`.

**Step 5: Commit**
```bash
git add web/src/pages/LoginPage.tsx web/src/App.tsx internal/api/auth_handlers.go internal/api/router.go
git commit -m "feat: OIDC/SSO login flow"
```

---

## Summary of Tasks

| # | Task | Area | Priority |
|---|------|------|----------|
| 1 | Notebook update endpoint (PUT) | Backend | High |
| 2 | Member management API | Backend | High |
| 3 | Connector test + schema endpoints | Backend | High |
| 4 | Schedule enable/disable + update | Backend | Medium |
| 5 | Audit log API endpoint | Backend | Medium |
| 6 | Notebook inline rename | Frontend | High |
| 7 | Notebook parameters UI | Frontend | High |
| 8 | Connector management page | Frontend | High |
| 9 | Schema browser sidebar | Frontend | Medium |
| 10 | Schedules management panel | Frontend | Medium |
| 11 | Dashboard list page | Frontend | High |
| 12 | Dashboard editor | Frontend | High |
| 13 | Public dashboard viewer | Frontend | Medium |
| 14 | Member management page | Frontend | Medium |
| 15 | Audit log viewer | Frontend | Low |
| 16 | Shared navbar with role-aware links | Frontend | Medium |
| 17 | Yjs real-time collaboration | Frontend | Low |
| 18 | OIDC/SSO login | Full-stack | Low |
