import { describe, expect, it } from 'vitest'
import { buildAddWidgetPayload } from './addWidgetPayload'

const layout = { row: 0, col: 0, width: 6, height: 8 }

describe('buildAddWidgetPayload', () => {
  it('builds a source-less text widget', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'text', markdown: '## Hi', layout })).toEqual({
      type: 'text',
      layout,
      config: { text: { markdown: '## Hi' } },
    })
  })
  it('builds a query metric widget', () => {
    expect(buildAddWidgetPayload({ source: 'query', type: 'metric', connectorId: 'c1', query: 'SELECT 1', layout })).toEqual({
      type: 'metric',
      layout,
      config: {},
      connector_id: 'c1',
      query: 'SELECT 1',
      language: 'sql',
    })
  })
  it('builds a cell widget as today', () => {
    expect(buildAddWidgetPayload({ source: 'cell', type: 'table', notebookId: 'n1', cellId: 'k1', layout })).toEqual({
      type: 'table',
      layout,
      config: {},
      notebook_id: 'n1',
      cell_id: 'k1',
    })
  })
})
