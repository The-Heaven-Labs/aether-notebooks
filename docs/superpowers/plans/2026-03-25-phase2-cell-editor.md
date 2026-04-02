# Phase 2 — Cell Editor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Prerequisite:** Phase 1 complete (testing infrastructure, AppShell, notebook description migration).

**Goal:** Add cell collapse/hide, cell title/description/slug, markdown syntax highlighting with live preview, inline image paste, cell history with smart versioning, notebook snapshots, and JupyterLab-style keyboard shortcuts.

**Architecture:** New DB columns on `cells` (`source_visible`, `cell_collapsed`, `title`, `description`, `slug`) plus two new tables (`cell_versions`, `notebook_snapshots`). All persisted via the existing `PUT /cells/:id` endpoint (extended). Cell versioning logic lives in a new `cellVersioning` helper called from `handleUpdateCell`. Frontend gains a `CellHeader` component and a `HistoryPanel` component. `TextCell` is migrated to CodeMirror with a `ViewPlugin` for live markdown preview.

**Tech Stack:** Go, React 19, CodeMirror 6 (`@codemirror/lang-markdown` already in deps via existing `@codemirror/*` packages — needs explicit install), `react-diff-viewer-continued` for diff display.

---

## File Map

**Create:**
- `internal/database/migrations/003_cell_editor.sql`
- `internal/api/cell_history.go` — `upsertCellVersion()` helper + snapshot handlers
- `web/src/components/CellHeader.tsx` — title, description, slug, history button
- `web/src/components/HistoryPanel.tsx` — versions list + diff view
- `web/src/components/ShortcutsModal.tsx` — keyboard shortcuts cheat sheet
- `web/src/hooks/useNotebookKeyboardShortcuts.ts`
- `web/src/test/CellToolbar.test.tsx`
- `web/src/test/HistoryPanel.test.tsx`
- `e2e/cell-editor.spec.ts`
- `e2e/history.spec.ts`

**Modify:**
- `internal/models/notebook.go` — add `CellVersion`, `NotebookSnapshot` types
- `internal/api/cell_handlers.go` — add new fields to request/response, call `upsertCellVersion`
- `internal/api/notebook_handlers.go` — add snapshot routes wiring (routes in router.go)
- `internal/api/router.go` — add snapshot routes
- `web/src/components/CellToolbar.tsx` — add collapse/hide/history buttons
- `web/src/components/CodeCell.tsx` — add CellHeader, pass collapse/hide props
- `web/src/components/TextCell.tsx` — replace textarea with CodeMirror markdown editor
- `web/src/pages/NotebookPage.tsx` — wire keyboard shortcuts, snapshot button
- `web/src/types/index.ts` — add new fields to `Cell`, new types

---

## Task 1: Migration — Cell Editor Columns + History Tables

**Files:**
- Create: `internal/database/migrations/003_cell_editor.sql`

- [ ] **Step 1: Write the migration**

```sql
-- New display state columns on cells
ALTER TABLE cells ADD COLUMN IF NOT EXISTS source_visible BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE cells ADD COLUMN IF NOT EXISTS cell_collapsed BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE cells ADD COLUMN IF NOT EXISTS title VARCHAR(255);
ALTER TABLE cells ADD COLUMN IF NOT EXISTS description TEXT;
ALTER TABLE cells ADD COLUMN IF NOT EXISTS slug VARCHAR(100);

-- Slug uniqueness per notebook (only when set)
CREATE UNIQUE INDEX IF NOT EXISTS idx_cells_notebook_slug
  ON cells (notebook_id, slug)
  WHERE slug IS NOT NULL;

-- Per-cell version history
CREATE TABLE IF NOT EXISTS cell_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  cell_id UUID NOT NULL REFERENCES cells(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cell_versions_cell_created
  ON cell_versions (cell_id, created_at DESC);

-- Notebook-level snapshots
CREATE TABLE IF NOT EXISTS notebook_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  cell_sources JSONB NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notebook_snapshots_nb
  ON notebook_snapshots (notebook_id, created_at DESC);
```

- [ ] **Step 2: Commit migration**

```bash
git add internal/database/migrations/003_cell_editor.sql
git commit -m "feat(migration): add cell editor columns + cell_versions + notebook_snapshots tables"
```

---

## Task 2: Backend — Cell Versioning Logic

**Files:**
- Create: `internal/api/cell_history.go`
- Modify: `internal/api/cell_handlers.go`
- Modify: `internal/models/notebook.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add new model types in `internal/models/notebook.go`**

```go
type CellVersion struct {
	ID        string    `json:"id"`
	CellID    string    `json:"cell_id"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type NotebookSnapshot struct {
	ID          string            `json:"id"`
	NotebookID  string            `json:"notebook_id"`
	Name        string            `json:"name"`
	CellSources map[string]string `json:"cell_sources"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
}
```

Also extend `Cell` with new fields:
```go
type Cell struct {
	ID            string     `json:"id"`
	NotebookID    string     `json:"notebook_id"`
	Position      int        `json:"position"`
	Type          CellType   `json:"type"`
	Language      string     `json:"language,omitempty"`
	ConnectorID   string     `json:"connector_id,omitempty"`
	Source        string     `json:"source"`
	Outputs       []Output   `json:"outputs"`
	SourceVisible bool       `json:"source_visible"`
	CellCollapsed bool       `json:"cell_collapsed"`
	Title         string     `json:"title,omitempty"`
	Description   string     `json:"description,omitempty"`
	Slug          string     `json:"slug,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
```

- [ ] **Step 2: Write a failing test for cell versioning**

Add to `internal/api/cell_history_test.go` (new file):
```go
package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCellVersioning_FirstSaveCreatesVersion(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "ver1@example.com", "VerOrg1")
	nbID := createNotebook(t, srv, token, "VNB")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	// Update cell source
	body, _ := json.Marshal(map[string]string{"source": "SELECT 2"})
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update cell: %d %s", rec.Code, rec.Body.String())
	}

	// Fetch history
	req2 := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get versions: %d %s", rec2.Code, rec2.Body.String())
	}
	var versions []map[string]any
	json.NewDecoder(rec2.Body).Decode(&versions)
	if len(versions) == 0 {
		t.Fatal("expected at least one version")
	}
}

func TestCellVersioning_SmallEditMerges(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "ver2@example.com", "VerOrg2")
	nbID := createNotebook(t, srv, token, "VNB2")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	updateCellSource := func(source string) {
		body, _ := json.Marshal(map[string]string{"source": source})
		req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("update: %d", rec.Code)
		}
	}

	// Two saves with small diff (< 50 chars, < 60s) — should merge into 1 version
	updateCellSource("SELECT 1")
	updateCellSource("SELECT 2") // only 1 char diff

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var versions []map[string]any
	json.NewDecoder(rec.Body).Decode(&versions)
	if len(versions) != 1 {
		t.Fatalf("expected 1 merged version, got %d", len(versions))
	}
	if versions[0]["source"] != "SELECT 2" {
		t.Fatalf("expected merged source 'SELECT 2', got %v", versions[0]["source"])
	}
}

func TestCellVersioning_LargeDiffCreatesNewVersion(t *testing.T) {
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv, "ver3@example.com", "VerOrg3")
	nbID := createNotebook(t, srv, token, "VNB3")
	connID := createConnector(t, srv, token)
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	updateCellSource := func(source string) {
		body, _ := json.Marshal(map[string]string{"source": source})
		req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
	}

	// First save
	updateCellSource("SELECT 1")
	// Large diff: 50+ chars changed
	updateCellSource("SELECT id, name, created_at, updated_at FROM users WHERE active = true ORDER BY created_at DESC")

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var versions []map[string]any
	json.NewDecoder(rec.Body).Decode(&versions)
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions for large diff, got %d", len(versions))
	}
}

// levenshtein is duplicated here for the test to be self-contained
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 { return lb }
	if lb == 0 { return la }
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ { dp[0][j] = j }
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[la][lb]
}
func min3(a, b, c int) int {
	if a < b { if a < c { return a }; return c }
	if b < c { return b }; return c
}

var _ = strings.Contains // avoid unused import
```

- [ ] **Step 3: Run tests — expect FAIL**

```bash
task test:api 2>&1 | grep -E "TestCellVersioning|FAIL"
```

Expected: FAIL — handlers not yet updated.

- [ ] **Step 4: Create `internal/api/cell_history.go`**

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
)

const (
	versionMergeMaxDist = 50              // chars
	versionMergeWindow  = 60 * time.Second
)

// upsertCellVersion is called after each cell source save.
// It merges into the latest version if the edit is small and recent,
// otherwise inserts a new version row.
func (s *Server) upsertCellVersion(ctx context.Context, cellID, newSource string) error {
	var lastID string
	var lastSource string
	var lastCreatedAt time.Time

	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, source, created_at FROM cell_versions
		 WHERE cell_id = $1 ORDER BY created_at DESC LIMIT 1`,
		cellID,
	).Scan(&lastID, &lastSource, &lastCreatedAt)

	if err == pgx.ErrNoRows {
		// No versions yet — create first
		_, err = s.db.Pool.Exec(ctx,
			`INSERT INTO cell_versions (cell_id, source) VALUES ($1, $2)`,
			cellID, newSource,
		)
		return err
	}
	if err != nil {
		return err
	}

	dist := levenshteinDistance(lastSource, newSource)
	age := time.Since(lastCreatedAt)

	if dist < versionMergeMaxDist && age < versionMergeWindow {
		// Merge: update existing version in place
		_, err = s.db.Pool.Exec(ctx,
			`UPDATE cell_versions SET source = $1 WHERE id = $2`,
			newSource, lastID,
		)
		return err
	}

	// New version
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO cell_versions (cell_id, source) VALUES ($1, $2)`,
		cellID, newSource,
	)
	return err
}

func levenshteinDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				m := dp[i-1][j]
				if dp[i][j-1] < m {
					m = dp[i][j-1]
				}
				if dp[i-1][j-1] < m {
					m = dp[i-1][j-1]
				}
				dp[i][j] = 1 + m
			}
		}
	}
	return dp[la][lb]
}

// handleListCellVersions returns all versions for a cell, newest first.
func (s *Server) handleListCellVersions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	ctx := r.Context()

	var exists bool
	s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM cells c JOIN notebooks n ON n.id=c.notebook_id WHERE c.id=$1 AND c.notebook_id=$2 AND n.org_id=$3)`,
		cellID, nbID, claims.OrgID,
	).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, cell_id, source, created_at FROM cell_versions WHERE cell_id=$1 ORDER BY created_at DESC`,
		cellID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var versions []models.CellVersion
	for rows.Next() {
		var v models.CellVersion
		if err := rows.Scan(&v.ID, &v.CellID, &v.Source, &v.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		versions = append(versions, v)
	}
	if versions == nil {
		versions = []models.CellVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}

// handleRestoreCellVersion restores a cell to a previous version source.
func (s *Server) handleRestoreCellVersion(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	cellID := r.PathValue("cell_id")
	versionID := r.PathValue("version_id")
	ctx := r.Context()

	var source string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT cv.source FROM cell_versions cv
		 JOIN cells c ON c.id = cv.cell_id
		 JOIN notebooks n ON n.id = c.notebook_id
		 WHERE cv.id=$1 AND c.id=$2 AND c.notebook_id=$3 AND n.org_id=$4`,
		versionID, cellID, nbID, claims.OrgID,
	).Scan(&source)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Update the cell source
	var cell models.Cell
	var lang, connID *string
	var outputs []byte
	err = s.db.Pool.QueryRow(ctx,
		`UPDATE cells SET source=$1, updated_at=NOW()
		 WHERE id=$2 AND notebook_id=$3
		 RETURNING id, notebook_id, position, type, language, connector_id, source, outputs,
		           source_visible, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''),
		           created_at, updated_at`,
		source, cellID, nbID,
	).Scan(&cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
		&cell.Source, &outputs, &cell.SourceVisible, &cell.CellCollapsed,
		&cell.Title, &cell.Description, &cell.Slug,
		&cell.CreatedAt, &cell.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "restore failed")
		return
	}
	if lang != nil { cell.Language = *lang }
	if connID != nil { cell.ConnectorID = *connID }
	json.Unmarshal(outputs, &cell.Outputs)

	// Version the restored source
	s.upsertCellVersion(ctx, cellID, source)

	writeJSON(w, http.StatusOK, cell)
}

// Snapshot handlers

type createSnapshotRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")

	var req createSnapshotRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	ctx := r.Context()

	// Collect all cell sources
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, source FROM cells WHERE notebook_id=$1 ORDER BY position`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query cells failed")
		return
	}
	defer rows.Close()

	cellSources := map[string]string{}
	for rows.Next() {
		var id, src string
		rows.Scan(&id, &src)
		cellSources[id] = src
	}

	sourcesJSON, _ := json.Marshal(cellSources)

	var snap models.NotebookSnapshot
	var sourcesOut []byte
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO notebook_snapshots (notebook_id, name, cell_sources, created_by)
		 VALUES ($1,$2,$3,$4)
		 RETURNING id, notebook_id, name, cell_sources, created_by, created_at`,
		nbID, req.Name, sourcesJSON, claims.UserID,
	).Scan(&snap.ID, &snap.NotebookID, &snap.Name, &sourcesOut, &snap.CreatedBy, &snap.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create snapshot failed")
		return
	}
	json.Unmarshal(sourcesOut, &snap.CellSources)
	writeJSON(w, http.StatusCreated, snap)
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	ctx := r.Context()

	var exists bool
	s.db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notebooks WHERE id=$1 AND org_id=$2)`, nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, notebook_id, name, cell_sources, created_by, created_at
		 FROM notebook_snapshots WHERE notebook_id=$1 ORDER BY created_at DESC`,
		nbID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var snaps []models.NotebookSnapshot
	for rows.Next() {
		var snap models.NotebookSnapshot
		var sourcesOut []byte
		rows.Scan(&snap.ID, &snap.NotebookID, &snap.Name, &sourcesOut, &snap.CreatedBy, &snap.CreatedAt)
		json.Unmarshal(sourcesOut, &snap.CellSources)
		snaps = append(snaps, snap)
	}
	if snaps == nil {
		snaps = []models.NotebookSnapshot{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("id")
	snapID := r.PathValue("snapshot_id")
	ctx := r.Context()

	var sourcesJSON []byte
	err := s.db.Pool.QueryRow(ctx,
		`SELECT ns.cell_sources FROM notebook_snapshots ns
		 JOIN notebooks n ON n.id = ns.notebook_id
		 WHERE ns.id=$1 AND ns.notebook_id=$2 AND n.org_id=$3`,
		snapID, nbID, claims.OrgID,
	).Scan(&sourcesJSON)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var cellSources map[string]string
	json.Unmarshal(sourcesJSON, &cellSources)

	for cellID, src := range cellSources {
		s.db.Pool.Exec(ctx, `UPDATE cells SET source=$1, updated_at=NOW() WHERE id=$2 AND notebook_id=$3`, src, cellID, nbID)
		s.upsertCellVersion(ctx, cellID, src)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}
```

- [ ] **Step 5: Update `handleUpdateCell` to call `upsertCellVersion` and handle new fields**

In `internal/api/cell_handlers.go`, extend `updateCellRequest`:
```go
type updateCellRequest struct {
	Source        *string `json:"source,omitempty"`
	Language      *string `json:"language,omitempty"`
	ConnectorID   *string `json:"connector_id,omitempty"`
	Type          *string `json:"type,omitempty"`
	SourceVisible *bool   `json:"source_visible,omitempty"`
	CellCollapsed *bool   `json:"cell_collapsed,omitempty"`
	Title         *string `json:"title,omitempty"`
	Description   *string `json:"description,omitempty"`
	Slug          *string `json:"slug,omitempty"`
}
```

In the update query builder, add handling for each new field:
```go
if req.SourceVisible != nil {
    query += fmt.Sprintf(", source_visible = $%d", argN)
    args = append(args, *req.SourceVisible)
    argN++
}
if req.CellCollapsed != nil {
    query += fmt.Sprintf(", cell_collapsed = $%d", argN)
    args = append(args, *req.CellCollapsed)
    argN++
}
if req.Title != nil {
    query += fmt.Sprintf(", title = $%d", argN)
    args = append(args, *req.Title)
    argN++
}
if req.Description != nil {
    query += fmt.Sprintf(", description = $%d", argN)
    args = append(args, *req.Description)
    argN++
}
if req.Slug != nil {
    query += fmt.Sprintf(", slug = $%d", argN)
    args = append(args, nilIfEmptyStr(*req.Slug))
    argN++
}
```

Update RETURNING clause:
```go
query += " RETURNING id, notebook_id, position, type, language, connector_id, source, outputs, source_visible, cell_collapsed, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), created_at, updated_at"
```

Update Scan:
```go
err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
    &cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
    &cell.Source, &outputs, &cell.SourceVisible, &cell.CellCollapsed,
    &cell.Title, &cell.Description, &cell.Slug,
    &cell.CreatedAt, &cell.UpdatedAt,
)
```

After a successful source update, call versioning:
```go
if req.Source != nil {
    s.upsertCellVersion(ctx, cellID, *req.Source)
}
```

Add `nilIfEmptyStr` helper (or reuse existing `nilIfEmpty`):
```go
func nilIfEmptyStr(s string) interface{} {
    if s == "" { return nil }
    return s
}
```

Also update `handleCreateCell` RETURNING and Scan to include the new columns with defaults.

Also update `handleGetNotebook`'s cell scan to include the new columns.

- [ ] **Step 6: Register new routes in `internal/api/router.go`**

```go
// Cell history routes
s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions", authMW(http.HandlerFunc(s.handleListCellVersions)))
s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/cells/{cell_id}/versions/{version_id}/restore", authMW(RequireRole("editor")(http.HandlerFunc(s.handleRestoreCellVersion))))

// Snapshot routes
s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots", authMW(RequireRole("editor")(http.HandlerFunc(s.handleCreateSnapshot))))
s.mux.Handle("GET /api/v1/notebooks/{id}/snapshots", authMW(http.HandlerFunc(s.handleListSnapshots)))
s.mux.Handle("POST /api/v1/notebooks/{id}/snapshots/{snapshot_id}/restore", authMW(RequireRole("editor")(http.HandlerFunc(s.handleRestoreSnapshot))))
```

- [ ] **Step 7: Run tests — expect PASS**

```bash
task test:api 2>&1 | grep -E "TestCellVersioning|PASS|FAIL"
```

Expected: all 3 `TestCellVersioning_*` tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api/cell_history.go internal/api/cell_handlers.go internal/api/router.go internal/models/notebook.go internal/api/cell_history_test.go
git commit -m "feat: cell versioning logic (merge/create), snapshot endpoints, restore"
```

---

## Task 3: Frontend — CellToolbar + CellHeader

**Files:**
- Modify: `web/src/types/index.ts`
- Create: `web/src/components/CellHeader.tsx`
- Modify: `web/src/components/CellToolbar.tsx`
- Create: `web/src/test/CellToolbar.test.tsx`

- [ ] **Step 1: Update `Cell` type in `web/src/types/index.ts`**

```typescript
export interface Cell {
  id: string
  notebook_id: string
  type: 'code' | 'text'
  language: string
  source: string
  outputs: Output[]
  position: number
  connector_id?: string
  source_visible: boolean
  cell_collapsed: boolean
  title?: string
  description?: string
  slug?: string
  created_at: string
  updated_at: string
}

export interface CellVersion {
  id: string
  cell_id: string
  source: string
  created_at: string
}
```

- [ ] **Step 2: Write failing CellToolbar tests**

Create `web/src/test/CellToolbar.test.tsx`:
```tsx
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CellToolbar } from '../components/CellToolbar'

const baseProps = {
  onRun: vi.fn(),
  onDelete: vi.fn(),
  running: false,
  cellType: 'code' as const,
  sourceVisible: true,
  cellCollapsed: false,
  onToggleSourceVisible: vi.fn(),
  onToggleCellCollapsed: vi.fn(),
  onShowHistory: vi.fn(),
}

describe('CellToolbar', () => {
  it('calls onToggleSourceVisible with false when source is visible', () => {
    const onToggle = vi.fn()
    render(<CellToolbar {...baseProps} sourceVisible={true} onToggleSourceVisible={onToggle} />)
    fireEvent.click(screen.getByTitle('Hide source'))
    expect(onToggle).toHaveBeenCalledWith(false)
  })

  it('calls onToggleSourceVisible with true when source is hidden', () => {
    const onToggle = vi.fn()
    render(<CellToolbar {...baseProps} sourceVisible={false} onToggleSourceVisible={onToggle} />)
    fireEvent.click(screen.getByTitle('Show source'))
    expect(onToggle).toHaveBeenCalledWith(true)
  })

  it('calls onToggleCellCollapsed with true when cell is expanded', () => {
    const onToggle = vi.fn()
    render(<CellToolbar {...baseProps} cellCollapsed={false} onToggleCellCollapsed={onToggle} />)
    fireEvent.click(screen.getByTitle('Collapse cell'))
    expect(onToggle).toHaveBeenCalledWith(true)
  })

  it('calls onShowHistory', () => {
    const onHistory = vi.fn()
    render(<CellToolbar {...baseProps} onShowHistory={onHistory} />)
    fireEvent.click(screen.getByTitle('Cell history'))
    expect(onHistory).toHaveBeenCalled()
  })
})
```

- [ ] **Step 3: Run test — expect FAIL**

```bash
cd web && npm run test:run 2>&1 | grep -E "CellToolbar|FAIL"
```

Expected: FAIL

- [ ] **Step 4: Update `web/src/components/CellToolbar.tsx`**

Add new props and buttons:
```tsx
interface Props {
  onRun: () => void
  onDelete: () => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  running: boolean
  cellType: 'code' | 'text'
  connectors?: Connector[]
  connectorId?: string
  onAssignConnector?: (connectorId: string) => void
  sourceVisible: boolean
  cellCollapsed: boolean
  onToggleSourceVisible: (val: boolean) => void
  onToggleCellCollapsed: (val: boolean) => void
  onShowHistory: () => void
}
```

Add to the right-side buttons (before the delete button):
```tsx
<button
  style={styles.iconBtn}
  onClick={() => onToggleSourceVisible(!sourceVisible)}
  title={sourceVisible ? 'Hide source' : 'Show source'}
>
  {sourceVisible ? '⊟' : '⊞'}
</button>
<button
  style={styles.iconBtn}
  onClick={() => onToggleCellCollapsed(!cellCollapsed)}
  title={cellCollapsed ? 'Expand cell' : 'Collapse cell'}
>
  {cellCollapsed ? '▷' : '▽'}
</button>
<button
  style={styles.iconBtn}
  onClick={onShowHistory}
  title="Cell history"
>
  ⏱
</button>
```

- [ ] **Step 5: Run test — expect PASS**

```bash
cd web && npm run test:run 2>&1 | grep -E "CellToolbar|PASS|FAIL"
```

Expected: PASS

- [ ] **Step 6: Create `web/src/components/CellHeader.tsx`**

```tsx
import { useState } from 'react'
import type { Cell } from '../types'

interface Props {
  cell: Cell
  onUpdateCell: (updates: Partial<Pick<Cell, 'title' | 'description' | 'slug'>>) => void
  referencedByCount?: number
}

export function CellHeader({ cell, onUpdateCell, referencedByCount = 0 }: Props) {
  const [editingSlug, setEditingSlug] = useState(false)
  const [slugDraft, setSlugDraft] = useState(cell.slug ?? '')
  const [slugError, setSlugError] = useState('')

  const hasHeader = cell.title || cell.description || cell.slug

  if (!hasHeader && referencedByCount === 0) return null

  return (
    <div style={styles.header}>
      <div style={styles.titleRow}>
        {cell.title !== undefined && (
          <input
            style={styles.titleInput}
            value={cell.title}
            onChange={(e) => onUpdateCell({ title: e.target.value })}
            placeholder="Cell title…"
          />
        )}
        <div style={styles.slugArea}>
          {editingSlug ? (
            <input
              style={styles.slugInput}
              value={slugDraft}
              onChange={(e) => setSlugDraft(e.target.value.replace(/[^a-z0-9_]/g, '_'))}
              onBlur={() => {
                setEditingSlug(false)
                setSlugError('')
                onUpdateCell({ slug: slugDraft || undefined })
              }}
              onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur() }}
              autoFocus
            />
          ) : (
            <button
              style={styles.slugBadge}
              onClick={() => { setSlugDraft(cell.slug ?? ''); setEditingSlug(true) }}
              title="Click to edit cell slug (used in {{slug}} references)"
            >
              {cell.slug ? `{{${cell.slug}}}` : '+ slug'}
            </button>
          )}
          {slugError && <span style={styles.slugError}>{slugError}</span>}
          {referencedByCount > 0 && (
            <span style={styles.refBadge} title={`Referenced by ${referencedByCount} cell(s)`}>
              ↑{referencedByCount}
            </span>
          )}
        </div>
      </div>
      {cell.description !== undefined && (
        <input
          style={styles.descInput}
          value={cell.description}
          onChange={(e) => onUpdateCell({ description: e.target.value })}
          placeholder="Cell description…"
        />
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  header: { padding: '6px 16px 4px', borderBottom: '1px solid var(--border-light)', background: 'var(--bg-primary)' },
  titleRow: { display: 'flex', alignItems: 'center', gap: 8 },
  titleInput: { flex: 1, border: 'none', outline: 'none', fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', background: 'transparent', fontFamily: 'var(--font-sans)' },
  slugArea: { display: 'flex', alignItems: 'center', gap: 6 },
  slugBadge: { fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)', background: 'var(--bg-secondary)', border: '1px solid var(--border-light)', borderRadius: 4, padding: '2px 6px', cursor: 'pointer' },
  slugInput: { fontSize: 11, fontFamily: 'var(--font-mono)', padding: '2px 6px', border: '1px solid var(--accent)', borderRadius: 4, outline: 'none', width: 120 },
  slugError: { fontSize: 11, color: 'var(--error)' },
  refBadge: { fontSize: 10, color: 'var(--accent)', background: 'var(--accent-light)', borderRadius: 4, padding: '1px 5px', cursor: 'default' },
  descInput: { width: '100%', border: 'none', outline: 'none', fontSize: 12, color: 'var(--text-secondary)', background: 'transparent', fontFamily: 'var(--font-sans)', marginTop: 2 },
}
```

- [ ] **Step 7: Commit**

```bash
git add web/src/components/CellToolbar.tsx web/src/components/CellHeader.tsx web/src/types/index.ts web/src/test/CellToolbar.test.tsx
git commit -m "feat: cell collapse/hide/history toolbar buttons + CellHeader with title, description, slug"
```

---

## Task 4: Frontend — Markdown CodeMirror Editor with Live Preview

**Files:**
- Modify: `web/src/components/TextCell.tsx`

- [ ] **Step 1: Install markdown CodeMirror package**

```bash
cd web && npm install @codemirror/lang-markdown @codemirror/language-data
```

- [ ] **Step 2: Rewrite `TextCell.tsx` to use CodeMirror with live markdown preview**

```tsx
import { useEffect, useRef, useState } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, ViewPlugin, Decoration, DecorationSet, keymap, WidgetType } from '@codemirror/view'
import { defaultKeymap } from '@codemirror/commands'
import { markdown } from '@codemirror/lang-markdown'
import { languages } from '@codemirror/language-data'
import { syntaxHighlighting, defaultHighlightStyle } from '@codemirror/language'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import ReactDOM from 'react-dom/client'
import type { Cell } from '../types'
import { CellToolbar } from './CellToolbar'

// ── Live markdown preview: replace completed lines with rendered HTML ──

class MarkdownLineWidget extends WidgetType {
  constructor(readonly content: string) { super() }
  eq(other: MarkdownLineWidget) { return other.content === this.content }
  toDOM() {
    const div = document.createElement('div')
    div.className = 'cm-md-preview'
    div.style.cssText = 'padding:0 16px;font-size:14px;line-height:1.75;min-height:22px'
    const root = ReactDOM.createRoot(div)
    root.render(
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{this.content}</ReactMarkdown>
    )
    return div
  }
}

function buildMarkdownDecorations(view: EditorView): DecorationSet {
  const { state } = view
  const { head } = state.selection.main
  const activeLine = state.doc.lineAt(head).number
  const widgets: import('@codemirror/state').Range<Decoration>[] = []

  for (let i = 1; i <= state.doc.lines; i++) {
    if (i === activeLine) continue
    const line = state.doc.line(i)
    if (line.text.trim() === '') continue
    widgets.push(
      Decoration.replace({
        widget: new MarkdownLineWidget(line.text),
        inclusive: true,
      }).range(line.from, line.to)
    )
  }
  return Decoration.set(widgets, true)
}

const markdownPreviewPlugin = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet
    constructor(view: EditorView) { this.decorations = buildMarkdownDecorations(view) }
    update(update: import('@codemirror/view').ViewUpdate) {
      if (update.docChanged || update.selectionSet) {
        this.decorations = buildMarkdownDecorations(update.view)
      }
    }
  },
  { decorations: (v) => v.decorations }
)

// ── Image paste handler ──

const imagePasteExtension = EditorView.domEventHandlers({
  paste(event, view) {
    const items = Array.from(event.clipboardData?.items ?? [])
    const imageItem = items.find((i) => i.type.startsWith('image/'))
    if (!imageItem) return false
    event.preventDefault()
    const file = imageItem.getAsFile()
    if (!file) return false
    const reader = new FileReader()
    reader.onload = () => {
      const dataUrl = reader.result as string
      const md = `![pasted image](${dataUrl})`
      const { from } = view.state.selection.main
      view.dispatch({
        changes: { from, to: from, insert: md },
        selection: { anchor: from + md.length },
      })
    }
    reader.readAsDataURL(file)
    return true
  },
})

// ── TextCell component ──

interface SaveState {
  saving: boolean
  savedAt: Date | null
  error: string | null
}

function fmtTime(date: Date): string {
  const diffSec = Math.floor((Date.now() - date.getTime()) / 1000)
  if (diffSec < 5) return 'just now'
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

interface Props {
  cell: Cell
  onDelete: (cellId: string) => void
  onSourceChange: (cellId: string, source: string) => void
  onSave?: (cellId: string, source: string) => void
  onMoveUp?: () => void
  onMoveDown?: () => void
  onSwitchType?: () => void
  onUpdateCellMeta?: (updates: Partial<Pick<Cell, 'source_visible' | 'cell_collapsed' | 'title' | 'description' | 'slug'>>) => void
  onShowHistory?: () => void
  saveState?: SaveState
}

export function TextCell({ cell, onDelete, onSourceChange, onSave, onMoveUp, onMoveDown, onSwitchType, onUpdateCellMeta, onShowHistory, saveState }: Props) {
  const editorRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onSourceChangeRef = useRef(onSourceChange)
  onSourceChangeRef.current = onSourceChange

  useEffect(() => {
    if (!editorRef.current) return
    const view = new EditorView({
      state: EditorState.create({
        doc: cell.source,
        extensions: [
          keymap.of(defaultKeymap),
          markdown({ codeLanguages: languages }),
          syntaxHighlighting(defaultHighlightStyle),
          markdownPreviewPlugin,
          imagePasteExtension,
          EditorView.theme({
            '&': { fontFamily: 'var(--font-mono)', fontSize: '13px' },
            '.cm-content': { padding: '14px 16px', minHeight: '80px' },
            '.cm-line': { lineHeight: '1.65' },
            '.cm-focused': { outline: 'none' },
          }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onSourceChangeRef.current(cell.id, update.state.doc.toString())
            }
          }),
          EditorView.domEventHandlers({
            blur: (_, view) => {
              onSave?.(cell.id, view.state.doc.toString())
              return false
            },
          }),
        ],
      }),
      parent: editorRef.current,
    })
    viewRef.current = view
    return () => view.destroy()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cell.id])

  if (cell.cell_collapsed) {
    return (
      <div style={styles.collapsed}>
        <span style={styles.collapsedLabel}>{cell.title || 'Markdown cell'}</span>
        <button style={styles.expandBtn} onClick={() => onUpdateCellMeta?.({ cell_collapsed: false })}>▷ Expand</button>
      </div>
    )
  }

  return (
    <div style={styles.cell}>
      <CellToolbar
        cellType="text"
        onRun={() => {}}
        onDelete={() => onDelete(cell.id)}
        onMoveUp={onMoveUp}
        onMoveDown={onMoveDown}
        onSwitchType={onSwitchType}
        running={false}
        sourceVisible={cell.source_visible ?? true}
        cellCollapsed={cell.cell_collapsed ?? false}
        onToggleSourceVisible={(v) => onUpdateCellMeta?.({ source_visible: v })}
        onToggleCellCollapsed={(v) => onUpdateCellMeta?.({ cell_collapsed: v })}
        onShowHistory={() => onShowHistory?.()}
      />
      {(cell.source_visible ?? true) && <div ref={editorRef} />}
      {saveState && (
        <div style={styles.statusBar}>
          <span style={saveState.error ? styles.statusError : styles.statusSave}>
            {saveState.saving ? 'Saving…' : saveState.error ? `Save failed: ${saveState.error}` : saveState.savedAt ? `Saved ${fmtTime(saveState.savedAt)}` : ''}
          </span>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  cell: { border: '1px solid var(--border)', borderRadius: 10, background: 'white', overflow: 'hidden', boxShadow: 'var(--shadow-sm)' },
  collapsed: { border: '1px solid var(--border)', borderRadius: 10, background: 'var(--bg-secondary)', padding: '6px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' },
  collapsedLabel: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
  expandBtn: { fontSize: 12, background: 'transparent', border: 'none', color: 'var(--accent)', cursor: 'pointer' },
  statusBar: { padding: '4px 16px', fontSize: 11, minHeight: 24, background: '#faf9f7', borderTop: '1px solid var(--border-light)', display: 'flex', alignItems: 'center' },
  statusSave: { color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' },
  statusError: { color: 'var(--error)', fontFamily: 'var(--font-mono)' },
}
```

- [ ] **Step 3: Verify build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/TextCell.tsx
git commit -m "feat: migrate TextCell to CodeMirror with live markdown preview and image paste"
```

---

## Task 5: Frontend — History Panel

**Files:**
- Create: `web/src/components/HistoryPanel.tsx`
- Create: `web/src/test/HistoryPanel.test.tsx`

- [ ] **Step 1: Write failing HistoryPanel test**

Create `web/src/test/HistoryPanel.test.tsx`:
```tsx
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { HistoryPanel } from '../components/HistoryPanel'
import type { CellVersion } from '../types'

const versions: CellVersion[] = [
  { id: 'v1', cell_id: 'c1', source: 'SELECT 2', created_at: '2026-01-02T00:00:00Z' },
  { id: 'v2', cell_id: 'c1', source: 'SELECT 1', created_at: '2026-01-01T00:00:00Z' },
]

describe('HistoryPanel', () => {
  it('renders version list', () => {
    render(<HistoryPanel versions={versions} onRestore={vi.fn()} onClose={vi.fn()} currentSource="SELECT 2" />)
    expect(screen.getAllByRole('button', { name: /restore/i })).toHaveLength(2)
  })

  it('calls onRestore with version id', () => {
    const onRestore = vi.fn()
    render(<HistoryPanel versions={versions} onRestore={onRestore} onClose={vi.fn()} currentSource="SELECT 2" />)
    fireEvent.click(screen.getAllByRole('button', { name: /restore/i })[1])
    expect(onRestore).toHaveBeenCalledWith('v2')
  })

  it('calls onClose when close button clicked', () => {
    const onClose = vi.fn()
    render(<HistoryPanel versions={versions} onRestore={onClose} onClose={onClose} currentSource="SELECT 2" />)
    fireEvent.click(screen.getByTitle('Close history'))
    expect(onClose).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd web && npm run test:run 2>&1 | grep -E "HistoryPanel|FAIL"
```

Expected: FAIL

- [ ] **Step 3: Create `web/src/components/HistoryPanel.tsx`**

```tsx
import type { CellVersion } from '../types'

interface Props {
  versions: CellVersion[]
  currentSource: string
  onRestore: (versionId: string) => void
  onClose: () => void
}

export function HistoryPanel({ versions, currentSource, onRestore, onClose }: Props) {
  const fmt = (iso: string) => {
    const d = new Date(iso)
    return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  return (
    <div style={styles.panel}>
      <div style={styles.header}>
        <span style={styles.title}>Cell History</span>
        <button style={styles.closeBtn} onClick={onClose} title="Close history">✕</button>
      </div>
      {versions.length === 0 && <p style={styles.empty}>No history yet</p>}
      {versions.map((v, i) => {
        const isCurrent = v.source === currentSource && i === 0
        return (
          <div key={v.id} style={styles.item}>
            <div style={styles.itemHeader}>
              <span style={styles.ts}>{fmt(v.created_at)}</span>
              {isCurrent && <span style={styles.currentBadge}>current</span>}
            </div>
            <pre style={styles.preview}>{v.source.slice(0, 120)}{v.source.length > 120 ? '…' : ''}</pre>
            <button style={styles.restoreBtn} onClick={() => onRestore(v.id)}>Restore</button>
          </div>
        )
      })}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { width: 300, borderLeft: '1px solid var(--border)', background: 'white', display: 'flex', flexDirection: 'column', flexShrink: 0, overflowY: 'auto' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', borderBottom: '1px solid var(--border-light)', position: 'sticky', top: 0, background: 'white' },
  title: { fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  closeBtn: { background: 'transparent', border: 'none', cursor: 'pointer', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', textAlign: 'center' },
  item: { padding: '10px 14px', borderBottom: '1px solid var(--border-light)' },
  itemHeader: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 },
  ts: { fontSize: 11, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' },
  currentBadge: { fontSize: 10, background: 'var(--accent-light)', color: 'var(--accent)', borderRadius: 4, padding: '1px 5px', fontWeight: 600 },
  preview: { fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)', background: 'var(--bg-secondary)', padding: '6px 8px', borderRadius: 4, whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: '0 0 6px' },
  restoreBtn: { fontSize: 11, padding: '3px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--text-secondary)', fontWeight: 500 },
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd web && npm run test:run 2>&1 | grep -E "HistoryPanel|PASS|FAIL"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/components/HistoryPanel.tsx web/src/test/HistoryPanel.test.tsx
git commit -m "feat: HistoryPanel component for cell version history"
```

---

## Task 6: Frontend — Keyboard Shortcuts + Wire Everything into NotebookPage

**Files:**
- Create: `web/src/hooks/useNotebookKeyboardShortcuts.ts`
- Create: `web/src/components/ShortcutsModal.tsx`
- Modify: `web/src/pages/NotebookPage.tsx`
- Modify: `web/src/components/CodeCell.tsx`

- [ ] **Step 1: Create `web/src/hooks/useNotebookKeyboardShortcuts.ts`**

```typescript
import { useEffect, useRef } from 'react'

export interface ShortcutActions {
  runFocusedCell: () => void
  addCellBelow: () => void
  addCellAbove: () => void
  deleteFocusedCell: () => void
  moveFocusDown: () => void
  moveFocusUp: () => void
  convertToMarkdown: () => void
  convertToCode: () => void
  openShortcutsModal: () => void
}

export function useNotebookKeyboardShortcuts(
  actions: ShortcutActions,
  isEditingCell: boolean
) {
  const lastDRef = useRef<number>(0)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Never fire when user is typing in an input, textarea, or contenteditable
      const tag = (e.target as HTMLElement).tagName
      if (isEditingCell || tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return

      if (e.shiftKey && e.key === 'Enter') { e.preventDefault(); actions.runFocusedCell(); return }
      if (e.key === 'b' || e.key === 'B') { actions.addCellBelow(); return }
      if (e.key === 'a' || e.key === 'A') { actions.addCellAbove(); return }
      if (e.key === 'd' || e.key === 'D') {
        const now = Date.now()
        if (now - lastDRef.current < 500) { actions.deleteFocusedCell(); lastDRef.current = 0 }
        else { lastDRef.current = now }
        return
      }
      if (e.key === 'j' || e.key === 'ArrowDown') { actions.moveFocusDown(); return }
      if (e.key === 'k' || e.key === 'ArrowUp') { actions.moveFocusUp(); return }
      if (e.key === 'm' || e.key === 'M') { actions.convertToMarkdown(); return }
      if (e.key === 'y' || e.key === 'Y') { actions.convertToCode(); return }
      if (e.key === '?') { actions.openShortcutsModal(); return }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [actions, isEditingCell])
}
```

- [ ] **Step 2: Create `web/src/components/ShortcutsModal.tsx`**

```tsx
interface Props { onClose: () => void }

const SHORTCUTS = [
  { key: 'Shift+Enter', action: 'Run focused cell' },
  { key: 'B', action: 'Add cell below' },
  { key: 'A', action: 'Add cell above' },
  { key: 'D D', action: 'Delete cell' },
  { key: 'J / ↓', action: 'Move focus down' },
  { key: 'K / ↑', action: 'Move focus up' },
  { key: 'M', action: 'Convert to markdown' },
  { key: 'Y', action: 'Convert to code' },
  { key: '?', action: 'Show this modal' },
  { key: 'Ctrl+Enter (in editor)', action: 'Run cell' },
  { key: 'Ctrl+Shift+F (in SQL editor)', action: 'Format SQL' },
  { key: 'Escape (in editor)', action: 'Exit cell edit mode' },
]

export function ShortcutsModal({ onClose }: Props) {
  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>Keyboard Shortcuts</span>
          <button style={styles.close} onClick={onClose}>✕</button>
        </div>
        <table style={styles.table}>
          <tbody>
            {SHORTCUTS.map(({ key, action }) => (
              <tr key={key}>
                <td style={styles.key}><kbd style={styles.kbd}>{key}</kbd></td>
                <td style={styles.action}>{action}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 },
  modal: { background: 'white', borderRadius: 12, boxShadow: '0 8px 32px rgba(0,0,0,0.2)', minWidth: 400, maxHeight: '80vh', overflow: 'auto' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 20px', borderBottom: '1px solid var(--border-light)' },
  title: { fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'transparent', border: 'none', fontSize: 14, cursor: 'pointer', color: 'var(--text-muted)' },
  table: { width: '100%', borderCollapse: 'collapse', padding: '8px 20px' },
  key: { padding: '8px 20px 8px', width: 160 },
  kbd: { fontFamily: 'var(--font-mono)', fontSize: 12, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 6px' },
  action: { padding: '8px 20px 8px 0', fontSize: 13, color: 'var(--text-secondary)' },
}
```

- [ ] **Step 3: Wire history panel and keyboard shortcuts into `NotebookPage.tsx`**

Key additions to `NotebookPage.tsx`:

```tsx
import { useNotebookKeyboardShortcuts } from '../hooks/useNotebookKeyboardShortcuts'
import { HistoryPanel } from '../components/HistoryPanel'
import { ShortcutsModal } from '../components/ShortcutsModal'
import type { CellVersion } from '../types'

// New state
const [focusedCellId, setFocusedCellId] = useState<string | null>(null)
const [isEditingCell, setIsEditingCell] = useState(false)
const [historyCell, setHistoryCell] = useState<string | null>(null)
const [historyVersions, setHistoryVersions] = useState<CellVersion[]>([])
const [showShortcuts, setShowShortcuts] = useState(false)

// History fetch
const fetchHistory = async (cellId: string) => {
  const versions = await api.get<CellVersion[]>(`/api/v1/notebooks/${id}/cells/${cellId}/versions`)
  setHistoryVersions(versions)
  setHistoryCell(cellId)
}

const restoreVersion = async (cellId: string, versionId: string) => {
  const cell = await api.post<Cell>(`/api/v1/notebooks/${id}/cells/${cellId}/versions/${versionId}/restore`, {})
  setLocalCells(prev => prev.map(c => c.id === cell.id ? cell : c))
  setHistoryCell(null)
}

// Keyboard shortcuts
useNotebookKeyboardShortcuts(
  {
    runFocusedCell: () => { if (focusedCellId) handleRunCell(focusedCellId) },
    addCellBelow: () => createCell.mutate('code'),
    addCellAbove: () => createCell.mutate('code'),
    deleteFocusedCell: () => { if (focusedCellId) deleteCell.mutate(focusedCellId) },
    moveFocusDown: () => {
      if (!focusedCellId) return
      const idx = localCells.findIndex(c => c.id === focusedCellId)
      if (idx < localCells.length - 1) setFocusedCellId(localCells[idx + 1].id)
    },
    moveFocusUp: () => {
      if (!focusedCellId) return
      const idx = localCells.findIndex(c => c.id === focusedCellId)
      if (idx > 0) setFocusedCellId(localCells[idx - 1].id)
    },
    convertToMarkdown: () => {
      if (focusedCellId) updateCell.mutate({ cellId: focusedCellId, data: { type: 'text', language: 'markdown' } })
    },
    convertToCode: () => {
      if (focusedCellId) updateCell.mutate({ cellId: focusedCellId, data: { type: 'code', language: 'sql' } })
    },
    openShortcutsModal: () => setShowShortcuts(true),
  },
  isEditingCell
)
```

Pass `onShowHistory`, `onUpdateCellMeta`, `onFocus` props down to `CodeCell` and `TextCell`. Add snapshot button to the notebook toolbar.

Add at the end of the JSX return (before closing tag):
```tsx
{showShortcuts && <ShortcutsModal onClose={() => setShowShortcuts(false)} />}
{historyCell && (
  <div style={{ position: 'fixed', right: 0, top: 0, bottom: 0, zIndex: 200 }}>
    <HistoryPanel
      versions={historyVersions}
      currentSource={localCells.find(c => c.id === historyCell)?.source ?? ''}
      onRestore={(vId) => restoreVersion(historyCell, vId)}
      onClose={() => setHistoryCell(null)}
    />
  </div>
)}
```

- [ ] **Step 4: Update `CodeCell.tsx` to use new CellToolbar props and CellHeader**

Add imports:
```tsx
import { CellHeader } from './CellHeader'
```

Update `Props` to add:
```tsx
sourceVisible?: boolean
cellCollapsed?: boolean
onUpdateCellMeta?: (updates: object) => void
onShowHistory?: () => void
onFocus?: (cellId: string) => void
```

Wrap the cell with collapse logic and add `CellHeader` below the toolbar. Pass new props to `CellToolbar`.

- [ ] **Step 5: Verify build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: no errors.

- [ ] **Step 6: Run all tests**

```bash
task test && cd web && npm run test:run
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/hooks/useNotebookKeyboardShortcuts.ts web/src/components/ShortcutsModal.tsx web/src/pages/NotebookPage.tsx web/src/components/CodeCell.tsx
git commit -m "feat: keyboard shortcuts, history panel wired, collapse/hide in NotebookPage"
```

---

## Phase 2 Complete

```bash
task test        # All Go tests
task test:web    # Vitest
```

Run Phase 2 visual validation checklist from the spec before merging.
