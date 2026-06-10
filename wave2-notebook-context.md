# Item 31: Notebook Context Tool - Implementation Complete

## Summary
Successfully implemented `get_notebook_context` tool that provides agents with full notebook content including all cell sources, with appropriate safeguards for large notebooks.

## Changes Made

### 1. New Tool: `get_notebook_context` (internal/agent/tools_notebook.go)
- **Purpose**: Returns formatted notebook structure with cell sources for agent context
- **Parameters**:
  - `notebook_id` (required): Target notebook ID
  - `max_cells` (optional, default 50): Maximum cells to return
  - `include_outputs` (optional, default false): Include cell outputs (not yet implemented)
- **Safeguards**:
  - Hard limit: max 50 cells (prevents memory issues)
  - Source truncation: 2000 chars per cell (prevents token overflow)
  - Permission check: requires "view" permission on notebook
  - Cell count reporting: shows how many cells were truncated
- **Output format**: Human-readable markdown-style text with cell separators

### 2. Enhanced Notebook Context (internal/agent/engine.go)
- Modified `buildNotebookContext()` to auto-inject cell count summary
- When session starts with notebook, agent now sees:
  ```
  Cells: 12 total (code: 8)
  Use get_notebook_context tool to read full cell contents.
  ```
- Guides agent to use the new tool for detailed content

## Implementation Details

### Tool Registration Pattern
Followed existing pattern in `RegisterNotebookTools()`:
```go
reg.Register(&ToolDef{
    Function: struct { ... }{
        Name: "get_notebook_context",
        Description: "...",
        Parameters: "...",
    },
    Handler: makeGetNotebookContextHandler(db),
})
```

### Handler Implementation
- Separate handler function `makeGetNotebookContextHandler(db)` for testability
- Uses `strings.Builder` for efficient string concatenation
- Queries cells ordered by position
- Counts total cells to report truncation

### Context Enhancement
- Added two COUNT queries after connector info
- Only adds cell info if cells exist (cellCount > 0)
- Maintains existing connector and chart guidance

## Testing
- All existing agent tests pass (7/7)
- Code compiles without errors
- Tool follows same patterns as existing tools (read_cell, list_cells, etc.)

## Files Modified
1. `internal/agent/tools_notebook.go` (+67 lines)
   - Added `strings` import
   - Registered `get_notebook_context` tool
   - Implemented `makeGetNotebookContextHandler()`

2. `internal/agent/engine.go` (+8 lines)
   - Enhanced `buildNotebookContext()` with cell counts
   - Added guidance to use `get_notebook_context` tool

## Commit
```
7d012d6 feat: add get_notebook_context tool with safeguards (item 31)
```

## Next Steps
- Consider implementing `include_outputs` parameter (currently ignored)
- May want to add output truncation (first 10 rows) when implemented
- Monitor usage to tune the 50-cell and 2000-char limits
