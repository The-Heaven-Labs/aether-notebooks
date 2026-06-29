# Cell Reordering — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make cell reordering reliable for agents by adding a `swap_cells` tool, fixing the `create_cell`/`list_cells` position inconsistency, and improving tool descriptions.

**Architecture:** Three changes to `internal/agent/tools_notebook.go`: (1) fix `create_cell` return to use 1-indexed positions matching `list_cells`, (2) add new `swap_cells` tool with single-`UPDATE` atomic swap, (3) improve `move_cell` and `insert_cell` descriptions. Add tests in `internal/agent/tools_notebook_test.go`.

**Tech Stack:** Go, pgx, PostgreSQL, agent-tool pattern

---

### Task 1: Fix `create_cell` position return value

**Files:**
- Modify: `internal/agent/tools_notebook.go:372`

**Step 1: Locate the return statement**

Read `internal/agent/tools_notebook.go` line 372:
```go
return map[string]any{"cell_id": cellID, "position": position}, nil
```

**Step 2: Change to 1-indexed**

```go
return map[string]any{"cell_id": cellID, "position": position + 1}, nil
```

**Step 3: Build and verify compile**

Run: `go build ./internal/agent/...` in project root.

**Step 4: Update test assertion**

The existing test `TestAgentCreateCellWithPosition` at `tools_notebook_test.go:509` asserts:
```go
if pos, ok := resultMap["position"].(int); !ok || pos != 1 {
    t.Fatalf("expected position 1, got %v", resultMap["position"])
}
```

If this test was expecting position and the test data creates one cell at position 1, the new return is position+1. Verify the test still passes.

Run: `go test ./internal/agent/... -run TestAgentCreateCellWithPosition -v`

**Step 5: Commit**

```bash
git add internal/agent/tools_notebook.go
git commit -m "fix: return 1-indexed position from create_cell tool"
```

---

### Task 2: Add `swap_cells` tool

**Files:**
- Modify: `internal/agent/tools_notebook.go` (tool registration + handler)
- Test: `internal/agent/tools_notebook_test.go`

**Step 1: Read the existing tool registration pattern**

Read lines 80-86 and 129-140 of `tools_notebook.go` to understand the `ToolDef` structure.

**Step 2: Register the tool**

After the `move_cell` registration block (ends around line 140), add:

```go
reg.Register(&ToolDef{
    Function: struct {
        Name        string `json:"name"`
        Description string `json:"description"`
        Parameters  any    `json:"parameters"`
    }{
        Name:        "swap_cells",
        Description: "Swap the positions of two cells. Useful for reordering — two swaps can move any cell anywhere without needing to understand position cascading.",
        Parameters:  `{"type":"object","properties":{"cell_id_a":{"type":"string","description":"UUID of the first cell"},"cell_id_b":{"type":"string","description":"UUID of the second cell"}},"required":["cell_id_a","cell_id_b"]}`,
    },
    Handler: makeSwapCellsHandler(db),
    ConfirmRequired: true,
})
```

**Step 3: Implement `makeSwapCellsHandler`**

Add the handler function (after `makeMoveCellHandler`):

```go
func makeSwapCellsHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            CellA string `json:"cell_id_a"`
            CellB string `json:"cell_id_b"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }
        if req.CellA == "" || req.CellB == "" {
            return nil, fmt.Errorf("cell_id_a and cell_id_b are required")
        }
        if req.CellA == req.CellB {
            return nil, fmt.Errorf("cannot swap a cell with itself")
        }

        notebookID, err := ctx.GetNotebookIDForCell(req.CellA)
        if err != nil {
            return nil, fmt.Errorf("get notebook for cell A: %w", err)
        }
        nbID2, err := ctx.GetNotebookIDForCell(req.CellB)
        if err != nil {
            return nil, fmt.Errorf("get notebook for cell B: %w", err)
        }
        if notebookID != nbID2 {
            return nil, fmt.Errorf("cells must be in the same notebook")
        }
        if err := ctx.CheckPermission("notebook", notebookID, "edit"); err != nil {
            return nil, err
        }

        tx, err := db.Begin(ctx.Context)
        if err != nil {
            return nil, fmt.Errorf("begin tx: %w", err)
        }
        defer tx.Rollback(ctx.Context)

        var posA, posB int
        if err := tx.QueryRow(ctx.Context, `SELECT position FROM cells WHERE id=$1`, req.CellA).Scan(&posA); err != nil {
            return nil, fmt.Errorf("get cell A position: %w", err)
        }
        if err := tx.QueryRow(ctx.Context, `SELECT position FROM cells WHERE id=$1`, req.CellB).Scan(&posB); err != nil {
            return nil, fmt.Errorf("get cell B position: %w", err)
        }

        if _, err := tx.Exec(ctx.Context,
            `UPDATE cells SET position = CASE WHEN id = $1 THEN $3 WHEN id = $2 THEN $4 END WHERE id IN ($1, $2)`,
            req.CellA, req.CellB, posB, posA); err != nil {
            return nil, fmt.Errorf("swap positions: %w", err)
        }

        if err := tx.Commit(ctx.Context); err != nil {
            return nil, fmt.Errorf("commit swap: %w", err)
        }

        _ = ctx.AuditLog("cell.swap", "cell", req.CellA)
        _ = ctx.AuditLog("cell.swap", "cell", req.CellB)

        return map[string]any{"cell_id_a": req.CellA, "cell_id_b": req.CellB, "status": "swapped"}, nil
    }
}
```

**Step 4: Write the test**

Add to `tools_notebook_test.go`:

```go
func TestAgentSwapCells(t *testing.T) {
    db := setupTestDB(t)
    orgID, userID := createTestOrgAndUser(t, db.Pool)
    nbID := createTestNotebook(t, db.Pool, orgID, userID)

    // Create 3 cells at positions 0, 1, 2
    now := time.Now()
    cellIDs := make([]string, 3)
    for i := 0; i < 3; i++ {
        cellIDs[i] = uuid.New().String()
        _, err := db.Pool.Exec(context.Background(), `
            INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
            VALUES ($1, $2, 'code', 'sql', 'SELECT 1', $3, $4, $4)
        `, cellIDs[i], nbID, i, now)
        if err != nil {
            t.Fatalf("create cell %d: %v", i, err)
        }
    }

    reg := agent.NewToolRegistry()
    agent.RegisterNotebookTools(reg, db.Pool)
    swapDef, ok := reg.Get("swap_cells")
    if !ok {
        t.Fatalf("swap_cells tool not found")
    }
    handler := swapDef.Handler
    ctx := setupToolContext(t, db, orgID, userID, nbID)

    // Swap cells at positions 0 and 2
    args, _ := json.Marshal(map[string]any{
        "cell_id_a": cellIDs[0],
        "cell_id_b": cellIDs[2],
    })
    _, err := handler(args, ctx)
    if err != nil {
        t.Fatalf("swap cells: %v", err)
    }

    // Verify positions: cell[0] should be at 2, cell[2] at 0
    var pos0, pos2 int
    db.Pool.QueryRow(context.Background(), `SELECT position FROM cells WHERE id=$1`, cellIDs[0]).Scan(&pos0)
    db.Pool.QueryRow(context.Background(), `SELECT position FROM cells WHERE id=$1`, cellIDs[2]).Scan(&pos2)
    if pos0 != 2 || pos2 != 0 {
        t.Fatalf("expected positions 2 and 0, got %d and %d", pos0, pos2)
    }

    // Verify no duplicates (should be 3 cells at 0, 1, 2)
    rows, _ := db.Pool.Query(context.Background(),
        `SELECT position FROM cells WHERE notebook_id = $1 ORDER BY position`, nbID)
    seen := map[int]bool{}
    for rows.Next() {
        var p int
        rows.Scan(&p)
        if seen[p] {
            t.Fatalf("duplicate position %d", p)
        }
        seen[p] = true
    }
    rows.Close()
    if len(seen) != 3 {
        t.Fatalf("expected 3 unique positions, got %d", len(seen))
    }
}
```

**Step 5: Build and run test**

Run: `go test ./internal/agent/... -run TestAgentSwapCells -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/agent/tools_notebook.go internal/agent/tools_notebook_test.go
git commit -m "feat: add swap_cells tool for atomic cell position swapping"
```

---

### Task 3: Improve tool descriptions

**Files:**
- Modify: `internal/agent/tools_notebook.go` (description strings only)

**Step 1: Update `move_cell` description (line ~135)**

Change:
```go
Description: "Change a cell's position in the notebook",
```
To:
```go
Description: "Move a cell to a new 1-based position. Cells between the old and new position shift by 1 to make room.",
```

Change `cell_id` parameter description:
```
"The cell's UUID (from list_cells output, not the position number)"
```
To:
```
"The cell's UUID (the id field from list_cells, not the position number)"
```

**Step 2: Update `insert_cell` position description (line ~304)**

Add clarity to the position parameter or the tool description:
```go
Description: "Add a new cell to a notebook. If position <= 0, appends to the end. If position > 0, inserts at that 1-based position, shifting existing cells down.",
```

**Step 3: Build and verify**

Run: `go build ./internal/agent/...`

**Step 4: Run all agent tests**

Run: `go test ./internal/agent/... -count=1`
Expected: All tests pass

**Step 5: Commit**

```bash
git add internal/agent/tools_notebook.go
git commit -m "docs: improve tool descriptions for move_cell and insert_cell"
```
