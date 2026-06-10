# Wave 1 - Item 18: Configurable Max Tool Turns

**Status:** ✅ Complete
**Commit:** 4d5be1a

## Changes Made

### 1. Backend Model (`internal/models/agent.go`)
- Added `MaxTurns *int \`json:"max_turns,omitempty"\`` field to Agent struct

### 2. Engine (`internal/agent/engine.go`)
- Replaced `const maxTurns = 15` with configurable logic:
  ```go
  maxTurns := 90
  if agent.MaxTurns != nil && *agent.MaxTurns > 0 {
      maxTurns = *agent.MaxTurns
  }
  ```
- Updated SQL query in ProcessMessage to SELECT and SCAN `max_turns` column

### 3. Agent Handlers (`internal/api/agent_handlers.go`)
- Added `MaxTurns *int \`json:"max_turns"\`` to create request struct
- Added `MaxTurns *int \`json:"max_turns"\`` to update request struct
- Updated INSERT SQL to include `max_turns` column (parameter $10)
- Updated UPDATE SQL to include `max_turns = COALESCE($8, max_turns)`
- Updated list handler SQL to SELECT `max_turns`
- Updated get handler SQL to SELECT `max_turns`
- All Scan calls updated to include `&a.MaxTurns`

### 4. Database Migration (`internal/database/migrations/054_agents_max_turns.sql`)
```sql
ALTER TABLE agents ADD COLUMN IF NOT EXISTS max_turns INT DEFAULT NULL;
```

### 5. Frontend Types (`web/src/types/agent.ts`)
- Added `max_turns?: number` to Agent interface

### 6. Frontend AgentsPage (`web/src/pages/AgentsPage.tsx`)
- Added `max_turns: number` to AgentForm interface (default: 90)
- Added `max_turns: form.max_turns || undefined` to create API call
- Added `max_turns: form.max_turns || undefined` to update API call
- Added `max_turns: agent.max_turns ?? 90` to startEdit function
- Added "Max Tool Turns" number input field in AgentFormFields with:
  - Type: number
  - Min: 1, Max: 200
  - Helper text: "Default: 90, Max: 200"

## Validation
- ✅ Go code compiles (`go build ./...`)
- ✅ TypeScript compiles (`npx tsc --noEmit`)
- ✅ Go tests pass (`go test ./internal/api/... -run Agent`)
- ✅ Agent tests pass (`go test ./internal/agent/...`)

## Behavior
- When an agent has `max_turns` set, the engine uses that value
- When `max_turns` is NULL or 0, defaults to 90
- Previously hardcoded to 15, now 90 by default (6x increase)
- Frontend shows input field with range 1-200
