# Chat Links — Shareable Standalone & Notebook Agent Chats — Design

**Date:** 2026-10-03
**Status:** Implemented on `feat/chat-links` (approved; schematics confirmed in brainstorming)
**Builds on:** `2026-09-11-agent-session-sharing-design.md` (session ACLs, notebook
inheritance, read-only live viewer)

## Problem

Agent session sharing works well for chats attached to a notebook: a notebook's Chats drawer
lists its sessions, and the opt-in `share_with_notebook_viewers` flag lets notebook viewers
watch them live. But sharing a session with a person still leaves them no direct way to reach
it:

- Session identity lives only in client state and localStorage (`aether:lastSessionId`,
  `aether:agentChat:__global__`); there is no URL that resolves to a chat.
- "Shared with me" is scoped to the currently selected agent inside the docked AgentPanel, so
  a standalone chat shared with someone is effectively undiscoverable unless they happen to
  select the same agent and open history.
- Users cannot send a link that opens a given chat — the natural way to hand a thought,
  investigation, or result to a teammate.

## Goals

- A URL that opens a specific agent chat: `/chats/:id`.
- Works for **all** sessions — standalone (no notebook) and notebook-attached.
- Access stays ACL-gated: the link itself grants nothing; the recipient must be logged in and
  hold `view` (direct share, group, Everyone, notebook inheritance, or ownership).
- Viewers get the existing read-only live experience; the owner (and org admins in admin mode)
  get a full-page **interactive** chat, so opening your own link is a first-class chat surface.
- A Copy-link affordance in the existing share UI.
- No backend, schema, or API changes.

## Non-goals

- Public/unauthenticated chat links (`public_tokens` stays notebook/dashboard only, as decided
  in D6 of the session-sharing design).
- Request-access flow for a 403 link.
- Cross-org sharing.
- A global "all my chats" index page or notifications.
- A notebook breadcrumb on the chat page.
- Extracting AgentPanel's interactive core into a separate component (see Alternatives).

## Decisions

| # | Decision |
|---|---|
| D1 | The link is an ACL-gated deep link: `${window.location.origin}/chats/{session_id}`. The raw session UUID is opaque and grants nothing; `checkSessionPermission` remains the only gate. No new token type, no backend change. |
| D2 | New protected route `/chats/:id`, rendered inside `AppShell` (like other pages) with `noPadding`. |
| D3 | The page branches on `GET /api/v1/sessions/{id}`: `can_edit: true` → interactive page-mode AgentPanel; `can_edit: false` → existing `SessionViewer` (read-only, live). 403 → no-access state; 404 → not-found state. |
| D4 | Interactive page mode is delivered by adding `variant?: 'panel' \| 'page'` and `initialSessionId?: string` to `AgentPanel`, not by extracting its internals. The page variant fills its container and drops dock/resize/minimize/close chrome. |
| D5 | On `/chats/*`, `AppShell` suppresses the global AgentPanel and FAB so two chat instances never fight over singleton localStorage keys and the WS connection. |
| D6 | Copy-link lives in one place: `PermissionsPanel` for `agent_session` resources. It is reachable from every existing Share button (AgentPanel info bar, SessionViewer header, SessionHistory). |
| D7 | Editor state is resolved by the backend's existing `can_edit` (owner fallback, or org admin with admin mode). Non-owner ACL subjects can only ever hold `view`, so viewers always land in `SessionViewer`. |
| D8 | Starting a new chat from page mode updates the URL to the new session id (replace navigation) so the address bar always points at the open chat. |

## Architecture

```
/chat/:id  →  ChatPage (ProtectedRoute)
                │
                ├─ GET /api/v1/sessions/:id ──► 404 → NotFound state
                │                               403 → NoAccess state
                │                               200 ─┬─ can_edit: true  → <AgentPanel variant="page" initialSessionId={id} />
                │                                    └─ can_edit: false → <SessionViewer sessionId={id} session={...} />
                │
                └─ rendered inside <AppShell noPadding>
```

### ChatPage

- `useParams` for the session id; a `GET /sessions/:id` load gate decides the branch.
- Loading state until the session resolves.
- 404 (`session not found`) and 403 (`insufficient permissions`) each render a small full-page
  state with a "Go home" action. Copy lines:
  - 403: "You don't have access to this chat. Ask the chat owner to share it with you."
  - 404: "Chat not found or has been deleted."
- Sets `document.title` from the session title, fallback "Chat".
- Passes `session` into `SessionViewer` so it does not refetch the session header.

### AgentPanel page mode

New props (both optional, defaulting to today's behavior):

```ts
interface AgentPanelProps {
  // …existing…
  variant?: 'panel' | 'page'
  initialSessionId?: string
}
```

- **Restore precedence**: when `initialSessionId` is set, it wins over the localStorage restore
  path. The panel fetches that session, selects its agent, fetches messages, then connects the
  WS — the same sequence as `SessionHistory`'s `onResumeSession`, extracted into a reusable
  `resumeSession(id)` and called from both places.
- **Chrome**: `variant='page'` renders the root as a full-width/full-height region, omits the
  drag-resize handle, and hides close/minimize/dock controls; title, model/reasoning/
  auto-approve controls, Share, History, and Copy remain. Tool-confirm and question overlays
  remain `position:absolute` within the full-height root.
- **New chat**: calls an `onSessionChange` callback so `ChatPage` can `navigate(/chats/:newId,
  { replace: true })`.
- Interactive logic, WS handling, and persistence keys are otherwise unchanged. Mounting page
  mode for an owner makes that session the "last session" as usual, which is desirable.

### AppShell

- When `location.pathname` starts with `/chats/`, do not render the global AgentPanel, its
  floating/docked wrapper, or the FAB; Ctrl+K is a no-op.
- `currentPageContext` remains `undefined` on `/chats/*` (no `set_page_context` frames), since
  the chat page is not a notebook/dashboard/files context.

### PermissionsPanel

- For `resource_type === 'agent_session'`, render a "Chat link" section: the read-only URL
  `${window.location.origin}/chats/${sessionId}`, a Copy button (`navigator.clipboard` with a
  textarea fallback), and "Copied" feedback. The URL is built client-side; no API.

### SessionViewer

- Already renders standalone: it owns its transcript, WS live updates, subagent views, and
  read-only banner, and its close button only renders when `onClose` is provided. On the page,
  pass a back navigation via `onClose` and render it as the page's back affordance.
- Change the root `role="dialog"` presentation for the page context (region + heading) so a
  full-page view is not announced as a modal.

## UX flows

| Situation | Experience |
|---|---|
| Owner opens own link | Full-page interactive chat: send, stop, tool confirmations, questions, slash commands, model/reasoning/auto-approve, Share, History, Copy. |
| Org admin + admin mode | Same as owner (`can_edit: true`). |
| Recipient with `view` | Full-page `SessionViewer`: title, owner email, Live/Disconnected indicator, "Shared · Read-only" banner, no composer. Live WS updates while the owner chats. |
| Logged out | `ProtectedRoute` saves the target and `LoginPage` returns to `/chats/:id` after auth (existing mechanism). |
| No access (403) | No-access state; no request-access flow. |
| Missing/deleted (404) | Not-found state. |
| Access revoked mid-view | Live WS viewers are disconnected by the existing periodic view re-validation (~60s); a refresh shows the 403 state. |
| Notebook-attached session | Same route and behavior; no notebook-specific UI. |

## Edge cases

- **Empty session**: editors see a fresh composer; viewers see the existing "No messages in
  this session yet." empty state.
- **Session deleted while open**: WS disconnect surfaced by the existing disconnected state;
  refresh yields the 404 page.
- **Agent visibility**: only owners/admins-in-admin-mode reach the interactive page, so the
  session's agent is always selectable. Viewers never need agent metadata.
- **Subdomain routing**: `${window.location.origin}` preserves the org subdomain
  (e.g. `org1.aether.test`).
- **Admin mode**: read from `aether_admin_mode` as today; no new behavior.

## Alternatives considered

- **Extract AgentPanel's chat core into a reusable full-page component** (original approach C):
  rejected for now. The explore pass showed the interactive core is tightly coupled to
  singleton localStorage keys, the `aether:new-agent-chat` window event, AppShell panel chrome,
  and global DOM hooks; a mechanical 1,400-line move would carry the coupling and enlarge the
  regression surface without changing user-visible behavior. The `variant` prop delivers the
  same UX; extraction remains a possible follow-up refactor.
- **Route-aware docked AgentPanel** (approach B): rejected — requires read-only mode in the
  singleton panel and is awkward on small screens.
- **Reusing `public_tokens`** for chat links: rejected — the approved access model is
  ACL-gated, and public links were explicitly out of scope for sessions.

## Testing

**Unit / component (Vitest)**
- `ChatPage`: editor branch renders page-mode AgentPanel; viewer branch renders SessionViewer;
  403 → no-access; 404 → not-found; document title.
- `AgentPanel`: `initialSessionId` wins over localStorage restore (correct agent, messages,
  WS URL); page variant hides panel chrome; new chat calls `onSessionChange`.
- `AppShell`: no global panel/FAB on `/chats/*`; Ctrl+K no-op.
- `PermissionsPanel`: chat-link row URL and copy feedback.
- Existing AgentPanel / SessionViewer / SessionHistory / agentTranscript suites stay green.

**E2E (Playwright, new `e2e/chat-links.spec.ts`)**
1. Owner opens own standalone chat link → composer present; sends a message and receives the
   fake-LLM reply.
2. Shared recipient opens the link → read-only banner, no composer, live updates.
3. Logged-out visitor → login → lands back on the chat page.
4. Unauthorized user → no-access page; invalid id → not-found page.
5. Copy-link from PermissionsPanel yields the exact URL.
6. Notebook-attached session link resolves the same way.

**Manual**: real-browser validation with agent-browser for both owner and recipient flows, per
AGENTS.md.

## Delivery

- Frontend-only: no backend, migration, or swagger changes.
- No feature flag; existing notebook Chats drawer, "Shared with me", and ACL behavior are
  unchanged.
- Update the AGENTS.md session-sharing paragraph to mention `/chats/:id` links.
- Suggested task order: route + branch states → AgentPanel page mode + resume → AppShell
  suppression → PermissionsPanel copy link → SessionViewer page polish → tests + docs.

## Known limitations / follow-ups

- No request-access flow from the 403 page.
- Copy-link exists only in PermissionsPanel; additional surfaces (session history rows,
  SessionViewer header) are future polish.
- AgentPanel remains a single large component; the extraction refactor is deferred.
- No notebook breadcrumb on the chat page for notebook-attached sessions.
