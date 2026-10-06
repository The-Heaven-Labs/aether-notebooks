import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, BarChart3 } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { AppShell } from '../components/AppShell'
import { EmptyState } from '../components/EmptyState'
import { SectionHeader } from '../components/SectionHeader'
import { ErrorBanner } from '../components/ErrorBanner'
import { EChartsContainer, getTooltipStyle, useChartColors } from '../charts/common'
import {
  computeKpis,
  deriveAgents,
  deriveUsers,
  rangeToQuery,
  buildAgentTable,
  buildTokenChartOption,
  formatCost,
  formatTokens,
  type StatRow,
  type RangeKey,
} from '../utils/agentStats'

const RANGES: { key: RangeKey; label: string }[] = [
  { key: 'hour', label: 'Last hour' },
  { key: 'today', label: 'Today' },
  { key: '7d', label: '7d' },
  { key: '30d', label: '30d' },
  { key: 'custom', label: 'Custom' },
]

type SortCol = 'agent' | 'sessions' | 'messages' | 'in' | 'out' | 'subagent' | 'avgMs' | 'cost'

export function StatsPage() {
  const { user } = useAuth()
  const qc = useQueryClient()
  const [agentFilter, setAgentFilter] = useState('')
  const [userFilter, setUserFilter] = useState('')
  const [range, setRange] = useState<RangeKey>('7d')
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [chartMode, setChartMode] = useState<'tokens' | 'cost'>('tokens')
  const [sortCol, setSortCol] = useState<SortCol>('out')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')
  const [rollupMsg, setRollupMsg] = useState<string | null>(null)

  const q = useMemo(() => rangeToQuery(range, customFrom, customTo), [range, customFrom, customTo])

  const { data, isLoading, error } = useQuery({
    queryKey: ['agent-stats', agentFilter, userFilter, q.from, q.to, q.granularity],
    queryFn: () => {
      const params = new URLSearchParams({
        from: q.from,
        to: q.to,
        granularity: q.granularity,
      })
      if (agentFilter) params.set('agent_id', agentFilter)
      if (userFilter) params.set('user_id', userFilter)
      return api.get<StatRow[]>(`/api/v1/agents/stats?${params}`)
    },
  })

  const rollup = useMutation({
    mutationFn: () => api.post<{ rolled_up: { rows: number } }>('/api/v1/agents/stats/rollup'),
    onSuccess: (res) => {
      setRollupMsg(`Rolled up ${res.rolled_up.rows} bucket row(s)`)
      qc.invalidateQueries({ queryKey: ['agent-stats'] })
    },
    onError: (e: Error) => setRollupMsg(`Roll up failed: ${e.message}`),
  })

  const rows = data ?? []
  const kpis = useMemo(() => computeKpis(rows), [rows])
  const agents = useMemo(() => deriveAgents(rows), [rows])
  const users = useMemo(() => deriveUsers(rows), [rows])
  const table = useMemo(() => {
    const t = buildAgentTable(rows)
    const dir = sortDir === 'asc' ? 1 : -1
    const val = (a: (typeof t)[number]): number | string => {
      switch (sortCol) {
        case 'agent': return a.agent_name
        case 'sessions': return a.sessions
        case 'messages': return a.messages
        case 'in': return a.tokensIn
        case 'out': return a.tokensOut
        case 'subagent': return a.tokensSubagent
        case 'avgMs': return a.avgDurationMs
        case 'cost': return a.cost
      }
    }
    return [...t].sort((a, b) => {
      const va = val(a)
      const vb = val(b)
      if (typeof va === 'string') return va.localeCompare(vb as string) * dir
      return ((va as number) - (vb as number)) * dir
    })
  }, [rows, sortCol, sortDir])
  const chartColors = useChartColors()
  const chartOption = useMemo(() => {
    const opt = buildTokenChartOption(rows, { mode: chartMode, granularity: q.granularity, singleAgent: !!agentFilter })
    // The stats chart builds its own option; give its tooltip the shared
    // theme treatment (dark card, mono text, viewport-clamped, appended to
    // <body>) instead of ECharts' default white box.
    return {
      ...opt,
      tooltip: { ...(opt.tooltip as Record<string, unknown>), ...getTooltipStyle(chartColors) },
    }
  }, [rows, chartMode, q.granularity, agentFilter, chartColors])
  const showUserCol = !agentFilter && users.length > 1

  function handleSort(col: SortCol) {
    if (sortCol === col) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortCol(col)
      setSortDir(col === 'agent' ? 'asc' : 'desc')
    }
  }

  if (user && user.role !== 'admin') {
    return (
      <AppShell>
        <div style={styles.content}>
          <EmptyState title="Admin access required" text="Agent usage statistics are visible to organization admins." />
        </div>
      </AppShell>
    )
  }

  const kpiCards: { label: string; value: string }[] = [
    { label: 'Sessions', value: kpis.sessions.toLocaleString('en-US') },
    { label: 'Messages', value: kpis.messages.toLocaleString('en-US') },
    { label: 'Model calls', value: kpis.modelCalls.toLocaleString('en-US') },
    { label: 'Tokens in', value: formatTokens(kpis.tokensIn) },
    { label: 'Tokens out', value: formatTokens(kpis.tokensOut) },
    { label: 'Subagent tokens', value: formatTokens(kpis.subagent) },
    { label: 'Est. cost', value: formatCost(kpis.cost) },
  ]

  // Titles name every series so the single-agent bar+line view explains itself,
  // and disclose that all-agents bars include subagent output. Buckets are UTC.
  const chartTitle = chartMode === 'cost'
    ? (agentFilter ? 'Cost per bucket (UTC)' : 'Cost per bucket, by agent (UTC)')
    : (agentFilter ? 'Tokens in vs out per bucket (UTC)' : 'Tokens out per bucket, by agent (incl. subagent, UTC)')

  return (
    <AppShell>
      <div style={styles.content}>
        <SectionHeader title="Agent Usage" subtitle="Token usage, cost estimates, and activity across agents">
          <button
            style={styles.rollupButton}
            onClick={() => { setRollupMsg(null); rollup.mutate() }}
            disabled={rollup.isPending}
            title="Refreshes the current/previous hour aggregates"
          >
            <RefreshCw size={13} style={rollup.isPending ? { animation: 'spin 1s linear infinite' } : undefined} />
            {rollup.isPending ? ' Rolling up…' : ' Roll up now'}
          </button>
        </SectionHeader>
        {rollupMsg && <div style={styles.notice}>{rollupMsg}</div>}
        {error && <ErrorBanner message={`Failed to load stats: ${(error as Error).message}`} />}

        <div style={styles.filterBar}>
          <select aria-label="Filter by agent" style={styles.select} value={agentFilter} onChange={(e) => setAgentFilter(e.target.value)}>
            <option value="">All agents</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>{a.name}</option>
            ))}
          </select>
          <select aria-label="Filter by user" style={styles.select} value={userFilter} onChange={(e) => setUserFilter(e.target.value)}>
            <option value="">All users</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>{u.email || u.name}</option>
            ))}
          </select>
          <select aria-label="Time range" style={styles.select} value={range} onChange={(e) => setRange(e.target.value as RangeKey)}>
            {RANGES.map((r) => (
              <option key={r.key} value={r.key}>{r.label}</option>
            ))}
          </select>
          {range === 'custom' && (
            <>
              <input aria-label="Custom from" type="datetime-local" style={styles.select} value={customFrom} onChange={(e) => setCustomFrom(e.target.value)} />
              <input aria-label="Custom to" type="datetime-local" style={styles.select} value={customTo} onChange={(e) => setCustomTo(e.target.value)} />
            </>
          )}
          <div style={styles.toggle}>
            <button style={chartMode === 'tokens' ? styles.toggleActive : styles.toggleBtn} onClick={() => setChartMode('tokens')}>Tokens</button>
            <button style={chartMode === 'cost' ? styles.toggleActive : styles.toggleBtn} onClick={() => setChartMode('cost')}>Cost</button>
          </div>
        </div>

        <div style={styles.kpis}>
          {kpiCards.map((k) => (
            <div key={k.label} style={styles.kpi}>
              <div style={styles.kpiValue}>{isLoading ? '…' : k.value}</div>
              <div style={styles.kpiLabel}>{k.label}</div>
            </div>
          ))}
        </div>

        <div style={{ ...styles.card, padding: 22.4 }}>
          <div style={{ ...styles.cardTitle, marginBottom: 16.8 }}><BarChart3 size={14} /> {chartTitle}</div>
          {rows.length === 0 && !isLoading ? (
            <EmptyState title="No usage yet" text="Chat with an agent, then roll up to see activity here." />
          ) : (
            <EChartsContainer option={chartOption as never} height={280} />
          )}
        </div>

        <div style={styles.card}>
          <div style={styles.cardTitle}>Agents</div>
          <div style={styles.tableWrap}>
            <table style={styles.table}>
              <thead>
                <tr>
                  <th style={styles.th}><SortHeader label="Agent" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  {showUserCol && <th style={styles.th}><SortHeader label="User" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>}
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="Sessions" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="Messages" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="In" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="Out" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="Subagent" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="Avg ms" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                  <th style={{ ...styles.th, ...styles.thNum }}><SortHeader label="Est. cost" sortCol={sortCol} sortDir={sortDir} onSort={handleSort} /></th>
                </tr>
              </thead>
              <tbody>
                {table.map((a, i) => {
                  const isLast = i === table.length - 1
                  const cell = (kind?: React.CSSProperties): React.CSSProperties => ({ ...styles.td, ...kind, ...(isLast ? styles.tdLast : {}) })
                  return (
                    <tr key={a.agent_id} title="Filter to this agent" onClick={() => setAgentFilter(a.agent_id)} style={{ cursor: 'pointer' }}>
                      <td style={cell(styles.tdName)}>{a.agent_name}</td>
                      {showUserCol && <td style={cell(styles.tdUser)}>{a.user_email || a.user_name}</td>}
                      <td style={cell(styles.tdNum)}>{a.sessions.toLocaleString('en-US')}</td>
                      <td style={cell(styles.tdNum)}>{a.messages.toLocaleString('en-US')}</td>
                      <td style={cell(styles.tdNum)}>{formatTokens(a.tokensIn)}</td>
                      <td style={cell(styles.tdNum)}>{formatTokens(a.tokensOut)}</td>
                      <td style={cell(styles.tdNum)}>{formatTokens(a.tokensSubagent)}</td>
                      <td style={cell(styles.tdNum)}>{a.avgDurationMs.toLocaleString('en-US')}</td>
                      <td style={cell(styles.tdNum)}>{formatCost(a.cost)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </AppShell>
  )
}

const COL_KEYS: Record<string, 'agent' | 'sessions' | 'messages' | 'in' | 'out' | 'subagent' | 'avgMs' | 'cost' | null> = {
  Agent: 'agent',
  Sessions: 'sessions',
  Messages: 'messages',
  In: 'in',
  Out: 'out',
  Subagent: 'subagent',
  'Avg ms': 'avgMs',
  'Est. cost': 'cost',
  User: null,
}

function SortHeader({ label, sortCol, sortDir, onSort }: {
  label: string
  sortCol: string
  sortDir: 'asc' | 'desc'
  onSort: (c: 'agent' | 'sessions' | 'messages' | 'in' | 'out' | 'subagent' | 'avgMs' | 'cost') => void
}) {
  const key = COL_KEYS[label]
  if (!key) return <>{label}</>
  const active = sortCol === key
  return (
    <span onClick={(e) => { e.stopPropagation(); onSort(key) }} style={{ cursor: 'pointer', userSelect: 'none' }}>
      {label} {active ? (sortDir === 'asc' ? '▲' : '▼') : ''}
    </span>
  )
}

const styles: Record<string, React.CSSProperties> = {
  content: { maxWidth: 1200, margin: '0 auto', padding: '24px 16px' },
  rollupButton: {
    display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13, fontWeight: 600,
    color: 'var(--text-primary)', background: 'var(--bg-elevated)', border: '1px solid var(--border)',
    borderRadius: 6, padding: '7px 12px', cursor: 'pointer',
  },
  notice: { fontSize: 12, color: 'var(--text-muted)', marginBottom: 12 },
  filterBar: { display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginBottom: 16 },
  select: {
    fontSize: 13, color: 'var(--text-primary)', background: 'var(--bg-primary)',
    border: '1px solid var(--border)', borderRadius: 6, padding: '7px 10px',
  },
  toggle: { display: 'inline-flex', border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' },
  toggleBtn: { fontSize: 12, padding: '7px 12px', background: 'none', border: 'none', color: 'var(--text-muted)', cursor: 'pointer' },
  toggleActive: { fontSize: 12, padding: '7px 12px', background: 'var(--accent-light)', border: 'none', color: 'var(--accent)', cursor: 'pointer', fontWeight: 600 },
  kpis: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(120px, 1fr))', gap: 12, marginBottom: 16 },
  kpi: { background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 8, padding: '14px 16px', minWidth: 0 },
  kpiValue: {
    fontSize: 22, fontWeight: 700, color: 'var(--text-primary)',
    minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
  },
  kpiLabel: { fontSize: 12, color: 'var(--text-muted)', marginTop: 2 },
  card: { background: 'var(--bg-card)', border: '1px solid var(--border)', borderRadius: 8, padding: 16, marginBottom: 16 },
  cardTitle: { display: 'flex', alignItems: 'center', gap: 6, fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 12 },
  tableWrap: { borderRadius: 4, overflowX: 'auto', overflowY: 'hidden', border: '1px solid var(--border)' },
  table: { width: '100%', borderCollapse: 'collapse', background: 'var(--bg-card)' },
  th: { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', letterSpacing: '0.06em', borderBottom: '1px solid var(--border)', background: 'var(--bg-card)', fontFamily: 'var(--font-mono)', textTransform: 'uppercase' },
  thNum: { textAlign: 'right' },
  td: { padding: '12px 16px', fontSize: 13, color: 'var(--text-primary)', borderBottom: '1px solid var(--border-light)', fontVariantNumeric: 'tabular-nums' },
  tdLast: { borderBottom: 'none' },
  tdName: { fontWeight: 600 },
  tdUser: { fontFamily: 'var(--font-mono)', fontSize: 11.5, color: 'var(--text-muted)' },
  tdNum: { fontFamily: 'var(--font-mono)', fontSize: 12, textAlign: 'right' },
}
