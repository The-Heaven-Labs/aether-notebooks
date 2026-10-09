import type { Widget } from '../types'

interface Rect {
  id: string
  x: number
  y: number
  w: number
  h: number
}

function overlaps(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max)
}

/**
 * Rescales every widget layout when the dashboard's column count changes so
 * nothing falls outside the new grid. Coordinates scale proportionally; a
 * small packing pass then pushes any rounding collisions straight down,
 * mirroring the editor's vertical compaction.
 */
export function rescaleWidgetLayouts(
  widgets: Widget[],
  oldCols: number,
  newCols: number,
): Array<{ id: string; layout: Widget['layout'] }> {
  if (oldCols <= 0 || newCols <= 0 || oldCols === newCols) {
    return widgets.map(w => ({ id: w.id, layout: w.layout }))
  }
  const ratio = newCols / oldCols
  const scaled: Rect[] = widgets.map(w => {
    const width = clamp(Math.round(w.layout.width * ratio), 1, newCols)
    const x = clamp(Math.round(w.layout.col * ratio), 0, newCols - width)
    return {
      id: w.id,
      x,
      y: w.layout.row,
      w: width,
      h: w.layout.height,
    }
  })

  const placed: Rect[] = []
  for (const rect of [...scaled].sort((a, b) => a.y - b.y || a.x - b.x)) {
    let y = rect.y
    while (placed.some(p => overlaps({ ...rect, y }, p))) y += 1
    placed.push({ ...rect, y })
  }

  const byId = new Map(placed.map(r => [r.id, r]))
  return widgets.map(w => {
    const r = byId.get(w.id)!
    return { id: w.id, layout: { row: r.y, col: r.x, width: r.w, height: r.h } }
  })
}
