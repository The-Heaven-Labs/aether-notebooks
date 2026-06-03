# Session Titles Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add LLM-generated session titles with user-editable override capability to agent sessions.

**Architecture:** Add a nullable `title` column to `agent_sessions`. After the 2nd user message, trigger async LLM title generation. Add a PATCH endpoint for user edits. Update frontend to display titles with inline editing.

**Tech Stack:** Go (pgx, net/http), React/TypeScript, PostgreSQL

---

### Task 1: Database Migration

**Files:**
- Create: `internal/database/migrations/052_session_titles.sql`

**Step 1: Create migration file**

```sql
ALTER TABLE agent_sessions ADD COLUMN title TEXT;
```

**Step 2: Verify migration applies**

Run: `task test`
Expected: Tests pass (migration applies on server startup)

**Step 3: Commit**

```bash
git add internal/database/migrations/052_session_titles.sql
git commit -m "feat: add title column to agent_sessions"
```

---

### Task 2: Update Go Model

**Files:**
- Modify: `internal/models/agent.go:65-74`

**Step 1: Add Title field to AgentSession struct**

```go
type AgentSession struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	NotebookID string     `json:"notebook_id"`
	UserID     string     `json:"user_id"`
	MaxTurns   int        `json:"max_turns"`
	MaxTokens  int        `json:"max_tokens"`
	Title      *string    `json:"title,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}
```

**Step 2: Commit**

```bash
git add internal/models/agent.go
git commit -m "feat: add Title field to AgentSession model"
```

---

### Task 3: Update SessionStore

**Files:**
- Modify: `internal/agent/session.go`

**Step 1: Update CreateSession to accept optional title**

Modify signature and INSERT to include title:

```go
func (s *SessionStore) CreateSession(ctx context.Context, agentID, notebookID, userID string, maxTurns, maxTokens int, title *string) (*models.AgentSession, error) {
	session := &models.AgentSession{
		ID:         uuid.New().String(),
		AgentID:    agentID,
		NotebookID: notebookID,
		UserID:     userID,
		MaxTurns:   maxTurns,
		MaxTokens:  maxTokens,
		Title:      title,
		CreatedAt:  time.Now(),
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, max_tokens, title, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, session.ID, session.AgentID, session.NotebookID, session.UserID, session.MaxTurns, session.MaxTokens, session.Title, session.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return session, nil
}
```

**Step 2: Update GetSession to scan title**

```go
func (s *SessionStore) GetSession(ctx context.Context, sessionID string) (*models.AgentSession, error) {
	var session models.AgentSession
	var endedAt, title *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, title, created_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&session.ID, &session.AgentID, &session.NotebookID, &session.UserID, &session.MaxTurns, &session.MaxTokens, &endedAt, &title, &session.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	session.EndedAt = parseTime(endedAt)
	if title != nil {
		session.Title = title
	}
	return &session, nil
}
```

Note: `ended_at` is `*time.Time` in the model but the query returns `*string`. Keep existing pattern — the handler converts it. Actually, looking at the current code, it uses `*time.Time`. Let me check the actual column type... It's TIMESTAMPTZ. So we need to keep `*time.Time`. Let me adjust:

```go
func (s *SessionStore) GetSession(ctx context.Context, sessionID string) (*models.AgentSession, error) {
	var session models.AgentSession
	var endedAt *time.Time
	var title *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, title, created_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&session.ID, &session.AgentID, &session.NotebookID, &session.UserID, &session.MaxTurns, &session.MaxTokens, &endedAt, &title, &session.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	session.EndedAt = endedAt
	if title != nil {
		session.Title = title
	}
	return &session, nil
}
```

**Step 3: Add UpdateTitle method**

```go
func (s *SessionStore) UpdateTitle(ctx context.Context, sessionID string, title *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions SET title = $1 WHERE id = $2
	`, title, sessionID)
	if err != nil {
		return fmt.Errorf("update title: %w", err)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `task test`
Expected: Tests pass (some may fail due to CreateSession signature change — that's expected, we'll fix callers in next tasks)

**Step 5: Commit**

```bash
git add internal/agent/session.go
git commit -m "feat: update SessionStore for title support"
```

---

### Task 4: Update API Handlers

**Files:**
- Modify: `internal/api/agent_handlers.go`

**Step 1: Update handleCreateSession to accept optional title**

```go
func (h *agentHandlers) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req struct {
		NotebookID string  `json:"notebook_id"`
		MaxTurns   int     `json:"max_turns"`
		MaxTokens  int     `json:"max_tokens"`
		Title      *string `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.MaxTurns == 0 {
		req.MaxTurns = 100
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 100000
	}

	sessionID := uuid.New().String()
	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, max_tokens, title, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, sessionID, agentID, req.NotebookID, claims.UserID, req.MaxTurns, req.MaxTokens, req.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"session_id": sessionID})
}
```

**Step 2: Update handleListSessions to include title**

```go
func (h *agentHandlers) handleListSessions(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT s.id, s.agent_id, s.notebook_id, s.user_id, s.max_turns, s.max_tokens, s.ended_at, s.title, s.created_at,
			COALESCE(
				(SELECT content FROM agent_messages WHERE session_id = s.id AND role = 'user' ORDER BY created_at ASC LIMIT 1),
				''
			) as first_message,
			COALESCE(
				(SELECT COUNT(*) FROM agent_messages WHERE session_id = s.id),
				0
			) as message_count
		FROM agent_sessions s
		WHERE s.agent_id = $1
		ORDER BY s.created_at DESC LIMIT 50
	`, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var sessions []map[string]any
	for rows.Next() {
		var s models.AgentSession
		var firstMsg string
		var msgCount int
		var endedAt *time.Time
		var title *string
		rows.Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &endedAt, &title, &s.CreatedAt, &firstMsg, &msgCount)
		sessions = append(sessions, map[string]any{
			"id":            s.ID,
			"agent_id":      s.AgentID,
			"notebook_id":   s.NotebookID,
			"user_id":       s.UserID,
			"max_turns":     s.MaxTurns,
			"max_tokens":    s.MaxTokens,
			"ended_at":      endedAt,
			"title":         title,
			"created_at":    s.CreatedAt,
			"first_message": firstMsg,
			"message_count": msgCount,
		})
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sessions)
}
```

**Step 3: Add handleUpdateSessionTitle handler**

```go
func (h *agentHandlers) handleUpdateSessionTitle(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	var agentID string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT agent_id FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Title *string `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if err := h.server.agentEngine.SessionStore().UpdateTitle(r.Context(), sessionID, req.Title); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"title": req.Title})
}
```

**Step 4: Run tests**

Run: `task test`
Expected: Tests pass

**Step 5: Commit**

```bash
git add internal/api/agent_handlers.go
git commit -m "feat: add session title API handlers"
```

---

### Task 5: Register Route

**Files:**
- Modify: `internal/api/router.go:262-264`

**Step 1: Add route after session messages route**

```go
	s.mux.Handle("GET /api/v1/sessions/{session_id}/messages", authMW(http.HandlerFunc(ah.handleGetSessionMessages)))
	s.mux.Handle("PATCH /api/v1/sessions/{session_id}/title", authMW(http.HandlerFunc(ah.handleUpdateSessionTitle)))
```

**Step 2: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register session title update route"
```

---

### Task 6: LLM Title Generation in Engine

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/session.go`

**Step 1: Add GetMessagesWithLimit to session.go**

```go
func (s *SessionStore) GetMessagesWithLimit(ctx context.Context, sessionID string, limit int) ([]models.AgentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, role, content, tool_call_id, tool_calls, reasoning_content, tokens_input, tokens_output, model_calls, duration_ms, created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.AgentMessage
	for rows.Next() {
		var msg models.AgentMessage
		var content *string
		var toolCallID *string
		var toolCallsJSON []byte
		var reasoningContent *string
		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &reasoningContent, &msg.TokensInput, &msg.TokensOutput, &msg.ModelCalls, &msg.DurationMs, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		if content != nil {
			msg.Content = *content
		}
		if reasoningContent != nil {
			msg.ReasoningContent = *reasoningContent
		}
		msg.ToolCallID = toolCallID
		if toolCallsJSON != nil {
			json.Unmarshal(toolCallsJSON, &msg.ToolCalls)
		}
		messages = append(messages, msg)
	}

	return messages, nil
}
```

**Step 2: Add generateSessionTitle to engine.go**

Add after `summarizeAndNewSession`:

```go
func (e *Engine) generateSessionTitle(ctx context.Context, sessionID string, masterKey []byte) error {
	messages, err := e.session.GetMessagesWithLimit(ctx, sessionID, 5)
	if err != nil {
		return fmt.Errorf("get messages: %w", err)
	}

	if len(messages) < 2 {
		return nil
	}

	prompt := "Generate a concise title (max 50 characters) for this conversation. Only return the title, nothing else:\n\n"
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			prompt += fmt.Sprintf("User: %s\n", truncate(m.Content, 200))
		}
	}

	session, err := e.session.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}

	llmClient := e.llm
	if session.AgentID != "" {
		var agentModelConfigID *string
		err = e.pool.QueryRow(ctx, `SELECT model_config_id FROM agents WHERE id = $1`, session.AgentID).Scan(&agentModelConfigID)
		if err == nil && agentModelConfigID != nil && *agentModelConfigID != "" {
			var mc models.ModelConfig
			var defaultParams []byte
			err = e.pool.QueryRow(ctx, `SELECT id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, folder_id, created_by, created_at, updated_at FROM model_configs WHERE id = $1`, *agentModelConfigID).Scan(
				&mc.ID, &mc.OrgID, &mc.Name, &mc.Provider, &mc.BaseURL, &mc.Model, &mc.APIKeyEncrypted, &defaultParams, &mc.ContextWindow, &mc.FolderID, &mc.CreatedBy, &mc.CreatedAt, &mc.UpdatedAt)
			if err == nil {
				if defaultParams != nil {
					json.Unmarshal(defaultParams, &mc.DefaultParams)
				}
				llmClient = NewLLMClient(mc.BaseURL, mc.Model, mc.APIKeyEncrypted)
			}
		}
	}

	if llmClient == nil {
		return nil
	}

	resp, err := llmClient.Chat(ctx, []ChatMessage{{Role: "user", Content: prompt}}, nil, masterKey)
	if err != nil {
		return fmt.Errorf("llm chat: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil
	}

	title := strings.TrimSpace(resp.Choices[0].Message.Content)
	if title == "" {
		return nil
	}

	if len(title) > 50 {
		title = title[:50]
	}

	return e.session.UpdateTitle(ctx, sessionID, &title)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
```

**Step 3: Trigger title generation after 2nd user message in ProcessMessage**

After the user message is appended (around line 169), add:

```go
	userMsgID := uuid.New().String()
	e.session.AppendMessage(ctx, &models.AgentMessage{
		ID:        userMsgID,
		SessionID: sessionID,
		Role:      "user",
		Content:   userMessage,
		CreatedAt: time.Now(),
	})

	// Count user messages and trigger title generation on 2nd
	userMsgCount := 0
	for _, m := range messages {
		if m.Role == "user" {
			userMsgCount++
		}
	}
	if userMsgCount == 1 { // This is the 2nd user message (messages didn't include the one we just appended)
		go func() {
			if err := e.generateSessionTitle(context.Background(), sessionID, masterKey); err != nil {
				slog.Warn("engine: failed to generate session title", "session_id", sessionID, "error", err)
			}
		}()
	}
```

**Step 4: Run tests**

Run: `task test`
Expected: Tests pass

**Step 5: Commit**

```bash
git add internal/agent/engine.go internal/agent/session.go
git commit -m "feat: add LLM session title generation"
```

---

### Task 7: Update Frontend Types

**Files:**
- Modify: `web/src/types/agent.ts:58-67`

**Step 1: Add title to AgentSession**

```typescript
export interface AgentSession {
  id: string
  agent_id: string
  notebook_id: string
  user_id: string
  max_turns: number
  max_tokens: number
  title: string | null
  ended_at?: string
  created_at: string
}
```

**Step 2: Commit**

```bash
git add web/src/types/agent.ts
git commit -m "feat: add title to AgentSession type"
```

---

### Task 8: Update SessionHistory Component

**Files:**
- Modify: `web/src/components/SessionHistory.tsx`

**Step 1: Update SessionSummary interface**

```typescript
interface SessionSummary {
  id: string
  created_at: string
  first_message: string
  message_count: number
  notebook_id: string
  title: string | null
}
```

**Step 2: Update session item rendering to show title**

In the session list (around line 111-112), change:

```tsx
<div style={styles.sessionPreview}>
  {s.title || s.first_message || '(empty session)'}
</div>
```

**Step 3: Add inline editing capability**

Add state and handler for editing:

```tsx
const [editingTitle, setEditingTitle] = useState<string | null>(null)
const [editValue, setEditValue] = useState('')
```

Add save function:

```tsx
const handleSaveTitle = async (sessionId: string, title: string) => {
  try {
    await api.patch(`/api/v1/sessions/${sessionId}/title`, { title: title || null })
    setSessions(prev => prev.map(s => s.id === sessionId ? { ...s, title: title || null } : s))
    setEditingTitle(null)
  } catch {
    setError('Failed to update title')
  }
}
```

Update the session item to support editing:

```tsx
<button key={s.id} style={styles.sessionItem} onClick={() => !editingTitle && loadSession(s)}>
  <MessageSquare size={14} style={{ flexShrink: 0, marginTop: 2 }} />
  <div style={styles.sessionInfo}>
    <div style={styles.sessionPreview} onClick={(e) => {
      e.stopPropagation()
      setEditingTitle(s.id)
      setEditValue(s.title || s.first_message || '')
    }}>
      {editingTitle === s.id ? (
        <input
          style={styles.titleInput}
          value={editValue}
          onChange={(e) => setEditValue(e.target.value)}
          onBlur={() => handleSaveTitle(s.id, editValue)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') handleSaveTitle(s.id, editValue)
            if (e.key === 'Escape') setEditingTitle(null)
          }}
          autoFocus
          maxLength={50}
        />
      ) : (
        <>
          {s.title || s.first_message || '(empty session)'}
          <Edit2 size={12} style={styles.editIcon} />
        </>
      )}
    </div>
    <div style={styles.sessionMeta}>
      {new Date(s.created_at).toLocaleDateString()} · {s.message_count} messages
    </div>
  </div>
</button>
```

**Step 4: Add Edit2 import and styles**

```tsx
import { ArrowLeft, MessageSquare, Play, Edit2 } from 'lucide-react'
```

Add styles:

```tsx
titleInput: {
  width: '100%',
  fontSize: 13,
  fontWeight: 500,
  background: 'var(--bg-primary)',
  border: '1px solid var(--accent)',
  borderRadius: 3,
  padding: '2px 4px',
  color: 'var(--text-primary)',
  outline: 'none',
},
editIcon: {
  marginLeft: 4,
  opacity: 0,
  transition: 'opacity 0.15s',
  color: 'var(--text-muted)',
},
```

Update sessionItem to show edit icon on hover:

```tsx
sessionItem: {
  display: 'flex',
  gap: 8,
  width: '100%',
  padding: '10px 8px',
  background: 'none',
  border: 'none',
  borderBottom: '1px solid var(--border-light)',
  cursor: 'pointer',
  color: 'var(--text-primary)',
  textAlign: 'left' as const,
  '&:hover .edit-icon': { opacity: 1 },
},
```

Note: Inline styles don't support `&:hover`. We'll need to use CSS or a different approach. Let's use a wrapper with onMouseEnter/Leave:

Actually, simpler: just always show the edit icon but with low opacity, and increase on hover via inline state. Or use a CSS class. Let's keep it simple — show the edit icon always but subtle:

```tsx
editIcon: {
  marginLeft: 4,
  opacity: 0.3,
  color: 'var(--text-muted)',
},
```

**Step 5: Run frontend tests**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors

**Step 6: Commit**

```bash
git add web/src/components/SessionHistory.tsx
git commit -m "feat: display and edit session titles in history"
```

---

### Task 9: Update AgentPanel to Show Session Title

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`

**Step 1: Add state for session title**

```tsx
const [sessionTitle, setSessionTitle] = useState<string | null>(null)
```

**Step 2: Update header to show session title**

In the agent info section (around line 506-508), change:

```tsx
<div style={styles.agentInfo}>
  <Bot size={14} style={{ color: 'var(--accent)' }} />
  <span style={styles.agentName}>
    {sessionTitle || selectedAgent.name}
  </span>
  {sessionTitle && (
    <span style={styles.agentSubName}>{selectedAgent.name}</span>
  )}
  ...
```

**Step 3: Add agentSubName style**

```tsx
agentSubName: {
  fontSize: 11,
  color: 'var(--text-muted)',
  fontWeight: 400,
},
```

**Step 4: Run frontend tests**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors

**Step 5: Commit**

```bash
git add web/src/components/AgentPanel.tsx
git commit -m "feat: show session title in agent panel header"
```

---

### Task 10: Full Test Suite

**Step 1: Run all Go tests**

Run: `task test`
Expected: All tests pass

**Step 2: Run frontend type check**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

**Step 3: Run linter**

Run: `task check`
Expected: All checks pass

**Step 4: Final commit**

```bash
git add -A
git commit -m "feat: complete session titles with LLM generation and user editing"
```
