import type { Widget } from '../types'

/** True when the widget is backed by a connector query rather than a notebook cell. */
export function isQueryWidget(w: Widget): w is Widget & { connector_id: string; query: string } {
  return !!w.connector_id && !!w.query
}

/**
 * Label for the editor's widget-kind badge. Source-less text panels are
 * neither query nor cell widgets.
 */
export function widgetKindLabel(widget: Widget): 'Query' | 'Text' | 'Cell' {
  if (isQueryWidget(widget)) return 'Query'
  if (widget.type === 'text' && !widget.notebook_id && !widget.connector_id) return 'Text'
  return 'Cell'
}
