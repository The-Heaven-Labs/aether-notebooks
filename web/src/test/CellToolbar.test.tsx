import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CellToolbar } from '../components/CellToolbar'

const baseProps = {
  onRun: vi.fn(),
  onDelete: vi.fn(),
  running: false,
  cellType: 'code' as const,
  sourceVisible: true,
  cellCollapsed: false,
  onToggleSourceVisible: vi.fn(),
  onToggleCellCollapsed: vi.fn(),
  onShowHistory: vi.fn(),
}

describe('CellToolbar', () => {
  it('calls onToggleSourceVisible with false when source is visible', () => {
    const onToggle = vi.fn()
    render(<CellToolbar {...baseProps} sourceVisible={true} onToggleSourceVisible={onToggle} />)
    fireEvent.click(screen.getByTitle('Hide source'))
    expect(onToggle).toHaveBeenCalledWith(false)
  })

  it('calls onToggleSourceVisible with true when source is hidden', () => {
    const onToggle = vi.fn()
    render(<CellToolbar {...baseProps} sourceVisible={false} onToggleSourceVisible={onToggle} />)
    fireEvent.click(screen.getByTitle('Show source'))
    expect(onToggle).toHaveBeenCalledWith(true)
  })

  it('calls onToggleCellCollapsed with true when cell is expanded', () => {
    const onToggle = vi.fn()
    render(<CellToolbar {...baseProps} cellCollapsed={false} onToggleCellCollapsed={onToggle} />)
    fireEvent.click(screen.getByTitle('Collapse cell'))
    expect(onToggle).toHaveBeenCalledWith(true)
  })

  it('calls onShowHistory', () => {
    const onHistory = vi.fn()
    render(<CellToolbar {...baseProps} onShowHistory={onHistory} />)
    fireEvent.click(screen.getByTitle('Cell history'))
    expect(onHistory).toHaveBeenCalled()
  })
})
