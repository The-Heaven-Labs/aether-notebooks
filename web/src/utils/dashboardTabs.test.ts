import { describe, test, expect } from 'vitest'
import { activeTabId, widgetsInTab } from './dashboardTabs'
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
