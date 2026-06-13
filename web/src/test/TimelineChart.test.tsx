import { render, screen } from '@testing-library/react'
import { TimelineModule } from '../charts/TimelineChart'
import { describe, test, expect } from 'vitest'

// ECharts uses ResizeObserver
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const timeData = {
  columns: [
    { name: 'ts', type: 'timestamp' },
    { name: 'msg', type: 'text' },
    { name: 'level', type: 'text' },
  ],
  rows: [
    ['2024-01-01T10:00:00', 'User logged in', 'info'],
    ['2024-01-01T10:05:00', 'Failed login', 'warn'],
    ['2024-01-01T10:10:00', 'User logged out', 'info'],
  ],
}

const rangeData = {
  columns: [
    { name: 'task', type: 'text' },
    { name: 'start', type: 'timestamp' },
    { name: 'end', type: 'timestamp' },
  ],
  rows: [
    ['Build', '2024-01-01T09:00:00', '2024-01-01T09:30:00'],
    ['Test', '2024-01-01T09:30:00', '2024-01-01T10:00:00'],
    ['Deploy', '2024-01-01T10:00:00', '2024-01-01T10:15:00'],
  ],
}

test('renders point-in-time events', () => {
  render(
    <TimelineModule.Component
      data={timeData}
      config={{ chartType: 'timeline', timeColumn: 'ts', labelColumn: 'msg' }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders range-based events', () => {
  render(
    <TimelineModule.Component
      data={rangeData}
      config={{ chartType: 'timeline', timeColumn: 'start', endTimeColumn: 'end', labelColumn: 'task' }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('auto-detects time columns', () => {
  const detected = TimelineModule.detectColumns(timeData.columns, timeData.rows)
  expect(detected.timeColumn).toBe('ts')
})

test('config panel renders time column selector', () => {
  const onChange = vi.fn()
  render(
    <TimelineModule.ConfigPanel
      config={{ chartType: 'timeline' }}
      columns={['ts', 'msg', 'level']}
      onChange={onChange}
    />
  )
  // Use exact label match to disambiguate from 'End time column'
  expect(screen.getAllByRole('combobox').length).toBeGreaterThanOrEqual(4)
  expect(screen.getByText('Time column')).toBeInTheDocument()
})
