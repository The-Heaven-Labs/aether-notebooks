import { describe, expect, it } from 'vitest'
import type { Cell } from '../types'
import { clearDirtyForSyncedCells, mergeServerCell, saveDelayFor, withDurationMetrics } from './mergeCells'

function cell(overrides: Partial<Cell> = {}): Cell {
  return {
    id: 'cell-1',
    notebook_id: 'nb-1',
    type: 'code',
    language: 'sql',
    source: 'SELECT 1',
    outputs: [],
    position: 0,
    created_at: '2026-09-14T00:00:00Z',
    updated_at: '2026-09-14T00:00:00Z',
    source_visible: true,
    cell_collapsed: false,
    ...overrides,
  }
}

describe('mergeServerCell', () => {
  it('adopts the server cell and derives metrics when there is no local copy', () => {
    const server = cell({ duration_ms: 1250 })
    const merged = mergeServerCell(undefined, server, { pendingExec: false, dirty: false })
    expect(merged).not.toBe(server)
    expect(merged.duration_ms).toBe(1250)
    expect(merged.metrics).toEqual({ connect_time_ms: 0, query_time_ms: 0, render_time_ms: 0, total_time_ms: 1250 })
  })

  it('adopts server title, limit and agent timestamps when the source is unchanged', () => {
    const local = cell({ title: 'Old title', limit: 1000 })
    const server = cell({
      title: 'New title',
      limit: 250,
      agent_updated_at: '2026-09-14T00:00:05Z',
      updated_at: '2026-09-14T00:00:05Z',
    })
    const merged = mergeServerCell(local, server, { pendingExec: false, dirty: false })
    expect(merged.title).toBe('New title')
    expect(merged.limit).toBe(250)
    expect(merged.agent_updated_at).toBe('2026-09-14T00:00:05Z')
    expect(merged.updated_at).toBe('2026-09-14T00:00:05Z')
  })

  it('keeps the local cell while it is executing so live outputs are not replaced', () => {
    const local = cell({ outputs: [{ type: 'table', data: [] }] })
    const server = cell({ outputs: [] })
    expect(mergeServerCell(local, server, { pendingExec: true, dirty: false })).toBe(local)
  })

  it('keeps a dirty local source but adopts the rest of the server cell', () => {
    const local = cell({ source: 'SELECT unsaved', title: 'Old title' })
    const server = cell({ source: 'SELECT 2', title: 'Server title' })
    const merged = mergeServerCell(local, server, { pendingExec: false, dirty: true })
    expect(merged.source).toBe('SELECT unsaved')
    expect(merged.title).toBe('Server title')
  })

  it('adopts the server source when the local source is clean', () => {
    const local = cell({ source: 'SELECT 1' })
    const server = cell({ source: 'SELECT 2' })
    const merged = mergeServerCell(local, server, { pendingExec: false, dirty: false })
    expect(merged.source).toBe('SELECT 2')
  })

  it('keeps local outputs when a differing server source arrives mid-execution', () => {
    const local = cell({ source: 'SELECT unsaved', outputs: [{ type: 'table', data: [] }] })
    const server = cell({ source: 'SELECT 2', outputs: [] })
    const merged = mergeServerCell(local, server, { pendingExec: true, dirty: true })
    expect(merged.source).toBe('SELECT unsaved')
    expect(merged.outputs).toEqual([{ type: 'table', data: [] }])
  })
})

describe('withDurationMetrics', () => {
  it('leaves an existing metrics object untouched', () => {
    const metrics = { connect_time_ms: 1, query_time_ms: 2, render_time_ms: 3, total_time_ms: 6 }
    const input = cell({ duration_ms: 6, metrics })
    expect(withDurationMetrics(input)).toBe(input)
  })
})

describe('clearDirtyForSyncedCells', () => {
  it('clears dirty for a cell whose local and server sources are equal', () => {
    const dirty = new Set(['cell-1'])
    const next = clearDirtyForSyncedCells(
      dirty,
      [cell({ id: 'cell-1', source: 'SELECT 1' })],
      [cell({ id: 'cell-1', source: 'SELECT 1' })],
    )
    expect(next.has('cell-1')).toBe(false)
  })

  it('keeps dirty when the local and server sources differ', () => {
    const next = clearDirtyForSyncedCells(
      new Set(['cell-1']),
      [cell({ id: 'cell-1', source: 'SELECT unsaved' })],
      [cell({ id: 'cell-1', source: 'SELECT 2' })],
    )
    expect(next.has('cell-1')).toBe(true)
  })

  it('keeps dirty when there is no local copy', () => {
    const next = clearDirtyForSyncedCells(
      new Set(['cell-1']),
      [],
      [cell({ id: 'cell-1', source: 'SELECT 2' })],
    )
    expect(next.has('cell-1')).toBe(true)
  })

  it('leaves unrelated ids untouched and does not mutate the input set', () => {
    const dirty = new Set(['cell-1', 'other'])
    const next = clearDirtyForSyncedCells(
      dirty,
      [cell({ id: 'cell-1', source: 'SELECT 1' })],
      [cell({ id: 'cell-1', source: 'SELECT 1' })],
    )
    expect(next.has('cell-1')).toBe(false)
    expect(next.has('other')).toBe(true)
    expect(dirty.has('cell-1')).toBe(true)
  })
})

describe('saveDelayFor', () => {
  const now = new Date('2026-09-14T00:00:10Z').getTime()

  it('uses the debounce when there is no agent timestamp', () => {
    expect(saveDelayFor(undefined, now)).toBe(1500)
    expect(saveDelayFor(null, now)).toBe(1500)
  })

  it('uses the debounce once the suppression window has passed', () => {
    expect(saveDelayFor('2026-09-14T00:00:00Z', now)).toBe(1500)
  })

  it('defers until the suppression window elapses for a fresh agent update', () => {
    expect(saveDelayFor('2026-09-14T00:00:10Z', now)).toBe(5000)
  })

  it('defers only the remainder of the window when part of it elapsed', () => {
    expect(saveDelayFor('2026-09-14T00:00:06Z', now)).toBe(1500)
    expect(saveDelayFor('2026-09-14T00:00:07Z', now)).toBe(2000)
  })
})
