import { render, screen, fireEvent } from '@testing-library/react'
import { ChartView } from '../charts'
import { describe, test, expect, vi } from 'vitest'

// ECharts uses ResizeObserver
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const tableOutput = {
  type: 'table',
  data: {
    columns: [
      { name: 'month', type: 'text' },
      { name: 'revenue', type: 'float' },
    ],
    rows: [['Jan', 1000], ['Feb', 1500], ['Mar', 1200]],
  },
}

test('renders bar chart', () => {
  const config = { chartType: 'bar' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders line chart', () => {
  const config = { chartType: 'line' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders pie chart', () => {
  const config = { chartType: 'pie' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders timeline chart', () => {
  const output = {
    type: 'table',
    data: {
      columns: [
        { name: 'timestamp', type: 'timestamp' },
        { name: 'event', type: 'text' },
      ],
      rows: [['2024-01-01T10:00:00', 'Login'], ['2024-01-01T11:00:00', 'Logout']],
    },
    config: { chartType: 'timeline' as const },
  }
  render(<ChartView output={output} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders hierarchy tree chart', () => {
  const output = {
    type: 'table',
    data: {
      columns: [
        { name: 'pid', type: 'int4' },
        { name: 'ppid', type: 'int4' },
        { name: 'name', type: 'text' },
      ],
      rows: [[1, 0, 'init'], [2, 1, 'ssh'], [3, 1, 'nginx']],
    },
    config: { chartType: 'hierarchy_tree' as const },
  }
  render(<ChartView output={output} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('shows Configure button that toggles config panel', () => {
  const config = { chartType: 'bar' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} onConfigChange={() => {}} />)
  fireEvent.click(screen.getByRole('button', { name: /configure/i }))
  expect(screen.getByLabelText(/x axis/i)).toBeInTheDocument()
})

test('shows error for unknown chart type', () => {
  const config = { chartType: 'unknown' as any }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByText(/unknown chart type/i)).toBeInTheDocument()
})
