# Warehouse Pattern Entry & Auto-Compaction Visibility — Implementation Plan

> **For the implementer:** REQUIRED SUB-SKILLS: use `test-driven-development` for every code change (write the failing test first), and `verification-before-completion` before claiming any task done. Work task-by-task; commit at the end of each task.

**Goal:** Let warehouse admins commit many hidden-table patterns from a multi-line textarea (Enter adds all lines, Shift+Enter inserts a newline), and make agent auto-compaction unmistakable in the chat and token panel.

**Architecture:** Frontend-only. Two independent surfaces — `WarehouseHiddenTables` (a controlled textarea with batch normalization; one PUT per commit) and the agent chat (`CompactionDivider` becomes a full-width system-event marker; `TokenUsageMeter` gains cumulative/context labels, the compaction threshold, and a compaction count derived from already-loaded messages).

**Tech Stack:** React 19 + TypeScript + Vite, Vitest + Testing Library + MSW, inline `CSSProperties` styles, TanStack Query. Design tokens from `web/src/styles/theme.css` (`--border`, `--accent-light`, `--font-mono`, …).

**Design doc:** `docs/plans/2026-10-08-warehouse-patterns-compaction-visibility-design.md`

**Branch:** `feat/warehouse-patterns-compaction-visibility` (already created; design doc committed).

---

### Task 1: Warehouse hidden-table patterns — textarea with Enter-to-add

**Files:**
- Modify: `web/src/components/WarehouseHiddenTables.tsx`
- Test: `web/src/components/WarehouseHiddenTables.test.tsx`

- [ ] **Step 1: Write the failing tests.** Append these tests inside `describe('WarehouseHiddenTables', ...)`:

```tsx
  test('adds all non-empty lines in one PUT on Enter', async () => {
    let putBody: Record<string, unknown> | null = null
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        puts++
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', '^raw\\.old', '^analytics\\.x'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    const field = screen.getByLabelText('Pattern')
    fireEvent.change(field, { target: { value: '^raw\\.old\n\n  ^analytics\\.x  ' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', '^raw\\.old', '^analytics\\.x'] }))
    expect(puts).toBe(1)
    expect(field).toHaveValue('')
  })

  test('dedupes lines against existing patterns and within the batch', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', 'foo', 'bar'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    fireEvent.change(screen.getByLabelText('Pattern'), { target: { value: '_tmp\nfoo\nfoo\n\nbar\n' } })
    fireEvent.keyDown(screen.getByLabelText('Pattern'), { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', 'foo', 'bar'] }))
  })

  test('clears without a PUT when every line is already present', async () => {
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', () => {
        puts++
        return HttpResponse.json(WAREHOUSE)
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    const field = screen.getByLabelText('Pattern')
    fireEvent.change(field, { target: { value: '_tmp\n_tmp' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    expect(puts).toBe(0)
    expect(field).toHaveValue('')
  })

  test('Shift+Enter keeps the text and sends no request', async () => {
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', () => {
        puts++
        return HttpResponse.json(WAREHOUSE)
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    const field = screen.getByLabelText('Pattern')
    fireEvent.change(field, { target: { value: '^a' } })
    fireEvent.keyDown(field, { key: 'Enter', shiftKey: true })
    expect(puts).toBe(0)
    expect(field).toHaveValue('^a')
  })

  test('does not intercept paste — lines stay editable until Enter', async () => {
    let puts = 0
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        puts++
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', '^a', '^b'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    const field = screen.getByLabelText('Pattern')
    // The component must not turn a paste into an immediate request.
    fireEvent.paste(field, { clipboardData: { getData: () => '^a\n^b' } })
    expect(puts).toBe(0)
    // Simulate what the browser would then place in the textarea.
    fireEvent.change(field, { target: { value: '^a\n^b' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', '^a', '^b'] }))
  })

  test('pressing Enter in an empty field sends no request', async () => {
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', () => {
        puts++
        return HttpResponse.json(WAREHOUSE)
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={[]} connectors={CONNECTORS} />,
    )
    const field = await screen.findByLabelText('Pattern')
    fireEvent.keyDown(field, { key: 'Enter' })
    expect(puts).toBe(0)
  })
```

Also extend the existing invalid-pattern test (it currently ends with the banner assertion) with:

```tsx
    expect(screen.getByLabelText('Pattern')).toHaveValue('(')
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `cd web && npm run test:run -- src/components/WarehouseHiddenTables.test.tsx`
Expected: the new tests FAIL (Enter does not add; `# the single-line input drops the value`). The pre-existing tests pass.

- [ ] **Step 3: Implement the batch handler.** In `web/src/components/WarehouseHiddenTables.tsx`, replace `addPattern` with:

```tsx
  // Enter/Add commits every non-empty line as one batch. Lines are trimmed,
  // empties dropped, and duplicates (against existing patterns and within the
  // batch) ignored — first occurrence wins. All-duplicate input clears the
  // field without a request, matching the single-pattern behavior.
  const addPatterns = () => {
    if (save.isPending) return
    const next = [...patterns]
    for (const line of input.split(/\r?\n/)) {
      const trimmed = line.trim()
      if (trimmed && !next.includes(trimmed)) next.push(trimmed)
    }
    if (next.length === patterns.length) {
      setInput('')
      return
    }
    save.mutate(next)
  }
```

- [ ] **Step 4: Replace the input with the textarea and hint.** Replace the whole `<div style={styles.addRow}>…</div>` block with:

```tsx
      <div style={styles.addRow}>
        <textarea
          aria-label="Pattern"
          aria-describedby="hidden-table-pattern-hint"
          style={styles.textarea}
          placeholder={'e.g. ^analytics\\._tmp\n       ^raw\\.old$'}
          value={input}
          disabled={save.isPending}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              addPatterns()
            }
          }}
        />
        <button
          type="button"
          style={{ ...styles.addBtn, opacity: input.trim() && !save.isPending ? 1 : 0.5 }}
          disabled={!input.trim() || save.isPending}
          onClick={addPatterns}
        >
          {save.isPending ? 'Saving…' : 'Add'}
        </button>
      </div>
      <p id="hidden-table-pattern-hint" style={styles.keyHint}>
        One pattern per line · <kbd style={styles.kbd}>Enter</kbd> adds ·{' '}
        <kbd style={styles.kbd}>Shift+Enter</kbd> for a new line
      </p>
```

- [ ] **Step 5: Update styles.** In the `styles` object, replace `addRow` and `input` and add `keyHint` + `kbd`:

```ts
  addRow: { display: 'flex', gap: 8, alignItems: 'flex-end', maxWidth: 460 },
  textarea: {
    flex: 1, minHeight: 60, padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 6,
    fontSize: 12, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)',
    color: 'var(--text-primary)', resize: 'vertical', minWidth: 0,
  },
  keyHint: {
    fontSize: 11, color: 'var(--text-muted)', margin: 0, display: 'flex',
    alignItems: 'center', gap: 4, flexWrap: 'wrap',
  },
  kbd: {
    fontFamily: 'var(--font-mono)', fontSize: 10, background: 'var(--bg-secondary)',
    border: '1px solid var(--border)', borderRadius: 3, padding: '1px 5px',
    color: 'var(--text-primary)',
  },
```

(Do not keep the old `input` style — the textarea replaces its only consumer.)

- [ ] **Step 6: Run the tests to verify they pass.**

Run: `cd web && npm run test:run -- src/components/WarehouseHiddenTables.test.tsx`
Expected: PASS (all tests, including the pre-existing ones).

- [ ] **Step 7: Commit.**

```bash
git add web/src/components/WarehouseHiddenTables.tsx web/src/components/WarehouseHiddenTables.test.tsx
git commit -m "feat(web): enter-to-add multi-pattern textarea for warehouse hidden tables"
```

---

### Task 2: Auto-compaction marker in the chat transcript

**Files:**
- Modify: `web/src/components/AgentChatTranscript.tsx`
- Test: `web/src/components/AgentPanel.compaction.test.tsx` (imports `CompactionDivider` via the `AgentPanel` re-export)

- [ ] **Step 1: Rewrite the tests (failing first).** Replace the whole content of `web/src/components/AgentPanel.compaction.test.tsx` with:

```tsx
import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CompactionDivider } from './AgentPanel'

describe('CompactionDivider', () => {
  it('renders a labeled marker with compact before → after counts', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200, tokens_after: 400 }}
        fmtTime={() => ''}
      />,
    )
    expect(screen.getByText(/Context auto-compacted/)).toBeInTheDocument()
    expect(screen.getByText(/1\.2k → ~400/)).toBeInTheDocument()
  })

  it('renders the label alone when counts are missing (legacy rows)', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200 }}
        fmtTime={() => ''}
      />,
    )
    expect(screen.getByText(/Context auto-compacted/)).toBeInTheDocument()
    expect(screen.queryByText(/→/)).toBeNull()
  })

  it('expands to the summary, exact counts, and timestamp', () => {
    render(
      <CompactionDivider
        msg={{
          role: 'compaction',
          content: 'earlier context',
          tokens_before: 1200,
          tokens_after: 400,
          created_at: '2026-09-14T00:01:00Z',
        }}
        fmtTime={(iso) => `t:${iso}`}
      />,
    )
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(screen.getByText('earlier context')).toBeInTheDocument()
    expect(screen.getByText(/1,200 tokens \(actual\) → ~400 tokens \(estimated\)/)).toBeInTheDocument()
    expect(screen.getByText('t:2026-09-14T00:01:00Z')).toBeInTheDocument()
  })

  it('is toggleable and keyboard-operable through a native button', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200, tokens_after: 400 }}
        fmtTime={() => ''}
      />,
    )
    const pill = screen.getByRole('button', { expanded: false })
    expect(pill.tagName).toBe('BUTTON')
    fireEvent.click(pill)
    expect(screen.getByRole('button', { expanded: true })).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `cd web && npm run test:run -- src/components/AgentPanel.compaction.test.tsx`
Expected: FAIL — the label is still "Context compacted" and the counts render as `1.2k → 400 (~)`.

- [ ] **Step 3: Use the shared token formatter.** In `web/src/components/AgentChatTranscript.tsx`:

1. Change the React import to include `useId`: `import { useState, useEffect, memo, useId } from 'react'`.
2. Add `import { formatTokens } from '../utils/agentStats'` after the local imports (that util is the shared `1.5k` / `1.20M` formatter).
3. Delete the local `formatTokens` function (the one above `chatStyles`).

- [ ] **Step 4: Add the marker styles.** Below the `chatStyles` object (and after removing its `compactionMessage` entry — nothing else uses it; verify with `grep -rn "compactionMessage" web/src`), add:

```tsx
const compactionStyles: Record<string, React.CSSProperties> = {
  marker: { display: 'flex', alignItems: 'center', gap: 8, width: '100%', margin: '2px 0' },
  rule: { flex: 1, height: 1, background: 'var(--border-light)' },
  body: { display: 'flex', flexDirection: 'column', alignItems: 'center', maxWidth: '100%', minWidth: 0 },
  pill: {
    display: 'inline-flex', alignItems: 'center', gap: 6, maxWidth: '100%',
    background: 'var(--accent-light)', border: '1px solid var(--border)', borderRadius: 10,
    padding: '2px 10px', fontSize: 11, fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)', cursor: 'pointer',
  },
  counts: { opacity: 0.7, whiteSpace: 'nowrap' as const },
  details: {
    marginTop: 6, fontSize: 11, color: 'var(--text-secondary)', background: 'var(--bg-elevated)',
    border: '1px solid var(--border)', borderRadius: 6, padding: '8px 10px', maxWidth: 520,
    textAlign: 'left' as const,
  },
  countsLine: { opacity: 0.8, marginBottom: 4 },
  summary: { whiteSpace: 'pre-wrap' as const, opacity: 0.9 },
  time: { marginTop: 4, opacity: 0.5, fontSize: 10 },
}
```

- [ ] **Step 5: Replace `CompactionDivider` with the system-event marker.** Replace the current implementation with:

```tsx
export function CompactionDivider({ msg, fmtTime }: { msg: ChatMessage; fmtTime: (iso?: string) => string }) {
  const [open, setOpen] = useState(false)
  const detailsId = useId()
  const hasCounts = msg.tokens_before !== undefined && msg.tokens_after !== undefined
  return (
    <div style={compactionStyles.marker}>
      <span style={compactionStyles.rule} />
      <div style={compactionStyles.body}>
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          aria-controls={detailsId}
          style={compactionStyles.pill}
        >
          <span aria-hidden="true">{open ? '▾' : '▸'}</span>
          <span>⚙ Context auto-compacted</span>
          {hasCounts && (
            <span style={compactionStyles.counts}>
              {formatTokens(msg.tokens_before!)} → ~{formatTokens(msg.tokens_after!)}
            </span>
          )}
        </button>
        {open && (
          <div id={detailsId} style={compactionStyles.details}>
            {hasCounts && (
              <div style={compactionStyles.countsLine}>
                {msg.tokens_before!.toLocaleString()} tokens (actual) → ~{msg.tokens_after!.toLocaleString()} tokens (estimated)
              </div>
            )}
            {msg.content && <div style={compactionStyles.summary}>{msg.content}</div>}
            {msg.created_at && <div style={compactionStyles.time}>{fmtTime(msg.created_at)}</div>}
          </div>
        )}
      </div>
      <span style={compactionStyles.rule} />
    </div>
  )
}
```

- [ ] **Step 6: Render it outside the chat bubble.** In `MemoizedChatMessage`, change the content block so compaction rows bypass the bubble wrapper. Replace:

```tsx
      {msg.role !== 'reasoning' && (
        <div style={{ ...chatStyles.message, ...(msg.role === 'user' ? chatStyles.userMessage : msg.role === 'tool' ? chatStyles.toolMessage : msg.role === 'compaction' ? chatStyles.compactionMessage : chatStyles.assistantMessage) }}>
```

with:

```tsx
      {msg.role === 'compaction' ? (
        <CompactionDivider msg={msg} fmtTime={fmtTime} />
      ) : msg.role !== 'reasoning' && (
        <div style={{ ...chatStyles.message, ...(msg.role === 'user' ? chatStyles.userMessage : msg.role === 'tool' ? chatStyles.toolMessage : chatStyles.assistantMessage) }}>
```

Then delete the now-unreachable compaction branch inside the bubble — replace:

```tsx
          {msg.role === 'compaction' ? (
            <CompactionDivider msg={msg} fmtTime={fmtTime} />
          ) : msg.role === 'tool' ? (
```

with:

```tsx
          {msg.role === 'tool' ? (
```

- [ ] **Step 7: Run the tests to verify they pass.**

Run: `cd web && npm run test:run -- src/components/AgentPanel.compaction.test.tsx`
Expected: PASS.

- [ ] **Step 8: Run the neighboring suites (mapping + panel tokens) to catch regressions.**

Run: `cd web && npm run test:run -- src/utils/agentTranscript.test.ts src/test/AgentPanel.tokens.test.tsx`
Expected: PASS (existing assertions still hold).

- [ ] **Step 9: Commit.**

```bash
git add web/src/components/AgentChatTranscript.tsx web/src/components/AgentPanel.compaction.test.tsx
git commit -m "feat(web): promote auto-compaction to a visible chat event"
```

---

### Task 3: Token panel — cumulative input, threshold, compaction count

**Files:**
- Modify: `web/src/components/AgentPanel.tsx` (`TokenUsageMeter`)
- Test: `web/src/test/AgentPanel.tokens.test.tsx`

- [ ] **Step 1: Write the failing tests.** Append inside `describe('AgentPanel context-first token meter', ...)`:

```tsx
  it('labels cumulative input and shows the compaction threshold', async () => {
    server.use(
      http.get('/api/v1/agents', () => HttpResponse.json([{ ...AGENT, model_config_id: 'mc-1' }])),
      http.get('/api/v1/model-configs', () =>
        HttpResponse.json([{
          id: 'mc-1', org_id: 'org-1', name: 'Model', provider: 'openai', base_url: '',
          model: 'gpt-4', default_params: { compaction_threshold: 70 }, context_window: 1000,
          price_per_input_token: 0, price_per_output_token: 0, price_per_cache_read_token: 0,
          created_by: 'u1', created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
        }]),
      ),
    )
    seedSession(savedStateWithTokens(baseTokens({ input: 3000000, output: 100, context_current: 480 }), 1000))
    await renderPanel()
    fireEvent.click(await screen.findByText(/↑/))
    expect(screen.getByText('Input (cumulative)')).toBeInTheDocument()
    expect(screen.getByText(/compacts at 70%/)).toBeInTheDocument()
  })

  it('shows auto-compact off when the threshold is zero', async () => {
    server.use(
      http.get('/api/v1/agents', () => HttpResponse.json([{ ...AGENT, model_config_id: 'mc-1' }])),
      http.get('/api/v1/model-configs', () =>
        HttpResponse.json([{
          id: 'mc-1', org_id: 'org-1', name: 'Model', provider: 'openai', base_url: '',
          model: 'gpt-4', default_params: { compaction_threshold: 0 }, context_window: 1000,
          price_per_input_token: 0, price_per_output_token: 0, price_per_cache_read_token: 0,
          created_by: 'u1', created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
        }]),
      ),
    )
    seedSession(savedStateWithTokens(baseTokens({ input: 100, output: 10, context_current: 500 }), 1000))
    await renderPanel()
    fireEvent.click(await screen.findByText(/↑/))
    expect(screen.getByText(/auto-compact off/)).toBeInTheDocument()
  })

  it('shows the compaction count and latest recovery from synced messages', async () => {
    seedSession(savedStateWithTokens(baseTokens({ input: 100, output: 10, context_current: 500 }), 1000))
    const ws = await renderPanel()
    emit(ws, {
      type: 'reconnect_sync',
      messages: [
        { id: 'm1', role: 'user', content: 'hello', created_at: '2026-09-14T00:00:00Z' },
        { id: 'm2', role: 'compaction', content: 's1', tokens_direct: 900, tokens_after: 300, created_at: '2026-09-14T00:01:00Z' },
        { id: 'm3', role: 'compaction', content: 's2', tokens_direct: 1200, tokens_after: 400, created_at: '2026-09-14T00:02:00Z' },
      ],
    })
    fireEvent.click(await screen.findByText(/↑/))
    expect(screen.getByText(/Compacted 2× · last: 1\.2k → ~400/)).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `cd web && npm run test:run -- src/test/AgentPanel.tokens.test.tsx`
Expected: FAIL — `Input (cumulative)`, `compacts at 70%`, `auto-compact off`, and `Compacted 2×` are not rendered yet.

- [ ] **Step 3: Implement the meter changes.** In `web/src/components/AgentPanel.tsx`:

1. Add the import: `import { formatTokens } from '../utils/agentStats'`.
2. Inside `TokenUsageMeter`, right after `const percent = Math.round(contextPercent(...))`, add:

```tsx
  const compactionThreshold = (() => {
    const mc = modelConfigs.find((m) => m.id === modelConfigId)
    const t = mc?.default_params?.['compaction_threshold']
    return typeof t === 'number' ? t : 70
  })()
  const compactions = messages.filter((m) => m.role === 'compaction')
  const latestCompaction = compactions[compactions.length - 1]
```

3. Relabel the input row:

```tsx
                  <span style={{ color: 'var(--text-secondary)' }} title="Total prompt tokens across every model call in this session">Input (cumulative)</span>
```

4. Append the threshold to the current-context row:

```tsx
                <span>
                  {currentContext.toLocaleString()} / {windowSize.toLocaleString()} ({percent}%)
                  {compactionThreshold > 0 ? ` · compacts at ${compactionThreshold}%` : ' · auto-compact off'}
                </span>
```

5. Replace the `{hasCompacted && ( … )}` block with:

```tsx
            {compactions.length > 0 ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 4, fontSize: 10, color: 'var(--accent)' }}>
                <span>
                  ⚙ Compacted {compactions.length}×
                  {latestCompaction && latestCompaction.tokens_before !== undefined && latestCompaction.tokens_after !== undefined
                    ? ` · last: ${formatTokens(latestCompaction.tokens_before)} → ~${formatTokens(latestCompaction.tokens_after)}`
                    : ''}
                </span>
              </div>
            ) : hasCompacted ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 4, fontSize: 10, color: 'var(--accent)' }}>
                <span>⚙ Compacted</span>
                <span style={{ opacity: 0.6, fontSize: 9 }}>context was summarized</span>
              </div>
            ) : null}
```

- [ ] **Step 4: Run the tests to verify they pass.**

Run: `cd web && npm run test:run -- src/test/AgentPanel.tokens.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add web/src/components/AgentPanel.tsx web/src/test/AgentPanel.tokens.test.tsx
git commit -m "feat(web): clarify cumulative input and compaction state in the token panel"
```

---

### Task 4: Static checks and the full web test suite

- [ ] **Step 1: Type-check and build.**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: both succeed with no errors.

- [ ] **Step 2: Run the full web test suite.**

Run: `cd web && npm run test:run`
Expected: PASS. If unrelated pre-existing failures appear, record them and continue; if failures touch the changed files, fix before proceeding.

- [ ] **Step 3: Run the repo check.**

Run: `task check`
Expected: PASS (Go untouched; mostly a regression guard).

---

### Task 5: Real-browser validation (mandatory per AGENTS.md)

Use the `agent-browser` skill (CLI) against the running dev stack (`docker compose -f docker-compose.dev.yml ps`; web on `:5173`, API on `:8088`). Log in as `nova@heaven-labs.com` / `nova123`.

- [ ] **Step 1: Warehouse pattern field.**

1. Check the fixture: `docker exec aether-dev-aether-postgres-1 psql -U aether -d aether -c "SELECT w.id, w.name FROM warehouses w LIMIT 5;"`. If the dev org has no warehouse, create one from the Warehouses page (needs a ClickHouse connector to exist — check `SELECT id, name, type FROM connectors WHERE type='clickhouse' LIMIT 5;`; create one from Connectors → New Connector if missing).
2. `agent-browser open http://localhost:5173/warehouses`, expand a warehouse, and locate the Pattern textarea.
3. Type `^tmp\.a`, press `Shift+Enter`, type `^tmp\.b`, press `Enter`.
4. Expected: both patterns become chips in one shot; the textarea clears. Take a screenshot.
5. `agent-browser errors` → expect no page errors.

- [ ] **Step 2: Compaction marker.**

1. Create (or reuse) an agent chat session owned by nova: open an agent chat so `POST /agents/{id}/session` runs, then find it: `docker exec aether-dev-aether-postgres-1 psql -U aether -d aether -c "SELECT s.id FROM agent_sessions s JOIN users u ON u.id=s.user_id WHERE u.email='nova@heaven-labs.com' ORDER BY s.created_at DESC LIMIT 1;"`.
2. Seed a realistic compaction row:

```bash
docker exec aether-dev-aether-postgres-1 psql -U aether -d aether -c \
"INSERT INTO agent_messages (session_id, role, content, tokens_direct, tokens_after, kept_count, created_at) VALUES ('<session-id>', 'compaction', 'Validation summary: earlier context was summarized to stay within the window.', 812345, 180200, 8, NOW() - INTERVAL '1 minute');"
```

3. `agent-browser open http://localhost:5173/chats/<session-id>`.
4. Expected: a full-width centered marker `⚙ Context auto-compacted · 812k → ~180k`; clicking expands the summary, exact counts (812,345 actual → ~180,200 estimated) and timestamp; the token panel shows `Input (cumulative)` and `compacts at 70%`. Screenshot both states.
5. `agent-browser errors` → expect no page errors.

- [ ] **Step 3: Fix any defects found, re-run the affected tests, and commit fixes.**

```bash
git add -A
git commit -m "fix(web): <what the browser run surfaced>"
```

---

### Task 6: Impeccable detector + final review

- [ ] **Step 1: Run the design detector once, on the changed UI files.**

Run:
```bash
/home/jesus/.config/opencode/skills/impeccable/scripts/impeccable detect --json \
  web/src/components/WarehouseHiddenTables.tsx \
  web/src/components/AgentChatTranscript.tsx \
  web/src/components/AgentPanel.tsx
```
Expected: JSON findings. Resolve anything real (off-token colors, off-scale radii, banned patterns); ignore pure inline-style noise consistent with the codebase. Commit any fixes.

- [ ] **Step 2: Final verification before claiming done.**

Run: `cd web && npm run test:run && npx tsc --noEmit && npm run build`
Expected: all PASS. Evidence in hand before the completion claim.

- [ ] **Step 3: Wrap-up.**

Decide with the user: push the branch and open the PR (`gh pr create`), or stop at the local branch. PR body should cover both features and link the design doc.
