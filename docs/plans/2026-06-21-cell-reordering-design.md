# Cell Reordering — Tool Improvements

**Date:** 2026-06-21
**Status:** Approved for implementation

## Problem

Agents struggle to reorder cells for three reasons:

1. **`move_cell` shift cascade is complex** — Moving a cell to a new position requires the LLM to understand cascading semantics and the backend to execute a fragile negative-intermediate SQL pattern.
2. **Position index inconsistency** — `create_cell` returns 0-indexed positions while `list_cells` returns 1-indexed, confusing the LLM and triggering hallucination.
3. **Tool descriptions are vague** — The LLM lacks clear guidance on cascade behavior and ID semantics.

## Changes

### 1. New `swap_cells` tool

Atomically swaps positions of two cells using a single SQL `UPDATE` with `CASE`:

```sql
UPDATE cells SET position = CASE id WHEN $1 THEN $3 WHEN $2 THEN $4 END
WHERE id IN ($1, $2)
```

- No cascading shifts needed
- No unique constraint violations (PostgreSQL checks constraint once per statement)
- Two consecutive swaps achieve any single-cell move (bubble-sort style)
- Works alongside the existing (now-fixed) `move_cell`

**Tool definition:**
```go
Name:        "swap_cells",
Description: "Swap the positions of two cells. Useful for reordering — two swaps can move any cell anywhere without needing to understand position cascading.",
Parameters:  {
  "cell_id_a": "UUID of the first cell",
  "cell_id_b": "UUID of the second cell"
}
```

### 2. Fix `create_cell` position return value

`makeCreateCellHandler` currently returns the raw 0-indexed DB position:

```go
return map[string]any{"cell_id": cellID, "position": position}, nil
```

Change to 1-indexed to match `list_cells`:

```go
return map[string]any{"cell_id": cellID, "position": position + 1}, nil
```

This also affects the `insert_cell` agent tool — same fix applies.

### 3. Tool description improvements

| Tool | Current | New |
|---|---|---|
| `move_cell` description | "Change a cell's position in the notebook" | "Move a cell to a new 1-based position. Cells between the old and new position shift by 1 to make room." |
| `move_cell` cell_id param | "The cell's UUID (from list_cells output, not the position number)" | "The cell's UUID (the id field from list_cells, not the position number)" |
| `insert_cell` position param | *(implicit)* | "Insert at this 1-based position. Existing cells at >= this position shift down by 1. If omitted or 0, appends to the end." |

## Implementation Order

1. Fix `create_cell` position return (one-line change)
2. Add `swap_cells` tool (~40 lines, follows existing tool pattern)
3. Update tool descriptions in registration code
4. Run existing tests, add test for `swap_cells`
