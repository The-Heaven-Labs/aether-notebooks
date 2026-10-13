import { describe, expect, it } from 'vitest'
import { isCustomWidget, isHTMLPanel, isQueryWidget, isTextPanel, widgetKindLabel } from './widgetKind'
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
  it('labels source-less html panels', () => {
    expect(widgetKindLabel(widget({ type: 'html' }))).toBe('Panel')
  })
  it('labels source-less custom components', () => {
    expect(widgetKindLabel(widget({ type: 'custom' }))).toBe('Component')
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

describe('isHTMLPanel', () => {
  it('accepts a source-less html panel', () => {
    expect(isHTMLPanel(widget({ type: 'html' }))).toBe(true)
  })
  it('rejects a query-backed html widget', () => {
    expect(isHTMLPanel(widget({ type: 'html', connector_id: 'c1', query: 'SELECT 1' }))).toBe(false)
  })
  it('rejects other source-less types', () => {
    expect(isHTMLPanel(widget({ type: 'text' }))).toBe(false)
    expect(isHTMLPanel(widget())).toBe(false)
  })
})

describe('isCustomWidget', () => {
  it('accepts a source-less custom component', () => {
    expect(isCustomWidget(widget({ type: 'custom' }))).toBe(true)
  })
  it('rejects a query-backed custom widget', () => {
    expect(isCustomWidget(widget({ type: 'custom', connector_id: 'c1', query: 'SELECT 1' }))).toBe(false)
  })
  it('rejects other source-less types', () => {
    expect(isCustomWidget(widget({ type: 'html' }))).toBe(false)
    expect(isCustomWidget(widget())).toBe(false)
  })
})
