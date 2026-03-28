import { render, screen, fireEvent } from '@testing-library/react'
import { ChartView } from '../components/ChartView'

// Recharts uses ResizeObserver — mock it for jsdom
global.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const tableOutput = {
  type: 'table',
  data: {
    columns: [
      { name: 'month', type: 'string' },
      { name: 'revenue', type: 'float' },
    ],
    rows: [['Jan', 1000], ['Feb', 1500], ['Mar', 1200]],
  },
}

test('renders bar chart when type is bar', () => {
  const config = { chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }
  const { getByTestId } = render(<ChartView output={{ ...tableOutput, config }} />)
  expect(getByTestId('chart-container')).toBeInTheDocument()
})

test('renders line chart when type is line', () => {
  const config = { chartType: 'line', xAxis: 'month', yAxis: ['revenue'] }
  const { getByTestId } = render(<ChartView output={{ ...tableOutput, config }} />)
  expect(getByTestId('chart-container')).toBeInTheDocument()
})

test('renders pie chart when type is pie', () => {
  const config = { chartType: 'pie', xAxis: 'month', yAxis: ['revenue'] }
  const { getByTestId } = render(<ChartView output={{ ...tableOutput, config }} />)
  expect(getByTestId('chart-container')).toBeInTheDocument()
})

test('shows Configure button that toggles config panel', () => {
  const config = { chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }
  const onConfigChange = vi.fn()
  render(<ChartView output={{ ...tableOutput, config }} onConfigChange={onConfigChange} />)
  const btn = screen.getByRole('button', { name: /configure/i })
  fireEvent.click(btn)
  expect(screen.getByLabelText(/x axis/i)).toBeInTheDocument()
})

test('config panel calls onConfigChange when x-axis changes', () => {
  const config = { chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }
  const onConfigChange = vi.fn()
  render(<ChartView output={{ ...tableOutput, config }} onConfigChange={onConfigChange} />)
  fireEvent.click(screen.getByRole('button', { name: /configure/i }))
  fireEvent.change(screen.getByLabelText(/x axis/i), { target: { value: 'revenue' } })
  expect(onConfigChange).toHaveBeenCalledWith(expect.objectContaining({ xAxis: 'revenue' }))
})
