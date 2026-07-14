# Public Sharing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Share notebooks and dashboards publicly via unauthenticated read-only links, with org-level kill switch.

**Architecture:** A `public_tokens` table replaces the ad-hoc dashboard column. A single `GET /api/v1/public/{token}` endpoint serves both resource types. Share/revoke endpoints manage tokens. Org settings toggle the feature.

**Tech Stack:** Go, pgx, React, react-markdown

**Reference design:** `docs/plans/2026-06-28-public-sharing-design.md`

---

### Task 1: Migration — public_tokens table + org toggle

**Files:**
- Create: `internal/database/migrations/V074__public_tokens.sql`

**SQL:**

```sql
CREATE TABLE public_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('notebook', 'dashboard')),
    resource_id   UUID NOT NULL,
    token         TEXT NOT NULL UNIQUE DEFAULT encode(gen_random_bytes(16), 'hex'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_public_tokens_org ON public_tokens (org_id);
CREATE INDEX idx_public_tokens_resource ON public_tokens (resource_type, resource_id);

-- Migrate existing dashboard tokens (migration ID 0 for legacy)
INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_at, created_by)
SELECT d.org_id, 'dashboard', d.id, d.public_token, NOW(), '00000000-0000-0000-0000-000000000000'
FROM dashboards d WHERE d.public_token IS NOT NULL;

ALTER TABLE dashboards DROP COLUMN public_token;

ALTER TABLE orgs ADD COLUMN public_sharing_enabled BOOLEAN NOT NULL DEFAULT true;
```

**Commit:**

```bash
git add internal/database/migrations/V074__public_tokens.sql
git commit -m "feat: add public_tokens table and org sharing toggle"
```

---

### Task 2: Model — PublicToken struct

**Files:**
- Create: `internal/models/public_token.go`

**Code:**

```go
package models

import "time"

type PublicToken struct {
    ID           string    `json:"id"`
    OrgID        string    `json:"org_id"`
    ResourceType string    `json:"resource_type"`
    ResourceID   string    `json:"resource_id"`
    Token        string    `json:"token"`
    CreatedAt    time.Time `json:"created_at"`
    CreatedBy    string    `json:"created_by"`
}
```

**Commit:**

```bash
git add internal/models/public_token.go
git commit -m "feat: add PublicToken model"
```

---

### Task 3: Notebook share/revoke handlers

**Files:**
- Modify: `internal/api/notebook_handlers.go`

**Step 1: Add handleShareNotebook**

After `handleCloneNotebook`, add:

```go
func (s *Server) handleShareNotebook(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    nbID := r.PathValue("id")
    ctx := r.Context()

    allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "share")
    if err != nil {
        writeError(w, http.StatusInternalServerError, "permission check failed")
        return
    }
    if !allowed {
        writeError(w, http.StatusForbidden, "insufficient permissions")
        return
    }

    // Check org-level toggle
    var sharingEnabled bool
    s.db.Pool.QueryRow(ctx, `SELECT public_sharing_enabled FROM orgs WHERE id=$1`, claims.OrgID).Scan(&sharingEnabled)
    if !sharingEnabled {
        writeError(w, http.StatusForbidden, "public sharing is disabled for this organization")
        return
    }

    // Check for existing token
    var token string
    err = s.db.Pool.QueryRow(ctx,
        `SELECT token FROM public_tokens WHERE resource_type='notebook' AND resource_id=$1`,
        nbID,
    ).Scan(&token)
    if err == nil {
        writeJSON(w, http.StatusOK, map[string]string{"token": token})
        return
    }

    // Generate new token
    tokenBytes := make([]byte, 16)
    rand.Read(tokenBytes)
    token = hex.EncodeToString(tokenBytes)

    _, err = s.db.Pool.Exec(ctx,
        `INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_by)
         VALUES ($1, 'notebook', $2, $3, $4)`,
        claims.OrgID, nbID, token, claims.UserID,
    )
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to create share link")
        return
    }

    writeJSON(w, http.StatusCreated, map[string]string{"token": token})
}
```

**Step 2: Add handleRevokeNotebookShare**

```go
func (s *Server) handleRevokeNotebookShare(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    nbID := r.PathValue("id")

    allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "notebook", nbID, "share")
    if err != nil {
        writeError(w, http.StatusInternalServerError, "permission check failed")
        return
    }
    if !allowed {
        writeError(w, http.StatusForbidden, "insufficient permissions")
        return
    }

    _, err = s.db.Pool.Exec(r.Context(),
        `DELETE FROM public_tokens WHERE resource_type='notebook' AND resource_id=$1`,
        nbID,
    )
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to revoke share link")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

**Step 3: Add imports**

Add `"crypto/rand"`, `"encoding/hex"` to notebook_handlers.go's imports (check if already present).

**Step 4: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Commit:**

```bash
git add internal/api/notebook_handlers.go
git commit -m "feat: add notebook share/revoke handlers"
```

---

### Task 4: Refactor dashboard share to use public_tokens

**Files:**
- Modify: `internal/api/dashboard_handlers.go`

**Step 1: Update handleShareDashboard**

Replace the current implementation (which writes to `dashboards.public_token`) with the same pattern as notebook share — insert into `public_tokens` with `resource_type='dashboard'`.

```go
func (s *Server) handleShareDashboard(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    dashID := r.PathValue("id")
    ctx := r.Context()

    allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "dashboard", dashID, "share")
    if err != nil { writeError(w, http.StatusInternalServerError, "permission check failed"); return }
    if !allowed { writeError(w, http.StatusForbidden, "insufficient permissions"); return }

    var sharingEnabled bool
    s.db.Pool.QueryRow(ctx, `SELECT public_sharing_enabled FROM orgs WHERE id=$1`, claims.OrgID).Scan(&sharingEnabled)
    if !sharingEnabled {
        writeError(w, http.StatusForbidden, "public sharing is disabled for this organization")
        return
    }

    var token string
    err = s.db.Pool.QueryRow(ctx,
        `SELECT token FROM public_tokens WHERE resource_type='dashboard' AND resource_id=$1`,
        dashID,
    ).Scan(&token)
    if err == nil {
        writeJSON(w, http.StatusOK, map[string]string{"token": token})
        return
    }

    tokenBytes := make([]byte, 16)
    rand.Read(tokenBytes)
    token = hex.EncodeToString(tokenBytes)

    _, err = s.db.Pool.Exec(ctx,
        `INSERT INTO public_tokens (org_id, resource_type, resource_id, token, created_by)
         VALUES ($1, 'dashboard', $2, $3, $4)`,
        claims.OrgID, dashID, token, claims.UserID,
    )
    if err != nil { writeError(w, http.StatusInternalServerError, "failed to create share link"); return }

    writeJSON(w, http.StatusCreated, map[string]string{"token": token})
}
```

**Step 2: Update handlePublicDashboard**

Change from querying `dashboards WHERE public_token = $1` to querying `public_tokens` first, then the dashboard by ID.

**Step 3: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Commit:**

```bash
git add internal/api/dashboard_handlers.go
git commit -m "refactor: dashboard sharing uses public_tokens table"
```

---

### Task 5: Public access handler

**Files:**
- Create: `internal/api/public_handlers.go`

**Handler:**

```go
package api

import (
    "net/http"

    "github.com/jackc/pgx/v5"
)

func (s *Server) handlePublicResource(w http.ResponseWriter, r *http.Request) {
    token := r.PathValue("token")
    ctx := r.Context()

    var resourceType, resourceID, orgID string
    err := s.db.Pool.QueryRow(ctx,
        `SELECT pt.resource_type, pt.resource_id, pt.org_id
         FROM public_tokens pt
         JOIN orgs o ON o.id = pt.org_id
         WHERE pt.token = $1 AND o.public_sharing_enabled = true`,
        token,
    ).Scan(&resourceType, &resourceID, &orgID)
    if err != nil {
        writeError(w, http.StatusNotFound, "resource not found or sharing disabled")
        return
    }

    switch resourceType {
    case "notebook":
        s.servePublicNotebook(w, r, resourceID, orgID)
    case "dashboard":
        s.servePublicDashboard(w, r, resourceID)
    default:
        writeError(w, http.StatusNotFound, "unknown resource type")
    }
}
```

**Step 1: Add servePublicNotebook helper**

Queries the notebook + cells (with outputs, excluding metadata), returns JSON:

```go
func (s *Server) servePublicNotebook(w http.ResponseWriter, r *http.Request, nbID, orgID string) {
    ctx := r.Context()

    var nb models.Notebook
    var params []byte
    err := s.db.Pool.QueryRow(ctx,
        `SELECT id, title, COALESCE(description,''), parameters, created_at, updated_at
         FROM notebooks WHERE id=$1 AND org_id=$2`,
        nbID, orgID,
    ).Scan(&nb.ID, &nb.Title, &nb.Description, &params, &nb.CreatedAt, &nb.UpdatedAt)
    if err != nil {
        writeError(w, http.StatusNotFound, "notebook not found")
        return
    }
    json.Unmarshal(params, &nb.Parameters)

    rows, err := s.db.Pool.Query(ctx,
        `SELECT position, type, language, source, outputs, parameters
         FROM cells WHERE notebook_id=$1 ORDER BY position ASC`,
        nbID,
    )
    if err != nil {
        writeError(w, http.StatusInternalServerError, "query failed")
        return
    }
    defer rows.Close()

    type publicCell struct {
        Position   int              `json:"position"`
        Type       string           `json:"type"`
        Language   string           `json:"language,omitempty"`
        Source     string           `json:"source"`
        Outputs    []models.Output  `json:"outputs"`
        Parameters []models.Parameter `json:"parameters"`
    }
    var cells []publicCell
    for rows.Next() {
        var c publicCell
        var lang *string
        var outputs, cellParams []byte
        if err := rows.Scan(&c.Position, &c.Type, &lang, &c.Source, &outputs, &cellParams); err != nil {
            continue
        }
        if lang != nil { c.Language = *lang }
        json.Unmarshal(outputs, &c.Outputs)
        json.Unmarshal(cellParams, &c.Parameters)
        if c.Outputs == nil { c.Outputs = []models.Output{} }
        cells = append(cells, c)
    }
    if cells == nil { cells = []publicCell{} }

    writeJSON(w, http.StatusOK, map[string]any{
        "type":     "notebook",
        "notebook": nb,
        "cells":    cells,
    })
}
```

**Step 2: Add servePublicDashboard helper**

Extract the dashboard public serving logic from the current `handlePublicDashboard` into this helper (queries by dashboard ID instead of token).

**Step 3: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Commit:**

```bash
git add internal/api/public_handlers.go
git commit -m "feat: add public resource access handler unifying notebooks and dashboards"
```

---

### Task 6: Wire routes

**Files:**
- Modify: `internal/api/router.go`

**Changes:**

Add routes:

```go
// Public share routes (no auth)
s.mux.HandleFunc("GET /api/v1/public/{token}", s.handlePublicResource)

// Notebook share routes (auth required)
s.mux.Handle("POST /api/v1/notebooks/{id}/share", authMW(s.requirePermission("notebook", "id", "share")(http.HandlerFunc(s.handleShareNotebook))))
s.mux.Handle("DELETE /api/v1/notebooks/{id}/share", authMW(s.requirePermission("notebook", "id", "share")(http.HandlerFunc(s.handleRevokeNotebookShare))))
```

Remove the old dashboard public route and replace the share route.

Remove:
```go
s.mux.HandleFunc("GET /api/v1/public/dashboards/{token}", s.handlePublicDashboard)
```

The old `/api/v1/public/dashboards/{token}` route conflicts with `/api/v1/public/{token}` — remove it. The new generic route handles both notebooks and dashboards.

**Commit:**

```bash
git add internal/api/router.go
git commit -m "feat: wire public sharing routes"
```

---

### Task 7: Org settings — sharing toggle API

**Files:**
- Modify: `internal/api/sso_org_handlers.go` or create in `internal/api/org_handlers.go`

Actually, the SSO settings are in `sso_org_handlers.go`. Let's add the sharing toggle alongside the existing SSO settings handlers in `org_handlers.go`:

Add `handleGetOrgSharingSettings` / `handleUpdateOrgSharingSettings`:

```go
func (s *Server) handleGetOrgSharingSettings(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    var enabled bool
    err := s.db.Pool.QueryRow(r.Context(),
        `SELECT public_sharing_enabled FROM orgs WHERE id=$1`, claims.OrgID,
    ).Scan(&enabled)
    if err != nil { writeError(w, http.StatusInternalServerError, "query failed"); return }
    writeJSON(w, http.StatusOK, map[string]bool{"public_sharing_enabled": enabled})
}

func (s *Server) handleUpdateOrgSharingSettings(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    var req struct {
        Enabled bool `json:"public_sharing_enabled"`
    }
    if err := decodeJSON(r, &req); err != nil { writeError(w, http.StatusBadRequest, "invalid body"); return }
    _, err := s.db.Pool.Exec(r.Context(),
        `UPDATE orgs SET public_sharing_enabled=$1 WHERE id=$2`,
        req.Enabled, claims.OrgID,
    )
    if err != nil { writeError(w, http.StatusInternalServerError, "update failed"); return }
    writeJSON(w, http.StatusOK, map[string]bool{"public_sharing_enabled": req.Enabled})
}
```

Add routes in `router.go`:

```go
s.mux.Handle("GET /api/v1/org/sharing", authMW(http.HandlerFunc(s.handleGetOrgSharingSettings)))
s.mux.Handle("PUT /api/v1/org/sharing", authMW(RequireRole("admin")(http.HandlerFunc(s.handleUpdateOrgSharingSettings))))
```

**Commit:**

```bash
git add internal/api/org_handlers.go internal/api/router.go
git commit -m "feat: add org sharing settings API endpoint"
```

---

### Task 8: Frontend — ShareModal component

**Files:**
- Create: `web/src/components/ShareModal.tsx`

**Component:**

```tsx
import { useState } from 'react'
import { api } from '../api/client'

interface ShareModalProps {
  resourceType: 'notebook' | 'dashboard'
  resourceId: string
  onClose: () => void
}

export function ShareModal({ resourceType, resourceId, onClose }: ShareModalProps) {
  const [token, setToken] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [revoking, setRevoking] = useState(false)

  const publicUrl = token ? `${window.location.origin}/public/${token}` : null

  async function handleShare() {
    setError(null)
    try {
      const res = await api.post<{ token: string }>(`/api/v1/${resourceType}s/${resourceId}/share`, {})
      setToken(res.token)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  async function handleRevoke() {
    setRevoking(true)
    setError(null)
    try {
      await api.delete(`/api/v1/${resourceType}s/${resourceId}/share`)
      setToken(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setRevoking(false)
    }
  }

  async function handleCopy() {
    if (!publicUrl) return
    await navigator.clipboard.writeText(publicUrl)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div style={s.backdrop} onClick={onClose}>
      <div style={s.modal} onClick={e => e.stopPropagation()}>
        <div style={s.header}>
          <span style={s.title}>Share {resourceType}</span>
          <button style={s.closeBtn} onClick={onClose}>×</button>
        </div>
        <div style={s.body}>
          {error && <p style={{ color: 'var(--error)', fontSize: 13 }}>{error}</p>}
          {!token ? (
            <button style={s.btn} onClick={handleShare}>Generate public link</button>
          ) : (
            <>
              <div style={s.urlRow}>
                <input style={s.urlInput} value={publicUrl!} readOnly />
                <button style={s.copyBtn} onClick={handleCopy}>{copied ? 'Copied!' : 'Copy'}</button>
              </div>
              <button style={{ ...s.btn, marginTop: 12, background: 'var(--error)', color: '#fff' }} onClick={handleRevoke} disabled={revoking}>
                {revoking ? 'Revoking…' : 'Revoke public link'}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
  backdrop: { position: 'fixed', inset: 0, background: 'var(--bg-overlay)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 2000 },
  modal: { background: 'var(--bg-card)', borderRadius: 8, boxShadow: 'var(--shadow-lg)', width: 440, maxHeight: '80vh', overflow: 'hidden' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '14px 18px', borderBottom: '1px solid var(--border)' },
  title: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  closeBtn: { background: 'none', border: 'none', cursor: 'pointer', fontSize: 20, color: 'var(--text-muted)', lineHeight: 1, padding: '0 4px' },
  body: { padding: '18px' },
  btn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  urlRow: { display: 'flex', gap: 8 },
  urlInput: { flex: 1, padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, color: 'var(--text-primary)', background: 'var(--bg-input)', fontFamily: 'var(--font-mono)' },
  copyBtn: { padding: '7px 14px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 12, cursor: 'pointer' },
}
```

**Commit:**

```bash
git add web/src/components/ShareModal.tsx
git commit -m "feat: add ShareModal component"
```

---

### Task 9: Frontend — Share button in notebook toolbar

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

**Step 1: Add ShareModal state**

Find the state declarations and add:

```tsx
const [showShare, setShowShare] = useState(false)
```

**Step 2: Add Share button**

In the notebook toolbar (near the export button), add:

```tsx
<button style={toolbarStyles.btn} onClick={() => setShowShare(true)} disabled={!canEdit}>
  Share
</button>
```

**Step 3: Render ShareModal**

Near the bottom of the component (before the closing `</>`):

```tsx
{showShare && (
  <ShareModal resourceType="notebook" resourceId={id} onClose={() => setShowShare(false)} />
)}
```

**Commit:**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: add Share button to notebook toolbar"
```

---

### Task 10: Frontend — Public notebook page

**Files:**
- Create: `web/src/pages/PublicNotebookPage.tsx`
- Modify: `web/src/App.tsx`

**Step 1: Public notebook page component**

```tsx
import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import ReactMarkdown from 'react-markdown'
import { api } from '../api/client'
import { Skeleton } from '../components/Skeleton'
import { ChartView } from '../charts'
import type { Notebook, Cell } from '../types'

interface PublicCell {
  position: number; type: string; language?: string
  source: string; outputs: Output[]; parameters?: Parameter[]
}

export function PublicNotebookPage() {
  const { token } = useParams<{ token: string }>()
  const { data, isLoading, error } = useQuery({
    queryKey: ['public', token],
    queryFn: () => api.get<{ type: string; notebook: Notebook; cells: PublicCell[] }>(`/api/v1/public/${token}`),
    enabled: !!token,
  })

  if (isLoading) return <div style={{ padding: 40 }}><Skeleton count={5} height={40} /></div>
  if (error) return <div style={{ padding: 40, color: 'var(--error)' }}>Not found or sharing disabled</div>
  if (!data) return null

  return (
    <div style={{ maxWidth: 900, margin: '0 auto', padding: '24px 32px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 20 }}>
        <h1 style={{ fontSize: 20, fontWeight: 600, color: 'var(--text-primary)', margin: 0 }}>{data.notebook.title}</h1>
        <span style={{ fontSize: 11, background: 'var(--accent-light)', color: 'var(--accent)', padding: '2px 8px', borderRadius: 4, fontWeight: 600 }}>Read-only</span>
      </div>
      {data.cells.map(cell => (
        <div key={cell.position} style={{ marginBottom: 16 }}>
          {cell.type === 'text' ? (
            <div style={{ padding: '8px 0' }}><ReactMarkdown>{cell.source}</ReactMarkdown></div>
          ) : (
            <>
              <pre style={{ background: 'var(--bg-code)', padding: 12, borderRadius: 4, overflow: 'auto', fontSize: 13, color: 'var(--text-primary)' }}>{cell.source}</pre>
              {cell.outputs?.map((out, i) => (
                <div key={i} style={{ marginTop: 8 }}>
                  {out.chart_config ? (
                    <ChartView config={out.chart_config} />
                  ) : out.table ? (
                    <table style={{ borderCollapse: 'collapse', fontSize: 13 }}>{out.table.columns.map(c => <th key={c.name}>{c.name}</th>)}</table>
                  ) : null}
                </div>
              ))}
            </>
          )}
        </div>
      ))}
    </div>
  )
}
```

**Step 2: Add route in App.tsx**

```tsx
<Route path="/public/:token" element={<PublicNotebookPage />} />
```

Place it outside the `<ProtectedRoute>` wrapper, alongside the existing public dashboard route.

**Commit:**

```bash
git add web/src/pages/PublicNotebookPage.tsx web/src/App.tsx
git commit -m "feat: add public notebook page"
```

---

### Task 11: Frontend — Org sharing toggle

**Files:**
- Modify: `web/src/pages/OrgSettingsPage.tsx`

**Step 1: Add sharing toggle section**

Find the SSO settings section and add after it:

```tsx
<section style={sectionStyles.section}>
  <h3 style={sectionStyles.heading}>Public Sharing</h3>
  <p style={sectionStyles.desc}>Allow sharing notebooks and dashboards via public links.</p>
  <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', marginTop: 8 }}>
    <input type="checkbox" checked={sharingEnabled} onChange={e => handleToggleSharing(e.target.checked)} />
    <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>Enable public sharing</span>
  </label>
</section>
```

Add state and mutation:

```tsx
const [sharingEnabled, setSharingEnabled] = useState(true)

useEffect(() => {
  api.get<{ public_sharing_enabled: boolean }>('/api/v1/org/sharing')
    .then(r => setSharingEnabled(r.public_sharing_enabled))
    .catch(() => {})
}, [])

async function handleToggleSharing(enabled: boolean) {
  await api.put('/api/v1/org/sharing', { public_sharing_enabled: enabled })
  setSharingEnabled(enabled)
}
```

**Commit:**

```bash
git add web/src/pages/OrgSettingsPage.tsx
git commit -m "feat: add public sharing toggle to org settings"
```

---

### Task 12: Verify end-to-end

1. Restart the dev stack: `docker compose -f docker-compose.dev.yml restart api web`
2. Wait for rebuild
3. Create a notebook with some cells
4. Click "Share" in the toolbar → generates public link
5. Open the link in an incognito browser → see notebook with cells and outputs
6. Revoke the link → visit again → 404
7. Org admin disables sharing → existing links return 404, share button shows disabled state
