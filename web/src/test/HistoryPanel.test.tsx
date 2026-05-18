import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { HistoryPanel } from '../components/HistoryPanel'
import type { CellVersion } from '../types'

const versions: CellVersion[] = [
  { id: 'v1', cell_id: 'c1', source: 'SELECT 2', created_at: '2026-01-02T00:00:00Z', created_by: 'u1', user: { id: 'u1', name: 'Alice', email: 'alice@example.com' } },
  { id: 'v2', cell_id: 'c1', source: 'SELECT 1', created_at: '2026-01-01T00:00:00Z', created_by: 'u2', user: { id: 'u2', name: 'Bob', email: 'bob@example.com' } },
]

describe('HistoryPanel', () => {
  it('renders version list', () => {
    render(<HistoryPanel versions={versions} onRestore={vi.fn()} onClose={vi.fn()} currentSource="SELECT 2" />)
    expect(screen.getAllByRole('button', { name: /restore/i })).toHaveLength(2)
  })

  it('calls onRestore with version id', () => {
    const onRestore = vi.fn()
    render(<HistoryPanel versions={versions} onRestore={onRestore} onClose={vi.fn()} currentSource="SELECT 2" />)
    fireEvent.click(screen.getAllByRole('button', { name: /restore/i })[1])
    expect(onRestore).toHaveBeenCalledWith('v2')
  })

  it('calls onClose when close button clicked', () => {
    const onClose = vi.fn()
    render(<HistoryPanel versions={versions} onRestore={onClose} onClose={onClose} currentSource="SELECT 2" />)
    fireEvent.click(screen.getByTitle('Close history'))
    expect(onClose).toHaveBeenCalled()
  })
})
