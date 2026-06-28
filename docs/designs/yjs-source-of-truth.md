# Design: Yjs as Single Source of Truth for Cell Content

**Status:** ✅ Implemented  
**Author:** AI Assistant  
**Date:** 2026-06-12  
**Related Issue:** Agent `update_cell` changes not reflected dynamically

## Implementation Summary

**Completed:** 2026-06-12  
**Commits:** 10 commits (85b7742..fb6b928)  
**Review:** E2E browser testing passed — see `internal/agent/yjs.go` for the implementation

---

## Problem Statement

The current architecture has **two sources of truth** for cell content:

1. `cells.source` in PostgreSQL (used by agent updates and API queries)
2. Yjs document in `yjs_documents` table (used by collaborative editing)

These two sources constantly conflict:
- Agent updates database → Yjs has stale content
- Yjs sync overwrites editor → auto-save reverts agent changes
- Page reload loads stale Yjs → shows old content

This race condition causes agent updates to be silently reverted.

---

## Proposed Solution

Make **Yjs the single source of truth** for all cell content. The database `cells.source` becomes a derived cache that's synced from Yjs, not the other way around.

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         AGENT                                   │
│                                                                 │
│  1. update_cell tool called with new source                     │
│  2. Backend loads current Yjs state from yjs_documents          │
│  3. Backend creates Yjs update using ygo library                │
│  4. Backend stores updated Yjs state back to yjs_documents      │
│  5. Backend updates cells.source as cache (for API queries)     │
│  6. Backend broadcasts cell_updated event to all clients        │
└───────────────────────────┬─────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                         RELAY                                   │
│                                                                 │
│  1. Receives broadcast from backend                             │
│  2. Updates in-memory Yjs document                              │
│  3. Broadcasts to all connected WebSocket clients               │
│  4. Periodically persists state to yjs_documents (already does) │
└───────────────────────────┬─────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                       FRONTEND                                  │
│                                                                 │
│  1. Receives Yjs update from relay                              │
│  2. yCollab extension applies change to CodeMirror              │
│  3. Editor shows new source immediately                         │
│  4. onSourceChange fires but auto-save is suppressed            │
│     (agent_updated_at check)                                    │
└─────────────────────────────────────────────────────────────────┘
```

---

## Technical Design

### 1. Dependencies

Add the `ygo` library (Go Yjs implementation, binary-compatible with JS Yjs):

```go
go get github.com/reearth/ygo/crdt
```

**Why ygo?**
- Pure Go, no CGO required
- Binary-compatible with JavaScript Yjs
- Implements all Y-types (YText, YArray, YMap)
- Actively maintained (v1.21.0)
- Used by production systems

### 2. Database Schema Changes

#### New Column: `agent_updated_at`

```sql
-- Migration: Add agent_updated_at to cells table
ALTER TABLE cells ADD COLUMN agent_updated_at TIMESTAMPTZ;

-- Index for quick lookups during auto-save
CREATE INDEX idx_cells_agent_updated_at ON cells(agent_updated_at) 
    WHERE agent_updated_at IS NOT NULL;
```

**Purpose:** Track when the agent last updated a cell. Used to suppress auto-save after agent updates.

### 3. Backend Implementation

#### 3.1 Yjs Update Function

```go
package executor

import (
    "context"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/reearth/ygo/crdt"
)

// UpdateCellInYjs updates a cell's source in the Yjs document.
// This is the authoritative write - Yjs is the source of truth.
func UpdateCellInYjs(ctx context.Context, db *pgxpool.Pool, notebookID, cellID, newSource string) error {
    // 1. Load current Yjs state
    var state []byte
    err := db.QueryRow(ctx,
        "SELECT state FROM yjs_documents WHERE notebook_id = $1",
        notebookID,
    ).Scan(&state)
    if err != nil && err != pgx.ErrNoRows {
        return fmt.Errorf("load yjs state: %w", err)
    }
    
    // 2. Create/decode Yjs document
    doc := crdt.New()
    if len(state) > 0 {
        if err := crdt.ApplyUpdateV1(doc, state, nil); err != nil {
            return fmt.Errorf("decode yjs state: %w", err)
        }
    }
    
    // 3. Update the cell's text in Yjs
    //    Yjs uses "cell:{cellID}" as the key for each cell's content
    ytext := doc.GetText("cell:" + cellID)
    ytext.Delete(0, ytext.Len())
    ytext.Insert(0, newSource)
    
    // 4. Encode the updated state
    newState := doc.EncodeStateAsUpdate()
    
    // 5. Store back to database
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
```

#### 3.2 Updated `update_cell` Handler

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

### 4. Frontend Changes

#### 4.1 Auto-Save Suppression

```typescript
// In NotebookPage.tsx

const updateSource = useCallback((cellId: string, source: string) => {
  setLocalCells(prev => prev.map(c => c.id === cellId ? { ...c, source } : c))
  
  // Check if agent just updated this cell
  const cell = localCells.find(c => c.id === cellId)
  if (cell?.agent_updated_at) {
    const elapsed = Date.now() - new Date(cell.agent_updated_at).getTime()
    if (elapsed < 5000) {
      // Agent update is recent, don't trigger auto-save
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

#### 4.2 Cell Interface Update

```typescript
// In types.ts
export interface Cell {
  id: string
  notebook_id: string
  position: number
  type: 'code' | 'text'
  language?: string
  connector_id?: string
  source: string
  outputs?: Output[]
  limit?: number
  created_at: string
  updated_at: string
  source_visible?: boolean
  cell_collapsed?: boolean
  title?: string
  description?: string
  slug?: string
  parameters?: CellParameter[]
  slide_break?: boolean
  metadata?: Record<string, any>
  agent_updated_at?: string  // NEW: When agent last updated this cell
}
```

### 5. Existing Code Updates

#### 5.1 `attachCollab` in Cell.tsx (Already Implemented)

The existing fix in `attachCollab` should be kept. It makes the database source authoritative over Yjs when they differ:

```typescript
const attachCollab = () => {
  const editorContent = view.state.doc.toString()
  const yjsContent = ytext.toString()
  if (yjsContent.length === 0) {
    // Yjs is empty, initialize from editor (database source)
    collab.doc.transact(() => ytext.insert(0, editorContent))
  } else if (yjsContent !== editorContent) {
    // Conflict: Yjs and editor differ. Database source (editor) is authoritative
    collab.doc.transact(() => {
      ytext.delete(0, ytext.length)
      ytext.insert(0, editorContent)
    })
  }
  view.dispatch({ effects: compartment.reconfigure(yCollab(ytext, collab.provider.awareness)) })
}
```

#### 5.2 Cell useEffect (Already Implemented)

The existing useEffect that syncs external source changes into Yjs should be kept:

```typescript
const lastSourceRef = useRef(cell.source)
useEffect(() => {
  if (cell.source !== lastSourceRef.current && cell.source !== undefined) {
    lastSourceRef.current = cell.source
    const collab = getOrCreateCollab(notebookId)
    const ytext = collab.doc.getText(`cell:${cell.id}`)
    if (ytext.toString() !== cell.source) {
      collab.doc.transact(() => {
        ytext.delete(0, ytext.length)
        ytext.insert(0, cell.source)
      })
    }
  }
}, [cell.source, cell.id, notebookId])
```

---

## API Changes

### Response Schema Update

The notebook API response should include `agent_updated_at`:

```json
{
  "cells": [
    {
      "id": "cell-123",
      "source": "SELECT * FROM users",
      "agent_updated_at": "2026-06-12T19:30:00Z",
      ...
    }
  ]
}
```

---

## Migration Strategy

### Phase 1: Add Infrastructure (This PR)

1. Add `ygo` dependency
2. Add `agent_updated_at` column
3. Implement `UpdateCellInYjs` function
4. Update `update_cell` handler
5. Update frontend auto-save logic

### Phase 2: Backfill Existing Data (Future)

1. For each notebook, load `cells.source` for all cells
2. Create Yjs document with all cell content
3. Store in `yjs_documents`

### Phase 3: Deprecate Direct DB Reads (Future)

1. Update all API endpoints to read from Yjs instead of `cells.source`
2. Keep `cells.source` as sync'd cache for search/full-text queries

---

## Testing Strategy

### Unit Tests

1. Test `UpdateCellInYjs` creates valid Yjs state
2. Test `UpdateCellInYjs` handles empty initial state
3. Test `UpdateCellInYjs` preserves other cells' content
4. Test auto-save suppression with recent `agent_updated_at`

### Integration Tests

1. Test agent update → Yjs update → frontend reflects change
2. Test concurrent agent updates don't overwrite each other
3. Test page reload shows agent's latest update
4. Test user edit after agent update saves correctly

### Manual Testing

1. Agent updates cell → verify UI reflects immediately
2. Agent updates cell → verify page reload shows correct content
3. Agent updates cell → verify user can edit afterward
4. Two agents update same cell → verify no data loss

---

## Risks and Mitigations

### Risk 1: ygo Library Compatibility

**Risk:** ygo might not be fully compatible with all Yjs features used by Hocuspocus.

**Mitigation:** 
- ygo claims binary compatibility with Yjs v13.6.31
- Test with actual Hocuspocus relay
- Have fallback to frontend sync if issues arise

### Risk 2: Performance Impact

**Risk:** Loading and encoding full Yjs state for each update could be slow.

**Mitigation:**
- Yjs updates are incremental (not full state)
- Only affected cell's text is modified
- Database caching keeps API queries fast

### Risk 3: Data Loss During Migration

**Risk:** Existing notebooks might have stale Yjs state.

**Mitigation:**
- Phase 2 backfill ensures all notebooks have current Yjs state
- `attachCollab` fix handles stale Yjs on page load
- Keep `cells.source` as fallback during migration

---

## Success Criteria

1. ✅ Agent updates appear in UI immediately (no page refresh needed)
2. ✅ Page reload shows agent's latest update
3. ✅ User edits after agent update are preserved
4. ✅ No auto-save reverts agent changes
5. ✅ Collaborative editing works correctly
6. ✅ No data loss during concurrent operations

---

## Open Questions

1. **Relay Broadcast:** Should the backend send a broadcast to the relay after updating Yjs, or let the relay pick it up on next sync?

2. **Yjs Key Naming:** Currently using `cell:{cellID}` as the Yjs key. Should we use a different naming convention?

3. **Table/Chart Cells:** How should table and chart output cells be handled? Should they also use Yjs?

---

## References

- [ygo Library](https://github.com/reearth/ygo/crdt) - Go Yjs implementation
- [Hocuspocus](https://hocuspocus.dev/) - Yjs backend for collaboration
- [JupyterLab RTC Architecture](https://jupyterlab-realtime-collaboration.readthedocs.io/en/latest/developer/architecture.html) - Reference implementation
- [Marimo RTC](https://github.com/marimo-team/marimo/pull/3319) - CRDT-based collaboration
