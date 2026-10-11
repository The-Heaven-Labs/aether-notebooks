import { describe, expect, it } from 'vitest'
import { isQueryWidget, isTextPanel, widgetKindLabel } from './widgetKind'
import type { Widget } from '../types'

const base: Widget = {
  id: 'w1',
  dashboard_id: 'd1',
  type: 'table',
  layout: { row: 0, col: 0, width: 6, height: 8 },
  config: {},
  created_at: '2026-01-01T00:00:00Z',
}

function widget(overrides: Partial<Widget> = {}): Widget {
  return { ...base, ...overrides }
}

describe('widgetKindLabel', () => {
  it('labels query widgets', () => {
    expect(widgetKindLabel(widget({ connector_id: 'c1', query: 'SELECT 1' }))).toBe('Query')
  })
  it('labels source-less text panels', () => {
    expect(widgetKindLabel(widget({ type: 'text' }))).toBe('Text')
  })
  it('labels cell widgets', () => {
    expect(widgetKindLabel(widget({ notebook_id: 'n1', cell_id: 'k1' }))).toBe('Cell')
  })
  it('labels cell-linked text cells as cell widgets', () => {
    expect(widgetKindLabel(widget({ type: 'text', notebook_id: 'n1', cell_id: 'k1' }))).toBe('Cell')
  })
})

describe('isQueryWidget', () => {
  it('requires both a connector and a query', () => {
    expect(isQueryWidget(widget({ connector_id: 'c1', query: 'SELECT 1' }))).toBe(true)
    expect(isQueryWidget(widget({ connector_id: 'c1' }))).toBe(false)
    expect(isQueryWidget(widget({ query: 'SELECT 1' }))).toBe(false)
    expect(isQueryWidget(widget())).toBe(false)
  })
})

describe('isTextPanel', () => {
  it('accepts a source-less text panel', () => {
    expect(isTextPanel(widget({ type: 'text' }))).toBe(true)
  })
  it('rejects a cell-linked text cell', () => {
    expect(isTextPanel(widget({ type: 'text', notebook_id: 'n1', cell_id: 'k1' }))).toBe(false)
  })
  it('rejects a query widget', () => {
    expect(isTextPanel(widget({ connector_id: 'c1', query: 'SELECT 1' }))).toBe(false)
  })
  it('rejects a table cell widget', () => {
    expect(isTextPanel(widget({ notebook_id: 'n1', cell_id: 'k1' }))).toBe(false)
  })
})
