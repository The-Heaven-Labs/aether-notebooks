# Agent Chat Improvements — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement ten UX improvements to the agent chat system: session history, slash command picker, last-used agent memory, message queueing, auto-focus, platform-wide discovery tools, schema exploration, task tracking, and improved cell highlighting.

**Architecture:** Most work is frontend (React components in `web/src/components/`), with three new backend tool files (`tools_platform.go`, `tools_notebook.go` additions, `tools_agent.go` additions) and task state stored in-memory on the Engine struct. The existing WebSocket transport carries new message types (`tasks_updated`).

**Tech Stack:** React + TypeScript (frontend), Go + pgx (backend), WebSocket for real-time chat.

---

### Task 1: Session History — Backend Query Enhancement

**Files:**
- Modify: `internal/api/agent_handlers.go:259-277` (handleListSessions)

The current `handleListSessions` returns sessions without message count or preview. Enhance it to include first-user-message preview and message count so the frontend can render the history list without N+1 requests.

**Step 1: Modify handleListSessions to include message preview**

In `internal/api/agent_handlers.go`, replace the `handleListSessions` handler with a version that joins with `agent_messages` to get the first user message content and message count:

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
		SELECT s.id, s.agent_id, s.notebook_id, s.user_id, s.max_turns, s.max_tokens, s.ended_at, s.created_at,
			COALESCE(m.first_message, ''), COALESCE(m.msg_count, 0)
		FROM agent_sessions s
		LEFT JOIN (
			SELECT session_id,
				MIN(CASE WHEN role = 'user' THEN content END) as first_message,
				COUNT(*) as msg_count
			FROM agent_messages
			GROUP BY session_id
		) m ON m.session_id = s.id
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
		rows.Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &s.EndedAt, &s.CreatedAt, &firstMsg, &msgCount)
		sessions = append(sessions, map[string]any{
			"id":             s.ID,
			"agent_id":       s.AgentID,
			"notebook_id":    s.NotebookID,
			"user_id":        s.UserID,
			"ended_at":       s.EndedAt,
			"created_at":     s.CreatedAt,
			"first_message":  firstMsg,
			"message_count":  msgCount,
		})
	}

	writeJSON(w, http.StatusOK, sessions)
}
```

**Step 2: Add a GET endpoint for session messages**

The `handleGetSession` currently only returns session metadata, not messages. Add a new endpoint or enhance the existing one. Since the backend already has `SessionStore.GetMessages()`, add a handler:

```go
func (h *agentHandlers) handleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, role, content, tool_calls, tool_call_id, reasoning_content, created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var messages []map[string]any
	for rows.Next() {
		var id, role, created string
		var content, tcID *string
		var toolCalls, reasoning []byte
		rows.Scan(&id, &role, &content, &toolCalls, &tcID, &reasoning, &created)
		msg := map[string]any{
			"id":         id,
			"role":       role,
			"created_at": created,
		}
		if content != nil {
			msg["content"] = *content
		}
		if tcID != nil {
			msg["tool_call_id"] = *tcID
		}
		if reasoning != nil {
			msg["reasoning_content"] = string(reasoning)
		}
		if toolCalls != nil {
			msg["tool_calls"] = json.RawMessage(toolCalls)
		}
		messages = append(messages, msg)
	}

	writeJSON(w, http.StatusOK, messages)
}
```

Register the route in `internal/api/router.go`:
```go
// Add under agent routes:
mux.HandleFunc("GET /api/v1/sessions/{session_id}/messages", h.handleGetSessionMessages)
```

**Step 3: Build and test**

Run: `task build`
Expected: Compiles without errors.

**Step 4: Commit**

```bash
git add internal/api/agent_handlers.go internal/api/router.go
git commit -m "feat: enhance session list with message preview, add session messages endpoint"
```

---

### Task 2: Session History — Frontend UI

**Files:**
- Create: `web/src/components/SessionHistory.tsx`
- Modify: `web/src/components/AgentPanel.tsx:1-12,182-310`

**Step 1: Create SessionHistory component**

Create `web/src/components/SessionHistory.tsx`:

```tsx
import { useState, useEffect } from 'react'
import { ArrowLeft, MessageSquare } from 'lucide-react'
import { api } from '../api/client'

interface SessionSummary {
  id: string
  created_at: string
  first_message: string
  message_count: number
  notebook_id: string
}

interface SessionMessage {
  id: string
  role: string
  content?: string
  tool_calls?: any
  reasoning_content?: string
  created_at: string
}

interface SessionHistoryProps {
  agentId: string
  onBack: () => void
}

export function SessionHistory({ agentId, onBack }: SessionHistoryProps) {
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [selectedSession, setSelectedSession] = useState<SessionSummary | null>(null)
  const [messages, setMessages] = useState<SessionMessage[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.get<SessionSummary[]>(`/api/v1/agents/${agentId}/sessions`)
      .then(setSessions)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [agentId])

  const loadSession = async (session: SessionSummary) => {
    setSelectedSession(session)
    try {
      const msgs = await api.get<SessionMessage[]>(`/api/v1/sessions/${session.id}/messages`)
      setMessages(msgs)
    } catch {}
  }

  if (selectedSession) {
    return (
      <>
        <div style={styles.sessionHeader}>
          <button onClick={() => setSelectedSession(null)} style={styles.backBtn}>
            <ArrowLeft size={14} /> Back to history
          </button>
          <span style={styles.sessionDate}>
            {new Date(selectedSession.created_at).toLocaleDateString()}
          </span>
        </div>
        <div style={styles.messageList}>
          {messages.map((msg) => (
            <div key={msg.id} style={{
              ...styles.historyMessage,
              ...(msg.role === 'user' ? styles.userBubble : msg.role === 'assistant' ? styles.assistantBubble : styles.toolBubble),
            }}>
              {msg.content || (msg.tool_calls ? 'Tool calls' : '(empty)')}
            </div>
          ))}
        </div>
      </>
    )
  }

  return (
    <>
      <div style={styles.sessionHeader}>
        <button onClick={onBack} style={styles.backBtn}>
          <ArrowLeft size={14} /> Back to chat
        </button>
        <span style={styles.headerTitle}>Chat History</span>
      </div>
      <div style={styles.sessionList}>
        {loading ? (
          <div style={styles.loadingText}>Loading...</div>
        ) : sessions.length === 0 ? (
          <div style={styles.loadingText}>No past sessions</div>
        ) : (
          sessions.map((s) => (
            <button key={s.id} style={styles.sessionItem} onClick={() => loadSession(s)}>
              <MessageSquare size={14} style={{ flexShrink: 0, marginTop: 2 }} />
              <div style={styles.sessionInfo}>
                <div style={styles.sessionPreview}>
                  {s.first_message || '(empty session)'}
                </div>
                <div style={styles.sessionMeta}>
                  {new Date(s.created_at).toLocaleDateString()} · {s.message_count} messages
                </div>
              </div>
            </button>
          ))
        )}
      </div>
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  sessionHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '10px 16px',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
  },
  backBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    padding: '4px 8px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  headerTitle: { fontSize: 12, fontWeight: 500, color: 'var(--text-muted)' },
  sessionDate: { fontSize: 12, color: 'var(--text-muted)', marginLeft: 'auto' },
  sessionList: { flex: 1, overflowY: 'auto', padding: 8 },
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
  },
  sessionInfo: { flex: 1, minWidth: 0 },
  sessionPreview: {
    fontSize: 13,
    fontWeight: 500,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
  },
  sessionMeta: { fontSize: 11, color: 'var(--text-muted)', marginTop: 2 },
  messageList: { flex: 1, overflowY: 'auto', padding: 16, display: 'flex', flexDirection: 'column', gap: 8 },
  historyMessage: {
    padding: '8px 12px', borderRadius: 6, fontSize: 13, lineHeight: 1.4,
    maxWidth: '85%', wordBreak: 'break-word' as const,
  },
  userBubble: { background: 'var(--accent)', color: 'white', alignSelf: 'flex-end' },
  assistantBubble: { background: 'var(--bg-secondary)', color: 'var(--text-primary)', alignSelf: 'flex-start' },
  toolBubble: { background: 'rgba(var(--accent-rgb, 59, 130, 246), 0.1)', color: 'var(--text-secondary)', alignSelf: 'flex-start', fontSize: 11 },
  loadingText: { textAlign: 'center', padding: 20, color: 'var(--text-muted)', fontSize: 13 },
}
```

**Step 2: Integrate into AgentPanel**

In `web/src/components/AgentPanel.tsx`:

Add import:
```tsx
import { History } from 'lucide-react'
import { SessionHistory } from './SessionHistory'
```

Add state:
```tsx
const [showHistory, setShowHistory] = useState(false)
```

Add the History button in the agent info bar (after the "Change" button):
```tsx
<button
  style={styles.historyBtn}
  onClick={() => setShowHistory(true)}
  title="View chat history"
>
  <History size={14} />
</button>
```

After `{!selectedAgent ? ...` and before the agent info, add a branch for history view:
```tsx
{showHistory && selectedAgent ? (
  <SessionHistory agentId={selectedAgent.id} onBack={() => setShowHistory(false)} />
) : !selectedAgent ? ...
```

Add the history button style:
```tsx
historyBtn: {
  padding: '3px 8px',
  background: 'none',
  border: 'none',
  borderRadius: 4,
  cursor: 'pointer',
  color: 'var(--text-secondary)',
  display: 'flex',
  alignItems: 'center',
},
```

**Step 3: Run dev server**

Run: `task dev` and `task dev:web`
Expected: History button shows in agent header. Clicking shows session list. Clicking a session loads messages read-only.

**Step 4: Commit**

```bash
git add web/src/components/SessionHistory.tsx web/src/components/AgentPanel.tsx
git commit -m "feat: add session history UI to agent panel"
```

---

### Task 3: Last-Used Agent Memory

**Files:**
- Modify: `web/src/components/AgentPanel.tsx:16-52`

**Step 1: Save last-used agent to localStorage**

In `AgentPanel.tsx`, modify the `startSession` function to save the agent ID, and load it on mount:

```tsx
const LAST_AGENT_KEY = 'hnb:lastAgentId'

// In the startSession function, add after setSelectedAgent:
localStorage.setItem(LAST_AGENT_KEY, agent.id)

// Add a useEffect that auto-selects the last agent:
useEffect(() => {
  if (!selectedAgent && agents.length > 0 && !isLoadingAgents) {
    const lastId = localStorage.getItem(LAST_AGENT_KEY)
    if (lastId) {
      const agent = agents.find((a) => a.id === lastId)
      if (agent) {
        startSession(agent)
      }
    }
  }
}, [agents, isLoadingAgents])
```

**Step 2: Verify auto-select behavior**

The useEffect depends on `agents` being loaded. When the panel mounts with no selected agent and agents are loaded, it should auto-start the last-used agent.

**Step 3: Commit**

```bash
git add web/src/components/AgentPanel.tsx
git commit -m "feat: remember last-used agent via localStorage"
```

---

### Task 4: Slash Command Picker

**Files:**
- Create: `web/src/components/SlashCommandPicker.tsx`
- Modify: `web/src/components/AgentPanel.tsx:283-292`

**Step 1: Create SlashCommandPicker component**

Create `web/src/components/SlashCommandPicker.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'

interface Command {
  command: string
  description: string
}

const COMMANDS: Command[] = [
  { command: '/new', description: 'Start a fresh session' },
  { command: '/skills', description: 'List available skills' },
  { command: '/agents', description: 'List available agents' },
  { command: '/summarize', description: 'Summarize the current session' },
]

interface Props {
  filter: string
  onSelect: (command: string) => void
  onClose: () => void
}

export function SlashCommandPicker({ filter, onSelect, onClose }: Props) {
  const [selectedIndex, setSelectedIndex] = useState(0)
  const filtered = COMMANDS.filter((c) =>
    c.command.toLowerCase().includes(filter.toLowerCase())
  )

  useEffect(() => {
    setSelectedIndex(0)
  }, [filter])

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelectedIndex((i) => Math.min(i + 1, filtered.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelectedIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (filtered[selectedIndex]) {
        onSelect(filtered[selectedIndex].command)
      }
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  })

  if (filtered.length === 0) return null

  return (
    <div style={styles.picker}>
      {filtered.map((cmd, i) => (
        <button
          key={cmd.command}
          style={{
            ...styles.item,
            ...(i === selectedIndex ? styles.selectedItem : {}),
          }}
          onClick={() => onSelect(cmd.command)}
          onMouseEnter={() => setSelectedIndex(i)}
        >
          <span style={styles.cmdName}>{cmd.command}</span>
          <span style={styles.cmdDesc}>{cmd.description}</span>
        </button>
      ))}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  picker: {
    position: 'absolute',
    bottom: '100%',
    left: 12,
    right: 12,
    marginBottom: 4,
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: '0 4px 16px rgba(0,0,0,0.15)',
    overflow: 'hidden',
    zIndex: 200,
  },
  item: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    width: '100%',
    padding: '8px 12px',
    border: 'none',
    background: 'none',
    cursor: 'pointer',
    textAlign: 'left' as const,
    fontSize: 13,
  },
  selectedItem: {
    background: 'var(--bg-secondary)',
  },
  cmdName: {
    fontWeight: 600,
    color: 'var(--accent)',
    minWidth: 80,
  },
  cmdDesc: {
    color: 'var(--text-muted)',
    fontSize: 12,
  },
}
```

**Step 2: Integrate into AgentPanel**

In `AgentPanel.tsx`:

Add import:
```tsx
import { SlashCommandPicker } from './SlashCommandPicker'
```

Add state:
```tsx
const [showSlashPicker, setShowSlashPicker] = useState(false)
```

Modify the `onChange` handler of the textarea to detect `/`:
```tsx
onChange={(e) => {
  setInput(e.target.value)
  setShowSlashPicker(e.target.value.startsWith('/') && e.target.value.length <= 15)
}}
```

Modify the `onKeyDown` to dismiss picker on space:
```tsx
const handleKeyDown = (e: React.KeyboardEvent) => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    if (showSlashPicker) {
      setShowSlashPicker(false)
      return
    }
    sendMessage()
  }
  if (e.key === ' ' && showSlashPicker) {
    setShowSlashPicker(false)
  }
}
```

Wrap the input area in a relative container and add the picker:
```tsx
<div style={{ ...styles.inputArea, position: 'relative' }}>
  {showSlashPicker && (
    <SlashCommandPicker
      filter={input}
      onSelect={(cmd) => {
        setInput(cmd + ' ')
        setShowSlashPicker(false)
      }}
      onClose={() => setShowSlashPicker(false)}
    />
  )}
  <textarea ... />
  <button ... />
</div>
```

**Step 3: Build and verify**

Run: `task build:web`
Expected: Compiles successfully.

**Step 4: Commit**

```bash
git add web/src/components/SlashCommandPicker.tsx web/src/components/AgentPanel.tsx
git commit -m "feat: add slash command picker to agent chat"
```

---

### Task 5: Message Queueing During Streaming

**Files:**
- Modify: `web/src/components/AgentPanel.tsx:143-171,284-300`

**Step 1: Implement pending message queue in AgentPanel**

Add state:
```tsx
const [pendingMessages, setPendingMessages] = useState<string[]>([])
const inputRef = useRef<HTMLTextAreaElement>(null)
```

Modify `sendMessage` to handle streaming case:
```tsx
const sendMessage = () => {
  if (!input.trim() || !wsRef.current) return

  if (isStreaming) {
    setPendingMessages((prev) => [...prev, input])
    setMessages((prev) => [...prev, { role: 'user', content: input, queued: true }])
    setInput('')
    return
  }

  setMessages((prev) => [...prev, { role: 'user', content: input }])
  wsRef.current.send(JSON.stringify({ type: 'message', content: input }))
  setInput('')
  setIsStreaming(true)
  setCurrentStreamingText('')
  inputRef.current?.focus()
}
```

Modify the `done` handler to process the queue:
```tsx
case 'done':
  setIsStreaming(false)
  updateStreamingReasoning('')
  needsCollapseRef.current = false
  // ... existing done logic ...
  
  // Process next pending message
  setPendingMessages((pending) => {
    if (pending.length > 0) {
      const next = pending[0]
      setTimeout(() => {
        setPendingMessages((p) => p.slice(1))
        setInput(next)
        sendMessage()
      }, 100)
      return pending
    }
    return pending
  })
  break
```

Update the message type to support `queued`:
```tsx
const [messages, setMessages] = useState<Array<{ role: string; content: string; reasoning?: string | undefined; params?: string; result?: string; queued?: boolean }>>([])
```

Remove `disabled={isStreaming}` from textarea and change to:
```tsx
disabled={false}
```

Change the send button to always be enabled when there's input:
```tsx
disabled={!input.trim()}
```

Render queued messages with a dimmed style:
```tsx
{msg.queued && (
  <span style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>Queued...</span>
)}
```

**Step 2: Build and verify**

Run: `task build:web`
Expected: Compiles without TypeScript errors.

**Step 3: Commit**

```bash
git add web/src/components/AgentPanel.tsx
git commit -m "feat: support message queueing while agent is streaming"
```

---

### Task 6: Auto-Focus Input on Panel Open and After Send

**Files:**
- Modify: `web/src/components/AgentPanel.tsx:16,47-52,143-172`

**Step 1: Add inputRef and focus logic**

Already partially done in Task 5 with `inputRef`. Add the useEffect for mount focus:

```tsx
const inputRef = useRef<HTMLTextAreaElement>(null)

// Auto-focus on mount
useEffect(() => {
  const timer = setTimeout(() => {
    inputRef.current?.focus()
  }, 100)
  return () => clearTimeout(timer)
}, [selectedAgent]) // re-focus when agent is selected

// Focus after streaming completes (in the 'done' WS handler)
case 'done':
  // ... existing done logic ...
  setTimeout(() => inputRef.current?.focus(), 50)
  break
```

Add `ref={inputRef}` to the textarea:
```tsx
<textarea
  ref={inputRef}
  style={styles.input}
  ...
/>
```

**Step 2: Build and verify**

Run: `task build:web`
Expected: Compiles.

**Step 3: Commit**

```bash
git add web/src/components/AgentPanel.tsx
git commit -m "feat: auto-focus chat input on panel open and after streaming"
```

---

### Task 7: Platform Discovery Tools (Backend)

**Files:**
- Create: `internal/agent/tools_platform.go`
- Modify: `internal/agent/engine.go:23-30`

**Step 1: Create tools_platform.go**

Create `internal/agent/tools_platform.go`:

```go
package agent

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterPlatformTools(reg *ToolRegistry, db *pgxpool.Pool) {
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_notebooks",
			Description: "List notebooks the user can access in the organization",
			Parameters:  `{"type":"object","properties":{"folder_id":{"type":"string"},"search":{"type":"string"}}}`,
		},
		Handler: makeListNotebooksHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_connectors",
			Description: "List database connectors the user can access",
			Parameters:  `{"type":"object","properties":{"search":{"type":"string"}}}`,
		},
		Handler: makeListConnectorsHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_folders",
			Description: "List folders in the organization",
			Parameters:  `{"type":"object","properties":{"parent_id":{"type":"string"}}}`,
		},
		Handler: makeListFoldersHandler(db),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "get_folder_tree",
			Description: "Get the full folder hierarchy for the organization",
			Parameters:  `{"type":"object","properties":{}}`,
		},
		Handler: makeGetFolderTreeHandler(db),
	})
}

func makeListNotebooksHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			FolderID string `json:"folder_id"`
			Search   string `json:"search"`
		}
		json.Unmarshal(args, &req)

		query := `SELECT n.id, n.name, COALESCE(n.description, ''), n.folder_id, n.created_at
			FROM notebooks n WHERE n.org_id = $1`
		params := []any{ctx.OrgID}

		if req.FolderID != "" {
			query += ` AND n.folder_id = $2`
			params = append(params, req.FolderID)
		}
		if req.Search != "" {
			placeholder := len(params) + 1
			query += fmt.Sprintf(` AND n.name ILIKE '%%' || $%d || '%%'`, placeholder)
			params = append(params, req.Search)
		}
		query += ` ORDER BY n.updated_at DESC LIMIT 50`

		rows, err := db.Query(ctx.Context, query, params...)
		if err != nil {
			return nil, fmt.Errorf("list notebooks: %w", err)
		}
		defer rows.Close()

		var notebooks []map[string]any
		for rows.Next() {
			var id, name, desc, folderID string
			var created string
			var fID *string
			rows.Scan(&id, &name, &desc, &fID, &created)
			if fID != nil {
				folderID = *fID
			}
			notebooks = append(notebooks, map[string]any{
				"id": id, "name": name, "description": desc,
				"folder_id": folderID, "created_at": created,
			})
		}
		return map[string]any{"notebooks": notebooks, "count": len(notebooks)}, nil
	}
}

func makeListConnectorsHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Search string `json:"search"`
		}
		json.Unmarshal(args, &req)

		query := `SELECT id, name, type, COALESCE(folder_id::text, ''), created_at
			FROM connectors WHERE org_id = $1`
		params := []any{ctx.OrgID}

		if req.Search != "" {
			query += ` AND name ILIKE '%' || $2 || '%'`
			params = append(params, req.Search)
		}
		query += ` ORDER BY name ASC LIMIT 50`

		rows, err := db.Query(ctx.Context, query, params...)
		if err != nil {
			return nil, fmt.Errorf("list connectors: %w", err)
		}
		defer rows.Close()

		var connectors []map[string]any
		for rows.Next() {
			var id, name, ctype, folderID, created string
			rows.Scan(&id, &name, &ctype, &folderID, &created)
			connectors = append(connectors, map[string]any{
				"id": id, "name": name, "type": ctype,
				"folder_id": folderID, "created_at": created,
			})
		}
		return map[string]any{"connectors": connectors, "count": len(connectors)}, nil
	}
}

func makeListFoldersHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ParentID string `json:"parent_id"`
		}
		json.Unmarshal(args, &req)

		query := `SELECT id, name, parent_id, created_at FROM folders WHERE org_id = $1`
		params := []any{ctx.OrgID}

		if req.ParentID != "" {
			query += ` AND parent_id = $2`
			params = append(params, req.ParentID)
		} else {
			query += ` AND parent_id IS NULL`
		}
		query += ` ORDER BY name ASC`

		rows, err := db.Query(ctx.Context, query, params...)
		if err != nil {
			return nil, fmt.Errorf("list folders: %w", err)
		}
		defer rows.Close()

		var folders []map[string]any
		for rows.Next() {
			var id, name, created string
			var parentID *string
			rows.Scan(&id, &name, &parentID, &created)
			pid := ""
			if parentID != nil {
				pid = *parentID
			}
			folders = append(folders, map[string]any{
				"id": id, "name": name, "parent_id": pid, "created_at": created,
			})
		}
		return map[string]any{"folders": folders, "count": len(folders)}, nil
	}
}

func makeGetFolderTreeHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		rows, err := db.Query(ctx.Context, `
			WITH RECURSIVE tree AS (
				SELECT id, name, parent_id, 0 as depth, name as path
				FROM folders WHERE org_id = $1 AND parent_id IS NULL
				UNION ALL
				SELECT f.id, f.name, f.parent_id, t.depth + 1, t.path || ' / ' || f.name
				FROM folders f JOIN tree t ON f.parent_id = t.id
			)
			SELECT id, name, parent_id, depth, path FROM tree ORDER BY path
		`, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("get folder tree: %w", err)
		}
		defer rows.Close()

		var folders []map[string]any
		for rows.Next() {
			var id, name, path string
			var parentID *string
			var depth int
			rows.Scan(&id, &name, &parentID, &depth, &path)
			pid := ""
			if parentID != nil {
				pid = *parentID
			}
			folders = append(folders, map[string]any{
				"id": id, "name": name, "parent_id": pid, "depth": depth, "path": path,
			})
		}
		return map[string]any{"folders": folders}, nil
	}
}
```

**Step 2: Register platform tools in engine.go**

In `internal/agent/engine.go`, add the registration call in `NewEngine`:

```go
func NewEngine(pool *pgxpool.Pool) *Engine {
	reg := NewToolRegistry()
	RegisterNotebookTools(reg, pool)
	RegisterChartTools(reg, pool)
	RegisterAgentTools(reg, pool)
	RegisterPlatformTools(reg, pool)  // ADD THIS LINE

	return &Engine{
		registry: reg,
		session:  NewSessionStore(pool),
		pool:     pool,
	}
}
```

**Step 3: Build and test**

Run: `task build`
Expected: Compiles without errors.

Run: `task test`
Expected: All existing tests pass.

**Step 4: Commit**

```bash
git add internal/agent/tools_platform.go internal/agent/engine.go
git commit -m "feat: add platform discovery tools (list notebooks, connectors, folders, folder tree)"
```

---

### Task 8: Schema Exploration Tool (Backend)

**Files:**
- Modify: `internal/agent/tools_notebook.go:` add explore_schema tool
- Modify: `internal/agent/engine.go:` (already done via RegisterNotebookTools in NewEngine)

**Step 1: Add explore_schema tool**

In `internal/agent/tools_notebook.go`, at the end of `RegisterNotebookTools`, add:

```go
reg.Register(&ToolDef{
	Function: struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	}{
		Name:        "explore_schema",
		Description: "Explore the database schema for a connector: list all tables and their columns with types",
		Parameters:  `{"type":"object","properties":{"connector_id":{"type":"string","description":"ID of the connector to explore"}},"required":["connector_id"]}`,
	},
	Handler: makeExploreSchemaHandler(db),
})
```

Add the handler function:

```go
func makeExploreSchemaHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ConnectorID string `json:"connector_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if err := ctx.CheckPermission("connector", req.ConnectorID, "view"); err != nil {
			return nil, err
		}

		var connType string
		var configEnc []byte
		err := db.QueryRow(ctx.Context,
			`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
			req.ConnectorID, ctx.OrgID,
		).Scan(&connType, &configEnc)
		if err != nil {
			return nil, fmt.Errorf("get connector: %w", err)
		}

		// We need the master key from ToolContext — add it
		// For now, use the DB connection directly to query information_schema
		// The agent's DB pool connects to the app DB, not the connector DB.
		// We need to establish a connection to the actual connector database.
		// This requires crypto.Decrypt + executor interface.

		return map[string]any{
			"status":  "not_implemented",
			"message": "Schema exploration requires connector-specific database connections. See Task 8 Step 2 for the full implementation approach.",
		}, nil
	}
}
```

**Step 2: Full implementation with connector DB connection**

The schemas exploration needs a connection to the actual connector database (Postgres or ClickHouse). Since the tool context doesn't currently have access to the master key, we need to:

1. Add `MasterKey []byte` to `ToolContext` in `internal/agent/types.go`
2. Modify the `ProcessMessage` loop in `engine.go` to populate it
3. Use `crypto.Decrypt` + `executor` to connect to connector DB and query schema

Add to `ToolContext` in `types.go`:
```go
type ToolContext struct {
	// ... existing fields ...
	MasterKey []byte
}
```

In `engine.go` ProcessMessage, add to toolCtx:
```go
toolCtx := &ToolContext{
	// ... existing fields ...
	MasterKey: masterKey,
}
```

Now implement the full handler (replace the stub from Step 1):

```go
import (
	// add these imports to the top of tools_notebook.go
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
)

func makeExploreSchemaHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ConnectorID string `json:"connector_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if err := ctx.CheckPermission("connector", req.ConnectorID, "view"); err != nil {
			return nil, err
		}

		// Fetch connector type and encrypted config
		var connType string
		var configEnc []byte
		err := db.QueryRow(ctx.Context,
			`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
			req.ConnectorID, ctx.OrgID,
		).Scan(&connType, &configEnc)
		if err != nil {
			return nil, fmt.Errorf("get connector: %w", err)
		}

		// Decrypt credentials
		plain, err := crypto.Decrypt(configEnc, ctx.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials: %w", err)
		}

		var cfg models.ConnectorConfig
		if err := json.Unmarshal(plain, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		// Build executor and query schema
		var exec executor.Executor
		switch models.ConnectorType(connType) {
		case models.ConnectorPostgres:
			exec, err = executor.NewPostgresExecutor(cfg)
		case models.ConnectorClickHouse:
			exec, err = executor.NewClickHouseExecutor(cfg)
		default:
			return nil, fmt.Errorf("unsupported connector type: %s", connType)
		}
		if err != nil {
			return nil, fmt.Errorf("connect to connector db: %w", err)
		}

		// Query schema depending on connector type
		var schemaQuery string
		switch models.ConnectorType(connType) {
		case models.ConnectorPostgres:
			schemaQuery = `SELECT table_schema, table_name, column_name, data_type, is_nullable
				FROM information_schema.columns
				WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
				ORDER BY table_schema, table_name, ordinal_position`
		case models.ConnectorClickHouse:
			schemaQuery = `SELECT database, table, name, type, '' as is_nullable
				FROM system.columns
				ORDER BY database, table, position`
		}

		result, err := exec.Query(ctx.Context, schemaQuery, nil)
		if err != nil {
			return nil, fmt.Errorf("query schema: %w", err)
		}

		// Group by table
		tables := make(map[string][]map[string]any)
		for _, row := range result.Rows {
			var tableName, colName, colType, nullable string
			if models.ConnectorType(connType) == models.ConnectorPostgres {
				var schemaName string
				if len(row) >= 5 {
					if s, ok := row[0].(string); ok { schemaName = s }
					if s, ok := row[1].(string); ok { tableName = s }
					if s, ok := row[2].(string); ok { colName = s }
					if s, ok := row[3].(string); ok { colType = s }
					if s, ok := row[4].(string); ok { nullable = s }
				}
				tableName = schemaName + "." + tableName
			} else {
				if len(row) >= 4 {
					if s, ok := row[1].(string); ok { tableName = s }
					if s, ok := row[2].(string); ok { colName = s }
					if s, ok := row[3].(string); ok { colType = s }
				}
			}
			tables[tableName] = append(tables[tableName], map[string]any{
				"name": colName, "type": colType, "nullable": nullable,
			})
		}

		var schema []map[string]any
		for tableName, columns := range tables {
			schema = append(schema, map[string]any{
				"table_name": tableName,
				"columns":    columns,
				"column_count": len(columns),
			})
		}

		return map[string]any{"tables": schema, "total_tables": len(schema)}, nil
	}
}
```

**Step 3: Add crypto and executor imports to tools_notebook.go**

Add to the import block at the top of `tools_notebook.go`:
```go
import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
)
```

**Step 4: Build and test**

Run: `task build`
Expected: Compiles without errors.

Run: `task test`
Expected: All existing tests pass.

**Step 5: Commit**

```bash
git add internal/agent/tools_notebook.go internal/agent/types.go internal/agent/engine.go
git commit -m "feat: add explore_schema tool for connector database schema discovery"
```

---

### Task 9: Task Tracking Tools (Backend)

**Files:**
- Modify: `internal/agent/tools_agent.go:11-63` (add task tools)
- Modify: `internal/agent/engine.go:15-21` (add task storage)
- Modify: `internal/agent/types.go:25-29` (add tasks_updated event)

**Step 1: Add task storage to Engine**

In `internal/agent/engine.go`, add a tasks map to the Engine struct:

```go
type Engine struct {
	registry *ToolRegistry
	session  *SessionStore
	llm      *LLMClient
	pool     *pgxpool.Pool
	mu       sync.Mutex
	tasks    map[string][]AgentTask  // sessionID -> tasks
}

type AgentTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending, in_progress, done
}
```

Initialize in `NewEngine`:
```go
return &Engine{
	registry: reg,
	session:  NewSessionStore(pool),
	pool:     pool,
	tasks:    make(map[string][]AgentTask),
}
```

**Step 2: Add tasks_updated event type**

In `internal/agent/types.go`, append a `TasksUpdated` field to EngineEvent:

```go
type EngineEvent struct {
	Type     string `json:"type"`
	CellID   string `json:"cell_id,omitempty"`
	Position int    `json:"position,omitempty"`
	Tasks    []AgentTask `json:"tasks,omitempty"`
}
```

Add `AgentTask` type to types.go:
```go
type AgentTask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
```

Add `EmitTasksUpdated` to `ToolContext`:
```go
func (tc *ToolContext) EmitTasksUpdated(tasks []AgentTask) {
	if tc.Events != nil {
		*tc.Events = append(*tc.Events, EngineEvent{Type: "tasks_updated", Tasks: tasks})
	}
}
```

**Step 3: Add task tool handlers to tools_agent.go**

Add the registration calls inside `RegisterAgentTools`, before the closing `}`:

```go
reg.Register(&ToolDef{
	Function: struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	}{
		Name:        "create_tasks",
		Description: "Create a task list for the current session to track progress on complex work. Use this to break down complex requests into smaller, trackable tasks.",
		Parameters:  `{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string","description":"Short unique identifier for the task"},"description":{"type":"string","description":"What needs to be done"}},"required":["id","description"]}}},"required":["tasks"]}`,
	},
	Handler: makeCreateTasksHandler(),
})

reg.Register(&ToolDef{
	Function: struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	}{
		Name:        "update_task",
		Description: "Update a task's status: pending, in_progress, or done",
		Parameters:  `{"type":"object","properties":{"task_id":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","done"]}},"required":["task_id","status"]}`,
	},
	Handler: makeUpdateTaskHandler(),
})

reg.Register(&ToolDef{
	Function: struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	}{
		Name:        "get_tasks",
		Description: "Get the current task list for this session",
		Parameters:  `{"type":"object","properties":{}}`,
	},
	Handler: makeGetTasksHandler(),
})
```

Add the handler functions:

```go
func makeCreateTasksHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tasks []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		tasks := make([]AgentTask, len(req.Tasks))
		for i, t := range req.Tasks {
			tasks[i] = AgentTask{ID: t.ID, Description: t.Description, Status: "pending"}
		}

		ctx.EmitTasksUpdated(tasks)

		return map[string]any{"tasks": tasks, "count": len(tasks)}, nil
	}
}

func makeUpdateTaskHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		// Return current tasks with updated status
		tasks := []AgentTask{{ID: req.TaskID, Description: "(updated)", Status: req.Status}}
		ctx.EmitTasksUpdated(tasks)

		return map[string]any{"task_id": req.TaskID, "status": req.Status}, nil
	}
}

func makeGetTasksHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		// Return empty; the state is emitted via events
		return map[string]any{"tasks": []AgentTask{}, "message": "Task state is tracked via events"}, nil
	}
}
```

**Step 4: Handle tasks_updated in the WS handler**

In `internal/api/agent_ws.go`, after the `cell_created` event emission block, add handling for `tasks_updated`:

```go
for _, evt := range events {
	switch evt.Type {
	case "cell_created":
		conn.WriteJSON(struct {
			Type     string `json:"type"`
			CellID   string `json:"cell_id"`
			Position int    `json:"position"`
		}{Type: evt.Type, CellID: evt.CellID, Position: evt.Position})
	case "tasks_updated":
		conn.WriteJSON(struct {
			Type  string      `json:"type"`
			Data  interface{} `json:"data"`
		}{Type: "tasks_updated", Data: evt.Tasks})
	}
}
```

Apply the same change to `handleAgentWSWithUpgrader`.

**Step 5: Build and test**

Run: `task build`
Expected: Compiles without errors.

**Step 6: Commit**

```bash
git add internal/agent/tools_agent.go internal/agent/engine.go internal/agent/types.go internal/api/agent_ws.go
git commit -m "feat: add agent task tracking tools (create, update, get tasks)"
```

---

### Task 10: Task List — Frontend UI

**Files:**
- Create: `web/src/components/TaskList.tsx`
- Modify: `web/src/components/AgentPanel.tsx:1-12,225-282`
- Modify: `web/src/types/agent.ts:91-102`

**Step 1: Add tasks_updated to WSMessage type**

In `web/src/types/agent.ts`, add to the `WSMessage` union:

```ts
export interface AgentTaskItem {
  id: string
  description: string
  status: 'pending' | 'in_progress' | 'done'
}

// In WSMessage union, add:
| { type: 'tasks_updated'; data: AgentTaskItem[] }
```

**Step 2: Create TaskList component**

Create `web/src/components/TaskList.tsx`:

```tsx
import { Check, Circle, Loader2 } from 'lucide-react'
import type { AgentTaskItem } from '../types/agent'

interface Props {
  tasks: AgentTaskItem[]
}

function statusIcon(status: string) {
  switch (status) {
    case 'done': return <Check size={12} style={{ color: 'var(--accent)' }} />
    case 'in_progress': return <Loader2 size={12} style={{ animation: 'spin 1s linear infinite', color: 'var(--accent)' }} />
    default: return <Circle size={12} style={{ color: 'var(--text-muted)' }} />
  }
}

export function TaskList({ tasks }: Props) {
  if (tasks.length === 0) return null

  return (
    <details open style={styles.container}>
      <summary style={styles.header}>Tasks ({tasks.filter(t => t.status === 'done').length}/{tasks.length})</summary>
      <div style={styles.list}>
        {tasks.map((task) => (
          <div key={task.id} style={{
            ...styles.item,
            ...(task.status === 'done' ? styles.doneItem : {}),
          }}>
            {statusIcon(task.status)}
            <span style={{
              ...styles.description,
              ...(task.status === 'done' ? styles.doneText : {}),
            }}>
              {task.description}
            </span>
          </div>
        ))}
      </div>
    </details>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    padding: '8px 12px',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border-light)',
    fontSize: 12,
  },
  header: {
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 11,
    fontWeight: 500,
    marginBottom: 4,
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
  },
  item: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '2px 0',
  },
  doneItem: {
    opacity: 0.5,
  },
  description: {
    color: 'var(--text-primary)',
    fontSize: 12,
  },
  doneText: {
    textDecoration: 'line-through',
  },
}
```

**Step 3: Integrate TaskList into AgentPanel**

In `AgentPanel.tsx`:

Add import:
```tsx
import { TaskList } from './TaskList'
import type { AgentTaskItem } from '../types/agent'
```

Add state:
```tsx
const [tasks, setTasks] = useState<AgentTaskItem[]>([])
```

Handle `tasks_updated` in the WS message switch:
```tsx
case 'tasks_updated':
  setTasks((prev) => {
    const incoming = msg.data as AgentTaskItem[]
    const merged = [...prev]
    for (const t of incoming) {
      const idx = merged.findIndex((m) => m.id === t.id)
      if (idx >= 0) {
        merged[idx] = { ...merged[idx], ...t }
      } else {
        merged.push(t)
      }
    }
    return merged
  })
  break
```

Render TaskList above messages (after the agent info bar):
```tsx
<TaskList tasks={tasks} />
```

**Step 4: Build and verify**

Run: `task build:web`
Expected: Compiles without TypeScript errors.

**Step 5: Commit**

```bash
git add web/src/components/TaskList.tsx web/src/components/AgentPanel.tsx web/src/types/agent.ts
git commit -m "feat: add task list UI component for agent task tracking"
```

---

### Task 11: Improved Cell Highlight & Scroll

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx:740-747`

**Step 1: Replace setTimeout with DOM polling**

In `NotebookPage.tsx`, replace the `onCellScrollTo` callback:

```tsx
onCellScrollTo={(cellId) => {
  let attempts = 0
  const maxAttempts = 50
  const interval = setInterval(() => {
    const el = document.getElementById('cell-' + cellId)
    if (el) {
      clearInterval(interval)
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('cell-flash')
      setTimeout(() => el.classList.remove('cell-flash'), 3000)
    } else if (++attempts >= maxAttempts) {
      clearInterval(interval)
    }
  }, 100)
}}
```

**Step 2: Update CSS for more prominent flash**

Add to `web/src/theme.css` or the relevant stylesheet:

```css
@keyframes cellFlash {
  0% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); }
  50% { box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.3); }
  100% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0); }
}

.cell-flash {
  animation: cellFlash 1.5s ease-out;
  border-left: 3px solid var(--accent) !important;
  transition: border-left-color 0.3s ease;
}

.cell-flash::after {
  animation: none;
  border-left-color: transparent;
}
```

**Step 3: Build and verify**

Run: `task build:web`
Expected: Compiles.

**Step 4: Commit**

```bash
git add web/src/pages/NotebookPage.tsx web/src/theme.css
git commit -m "feat: improve cell highlight with DOM polling and prominent flash animation"
```

---

## Verification Checklist

After all tasks are complete, run the full verification:

```bash
# Go tests
task test

# Frontend build
task build:web

# Full build
task build:all

# Lint
task check
```
