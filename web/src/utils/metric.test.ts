import { describe, expect, it } from 'vitest'
import { computeMetric, formatValue } from './metric'

const result = {
  columns: [{ name: 'revenue' }, { name: 'day' }, { name: 'prev' }],
  rows: [
    [120, '2026-10-01', 100],
    [80, '2026-10-02', 90],
  ],
}

describe('computeMetric', () => {
  it('sums the value column', () => {
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'sum' })).toBe(200)
  })
  it('returns the first value for aggregation none', () => {
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'none' })).toBe(120)
  })
  it('averages, counts, and counts distinct', () => {
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'avg' })).toBe(100)
    expect(computeMetric(result as never, { value_column: 'revenue', aggregation: 'count' })).toBe(2)
    expect(computeMetric(result as never, { value_column: 'day', aggregation: 'count_distinct' })).toBe(2)
  })
  it('returns null for an unknown column or empty rows', () => {
    expect(computeMetric(result as never, { value_column: 'nope', aggregation: 'sum' })).toBeNull()
    expect(computeMetric({ columns: [], rows: [] } as never, { value_column: 'x', aggregation: 'sum' })).toBeNull()
  })
})

describe('formatValue', () => {
  it('formats plain numbers with decimals', () => {
    expect(formatValue(1234.5, { style: 'number', decimals: 1 })).toBe('1,234.5')
  })
  it('formats currency', () => {
    expect(formatValue(200, { style: 'currency', currency: 'USD', decimals: 0 })).toBe('$200')
  })
  it('formats percent as-is with a suffix (no 100x multiplication)', () => {
    expect(formatValue(12.5, { style: 'percent', decimals: 1 })).toBe('12.5%')
  })
  it('formats compact numbers', () => {
    expect(formatValue(1_250_000, { style: 'number', compact: true })).toBe('1.3M')
  })
  it('renders null as an em dash', () => {
    expect(formatValue(null)).toBe('—')
  })
})
