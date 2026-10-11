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
