# Implementation Plan: Empty States, Onboarding & Polish (Group 4)

## Goal
Implement 6 UI/UX improvements across the hnb frontend (and one backend endpoint) to provide better empty states, guidance, and visual polish.

## Architecture
- **Frontend**: React + TypeScript, inline styles (no CSS modules), Lucide React icons
- **Backend**: Go, `net/http` ServeMux, `internal/api/` package
- **Shared component**: `web/src/components/EmptyState.tsx` (already exists)
- **API client**: `web/src/api/client.ts` — `api.post<T>(path, body)` pattern

## Tech Stack
- React 18 + TypeScript
- `@tanstack/react-query` for data fetching
- `lucide-react` for icons
- Go 1.22+ backend with `net/http`
- Recharts for chart rendering

---

## Task 1: Profile Status Character Limit Indicator (Issue 4)

**File**: `web/src/pages/ProfilePage.tsx`
**Effort**: ~5 min | **Risk**: Zero

### Step 1.1: Add character counter below status input

**Current code** (lines 89–93):
```tsx
            <label style={styles.label}>
            Status <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span>
            <input style={styles.input} value={status} placeholder="e.g. On vacation"
              onChange={e => setStatus(e.target.value)} maxLength={100} />
          </label>
```

**Replace with**:
```tsx
            <label style={styles.label}>
            Status <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span>
            <input style={styles.input} value={status} placeholder="e.g. On vacation"
              onChange={e => setStatus(e.target.value)} maxLength={100} />
            <span style={{
              fontSize: 11,
              color: status.length > 90 ? 'var(--error)' : 'var(--text-muted)',
              textAlign: 'right',
              marginTop: 2,
              display: 'block',
            }}>
              {status.length}/100
            </span>
          </label>
```

### Step 1.2: Commit
```bash
git add web/src/pages/ProfilePage.tsx
git commit -m "feat(profile): add character counter to status field"
```

---

## Task 2: Dashboard Widget Column Button Tooltips (Issue 6)

**File**: `web/src/pages/DashboardEditorPage.tsx`
**Effort**: ~5 min | **Risk**: Zero

### Step 2.1: Add `title` attributes to column buttons

**Current code** (lines 237–256):
```tsx
            {[6, 8, 12, 16, 24].map(c => (
              <button
                key={c}
                type="button"
                style={{
                  padding: '3px 8px',
                  fontSize: 12,
                  fontWeight: 600,
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  cursor: 'pointer',
                  background: gridCols === c ? 'var(--accent)' : 'var(--bg-input)',
                  color: gridCols === c ? '#fff' : 'var(--text-secondary)',
                }}
                onClick={async () => {
```

**Replace with** (add `title` prop):
```tsx
            {[6, 8, 12, 16, 24].map(c => (
              <button
                key={c}
                type="button"
                title={`${c} grid columns — ${c <= 8 ? 'compact' : c <= 12 ? 'standard' : 'wide'} layout`}
                style={{
                  padding: '3px 8px',
                  fontSize: 12,
                  fontWeight: 600,
                  border: '1px solid var(--border)',
                  borderRadius: 4,
                  cursor: 'pointer',
                  background: gridCols === c ? 'var(--accent)' : 'var(--bg-input)',
                  color: gridCols === c ? '#fff' : 'var(--text-secondary)',
                }}
                onClick={async () => {
```

### Step 2.2: Commit
```bash
git add web/src/pages/DashboardEditorPage.tsx
git commit -m "feat(dashboard): add tooltips to grid column buttons"
```

---

## Task 3: Enhanced Default Connector Badge (Issue 5)

**File**: `web/src/pages/ConnectorsPage.tsx`
**Effort**: ~10 min | **Risk**: Visual only

### Step 3.1: Add `Star` import from lucide-react

**Current code** (line 7):
```tsx
import { Check, X } from 'lucide-react'
```

**Replace with**:
```tsx
import { Check, X, Star } from 'lucide-react'
```

### Step 3.2: Upgrade the default connector badge

**Current code** (lines 272–279):
```tsx
                  {c.is_default && (
                    <span style={{ fontSize: 11, background: 'var(--accent-light)', border: '1px solid var(--border)',
                      borderRadius: 3, padding: '1px 6px', color: 'var(--text-secondary)', fontFamily: 'var(--font-mono)',
                      marginLeft: 8 }}>
                      default
                    </span>
                  )}
```

**Replace with**:
```tsx
                  {c.is_default && (
                    <span style={{
                      fontSize: 11,
                      background: 'var(--accent-light)',
                      border: '1px solid var(--accent)',
                      borderRadius: 10,
                      padding: '2px 8px',
                      color: 'var(--accent)',
                      fontWeight: 600,
                      marginLeft: 8,
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 3,
                    }}>
                      <Star size={10} fill="var(--accent)" />
                      Default
                    </span>
                  )}
```

### Step 3.3: Add tooltip to "Set default" button

**Current code** (lines 308–313):
```tsx
                  {!c.is_default && (
                    <button type="button"
                      style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4,
                        fontSize: 12, padding: '3px 10px', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 }}
                      onClick={() => setDefault.mutate(c.id)}>
                      Set default
                    </button>
                  )}
```

**Replace with**:
```tsx
                  {!c.is_default && (
                    <button type="button"
                      title="Set as default connector for new notebooks"
                      style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4,
                        fontSize: 12, padding: '3px 10px', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 }}
                      onClick={() => setDefault.mutate(c.id)}>
                      Set default
                    </button>
                  )}
```

### Step 3.4: Commit
```bash
git add web/src/pages/ConnectorsPage.tsx
git commit -m "feat(connectors): enhance default connector badge with star icon"
```

---

## Task 4: Chart View Empty State Guidance (Issue 2)

**File**: `web/src/components/ChartView.tsx`
**Effort**: ~15 min | **Risk**: Visual only

### Step 4.1: Replace bare `<p>` empty state with guided message

**Current code** (lines 113–115):
```tsx
  if (columns.length < 2) {
    return <p style={styles.empty}>Need at least 2 columns to chart</p>
  }
```

**Replace with**:
```tsx
  if (columns.length < 2) {
    return (
      <div style={styles.emptyGuidance}>
        <div style={styles.emptyIcon}>📊</div>
        <p style={styles.emptyTitle}>Not enough data to chart</p>
        <p style={styles.emptyText}>
          Charts need at least 2 columns — one for the X axis and one or more for Y values.
          Run a query that returns multiple columns, then switch to chart view.
        </p>
      </div>
    )
  }
```

### Step 4.2: Add "Configure" hint below chart when using defaults

**Current code** (lines 228–237, the return block with the chart):
```tsx
      <div>
        <button
          style={styles.configBtn}
          onClick={() => setShowConfig(v => !v)}
          aria-label={showConfig ? 'Close chart config' : 'Configure chart'}
        >
          <Settings2 size={13} />
          {showConfig ? ' Close' : ' Configure'}
        </button>
        {showConfig && (
          <ChartConfigPanel
            config={effectiveConfig}
            columns={columns}
            onChange={handleConfigChange}
          />
        )}
      </div>
```

**Replace with**:
```tsx
      <div>
        <button
          style={styles.configBtn}
          onClick={() => setShowConfig(v => !v)}
          aria-label={showConfig ? 'Close chart config' : 'Configure chart'}
        >
          <Settings2 size={13} />
          {showConfig ? ' Close' : ' Configure'}
        </button>
        {!showConfig && !effectiveConfig.chartType && (
          <span style={styles.configHint}>
            💡 Click "Configure" to change chart type, axes, and colors
          </span>
        )}
        {showConfig && (
          <ChartConfigPanel
            config={effectiveConfig}
            columns={columns}
            onChange={handleConfigChange}
          />
        )}
      </div>
```

### Step 4.3: Add new styles to the `styles` object

**Current code** (lines 244–260, the styles object):
```tsx
const styles: Record<string, React.CSSProperties> = {
  wrap: {
    width: '100%',
    padding: '12px 16px 4px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
  },
  configBtn: {
    fontSize: 11,
    color: 'var(--text-muted)',
    padding: '4px 12px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  empty: {
    padding: '16px',
    color: 'var(--text-muted)',
    fontSize: 13,
    textAlign: 'center',
  },
}
```

**Replace with**:
```tsx
const styles: Record<string, React.CSSProperties> = {
  wrap: {
    width: '100%',
    padding: '12px 16px 4px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
  },
  configBtn: {
    fontSize: 11,
    color: 'var(--text-muted)',
    padding: '4px 12px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  empty: {
    padding: '16px',
    color: 'var(--text-muted)',
    fontSize: 13,
    textAlign: 'center',
  },
  emptyGuidance: {
    padding: '32px 24px',
    textAlign: 'center',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: 8,
  },
  emptyIcon: { fontSize: 32, marginBottom: 4 },
  emptyTitle: { fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: 0 },
  emptyText: { fontSize: 13, color: 'var(--text-muted)', margin: 0, maxWidth: 360, lineHeight: 1.5 },
  configHint: { fontSize: 11, color: 'var(--text-muted)', marginLeft: 8, fontStyle: 'italic' },
}
```

### Step 4.4: Commit
```bash
git add web/src/components/ChartView.tsx
git commit -m "feat(chart): add guided empty state and configure hint"
```

---

## Task 5: Empty State Illustrations on 5 Pages (Issue 1)

**Files**: ConnectorsPage, AgentsPage, ModelsPage, SkillsPage, MCPPage
**Effort**: ~30 min | **Risk**: Visual only

### Step 5.1: ConnectorsPage — Replace inline empty state with `<EmptyState>`

**File**: `web/src/pages/ConnectorsPage.tsx`

#### 5.1.1: Add imports

**Current code** (lines 1–11):
```tsx
import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X, Star } from 'lucide-react'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { StatusBadge } from '../components/StatusBadge'
import { SectionHeader } from '../components/SectionHeader'
import { PermissionsPanel } from '../components/PermissionsPanel'
```

**Replace with** (add `Database` and `EmptyState`):
```tsx
import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X, Star, Database } from 'lucide-react'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { StatusBadge } from '../components/StatusBadge'
import { SectionHeader } from '../components/SectionHeader'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { EmptyState } from '../components/EmptyState'
```

#### 5.1.2: Add `isLoading` to useQuery destructuring

**Current code** (lines 57–60):
```tsx
  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })
```

**Replace with**:
```tsx
  const { data: connectors = [], isLoading } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })
```

#### 5.1.3: Replace the StyledTable block with conditional rendering

**Current code** (lines 261–324, the `<StyledTable>` block):
```tsx
        <StyledTable headers={['Name', 'Type', 'Host', 'Database', 'Status', '']}>
          {connectors.map((c) => {
            ...
          })}
          {connectors.length === 0 && (
            <tr>
              <td colSpan={6} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No connectors yet. Add one to connect to your databases.
              </td>
            </tr>
          )}
        </StyledTable>
```

**Replace with**:
```tsx
        {connectors.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Database size={28} />}
            title="No connectors yet"
            text="Add a connector to link your databases and start querying."
            action={{ label: '+ New Connector', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Type', 'Host', 'Database', 'Status', '']}>
            {connectors.map((c) => {
              const test = testResults[c.id]
              return (
                <tr key={c.id} style={rowStyle}>
                  <td style={cellStyle}>
                    <strong>{c.name}</strong>
                    {c.is_default && (
                      <span style={{
                        fontSize: 11,
                        background: 'var(--accent-light)',
                        border: '1px solid var(--accent)',
                        borderRadius: 10,
                        padding: '2px 8px',
                        color: 'var(--accent)',
                        fontWeight: 600,
                        marginLeft: 8,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 3,
                      }}>
                        <Star size={10} fill="var(--accent)" />
                        Default
                      </span>
                    )}
                  </td>
                  <td style={cellStyle}><code style={styles.badge}>{c.type}</code></td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    {c.config?.host ?? '—'}
                  </td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    {c.config?.database ?? '—'}
                  </td>
                  <td style={cellStyle}>
                    {testingId === c.id ? (
                      <StatusBadge status="neutral" label="Testing…" />
                    ) : test ? (
                      <StatusBadge
                        status={test.ok ? 'success' : 'error'}
                        label={test.ok ? 'Connected' : (test.error ?? 'Failed')}
                        icon={test.ok ? <Check size={12} /> : <X size={12} />}
                      />
                    ) : (
                      <StatusBadge status="neutral" label="—" />
                    )}
                  </td>
                  <td style={styles.tdActions}>
                    <button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)} disabled={testingId === c.id}>
                      {testingId === c.id ? 'Testing…' : 'Test'}
                    </button>
                    <button type="button" style={styles.editBtn} onClick={() => {
                      setEditing(c.id)
                      setEditForm({
                        name: c.name,
                        type: c.type as ConnectorType,
                        host: c.config?.host ?? '',
                        port: String(c.config?.port ?? 5432),
                        database: c.config?.database ?? '',
                        user: c.config?.user ?? '',
                        password: '',
                        ssl_mode: c.config?.ssl_mode ?? 'disable',
                        is_default: c.is_default ?? false,
                      })
                    }}>Edit</button>
                    <button type="button" style={styles.actionBtn} onClick={() => setPermissionsTarget({ type: 'connector', id: c.id, name: c.name })}>Permissions</button>
                    {!c.is_default && (
                      <button type="button"
                        title="Set as default connector for new notebooks"
                        style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4,
                          fontSize: 12, padding: '3px 10px', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 }}
                        onClick={() => setDefault.mutate(c.id)}>
                        Set default
                      </button>
                    )}
                    <button
                      type="button"
                      style={styles.deleteBtn}
                      onClick={() => { if (confirm(`Delete "${c.name}"?`)) deleteConnector.mutate(c.id) }}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              )
            })}
          </StyledTable>
        )}
```

> **Note**: This replaces both the empty-state `<tr>` and wraps the existing table rows in a conditional. The badge upgrade (Task 3) and tooltip (Task 3) are included here since they're in the same block.

### Step 5.2: AgentsPage — Replace inline empty state

**File**: `web/src/pages/AgentsPage.tsx`

#### 5.2.1: Add imports

**Current code** (lines 1–8):
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { api } from '../api/client'
import type { Agent, ModelConfig, Skill, MCPServerOrg } from '../types/agent'
```

**Replace with**:
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { api } from '../api/client'
import { Bot } from 'lucide-react'
import type { Agent, ModelConfig, Skill, MCPServerOrg } from '../types/agent'
```

#### 5.2.2: Replace the StyledTable block

**Current code** (lines 168–200):
```tsx
        <StyledTable headers={['Name', 'Model Config', 'Skills', 'MCP Servers', '']}>
          {agents.map((a) => {
            ...
          })}
          {agents.length === 0 && !isLoading && (
            <tr>
              <td colSpan={5} style={{ ...cellStyle, textAlign: 'center', padding: '48px 20px' }}>
                <div style={{ color: 'var(--text-muted)' }}>
                  <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 4 }}>No agents yet</div>
                  <div style={{ fontSize: 13, maxWidth: 360, margin: '0 auto', lineHeight: 1.5 }}>
                    Agents are AI assistants that can run queries, analyze data, and build notebooks for you.
                    Click &ldquo;+ New Agent&rdquo; to create your first one.
                  </div>
                </div>
              </td>
            </tr>
          )}
        </StyledTable>
```

**Replace with**:
```tsx
        {agents.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Bot size={28} />}
            title="No agents yet"
            text="Agents are AI assistants that can run queries, analyze data, and build notebooks for you."
            action={{ label: '+ New Agent', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Model Config', 'Skills', 'MCP Servers', '']}>
            {agents.map((a) => {
              const mc = modelConfigs.find(m => m.id === a.model_config_id)
              return (
                <tr key={a.id} style={rowStyle}>
                  <td style={cellStyle}>
                    <strong>{a.name}</strong>
                    {a.description && <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginTop: 2 }}>{a.description}</div>}
                  </td>
                  <td style={cellStyle}>
                    {mc ? <span style={styles.badge}>{mc.name}</span> : <span style={{ color: 'var(--text-muted)' }}>default</span>}
                  </td>
                  <td style={cellStyle}>
                    {a.skill_ids?.length
                      ? a.skill_ids.map(id => skills.find(s => s.id === id)?.name ?? id).filter(Boolean).join(', ') || '—'
                      : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                  </td>
                  <td style={cellStyle}>
                    {a.mcp_servers?.length
                      ? a.mcp_servers.map(m => m.name).join(', ')
                      : <span style={{ color: 'var(--text-muted)' }}>—</span>}
                  </td>
                  <td style={styles.tdActions}>
                    <button type="button" style={styles.editBtn} onClick={() => startEdit(a)}>Edit</button>
                    <button type="button" style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${a.name}"?`)) deleteMutation.mutate(a.id) }}>
                      Delete
                    </button>
                  </td>
                </tr>
              )
            })}
          </StyledTable>
        )}
```

### Step 5.3: ModelsPage — Replace inline empty state

**File**: `web/src/pages/ModelsPage.tsx`

#### 5.3.1: Add imports

**Current code** (lines 1–8):
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { api } from '../api/client'
import type { ModelConfig } from '../types/agent'
```

**Replace with**:
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { api } from '../api/client'
import { Brain } from 'lucide-react'
import type { ModelConfig } from '../types/agent'
```

#### 5.3.2: Replace the StyledTable block

**Current code** (lines 178–198):
```tsx
        <StyledTable headers={['Name', 'Provider', 'Endpoint', 'Model', 'Context Window', '']}>
          {configs.map((c) => (
            ...
          ))}
          {configs.length === 0 && !isLoading && (
            <tr>
              <td colSpan={6} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No model configs yet. Add one to configure AI providers for agents.
              </td>
            </tr>
          )}
        </StyledTable>
```

**Replace with**:
```tsx
        {configs.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Brain size={28} />}
            title="No model configs yet"
            text="Add a model configuration to connect AI providers for your agents."
            action={{ label: '+ New Model', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Provider', 'Endpoint', 'Model', 'Context Window', '']}>
            {configs.map((c) => (
              <tr key={c.id} style={rowStyle}>
                <td style={cellStyle}><strong>{c.name}</strong></td>
                <td style={cellStyle}><code style={styles.badge}>{c.provider}</code></td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{c.base_url}</td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{c.model}</td>
                <td style={{ ...cellStyle, fontSize: 12, color: 'var(--text-muted)' }}>{c.context_window?.toLocaleString() ?? '—'}</td>
                <td style={styles.tdActions}>
                  <button
                    type="button"
                    style={testMutation.isPending && testResult?.id === c.id ? { ...styles.testBtn, opacity: 0.6 } : styles.testBtn}
                    onClick={() => { setTestResult(null); testMutation.mutate(c.id) }}
                    disabled={testMutation.isPending}
                  >
                    {testMutation.isPending && testResult?.id === c.id ? 'Testing…' : 'Test'}
                  </button>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(c)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${c.name}"?`)) deleteMutation.mutate(c.id) }}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </StyledTable>
        )}
```

### Step 5.4: SkillsPage — Replace inline empty state

**File**: `web/src/pages/SkillsPage.tsx`

#### 5.4.1: Add imports

**Current code** (lines 1–8):
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { api } from '../api/client'
import type { Skill } from '../types/agent'
```

**Replace with**:
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { api } from '../api/client'
import { Zap } from 'lucide-react'
import type { Skill } from '../types/agent'
```

#### 5.4.2: Replace the StyledTable block

**Current code** (lines 152–172):
```tsx
        <StyledTable headers={['Name', 'Description', 'Tools', '']}>
          {skills.map((s) => (
            ...
          ))}
          {skills.length === 0 && !isLoading && (
            <tr>
              <td colSpan={4} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No skills yet. Skills are reusable AI behaviors you can attach to agents.
              </td>
            </tr>
          )}
        </StyledTable>
```

**Replace with**:
```tsx
        {skills.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Zap size={28} />}
            title="No skills yet"
            text="Skills are reusable AI behaviors you can attach to agents to give them specialized capabilities."
            action={{ label: '+ New Skill', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Description', 'Tools', '']}>
            {skills.map((s) => (
              <tr key={s.id} style={rowStyle}>
                <td style={cellStyle}><strong>{s.name}</strong></td>
                <td style={cellStyle}><span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>{s.description || '—'}</span></td>
                <td style={cellStyle}>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                    {(s.tool_ids ?? []).map(id => (
                      <span key={id} style={styles.toolTag}>{TOOL_OPTIONS.find(t => t.id === id)?.label ?? id}</span>
                    ))}
                    {(!s.tool_ids || s.tool_ids.length === 0) && <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>}
                  </div>
                </td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(s)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${s.name}"?`)) deleteMutation.mutate(s.id) }}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </StyledTable>
        )}
```

### Step 5.5: MCPPage — Replace inline empty state

**File**: `web/src/pages/MCPPage.tsx`

#### 5.5.1: Add imports

**Current code** (lines 1–8):
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { api } from '../api/client'
import type { MCPServerOrg } from '../types/agent'
```

**Replace with**:
```tsx
import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { FormCard } from '../components/FormCard'
import { EmptyState } from '../components/EmptyState'
import { api } from '../api/client'
import { Server } from 'lucide-react'
import type { MCPServerOrg } from '../types/agent'
```

#### 5.5.2: Replace the StyledTable block

**Current code** (lines 134–155):
```tsx
        <StyledTable headers={['Name', 'Type', 'Command', 'Args', '']}>
          {servers.map(s => (
            ...
          ))}
          {servers.length === 0 && !isLoading && (
            <tr>
              <td colSpan={5} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No MCP servers configured yet. Create one to extend agent capabilities.
              </td>
            </tr>
          )}
        </StyledTable>
```

**Replace with**:
```tsx
        {servers.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Server size={28} />}
            title="No MCP servers configured"
            text="MCP servers extend agent capabilities with external tools and data sources."
            action={{ label: '+ New MCP Server', onClick: () => setCreating(true) }}
          />
        ) : (
          <StyledTable headers={['Name', 'Type', 'Command', 'Args', '']}>
            {servers.map(s => (
              <tr key={s.id} style={rowStyle}>
                <td style={cellStyle}><strong>{s.name}</strong></td>
                <td style={cellStyle}><code style={styles.badge}>{s.type}</code></td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>{s.command}</td>
                <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{s.args?.join(' ') || '—'}</td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.editBtn} onClick={() => startEdit(s)}>Edit</button>
                  <button type="button" style={styles.deleteBtn} onClick={() => { if (confirm(`Delete "${s.name}"?`)) deleteMutation.mutate(s.id) }}>
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </StyledTable>
        )}
```

### Step 5.6: Commit all 5 pages
```bash
git add web/src/pages/ConnectorsPage.tsx web/src/pages/AgentsPage.tsx web/src/pages/ModelsPage.tsx web/src/pages/SkillsPage.tsx web/src/pages/MCPPage.tsx
git commit -m "feat: replace inline empty states with EmptyState component on all CRUD pages"
```

---

## Task 6: OIDC Provider "Test Connection" Button (Issue 3)

**Files**: `internal/api/sso_org_handlers.go`, `internal/api/router.go`, `web/src/pages/OrgSettingsPage.tsx`
**Effort**: ~45 min | **Risk**: Medium (new backend endpoint)

### Step 6.1: Add backend test endpoint handler

**File**: `internal/api/sso_org_handlers.go`

**Append** the following function at the end of the file (before the closing of the file, after `invalidateSSOOrgCache`):

```go
// handleOrgTestSSOProvider validates an OIDC discovery endpoint without persisting anything.
func (s *Server) handleOrgTestSSOProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DiscoveryURL string `json:"discovery_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "discovery_url is required")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(req.DiscoveryURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": fmt.Sprintf("failed to fetch discovery URL: %v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": fmt.Sprintf("discovery URL returned status %d", resp.StatusCode)})
		return
	}

	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid JSON response from discovery URL"})
		return
	}

	if discovery.Issuer == "" || discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "discovery document missing required fields (issuer, authorization_endpoint, token_endpoint)"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
```

**Note**: The `encoding/json` import is already present via `helpers.go` in the same package. The `fmt`, `net/http`, and `time` imports are already present in `sso_org_handlers.go`. Verify that `json` is imported — if not, add `"encoding/json"` to the import block at the top of `sso_org_handlers.go`.

Check the current imports in `sso_org_handlers.go`:
```go
import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/jackc/pgx/v5"
)
```

**Add `"encoding/json"` to the import block**:
```go
import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/jackc/pgx/v5"
)
```

### Step 6.2: Register the route

**File**: `internal/api/router.go`

**Current code** (line 243):
```go
	s.mux.Handle("PUT /api/v1/sso/settings", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgUpdateSSOSettings))))
```

**Add after it**:
```go
	s.mux.Handle("POST /api/v1/sso/test", authMW(RequireRole("admin")(http.HandlerFunc(s.handleOrgTestSSOProvider))))
```

### Step 6.3: Add backend test

**File**: `internal/api/sso_org_handlers_test.go`

**Append** the following test function at the end of the file:

```go
func TestOrgTestSSOProvider_DiscoveryValidation(t *testing.T) {
	// Start a test HTTP server that serves a valid OIDC discovery document
	discoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "https://example.com",
			"authorization_endpoint": "https://example.com/authorize",
			"token_endpoint":         "https://example.com/token",
		})
	}))
	defer discoveryServer.Close()

	srv := setupTestServer(t)
	t.Cleanup(func() { srv.Close() })

	adminToken := testJWT(t, srv, "org1", "admin-user", "admin")

	// Test with valid discovery URL
	body, _ := json.Marshal(map[string]string{"discovery_url": discoveryServer.URL})
	req := httptest.NewRequest("POST", "/api/v1/sso/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var result map[string]any
	json.NewDecoder(rec.Body).Decode(&result)
	if result["ok"] != true {
		t.Fatalf("expected ok=true, got %v", result)
	}

	// Test with invalid URL
	body, _ = json.Marshal(map[string]string{"discovery_url": "https://invalid.example.com/.well-known/openid-configuration"})
	req = httptest.NewRequest("POST", "/api/v1/sso/test", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	json.NewDecoder(rec.Body).Decode(&result)
	if result["ok"] != false {
		t.Fatalf("expected ok=false for invalid URL, got %v", result)
	}
}
```

> **Note**: Ensure `bytes`, `encoding/json`, `net/http`, `net/http/httptest`, and `testing` are imported in the test file. Check existing imports at the top of `sso_org_handlers_test.go`.

### Step 6.4: Run backend tests
```bash
task test:api
```
**Expected**: All tests pass, including the new `TestOrgTestSSOProvider_DiscoveryValidation`.

### Step 6.5: Add "Test Connection" button to ProviderForm (frontend)

**File**: `web/src/pages/OrgSettingsPage.tsx`

#### 6.5.1: Add state and handler to ProviderForm

**Current code** (lines 40–106, the ProviderForm function):
```tsx
function ProviderForm({
  initial,
  isEdit,
  onSave,
  onCancel,
  saving,
  error,
}: {
  initial: ProviderFormValues
  isEdit: boolean
  onSave: (values: ProviderFormValues) => void
  onCancel: () => void
  saving: boolean
  error: string | null
}) {
  const [values, setValues] = useState<ProviderFormValues>(initial)

  const set = (field: keyof ProviderFormValues) =>
    (e: React.ChangeEvent<HTMLInputElement>) =>
      setValues(v => ({ ...v, [field]: field === 'enabled' ? (e.target as HTMLInputElement).checked : e.target.value }))

  return (
    ...
```

**Replace with**:
```tsx
function ProviderForm({
  initial,
  isEdit,
  onSave,
  onCancel,
  saving,
  error,
}: {
  initial: ProviderFormValues
  isEdit: boolean
  onSave: (values: ProviderFormValues) => void
  onCancel: () => void
  saving: boolean
  error: string | null
}) {
  const [values, setValues] = useState<ProviderFormValues>(initial)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; error?: string } | null>(null)

  const set = (field: keyof ProviderFormValues) =>
    (e: React.ChangeEvent<HTMLInputElement>) =>
      setValues(v => ({ ...v, [field]: field === 'enabled' ? (e.target as HTMLInputElement).checked : e.target.value }))

  const handleTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await api.post<{ ok: boolean; error?: string }>('/api/v1/sso/test', {
        discovery_url: values.discovery_url,
      })
      setTestResult(res)
    } catch {
      setTestResult({ ok: false, error: 'Request failed' })
    } finally {
      setTesting(false)
    }
  }

  return (
    ...
```

#### 6.5.2: Replace the form actions section

**Current code** (lines 96–106):
```tsx
      {error && <div style={formStyles.error}>{error}</div>}
      <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
        <button
          style={{ ...formStyles.btn, opacity: saving ? 0.6 : 1 }}
          onClick={() => onSave(values)}
          disabled={saving}
        >
          {saving ? 'Saving…' : isEdit ? 'Save Changes' : 'Add Provider'}
        </button>
        <button style={formStyles.cancelBtn} onClick={onCancel} disabled={saving}>
          Cancel
        </button>
      </div>
```

**Replace with**:
```tsx
      {error && <div style={formStyles.error}>{error}</div>}
      <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
        <button
          style={formStyles.testBtn}
          onClick={handleTest}
          disabled={testing || !values.discovery_url}
        >
          {testing ? 'Testing…' : 'Test Connection'}
        </button>
        {testResult && (
          <span style={{
            fontSize: 12,
            color: testResult.ok ? 'var(--success, #22c55e)' : 'var(--error)',
            alignSelf: 'center',
          }}>
            {testResult.ok ? '✓ Discovery OK' : `✗ ${testResult.error}`}
          </span>
        )}
        <span style={{ flex: 1 }} />
        <button
          style={{ ...formStyles.btn, opacity: saving ? 0.6 : 1 }}
          onClick={() => onSave(values)}
          disabled={saving}
        >
          {saving ? 'Saving…' : isEdit ? 'Save Changes' : 'Add Provider'}
        </button>
        <button style={formStyles.cancelBtn} onClick={onCancel} disabled={saving}>
          Cancel
        </button>
      </div>
```

#### 6.5.3: Add `testBtn` style to formStyles

**Current code** (formStyles object, around line 130):
```tsx
  cancelBtn: {
    padding: '7px 16px',
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    cursor: 'pointer',
  },
```

**Add `testBtn` right before `cancelBtn`**:
```tsx
  testBtn: {
    padding: '7px 16px',
    background: 'none',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  cancelBtn: {
    padding: '7px 16px',
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    cursor: 'pointer',
  },
```

### Step 6.6: Commit
```bash
git add internal/api/sso_org_handlers.go internal/api/router.go internal/api/sso_org_handlers_test.go web/src/pages/OrgSettingsPage.tsx
git commit -m "feat(sso): add Test Connection button to OIDC provider form"
```

---

## Task 7: Verification

### Step 7.1: Run all frontend build checks
```bash
cd web && npx tsc --noEmit
```
**Expected**: No TypeScript errors.

### Step 7.2: Run backend tests
```bash
task test
```
**Expected**: All tests pass.

### Step 7.3: Visual verification (manual)
Start dev servers:
```bash
task dev        # Go API server
task dev:web    # Vite dev server
```

Check each page with empty data:
1. `/connectors` — should show Database icon + "No connectors yet" + "+ New Connector" button
2. `/agents` — should show Bot icon + "No agents yet" + "+ New Agent" button
3. `/models` — should show Brain icon + "No model configs yet" + "+ New Model" button
4. `/skills` — should show Zap icon + "No skills yet" + "+ New Skill" button
5. `/mcps` — should show Server icon + "No MCP servers configured" + "+ New MCP Server" button
6. `/profile` — status field should show "0/100" counter below input
7. `/dashboards/:id` — column buttons should show tooltips on hover
8. `/settings` — OIDC provider form should have "Test Connection" button
9. Chart view with <2 columns — should show 📊 icon + guidance text
10. Connectors page with data — default connector should have star badge

### Step 7.4: Final commit (if any fixes needed)
```bash
git add -A
git commit -m "fix: address review feedback on empty states polish"
```

---

## Summary

| Task | Issue | File(s) | Complexity | Dependencies |
|------|-------|---------|------------|--------------|
| 1 | Status char counter | `ProfilePage.tsx` | Trivial | None |
| 2 | Column tooltips | `DashboardEditorPage.tsx` | Trivial | None |
| 3 | Default badge | `ConnectorsPage.tsx` | Low | `Star` from lucide-react |
| 4 | Chart guidance | `ChartView.tsx` | Low | None |
| 5 | Empty states (×5) | 5 page files | Medium | `EmptyState` component |
| 6 | OIDC test button | `OrgSettingsPage.tsx` + backend | Medium | New `POST /api/v1/sso/test` endpoint |

**Total estimated effort: ~1.5 hours**
