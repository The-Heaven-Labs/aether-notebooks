import { describe, test, expect } from 'vitest'
import { activeTabId, widgetsInTab, widgetTabId, tabQueryString, orderWidgetsForEditor } from './dashboardTabs'
import type { DashboardTab, Widget } from '../types'

const TABS: DashboardTab[] = [
  { id: 'overview', title: 'Overview' },
  { id: 'orders', title: 'Orders' },
]

function widget(id: string, tab?: string): Widget {
  return {
    id,
    dashboard_id: 'd1',
    notebook_id: null,
    cell_id: null,
    connector_id: null,
    query: null,
    language: 'sql',
    type: 'table',
    layout: {
      row: 0,
      col: 0,
      width: 6,
      height: 6,
      ...(tab !== undefined ? { tab } : {}),
    },
    config: {},
    created_at: '2026-01-01T00:00:00Z',
  }
}

function placedWidget(id: string, row: number, col: number, tab?: string): Widget {
  const base = widget(id, tab)
  return { ...base, layout: { ...base.layout, row, col } }
}

describe('activeTabId', () => {
  test('returns the requested tab when it exists', () => {
    expect(activeTabId(TABS, 'orders')).toBe('orders')
  })

  test('falls back to the first tab for absent or unknown requests', () => {
    expect(activeTabId(TABS, null)).toBe('overview')
    expect(activeTabId(TABS, undefined)).toBe('overview')
    expect(activeTabId(TABS, '')).toBe('overview')
    expect(activeTabId(TABS, 'nope')).toBe('overview')
  })

  test('returns null when the dashboard defines no tabs', () => {
    expect(activeTabId([], 'orders')).toBeNull()
    expect(activeTabId([], null)).toBeNull()
  })
})

describe('widgetTabId', () => {
  test('maps a widget to its tab with the first-tab fallback', () => {
    expect(widgetTabId(widget('w1', 'orders'), TABS)).toBe('orders')
    expect(widgetTabId(widget('w2'), TABS)).toBe('overview')
    expect(widgetTabId(widget('w3', ''), TABS)).toBe('overview')
    expect(widgetTabId(widget('w4', 'gone'), TABS)).toBe('overview')
  })

  test('returns null when the dashboard defines no tabs', () => {
    expect(widgetTabId(widget('w1', 'orders'), [])).toBeNull()
    expect(widgetTabId(widget('w2'), [])).toBeNull()
  })
})

describe('widgetsInTab', () => {
  // w3: unassigned, w4: empty tab, w5: unknown tab — all fall to the first tab.
  const widgets = [
    widget('w1', 'overview'),
    widget('w2', 'orders'),
    widget('w3'),
    widget('w4', ''),
    widget('w5', 'gone'),
  ]

  test('returns every widget when the dashboard defines no tabs', () => {
    expect(widgetsInTab(widgets, [], null).map((w) => w.id)).toEqual(['w1', 'w2', 'w3', 'w4', 'w5'])
    expect(widgetsInTab(widgets, [], 'orders').map((w) => w.id)).toEqual(['w1', 'w2', 'w3', 'w4', 'w5'])
  })

  test('the first tab collects unassigned, empty, and unknown widget tabs', () => {
    expect(widgetsInTab(widgets, TABS, null).map((w) => w.id)).toEqual(['w1', 'w3', 'w4', 'w5'])
  })

  test('a known tab shows only the widgets assigned to it', () => {
    expect(widgetsInTab(widgets, TABS, 'orders').map((w) => w.id)).toEqual(['w2'])
  })

  test('an unknown requested tab falls back to the first tab', () => {
    expect(widgetsInTab(widgets, TABS, 'nope').map((w) => w.id)).toEqual(['w1', 'w3', 'w4', 'w5'])
    expect(widgetsInTab(widgets, TABS, '').map((w) => w.id)).toEqual(['w1', 'w3', 'w4', 'w5'])
  })
})

describe('orderWidgetsForEditor', () => {
  test('orders by chapter bucket before grid position', () => {
    const widgets = [
      placedWidget('orders-first', 0, 0, 'orders'),
      placedWidget('overview-last', 9, 9, 'overview'),
    ]
    expect(orderWidgetsForEditor(widgets, TABS).map((w) => w.id)).toEqual([
      'overview-last',
      'orders-first',
    ])
  })

  test('puts untabbed, empty, and unknown tabs in the first bucket', () => {
    const widgets = [
      placedWidget('orders-1', 0, 0, 'orders'),
      placedWidget('untabbed', 0, 0),
      placedWidget('empty', 0, 0, ''),
      placedWidget('unknown', 0, 0, 'gone'),
    ]
    // All first-bucket widgets share (0,0), so stable document order is kept.
    expect(orderWidgetsForEditor(widgets, TABS).map((w) => w.id)).toEqual([
      'untabbed',
      'empty',
      'unknown',
      'orders-1',
    ])
  })

  test('orders by row then column inside a bucket', () => {
    const widgets = [
      placedWidget('r1c2', 1, 2),
      placedWidget('r0c5', 0, 5),
      placedWidget('r0c1', 0, 1),
      placedWidget('r2c0', 2, 0),
    ]
    expect(orderWidgetsForEditor(widgets, TABS).map((w) => w.id)).toEqual([
      'r0c1',
      'r0c5',
      'r1c2',
      'r2c0',
    ])
  })

  test('is stable for equal positions', () => {
    const widgets = [
      placedWidget('a', 1, 1),
      placedWidget('b', 1, 1),
      placedWidget('c', 1, 1),
    ]
    expect(orderWidgetsForEditor(widgets, TABS).map((w) => w.id)).toEqual(['a', 'b', 'c'])
  })

  test('uses a single bucket ordered by row/col when there are no tabs', () => {
    const widgets = [
      placedWidget('b', 2, 0),
      placedWidget('a', 0, 0),
      placedWidget('c', 1, 0),
    ]
    expect(orderWidgetsForEditor(widgets, []).map((w) => w.id)).toEqual(['a', 'c', 'b'])
  })

  test('does not mutate the input array', () => {
    const widgets = [placedWidget('b', 2, 0), placedWidget('a', 0, 0)]
    const copy = [...widgets]
    orderWidgetsForEditor(widgets, TABS)
    expect(widgets).toEqual(copy)
  })
})

describe('tabQueryString', () => {
  test('returns ?tab=<encoded id> for a valid id', () => {
    expect(tabQueryString('orders')).toBe('?tab=orders')
    expect(tabQueryString('tab-2')).toBe('?tab=tab-2')
    expect(tabQueryString('a b')).toBe('?tab=a%20b')
  })

  test('returns an empty string for null, undefined, or empty id', () => {
    expect(tabQueryString(null)).toBe('')
    expect(tabQueryString(undefined)).toBe('')
    expect(tabQueryString('')).toBe('')
  })
})
