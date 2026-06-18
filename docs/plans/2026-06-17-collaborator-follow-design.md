# Collaborator Follow Feature — Design

**Date:** 2026-06-17
**Status:** Design approved, pending implementation plan

## Problem

When any collaborator runs a cell in a shared notebook, all other viewers are automatically scrolled to that cell via `flashCell()`. This is disruptive — if user B is working on cell 10 and user A runs cell 2, B gets yanked away.

## Solution

A **Google Docs-style "follow" feature**: users can click a collaborator's avatar to follow them. When following, the user's viewport tracks the followed user's position and scrolls to cells they execute. When not following anyone, remote changes are visible (cursor, output updates) but never cause scrolling.

## Architecture

Two independent channels, both gated by a single "following" local state:

1. **Yjs awareness** (via Hocuspocus relay, port 3001) — real-time presence, focus, and scroll tracking. No backend changes needed.
2. **Notebook WebSocket** (via Go API, port 8080) — cell execution events with added `user_email` field to identify the source user.

Both channels feed into the same scroll decision: "is this change from the user I'm following?"

## Awareness Protocol

Awareness currently carries `user { name, email, color }`. New fields:

```typescript
provider.awareness?.setLocalStateField('focus', {
  cellId: string | null,    // current focused cell
  scrollTop: number | null, // throttled viewport scroll position
  updatedAt: number,        // timestamp
})
provider.awareness?.setLocalStateField('following', {
  email: string | null,     // who this user follows (null = not following)
})
```

**Update triggers:**
- `focus.cellId` — on cell focus/blur (immediate)
- `focus.scrollTop` — on notebook scroll (throttled to ~500ms)
- `following` — on follow/unfollow action

**Read/react (in NotebookPage):**
- Listen to `provider.awareness.on('change', ...)`
- When following user A → if A's `focus.cellId` changes → scroll to that cell
- When following user A → if A's `focus.scrollTop` changes significantly (>200px) → adjust viewport
- Scan all awareness states to count followers (states where `following.email === myEmail`)

## Backend WebSocket Changes

Add `user_email` (and optional `user_name`) to all existing broadcast events. Minimal changes — just one new field per message.

| Handler | Event | File |
|---|---|---|
| `execute_handlers.go` | `cell_output` | Add `user_email` from JWT claims |
| `cell_handlers.go` | `cell_created` | Add `user_email` from JWT claims |
| `cell_handlers.go` | `cell_updated` | Add `user_email` from JWT claims |
| `cell_handlers.go` | `cell_metadata_changed` | Add `user_email` from JWT claims |
| `cell_handlers.go` | `cell_deleted` | Add `user_email` from JWT claims |
| Agent broadcasts | all | Add `user_email: "agent@hnb"` |

## Follow State Machine

```typescript
const [following, setFollowing] = useState<{ email: string; name: string } | null>(null)
```

- **Follow**: Click on a collaborator's avatar → set `following` to their identity → update awareness
- **Unfollow**: Click followed avatar again, or press Escape → set `following = null` → update awareness
- On follow → immediately scroll to their last known cell from awareness

## Scroll Decision Tree

```
WebSocket cell event arrives
  └── event.user_email === following?.email?
        └── YES → scroll to cell (existing flashCell behavior)
        └── NO  → ignore (no scroll)

Yjs awareness change from followed user
  └── focus.cellId changed?
        └── YES → scroll to cell (smooth, block: center)
        └── NO  → check focus.scrollTop changed >200px?
                      └── YES → adjust viewport
                      └── NO  → ignore
```

## Toolbar Redesign

Current toolbar has 11 buttons on the right — too crowded. Reorganize into dropdown groups:

```
[Connector ▼]  [A] [B] [C] [+2]  │  [View ▼] [Run All] [AI] [Share ▼]
                 ↑collab avatars
```

- **Left**: ConnectorSelector + Collaborator avatars (max 4 visible, `+N` overflow)
- **Right**: View dropdown (Parameters, Schema, Schedules, Collapse/Hide toggles), Run All, AI, Share dropdown (Export, Present, Permissions)

## UI Components

### Collaborator Avatar List (new)
- Colored circle with initials (from awareness `user.color`)
- Max 4 visible; overflow shows `+N`
- Click unfollowed user → follow them
- Click followed user or press Escape → unfollow
- Click self → no-op
- **Following state**: Highlighted border + "Viewing" tooltip
- **Followed state**: Small dot indicator showing someone is following
- **AI avatar** (conditional): Shown when agent is active; teal color

### Avatar Overflow Popover
- Clicking `+N` shows a tooltip/popover listing all collaborator names
- Each name clickable to follow

## Agent Integration

- Agent WebSocket events carry `user_email: "agent@hnb"`
- **Agent invoker**: Always sees agent actions (existing `AgentPanel` → `onCellScrollTo` behavior, unchanged)
- **Other users**: Can optionally follow the agent via the AI avatar in collaborator list
- Agent has no Yjs awareness — all tracking via WebSocket events

## Safety: Mutual Following

No infinite loop possible. Scrolling to a followed user's cell is a DOM operation — it does not change awareness state. Awareness only updates on explicit user interaction (cell focus, scroll). So A→B→A cycles cannot happen.

## Testing

- **Backend**: Verify `user_email` appears in all WebSocket broadcast JSON payloads
- **Frontend unit**: Awareness state updates on focus/scroll; flashCell gating logic
- **Frontend integration (Playwright)**: Two browser windows, same notebook; verify scroll suppression without follow, scroll activation with follow; verify avatar list rendering with overflow
