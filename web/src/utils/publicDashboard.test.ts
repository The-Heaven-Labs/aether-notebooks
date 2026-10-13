import { describe, expect, it } from 'vitest'
import { visibleWidgets, variableDefaults } from './publicDashboard'
import type { DashboardVariable, Widget } from '../types'

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

const queryWidget = widget({ id: 'q1', connector_id: 'c1', query: 'SELECT 1' })
const cellWidget = widget({ id: 'k1', notebook_id: 'n1', cell_id: 'cell1' })
const textPanel = (id: string, markdown: string) =>
  widget({ id, type: 'text', config: { text: { markdown } } })
const htmlPanel = (id: string, html: string, css = '') =>
  widget({ id, type: 'html', config: { html, css } })

describe('visibleWidgets', () => {
  it('excludes query widgets when the dashboard is not live', () => {
    expect(visibleWidgets([queryWidget], {}, false)).toEqual([])
  })
  it('includes query widgets when the dashboard is live', () => {
    expect(visibleWidgets([queryWidget], {}, true)).toEqual([queryWidget])
  })
  it('includes cell widgets only when stored outputs exist', () => {
    expect(visibleWidgets([cellWidget], { cell1: { outputs: [{ type: 'table' }] } }, false)).toEqual([cellWidget])
    expect(visibleWidgets([cellWidget], { cell1: { outputs: [] } }, false)).toEqual([])
    expect(visibleWidgets([cellWidget], { cell1: {} }, false)).toEqual([])
    expect(visibleWidgets([cellWidget], {}, false)).toEqual([])
  })
  it('excludes text panels with empty or whitespace-only source', () => {
    expect(visibleWidgets([textPanel('t1', '   \n  ')], {}, false)).toEqual([])
    expect(visibleWidgets([textPanel('t1', '')], {}, true)).toEqual([])
  })
  it('includes text panels with source markdown', () => {
    const panel = textPanel('t1', '## What changed')
    expect(visibleWidgets([panel], {}, false)).toEqual([panel])
    expect(visibleWidgets([panel], {}, true)).toEqual([panel])
  })
  it('excludes html panels with no markup and no stylesheet', () => {
    expect(visibleWidgets([htmlPanel('h1', '   \n  ')], {}, false)).toEqual([])
    expect(visibleWidgets([htmlPanel('h1', '', '  ')], {}, true)).toEqual([])
  })
  it('includes authored html panels (markup or stylesheet), static or live', () => {
    const panel = htmlPanel('h1', '<div class="stat">42</div>')
    expect(visibleWidgets([panel], {}, false)).toEqual([panel])
    expect(visibleWidgets([panel], {}, true)).toEqual([panel])
    const cssOnly = htmlPanel('h2', '', '.stat { color: red; }')
    expect(visibleWidgets([cssOnly], {}, false)).toEqual([cssOnly])
  })
  it('excludes custom components with an empty bundle', () => {
    const empty = widget({ id: 'c1', type: 'custom', config: { html: '  ', css: '' } })
    expect(visibleWidgets([empty], {}, false)).toEqual([])
    expect(visibleWidgets([empty], {}, true)).toEqual([])
  })
  it('includes custom components with any authored part, static or live', () => {
    const component = widget({ id: 'c1', type: 'custom', config: { html: '<div id="root"></div>' } })
    expect(visibleWidgets([component], {}, false)).toEqual([component])
    expect(visibleWidgets([component], {}, true)).toEqual([component])
    const jsOnly = widget({ id: 'c2', type: 'custom', config: { js: '1' } })
    expect(visibleWidgets([jsOnly], {}, false)).toEqual([jsOnly])
  })
  it('filters a mixed list without reordering', () => {
    const empty = textPanel('t2', '  ')
    const panel = textPanel('t1', '## What changed')
    const list = [panel, queryWidget, cellWidget, empty]
    expect(visibleWidgets(list, { cell1: { outputs: [{ type: 'table' }] } }, false)).toEqual([panel, cellWidget])
  })
})

describe('variableDefaults', () => {
  it('preserves falsy defaults', () => {
    const variables: DashboardVariable[] = [
      { name: 'count', type: 'number', default: 0 },
      { name: 'enabled', type: 'boolean', default: false },
      { name: 'regions', type: 'multi_select', default: [] },
      { name: 'note', type: 'text', default: null },
    ]
    expect(variableDefaults(variables)).toEqual({ count: 0, enabled: false, regions: [], note: null })
  })
  it('omits variables without a configured default', () => {
    const variables: DashboardVariable[] = [
      { name: 'region', type: 'text', default: 'EMEA' },
      { name: 'bare', type: 'text' },
      { name: 'explicit', type: 'number', default: undefined },
    ]
    expect(variableDefaults(variables)).toEqual({ region: 'EMEA' })
  })
})
