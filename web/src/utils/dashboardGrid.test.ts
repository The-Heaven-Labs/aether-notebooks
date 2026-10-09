import { describe, test, expect } from 'vitest'
import { rescaleWidgetLayouts } from './dashboardGrid'
import type { Widget } from '../types'

function widget(id: string, col: number, row: number, width: number, height: number): Widget {
  return {
    id,
    dashboard_id: 'd1',
    type: 'chart',
    layout: { col, row, width, height },
  } as Widget
}

describe('rescaleWidgetLayouts', () => {
  test('scales positions and sizes proportionally', () => {
    const widgets = [widget('a', 0, 0, 6, 8), widget('b', 6, 0, 6, 8)]
    const out = rescaleWidgetLayouts(widgets, 12, 6)
    expect(out).toEqual([
      { id: 'a', layout: { col: 0, row: 0, width: 3, height: 8 } },
      { id: 'b', layout: { col: 3, row: 0, width: 3, height: 8 } },
    ])
  })

  test('keeps every widget inside the new grid', () => {
    const widgets = [widget('a', 8, 0, 4, 5)]
    const out = rescaleWidgetLayouts(widgets, 12, 8)
    const l = out[0].layout
    expect(l.col + l.width).toBeLessThanOrEqual(8)
  })

  test('pushes rounding collisions downward instead of overlapping', () => {
    // 10 cols -> 3 cols: both 5-wide panels clamp to width 2 at the same x.
    const widgets = [widget('a', 0, 0, 5, 4), widget('b', 5, 0, 5, 4)]
    const out = rescaleWidgetLayouts(widgets, 10, 3)
    const a = out.find(o => o.id === 'a')!.layout
    const b = out.find(o => o.id === 'b')!.layout
    const collide = a.col < b.col + b.width && b.col < a.col + a.width
      && a.row < b.row + b.height && b.row < a.row + a.height
    expect(collide).toBe(false)
  })

  test('is a no-op when the count does not change', () => {
    const widgets = [widget('a', 1, 2, 4, 6)]
    expect(rescaleWidgetLayouts(widgets, 12, 12)).toEqual([{ id: 'a', layout: { col: 1, row: 2, width: 4, height: 6 } }])
  })
})
