# Cell UX, Agent Tools, Compaction & Sync Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all seven items of `IMPROVEMENTS.md` (create_cell limit, focus new agent cells, remove cell description, detail-view Ctrl+C, durable+visible compaction, session-aware token meter, stale `update_cell` display) in one PR on `feat/cell-ux-compaction-and-sync`.

**Architecture:** Go API/engine changes carry new migration columns (`V100`, `V101`); compaction becomes a durable context boundary and session usage becomes server-authoritative. The frontend gets a selection-aware copy handler, an agent-focus queue, a Yjs attach fix, and a context-first token meter. All wire changes are additive JSON.

**Tech Stack:** Go 1.x (`net/http`, pgx), React 18 + TypeScript + Vitest/RTL, Yjs (`yCollab`), Postgres migrations embedded in `internal/database/migrations/`.

**Design doc:** `docs/plans/2026-09-13-cell-ux-compaction-and-sync-design.md`

**Environment notes (read once):**
- Tests hit a real Postgres: run `task infra:up` first.
- Full Go suite: `AETHER_RATE_LIMIT_REGISTER=500 go test ./... -count=1 -timeout 30m` (`task check` times out under load).
- Frontend typecheck is `cd web && npx tsc -p tsconfig.app.json --noEmit` (`npx tsc --noEmit` is a no-op). Tests: `npm run test:run`. Build: `npm run build`.
- Migrations are embedded and applied at API startup and by `setupTestServer`/`setupTestDB`; never edit an existing migration.
- Commit after every task. Do not push until Task 13.

---

## Task 1: V101 migration + agent models + message ordering

**Files:**
- Create: `internal/database/migrations/V101__agent_usage.sql`
- Modify: `internal/models/agent.go` (AgentMessage ~:131-136, AgentSession ~:100-112)
- Modify: `internal/agent/session.go` (~:125-145 GetMessages)
- Test: `internal/database/database_test.go` (extend `TestMigrate`)

**Step 1: Write the failing test**

Append to `internal/database/database_test.go` (mirror the file's existing DB setup):

```go
func TestMigrateAgentUsageColumns(t *testing.T) {
	db := setupTestDB(t) // use this file's existing helper; adapt name if different
	ctx := context.Background()

	for _, col := range []string{"tokens_after"} {
		var n int
		err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name='agent_messages' AND column_name=$1`, col).Scan(&n)
		if err != nil || n != 1 {
			t.Fatalf("agent_messages.%s missing (n=%d err=%v)", col, n, err)
		}
	}
	for _, col := range []string{"context_tokens", "context_window", "total_input", "total_output", "total_reasoning", "total_cache_read", "total_model_calls", "total_subagent_input", "total_subagent_output"} {
		var n int
		err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name='agent_sessions' AND column_name=$1`, col).Scan(&n)
		if err != nil || n != 1 {
			t.Fatalf("agent_sessions.%s missing (n=%d err=%v)", col, n, err)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestMigrateAgentUsageColumns -count=1 -v`
Expected: FAIL — columns missing.

**Step 3: Add the migration**

Create `internal/database/migrations/V101__agent_usage.sql`:

```sql
ALTER TABLE agent_messages ADD COLUMN IF NOT EXISTS tokens_after INT;

ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS context_tokens INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS context_window INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_input BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_output BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_reasoning BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_cache_read BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_model_calls INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_subagent_input BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_subagent_output BIGINT NOT NULL DEFAULT 0;
```

**Step 4: Add model fields**

In `internal/models/agent.go`:

- `AgentMessage`: add `TokensAfter *int \`json:"tokens_after,omitempty"\`` right after `TokensDirect`.
- `AgentSession`: add after `CreatedAt` (before `AdminMode`):

```go
	ContextTokens      int64 `json:"context_tokens"`
	ContextWindow      int   `json:"context_window"`
	TotalInput         int64 `json:"total_input"`
	TotalOutput        int64 `json:"total_output"`
	TotalReasoning     int64 `json:"total_reasoning"`
	TotalCacheRead     int64 `json:"total_cache_read"`
	TotalModelCalls    int   `json:"total_model_calls"`
	TotalSubagentInput  int64 `json:"total_subagent_input"`
	TotalSubagentOutput int64 `json:"total_subagent_output"`
```

Add a shared struct for session usage snapshots:

```go
// SessionUsage is a compaction-aware snapshot of a session's token accounting.
type SessionUsage struct {
	Input              int64 `json:"input"`
	Output             int64 `json:"output"`
	Reasoning          int64 `json:"reasoning"`
	CacheRead          int64 `json:"cache_read"`
	ModelCalls         int   `json:"model_calls"`
	SubagentInput      int64 `json:"subagent_input"`
	SubagentOutput     int64 `json:"subagent_output"`
	ContextTokens      int64 `json:"context_tokens"`
	ContextWindow      int   `json:"context_window"`
}
```

**Step 5: Select `tokens_after` + deterministic ordering**

In `internal/agent/session.go` `GetMessages`:
- add `COALESCE(tokens_after,0)` to the SELECT (before `image_ids`),
- change ordering to `ORDER BY created_at ASC, id ASC`,
- scan into a local `var tokensAfter *int` then `msg.TokensAfter = tokensAfter`.

**Step 6: Run tests**

Run: `go test ./internal/database/ ./internal/agent/ -count=1`
Expected: PASS (existing agent tests unaffected).

**Step 7: Commit**

```bash
git add internal/database/migrations/V101__agent_usage.sql internal/models/agent.go internal/agent/session.go internal/database/database_test.go
git commit -m "feat(db): add agent session usage and compaction-after token columns"
```

---

## Task 2: Remove cell description — Go removal sweep

**Files:**
- Modify: `internal/models/notebook.go:43,89`
- Modify: `internal/api/cell_handlers.go` (list from design doc §P3)
- Modify: `internal/api/cell_history.go:220-225`
- Modify: `internal/api/notebook_handlers.go` (create-with-cells, get notebook, clone)
- Modify: `internal/agent/snapshot.go` (capture ~:23-54, restore ~:204-227)
- Modify: `internal/agent/tools_notebook.go` (create_cell ~:86-87,455,476,480-482,535-537,557; update_cell ~:102-103,604,636,641; read_cell ~:332,342,348,437)
- Test: `internal/agent/tools_notebook_test.go:1493-1567`, fixtures at `:129,267,321,512,546,1595,1658,1690`; `internal/api/mcp_pat_test.go:44`

**Step 1: Rewrite the behavior tests first (they must fail)**

In `internal/agent/tools_notebook_test.go`:

Replace `TestAgentCreateCellRequiresTitleAndDescription` with a title-only test:

```go
func TestAgentCreateCellRequiresTitle(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, _ := reg.Get("create_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)

	for name, argsIn := range map[string]map[string]any{
		"missing title": {"source": "SELECT 1"},
		"blank title":   {"title": "   ", "source": "SELECT 1"},
	} {
		base := map[string]any{"notebook_id": nbID, "type": "code"}
		for k, v := range argsIn {
			base[k] = v
		}
		args, _ := json.Marshal(base)
		if _, err := createCellDef.Handler(args, ctx); err == nil {
			t.Fatalf("%s: expected tool error", name)
		}
	}
}
```

Replace `TestAgentCreateCellPersistsTitleDescription` with `TestAgentCreateCellPersistsTitle` (title + broadcast only) and delete the description assertions at `:1554-1558,:1564-1566`. Remove `"description"` keys from every fixture in this file listed above, and from `internal/api/mcp_pat_test.go:44`.

**Step 2: Run tests to verify they fail to compile/pass**

Run: `go test ./internal/agent/ -run TestAgentCreateCell -count=1`
Expected: FAIL — description still required, so the missing-description case errors unexpectedly, or compile errors from removed helpers.

**Step 3: Remove the Go fields and SQL**

- `internal/models/notebook.go`: delete `Cell.Description` (`:43`) and `SnapshotCell.Description` (`:89`).
- `internal/api/cell_handlers.go`: delete `updateCellRequest.Description` (`:35`); remove `COALESCE(description,'')` and `&cell.Description` from every SELECT/RETURNING/scan (`:145,:149,:300,:310,:486,:491,:498,:544`); delete the `description = $N` SET clause and arg (`:270-274`); delete `update_msg["description"]` (`:376-378`); remove `description` from the duplicate INSERT column list (`:533`) and its bind arg (`:540`).
- `internal/api/cell_history.go`: remove `COALESCE(description,'')`/`&cell.Description` (`:220-225`).
- `internal/api/notebook_handlers.go`: same for `:134,:140,:288,:308` and the clone path `:952,:967,:974,:984,:992,:996`.
- `internal/agent/snapshot.go`: remove the snapshot SELECT column, `cDesc` var/scan/assignment (`:23,:37,:41,:53-54`); remove `description=$13` plus arg and the INSERT column plus arg from restore (`:208,:214,:221,:226`), renumbering placeholders.
- `internal/agent/tools_notebook.go`: remove description from create_cell prose/schema/required (`:86-87`), req struct (`:455`), TrimSpace (`:476`), validation block (`:480-482`), INSERT columns/args (`:535-537`), broadcast payload (`:557`); remove from update_cell prose/schema (`:102-103`), req struct (`:604`), and `description = COALESCE(NULLIF($4,''), description),` (`:636`) plus arg (`:641`); remove from read_cell struct (`:332`), SELECT (`:342`), scan (`:348`), output map (`:437`).

**Step 4: Run tests**

Run: `go build ./... && go test ./internal/api/ ./internal/agent/ -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/models/notebook.go internal/api/cell_handlers.go internal/api/cell_history.go internal/api/notebook_handlers.go internal/agent/snapshot.go internal/agent/tools_notebook.go internal/agent/tools_notebook_test.go internal/api/mcp_pat_test.go
git commit -m "refactor(cells): remove unused cell description from models, API and agent tools"
```

---

## Task 3: V100 migration + CLI/web description cleanup

**Files:**
- Create: `internal/database/migrations/V100__drop_cells_description.sql`
- Modify: `internal/cli/types.go:41`
- Modify: `web/src/hooks/useNotebookWs.ts:64`
- Test: `internal/api/notebook_handlers_test.go` (add a shape assertion)

**Step 1: Write the failing test**

In `internal/api/notebook_handlers_test.go`, add (mirror the file's existing notebook creation helper):

```go
func TestNotebookCellsHaveNoDescriptionField(t *testing.T) {
	s := setupTestServer(t)
	// ... create org/user/notebook + one cell using existing helpers in this file ...
	// assert the raw JSON response contains no "description" key for any cell:
	// body of GET /api/v1/notebooks/{id}: if strings.Contains(string(body), `"description"`) after
	// deleting the notebook's own description in the fixture -> fail.
}
```

Use this file's existing request helpers; the notebook fixture must have an empty notebook description so the only possible `"description"` key would be the cell's.

**Step 2: Run to verify it passes already (removal done in Task 2)**

Run: `go test ./internal/api/ -run TestNotebookCellsHaveNoDescriptionField -count=1 -v`
Expected: PASS (column still exists but nothing selects it). Keep the test as a regression guard.

**Step 3: Add the drop migration**

Create `internal/database/migrations/V100__drop_cells_description.sql`:

```sql
ALTER TABLE cells DROP COLUMN IF EXISTS description;
```

Note: migration numbers must be unique; V100/V101 are free after main's V100/V101 (auto-approve/auto-answer). If another migration lands first, renumber to the next free numbers.

**Step 4: Remove remaining consumers**

- `internal/cli/types.go:41`: delete `Description string \`json:"description"\`` from `Cell` (JSON decode tolerates missing).
- `web/src/hooks/useNotebookWs.ts:64`: remove `'description'` from the cell_updated whitelist array.

**Step 5: Verify migration applies and API starts**

```bash
docker compose -f docker-compose.dev.yml restart api
docker exec aether-postgres psql -U aether -d aether -tAc \
  "SELECT COUNT(*) FROM information_schema.columns WHERE table_name='cells' AND column_name='description'"
```
Expected: `0`. Then `curl -s -o /dev/null -w '%{http_code}\n' localhost:8088/healthz` → `200`.

**Step 6: Run tests + commit**

Run: `go test ./internal/api/ ./internal/database/ -count=1`

```bash
git add internal/database/migrations/V100__drop_cells_description.sql internal/cli/types.go web/src/hooks/useNotebookWs.ts internal/api/notebook_handlers_test.go
git commit -m "refactor(cells)!: drop unused cells.description column"
```

---

## Task 4: `create_cell` limit parameter (P1)

**Files:**
- Modify: `internal/agent/tools_notebook.go:86-87` (schema/prose), `:450-461` (req struct), `:533-537` (INSERT), `:546-569` (broadcast), `:571` (result)
- Test: `internal/agent/tools_notebook_test.go` (append after the create-cell tests)

**Step 1: Write the failing tests**

```go
func TestCreateCellLimit(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	createCellDef, _ := reg.Get("create_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	var broadcasts []map[string]any
	ctx.BroadcastFunc = func(notebookID string, msg any) {
		if m, ok := msg.(map[string]any); ok {
			broadcasts = append(broadcasts, m)
		}
	}

	create := func(extra map[string]any) (map[string]any, error) {
		args := map[string]any{"notebook_id": nbID, "type": "code", "source": "SELECT 1", "title": "T"}
		for k, v := range extra {
			args[k] = v
		}
		b, _ := json.Marshal(args)
		res, err := createCellDef.Handler(b, ctx)
		if err != nil {
			return nil, err
		}
		return res.(map[string]any), nil
	}

	// default -> 1000
	res, err := create("")
	if err != nil {
		t.Fatalf("default create: %v", err)
	}
	cellID := res["cell_id"].(string)
	var limit *int
	if err := db.Pool.QueryRow(context.Background(), `SELECT "limit" FROM cells WHERE id=$1`, cellID).Scan(&limit); err != nil {
		t.Fatalf("query limit: %v", err)
	}
	if limit == nil || *limit != 1000 {
		t.Fatalf("default limit = %v, want 1000", limit)
	}
	if res["limit"] != 1000 {
		t.Fatalf("result limit = %v, want 1000", res["limit"])
	}

	// explicit
	res, err = create(map[string]any{"limit": 250})
	if err != nil {
		t.Fatalf("explicit create: %v", err)
	}
	if res["limit"] != 250 {
		t.Fatalf("result limit = %v, want 250", res["limit"])
	}

	// zero -> unlimited (NULL)
	res, err = create(map[string]any{"limit": 0})
	if err != nil {
		t.Fatalf("zero create: %v", err)
	}
	cellID = res["cell_id"].(string)
	if err := db.Pool.QueryRow(context.Background(), `SELECT "limit" FROM cells WHERE id=$1`, cellID).Scan(&limit); err != nil {
		t.Fatalf("query unlimited: %v", err)
	}
	if limit != nil {
		t.Fatalf("zero limit = %v, want NULL", *limit)
	}

	// negative -> tool error, no insert
	if _, err := create(map[string]any{"limit": -1}); err == nil {
		t.Fatal("negative limit must error")
	}
	var count int
	_ = db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM cells WHERE notebook_id=$1`, nbID).Scan(&count)
	if count != 3 {
		t.Fatalf("cells = %d, want 3", count)
	}
	// broadcast carries the effective limit
	last := broadcasts[len(broadcasts)-1]["cell"].(map[string]any)
	if last["limit"] == nil {
		t.Fatalf("broadcast missing limit: %v", last)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -run TestCreateCellLimit -count=1 -v`
Expected: FAIL — `limit` is unknown/ignored; result has no `limit`.

**Step 3: Implement**

In `internal/agent/tools_notebook.go`:

- req struct: add `Limit *int \`json:"limit"\``.
- validation right after the title check:

```go
		limit := 1000
		if req.Limit != nil {
			if *req.Limit < 0 {
				return nil, fmt.Errorf("limit must be >= 0 (0 = unlimited)")
			}
			limit = *req.Limit
		}
```

- INSERT: bind the limit with NULL for unlimited:

```go
		var limitArg any
		if limit > 0 {
			limitArg = limit
		}
		_, err := db.Exec(ctx.Context, `
			INSERT INTO cells (id, notebook_id, type, language, connector_id, source, title, position, "limit", created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		`, cellID, req.NotebookID, req.Type, language, connID, req.Source, req.Title, position, limitArg, now)
```

(Description columns were already removed in Task 2.)

- broadcast `"limit": limitArg`; result `base := map[string]any{"cell_id": cellID, "position": position + 1, "title": req.Title, "limit": limit}`.
- schema/prose: add `"limit":{"type":"integer","description":"Row limit for SQL results (default 1000, 0 = unlimited)"}` and mention it in the Description string.

**Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestCreateCell -count=1 -v`
Expected: PASS (all create-cell tests).

**Step 5: Commit**

```bash
git add internal/agent/tools_notebook.go internal/agent/tools_notebook_test.go
git commit -m "feat(agent): configurable create_cell limit (default 1000, 0 = unlimited)"
```

---

## Task 5: Detail-view Ctrl+C (P4)

**Files:**
- Modify: `web/src/components/OutputRenderer.tsx:11-22` (global reset), `:529-550` (handlers)
- Test: `web/src/test/OutputRenderer.test.tsx` (append; mirror its existing render helpers)

**Step 1: Write the failing tests**

Append tests that render a table cell output, open the detail panel, and:

```tsx
it('copies the user selection instead of the whole value', () => {
  // render output with a table, open detail (click a cell), then:
  const sel = window.getSelection()!
  const range = document.createRange()
  range.selectNodeContents(document.querySelector('[data-detail-value]')!.firstChild!)
  sel.removeAllRanges()
  sel.addRange(range)
  const copyEvent = new Event('copy', { bubbles: true, cancelable: true }) as ClipboardEvent & { clipboardData: DataTransfer }
  // jsdom: attach a DataTransfer stub
  const data: Record<string, string> = {}
  Object.defineProperty(copyEvent, 'clipboardData', { value: { setData: (k: string, v: string) => { data[k] = v } } })
  document.dispatchEvent(copyEvent)
  expect(copyEvent.defaultPrevented).toBe(false)
  expect(data['text/plain']).toBeUndefined()
})

it('copies the whole value when nothing is selected', () => {
  window.getSelection()!.removeAllRanges()
  // dispatch copy as above
  expect(copyEvent.defaultPrevented).toBe(true)
  expect(data['text/plain']).toBe(fullValue)
})
```

Add a third test that unmounting the open detail panel makes `isAnyDetailActive()` false.

If RTL/jsdom makes `ClipboardEvent` construction awkward, dispatch `new Event('copy')` and attach `clipboardData` as shown; the component code below must read `e.clipboardData` defensively.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/test/OutputRenderer.test.tsx`
Expected: FAIL — keydown path doesn't respond to `copy` events.

**Step 3: Implement**

In `OutputRenderer.tsx`:

- Change the keydown effect to Escape/arrows only (delete the `c` branch, `:537-546`).
- Add a copy effect:

```tsx
  useEffect(() => {
    if (!detail || !isDetailActive) return
    const onCopy = (e: ClipboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target?.closest('.cm-editor') || target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA') return
      const sel = window.getSelection()
      if (sel && !sel.isCollapsed && sel.toString().length > 0) return
      e.preventDefault()
      e.clipboardData?.setData('text/plain', detail.value)
    }
    document.addEventListener('copy', onCopy)
    return () => document.removeEventListener('copy', onCopy)
  }, [detail, isDetailActive])
```

- Reset the module global on unmount:

```tsx
  useEffect(() => () => {
    if (activeDetailCellId === cellId) setActiveDetailCell(null)
  }, [cellId])
```

**Step 4: Run tests + typecheck**

Run: `cd web && npx vitest run --project=default src/test/OutputRenderer.test.tsx && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add web/src/components/OutputRenderer.tsx web/src/test/OutputRenderer.test.tsx
git commit -m "fix(output): stop hijacking Ctrl+C in the detail view"
```

---

## Task 6: Focus agent-created cells (P2)

**Files:**
- Create: `web/src/utils/agentFocus.ts`
- Test: `web/src/utils/agentFocus.test.ts`
- Modify: `web/src/pages/NotebookPage.tsx:266-340` (WS callbacks), `:220` (focusedCellId), unmount cleanup

**Step 1: Write the failing test**

`web/src/utils/agentFocus.test.ts`:

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createFlashQueue, isAgentOrigin } from './agentFocus'

describe('isAgentOrigin', () => {
  it('recognizes the agent user', () => {
    expect(isAgentOrigin('agent@aether')).toBe(true)
    expect(isAgentOrigin('nova@heaven-labs.com')).toBe(false)
    expect(isAgentOrigin(undefined)).toBe(false)
  })
})

describe('createFlashQueue', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('flashes only the last cell in a burst', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a'); q.push('b'); q.push('c')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
    expect(flash).toHaveBeenCalledWith('c')
  })

  it('flush runs the pending flash immediately and clears it', () => {
    const flash = vi.fn()
    const q = createFlashQueue(flash, 250)
    q.push('a')
    q.flush()
    expect(flash).toHaveBeenCalledWith('a')
    vi.advanceTimersByTime(250)
    expect(flash).toHaveBeenCalledTimes(1)
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/utils/agentFocus.test.ts`
Expected: FAIL — module not found.

**Step 3: Implement the helper**

`web/src/utils/agentFocus.ts`:

```ts
export function isAgentOrigin(userEmail?: string | null): boolean {
  return userEmail === 'agent@aether'
}

export interface FlashQueue {
  push(cellId: string): void
  flush(): void
}

export function createFlashQueue(flash: (cellId: string) => void, delayMs = 250): FlashQueue {
  let pending: string | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  const run = () => {
    timer = null
    const id = pending
    pending = null
    if (id) flash(id)
  }
  return {
    push(cellId: string) {
      pending = cellId
      if (timer) clearTimeout(timer)
      timer = setTimeout(run, delayMs)
    },
    flush() {
      if (timer) clearTimeout(timer)
      run()
    },
  }
}
```

**Step 4: Wire it into NotebookPage**

In `NotebookPage.tsx`:

- Create a ref once: `const agentFlashRef = useRef(createFlashQueue((id) => flashCellRef.current?.(id)));` — since `flashCell` is a callback defined later, keep a `flashCellRef` updated to the latest `flashCell`, or construct the queue in a `useMemo` after `flashCell` and store in a ref. Flush on unmount (`useEffect(() => () => agentFlashRef.current?.flush(), [])`).
- In `onCellCreated` (`:309-326`): 

```ts
const isAgent = isAgentOrigin(userEmail)
if (isAgent) {
  agentFlashRef.current?.push(cellId)
  setFocusedCellId(cellId)
} else if (shouldScroll(userEmail)) {
  flashCell(cellId)
}
```

- In `onCellOutput` (`:270-283`): `if (isAgent) agentFlashRef.current?.push(cellId) else if (shouldScroll(userEmail)) flashCell(cellId)`.
- Import `createFlashQueue, isAgentOrigin` from `../utils/agentFocus`.

**Step 5: Run tests + typecheck**

Run: `cd web && npx vitest run --project=default src/utils/agentFocus.test.ts && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS / clean.

**Step 6: Commit**

```bash
git add web/src/utils/agentFocus.ts web/src/utils/agentFocus.test.ts web/src/pages/NotebookPage.tsx
git commit -m "feat(notebook): focus and highlight cells created by the agent"
```

---

## Task 7: Yjs attach race fix (P7, frontend)

**Files:**
- Modify: `web/src/components/Cell.tsx:29-95` (registry accessor), `:319-345` (attachCollab), `:358-372` (source effect)
- Test: `web/src/components/Cell.test.tsx` (append)

**Step 1: Write the failing test**

Add a test that:
1. Renders a `Cell` with a mocked collab provider whose `synced` is `false` and whose `doc` has an empty `Y.Text` for `cell:{id}`.
2. Re-renders with a changed `cell.source` (simulating an agent update arriving pre-sync).
3. Fires the provider's `synced` event with `true`.
4. Asserts the Y.Text now contains the **new** source (and, once yCollab applies, the editor doc matches the new source).

Also add a regression assertion: when the Y.Text already contains `"SELECT new"` and the editor buffer contains `"SELECT old"`, syncing must **not** overwrite the Y.Text with `"SELECT old"`.

Use the existing mocking style in `Cell.test.tsx` (mock `getOrCreateCollab`/provider module or inject via the existing test seams). Mock `yCollab`/`YSyncConfig` state minimally as the file already does — if no seam exists, export `attachCollabForTest` is **not** acceptable; instead test through the component with a fake provider object registered in the module-level registry.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/components/Cell.test.tsx`
Expected: FAIL — current code clobbers Yjs with the stale editor buffer.

**Step 3: Implement**

In `Cell.tsx`:

- Add a non-creating accessor near the other registry helpers:

```ts
export function peekCollab(notebookId: string): NotebookCollab | undefined {
  return collabRegistry.get(notebookId)
}
```

(Use the actual registry variable name in the file — the helpers `getOrCreateCollab`/`releaseCollab` access it directly.)

- Replace the clobber logic in `attachCollab` (`:319-337`):

```ts
    const attachCollab = () => {
      const editorContent = view.state.doc.toString()
      // Seed Yjs from the database-backed editor only when the shared doc is
      // empty. Otherwise Yjs wins: reconfiguring yCollab below applies the
      // shared text to the editor, which is what fixes the stale-update race.
      if (ytext.length === 0 && editorContent.length > 0) {
        const ySyncConfig = new YSyncConfig(ytext, collab.provider.awareness)
        collab.doc.transact(() => {
          ytext.insert(0, editorContent)
        }, ySyncConfig)
      }
      const ySyncConfig = new YSyncConfig(ytext, collab.provider.awareness)
      view.dispatch({ effects: compartment.reconfigure([
        yCollab(ytext, collab.provider.awareness, { undoManager: false }),
        ySyncFacet.of(ySyncConfig),
      ]) })
    }
```

- Source effect (`:358-372`): use `peekCollab` instead of `getOrCreateCollab` so a not-yet-mounted provider isn't created/leaked. When no provider exists, return early — the editor mounts later and `attachCollab` seeds from the cell source.

```ts
  useEffect(() => {
    if (cell.source !== lastSourceRef.current && cell.source !== undefined) {
      lastSourceRef.current = cell.source
      const collab = peekCollab(notebookId)
      if (!collab) return
      const ytext = collab.doc.getText(`cell:${cell.id}`)
      if (ytext.toString() !== cell.source) {
        collab.doc.transact(() => {
          ytext.delete(0, ytext.length)
          ytext.insert(0, cell.source)
        })
      }
    }
  }, [cell.source, cell.id, notebookId])
```

**Step 4: Run tests + typecheck**

Run: `cd web && npx vitest run --project=default src/components/Cell.test.tsx && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add web/src/components/Cell.tsx web/src/components/Cell.test.tsx
git commit -m "fix(cell): stop clobbering Yjs with stale editor content on sync"
```

---

## Task 8: Broadcast hygiene + WS reconnect resync (P7, Go + web)

**Files:**
- Modify: `internal/agent/tools_notebook.go:598-660` (update_cell broadcast)
- Modify: `internal/api/cell_handlers.go` (REST update broadcast, ~:360-390)
- Modify: `web/src/hooks/useNotebookWs.ts:9-118` (whitelist + onReconnect)
- Modify: `web/src/pages/NotebookPage.tsx` (pass onReconnect → invalidate notebook query)
- Test: `internal/agent/tools_notebook_test.go`, `web/src/hooks/useNotebookWs.test.ts`

**Step 1: Write the failing Go test**

Add to `tools_notebook_test.go`:

```go
func TestUpdateCellBroadcastOmitsUnchangedSource(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	// create a cell directly via SQL: id, notebook, type, language, source, title
	cellID := createTestCell(t, db.Pool, nbID, "SELECT 1")
	if cellID == "" { t.Fatal("no cell") }

	reg := agent.NewToolRegistry()
	agent.RegisterNotebookTools(reg, db.Pool)
	updateDef, _ := reg.Get("update_cell")
	ctx := setupToolContext(t, db, orgID, userID, nbID)
	var last map[string]any
	ctx.BroadcastFunc = func(notebookID string, msg any) {
		if m, ok := msg.(map[string]any); ok { last = m }
	}

	// title-only update must not include source
	args, _ := json.Marshal(map[string]any{"cell_id": cellID, "title": "Renamed"})
	if _, err := updateDef.Handler(args, ctx); err != nil { t.Fatalf("update: %v", err) }
	cell := last["cell"].(map[string]any)
	if _, hasSource := cell["source"]; hasSource {
		t.Fatalf("title-only update must omit source, got %v", cell["source"])
	}
	if cell["agent_updated_at"] == nil || cell["updated_at"] == nil {
		t.Fatalf("broadcast missing timestamps: %v", cell)
	}

	// source update includes it
	args, _ = json.Marshal(map[string]any{"cell_id": cellID, "source": "SELECT 2"})
	if _, err := updateDef.Handler(args, ctx); err != nil { t.Fatalf("update source: %v", err) }
	cell = last["cell"].(map[string]any)
	if cell["source"] != "SELECT 2" {
		t.Fatalf("source update must include source, got %v", cell["source"])
	}
}
```

Use an existing cell-insert helper if one exists in the package; otherwise insert with `INSERT INTO cells (notebook_id, type, language, source, position) VALUES ...` and scan the id.

**Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -run TestUpdateCellBroadcastOmitsUnchangedSource -count=1 -v`
Expected: FAIL — source always present; timestamps missing.

**Step 3: Implement the Go change**

`makeUpdateCellHandler` (`tools_notebook.go:598+`):
- Change `Source string` to `Source *string` in the req struct; use `src := ""; if req.Source != nil { src = *req.Source }` in the SQL args (keep the `COALESCE(NULLIF($N,''), source)` semantics).
- Build the broadcast cell map with `source` only when `src != ""`:
- include `"agent_updated_at": now, "updated_at": now` where `now := time.Now()` (reuse the query's updated timestamp).

`internal/api/cell_handlers.go` REST update broadcast: include `agent_updated_at` (when non-nil) and `updated_at` in `update_msg`, mirroring the agent handler.

**Step 4: Frontend whitelist + reconnect**

`web/src/hooks/useNotebookWs.ts`:
- add `'agent_updated_at', 'updated_at'` to the whitelist at `:64`.
- accept an `onReconnect?: () => void` option; track a `everConnected` ref; call `onReconnect()` on `onopen` when it was previously opened. Keep existing reconnect timing.

`web/src/pages/NotebookPage.tsx`: pass `onReconnect={() => queryClient.invalidateQueries({ queryKey: ['notebook', id] })}` (use the file's existing query-client/query-key conventions).

**Step 5: Run tests**

Run: `go test ./internal/agent/ -run TestUpdateCell -count=1 && cd web && npx vitest run --project=default src/hooks/useNotebookWs.test.ts && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS / clean.

**Step 6: Commit**

```bash
git add internal/agent/tools_notebook.go internal/api/cell_handlers.go internal/agent/tools_notebook_test.go web/src/hooks/useNotebookWs.ts web/src/hooks/useNotebookWs.test.ts web/src/pages/NotebookPage.tsx
git commit -m "fix(sync): omit unchanged source, carry agent timestamps, resync after WS reconnect"
```

---

## Task 9: Durable compaction in the engine (P5)

**Files:**
- Modify: `internal/agent/engine.go:250-304` (compactChatHistory), `:590-644` (history rebuild), `:884-909` (trigger/persist/emit)
- Modify: `internal/agent/session.go` (`AppendMessage` binds `TokensAfter`)
- Test: `internal/agent/engine_chat_ux_test.go`

**Step 1: Write the failing tests**

Extend `TestCompactionEmitsEventAndPersists` and add:

```go
func TestCompactionIsDurableAcrossTurns(t *testing.T) {
	// Drive a session through compaction with the existing fake LLM pattern,
	// finish the turn, then call ProcessMessage again with a new user message.
	// Capture the chat messages the fake LLM receives on the second turn.
	// Assert: first message is the system prompt, second is the injected summary
	// system message, and NONE of the pre-compaction user/assistant/tool messages appear.
}

func TestCompactionEventAndRowCarryAfterTokens(t *testing.T) {
	// After the trigger: assert the persisted compaction row has TokensDirect > 0
	// and TokensAfter != nil and > 0, and the emitted context_compacted event has
	// Tokens.Input == before and Tokens.ContextCurrent == after.
}
```

Reuse the existing fake LLM + context-window helpers in `engine_chat_ux_test.go` (the file already has compaction tests and a scripted chat client).

**Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -run 'TestCompaction(IsDurable|EventAndRow)' -count=1 -v`
Expected: FAIL — second turn re-sends old messages; `TokensAfter` nil.

**Step 3: Implement**

- `compactChatHistory` signature → `([]ChatMessage, string, int, int)` returning `(compacted, summary, promptTokens, completionTokens)`; capture `resp.Usage.PromptTokens`/`CompletionTokens` from the summarization call (`:278-280`). Compute:

```go
	afterEstimate := e.tokenCounter.CountMessages(compacted)
```

and return it as the third value.
- History rebuild (`:590-644`): find the latest compaction row before the loop and start from it:

```go
	boundary := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "compaction" {
			boundary = i
			break
		}
	}
	for i, m := range messages {
		if i < boundary {
			continue
		}
		if m.Role == "compaction" {
			chatMsgs = append(chatMsgs, ChatMessage{
				Role:    "system",
				Content: "The following is a summary of earlier conversation history:\n\n" + m.Content + "\n\n(older context was compacted to stay within context window limits)",
			})
			continue
		}
		// ... existing role handling unchanged ...
	}
```

- Trigger (`:884-909`): accept the new return values; persist `TokensAfter: &afterEstimate`; emit `Tokens: &TokenBreakdown{Input: beforeCtx, ContextCurrent: afterEstimate}`; add the summarization usage to the session totals (Task 11 wires `AddUsage`, this task can leave a TODO only if Task 11 lands immediately after — otherwise add the call here using `e.session.AddUsage`).

**Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestCompaction -count=1 -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/agent/engine.go internal/agent/engine_chat_ux_test.go
git commit -m "feat(agent): durable compaction boundary with real before/after token counts"
```

---

## Task 10: Compaction divider plumbing (P5, API + web)

**Files:**
- Modify: `internal/api/agent_handlers.go:809-893` (messages endpoint)
- Modify: `internal/api/agent_ws.go:241-263` + `507-535` (reconnect_sync + scan)
- Modify: `web/src/utils/agentTranscript.ts:107-110`
- Modify: `web/src/components/AgentPanel.tsx:105-121` (divider text)
- Modify: `web/src/components/SessionHistory.tsx:98-106` (render compaction as summary)
- Test: `web/src/test/HistoryPanel.test.tsx`, `internal/api/agent_ws_test.go`

**Step 1: Write the failing tests**

- Go (`agent_ws_test.go`): assert `reconnect_sync` payload messages include `tokens_direct`/`tokens_after` for a persisted compaction row.
- Vitest (`agentTranscript` unit or existing panel test): mapping a compaction row sets `tokens_before` and `tokens_after`; divider renders `"1.2k → 400 (~)"`.

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run Reconnect -count=1 && cd web && npx vitest run --project=default src/test/HistoryPanel.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- Messages endpoint: add `COALESCE(tokens_direct,0), COALESCE(tokens_after,0)` to the SELECT and emit `tokens_direct`/`tokens_after` in each message object.
- `reconnect_sync` query + `scanAgentMessages`: add `COALESCE(tokens_direct,0)`, `COALESCE(tokens_after,0)`; include in the payload.
- `agentTranscript.ts` compaction branch: set both `tokens_before` (from `tokens_direct`) and `tokens_after`.
- `CompactionDivider`: render `before → after (~)` (the `~` marks the estimate).
- `SessionHistory`: render compaction rows as the same summary block (label + truncated summary) instead of a tool bubble.

**Step 4: Run tests**

Run: `go test ./internal/api/ -count=1 && cd web && npx vitest run --project=default && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add internal/api/agent_handlers.go internal/api/agent_ws.go internal/api/agent_ws_test.go web/src/utils/agentTranscript.ts web/src/components/AgentPanel.tsx web/src/components/SessionHistory.tsx
git commit -m "feat(agent-ui): show real compaction before/after counts in live, reload and history"
```

---

## Task 11: Session usage persistence + events (P6, Go)

**Files:**
- Modify: `internal/agent/session.go` (add `AddUsage`, `GetUsage`)
- Modify: `internal/agent/engine.go` (usage accumulation, event payloads, done)
- Modify: `internal/api/agent_ws.go:422-473` (forward `session_usage`; reconnect payload)
- Modify: `internal/api/agent_handlers.go` (new `GET /agents/sessions/{id}/usage`)
- Modify: `internal/agent/subagent.go:246-266` (add subagent usage to parent session + event)
- Test: `internal/agent/stats_test.go` or new `internal/agent/session_usage_test.go`, `internal/api/agent_handlers_test.go`

**Step 1: Write the failing test**

`internal/agent/session_usage_test.go`:

```go
func TestSessionUsageAccumulates(t *testing.T) {
	db := setupTestDB(t)
	// create agent + session via existing helpers
	store := agent.NewSessionStore(db.Pool)
	ctx := context.Background()

	if err := store.AddUsage(ctx, sessionID, agent.SessionUsageDelta{Input: 100, Output: 20, ModelCalls: 1, ContextTokens: 120, ContextWindow: 128000}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.AddUsage(ctx, sessionID, agent.SessionUsageDelta{Input: 50, Output: 5, ModelCalls: 1, ContextTokens: 90}); err != nil {
		t.Fatalf("add: %v", err)
	}
	u, err := store.GetUsage(ctx, sessionID)
	if err != nil { t.Fatalf("get: %v", err) }
	if u.Input != 150 || u.Output != 25 || u.ModelCalls != 2 {
		t.Fatalf("usage = %+v", u)
	}
	if u.ContextTokens != 90 || u.ContextWindow != 128000 {
		t.Fatalf("context = %d/%d", u.ContextTokens, u.ContextWindow)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -run TestSessionUsageAccumulates -count=1 -v`
Expected: FAIL — `SessionUsageDelta` undefined.

**Step 3: Implement the store**

In `internal/agent/session.go`:

```go
type SessionUsageDelta struct {
	Input, Output, Reasoning, CacheRead int64
	ModelCalls                          int
	SubagentInput, SubagentOutput       int64
	ContextTokens                       int64
	ContextWindow                       int
}

func (s *SessionStore) AddUsage(ctx context.Context, sessionID string, d SessionUsageDelta) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE agent_sessions SET
			total_input = total_input + $2,
			total_output = total_output + $3,
			total_reasoning = total_reasoning + $4,
			total_cache_read = total_cache_read + $5,
			total_model_calls = total_model_calls + $6,
			total_subagent_input = total_subagent_input + $7,
			total_subagent_output = total_subagent_output + $8,
			context_tokens = CASE WHEN $9 > 0 THEN $9 ELSE context_tokens END,
			context_window = CASE WHEN $10 > 0 THEN $10 ELSE context_window END
		WHERE id = $1`,
		sessionID, d.Input, d.Output, d.Reasoning, d.CacheRead, d.ModelCalls, d.SubagentInput, d.SubagentOutput, d.ContextTokens, d.ContextWindow)
	return err
}

func (s *SessionStore) GetUsage(ctx context.Context, sessionID string) (*models.SessionUsage, error) {
	var u models.SessionUsage
	err := s.pool.QueryRow(ctx, `
		SELECT total_input, total_output, total_reasoning, total_cache_read, total_model_calls,
		       total_subagent_input, total_subagent_output, context_tokens, context_window
		FROM agent_sessions WHERE id = $1`, sessionID).
		Scan(&u.Input, &u.Output, &u.Reasoning, &u.CacheRead, &u.ModelCalls, &u.SubagentInput, &u.SubagentOutput, &u.ContextTokens, &u.ContextWindow)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
```

**Step 4: Wire the engine**

- At `ProcessMessage` start (after session load): `usage, _ := e.session.GetUsage(ctx, sessionID)` (ignore error → zero value).
- After each call's usage accumulation (`:852-860`): `usage.add(resp.Usage, contextWindow)` locally and `_ = e.session.AddUsage(...)` with that call's delta (prompt/completion/reasoning/cached, ModelCalls: 1, ContextTokens: prompt tokens, ContextWindow: contextWindow).
- Add `SessionUsage *models.SessionUsage` to `EngineEvent` (`internal/agent/types.go`) and include `SessionUsage: usage.snapshot()` in both `token_update` and `done` events.
- `agent_ws.go`: forward `session_usage` in `token_update` and `done` payloads.
- `subagent.go` after persisting task tokens (`:246-249`): `e.session.AddUsage(ctx, parentSessionID, SessionUsageDelta{SubagentInput: ..., SubagentOutput: ..., ModelCalls: 1})` (use the parent session id available in the task row) and include the refreshed snapshot in the `subagent_status` event.
- Compaction: add the summarization call's usage via `AddUsage` (Task 9 returns it).

**Step 5: New usage endpoint**

`internal/api/agent_handlers.go`: add `GET /agents/sessions/{id}/usage` returning the `models.SessionUsage` JSON (auth + session ownership check mirroring the messages endpoint). Register in `router.go`. Add an API test asserting 200 + accumulated values.

**Step 6: Run tests**

Run: `go test ./internal/agent/ ./internal/api/ -count=1`
Expected: PASS.

**Step 7: Commit**

```bash
git add internal/agent/session.go internal/agent/session_usage_test.go internal/agent/engine.go internal/agent/types.go internal/agent/subagent.go internal/api/agent_ws.go internal/api/agent_handlers.go internal/api/agent_handlers_test.go internal/api/router.go
git commit -m "feat(agent): server-authoritative compaction-aware session token usage"
```

---

## Task 12: Context-first token meter (P6, web)

**Files:**
- Modify: `web/src/types/agent.ts` (add `SessionUsage`, event fields)
- Modify: `web/src/components/AgentPanel.tsx:258-267` (state), `:680-692` (persist), `:796-830` (handlers), `:922-923` (token_update), `:1483-1595` (meter)
- Test: `web/src/test/StatsPage.test.tsx` is stats; add a focused panel test to `web/src/components/AgentPanel.test.tsx` if it exists, else `web/src/test/AgentPanel.tokens.test.tsx`

**Step 1: Write the failing tests**

- Reducer-level: `done` **replaces** `totalTokens.input` with the event value and does not add the previous value; `context_current` survives `done`.
- Headline: `(context_current / context_window) * 100` is rendered, not `(input + output) / window`.
- Reconnect with `session_usage` populates the hover "This session" rows.

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/test/AgentPanel.tokens.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `web/src/types/agent.ts`: add

```ts
export interface SessionUsage {
  input: number
  output: number
  reasoning: number
  cache_read: number
  model_calls: number
  subagent_input: number
  subagent_output: number
  context_tokens: number
  context_window: number
}
```

and `session_usage?: SessionUsage` on the `token_update`/`done`/`reconnect_sync` event variants.
- `AgentPanel.tsx`:
  - add `const [sessionUsage, setSessionUsage] = useState<SessionUsage | null>(null)`; include in the persisted `AgentChatState` and restore it (plus `hasCompacted`).
  - `token_update`: replace fields with event values (never add), keep `context_current`; `setSessionUsage(msg.session_usage ?? prev)`.
  - `done`: same set-not-add semantics; do **not** clear `context_current`; apply `session_usage`.
  - `reconnect_sync`: apply `session_usage` from the payload.
  - remove the client-side subagent accumulation into `totalTokens.subagent_*` (server totals now carry it); keep the live subagent task list UI.
  - headline percent: `((totalTokens.context_current ?? sessionUsage?.context_tokens ?? 0) / (contextWindow || sessionUsage?.context_window || 1)) * 100`.
  - hover: keep the per-component "Estimated (tiktoken)" breakdown, replace the cumulative window rows with a "This session" section using `sessionUsage` (input/output/reasoning/cache, model calls, subagent input/output).

**Step 4: Run tests + typecheck + build**

Run: `cd web && npx vitest run --project=default && npx tsc -p tsconfig.app.json --noEmit && npm run build`
Expected: PASS / clean.

**Step 5: Commit**

```bash
git add web/src/types/agent.ts web/src/components/AgentPanel.tsx web/src/test/AgentPanel.tokens.test.tsx
git commit -m "feat(agent-ui): show current-context meter and compaction-aware session totals"
```

---

## Task 13: Full verification, browser sweep, PR

**Step 1: Full automated checks**

```bash
task fmt
task vet
AETHER_RATE_LIMIT_REGISTER=500 go test ./... -count=1 -timeout 30m
cd web && npx tsc -p tsconfig.app.json --noEmit && npm run test:run && npm run build
cd ../relay && npm run build
cd .. && AETHER_RATE_LIMIT_REGISTER=500 task test:e2e
```
Expected: all green. `go mod tidy` must produce no diff.

**Step 2: Migration sanity on dev DB**

```bash
docker compose -f docker-compose.dev.yml restart api
docker exec aether-postgres psql -U aether -d aether -tAc "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 2"
```
Expected: V100/V101 applied; API healthy.

**Step 3: Browser sweep (agent-browser + image analyzer)**

- **P1/P3**: create a cell via the agent tool (or `create_cell` through MCP) with `limit: 250` → `Row limit: LIMIT 1000` select reflects 250? (the select only lists preset values — verify the cell still renders and the DB value is honored by a `run_cell` returning ≤250 rows). Confirm `GET /notebooks/{id}` cell JSON has no `description` and the UI renders unchanged.
- **P2**: run an agent `create_cell(run=true)` → scroll/highlight on create, again on completion; type in the chat while it happens (input focus preserved).
- **P4**: select part of a value in the detail view → Ctrl+C copies the selection; no selection → whole value; close the view and confirm normal copy works; leave the panel open and copy from the agent panel (not hijacked).
- **P5/P6**: configure a dev model with a tiny `context_window` and threshold, run a long scripted conversation → divider shows real `before → after (~)`; headline tracks current context during the turn and after compaction without a new user message; hover shows session totals; refresh restores both.
- **P7**: with the relay WS blocked, agent `update_cell` → after sync the editor shows the new content (no F5); title-only update doesn't clear source; notebook WS drop/reconnect refetches.

Capture screenshots in `/tmp/opencode/` and review with the image analyzer. Record any failures as new tasks (do not hand-wave).

**Step 4: Self-review + docs**

- Re-read the design doc; confirm each decision is implemented or explicitly deferred.
- Update `IMPROVEMENTS.md` checkboxes only if the user asks (it is a user-owned scratchpad; do not commit it unless requested).

**Step 5: Push and PR**

```bash
git push -u origin feat/cell-ux-compaction-and-sync
gh pr create --base main --title "feat: cell UX, agent tools, compaction and sync improvements" --body "<fill from design doc: sections P1-P7, test evidence, browser sweep results, the relay-Yjs follow-up, and the breaking change: cells.description dropped>"
```

CI must pass (Go tests, frontend build, relay build, e2e smoke). Report the PR URL.

---

## Notes for the implementer

- Run each task's tests before moving on; if a task's test cannot be made to fail first (e.g. pure migrations), say so in the commit message.
- Never edit applied migrations; if a migration number collides, renumber to the next free `V<number>`.
- Keep all wire changes additive; do not rename existing JSON fields.
- If the fake-LLM compaction tests need a new knob (e.g. scripted second compaction), extend the existing test client rather than introducing mocks.
- `agent_stats_hourly` rollups are unrelated; do not change their semantics.
