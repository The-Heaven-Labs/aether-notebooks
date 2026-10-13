import type { Widget } from '../types'

/** True when the widget is backed by a connector query rather than a notebook cell. */
export function isQueryWidget(w: Widget): w is Widget & { connector_id: string; query: string } {
  return !!w.connector_id && !!w.query
}

/** True for a source-less text panel: no notebook cell and no query behind it. */
export function isTextPanel(widget: Widget): boolean {
  return widget.type === 'text' && !widget.notebook_id && !widget.connector_id
}

/** True for a source-less html panel: no notebook cell and no query behind it. */
export function isHTMLPanel(widget: Widget): boolean {
  return widget.type === 'html' && !widget.notebook_id && !widget.connector_id
}

/** True for a source-less custom component: no notebook cell and no query behind it. */
export function isCustomWidget(widget: Widget): boolean {
  return widget.type === 'custom' && !widget.notebook_id && !widget.connector_id
}

/**
 * Label for the editor's widget-kind badge. Source-less text and html panels
 * are neither query nor cell widgets.
 */
export function widgetKindLabel(widget: Widget): 'Query' | 'Text' | 'Panel' | 'Component' | 'Cell' {
  if (isQueryWidget(widget)) return 'Query'
  if (isTextPanel(widget)) return 'Text'
  if (isHTMLPanel(widget)) return 'Panel'
  if (isCustomWidget(widget)) return 'Component'
  return 'Cell'
}
