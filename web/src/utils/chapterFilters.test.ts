import { describe, test, expect } from 'vitest'
import { variablesForChapter } from './chapterFilters'
import type { DashboardTab, DashboardVariable, Widget } from '../types'

function widget(overrides: Partial<Widget> = {}): Widget {
  return {
    id: 'w1',
    dashboard_id: 'd1',
    connector_id: 'c1',
    query: 'SELECT 1',
    language: 'sql',
    type: 'chart',
    layout: { row: 0, col: 0, width: 6, height: 6 },
    config: {},
    created_at: '2026-10-01T00:00:00Z',
    ...overrides,
  }
}

function variable(overrides: Partial<DashboardVariable> = {}): DashboardVariable {
  return { name: 'region', type: 'text', ...overrides }
}

const TABS: DashboardTab[] = [
  { id: 'overview', title: 'Overview' },
  { id: 'threats', title: 'Threats' },
]

describe('variablesForChapter', () => {
  test('direct references from a chapter query', () => {
    const variables = [variable({ name: 'country' }), variable({ name: 'window', type: 'date_range' })]
    const widgets = [
      widget({
        query: 'SELECT * FROM attacks WHERE c = {{country}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual(['country'])
  })

  test('text panel markdown counts as a reference', () => {
    const variables = [variable({ name: 'window', type: 'date_range' }), variable({ name: 'country' })]
    const widgets = [
      widget({
        type: 'text',
        query: null,
        config: { text: { markdown: '## Events in {{window}}' } },
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual(['window'])
  })

  test('transitive closure through an options query, to a fixpoint', () => {
    const variables = [
      variable({ name: 'territory' }),
      variable({
        name: 'region',
        options: { mode: 'query', query: { connector_id: 'c1', sql: 'SELECT DISTINCT t FROM r WHERE area = {{territory}}' } },
      }),
      variable({
        name: 'country',
        options: { mode: 'query', query: { connector_id: 'c1', sql: 'SELECT c FROM countries WHERE region = {{region}}' } },
      }),
    ]
    const widgets = [
      widget({
        query: 'SELECT * FROM attacks WHERE c = {{country}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual([
      'territory',
      'region',
      'country',
    ])
  })

  test('depends_on targets are included even without an options query', () => {
    const variables = [
      variable({ name: 'dashboard_id' }),
      variable({ name: 'country', depends_on: ['dashboard_id'] }),
    ]
    const widgets = [
      widget({
        query: 'SELECT * FROM attacks WHERE c = {{country}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual([
      'dashboard_id',
      'country',
    ])
  })

  test('widgets with no tab belong to the first chapter', () => {
    const variables = [variable({ name: 'country' })]
    const widgets = [widget({ query: 'SELECT {{country}}' })]
    expect(variablesForChapter(widgets, variables, TABS, 'overview').map((v) => v.name)).toEqual(['country'])
    expect(variablesForChapter(widgets, variables, TABS, 'threats')).toEqual([])
  })

  test('widgets naming an unknown tab fall back to the first chapter', () => {
    const variables = [variable({ name: 'country' })]
    const widgets = [
      widget({ query: 'SELECT {{country}}', layout: { row: 0, col: 0, width: 6, height: 6, tab: 'deleted-chapter' } }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'overview').map((v) => v.name)).toEqual(['country'])
    expect(variablesForChapter(widgets, variables, TABS, 'threats')).toEqual([])
  })

  test('no tabs → every variable', () => {
    const variables = [variable({ name: 'country' }), variable({ name: 'window', type: 'date_range' })]
    const widgets = [widget({ query: 'SELECT {{country}}' })]
    expect(variablesForChapter(widgets, variables, [], 'overview').map((v) => v.name)).toEqual(['country', 'window'])
  })

  test('a chapter using nothing → []', () => {
    const variables = [variable({ name: 'country' })]
    const widgets = [
      widget({
        query: 'SELECT {{country}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'overview' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats')).toEqual([])
  })

  test('date-range tokens resolve to their variable', () => {
    const variables = [variable({ name: 'window', type: 'date_range' })]
    const widgets = [
      widget({
        query: 'SELECT * FROM t WHERE ts BETWEEN {{ window_start }} AND {{window_end}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual(['window'])
  })

  test('unknown tokens are ignored', () => {
    const variables = [variable({ name: 'country' })]
    const widgets = [
      widget({
        query: 'SELECT * FROM t WHERE a = {{nope}} AND b = {{country}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual(['country'])
  })

  test('order follows the definitions list, not usage order', () => {
    const variables = [variable({ name: 'a' }), variable({ name: 'b' }), variable({ name: 'c' })]
    const widgets = [
      widget({
        query: 'SELECT {{c}}, {{a}}, {{b}}',
        layout: { row: 0, col: 0, width: 6, height: 6, tab: 'threats' },
      }),
    ]
    expect(variablesForChapter(widgets, variables, TABS, 'threats').map((v) => v.name)).toEqual(['a', 'b', 'c'])
  })
})
