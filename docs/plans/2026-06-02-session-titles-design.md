# Session Titles Design

**Date:** 2026-06-02

## Problem

Agent sessions currently display the first user message as a title, which results in long, unwieldy, and often unhelpful labels in the session history sidebar.

## Solution

Add LLM-generated session titles with user-editable override capability.

## Database

- Add `title TEXT` column to `agent_sessions` table (nullable)
- New migration: `047_session_titles.sql`

## Backend Changes

### Models (`internal/models/agent.go`)
- Add `Title *string` field to `AgentSession` struct

### Session Store (`internal/agent/session.go`)
- Add `UpdateTitle(ctx, sessionID, title string) error` method
- Update `GetSession` and `ListSessions` queries to include `title`

### API Handlers (`internal/api/agent_handlers.go`)
- `handleCreateSession`: Accept optional `title` in request body
- `handleUpdateSessionTitle`: New handler (`PATCH /api/v1/sessions/{id}/title`) accepting `{ title: string }`
- `handleListSessions`: Return `title` field; if null, compute fallback from first message

### Engine (`internal/agent/engine.go`)
- Add `generateSessionTitle(ctx, sessionID string) error` method
- Trigger after 2nd user message (or 3rd total message)
- Prompt: "Generate a concise title (max 50 chars) for this conversation about: [first 2-3 messages]"
- Single non-streaming LLM call, result stored directly to DB
- If generation fails or returns empty, leave title as null (UI falls back to first message)

### Router (`internal/api/router.go`)
- Add route: `PATCH /api/v1/sessions/{session_id}/title` → `handleUpdateSessionTitle`

## Frontend Changes

### Types (`web/src/types/agent.ts`)
- Add `title: string | null` to `AgentSession` type

### SessionHistory (`web/src/components/SessionHistory.tsx`)
- Display `session.title` when available
- Fallback to truncated first message when title is null/empty
- Add inline editing: click title to edit (pencil icon on hover)
- Optimistic UI updates on rename, rollback on error

### AgentPanel (`web/src/components/AgentPanel.tsx`)
- Show current session title in header area
- Fallback to agent name when no title exists

## Edge Cases

- **Existing sessions**: `title` remains null, UI shows fallback
- **Empty title after generation**: fallback to truncated first message
- **User clears title**: store as null, show fallback
- **LLM generation failure**: silent failure, title remains null

## Migration

```sql
ALTER TABLE agent_sessions ADD COLUMN title TEXT;
```

## API Contract

### PATCH /api/v1/sessions/{session_id}/title

**Request:**
```json
{ "title": "My Custom Title" }
```

**Response:** `204 No Content`

**Auth:** JWT + session ownership check
