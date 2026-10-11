import { describe, expect, it } from 'vitest'
import { DATA_WIDGET_MIN_H, TEXT_PANEL_MIN_H, toGridItem } from './widgetLayout'
import type { Widget } from '../types'

function widget(overrides: Partial<Widget>): Widget {
  return {
    id: 'w1',
    type: 'chart',
    layout: { row: 3, col: 2, width: 6, height: 4 },
    config: {},
    ...overrides,
  } as Widget
}

describe('toGridItem', () => {
  it('keeps the data-widget floor for charts and tables', () => {
    const item = toGridItem(widget({ type: 'chart' }))
    expect(item.h).toBe(DATA_WIDGET_MIN_H)
    expect(item.minH).toBe(DATA_WIDGET_MIN_H)
  })

  it('keeps the data-widget floor for cell-linked text cells', () => {
    const item = toGridItem(widget({ type: 'text', notebook_id: 'n1', cell_id: 'c1' }))
    expect(item.h).toBe(DATA_WIDGET_MIN_H)
    expect(item.minH).toBe(DATA_WIDGET_MIN_H)
  })

  it('lets a source-less text panel render at its authored height', () => {
    const item = toGridItem(widget({ type: 'text', layout: { row: 8, col: 0, width: 12, height: 3 } }))
    expect(item.h).toBe(3)
    expect(item.minH).toBe(TEXT_PANEL_MIN_H)
  })

  it('does not shrink a tall text panel', () => {
    const item = toGridItem(widget({ type: 'text', layout: { row: 0, col: 0, width: 6, height: 8 } }))
    expect(item.h).toBe(8)
  })

  it('carries position, size, and bounds through', () => {
    const item = toGridItem(widget({ layout: { row: 5, col: 7, width: 3, height: 9 } }))
    expect(item).toMatchObject({ x: 7, y: 5, w: 3, h: 9, minW: 2, maxH: 24 })
  })
})
