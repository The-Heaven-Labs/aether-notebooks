import { describe, expect, it } from 'vitest'
import { buildAddWidgetPayload, validTypesForSource } from './addWidgetPayload'

const layout = { row: 0, col: 0, width: 6, height: 8 }

describe('validTypesForSource', () => {
  it('offers text and the html panel for the source-less option', () => {
    expect(validTypesForSource('none')).toEqual(['text', 'html'])
    expect(validTypesForSource('none', true)).toEqual(['text', 'html'])
  })
  it('drops the html panel when the org switch is off', () => {
    expect(validTypesForSource('none', false)).toEqual(['text'])
  })
  it('offers table and chart for cell widgets', () => {
    expect(validTypesForSource('cell')).toEqual(['table', 'chart'])
  })
  it('offers metric for query widgets', () => {
    expect(validTypesForSource('query')).toEqual(['table', 'chart', 'metric'])
  })
  it('offers the custom component for the source-less option only when enabled', () => {
    expect(validTypesForSource('none', true, true)).toContain('custom')
    expect(validTypesForSource('none', true, false)).not.toContain('custom')
    // Default: off (the org switch is absent/disabled by default).
    expect(validTypesForSource('none', true)).not.toContain('custom')
  })
})

describe('buildAddWidgetPayload', () => {
  it('builds a source-less text widget', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'text', markdown: '## Hi', layout })).toEqual({
      type: 'text',
      layout,
      config: { text: { markdown: '## Hi' } },
    })
  })
  it('defaults a missing markdown to an empty string', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'text', layout })).toEqual({
      type: 'text',
      layout,
      config: { text: { markdown: '' } },
    })
  })
  it('coerces the type to text for source-less widgets', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'table', markdown: '# Hi', layout })).toEqual({
      type: 'text',
      layout,
      config: { text: { markdown: '# Hi' } },
    })
  })
  it('builds a source-less html panel with no source keys', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'html', layout })).toEqual({
      type: 'html',
      layout,
      config: {},
    })
  })
  it('builds a source-less custom component with an empty config', () => {
    expect(buildAddWidgetPayload({ source: 'none', type: 'custom', layout })).toEqual({
      type: 'custom',
      layout,
      config: {},
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
