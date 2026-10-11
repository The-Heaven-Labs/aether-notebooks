import { describe, expect, it } from 'vitest'
import { computeComparison, computeMetric, formatValue, metricConfigFrom, sparklinePoints } from './metric'

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

describe('computeComparison', () => {
  it('computes delta and percent against the first numeric baseline', () => {
    expect(computeComparison(result as never, { value_column: 'revenue', aggregation: 'sum', comparison: { column: 'prev' } })).toEqual({ delta: 100, percent: 100 })
  })
  it('skips non-numeric baseline values', () => {
    const mixed = {
      columns: [{ name: 'revenue' }, { name: 'prev' }],
      rows: [
        [100, 'n/a'],
        [20, 50],
      ],
    }
    expect(computeComparison(mixed as never, { value_column: 'revenue', aggregation: 'sum', comparison: { column: 'prev' } })).toEqual({ delta: 70, percent: 140 })
  })
  it('returns percent 0 when the baseline is zero', () => {
    const zero = { columns: [{ name: 'v' }, { name: 'p' }], rows: [[5, 0]] }
    expect(computeComparison(zero as never, { value_column: 'v', aggregation: 'sum', comparison: { column: 'p' } })).toEqual({ delta: 5, percent: 0 })
  })
  it('returns null for a missing comparison column, missing config, or null main value', () => {
    expect(computeComparison(result as never, { value_column: 'revenue', aggregation: 'sum', comparison: { column: 'missing' } })).toBeNull()
    expect(computeComparison(result as never, { value_column: 'revenue', aggregation: 'sum' })).toBeNull()
    expect(computeComparison(result as never, { value_column: 'nope', aggregation: 'sum', comparison: { column: 'prev' } })).toBeNull()
  })
})

describe('sparklinePoints', () => {
  it('sorts numeric x values numerically', () => {
    const numeric = { columns: [{ name: 'x' }, { name: 'y' }], rows: [[3, 30], [1, 10], [2, 20]] }
    expect(sparklinePoints(numeric as never, { sparkline: { x_column: 'x', y_column: 'y' } })).toEqual([[0, 10], [1, 20], [2, 30]])
  })
  it('sorts date-like string x values by parsed time', () => {
    const dated = { columns: [{ name: 'day' }, { name: 'y' }], rows: [['2026-10-02', 2], ['2026-10-01', 1]] }
    expect(sparklinePoints(dated as never, { sparkline: { x_column: 'day', y_column: 'y' } })).toEqual([[0, 1], [1, 2]])
  })
  it('preserves row order when x is neither numeric nor date-like', () => {
    const labels = { columns: [{ name: 'label' }, { name: 'y' }], rows: [['beta', 2], ['alpha', 1]] }
    expect(sparklinePoints(labels as never, { sparkline: { x_column: 'label', y_column: 'y' } })).toEqual([[0, 2], [1, 1]])
  })
  it('returns null with fewer than two numeric y rows or a missing column', () => {
    const mixed = { columns: [{ name: 'x' }, { name: 'y' }], rows: [[1, 10], [2, 'skip']] }
    expect(sparklinePoints(mixed as never, { sparkline: { x_column: 'x', y_column: 'y' } })).toBeNull()
    expect(sparklinePoints(result as never, { sparkline: { x_column: 'missing', y_column: 'revenue' } })).toBeNull()
    expect(sparklinePoints(result as never, { sparkline: { x_column: 'day', y_column: 'missing' } })).toBeNull()
  })
  it('does not mutate the input rows', () => {
    const rows = [[3, 30], [1, 10], [2, 20]]
    const numeric = { columns: [{ name: 'x' }, { name: 'y' }], rows }
    sparklinePoints(numeric as never, { sparkline: { x_column: 'x', y_column: 'y' } })
    expect(rows).toEqual([[3, 30], [1, 10], [2, 20]])
  })
})

describe('metricConfigFrom', () => {
  it('extracts the metric object from widget config', () => {
    expect(metricConfigFrom({ metric: { value_column: 'revenue', aggregation: 'sum' } })).toEqual({ value_column: 'revenue', aggregation: 'sum' })
  })
  it('falls back to an empty config for missing or non-object metric', () => {
    expect(metricConfigFrom(undefined)).toEqual({})
    expect(metricConfigFrom({ metric: 'sum' })).toEqual({})
  })
})

describe('computeMetric aggregations', () => {
  const values = { columns: [{ name: 'v' }], rows: [[1], [3], ['skip'], [2]] }
  it('computes min and max over numeric values', () => {
    expect(computeMetric(values as never, { value_column: 'v', aggregation: 'min' })).toBe(1)
    expect(computeMetric(values as never, { value_column: 'v', aggregation: 'max' })).toBe(3)
  })
  it('computes the median for odd and even counts', () => {
    expect(computeMetric(values as never, { value_column: 'v', aggregation: 'median' })).toBe(2)
    const even = { columns: [{ name: 'v' }], rows: [[1], [3], [2], [10]] }
    expect(computeMetric(even as never, { value_column: 'v', aggregation: 'median' })).toBe(2.5)
  })
  it('returns the first raw value for aggregation first', () => {
    expect(computeMetric(values as never, { value_column: 'v', aggregation: 'first' })).toBe(1)
  })
  it('counts distinct values with string coercion', () => {
    const coerced = { columns: [{ name: 'v' }], rows: [[1], ['1'], [2]] }
    expect(computeMetric(coerced as never, { value_column: 'v', aggregation: 'count_distinct' })).toBe(2)
  })
})
