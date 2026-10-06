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

test('renders through a portal so a transformed ancestor cannot trap it', () => {
  // react-grid-layout items carry a CSS transform, which makes them the
  // containing block for fixed-position descendants. If the modal rendered
  // inline it would be confined to the widget card instead of the viewport.
  const { container } = render(
    <div style={{ transform: 'translateY(4px)' }}>
      <ChartConfigModal
        config={{ chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }}
        columns={['month', 'revenue']}
        data={data}
        groupValues={[]}
        onSave={vi.fn()}
        onClose={vi.fn()}
      />
    </div>,
  )
  const dialog = screen.getByRole('dialog', { name: 'Chart Configuration' })
  expect(container.contains(dialog)).toBe(false)
  expect(document.body.contains(dialog)).toBe(true)
})
