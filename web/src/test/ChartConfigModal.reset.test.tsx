import { render, screen, fireEvent } from '@testing-library/react'
import { test, expect, vi } from 'vitest'
import { ChartConfigModal } from '../charts/ChartConfigModal'

const data = {
  columns: [
    { name: 'month', type: 'text' },
    { name: 'revenue', type: 'float' },
  ],
  rows: [['Jan', 1000], ['Feb', 1500]],
}

function renderModal(onResetToNotebook?: () => void) {
  const onSave = vi.fn()
  const onClose = vi.fn()
  render(
    <ChartConfigModal
      config={{ chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }}
      columns={['month', 'revenue']}
      data={data}
      groupValues={[]}
      onSave={onSave}
      onClose={onClose}
      onResetToNotebook={onResetToNotebook}
    />,
  )
  return { onSave, onClose }
}

test('renders reset button and resets then closes when clicked', () => {
  const onResetToNotebook = vi.fn()
  const { onClose } = renderModal(onResetToNotebook)

  const btn = screen.getByRole('button', { name: 'Reset to notebook' })
  fireEvent.click(btn)

  expect(onResetToNotebook).toHaveBeenCalledTimes(1)
  expect(onClose).toHaveBeenCalledTimes(1)
})

test('omits reset button when no callback is provided', () => {
  renderModal()
  expect(screen.queryByRole('button', { name: 'Reset to notebook' })).toBeNull()
})
