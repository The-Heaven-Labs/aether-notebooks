export interface StatRow {
  bucket_start: string
  agent_id: string
  agent_name: string
  user_id: string
  user_name: string
  user_email: string
  sessions_count: number
  messages_count: number
  tokens_input: number
  tokens_output: number
  tokens_direct: number
  tokens_subagent: number
  model_calls: number
  total_duration_ms: number
  est_cost_usd: number
}

export interface Kpis {
  sessions: number
  messages: number
  modelCalls: number
  tokensIn: number
  tokensOut: number
  cost: number
}

export function computeKpis(rows: StatRow[]): Kpis {
  return rows.reduce<Kpis>(
    (acc, r) => ({
      sessions: acc.sessions + (r.sessions_count || 0),
      messages: acc.messages + (r.messages_count || 0),
      modelCalls: acc.modelCalls + (r.model_calls || 0),
      tokensIn: acc.tokensIn + (r.tokens_input || 0),
      tokensOut: acc.tokensOut + (r.tokens_output || 0),
      cost: acc.cost + (r.est_cost_usd || 0),
    }),
    { sessions: 0, messages: 0, modelCalls: 0, tokensIn: 0, tokensOut: 0, cost: 0 },
  )
}

export function deriveAgents(rows: StatRow[]): { id: string; name: string }[] {
  const map = new Map<string, string>()
  for (const r of rows) {
    if (r.agent_id && !map.has(r.agent_id)) map.set(r.agent_id, r.agent_name || r.agent_id)
  }
  return [...map.entries()]
    .map(([id, name]) => ({ id, name }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

export function deriveUsers(rows: StatRow[]): { id: string; name: string; email: string }[] {
  const map = new Map<string, { name: string; email: string }>()
  for (const r of rows) {
    if (r.user_id && !map.has(r.user_id)) {
      map.set(r.user_id, { name: r.user_name || r.user_email || r.user_id, email: r.user_email || '' })
    }
  }
  return [...map.entries()]
    .map(([id, v]) => ({ id, ...v }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

export type RangeKey = 'hour' | 'today' | '7d' | '30d' | 'custom'

export interface RangeQuery {
  from: string
  to: string
  granularity: 'hour' | 'day'
}

function startOfDay(d: Date): Date {
  const c = new Date(d)
  c.setHours(0, 0, 0, 0)
  return c
}

export function rangeToQuery(range: RangeKey, customFrom: string, customTo: string, now = new Date()): RangeQuery {
  switch (range) {
    case 'hour':
      return { from: new Date(now.getTime() - 3600_000).toISOString(), to: now.toISOString(), granularity: 'hour' }
    case 'today':
      return { from: startOfDay(now).toISOString(), to: now.toISOString(), granularity: 'hour' }
    case '7d':
      return { from: new Date(now.getTime() - 7 * 86400_000).toISOString(), to: now.toISOString(), granularity: 'day' }
    case 'custom': {
      const from = customFrom ? new Date(customFrom) : new Date(now.getTime() - 7 * 86400_000)
      const to = customTo ? new Date(customTo) : now
      const spanMs = to.getTime() - from.getTime()
      return { from: from.toISOString(), to: to.toISOString(), granularity: spanMs <= 48 * 3600_000 ? 'hour' : 'day' }
    }
    case '30d':
    default:
      return { from: new Date(now.getTime() - 30 * 86400_000).toISOString(), to: now.toISOString(), granularity: 'day' }
  }
}

export interface AgentAgg {
  agent_id: string
  agent_name: string
  user_name: string
  user_email: string
  sessions: number
  messages: number
  tokensIn: number
  tokensOut: number
  tokensSubagent: number
  avgDurationMs: number
  cost: number
}

// buildAgentTable aggregates rows per agent (plus user attribution when the
// result spans users), sorted by tokens out descending.
export function buildAgentTable(rows: StatRow[]): AgentAgg[] {
  const map = new Map<string, AgentAgg>()
  const calls = new Map<string, number>()
  const durations = new Map<string, number>()
  for (const r of rows) {
    let agg = map.get(r.agent_id)
    if (!agg) {
      agg = {
        agent_id: r.agent_id,
        agent_name: r.agent_name || r.agent_id,
        user_name: r.user_name || '',
        user_email: r.user_email || '',
        sessions: 0,
        messages: 0,
        tokensIn: 0,
        tokensOut: 0,
        tokensSubagent: 0,
        avgDurationMs: 0,
        cost: 0,
      }
      map.set(r.agent_id, agg)
      calls.set(r.agent_id, 0)
      durations.set(r.agent_id, 0)
    }
    agg.sessions += r.sessions_count || 0
    agg.messages += r.messages_count || 0
    agg.tokensIn += r.tokens_input || 0
    agg.tokensOut += r.tokens_output || 0
    agg.tokensSubagent += r.tokens_subagent || 0
    agg.cost += r.est_cost_usd || 0
    calls.set(r.agent_id, (calls.get(r.agent_id) || 0) + (r.model_calls || 0))
    durations.set(r.agent_id, (durations.get(r.agent_id) || 0) + (r.total_duration_ms || 0))
  }
  const out = [...map.values()]
  for (const a of out) {
    const n = calls.get(a.agent_id) || 0
    a.avgDurationMs = n > 0 ? Math.round((durations.get(a.agent_id) || 0) / n) : 0
  }
  return out.sort((a, b) => b.tokensOut - a.tokensOut || b.tokensIn - a.tokensIn)
}

function bucketLabel(iso: string, granularity: 'hour' | 'day'): string {
  const d = new Date(iso)
  if (granularity === 'hour') {
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// buildTokenChartOption stacks tokens-out per bucket (top 8 agents + "other"),
// toggles to a cost view, and overlays tokens-in as a line in single-agent mode.
export function buildTokenChartOption(
  rows: StatRow[],
  opts: { mode: 'tokens' | 'cost'; granularity: 'hour' | 'day'; singleAgent: boolean },
): Record<string, unknown> {
  const buckets = [...new Set(rows.map((r) => r.bucket_start))].sort()
  const valueOf = (r: StatRow) => (opts.mode === 'cost' ? r.est_cost_usd || 0 : r.tokens_output || 0)
  const totals = new Map<string, number>()
  for (const r of rows) totals.set(r.agent_id, (totals.get(r.agent_id) || 0) + valueOf(r))
  const ranked = [...totals.entries()].sort((a, b) => b[1] - a[1])
  const top = new Set(ranked.slice(0, 8).map(([id]) => id))
  const names = new Map<string, string>()
  for (const r of rows) {
    if (!names.has(r.agent_id)) names.set(r.agent_id, r.agent_name || r.agent_id)
  }

  const sumFor = (bucket: string, pred: (r: StatRow) => boolean) =>
    rows.filter((r) => r.bucket_start === bucket && pred(r)).reduce((a, r) => a + valueOf(r), 0)

  const series: Record<string, unknown>[] = []
  for (const [id] of ranked.slice(0, 8)) {
    series.push({
      name: names.get(id) || id,
      type: 'bar',
      stack: 'total',
      data: buckets.map((b) => sumFor(b, (r) => r.agent_id === id)),
    })
  }
  if (ranked.length > 8) {
    series.push({
      name: 'other',
      type: 'bar',
      stack: 'total',
      data: buckets.map((b) => sumFor(b, (r) => !top.has(r.agent_id))),
    })
  }
  if (opts.singleAgent && opts.mode === 'tokens') {
    series.push({
      name: 'tokens in',
      type: 'line',
      data: buckets.map((b) => rows.filter((r) => r.bucket_start === b).reduce((a, r) => a + (r.tokens_input || 0), 0)),
    })
  }

  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { show: series.length > 1 && series.length <= 10 },
    grid: { top: 32, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'category', data: buckets.map((b) => bucketLabel(b, opts.granularity)) },
    yAxis: { type: 'value' },
    series,
  }
}

export function formatCost(usd: number): string {
  return '$' + usd.toLocaleString('en-US', { minimumFractionDigits: 4, maximumFractionDigits: 4 })
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k'
  return String(Math.round(n))
}
