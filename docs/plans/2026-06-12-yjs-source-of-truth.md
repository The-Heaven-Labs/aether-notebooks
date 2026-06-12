# Yjs Source of Truth Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Yjs the single source of truth for cell content, so agent updates via `update_cell` are immediately reflected in the collaborative editor without being reverted by auto-save.

**Architecture:** The agent's `update_cell` tool will write changes directly to the Yjs document (stored in `yjs_documents` table) using the `ygo` Go library. The database `cells.source` becomes a derived cache. A new `agent_updated_at` column suppresses auto-save on the frontend after agent updates.

**Tech Stack:** Go (ygo library for Yjs), PostgreSQL, React/TypeScript, Yjs/Hocuspocus

---

## Task 1: Add `ygo` Dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add the dependency**

```bash
cd /home/jesus/Projects/hnb-claude
go get github.com/reearth/ygo@latest
```

**Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: Clean build with no errors.

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add ygo library for Go Yjs implementation"
```

---

## Task 2: Add `agent_updated_at` Migration

**Files:**
- Create: `internal/database/migrations/059_agent_updated_at.sql`

**Step 1: Write the migration**

```sql
-- Migration 059: Add agent_updated_at to cells table
-- Tracks when the agent last updated a cell's source, used to suppress
-- auto-save on the frontend after agent updates.

ALTER TABLE cells ADD COLUMN agent_updated_at TIMESTAMPTZ;

-- Partial index for quick lookups during auto-save suppression
CREATE INDEX idx_cells_agent_updated_at ON cells(agent_updated_at)
    WHERE agent_updated_at IS NOT NULL;
```

**Step 2: Verify migration runs**

```bash
task test 2>&1 | head -20
```

Expected: Tests pass (migration runs automatically on DB connect).

**Step 3: Commit**

```bash
git add internal/database/migrations/059_agent_updated_at.sql
git commit -m "migrations: add agent_updated_at column to cells table"
```

---

## Task 3: Update Cell Model with `agent_updated_at`

**Files:**
- Modify: `internal/models/notebook.go`

**Step 1: Add field to Cell struct**

In `internal/models/notebook.go`, add `AgentUpdatedAt` to the `Cell` struct:

```go
type Cell struct {
	ID              string          `json:"id"`
	NotebookID      string          `json:"notebook_id"`
	Position        int             `json:"position"`
	Type            CellType        `json:"type"`
	Language        string          `json:"language,omitempty"`
	ConnectorID     string          `json:"connector_id,omitempty"`
	Source          string          `json:"source"`
	Outputs         []Output        `json:"outputs"`
	SourceVisible   bool            `json:"source_visible"`
	CellCollapsed   bool            `json:"cell_collapsed"`
	SlideBreak      bool            `json:"slide_break"`
	Parameters      []Parameter     `json:"parameters"`
	Title           string          `json:"title,omitempty"`
	Description     string          `json:"description,omitempty"`
	Slug            string          `json:"slug,omitempty"`
	Limit           *int            `json:"limit,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	AgentUpdatedAt  *time.Time      `json:"agent_updated_at,omitempty"`
}
```

**Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: Clean build.

**Step 3: Commit**

```bash
git add internal/models/notebook.go
git commit -m "models: add AgentUpdatedAt field to Cell struct"
```

---

## Task 4: Create Yjs Update Function

**Files:**
- Create: `internal/agent/yjs.go`
- Create: `internal/agent/yjs_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/yjs_test.go
package agent_test

import (
	"context"
	"testing"

	"github.com/heavenlabs/hnb/internal/agent"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUpdateCellInYjs_CreatesValidState(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "sql", "SELECT 1")

	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cellID, "SELECT 42")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	// Verify state was stored
	var state []byte
	err = db.Pool.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1", nbID,
	).Scan(&state)
	if err != nil {
		t.Fatalf("no yjs state found: %v", err)
	}
	if len(state) == 0 {
		t.Fatal("yjs state is empty")
	}
}

func TestUpdateCellInYjs_PreservesOtherCells(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cell1 := createTestCell(t, db.Pool, nbID, "sql", "SELECT 1")
	cell2 := createTestCell(t, db.Pool, nbID, "sql", "SELECT 2")

	// Update cell1
	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cell1, "SELECT 100")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	// Update cell2
	err = agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cell2, "SELECT 200")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	// Verify cell1 content is preserved by decoding the Yjs state
	doc, err := agent.DecodeYjsState(db.Pool, nbID)
	if err != nil {
		t.Fatalf("DecodeYjsState failed: %v", err)
	}

	ytext1 := doc.GetText("cell:" + cell1)
	if ytext1.String() != "SELECT 100" {
		t.Errorf("cell1: expected 'SELECT 100', got '%s'", ytext1.String())
	}

	ytext2 := doc.GetText("cell:" + cell2)
	if ytext2.String() != "SELECT 200" {
		t.Errorf("cell2: expected 'SELECT 200', got '%s'", ytext2.String())
	}
}

func TestUpdateCellInYjs_HandlesEmptyInitialState(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "sql", "SELECT 1")

	// No yjs_documents row yet — function should create it
	err := agent.UpdateCellInYjs(context.Background(), db.Pool, nbID, cellID, "SELECT 99")
	if err != nil {
		t.Fatalf("UpdateCellInYjs failed: %v", err)
	}

	doc, err := agent.DecodeYjsState(db.Pool, nbID)
	if err != nil {
		t.Fatalf("DecodeYjsState failed: %v", err)
	}

	ytext := doc.GetText("cell:" + cellID)
	if ytext.String() != "SELECT 99" {
		t.Errorf("expected 'SELECT 99', got '%s'", ytext.String())
	}
}

// Helper: createTestCell inserts a cell and returns its ID
func createTestCell(t *testing.T, pool *pgxpool.Pool, nbID, lang, source string) string {
	t.Helper()
	cellID := "test-cell-" + source // deterministic for assertions
	_, err := pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
		VALUES ($1, $2, 'code', $3, $4, 0, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, cellID, nbID, lang, source)
	if err != nil {
		t.Fatalf("create cell: %v", err)
	}
	return cellID
}
```

**Step 2: Run test to verify it fails**

```bash
cd /home/jesus/Projects/hnb-claude
go test ./internal/agent/ -run TestUpdateCellInYjs -v 2>&1 | tail -20
```

Expected: FAIL with `undefined: agent.UpdateCellInYjs` or similar compilation error.

**Step 3: Write minimal implementation**

```go
// internal/agent/yjs.go
package agent

import (
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/reearth/ygo"
)

// UpdateCellInYjs updates a cell's source content in the Yjs document.
// Yjs is the source of truth; cells.source is a derived cache.
func UpdateCellInYjs(ctx context.Context, db *pgxpool.Pool, notebookID, cellID, newSource string) error {
	// 1. Load current Yjs state (may not exist yet)
	var state []byte
	err := db.QueryRow(ctx,
		"SELECT state FROM yjs_documents WHERE notebook_id = $1",
		notebookID,
	).Scan(&state)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("load yjs state: %w", err)
	}

	// 2. Create/decode Yjs document
	doc := ygo.NewDoc()
	if len(state) > 0 {
		if err := ygo.ApplyUpdate(doc, state); err != nil {
			return fmt.Errorf("decode yjs state: %w", err)
		}
	}

	// 3. Update the cell's text content in Yjs
	//    Key pattern: "cell:{cellID}" — matches frontend convention
	ytext := doc.GetText("cell:" + cellID)
	existing := ytext.String()
	if existing == newSource {
		// No change needed
		return nil
	}
	ytext.Delete(0, ytext.Len())
	ytext.Insert(0, newSource)

	// 4. Encode the updated state
	newState := ygo.EncodeStateAsUpdate(doc)

	// 5. Store back to database (upsert)
	_, err = db.Exec(ctx,
		`INSERT INTO yjs_documents (notebook_id, state, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (notebook_id) DO UPDATE SET state = $2, updated_at = NOW()`,
		notebookID, newState,
	)
	if err != nil {
		return fmt.Errorf("save yjs state: %w", err)
	}

	return nil
}

// DecodeYjsState loads and decodes a Yjs document from the database.
// Used in tests to verify Yjs state.
func DecodeYjsState(db *pgxpool.Pool, notebookID string) (*ygo.Doc, error) {
	var state []byte
	err := db.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1",
		notebookID,
	).Scan(&state)
	if err != nil {
		return nil, fmt.Errorf("load yjs state: %w", err)
	}

	doc := ygo.NewDoc()
	if len(state) > 0 {
		if err := ygo.ApplyUpdate(doc, state); err != nil {
			return nil, fmt.Errorf("decode yjs state: %w", err)
		}
	}
	return doc, nil
}
```

**Step 4: Run test to verify it passes**

```bash
cd /home/jesus/Projects/hnb-claude
go test ./internal/agent/ -run TestUpdateCellInYjs -v 2>&1 | tail -20
```

Expected: All 3 tests PASS.

**Step 5: Commit**

```bash
git add internal/agent/yjs.go internal/agent/yjs_test.go
git commit -m "agent: add Yjs update function for cell source truth"
```

---

## Task 5: Update Agent `update_cell` Handler

**Files:**
- Modify: `internal/agent/tools_notebook.go` (the `makeUpdateCellHandler` function, ~line 180-230)

**Step 1: Write the failing test**

Add to `internal/agent/tools_notebook_test.go`:

```go
func TestUpdateCell_WritesToYjs(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "sql", "SELECT 1")

	ctx := setupToolContext(t, db, orgID, userID, nbID)
	args, _ := json.Marshal(map[string]string{
		"cell_id": cellID,
		"source":  "SELECT 42",
	})

	handler := makeUpdateCellHandler(db.Pool)
	_, err := handler(args, ctx)
	if err != nil {
		t.Fatalf("update_cell failed: %v", err)
	}

	// Verify Yjs document has the new source
	doc, err := DecodeYjsState(db.Pool, nbID)
	if err != nil {
		t.Fatalf("DecodeYjsState failed: %v", err)
	}
	ytext := doc.GetText("cell:" + cellID)
	if ytext.String() != "SELECT 42" {
		t.Errorf("Yjs: expected 'SELECT 42', got '%s'", ytext.String())
	}

	// Verify DB cache also updated
	var source string
	db.Pool.QueryRow(context.Background(), "SELECT source FROM cells WHERE id = $1", cellID).Scan(&source)
	if source != "SELECT 42" {
		t.Errorf("DB: expected 'SELECT 42', got '%s'", source)
	}
}

func TestUpdateCell_SetsAgentUpdatedAt(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	cellID := createTestCell(t, db.Pool, nbID, "sql", "SELECT 1")

	ctx := setupToolContext(t, db, orgID, userID, nbID)
	args, _ := json.Marshal(map[string]string{
		"cell_id": cellID,
		"source":  "SELECT 99",
	})

	handler := makeUpdateCellHandler(db.Pool)
	_, err := handler(args, ctx)
	if err != nil {
		t.Fatalf("update_cell failed: %v", err)
	}

	// Verify agent_updated_at is set
	var agentUpdatedAt interface{}
	err = db.Pool.QueryRow(context.Background(),
		"SELECT agent_updated_at FROM cells WHERE id = $1", cellID,
	).Scan(&agentUpdatedAt)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if agentUpdatedAt == nil {
		t.Error("agent_updated_at should be set after agent update")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /home/jesus/Projects/hnb-claude
go test ./internal/agent/ -run TestUpdateCell_WritesToYjs -v 2>&1 | tail -20
```

Expected: FAIL — the handler doesn't write to Yjs yet.

**Step 3: Update the handler**

Replace the `makeUpdateCellHandler` function in `internal/agent/tools_notebook.go`:

```go
func makeUpdateCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID      string `json:"cell_id"`
			Source      string `json:"source"`
			Title       string `json:"title"`
			Description string `json:"description"`
			ConnectorID string `json:"connector_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		if err := ctx.CheckPermission("notebook", notebookID, "edit"); err != nil {
			return nil, err
		}

		// 1. Update Yjs document (source of truth)
		if req.Source != "" {
			if err := UpdateCellInYjs(ctx.Context, db, notebookID, req.CellID, req.Source); err != nil {
				return nil, fmt.Errorf("update yjs: %w", err)
			}
		}

		// 2. Update database cache (for API queries, search, exports)
		var connID *string
		if req.ConnectorID != "" {
			connID = &req.ConnectorID
		}
		_, err = db.Exec(ctx.Context, `
			UPDATE cells SET
				source = COALESCE(NULLIF($2, ''), source),
				title = COALESCE(NULLIF($3, ''), title),
				description = COALESCE(NULLIF($4, ''), description),
				connector_id = COALESCE($5, connector_id),
				agent_updated_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, req.CellID, req.Source, req.Title, req.Description, connID)
		if err != nil {
			return nil, fmt.Errorf("update cache: %w", err)
		}

		_ = ctx.AuditLog("cell.update", "cell", req.CellID)

		// 3. Notify connected clients
		ctx.EmitCellUpdated(req.CellID, req.Source)
		if ctx.BroadcastFunc != nil {
			ctx.BroadcastFunc(notebookID, map[string]any{
				"type":    "cell_updated",
				"cell_id": req.CellID,
				"source":  req.Source,
			})
		}

		return map[string]any{"cell_id": req.CellID}, nil
	}
}
```

**Step 4: Run test to verify it passes**

```bash
cd /home/jesus/Projects/hnb-claude
go test ./internal/agent/ -run 'TestUpdateCell_(WritesToYjs|SetsAgentUpdatedAt)' -v 2>&1 | tail -20
```

Expected: Both tests PASS.

**Step 5: Commit**

```bash
git add internal/agent/tools_notebook.go internal/agent/tools_notebook_test.go
git commit -m "agent: update_cell writes to Yjs first, sets agent_updated_at"
```

---

## Task 6: Update API `update_cell` Handler

**Files:**
- Modify: `internal/api/cell_handlers.go` (the `handleUpdateCell` function, ~line 161-295)

**Step 1: Write the failing test**

Add to `internal/api/cell_handlers_test.go`:

```go
func TestUpdateCellSource_WritesToYjs(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	email := fmt.Sprintf("yjs-update-%d@example.com", ts)
	token := registerAndGetToken(t, srv, email, "Yjs Org")
	nbID := createNotebook(t, srv, token, "Yjs NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", "")

	// Update cell source
	body, _ := json.Marshal(map[string]string{"source": "SELECT 99"})
	req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify Yjs document was updated
	db := srv.db // access the pool
	var state []byte
	err := db.Pool.QueryRow(context.Background(),
		"SELECT state FROM yjs_documents WHERE notebook_id = $1", nbID,
	).Scan(&state)
	if err != nil {
		t.Fatalf("no yjs state: %v", err)
	}
	if len(state) == 0 {
		t.Fatal("yjs state is empty")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd /home/jesus/Projects/hnb-claude
go test ./internal/api/ -run TestUpdateCellSource_WritesToYjs -v 2>&1 | tail -20
```

Expected: FAIL — the API handler doesn't write to Yjs yet.

**Step 3: Update the API handler**

In `internal/api/cell_handlers.go`, add the Yjs write inside `handleUpdateCell`. After the existing database update query, add:

```go
// Inside handleUpdateCell, after the existing UPDATE query succeeds:

// Write to Yjs (source of truth) if source changed
if req.Source != nil {
    if err := agent.UpdateCellInYjs(ctx, s.db.Pool, nbID, cellID, *req.Source); err != nil {
        // Log but don't fail — Yjs write is best-effort for API updates
        // (agent updates are the primary path)
        log.Printf("WARNING: yjs update failed for cell %s: %v", cellID, err)
    }
}
```

Add the import for the agent package at the top of the file:

```go
import (
    // ... existing imports ...
    "github.com/heavenlabs/hnb/internal/agent"
)
```

**Step 4: Run test to verify it passes**

```bash
cd /home/jesus/Projects/hnb-claude
go test ./internal/api/ -run TestUpdateCellSource_WritesToYjs -v 2>&1 | tail -20
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/cell_handlers.go internal/api/cell_handlers_test.go
git commit -m "api: update_cell writes to Yjs when source changes"
```

---

## Task 7: Update Notebook API Response to Include `agent_updated_at`

**Files:**
- Modify: `internal/api/cell_handlers.go` (the RETURNING clause in `handleUpdateCell`)
- Modify: `internal/api/cell_handlers.go` (notebook GET handler's cell query)

**Step 1: Add `agent_updated_at` to the RETURNING clause**

In `handleUpdateCell`, find the RETURNING clause (~line 280) and add `agent_updated_at`:

```go
query += " RETURNING id, notebook_id, position, type, language, connector_id, source, outputs, source_visible, cell_collapsed, slide_break, parameters, COALESCE(title,''), COALESCE(description,''), COALESCE(slug,''), \"limit\", COALESCE(metadata, '{}'), created_at, updated_at, agent_updated_at"
```

And add the scan for it:

```go
var agentUpdatedAt *time.Time
err := s.db.Pool.QueryRow(ctx, query, args...).Scan(
    &cell.ID, &cell.NotebookID, &cell.Position, &cell.Type, &lang, &connID,
    &cell.Source, &outputs, &cell.SourceVisible, &cell.CellCollapsed, &cell.SlideBreak, &cellParams,
    &cell.Title, &cell.Description, &cell.Slug, &limit, &cell.Metadata,
    &cell.CreatedAt, &cell.UpdatedAt, &agentUpdatedAt,
)
```

**Step 2: Update notebook GET to include `agent_updated_at` in cell queries**

Find all queries that SELECT from `cells` and add `agent_updated_at` to the column list. The main one is in the notebook GET handler. Search for `SELECT.*FROM cells` and add the column.

**Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: Clean build.

**Step 4: Commit**

```bash
git add internal/api/cell_handlers.go
git commit -m "api: include agent_updated_at in cell responses"
```

---

## Task 8: Update Frontend Types

**Files:**
- Modify: `web/src/types/index.ts`

**Step 1: Add `agent_updated_at` to Cell interface**

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
  created_at: string
  updated_at: string
  source_visible: boolean
  cell_collapsed: boolean
  slide_break?: boolean
  title?: string
  slug?: string
  parameters?: Parameter[]
  limit?: number | null
  metadata?: Record<string, unknown>
  agent_updated_at?: string  // When agent last updated this cell
  metrics?: {
    connect_time_ms: number
    query_time_ms: number
    render_time_ms: number
    total_time_ms: number
  }
}
```

**Step 2: Verify TypeScript compiles**

```bash
cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit
```

Expected: No type errors.

**Step 3: Commit**

```bash
git add web/src/types/index.ts
git commit -m "frontend: add agent_updated_at to Cell type"
```

---

## Task 9: Update Frontend Auto-Save to Suppress After Agent Update

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx` (the `updateSource` callback)

**Step 1: Update `updateSource` to check `agent_updated_at`**

In `NotebookPage.tsx`, find the `updateSource` callback and add the suppression check:

```typescript
const updateSource = useCallback((cellId: string, source: string) => {
  setLocalCells((prev) => {
    const cell = prev.find(c => c.id === cellId)
    // If source is the same as what we already have, skip (no save needed)
    if (cell && cell.source === source) return prev
    return prev.map((c) => (c.id === cellId ? { ...c, source } : c))
  })

  // Check if agent just updated this cell — suppress auto-save
  const cell = localCells.find(c => c.id === cellId)
  if (cell?.agent_updated_at) {
    const elapsed = Date.now() - new Date(cell.agent_updated_at).getTime()
    if (elapsed < 5000) {
      // Agent update is recent (< 5s), don't trigger auto-save
      // The agent already updated Yjs, no need to save again
      return
    }
  }

  // Normal auto-save flow
  clearTimeout(saveTimers.current[cellId])
  saveTimers.current[cellId] = setTimeout(() => {
    saveCellSource(cellId, source)
  }, 1500)
}, [saveCellSource, localCells])
```

**Step 2: Verify TypeScript compiles**

```bash
cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit
```

Expected: No type errors.

**Step 3: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "frontend: suppress auto-save after agent cell updates"
```

---

## Task 10: Run Full Test Suite

**Step 1: Run all tests**

```bash
cd /home/jesus/Projects/hnb-claude
task test 2>&1 | tail -30
```

Expected: All tests pass.

**Step 2: Run frontend build check**

```bash
cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit && npx vite build
```

Expected: Clean build.

**Step 3: Commit any fixes**

If there are issues, fix and commit.

---

## Task 11: Manual Verification

**Step 1: Start dev stack**

```bash
docker compose -f docker-compose.dev.yml up -d
```

**Step 2: Test agent update flow**

1. Open a notebook in the browser
2. Add a code cell with `SELECT 1`
3. Open the AI panel and ask the agent to update the cell source to `SELECT 42`
4. Verify the editor shows `SELECT 42` immediately (no page refresh)
5. Verify the "Saved" indicator doesn't show a spurious save

**Step 3: Test page reload**

1. After agent update, refresh the page
2. Verify the cell still shows `SELECT 42`
3. Verify you can edit the cell and auto-save works

**Step 4: Test concurrent edits**

1. Agent updates cell to `SELECT 100`
2. Immediately edit the cell in the editor
3. Verify no data loss

---

## Open Items (Future Work)

1. **Phase 2: Backfill** — Write a migration that loads `cells.source` for all notebooks and creates Yjs documents
2. **Phase 3: Deprecate direct reads** — Update all API endpoints to read from Yjs instead of `cells.source`
3. **Relay broadcast** — Decide if the backend should push to the relay or let it pick up on next sync
4. **Table/chart cells** — Handle non-text cell types in Yjs

---

## Summary

| Task | Description | Files Changed |
|------|-------------|---------------|
| 1 | Add ygo dependency | go.mod, go.sum |
| 2 | Migration: agent_updated_at | 059_agent_updated_at.sql |
| 3 | Update Cell model | models/notebook.go |
| 4 | Create Yjs update function | agent/yjs.go, agent/yjs_test.go |
| 5 | Update agent update_cell handler | agent/tools_notebook.go |
| 6 | Update API update_cell handler | api/cell_handlers.go |
| 7 | Include agent_updated_at in responses | api/cell_handlers.go |
| 8 | Update frontend types | types/index.ts |
| 9 | Suppress auto-save after agent update | NotebookPage.tsx |
| 10 | Full test suite | — |
| 11 | Manual verification | — |
