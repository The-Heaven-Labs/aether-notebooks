# Item 30: Subagent Spawning Fix

## Root Cause
The `spawn_subagents` tool handler in `tools_agent.go` created task records in the database with status `'queued'` but never launched the actual subagent execution. Tasks were persisted but never ran.

## Fix Applied

### 1. `internal/agent/subagent.go` — Added `RunQueuedTasks` and `runSubagentLoop`

- **`RunQueuedTasks(ctx, parentSessionID, taskIDs, masterKey, broadcastFn, notebookID)`**: New public method that takes already-created task IDs, fetches their goal/context from DB, and runs them in parallel (semaphore-limited to `MaxSubagentParallelism=3`). Updates status to `'running'` → `'completed'`/`'failed'` and broadcasts `subagent_status` events via `BroadcastFunc`.

- **`runSubagentLoop(ctx, parentSessionID, taskID, goal, userID, orgID, masterKey)`**: Extracted the LLM loop from the existing `runSubagent` into a separate method that does NOT insert the task record (since it already exists). Handles multi-turn tool calls just like the original.

### 2. `internal/agent/tools_agent.go` — Updated spawn handler

- Changed `RegisterAgentTools(reg, pool)` → `RegisterAgentTools(reg, pool, engine)` to give the spawn handler access to the Engine.
- `makeSpawnSubagentsHandler` now:
  1. Creates task records with `'queued'` status (unchanged)
  2. Copies `masterKey`, `sessionID`, `notebookID`, `broadcastFn`, and `taskIDs` for the goroutine
  3. Launches a goroutine that calls `engine.RunQueuedTasks()` with a fresh `context.Background()`
  4. Returns immediately with task IDs and `"spawned"` status

### 3. `internal/agent/engine.go` — Restructured `NewEngine`

- Engine struct is now created first, then tools are registered. This allows `RegisterAgentTools` to receive the engine reference needed for the spawn handler.

## Flow After Fix
```
Agent calls spawn_subagents tool
  → Handler creates DB records (status: 'queued')
  → Handler launches goroutine running RunQueuedTasks
  → Handler returns immediately with task_ids
  → Goroutine: for each task (parallel, max 3):
      → Updates status to 'running'
      → Broadcasts subagent_status event
      → Runs LLM loop (up to 20 turns)
      → Updates status to 'completed'/'failed'
      → Broadcasts final status event
  → Frontend receives events via WebSocket and shows progress
```

## Validation
- `go build ./...` — clean
- `go test ./internal/agent/...` — pass
- `go test ./internal/api/... -run Agent` — pass

## Files Changed
- `internal/agent/subagent.go` (+113 lines: RunQueuedTasks, runSubagentLoop)
- `internal/agent/tools_agent.go` (+30 lines: engine param, goroutine launch)
- `internal/agent/engine.go` (restructured NewEngine to create Engine before registering tools)
