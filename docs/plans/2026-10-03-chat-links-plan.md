# Chat Links Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an ACL-gated `/chats/:id` deep link that opens any agent session — standalone or notebook-attached — as a full-page interactive chat for editors and the existing read-only live viewer for everyone else.

**Architecture:** Frontend-only. A new `ChatPage` branches on `GET /api/v1/sessions/:id`'s `can_edit`: editors get `AgentPanel` in a new `variant="page"` mode driven by `initialSessionId`; viewers get `SessionViewer` with a new `page` flag. The global docked AgentPanel/FAB are suppressed on `/chats/*`. Links are built client-side as `${origin}/chats/{id}` and copyable from `PermissionsPanel`. No backend, schema, or API changes — `checkSessionPermission` remains the only gate.

**Tech Stack:** React 19 + TypeScript + Vite, react-router-dom, TanStack Query, Vitest + Testing Library + MSW, Playwright.

**Design doc:** `docs/plans/2026-10-03-chat-links-design.md`

---

### Task 1: Chat link URL helper

**Files:**
- Create: `web/src/utils/chatLink.ts`
- Test: `web/src/utils/chatLink.test.ts`

**Step 1: Write the failing test**

```ts
// web/src/utils/chatLink.test.ts
import { describe, it, expect } from 'vitest'
import { chatLinkUrl } from './chatLink'

describe('chatLinkUrl', () => {
  it('builds an absolute /chats/:id URL from the given origin', () => {
    expect(chatLinkUrl('s-1', 'https://org1.aether.test')).toBe('https://org1.aether.test/chats/s-1')
  })

  it('defaults to the current origin so subdomain orgs keep working', () => {
    expect(chatLinkUrl('s-1')).toBe(`${window.location.origin}/chats/s-1`)
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/utils/chatLink.test.ts`
Expected: FAIL — `Failed to resolve import "./chatLink"`.

**Step 3: Write minimal implementation**

```ts
// web/src/utils/chatLink.ts
/** Absolute URL that opens a chat: `${origin}/chats/{sessionId}`. The origin
 * defaults to the current window so subdomain-based orgs resolve correctly. */
export function chatLinkUrl(sessionId: string, origin: string = window.location.origin): string {
  return `${origin}/chats/${sessionId}`
}
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/utils/chatLink.test.ts`
Expected: PASS (2 tests).

**Step 5: Commit**

```bash
git add web/src/utils/chatLink.ts web/src/utils/chatLink.test.ts
git commit -m "feat(web): add chat link URL helper"
```

---

### Task 2: Copy-link section in PermissionsPanel

**Files:**
- Modify: `web/src/components/PermissionsPanel.tsx` (component body ~line 634; styles ~line 1128)
- Test: `web/src/test/PermissionsPanel.test.tsx`

**Step 1: Write the failing test**

Append to `web/src/test/PermissionsPanel.test.tsx` (new top-level `describe`; the file already imports `http`, `HttpResponse`, `server`, `screen`, `fireEvent`, `waitFor`, `vi`, `renderPanel`):

```tsx
describe('Session chat link', () => {
  function renderSessionLinkPanel() {
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([])),
    )
    return renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
    })
  }

  test('shows the absolute chat link', async () => {
    renderSessionLinkPanel()
    const input = await screen.findByLabelText('Chat link')
    expect(input).toHaveValue(`${window.location.origin}/chats/s-1`)
  })

  test('copies the chat link to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    renderSessionLinkPanel()

    fireEvent.click(await screen.findByRole('button', { name: /copy chat link/i }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/chats/s-1`))
    expect(await screen.findByText('Copied')).toBeInTheDocument()
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/PermissionsPanel.test.tsx`
Expected: FAIL — `Unable to find a label with the text of: Chat link`.

**Step 3: Write implementation**

In `web/src/components/PermissionsPanel.tsx`:

1. Add the import next to `groupLabel` (line 5):

```tsx
import { chatLinkUrl } from '../utils/chatLink'
```

2. Add state next to `notebookError` (line 415):

```tsx
  const [linkCopied, setLinkCopied] = useState(false)
```

3. Add the handler next to `handleToggleInheritance` / `handleNotebookChange` (search for `async function handleNotebookChange`; place it after that function):

```tsx
  async function handleCopyChatLink() {
    try {
      await navigator.clipboard.writeText(chatLinkUrl(resourceId))
    } catch {
      // Clipboard access can be denied (e.g. non-secure context); the input
      // stays selectable as a fallback.
    }
    setLinkCopied(true)
    setTimeout(() => setLinkCopied(false), 1500)
  }
```

4. Insert the section at the top of the Body, right before `{sessionNotebookLink && canEdit && (` (line 635):

```tsx
          {resourceType === 'agent_session' && (
            <div style={styles.notebookInherit}>
              <span style={styles.notebookInheritText}>
                <span style={styles.notebookInheritTitle}>Chat link</span>
                <span style={styles.notebookInheritHint}>
                  Anyone with access can open this chat directly.
                </span>
              </span>
              <div style={styles.chatLinkRow}>
                <input
                  aria-label="Chat link"
                  readOnly
                  value={chatLinkUrl(resourceId)}
                  onFocus={(e) => e.currentTarget.select()}
                  style={styles.chatLinkInput}
                />
                <button
                  type="button"
                  aria-label="Copy chat link"
                  onClick={() => { void handleCopyChatLink() }}
                  style={styles.chatLinkCopy}
                >
                  {linkCopied ? 'Copied' : 'Copy'}
                </button>
              </div>
            </div>
          )}
```

5. Add styles next to `notebookInherit` (line 1128):

```tsx
  chatLinkRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    marginTop: 6,
  },
  chatLinkInput: {
    flex: 1,
    minWidth: 0,
    padding: '6px 8px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-secondary)',
    fontSize: 12,
  },
  chatLinkCopy: {
    flexShrink: 0,
    padding: '6px 10px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
    fontSize: 12,
    fontWeight: 500,
  },
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/test/PermissionsPanel.test.tsx`
Expected: PASS (all tests, including the existing notebook-link suite).

**Step 5: Commit**

```bash
git add web/src/components/PermissionsPanel.tsx web/src/test/PermissionsPanel.test.tsx
git commit -m "feat(web): expose copyable chat link in permissions panel"
```

---

### Task 3: AgentPanel `initialSessionId` resume path

**Files:**
- Modify: `web/src/components/AgentPanel.tsx` (props line 21; restore effect line 645; new effect after line 701; resume extract near line 1057; resize effect line 1326)
- Test: Create `web/src/test/AgentPanel.page.test.tsx`

**Step 1: Write the failing test**

Create `web/src/test/AgentPanel.page.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { AgentPanel } from '../components/AgentPanel'
import { server } from './server'

const AGENT = {
  id: 'a1',
  org_id: 'org-1',
  name: 'Test Agent',
  skill_ids: [],
  tool_ids: [],
  mcp_server_ids: [],
  mcp_servers: [],
  created_by: 'u1',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

const SESSION = {
  id: 's1',
  agent_id: 'a1',
  notebook_id: 'nb-1',
  user_id: 'u1',
  max_turns: 10,
  title: 'Revenue chat',
  created_at: '2026-09-14T00:00:00Z',
  owner_email: 'alice@test.com',
  shared: false,
  can_edit: true,
  share_with_notebook_viewers: false,
}

const MESSAGES = [
  { id: 'm1', session_id: 's1', role: 'user', content: 'hello from owner', created_at: '2026-09-14T00:00:01Z' },
]

class MockWebSocket {
  static instances: MockWebSocket[] = []
  static OPEN = 1
  static CLOSED = 3
  url: string
  readyState = MockWebSocket.OPEN
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sent: string[] = []
  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }
  send(data: string) { this.sent.push(data) }
  close() { this.readyState = MockWebSocket.CLOSED }
}

const realWebSocket = globalThis.WebSocket

function renderPagePanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <AgentPanel variant="page" initialSessionId="s1" onClose={() => {}} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
  // A different localStorage session must NOT win over initialSessionId.
  localStorage.setItem('aether:lastAgentId', 'a1')
  localStorage.setItem('aether:lastSessionId', 's-other')
  server.use(
    http.get('/api/v1/agents', () => HttpResponse.json([AGENT])),
    http.get('/api/v1/model-configs', () => HttpResponse.json([])),
    http.get('/api/v1/agents/sessions/s1/usage', () => new HttpResponse(null, { status: 404 })),
    http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
    http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
  )
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('AgentPanel page mode', () => {
  it('opens initialSessionId instead of the localStorage session', async () => {
    renderPagePanel()

    expect(await screen.findByText('hello from owner')).toBeInTheDocument()
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
    const ws = MockWebSocket.instances[MockWebSocket.instances.length - 1]
    expect(ws.url).toContain('/api/v1/ws/agents/s1')
    expect(ws.url).not.toContain('s-other')
    expect(localStorage.getItem('aether:lastSessionId')).toBe('s1')
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.page.test.tsx`
Expected: FAIL — `variant`/`initialSessionId` are not props (TS) and the panel opens `s-other`.

**Step 3: Write implementation**

In `web/src/components/AgentPanel.tsx`:

1. Props (line 21) — make panel chrome optional and add the new props:

```tsx
interface AgentPanelProps {
  notebookId?: string
  pageContext?: { type: 'notebook' | 'dashboard' | 'files'; id?: string; title?: string }
  width?: number
  onResize?: (width: number) => void
  onClose?: () => void
  onMinimize?: () => void
  onDock?: () => void
  docked?: boolean
  /** 'page' renders a full-page chat: no resize/dock/minimize chrome. */
  variant?: 'panel' | 'page'
  /** When set, the panel opens this session instead of restoring the
   * localStorage session (used by the /chats/:id page). */
  initialSessionId?: string
}
```

2. Add `AgentSession` to the type imports (line 8):

```tsx
import type { Agent, AgentSession, AgentTaskItem, ModelConfig, SessionUsage, TokenBreakdown, WSMessage } from '../types/agent'
```

3. Destructure the new props. Find `export function AgentPanel({ notebookId, pageContext, width, ...` (line 304) and replace the destructuring with:

```tsx
export function AgentPanel({ notebookId, pageContext, width, onResize, onClose, onMinimize, onDock, docked, variant = 'panel', initialSessionId }: AgentPanelProps) {
  const pageMode = variant === 'page'
```

4. Guard the restore effect (line 646) so it never races the route session:

```tsx
    if (!selectedAgent && agents.length > 0 && !isLoadingAgents && !initialSessionId) {
```

5. Add a new effect immediately after the restore effect's closing `}, [agents, isLoadingAgents])` (line 701):

```tsx
  // Page mode: the route owns the session id. Open it instead of the
  // localStorage restore path. ChatPage keeps the URL in sync afterwards, and
  // the sessionIdRef guard skips the refetch/reconnect when the URL already
  // points at the session this panel just opened.
  useEffect(() => {
    if (!initialSessionId || isLoadingAgents || agents.length === 0) return
    if (sessionIdRef.current === initialSessionId) return
    let cancelled = false
    void (async () => {
      try {
        const sess = await api.get<AgentSession>(`/api/v1/sessions/${initialSessionId}`)
        if (cancelled) return
        const agent = agents.find((a) => a.id === sess.agent_id)
        if (agent) setSelectedAgent(agent)
        localStorage.setItem(LAST_AGENT_KEY, sess.agent_id)
        await resumeSession(initialSessionId, agent?.id ?? sess.agent_id)
      } catch {
        if (!cancelled) setError('Failed to open session')
      }
    })()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialSessionId, agents, isLoadingAgents])
```

6. Extract the history-resume sequence into `resumeSession`; place it right before `const connectToSession` (line 1057):

```tsx
  // resumeSession opens an existing session by id: seed the transcript from
  // REST, then let the WS reconnect_sync take over. Shared by the history
  // resume action and the /chats/:id page.
  const resumeSession = async (sid: string, agentIdForSave?: string) => {
    closeWS()
    setSessionId(sid)
    resetMeterState()
    try {
      const msgs = await api.get<Array<{ id: string; role: string; content: string; reasoning_content?: string; image_ids?: string[]; duration_ms?: number; tokens_direct?: number; tokens_after?: number; created_at?: string }>>(`/api/v1/sessions/${sid}/messages`)
      const formatted = mapServerMessagesToChat(msgs)
      setMessages(formatted)
      if (formatted.some((m) => m.role === 'compaction')) setHasCompacted(true)
      const aid = agentIdForSave ?? selectedAgentRef.current?.id
      if (aid) saveChatState(aid, sid, formatted, undefined)
    } catch {
      setMessages([])
    }
    setIsStreaming(false)
    connectWebSocket(sid)
  }
```

7. Replace the `onResumeSession` body (lines 1434–1452) with:

```tsx
          onResumeSession={async (session) => {
            setShowHistory(false)
            await resumeSession(session.id)
          }}
```

8. Make the resize effect safe when `width`/`onResize` are absent (line 1326):

```tsx
  useEffect(() => {
    const handle = resizeRef.current
    if (!handle || !onResize) return
```

and line 1336 becomes:

```tsx
      startWidth = width ?? 460
```

with the effect deps unchanged (`[width, onResize]`).

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.page.test.tsx src/test/AgentPanel.sharing.test.tsx src/test/AgentPanel.tokens.test.tsx`
Expected: PASS — new test plus both existing suites (regression gate for `onResumeSession`).

**Step 5: Commit**

```bash
git add web/src/components/AgentPanel.tsx web/src/test/AgentPanel.page.test.tsx
git commit -m "feat(web): open an explicit session in AgentPanel via initialSessionId"
```

---

### Task 4: AgentPanel page variant layout

**Files:**
- Modify: `web/src/components/AgentPanel.tsx` (render line 1362; header line 1420; styles line 1983)
- Modify: `web/src/components/PanelHeader.tsx` (props line 4; close button line 37)
- Test: `web/src/test/AgentPanel.page.test.tsx`

**Step 1: Write the failing test**

Append inside `describe('AgentPanel page mode')`:

```tsx
  it('renders full-page chrome: no resize/dock/minimize, a Back affordance', async () => {
    renderPagePanel()
    await screen.findByText('hello from owner')

    expect(screen.queryByTitle('Minimize')).toBeNull()
    expect(screen.queryByTitle('Dock to right side')).toBeNull()
    expect(screen.queryByTitle('Undock panel')).toBeNull()
    expect(screen.getByTitle('Back')).toBeInTheDocument()
  })
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.page.test.tsx`
Expected: FAIL — page mode still renders the standard close button (no `Back` title).

**Step 3: Write implementation**

1. `web/src/components/PanelHeader.tsx` — add a `back` prop and swap the icon:

```tsx
import { X, ChevronDown, PanelRightOpen, PanelRightClose, ArrowLeft } from 'lucide-react'

interface Props {
  title: string
  onClose?: () => void
  onMinimize?: () => void
  onDock?: () => void
  docked?: boolean
  back?: boolean
  closeTitle?: string
  style?: React.CSSProperties
}

export function PanelHeader({ title, onClose, onMinimize, onDock, docked, back, closeTitle = 'Close', style }: Props) {
```

and inside the close button (line 43):

```tsx
            {back ? <ArrowLeft size={14} /> : <X size={13} />}
```

2. `web/src/components/AgentPanel.tsx`:

a. Root element (line 1363):

```tsx
    <div ref={panelRef} style={{ ...styles.panel, ...(pageMode ? styles.pagePanel : { width }) }}>
```

b. Wrap the resize handle (line 1416) so page mode omits it:

```tsx
      {!pageMode && (
        <div
          ref={resizeRef}
          style={styles.resizeHandle}
        />
      )}
```

c. Header (line 1420) — hide dock/minimize and show Back:

```tsx
      <PanelHeader
        title={sessionTitle || (selectedAgent ? selectedAgent.name : 'AI Agent')}
        onClose={onClose}
        onMinimize={pageMode ? undefined : onMinimize}
        onDock={pageMode ? undefined : onDock}
        docked={docked}
        back={pageMode}
        closeTitle={pageMode ? 'Back' : 'Close agent panel'}
        style={{ borderBottom: '1px solid var(--border)', flexShrink: 0 }}
      />
```

d. Add `pagePanel` to `styles` (after `panel`, line 1994):

```tsx
  pagePanel: {
    width: '100%',
    flex: 1,
    borderLeft: 'none',
  },
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.page.test.tsx src/test/AgentPanel.sharing.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AgentPanel.tsx web/src/components/PanelHeader.tsx web/src/test/AgentPanel.page.test.tsx
git commit -m "feat(web): add full-page AgentPanel variant"
```

---

### Task 5: AgentPanel URL sync via `onSessionChange`

**Files:**
- Modify: `web/src/components/AgentPanel.tsx` (props; `startSession` line 1011; `connectToSession` line 1057; `resumeSession` from Task 3)
- Test: `web/src/test/AgentPanel.page.test.tsx`

**Step 1: Write the failing test**

Append inside `describe('AgentPanel page mode')`:

```tsx
  it('reports a newly started session so the page can update the URL', async () => {
    const onSessionChange = vi.fn()
    server.use(
      http.post('/api/v1/agents/:id/session', () => HttpResponse.json({ session_id: 's-new' })),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AgentPanel variant="page" initialSessionId="s1" onClose={() => {}} onSessionChange={onSessionChange} />
      </QueryClientProvider>,
    )
    await screen.findByText('hello from owner')

    act(() => {
      window.dispatchEvent(new CustomEvent('aether:new-agent-chat'))
    })

    await waitFor(() => expect(onSessionChange).toHaveBeenCalledWith('s-new'))
  })
```

(Add `act` to the `@testing-library/react` import at the top of the file.)

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.page.test.tsx`
Expected: FAIL — `onSessionChange` is not called / not a prop.

**Step 3: Write implementation**

1. Add to `AgentPanelProps`:

```tsx
  /** Called whenever the active session changes, so /chats/:id can keep the
   * address bar pointing at the open chat. */
  onSessionChange?: (sessionId: string) => void
```

2. Destructure it in the component signature.

3. Call it after each session switch:

- In `startSession`, after `connectWebSocket(res.session_id, { seedUsage: false })` (line 1027):

```tsx
      onSessionChange?.(res.session_id)
```

- In `connectToSession`, after `connectWebSocket(sessionID, { seedUsage: false })` (line 1065):

```tsx
    onSessionChange?.(sessionID)
```

- In `resumeSession` (from Task 3), after `connectWebSocket(sid)`:

```tsx
    onSessionChange?.(sid)
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.page.test.tsx src/test/AgentPanel.sharing.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/AgentPanel.tsx web/src/test/AgentPanel.page.test.tsx
git commit -m "feat(web): let AgentPanel keep the chat URL in sync"
```

---

### Task 6: SessionViewer page mode

**Files:**
- Modify: `web/src/components/SessionViewer.tsx` (props line 16; render line 452; close button line 478)
- Test: `web/src/components/SessionViewer.test.tsx`

**Step 1: Write the failing test**

Append inside `describe('SessionViewer')`:

```tsx
  it('renders as a page region with a Back affordance when page is set', async () => {
    const onClose = vi.fn()
    renderWithProviders(<SessionViewer sessionId="s1" page onClose={onClose} />)
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
    await screen.findByText('hello from owner')

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.getByRole('region', { name: 'Agent chat' })).toBeInTheDocument()
    fireEvent.click(screen.getByTitle('Back'))
    expect(onClose).toHaveBeenCalled()
  })
```

Add `vi` is already imported; `fireEvent` is imported. `renderWithProviders` is imported.

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/components/SessionViewer.test.tsx`
Expected: FAIL — `page` is not a prop and the root is still a dialog.

**Step 3: Write implementation**

In `web/src/components/SessionViewer.tsx`:

1. Props (line 16) — add:

```tsx
  /** Full-page presentation: region semantics and a Back affordance. */
  page?: boolean
```

2. Destructure `page` in the component signature (line 36).

3. Root (line 453):

```tsx
    <div style={styles.panel} role={page ? 'region' : 'dialog'} aria-label={page ? 'Agent chat' : 'Shared agent session'}>
```

4. Close button (line 478):

```tsx
        {onClose && (
          <button type="button" style={styles.closeBtn} onClick={onClose} title={page ? 'Back' : 'Close viewer'} aria-label={page ? 'Back' : 'Close viewer'}>
            {page ? <ArrowLeft size={14} /> : <X size={14} />}
          </button>
        )}
```

(`ArrowLeft` and `X` are already imported at line 2.)

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/components/SessionViewer.test.tsx`
Expected: PASS (all existing tests plus the new one).

**Step 5: Commit**

```bash
git add web/src/components/SessionViewer.tsx web/src/components/SessionViewer.test.tsx
git commit -m "feat(web): add page mode to SessionViewer"
```

---

### Task 7: ChatPage + `/chats/:id` route

**Files:**
- Create: `web/src/pages/ChatPage.tsx`
- Modify: `web/src/App.tsx` (imports ~line 33; routes ~line 146)
- Test: Create `web/src/test/ChatPage.test.tsx`

**Step 1: Write the failing test**

Create `web/src/test/ChatPage.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { Routes, Route } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { ChatPage } from '../pages/ChatPage'
import { server } from './server'
import { renderWithProviders } from './utils'

const SESSION = {
  id: 's1',
  agent_id: 'a1',
  notebook_id: '',
  user_id: 'u1',
  max_turns: 10,
  title: 'Revenue analysis',
  created_at: '2026-09-29T10:00:00Z',
  owner_email: 'owner@example.com',
  shared: false,
  can_edit: false,
  share_with_notebook_viewers: false,
}

const MESSAGES = [
  { id: 'm1', session_id: 's1', role: 'user', content: 'hello from owner', created_at: '2026-09-29T10:00:01Z' },
]

class MockWebSocket {
  static instances: MockWebSocket[] = []
  static OPEN = 1
  static CLOSED = 3
  url: string
  readyState = MockWebSocket.OPEN
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sent: string[] = []
  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }
  send(data: string) { this.sent.push(data) }
  close() { this.readyState = MockWebSocket.CLOSED }
}

const realWebSocket = globalThis.WebSocket

function renderChatPage(path = '/chats/s1') {
  return renderWithProviders(
    <Routes>
      <Route path="/chats/:id" element={<ChatPage />} />
    </Routes>,
    { initialPath: path },
  )
}

beforeEach(() => {
  localStorage.clear()
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('ChatPage', () => {
  it('renders the read-only live viewer for a shared session', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
    )
    renderChatPage()

    expect(await screen.findByText('hello from owner')).toBeInTheDocument()
    expect(screen.getByText('Shared · Read-only')).toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/Message agent/)).toBeNull()
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
  })

  it('renders the interactive page-mode panel for an editor', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json({ ...SESSION, shared: false, can_edit: true })),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
      http.get('/api/v1/agents', () => HttpResponse.json([{
        id: 'a1', org_id: 'org-1', name: 'Test Agent', skill_ids: [], tool_ids: [],
        mcp_server_ids: [], mcp_servers: [], created_by: 'u1',
        created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
      }])),
      http.get('/api/v1/model-configs', () => HttpResponse.json([])),
      http.get('/api/v1/agents/sessions/s1/usage', () => new HttpResponse(null, { status: 404 })),
    )
    renderChatPage()

    expect(await screen.findByPlaceholderText(/Message agent/)).toBeInTheDocument()
    expect(screen.getByTitle('Back')).toBeInTheDocument()
    expect(screen.queryByText('Shared · Read-only')).toBeNull()
  })

  it('shows a no-access state on 403', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => new HttpResponse(null, { status: 403 })),
    )
    renderChatPage()

    expect(await screen.findByText(/don't have access to this chat/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /go home/i })).toBeInTheDocument()
  })

  it('shows a not-found state on 404', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => new HttpResponse(null, { status: 404 })),
    )
    renderChatPage()

    expect(await screen.findByText(/Chat not found or has been deleted/i)).toBeInTheDocument()
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/ChatPage.test.tsx`
Expected: FAIL — `Failed to resolve import "../pages/ChatPage"`.

**Step 3: Write implementation**

Create `web/src/pages/ChatPage.tsx`:

```tsx
import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { AppShell } from '../components/AppShell'
import { AgentPanel } from '../components/AgentPanel'
import { SessionViewer } from '../components/SessionViewer'
import { api, ApiError } from '../api/client'
import type { AgentSession } from '../types/agent'

type LoadError = 'forbidden' | 'notfound' | 'error'

// ChatPage is the /chats/:id route: the shareable deep link for an agent chat.
// Access is ACL-gated by the API — the link itself grants nothing. Editors
// (owner, or org admin in admin mode) get the interactive full-page panel;
// everyone else gets the read-only live SessionViewer.
export function ChatPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [session, setSession] = useState<AgentSession | null>(null)
  const [loadError, setLoadError] = useState<LoadError | null>(null)

  useEffect(() => {
    if (!id) return
    let cancelled = false
    setSession(null)
    setLoadError(null)
    api.get<AgentSession>(`/api/v1/sessions/${id}`)
      .then((s) => { if (!cancelled) setSession(s) })
      .catch((e: unknown) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 403) setLoadError('forbidden')
        else if (e instanceof ApiError && e.status === 404) setLoadError('notfound')
        else setLoadError('error')
      })
    return () => { cancelled = true }
  }, [id])

  useEffect(() => {
    document.title = session?.title ? `${session.title} · Aether` : 'Chat · Aether'
  }, [session?.title])

  let body: ReactNode
  if (loadError) {
    const message = loadError === 'forbidden'
      ? "You don't have access to this chat. Ask the chat owner to share it with you."
      : loadError === 'notfound'
        ? 'Chat not found or has been deleted.'
        : 'Could not load this chat.'
    body = (
      <div style={styles.state}>
        <div style={styles.stateText}>{message}</div>
        <Link to="/" style={styles.stateLink}>Go home</Link>
      </div>
    )
  } else if (!session) {
    body = <div style={styles.state}><div style={styles.stateText}>Loading chat…</div></div>
  } else if (session.can_edit) {
    body = (
      <AgentPanel
        variant="page"
        initialSessionId={id}
        onSessionChange={(sid) => navigate(`/chats/${sid}`, { replace: true })}
        onClose={() => navigate(-1)}
      />
    )
  } else {
    body = <SessionViewer sessionId={id!} session={session} page onClose={() => navigate(-1)} />
  }

  return (
    <AppShell noPadding>
      <div style={styles.page}>{body}</div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    display: 'flex',
    flexDirection: 'column',
    flex: 1,
    minHeight: 0,
  },
  state: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 12,
    flex: 1,
    padding: 40,
  },
  stateText: {
    color: 'var(--text-secondary)',
    fontSize: 14,
    textAlign: 'center',
    maxWidth: 420,
  },
  stateLink: {
    color: 'var(--accent)',
    fontSize: 13,
    textDecoration: 'none',
  },
}
```

Add the route in `web/src/App.tsx`:

1. Import (alphabetical position after `ChatPage`-adjacent imports is not enforced; put after line 7 `HomePage`):

```tsx
import { ChatPage } from './pages/ChatPage'
```

2. Route — add after the `/notebooks/:id` route block (line 66):

```tsx
      <Route
        path="/chats/:id"
        element={
          <ProtectedRoute>
            <ChatPage />
          </ProtectedRoute>
        }
      />
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/test/ChatPage.test.tsx`
Expected: PASS (4 tests).

**Step 5: Commit**

```bash
git add web/src/pages/ChatPage.tsx web/src/App.tsx web/src/test/ChatPage.test.tsx
git commit -m "feat(web): add /chats/:id page for shareable agent chats"
```

---

### Task 8: Suppress the global agent panel on chat pages

**Files:**
- Modify: `web/src/components/AppShell.tsx` (Ctrl+K effect line 132; new-agent event line 149; render lines 171–263)
- Test: `web/src/test/AppShell.test.tsx`

**Step 1: Write the failing test**

Append to `web/src/test/AppShell.test.tsx`:

```tsx
  it('hides the global agent panel and FAB on chat pages', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/chats/s-1' })
    expect(screen.queryByTitle('Open AI Agent (Ctrl+K)')).toBeNull()
  })

  it('shows the agent FAB away from chat pages', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/' })
    expect(screen.getByTitle('Open AI Agent (Ctrl+K)')).toBeInTheDocument()
  })
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/test/AppShell.test.tsx`
Expected: FAIL — FAB is rendered on `/chats/s-1`.

**Step 3: Write implementation**

In `web/src/components/AppShell.tsx`:

1. After `const location = useLocation()` (line 94):

```tsx
  // The chat page renders its own full-page chat; a second (global) panel would
  // fight it over singleton storage keys and the session WebSocket.
  const isChatPage = location.pathname.startsWith('/chats/')
```

2. Ctrl+K effect (line 132) — guard and update deps:

```tsx
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (isChatPage) return
      const tag = (e.target as HTMLElement).tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        setShowGlobalAgent(v => !v)
        setGlobalAgentMinimized(false)
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [isChatPage])
```

3. `aether:new-agent-chat` listener (line 149) — guard and update deps:

```tsx
  useEffect(() => {
    const handler = () => {
      if (isChatPage) return
      setShowGlobalAgent(true)
      setGlobalAgentMinimized(false)
    }
    window.addEventListener('aether:new-agent-chat', handler)
    return () => window.removeEventListener('aether:new-agent-chat', handler)
  }, [isChatPage])
```

4. Render: add `!isChatPage` to the FAB (line 192), the floating wrapper (line 203), the minimized bar (line 251), and the docked padding condition (line 173):

```tsx
        ...(showGlobalAgent && !globalAgentMinimized && globalAgentDocked && !isChatPage ? { paddingRight: globalAgentWidth } : {}),
```

```tsx
      {!isChatPage && !showGlobalAgent && (
```

```tsx
      {!isChatPage && showGlobalAgent && !globalAgentMinimized && (
```

```tsx
      {!isChatPage && showGlobalAgent && globalAgentMinimized && (
```

**Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run --project=default src/test/AppShell.test.tsx`
Expected: PASS (5 tests).

**Step 5: Commit**

```bash
git add web/src/components/AppShell.tsx web/src/test/AppShell.test.tsx
git commit -m "feat(web): suppress global agent panel on chat pages"
```

---

### Task 9: E2E — extract shared session helpers, add chat-links spec

**Files:**
- Create: `e2e/agent-session-helpers.ts`
- Modify: `e2e/session-sharing.spec.ts` (replace local helper block, lines 19–237, with imports)
- Create: `e2e/chat-links.spec.ts`

**Step 1: Extract helpers**

Move the following declarations **verbatim** from `e2e/session-sharing.spec.ts` into the new `e2e/agent-session-helpers.ts`, prefixing each with `export` and adding the needed imports:

- `BASE_URL` (line 19) → `export const BASE_URL`
- `PASSWORD` (line 20) → `export const PASSWORD`
- `FakeLlm` interface (line 22) → export
- `startFakeLlm` (line 32) → export
- `counter`, `uniqueSuffix` (lines 67–71) → export
- `authHeaders` (line 73) → export
- `tokenFrom` (line 77) → export
- `Member`, `listMembers` (lines 83–94) → export
- `registerViaApi` (line 98) → export
- `joinViaInvite` (line 107) → export
- `SessionFixture`, `provisionSession` (lines 126–186) → export
- `sendWsMessage` (line 192) → export
- `putSessionShares` (line 229) → export

File header imports: `import { expect, type APIRequestContext, type Page } from '@playwright/test'` and `import http from 'node:http'` and `import type { AddressInfo } from 'node:net'`.

Leave `expectSessionFlag`, `openChatsDrawer`, `viewerDialog`, `permissionsDialog`, `openAgentHistory`, `fakeLlm`, the `beforeAll`/`afterAll`, and all tests in `session-sharing.spec.ts`. Replace its top imports with:

```ts
import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { registerAndOnboard, login } from './helpers'
import {
  BASE_URL,
  PASSWORD,
  startFakeLlm,
  uniqueSuffix,
  authHeaders,
  tokenFrom,
  listMembers,
  registerViaApi,
  joinViaInvite,
  provisionSession,
  sendWsMessage,
  putSessionShares,
  type FakeLlm,
} from './agent-session-helpers'
```

(Keep only the names the spec still uses; unused imports fail lint. Verify with `npx tsc --noEmit` from `web`? E2E is not in the web tsconfig — run `npx playwright test --list` from the repo root to type-check the spec files.)

**Step 2: Verify the existing suite still parses**

Run: `npx playwright test --config=e2e/playwright.config.ts --list`
Expected: lists all tests from both `session-sharing.spec.ts` and the not-yet-created `chat-links.spec.ts` (so run this once the new file exists).

**Step 3: Write the new E2E spec**

Create `e2e/chat-links.spec.ts`:

```ts
import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { registerAndOnboard, login } from './helpers'
import {
  BASE_URL,
  PASSWORD,
  joinViaInvite,
  listMembers,
  provisionSession,
  putSessionShares,
  registerViaApi,
  sendWsMessage,
  startFakeLlm,
  tokenFrom,
  uniqueSuffix,
  type FakeLlm,
} from './agent-session-helpers'

// End-to-end coverage for /chats/:id deep links: ACL-gated shareable links for
// standalone and notebook-attached agent chats. Owners get the full-page
// interactive chat; recipients get the read-only live viewer.

let fakeLlm: FakeLlm

test.beforeAll(async () => {
  fakeLlm = await startFakeLlm()
})

test.afterAll(async () => {
  await fakeLlm.close()
})

async function ownerWithSession(
  request: APIRequestContext,
  page: Page,
  opts: { withNotebook?: boolean } = {},
) {
  const suffix = uniqueSuffix()
  await registerAndOnboard(page, suffix)
  const token = await tokenFrom(page)
  const headers = { Authorization: `Bearer ${token}` }
  const fixture = await provisionSession(request, headers, { withNotebook: opts.withNotebook ?? false })
  return { token, headers, ...fixture }
}

async function shareWithNewRecipient(
  request: APIRequestContext,
  ownerHeaders: Record<string, string>,
  sessionId: string,
) {
  const email = `e2e-recipient-${uniqueSuffix()}@example.com`
  const onboarding = await registerViaApi(request, email, 'E2E Recipient')
  await joinViaInvite(request, ownerHeaders, onboarding)
  const members = await listMembers(request, ownerHeaders)
  const recipient = members.find((m) => m.email === email)
  expect(recipient).toBeTruthy()
  await putSessionShares(request, ownerHeaders, sessionId, [
    { subject_type: 'user', subject_id: recipient!.user_id, actions: ['view'] },
  ])
  return { email, userId: recipient!.user_id }
}

// loginKeepingRedirect logs in without the shared helper's waitForURL('/'):
// ProtectedRoute stores the deep link and LoginPage returns to it after auth.
async function loginKeepingRedirect(page: Page, email: string, password: string, returnPath: string) {
  await page.goto('/login')
  await page.fill('input[type="email"]', email)
  await page.locator('input[type="email"]').press('Enter')
  await page.fill('input[type="password"]', password)
  await page.locator('input[type="password"]').press('Enter')
  await page.waitForURL((url) => url.pathname === returnPath, { timeout: 20_000 })
}

test.describe('chat links', () => {
  test('owner opens their standalone chat link in an interactive full-page chat', async ({ page, request }) => {
    const { sessionId } = await ownerWithSession(request, page)

    await page.goto(`/chats/${sessionId}`)
    const composer = page.getByPlaceholder(/Message agent/)
    await expect(composer).toBeVisible({ timeout: 20_000 })
    await composer.fill('hello from the link')
    await composer.press('Enter')
    await expect(page.getByText('Echo: hello from the link')).toBeVisible({ timeout: 20_000 })
  })

  test('a shared recipient opens the link read-only and sees live updates', async ({ browser, request }) => {
    const ownerPage = await browser.newPage()
    const { token, headers, sessionId } = await ownerWithSession(request, ownerPage)
    await sendWsMessage(sessionId, token, 'first message')
    const recipient = await shareWithNewRecipient(request, headers, sessionId)

    const recipientPage = await browser.newPage()
    await login(recipientPage, recipient.email, PASSWORD)
    await recipientPage.goto(`/chats/${sessionId}`)

    await expect(recipientPage.getByText('Shared · Read-only')).toBeVisible({ timeout: 20_000 })
    await expect(recipientPage.getByText('first message')).toBeVisible()
    await expect(recipientPage.getByPlaceholder(/Message agent/)).toHaveCount(0)

    await sendWsMessage(sessionId, token, 'live update')
    await expect(recipientPage.getByText('Echo: live update')).toBeVisible({ timeout: 20_000 })

    await ownerPage.close()
    await recipientPage.close()
  })

  test('a logged-out visitor returns to the chat link after login', async ({ browser, page, request }) => {
    const ownerPage = await browser.newPage()
    const { headers, sessionId } = await ownerWithSession(request, ownerPage)
    const recipient = await shareWithNewRecipient(request, headers, sessionId)

    await page.goto(`/chats/${sessionId}`)
    await expect(page).toHaveURL(/\/login/)
    await loginKeepingRedirect(page, recipient.email, PASSWORD, `/chats/${sessionId}`)

    await expect(page.getByText('Shared · Read-only')).toBeVisible({ timeout: 20_000 })
    await ownerPage.close()
  })

  test('an unauthorized user sees no-access, and a bad id shows not-found', async ({ browser, page, request }) => {
    const ownerPage = await browser.newPage()
    const { sessionId } = await ownerWithSession(request, ownerPage)

    await registerAndOnboard(page, uniqueSuffix()) // a separate user in their own org
    await page.goto(`/chats/${sessionId}`)
    await expect(page.getByText(/don't have access to this chat/i)).toBeVisible({ timeout: 20_000 })

    await page.goto('/chats/00000000-0000-0000-0000-000000000000')
    await expect(page.getByText(/Chat not found or has been deleted/i)).toBeVisible({ timeout: 20_000 })
    await ownerPage.close()
  })

  test('the permissions panel exposes the exact chat link', async ({ page, request }) => {
    const { sessionId } = await ownerWithSession(request, page)

    await page.goto(`/chats/${sessionId}`)
    await page.getByRole('button', { name: /Share this session/ }).click()
    const dialog = page.getByRole('dialog', { name: /permissions/i })
    await expect(dialog.getByLabel('Chat link')).toHaveValue(`${BASE_URL}/chats/${sessionId}`)
  })

  test('a notebook-attached session link resolves the same way', async ({ page, request }) => {
    const { sessionId } = await ownerWithSession(request, page, { withNotebook: true })

    await page.goto(`/chats/${sessionId}`)
    await expect(page.getByPlaceholder(/Message agent/)).toBeVisible({ timeout: 20_000 })
  })
})
```

**Step 4: Run the E2E spec (dev stack required)**

```bash
docker compose -f docker-compose.dev.yml up -d
npx playwright test --config=e2e/playwright.config.ts chat-links.spec.ts
```

Expected: 6 passed. If a registration rate-limit error appears, the dev compose already raises register/login limits (see the header comment in `session-sharing.spec.ts`).

**Step 5: Commit**

```bash
git add e2e/agent-session-helpers.ts e2e/session-sharing.spec.ts e2e/chat-links.spec.ts
git commit -m "test(e2e): cover shareable chat links"
```

---

### Task 10: Docs + full validation

**Files:**
- Modify: `AGENTS.md` (session linkage paragraph)
- Modify: `docs/plans/2026-10-03-chat-links-design.md` (status only, optional)

**Step 1: Update AGENTS.md**

In the **Session notebook linkage** paragraph (search for `**Session notebook linkage**`), append this sentence at the end:

```
Any session — standalone or notebook-attached — is directly addressable at `/chats/{id}`; the link is ACL-gated (the URL itself grants nothing), rendering an interactive full-page chat for editors and the read-only live viewer for recipients.
```

**Step 2: Frontend typecheck and build**

Run:

```bash
cd web && npx tsc --noEmit
cd web && npm run build
```

Expected: both clean (`tsc -b` in `npm run build` is stricter than `--noEmit`).

**Step 3: Targeted unit suites**

Run:

```bash
cd web && npx vitest run --project=default src/utils/chatLink.test.ts src/test/PermissionsPanel.test.tsx src/test/AgentPanel.page.test.tsx src/test/ChatPage.test.tsx src/test/AppShell.test.tsx src/components/SessionViewer.test.tsx src/test/AgentPanel.sharing.test.tsx src/test/AgentPanel.tokens.test.tsx src/test/SessionHistory.test.tsx
```

Expected: all pass.

**Step 4: Full E2E for the touched suites**

Run:

```bash
npx playwright test --config=e2e/playwright.config.ts chat-links.spec.ts session-sharing.spec.ts
```

Expected: all pass (the session-sharing suite guards against regressions in resume/share/WS).

**Step 5: Real-browser manual validation (mandatory per AGENTS.md)**

With the dev stack up, use the `agent-browser` skill:

1. `agent-browser open http://localhost:5173` → login as `nova@heaven-labs.com` / `nova123`.
2. Open the agent panel (FAB or Ctrl+K), start a standalone chat, send a message.
3. Open Share → **Chat link** is populated; open it in a second session (or copy it) — the owner lands in the full-page interactive chat.
4. Login as `sol@heaven-labs.com` / `sol123` (second browser context), open the copied link → "Shared · Read-only", no composer.
5. Owner sends another message; verify the recipient sees it live. Check `agent-browser errors` on both sessions for console errors.
6. Verify `/chats/*` hides the global FAB/panel and Ctrl+K does nothing.

**Step 6: Commit**

```bash
git add AGENTS.md docs/plans/2026-10-03-chat-links-design.md
git commit -m "docs: document shareable chat links"
```

---

## Notes for the implementer

- No backend, migration, or swagger changes are expected. If you think one is needed, stop and re-check the design — every endpoint used (`GET /sessions/:id`, `/messages`, WS, ACL) already enforces access via `checkSessionPermission`.
- `AgentPanel` remains a singleton-style component; page mode intentionally keeps the existing localStorage keys (`aether:lastSessionId`, `aether:agentChat:__global__`). The `sessionIdRef` guard in the `initialSessionId` effect prevents a refetch/reconnect loop when `onSessionChange` navigates to a just-opened session.
- Run commands from the stated directories; all Vitest runs must pin `--project=default`.
