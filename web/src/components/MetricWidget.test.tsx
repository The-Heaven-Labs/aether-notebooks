import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MetricWidget } from './MetricWidget'
import type { ResultSet } from '../types'

const result = {
  columns: [{ name: 'revenue' }, { name: 'day' }, { name: 'prev' }],
  rows: [
    [120, '2026-10-01', 100],
    [80, '2026-10-02', 90],
  ],
} as unknown as ResultSet

describe('MetricWidget', () => {
  it('renders the label and the formatted aggregate', () => {
    render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'sum', label: 'Total revenue', format: { style: 'currency', currency: 'USD', decimals: 0 } }} />)
    expect(screen.getByText('Total revenue')).toBeInTheDocument()
    expect(screen.getByText('$200')).toBeInTheDocument()
  })

  it('shows the comparison as a signed percent', () => {
    render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'sum', comparison: { column: 'prev', mode: 'percent' } }} />)
    expect(screen.getByText('+100.0%')).toBeInTheDocument()
  })

  it('renders a sparkline polyline when configured', () => {
    const { container } = render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'none', sparkline: { x_column: 'day', y_column: 'revenue' } }} />)
    expect(container.querySelector('svg polyline')).toBeTruthy()
  })

  it('renders an em dash for a missing column', () => {
    render(<MetricWidget result={result} config={{ value_column: 'nope', aggregation: 'sum' }} />)
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('shows the comparison as a signed delta in delta mode', () => {
    render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'sum', comparison: { column: 'prev', mode: 'delta' } }} />)
    expect(screen.getByText('+100')).toBeInTheDocument()
  })

  it('renders a negative comparison with the error color and a down icon', () => {
    const down = {
      columns: [{ name: 'revenue' }, { name: 'prev' }],
      rows: [[80, 90]],
    } as unknown as ResultSet
    const { container } = render(<MetricWidget result={down} config={{ value_column: 'revenue', aggregation: 'none', comparison: { column: 'prev', mode: 'percent' } }} />)
    expect(screen.getByText('-11.1%')).toHaveStyle({ color: 'var(--error)' })
    expect(container.querySelector('.lucide-trending-down')).toBeTruthy()
  })

  it('renders an em dash instead of a percent when the comparison baseline is zero', () => {
    const zeroBaseline = {
      columns: [{ name: 'revenue' }, { name: 'prev' }],
      rows: [[200, 0]],
    } as unknown as ResultSet
    render(<MetricWidget result={zeroBaseline} config={{ value_column: 'revenue', aggregation: 'none', comparison: { column: 'prev', mode: 'percent' } }} />)
    expect(screen.getByText('—')).toHaveStyle({ color: 'var(--text-muted)' })
    expect(screen.queryByText('+0.0%')).not.toBeInTheDocument()
  })

  it('still renders a muted +0.0% for a genuine zero change', () => {
    const unchanged = {
      columns: [{ name: 'revenue' }, { name: 'prev' }],
      rows: [[200, 200]],
    } as unknown as ResultSet
    render(<MetricWidget result={unchanged} config={{ value_column: 'revenue', aggregation: 'none', comparison: { column: 'prev', mode: 'percent' } }} />)
    expect(screen.getByText('+0.0%')).toHaveStyle({ color: 'var(--text-muted)' })
  })

  it('includes the reading and the comparison in the group aria-label', () => {
    render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'sum', label: 'Total revenue', format: { style: 'currency', currency: 'USD', decimals: 0 }, comparison: { column: 'prev', mode: 'percent' } }} />)
    expect(screen.getByRole('group', { name: 'Total revenue: $200+100.0%' })).toBeInTheDocument()
  })

  it('teaches instead of crashing when no value column is configured', () => {
    render(<MetricWidget result={result} config={{}} />)
    expect(screen.getByText('Pick a value column in the widget settings.')).toBeInTheDocument()
  })

  it('normalizes sparkline points between min and max with 2px vertical padding', () => {
    const { container } = render(<MetricWidget result={result} config={{ value_column: 'revenue', aggregation: 'none', sparkline: { x_column: 'day', y_column: 'revenue' } }} />)
    expect(container.querySelector('svg polyline')?.getAttribute('points')).toBe('0.0,2.0 120.0,26.0')
  })

  it('draws a flat sparkline at mid-height when every point is equal', () => {
    const flat = {
      columns: [{ name: 'v' }, { name: 'd' }],
      rows: [
        [5, '2026-10-01'],
        [5, '2026-10-02'],
      ],
    } as unknown as ResultSet
    const { container } = render(<MetricWidget result={flat} config={{ value_column: 'v', aggregation: 'none', sparkline: { x_column: 'd', y_column: 'v' } }} />)
    expect(container.querySelector('svg polyline')?.getAttribute('points')).toBe('0.0,14.0 120.0,14.0')
  })
})
