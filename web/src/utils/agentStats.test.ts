import { describe, it, expect } from 'vitest'
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
} from '../utils/agentStats'

const NOW = new Date('2026-09-11T12:00:00Z')

function row(over: Partial<StatRow> = {}): StatRow {
  return {
    bucket_start: '2026-09-11T10:00:00Z',
    agent_id: 'a1',
    agent_name: 'Alpha',
    user_id: 'u1',
    user_name: 'Ann',
    user_email: 'ann@example.com',
    sessions_count: 1,
    messages_count: 2,
    tokens_input: 100,
    tokens_output: 50,
    tokens_direct: 7,
    tokens_subagent: 5,
    model_calls: 2,
    total_duration_ms: 3000,
    est_cost_usd: 0.0006,
    ...over,
  }
}

describe('computeKpis', () => {
  it('sums across rows', () => {
    const k = computeKpis([row(), row({ sessions_count: 2, tokens_input: 10, est_cost_usd: 0.0001 })])
    expect(k).toMatchObject({ sessions: 3, messages: 4, modelCalls: 4, tokensIn: 110, tokensOut: 100 })
    expect(k.cost).toBeCloseTo(0.0007, 10)
  })

  it('handles empty input', () => {
    expect(computeKpis([])).toMatchObject({ sessions: 0, cost: 0 })
  })
})

describe('deriveAgents / deriveUsers', () => {
  it('dedupes and sorts', () => {
    const rows = [row(), row({ agent_id: 'a2', agent_name: 'Beta' })]
    expect(deriveAgents(rows).map((a) => a.name)).toEqual(['Alpha', 'Beta'])
    expect(deriveUsers(rows)).toHaveLength(1)
  })
})

describe('rangeToQuery', () => {
  it('maps presets', () => {
    expect(rangeToQuery('hour', '', '', NOW).granularity).toBe('hour')
    expect(rangeToQuery('today', '', '', NOW).granularity).toBe('hour')
    expect(rangeToQuery('7d', '', '', NOW).granularity).toBe('day')
    const h = rangeToQuery('hour', '', '', NOW)
    expect(new Date(h.to).getTime() - new Date(h.from).getTime()).toBe(3600_000)
  })

  it('picks granularity by custom span', () => {
    expect(rangeToQuery('custom', '2026-09-11T10:00', '2026-09-11T11:00', NOW).granularity).toBe('hour')
    expect(rangeToQuery('custom', '2026-09-01T10:00', '2026-09-11T11:00', NOW).granularity).toBe('day')
  })
})

describe('buildAgentTable', () => {
  it('aggregates per agent and sorts by tokens out', () => {
    const t = buildAgentTable([
      row(),
      row({ agent_id: 'a2', agent_name: 'Beta', tokens_output: 500, total_duration_ms: 1000, model_calls: 1 }),
    ])
    expect(t.map((a) => a.agent_id)).toEqual(['a2', 'a1'])
    expect(t[1].avgDurationMs).toBe(1500)
  })
})

describe('buildTokenChartOption', () => {
  function manyAgents(n: number) {
    return Array.from({ length: n }, (_, i) =>
      row({ agent_id: `a${i}`, agent_name: `Agent ${i}`, tokens_output: 100 - i }),
    )
  }

  it('caps at top 8 + other', () => {
    const opt = buildTokenChartOption(manyAgents(10), { mode: 'tokens', granularity: 'day', singleAgent: false })
    const series = opt.series as { name: string }[]
    expect(series.map((s) => s.name)).toContain('other')
    expect(series).toHaveLength(9)
  })

  it('overlays tokens-in line in single-agent mode', () => {
    const opt = buildTokenChartOption([row()], { mode: 'tokens', granularity: 'hour', singleAgent: true })
    const series = opt.series as { name: string; type: string }[]
    expect(series.some((s) => s.type === 'line')).toBe(true)
  })

  it('cost mode uses est_cost_usd', () => {
    const opt = buildTokenChartOption([row({ est_cost_usd: 1.5 })], { mode: 'cost', granularity: 'day', singleAgent: false })
    const series = opt.series as { data: number[] }[]
    expect(series[0].data).toEqual([1.5])
  })
})

describe('formatters', () => {
  it('formats cost and tokens', () => {
    expect(formatCost(0.0006)).toBe('$0.0006')
    expect(formatTokens(1500)).toBe('1.5k')
    expect(formatTokens(999)).toBe('999')
  })

  it('tiers cost formatting by magnitude', () => {
    expect(formatCost(0)).toBe('$0.0000')
    expect(formatCost(0.45)).toBe('$0.4500')
    expect(formatCost(12.3456)).toBe('$12.35')
    expect(formatCost(1234.567)).toBe('$1,234.57')
    expect(formatCost(12345.6789)).toBe('$12.3k')
    expect(formatCost(1030624)).toBe('$1.03M')
  })
})
