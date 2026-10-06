import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { RowAction, RowActionsBreak, RowActionsCell, RowActionsHeader } from './RowActions'

describe('RowAction', () => {
  test('renders an accessible icon button and fires onClick', () => {
    const onClick = vi.fn()
    render(<RowAction label="Edit thing" icon={<span data-testid="icon" />} accent onClick={onClick} />)
    const button = screen.getByRole('button', { name: 'Edit thing' })
    expect(button).toHaveAttribute('title', 'Edit thing')
    expect(button.className).toContain('row-action--accent')
    expect(screen.getByTestId('icon')).toBeInTheDocument()
    fireEvent.click(button)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  test('shows a spinner and disables the button while spinning', () => {
    render(<RowAction label="Test thing" icon={<span />} spinning disabled onClick={() => {}} />)
    const button = screen.getByRole('button', { name: 'Test thing' })
    expect(button).toBeDisabled()
    expect(button.querySelector('.row-action-spin')).not.toBeNull()
  })

  test('supports a danger variant and a longer tooltip than the accessible name', () => {
    render(<RowAction label="Delete thing" title="Delete thing permanently" danger icon={<span />} onClick={() => {}} />)
    const button = screen.getByRole('button', { name: 'Delete thing' })
    expect(button).toHaveAttribute('title', 'Delete thing permanently')
    expect(button.className).toContain('row-action--danger')
  })
})

describe('RowActionsCell', () => {
  test('wraps actions in the shared grid and supports a row break', () => {
    render(
      <table>
        <tbody>
          <tr>
            <RowActionsCell>
              <RowAction label="First" icon={<span />} onClick={() => {}} />
              <RowActionsBreak />
              <RowAction label="Second" icon={<span />} onClick={() => {}} />
            </RowActionsCell>
          </tr>
        </tbody>
      </table>,
    )
    const cell = screen.getByRole('cell')
    expect(cell.className).toContain('row-actions-cell')
    expect(cell.querySelector('.row-actions-grid')).not.toBeNull()
    expect(cell.querySelector('.row-actions-break')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'First' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Second' })).toBeInTheDocument()
  })
})

describe('RowActionsHeader', () => {
  test('renders a screen-reader-only column label', () => {
    render(
      <table>
        <thead>
          <tr><th><RowActionsHeader /></th></tr>
        </thead>
      </table>,
    )
    expect(screen.getByText('Actions')).toHaveClass('sr-only')
  })
})
