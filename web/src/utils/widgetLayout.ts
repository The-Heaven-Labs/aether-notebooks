import type { LayoutItem } from 'react-grid-layout'
import { isTextPanel } from './widgetKind'
import type { Widget } from '../types'

/**
 * Minimum rendered height, in grid rows, for data widgets. The widget chrome
 * (meta bar + footer) plus a readable plot needs about six rows: without a
 * floor, a 4-row chart renders ~40px of plot under the chrome.
 */
export const DATA_WIDGET_MIN_H = 6

/**
 * Minimum rendered height for text panels. A panel is prose: a chapter
 * heading plus a line fits in two rows, so a heading panel does not sit in
 * 200px of dead space above the content below it.
 */
export const TEXT_PANEL_MIN_H = 2

/**
 * Minimum rendered height, in grid rows, for a widget by type. Text panels
 * get a tighter floor than data widgets, which need room for chrome plus a
 * readable plot.
 */
export function widgetMinRows(w: Widget): number {
  return isTextPanel(w) ? TEXT_PANEL_MIN_H : DATA_WIDGET_MIN_H
}

/**
 * Map a stored widget to its grid item. The height floor is type-aware:
 * `minH` alone only constrains resizing, so the rendered height is clamped
 * too, or the layout would reserve space the widget does not fill.
 */
export function toGridItem(w: Widget): LayoutItem {
  const minH = widgetMinRows(w)
  return {
    i: w.id,
    x: w.layout.col,
    y: w.layout.row,
    w: w.layout.width,
    h: Math.max(w.layout.height, minH),
    minW: 2,
    minH,
    maxH: 24,
  }
}
